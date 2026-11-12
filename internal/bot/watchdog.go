package bot

import (
	"context"
	"fmt"
	"time"
)

type watchdogAction int

const (
	actionNone watchdogAction = iota
	actionReconnect
	actionExit
)

// watchdogState decides what to do about an unhealthy gateway connection.
// It is kept free of I/O so it can be unit tested.
type watchdogState struct {
	reconnectAfter time.Duration // 0 disables forced reconnects
	exitAfter      time.Duration // 0 disables exiting

	unhealthySince time.Time
	lastForced     time.Time
}

func (w *watchdogState) observe(now time.Time, healthy bool) watchdogAction {
	if healthy {
		w.unhealthySince = time.Time{}
		w.lastForced = time.Time{}
		return actionNone
	}
	if w.unhealthySince.IsZero() {
		w.unhealthySince = now
		return actionNone
	}
	down := now.Sub(w.unhealthySince)
	if w.exitAfter > 0 && down >= w.exitAfter {
		return actionExit
	}
	if w.reconnectAfter > 0 && down >= w.reconnectAfter &&
		(w.lastForced.IsZero() || now.Sub(w.lastForced) >= w.reconnectAfter) {
		w.lastForced = now
		return actionReconnect
	}
	return actionNone
}

// Watchdog checks the gateway connection every interval. discordgo already
// reconnects on its own; the watchdog covers the cases where that silently
// stalls. After reconnectAfter of continuous ill health it forces a fresh
// connection, and after exitAfter it calls fatal so the process can exit and
// be restarted by its supervisor. It returns when ctx is cancelled.
func (b *Bot) Watchdog(ctx context.Context, interval, reconnectAfter, exitAfter time.Duration, fatal func(error)) {
	w := &watchdogState{reconnectAfter: reconnectAfter, exitAfter: exitAfter}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			st := b.Healthy()
			wasHealthy := w.unhealthySince.IsZero()
			switch w.observe(now, st.Connected) {
			case actionReconnect:
				b.log.Warn("discord connection unhealthy; forcing reconnect",
					"down_for", now.Sub(w.unhealthySince).Round(time.Second), "reason", st.Reason)
				b.forceReconnect()
			case actionExit:
				fatal(fmt.Errorf("discord connection unhealthy for %s (%s)", now.Sub(w.unhealthySince).Round(time.Second), st.Reason))
				return
			default:
				if wasHealthy && !st.Connected {
					b.log.Warn("discord connection unhealthy", "reason", st.Reason)
				} else if !wasHealthy && st.Connected {
					b.log.Info("discord connection healthy again")
				}
			}
		}
	}
}
