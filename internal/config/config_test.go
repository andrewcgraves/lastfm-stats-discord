package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var allKeys = []string{
	"DISCORD_TOKEN", "CHANNEL_ID", "LASTFM_API_KEY", "TABLE_NAME", "SPOTIFY_ID", "SPOTIFY_SECRET",
	"AWS_REGION", "DIGEST_SCHEDULE", "DIGEST_TIMEZONE", "DIGEST_ACTIVITY_GRAPH", "HEALTH_PORT", "PORT",
	"WATCHDOG_RECONNECT_AFTER", "WATCHDOG_EXIT_AFTER", "LOG_LEVEL",
}

// setEnv clears every config variable, then applies vals.
func setEnv(t *testing.T, vals map[string]string) {
	t.Helper()
	for _, k := range allKeys {
		t.Setenv(k, "")
	}
	for k, v := range vals {
		t.Setenv(k, v)
	}
}

func required() map[string]string {
	return map[string]string{
		"DISCORD_TOKEN":  "tok",
		"CHANNEL_ID":     "123",
		"LASTFM_API_KEY": "key",
		"TABLE_NAME":     "table",
	}
}

func TestDefaults(t *testing.T) {
	setEnv(t, required())
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.DigestSchedule != "30 12 * * 6" || c.DigestLocation != time.UTC {
		t.Errorf("schedule = %q in %v, want Saturdays 12:30 UTC (the original schedule)", c.DigestSchedule, c.DigestLocation)
	}
	if c.AWSRegion != "us-west-2" || c.HealthAddr != ":8080" || c.ActivityGraph {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if c.WatchdogReconnectAfter != 5*time.Minute || c.WatchdogExitAfter != 30*time.Minute {
		t.Errorf("unexpected watchdog defaults: %v / %v", c.WatchdogReconnectAfter, c.WatchdogExitAfter)
	}
}

func TestReportsEveryMissingVariable(t *testing.T) {
	setEnv(t, nil)
	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, k := range []string{"DISCORD_TOKEN", "CHANNEL_ID", "LASTFM_API_KEY", "TABLE_NAME"} {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("error should mention %s: %v", k, err)
		}
	}
}

func TestInvalidValues(t *testing.T) {
	tests := map[string]map[string]string{
		"bad schedule":       {"DIGEST_SCHEDULE": "every saturday"},
		"bad timezone":       {"DIGEST_TIMEZONE": "Mars/Olympus"},
		"bad bool":           {"DIGEST_ACTIVITY_GRAPH": "sometimes"},
		"bad port":           {"HEALTH_PORT": "99999"},
		"bad duration":       {"WATCHDOG_EXIT_AFTER": "soon"},
		"negative duration":  {"WATCHDOG_RECONNECT_AFTER": "-5m"},
		"bad log level":      {"LOG_LEVEL": "loud"},
		"half spotify creds": {"SPOTIFY_ID": "id"},
	}
	for name, extra := range tests {
		t.Run(name, func(t *testing.T) {
			vals := required()
			var key string
			for k, v := range extra {
				vals[k] = v
				key = k
			}
			setEnv(t, vals)
			_, err := FromEnv()
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("expected an error mentioning %s, got %v", key, err)
			}
		})
	}
}

func TestOverrides(t *testing.T) {
	vals := required()
	vals["DISCORD_TOKEN"] = "Bot abc"
	vals["DIGEST_SCHEDULE"] = "0 9 * * 1"
	vals["DIGEST_TIMEZONE"] = "America/Los_Angeles"
	vals["DIGEST_ACTIVITY_GRAPH"] = "true"
	vals["HEALTH_PORT"] = "off"
	vals["WATCHDOG_EXIT_AFTER"] = "0"
	vals["SPOTIFY_ID"] = "id"
	vals["SPOTIFY_SECRET"] = "secret"
	vals["LOG_LEVEL"] = "debug"
	setEnv(t, vals)

	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.DiscordToken != "abc" {
		t.Errorf("a pasted \"Bot \" prefix should be stripped, got %q", c.DiscordToken)
	}
	if c.DigestLocation.String() != "America/Los_Angeles" || !c.ActivityGraph || c.HealthAddr != "" || c.WatchdogExitAfter != 0 {
		t.Errorf("overrides not applied: %+v", c)
	}
}

func TestPortFallback(t *testing.T) {
	vals := required()
	vals["PORT"] = "9000"
	setEnv(t, vals)
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.HealthAddr != ":9000" {
		t.Fatalf("HealthAddr = %q, want :9000", c.HealthAddr)
	}
}

func TestLoadEnvFile(t *testing.T) {
	if err := LoadEnvFile(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatalf("a missing file should be ignored, got %v", err)
	}
	if err := LoadEnvFile(""); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("CFG_TEST_NEW=from-file\nCFG_TEST_SET=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CFG_TEST_SET", "from-env")
	t.Setenv("CFG_TEST_NEW", "")
	os.Unsetenv("CFG_TEST_NEW")

	if err := LoadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("CFG_TEST_NEW"); got != "from-file" {
		t.Errorf("CFG_TEST_NEW = %q, want from-file", got)
	}
	if got := os.Getenv("CFG_TEST_SET"); got != "from-env" {
		t.Errorf("the environment should win over the file, got %q", got)
	}
}
