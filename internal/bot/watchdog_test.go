package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestWatchdogState(t *testing.T) {
	t0 := time.Date(2026, 9, 15, 14, 15, 0, 0, time.UTC)
	at := func(d time.Duration) time.Time { return t0.Add(d) }

	type step struct {
		at      time.Duration
		healthy bool
		want    watchdogAction
	}
	tests := map[string]struct {
		reconnect, exit time.Duration
		steps           []step
	}{
		"healthy does nothing": {
			reconnect: 5 * time.Minute, exit: 30 * time.Minute,
			steps: []step{{0, true, actionNone}, {time.Hour, true, actionNone}},
		},
		"brief blip is left to discordgo": {
			reconnect: 5 * time.Minute, exit: 30 * time.Minute,
			steps: []step{{0, false, actionNone}, {2 * time.Minute, false, actionNone}, {3 * time.Minute, true, actionNone}},
		},
		"prolonged outage forces reconnects, spaced out, then exits": {
			reconnect: 5 * time.Minute, exit: 30 * time.Minute,
			steps: []step{
				{0, false, actionNone},
				{5 * time.Minute, false, actionReconnect},
				{6 * time.Minute, false, actionNone},
				{10 * time.Minute, false, actionReconnect},
				{29 * time.Minute, false, actionReconnect},
				{30 * time.Minute, false, actionExit},
			},
		},
		"recovery resets the clock": {
			reconnect: 5 * time.Minute, exit: 30 * time.Minute,
			steps: []step{
				{0, false, actionNone},
				{25 * time.Minute, false, actionReconnect},
				{26 * time.Minute, true, actionNone},
				{27 * time.Minute, false, actionNone},
				{50 * time.Minute, false, actionReconnect}, // 23m into the new outage: no exit yet
				{57 * time.Minute, false, actionExit},
			},
		},
		"exit disabled": {
			reconnect: 5 * time.Minute, exit: 0,
			steps: []step{{0, false, actionNone}, {48 * time.Hour, false, actionReconnect}},
		},
		"everything disabled": {
			steps: []step{{0, false, actionNone}, {48 * time.Hour, false, actionNone}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			w := &watchdogState{reconnectAfter: tc.reconnect, exitAfter: tc.exit}
			for i, s := range tc.steps {
				if got := w.observe(at(s.at), s.healthy); got != s.want {
					t.Fatalf("step %d (t+%s healthy=%v): got %v, want %v", i, s.at, s.healthy, got, s.want)
				}
			}
		})
	}
}

func TestWatchdogCallsFatal(t *testing.T) {
	h := newHarness(t) // session never connects, so it is always unhealthy
	fatal := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go h.bot.Watchdog(ctx, 5*time.Millisecond, 0, 20*time.Millisecond, func(err error) { fatal <- err })
	select {
	case err := <-fatal:
		if err == nil {
			t.Fatal("expected an error")
		}
	case <-ctx.Done():
		t.Fatal("watchdog never gave up on a dead connection")
	}
}

func TestWatchdogStopsWithContext(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.bot.Watchdog(ctx, time.Millisecond, 0, 0, func(error) { t.Error("fatal should not be called") })
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not stop")
	}
}

func TestRetry(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	t.Run("succeeds after failures", func(t *testing.T) {
		n := 0
		err := retry(ctx, log, "x", func() (bool, error) {
			n++
			if n < 3 {
				return true, errors.New("flaky")
			}
			return true, nil
		})
		if err != nil || n != 3 {
			t.Fatalf("err=%v after %d calls", err, n)
		}
	})

	t.Run("stops on permanent errors", func(t *testing.T) {
		n := 0
		perm := errors.New("permanent")
		err := retry(ctx, log, "x", func() (bool, error) { n++; return false, perm })
		if !errors.Is(err, perm) || n != 1 {
			t.Fatalf("err=%v after %d calls", err, n)
		}
	})

	t.Run("respects max attempts", func(t *testing.T) {
		n := 0
		err := retry(ctx, log, "x", func() (bool, error) { n++; return true, errors.New("down") }, maxAttempts(4))
		if err == nil || n != 4 {
			t.Fatalf("err=%v after %d calls", err, n)
		}
	})

	t.Run("stops when cancelled", func(t *testing.T) {
		old := retryDelayScale
		retryDelayScale = 1
		defer func() { retryDelayScale = old }()

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		err := retry(ctx, log, "x", func() (bool, error) { return true, errors.New("down") }, backoffFrom(time.Hour, time.Hour))
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
			t.Fatalf("err=%v after %s", err, time.Since(start))
		}
	})
}
