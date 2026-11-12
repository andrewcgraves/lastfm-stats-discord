package bot

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"
)

// retryDelayScale scales every retry wait; tests set it to 0.
var retryDelayScale = 1.0

type retryConfig struct {
	attempts int // 0 means unlimited
	initial  time.Duration
	max      time.Duration
}

type retryOption func(*retryConfig)

func maxAttempts(n int) retryOption { return func(c *retryConfig) { c.attempts = n } }

func backoffFrom(initial, max time.Duration) retryOption {
	return func(c *retryConfig) { c.initial, c.max = initial, max }
}

// retry calls fn until it succeeds, reports a non-retryable error, runs out of
// attempts, or ctx is cancelled. Waits grow exponentially with jitter.
func retry(ctx context.Context, log *slog.Logger, what string, fn func() (retryable bool, err error), opts ...retryOption) error {
	cfg := retryConfig{initial: 2 * time.Second, max: 5 * time.Minute}
	for _, o := range opts {
		o(&cfg)
	}

	wait := cfg.initial
	for attempt := 1; ; attempt++ {
		retryable, err := fn()
		if err == nil {
			if attempt > 1 {
				log.Info(what+" succeeded", "attempt", attempt)
			}
			return nil
		}
		if !retryable || (cfg.attempts > 0 && attempt >= cfg.attempts) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// ±20% jitter so restarts don't synchronise against Discord.
		d := wait + time.Duration((rand.Float64()*0.4-0.2)*float64(wait))
		d = time.Duration(float64(d) * retryDelayScale)
		log.Warn(what+" failed; retrying", "attempt", attempt, "retry_in", d.Round(time.Millisecond), "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
		wait *= 2
		if wait > cfg.max {
			wait = cfg.max
		}
	}
}
