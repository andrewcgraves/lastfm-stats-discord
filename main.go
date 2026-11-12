// Command lastfm-stats-discord is a Discord bot that posts a weekly digest of
// its members' Last.fm listening.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // DIGEST_TIMEZONE works even in minimal containers

	"github.com/andrewcgraves/lastfm-stats-discord/internal/artwork"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/bot"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/config"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/digest"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/lastfm"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/store"
	"github.com/robfig/cron/v3"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	os.Exit(run())
}

func loadEnv() {
	envFile := config.DefaultEnvFile
	if v := os.Getenv("ENV_FILE"); v != "" {
		envFile = v
	}
	for _, f := range []string{envFile, ".env"} {
		if err := config.LoadEnvFile(f); err != nil {
			fmt.Fprintln(os.Stderr, "warning:", err)
		}
	}
}

func run() int {
	loadEnv()
	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration:\n%v\n", err)
		return 2
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)
	log.Info("starting lastfm-stats-discord", "version", version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, fail := context.WithCancelCause(ctx)
	defer fail(nil)

	db, err := store.NewDynamo(ctx, cfg.AWSRegion, cfg.TableName)
	if err != nil {
		log.Error("setting up DynamoDB", "err", err)
		return 1
	}
	// A database outage shouldn't keep the bot offline: warn and carry on.
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	if err := db.Ping(pingCtx); err != nil {
		log.Warn("DynamoDB table not reachable yet; continuing", "table", cfg.TableName, "err", err)
	} else {
		log.Info("connected to DynamoDB", "table", cfg.TableName)
	}
	cancel()

	lfm := lastfm.New(cfg.LastFMAPIKey)

	var art artwork.Finder = artwork.None{}
	if cfg.SpotifyID != "" {
		art = artwork.NewSpotify(cfg.SpotifyID, cfg.SpotifySecret)
		log.Info("spotify artist images enabled")
	} else {
		log.Info("SPOTIFY_ID not set; digests will have no artist images")
	}

	builder := &digest.Builder{
		LastFM:   lfm,
		Artwork:  art,
		Users:    db,
		Log:      log.With("component", "digest"),
		Graph:    cfg.ActivityGraph,
		Location: cfg.DigestLocation,
	}

	b, err := bot.New(bot.Options{
		Token:     cfg.DiscordToken,
		ChannelID: cfg.ChannelID,
		Digest:    builder,
		Store:     db,
		LastFM:    lfm,
		Log:       log.With("component", "discord"),
	})
	if err != nil {
		log.Error("creating discord session", "err", err)
		return 1
	}
	if err := b.Start(ctx); err != nil {
		if ctx.Err() != nil {
			return 0
		}
		log.Error("connecting to discord", "err", err)
		return 1
	}

	var srv *http.Server
	if cfg.HealthAddr != "" {
		srv = &http.Server{Addr: cfg.HealthAddr, Handler: b.HealthHandler(), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			log.Info("health endpoint listening", "addr", cfg.HealthAddr+"/healthz")
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Warn("health endpoint stopped", "err", err)
			}
		}()
	}

	cronLog := cronLogger{log.With("component", "scheduler")}
	sched := cron.New(
		cron.WithLocation(cfg.DigestLocation),
		cron.WithChain(cron.Recover(cronLog), cron.SkipIfStillRunning(cronLog)),
	)
	if _, err := sched.AddFunc(cfg.DigestSchedule, func() { b.RunScheduledDigest(ctx) }); err != nil {
		log.Error("scheduling digest", "err", err)
		return 1
	}
	sched.Start()
	if entries := sched.Entries(); len(entries) > 0 {
		log.Info("weekly digest scheduled", "schedule", cfg.DigestSchedule,
			"timezone", cfg.DigestLocation.String(), "next_run", entries[0].Next.Format(time.RFC3339))
	}

	go b.Watchdog(ctx, 30*time.Second, cfg.WatchdogReconnectAfter, cfg.WatchdogExitAfter, func(err error) {
		fail(err)
	})

	<-ctx.Done()
	cause := context.Cause(ctx)
	log.Info("shutting down", "reason", cause)

	// Give a running digest a moment to finish, then close everything.
	select {
	case <-sched.Stop().Done():
	case <-time.After(30 * time.Second):
		log.Warn("timed out waiting for scheduled job to finish")
	}
	if srv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(shutdownCtx)
		cancel()
	}
	// If discordgo is wedged (the reason the watchdog may have fired), Close
	// can block forever; never let that stop the process from exiting.
	closed := make(chan struct{})
	go func() { b.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		log.Warn("timed out closing the discord session")
	}

	if cause != nil && !errors.Is(cause, context.Canceled) {
		log.Error("exiting so the supervisor can restart the bot", "err", cause)
		return 1
	}
	return 0
}

// healthcheck is used by the Docker HEALTHCHECK: it exits 0 when the running
// bot reports a healthy gateway connection.
func healthcheck() int {
	loadEnv()
	port := os.Getenv("HEALTH_PORT")
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "unhealthy:", resp.Status)
		return 1
	}
	return 0
}

// cronLogger adapts slog to cron.Logger.
type cronLogger struct{ l *slog.Logger }

func (c cronLogger) Info(msg string, kv ...any) { c.l.Debug(msg, kv...) }
func (c cronLogger) Error(err error, msg string, kv ...any) {
	c.l.Error(msg, append(kv, "err", err)...)
}
