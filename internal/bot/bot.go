// Package bot owns the Discord connection: it keeps the gateway alive,
// registers slash commands, dispatches interactions and posts digests.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andrewcgraves/lastfm-stats-discord/internal/digest"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/lastfm"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/store"
	"github.com/bwmarrin/discordgo"
)

// A heartbeat ACK arrives every ~41s on a healthy connection. discordgo itself
// gives up on a connection after 5 missed ACKs; this is a little longer so the
// watchdog only steps in when discordgo's own recovery has not worked.
const heartbeatStaleAfter = 4 * time.Minute

// DigestBuilder builds the weekly digest.
type DigestBuilder interface {
	Build(ctx context.Context) (*digest.Digest, error)
}

// UserLookup validates Last.fm usernames.
type UserLookup interface {
	UserInfo(ctx context.Context, user string) (*lastfm.UserInfo, error)
}

// Options configures a Bot.
type Options struct {
	Token     string
	ChannelID string
	Digest    DigestBuilder
	Store     store.Store
	LastFM    UserLookup
	Log       *slog.Logger
}

// Bot is a running Discord bot.
type Bot struct {
	s         *discordgo.Session
	log       *slog.Logger
	digest    DigestBuilder
	store     store.Store
	lastfm    UserLookup
	channelID string
	handlers  map[string]commandHandler

	// ctx is cancelled on shutdown; interaction handlers derive from it.
	ctx    context.Context
	cancel context.CancelFunc

	regMu         sync.Mutex
	registered    map[string]bool
	clearedGlobal atomic.Bool

	// digestMu ensures only one digest is generated at a time.
	digestMu sync.Mutex

	reconnecting atomic.Bool
}

// New creates a bot. It does not connect; call Start.
func New(opts Options) (*Bot, error) {
	if opts.Token == "" {
		return nil, errors.New("discord token is empty")
	}
	s, err := discordgo.New("Bot " + opts.Token)
	if err != nil {
		return nil, err
	}
	// Slash commands only need guild events; asking for less means less
	// gateway traffic and no privileged-intent surprises.
	s.Identify.Intents = discordgo.IntentsGuilds
	s.ShouldReconnectOnError = true
	s.ShouldRetryOnRateLimit = true
	s.MaxRestRetries = 3
	s.StateEnabled = true
	s.Client = &http.Client{Timeout: 30 * time.Second}
	s.LogLevel = discordgo.LogWarning
	routeDiscordgoLogs(opts.Log)

	ctx, cancel := context.WithCancel(context.Background())
	b := &Bot{
		s:          s,
		log:        opts.Log,
		digest:     opts.Digest,
		store:      opts.Store,
		lastfm:     opts.LastFM,
		channelID:  opts.ChannelID,
		ctx:        ctx,
		cancel:     cancel,
		registered: map[string]bool{},
	}
	b.handlers = map[string]commandHandler{
		"ping":           b.handlePing,
		"link-lastfm":    b.handleLinkLastFM,
		"manual-trigger": b.handleManualTrigger,
	}

	s.AddHandler(b.onReady)
	s.AddHandler(b.onResumed)
	s.AddHandler(b.onConnect)
	s.AddHandler(b.onDisconnect)
	s.AddHandler(b.onGuildCreate)
	s.AddHandler(b.onInteraction)
	return b, nil
}

// ErrUnauthorized means Discord rejected the bot token. Retrying won't help.
var ErrUnauthorized = errors.New("discord rejected the bot token (401 Unauthorized)")

// Start verifies the token and opens the gateway connection, retrying with
// backoff for as long as Discord is unreachable. It returns once connected,
// when ctx is cancelled, or with ErrUnauthorized.
func (b *Bot) Start(ctx context.Context) error {
	err := retry(ctx, b.log, "checking discord token", func() (bool, error) {
		_, err := b.s.User("@me", discordgo.WithContext(ctx))
		var rest *discordgo.RESTError
		if errors.As(err, &rest) && rest.Response != nil && rest.Response.StatusCode == http.StatusUnauthorized {
			return false, ErrUnauthorized
		}
		return true, err
	})
	if err != nil {
		return err
	}

	return retry(ctx, b.log, "opening discord gateway", func() (bool, error) {
		err := b.s.Open()
		if errors.Is(err, discordgo.ErrWSAlreadyOpen) {
			return false, nil
		}
		return true, err
	})
}

// Close disconnects from Discord and cancels in-flight interaction handlers.
func (b *Bot) Close() {
	b.cancel()
	if err := b.s.Close(); err != nil {
		b.log.Warn("closing discord session", "err", err)
	}
}

// Status describes the gateway connection.
type Status struct {
	Connected        bool      `json:"connected"`
	LastHeartbeatAck time.Time `json:"last_heartbeat_ack"`
	Latency          string    `json:"latency"`
	Reason           string    `json:"reason,omitempty"`
}

// Healthy reports whether the gateway connection is up and heartbeating.
// It never blocks for long, even if the session is wedged.
func (b *Bot) Healthy() Status {
	ch := make(chan Status, 1)
	go func() {
		b.s.RLock()
		ready := b.s.DataReady
		ack := b.s.LastHeartbeatAck
		sent := b.s.LastHeartbeatSent
		b.s.RUnlock()

		st := Status{Connected: ready, LastHeartbeatAck: ack, Latency: ack.Sub(sent).Round(time.Millisecond).String()}
		switch {
		case !ready:
			st.Reason = "gateway not connected"
		case time.Since(ack) > heartbeatStaleAfter:
			st.Connected = false
			st.Reason = fmt.Sprintf("no heartbeat ACK for %s", time.Since(ack).Round(time.Second))
		}
		ch <- st
	}()
	select {
	case st := <-ch:
		return st
	case <-time.After(10 * time.Second):
		return Status{Reason: "discord session is locked (stuck connecting?)"}
	}
}

// forceReconnect tears the gateway connection down and opens a new one. It is
// the watchdog's tool for sessions that discordgo has not managed to recover.
func (b *Bot) forceReconnect() {
	if !b.reconnecting.CompareAndSwap(false, true) {
		b.log.Warn("forced reconnect already in progress")
		return
	}
	go func() {
		defer b.reconnecting.Store(false)
		b.log.Warn("forcing a fresh discord gateway connection")
		if err := b.s.Close(); err != nil {
			b.log.Warn("closing stale session", "err", err)
		}
		if err := b.s.Open(); err != nil && !errors.Is(err, discordgo.ErrWSAlreadyOpen) {
			b.log.Error("forced reconnect failed; will try again", "err", err)
			return
		}
		b.log.Info("forced reconnect succeeded")
	}()
}

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	b.log.Info("connected to discord", "user", r.User.Username, "guilds", len(r.Guilds), "session", r.SessionID)
	if err := s.UpdateListeningStatus("your music..."); err != nil {
		b.log.Warn("setting presence", "err", err)
	}
	// Earlier versions registered commands globally as well as per guild.
	// Clear any global ones once so they don't appear twice.
	if b.clearedGlobal.CompareAndSwap(false, true) {
		b.safeGo("clear global commands", func() {
			if _, err := s.ApplicationCommandBulkOverwrite(r.User.ID, "", []*discordgo.ApplicationCommand{}, discordgo.WithContext(b.ctx)); err != nil {
				b.clearedGlobal.Store(false)
				b.log.Warn("clearing global commands", "err", err)
			}
		})
	}
}

func (b *Bot) onResumed(*discordgo.Session, *discordgo.Resumed) {
	b.log.Info("resumed discord gateway session")
}

func (b *Bot) onConnect(*discordgo.Session, *discordgo.Connect) {
	b.log.Info("discord gateway connected")
}

func (b *Bot) onDisconnect(*discordgo.Session, *discordgo.Disconnect) {
	b.log.Warn("discord gateway disconnected; discordgo will reconnect")
}

func (b *Bot) onGuildCreate(s *discordgo.Session, g *discordgo.GuildCreate) {
	if g.Unavailable {
		return
	}
	b.safeGo("register commands", func() { b.registerCommands(s, g.ID, g.Name) })
}

// registerCommands makes the guild's commands exactly match Commands in one
// idempotent API call.
func (b *Bot) registerCommands(s *discordgo.Session, guildID, guildName string) {
	b.regMu.Lock()
	defer b.regMu.Unlock()
	if b.registered[guildID] || s.State.User == nil {
		return
	}
	appID := s.State.User.ID
	err := retry(b.ctx, b.log, "registering commands", func() (bool, error) {
		_, err := s.ApplicationCommandBulkOverwrite(appID, guildID, Commands, discordgo.WithContext(b.ctx))
		return true, err
	}, maxAttempts(5))
	if err != nil {
		b.log.Error("registering commands failed", "guild", guildName, "err", err)
		return
	}
	b.registered[guildID] = true
	b.log.Info("registered slash commands", "guild", guildName, "count", len(Commands))
}

func (b *Bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	name := i.ApplicationCommandData().Name
	h, ok := b.handlers[name]
	if !ok {
		b.log.Warn("unknown command", "command", name)
		return
	}
	log := b.log.With("command", name, "user", interactionUser(i).ID, "guild", i.GuildID)
	defer func() {
		if r := recover(); r != nil {
			log.Error("command panicked", "panic", r, "stack", string(debug.Stack()))
			b.respondError(s, i, "Something went wrong running that command.")
		}
	}()
	start := time.Now()
	h(s, i, log)
	log.Info("handled command", "took", time.Since(start).Round(time.Millisecond))
}

// safeGo runs fn in a goroutine that cannot crash the process.
func (b *Bot) safeGo(what string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.log.Error("background task panicked", "task", what, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}

func interactionUser(i *discordgo.InteractionCreate) *discordgo.User {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User
	}
	if i.User != nil {
		return i.User
	}
	return &discordgo.User{}
}

// routeDiscordgoLogs sends discordgo's internal logging through slog.
func routeDiscordgoLogs(log *slog.Logger) {
	discordgo.Logger = func(msgL, caller int, format string, a ...interface{}) {
		level := slog.LevelDebug
		switch msgL {
		case discordgo.LogError:
			level = slog.LevelError
		case discordgo.LogWarning:
			level = slog.LevelWarn
		case discordgo.LogInformational:
			level = slog.LevelInfo
		}
		log.Log(context.Background(), level, fmt.Sprintf(format, a...), "source", "discordgo")
	}
}
