package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeGateway speaks just enough of the Discord gateway protocol for a
// session to connect: Hello, then READY after Identify, then heartbeat ACKs.
// While accepting is false it rejects the websocket upgrade, the way Discord
// did during the September 15 outage ("bad handshake").
type fakeGateway struct {
	srv       *httptest.Server
	accepting atomic.Bool
	upgrades  atomic.Int32
}

func newFakeGateway(t *testing.T) *fakeGateway {
	t.Helper()
	g := &fakeGateway{}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGateway) url() string { return "ws" + strings.TrimPrefix(g.srv.URL, "http") }

func (g *fakeGateway) serve(w http.ResponseWriter, r *http.Request) {
	if !g.accepting.Load() {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	g.upgrades.Add(1)

	send := func(v any) error { return conn.WriteJSON(v) }
	if send(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 1000}}) != nil {
		return
	}
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var p struct {
			Op int `json:"op"`
		}
		_ = json.Unmarshal(msg, &p)
		switch p.Op {
		case 2: // Identify
			err = send(map[string]any{"op": 0, "s": 1, "t": "READY", "d": map[string]any{
				"v": 10, "session_id": "sess-1", "user": map[string]any{"id": "app-1", "username": "bot"}, "guilds": []any{},
			}})
		case 1: // Heartbeat
			err = send(map[string]any{"op": 11})
		}
		if err != nil {
			return
		}
	}
}

// The bot's reason for existing: when Discord's gateway rejects connections
// for a while and then comes back, the watchdog must get the bot online again
// without a restart.
func TestWatchdogRecoversAfterGatewayOutage(t *testing.T) {
	h := newHarness(t)
	gw := newFakeGateway(t)
	h.discord.respond = func(r recorded) (int, string) {
		if r.Path == "/gateway" {
			return http.StatusOK, `{"url":"` + gw.url() + `"}`
		}
		return http.StatusOK, `{"id":"1"}`
	}
	t.Cleanup(func() { h.bot.s.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.bot.Watchdog(ctx, 5*time.Millisecond, 20*time.Millisecond, 0, func(err error) {
		t.Errorf("watchdog gave up: %v", err)
	})

	// Outage: forced reconnects are attempted but cannot succeed.
	time.Sleep(100 * time.Millisecond)
	if st := h.bot.Healthy(); st.Connected {
		t.Fatalf("should be unhealthy while the gateway rejects connections: %+v", st)
	}

	// Discord recovers.
	gw.accepting.Store(true)
	deadline := time.Now().Add(5 * time.Second)
	for !h.bot.Healthy().Connected {
		if time.Now().After(deadline) {
			t.Fatalf("bot did not reconnect after the gateway recovered: %+v", h.bot.Healthy())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
