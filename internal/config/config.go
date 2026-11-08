// Package config loads and validates the bot's configuration from the
// environment (optionally seeded from a .env file).
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
)

// DefaultEnvFile is where the original deployment mounted its .env file.
const DefaultEnvFile = "/.aws/config/.env"

// Config holds every setting the bot needs.
type Config struct {
	DiscordToken string
	ChannelID    string

	LastFMAPIKey string

	SpotifyID     string
	SpotifySecret string

	AWSRegion string
	TableName string

	// DigestSchedule is a standard 5-field cron expression evaluated in
	// DigestLocation.
	DigestSchedule string
	DigestLocation *time.Location
	ActivityGraph  bool

	// HealthAddr is the listen address for the HTTP health endpoint. Empty
	// disables it.
	HealthAddr string

	// WatchdogReconnectAfter is how long the Discord gateway may be unhealthy
	// before the watchdog forces a fresh connection.
	WatchdogReconnectAfter time.Duration
	// WatchdogExitAfter is how long the gateway may be unhealthy before the
	// process exits so its supervisor can restart it. Zero disables exiting.
	WatchdogExitAfter time.Duration

	LogLevel slog.Level
}

// LoadEnvFile loads KEY=VALUE pairs from path into the process environment
// without overriding variables that are already set. A missing file is not an
// error: the variables may come from the container environment instead.
func LoadEnvFile(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := godotenv.Load(path); err != nil {
		return fmt.Errorf("loading %s: %w", path, err)
	}
	return nil
}

// FromEnv reads the configuration from environment variables and validates it.
// Every problem is reported at once so a misconfigured deployment can be fixed
// in a single pass.
func FromEnv() (*Config, error) {
	var errs []error
	req := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", key))
		}
		return v
	}

	c := &Config{
		DiscordToken:   strings.TrimPrefix(req("DISCORD_TOKEN"), "Bot "),
		ChannelID:      req("CHANNEL_ID"),
		LastFMAPIKey:   req("LASTFM_API_KEY"),
		TableName:      req("TABLE_NAME"),
		SpotifyID:      strings.TrimSpace(os.Getenv("SPOTIFY_ID")),
		SpotifySecret:  strings.TrimSpace(os.Getenv("SPOTIFY_SECRET")),
		AWSRegion:      envOr("AWS_REGION", "us-west-2"),
		DigestSchedule: envOr("DIGEST_SCHEDULE", "30 12 * * 6"), // Saturdays 12:30
	}

	if _, err := cron.ParseStandard(c.DigestSchedule); err != nil {
		errs = append(errs, fmt.Errorf("DIGEST_SCHEDULE %q is not a valid cron expression: %w", c.DigestSchedule, err))
	}

	tz := envOr("DIGEST_TIMEZONE", "UTC")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		errs = append(errs, fmt.Errorf("DIGEST_TIMEZONE %q: %w", tz, err))
	}
	c.DigestLocation = loc

	c.ActivityGraph, err = envBool("DIGEST_ACTIVITY_GRAPH", false)
	if err != nil {
		errs = append(errs, err)
	}

	switch port := envOr("HEALTH_PORT", envOr("PORT", "8080")); strings.ToLower(port) {
	case "", "0", "off", "false", "disabled":
		c.HealthAddr = ""
	default:
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			errs = append(errs, fmt.Errorf("HEALTH_PORT %q is not a valid port", port))
		}
		c.HealthAddr = ":" + port
	}

	c.WatchdogReconnectAfter, err = envDuration("WATCHDOG_RECONNECT_AFTER", 5*time.Minute)
	if err != nil {
		errs = append(errs, err)
	}
	c.WatchdogExitAfter, err = envDuration("WATCHDOG_EXIT_AFTER", 30*time.Minute)
	if err != nil {
		errs = append(errs, err)
	}

	if err := c.LogLevel.UnmarshalText([]byte(envOr("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}

	if (c.SpotifyID == "") != (c.SpotifySecret == "") {
		errs = append(errs, errors.New("SPOTIFY_ID and SPOTIFY_SECRET must be set together"))
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) (bool, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback, fmt.Errorf("%s %q is not a boolean", key, v)
	}
	return b, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return fallback, fmt.Errorf("%s %q is not a valid duration (e.g. 10m)", key, v)
	}
	return d, nil
}
