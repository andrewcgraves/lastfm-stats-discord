package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andrewcgraves/lastfm-stats-discord/internal/digest"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/lastfm"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/store"
	"github.com/bwmarrin/discordgo"
)

func init() { retryDelayScale = 0 }

// fakeDiscord stands in for the Discord REST API. It records every request and
// answers through respond, which defaults to 200 with a minimal message body.
type fakeDiscord struct {
	mu       sync.Mutex
	requests []recorded
	respond  func(r recorded) (int, string)
}

type recorded struct {
	Method string
	Path   string
	Query  string
	Body   string
}

func (f *fakeDiscord) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	r := recorded{Method: req.Method, Path: strings.TrimPrefix(req.URL.Path, "/api/v"+discordgo.APIVersion), Query: req.URL.RawQuery, Body: string(body)}
	f.mu.Lock()
	f.requests = append(f.requests, r)
	respond := f.respond
	f.mu.Unlock()

	status, out := http.StatusOK, `{"id":"1"}`
	if respond != nil {
		status, out = respond(r)
	}
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(status)
	rec.WriteString(out)
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}

func (f *fakeDiscord) all() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.requests...)
}

type fakeStore struct {
	mu     sync.Mutex
	linked []store.User
	err    error
}

func (f *fakeStore) LinkUser(_ context.Context, u store.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.linked = append(f.linked, u)
	return nil
}

func (f *fakeStore) Users(context.Context) ([]store.User, error) { return nil, nil }

type fakeLookup struct {
	info *lastfm.UserInfo
	err  error
}

func (f fakeLookup) UserInfo(context.Context, string) (*lastfm.UserInfo, error) { return f.info, f.err }

type fakeDigest struct {
	mu    sync.Mutex
	calls int
	fn    func(call int) (*digest.Digest, error)
}

func (f *fakeDigest) Build(context.Context) (*digest.Digest, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	return f.fn(n)
}

func digestWith(n int) *digest.Digest {
	d := &digest.Digest{}
	for range n {
		d.Embeds = append(d.Embeds, &discordgo.MessageEmbed{Title: "user", Description: "stats"})
	}
	return d
}

type harness struct {
	bot     *Bot
	discord *fakeDiscord
	store   *fakeStore
	digest  *fakeDigest
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		discord: &fakeDiscord{},
		store:   &fakeStore{},
		digest:  &fakeDigest{fn: func(int) (*digest.Digest, error) { return digestWith(1), nil }},
	}
	b, err := New(Options{
		Token:     "test-token",
		ChannelID: "chan-1",
		Digest:    h.digest,
		Store:     h.store,
		LastFM:    fakeLookup{info: &lastfm.UserInfo{Name: "Alice"}},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	b.s.Client = &http.Client{Transport: h.discord}
	t.Cleanup(b.cancel)
	h.bot = b
	return h
}

func command(name string, opts ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID:      "int-1",
		AppID:   "app-1",
		Token:   "itok",
		Type:    discordgo.InteractionApplicationCommand,
		GuildID: "guild-1",
		Member:  &discordgo.Member{User: &discordgo.User{ID: "555"}},
		Data:    discordgo.ApplicationCommandInteractionData{Name: name, Options: opts},
	}}
}

func usernameOpt(v string) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name: "lastfm-username", Type: discordgo.ApplicationCommandOptionString, Value: v,
	}
}

const (
	callbackPath = "/interactions/int-1/itok/callback"
	originalPath = "/webhooks/app-1/itok/messages/@original"
	followupPath = "/webhooks/app-1/itok"
)

// decode parses a JSON request body (or the payload_json part of a multipart one).
func decode(t *testing.T, r recorded) map[string]any {
	t.Helper()
	body := r.Body
	if i := strings.Index(body, `name="payload_json"`); i >= 0 {
		body = body[i:]
		body = body[strings.Index(body, "{"):]
		body = body[:strings.LastIndex(body, "}")+1]
		if j := strings.Index(body, "\r\n--"); j >= 0 {
			body = body[:j]
		}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("decoding %s %s body %q: %v", r.Method, r.Path, r.Body, err)
	}
	return m
}

func content(t *testing.T, r recorded) string {
	t.Helper()
	m := decode(t, r)
	if d, ok := m["data"].(map[string]any); ok {
		m = d
	}
	s, _ := m["content"].(string)
	return s
}

func embedCount(t *testing.T, r recorded) int {
	t.Helper()
	m := decode(t, r)
	if d, ok := m["data"].(map[string]any); ok {
		m = d
	}
	e, _ := m["embeds"].([]any)
	return len(e)
}

func responseType(t *testing.T, r recorded) int {
	t.Helper()
	return int(decode(t, r)["type"].(float64))
}

func TestPing(t *testing.T) {
	h := newHarness(t)
	h.bot.onInteraction(h.bot.s, command("ping"))

	reqs := h.discord.all()
	if len(reqs) != 1 || reqs[0].Path != callbackPath {
		t.Fatalf("requests = %+v", reqs)
	}
	if responseType(t, reqs[0]) != int(discordgo.InteractionResponseChannelMessageWithSource) {
		t.Errorf("ping should reply immediately")
	}
	if !strings.HasPrefix(content(t, reqs[0]), "Pong") {
		t.Errorf("content = %q", content(t, reqs[0]))
	}
}

func TestLinkLastFM(t *testing.T) {
	h := newHarness(t)
	h.bot.onInteraction(h.bot.s, command("link-lastfm", usernameOpt("  alice ")))

	reqs := h.discord.all()
	if len(reqs) != 2 {
		t.Fatalf("requests = %+v", reqs)
	}
	if reqs[0].Path != callbackPath || responseType(t, reqs[0]) != int(discordgo.InteractionResponseDeferredChannelMessageWithSource) {
		t.Errorf("first request should defer the response: %+v", reqs[0])
	}
	if reqs[1].Method != http.MethodPatch || reqs[1].Path != originalPath {
		t.Errorf("second request should edit the response: %+v", reqs[1])
	}
	if got := content(t, reqs[1]); got != "(<@555>) :link: (Alice)" {
		t.Errorf("content = %q", got)
	}
	if len(h.store.linked) != 1 || h.store.linked[0] != (store.User{DiscordID: 555, LastFMName: "Alice"}) {
		t.Errorf("stored = %+v (should use Last.fm's canonical name)", h.store.linked)
	}
}

func TestLinkLastFMFromDM(t *testing.T) {
	h := newHarness(t)
	ic := command("link-lastfm", usernameOpt("alice"))
	ic.Member = nil // DMs carry User instead of Member; the old code crashed here
	ic.User = &discordgo.User{ID: "777"}
	h.bot.onInteraction(h.bot.s, ic)
	if len(h.store.linked) != 1 || h.store.linked[0].DiscordID != 777 {
		t.Fatalf("stored = %+v", h.store.linked)
	}
}

func TestLinkLastFMFailures(t *testing.T) {
	tests := map[string]struct {
		lookup   fakeLookup
		storeErr error
		want     string
	}{
		"unknown user":  {lookup: fakeLookup{err: errors.Join(lastfm.ErrUserNotFound, errors.New("6"))}, want: "couldn't find a Last.fm user called `alice`"},
		"last.fm down":  {lookup: fakeLookup{err: errors.New("timeout")}, want: "Last.fm isn't responding"},
		"database down": {lookup: fakeLookup{info: &lastfm.UserInfo{}}, storeErr: errors.New("dynamo"), want: "unexpected error"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.bot.lastfm = tc.lookup
			h.store.err = tc.storeErr
			h.bot.onInteraction(h.bot.s, command("link-lastfm", usernameOpt("alice")))
			reqs := h.discord.all()
			if len(reqs) != 2 || !strings.Contains(content(t, reqs[1]), tc.want) {
				t.Fatalf("want reply containing %q, got %+v", tc.want, reqs)
			}
			if len(h.store.linked) != 0 {
				t.Fatalf("nothing should be stored, got %+v", h.store.linked)
			}
		})
	}
}

func TestManualTriggerSplitsLargeDigests(t *testing.T) {
	h := newHarness(t)
	h.digest.fn = func(int) (*digest.Digest, error) { return digestWith(12), nil }
	h.bot.onInteraction(h.bot.s, command("manual-trigger"))

	reqs := h.discord.all()
	if len(reqs) != 3 {
		t.Fatalf("requests = %+v", reqs)
	}
	if responseType(t, reqs[0]) != int(discordgo.InteractionResponseDeferredChannelMessageWithSource) {
		t.Error("manual trigger must defer within Discord's 3 second window")
	}
	if reqs[1].Path != originalPath || embedCount(t, reqs[1]) != 10 {
		t.Errorf("first page should carry 10 embeds: %+v", reqs[1])
	}
	if reqs[2].Method != http.MethodPost || reqs[2].Path != followupPath || embedCount(t, reqs[2]) != 2 {
		t.Errorf("follow-up should carry the remaining 2 embeds: %+v", reqs[2])
	}
}

func TestManualTriggerBuildFailure(t *testing.T) {
	h := newHarness(t)
	h.digest.fn = func(int) (*digest.Digest, error) { return nil, errors.New("dynamo down") }
	h.bot.onInteraction(h.bot.s, command("manual-trigger"))
	reqs := h.discord.all()
	if len(reqs) != 2 || !strings.Contains(content(t, reqs[1]), "couldn't build the digest") {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestManualTriggerWhileBusy(t *testing.T) {
	h := newHarness(t)
	building, release := make(chan struct{}), make(chan struct{})
	h.digest.fn = func(int) (*digest.Digest, error) {
		close(building)
		<-release
		return digestWith(1), nil
	}
	first := make(chan struct{})
	go func() {
		h.bot.onInteraction(h.bot.s, command("manual-trigger"))
		close(first)
	}()
	<-building

	second := command("manual-trigger")
	second.ID = "int-2"
	h.bot.onInteraction(h.bot.s, second)
	close(release)
	<-first

	var busy *recorded
	for _, r := range h.discord.all() {
		if r.Path == "/interactions/int-2/itok/callback" {
			busy = &r
		}
	}
	if busy == nil || !strings.Contains(content(t, *busy), "already being generated") {
		t.Fatalf("second trigger should get a busy notice, requests = %+v", h.discord.all())
	}
	data := decode(t, *busy)["data"].(map[string]any)
	if int(data["flags"].(float64)) != int(discordgo.MessageFlagsEphemeral) {
		t.Error("busy notice should be ephemeral")
	}
	if h.digest.calls != 1 {
		t.Errorf("digest built %d times, want 1", h.digest.calls)
	}
}

func TestManualTriggerWorksAfterPanic(t *testing.T) {
	h := newHarness(t)
	h.digest.fn = func(call int) (*digest.Digest, error) {
		if call == 1 {
			panic("kaboom")
		}
		return digestWith(1), nil
	}
	h.bot.onInteraction(h.bot.s, command("manual-trigger")) // must not crash the test binary

	reqs := h.discord.all()
	if last := reqs[len(reqs)-1]; !strings.Contains(content(t, last), "Something went wrong") {
		t.Fatalf("expected an error reply, got %+v", reqs)
	}

	before := len(reqs)
	h.bot.onInteraction(h.bot.s, command("manual-trigger"))
	reqs = h.discord.all()[before:]
	if len(reqs) != 2 || reqs[1].Path != originalPath || embedCount(t, reqs[1]) != 1 {
		t.Fatalf("a later trigger should deliver the digest, got %+v", reqs)
	}
}

func TestIgnoresOtherInteractionTypes(t *testing.T) {
	h := newHarness(t)
	ic := command("ping")
	ic.Type = discordgo.InteractionMessageComponent
	ic.Data = discordgo.MessageComponentInteractionData{CustomID: "x"}
	h.bot.onInteraction(h.bot.s, ic) // ApplicationCommandData() would panic on this type
	ic = command("does-not-exist")
	h.bot.onInteraction(h.bot.s, ic)
	if n := len(h.discord.all()); n != 0 {
		t.Fatalf("expected no requests, got %d", n)
	}
}

func TestPostDigestRetriesServerErrors(t *testing.T) {
	h := newHarness(t)
	var calls int
	h.discord.respond = func(r recorded) (int, string) {
		calls++
		if calls <= 2 {
			return http.StatusInternalServerError, `{"message":"oops"}`
		}
		return http.StatusOK, `{"id":"1"}`
	}
	d := digestWith(1)
	d.Embeds = append(d.Embeds, &discordgo.MessageEmbed{Image: &discordgo.MessageEmbedImage{URL: "attachment://g.png"}})
	d.Files = []digest.File{{Name: "g.png", ContentType: "image/png", Data: []byte("PNGDATA")}}

	if err := h.bot.PostDigest(context.Background(), "chan-1", d); err != nil {
		t.Fatal(err)
	}
	reqs := h.discord.all()
	if len(reqs) != 3 {
		t.Fatalf("got %d requests, want 3", len(reqs))
	}
	for _, r := range reqs {
		if r.Path != "/channels/chan-1/messages" {
			t.Fatalf("unexpected path %s", r.Path)
		}
		// Every retry must carry the full attachment, not an exhausted reader.
		if !strings.Contains(r.Body, "PNGDATA") {
			t.Fatal("attachment missing from (re)tried request")
		}
	}
}

func TestPostDigestDoesNotRetryClientErrors(t *testing.T) {
	h := newHarness(t)
	h.discord.respond = func(recorded) (int, string) {
		return http.StatusForbidden, `{"message":"Missing Permissions","code":50013}`
	}
	if err := h.bot.PostDigest(context.Background(), "chan-1", digestWith(1)); err == nil {
		t.Fatal("expected an error")
	}
	if n := len(h.discord.all()); n != 1 {
		t.Fatalf("got %d requests, want 1", n)
	}
}

func TestScheduledDigest(t *testing.T) {
	h := newHarness(t)
	h.digest.fn = func(call int) (*digest.Digest, error) {
		if call == 1 {
			return nil, errors.New("transient") // first attempt fails, retry succeeds
		}
		return digestWith(11), nil
	}
	h.bot.RunScheduledDigest(context.Background())

	reqs := h.discord.all()
	if len(reqs) != 2 || reqs[0].Path != "/channels/chan-1/messages" || embedCount(t, reqs[0]) != 10 || embedCount(t, reqs[1]) != 1 {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestScheduledDigestReportsFailure(t *testing.T) {
	h := newHarness(t)
	h.digest.fn = func(int) (*digest.Digest, error) { return nil, errors.New("down") }
	h.bot.RunScheduledDigest(context.Background())

	if h.digest.calls != 3 {
		t.Errorf("expected 3 build attempts, got %d", h.digest.calls)
	}
	reqs := h.discord.all()
	if len(reqs) != 1 || !strings.Contains(content(t, reqs[0]), "couldn't build this week's digest") {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestStartRejectsBadToken(t *testing.T) {
	h := newHarness(t)
	h.discord.respond = func(recorded) (int, string) { return http.StatusUnauthorized, `{"message":"401: Unauthorized"}` }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.bot.Start(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("got %v, want ErrUnauthorized", err)
	}
	if n := len(h.discord.all()); n != 1 {
		t.Fatalf("a bad token should not be retried, got %d requests", n)
	}
}

func TestStartRetriesUntilCancelled(t *testing.T) {
	h := newHarness(t)
	h.discord.respond = func(recorded) (int, string) { return http.StatusServiceUnavailable, `{}` }
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := h.bot.Start(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want the context error", err)
	}
	if n := len(h.discord.all()); n < 2 {
		t.Fatalf("expected repeated attempts during an outage, got %d", n)
	}
}

func TestRegisterCommandsOncePerGuild(t *testing.T) {
	h := newHarness(t)
	h.discord.respond = func(recorded) (int, string) { return http.StatusOK, `[]` }
	h.bot.s.State.User = &discordgo.User{ID: "app-1"}
	h.bot.registerCommands(h.bot.s, "guild-1", "Guild")
	h.bot.registerCommands(h.bot.s, "guild-1", "Guild") // e.g. after a reconnect

	reqs := h.discord.all()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	if reqs[0].Method != http.MethodPut || reqs[0].Path != "/applications/app-1/guilds/guild-1/commands" {
		t.Fatalf("unexpected request %+v", reqs[0])
	}
	var cmds []struct{ Name string }
	if err := json.Unmarshal([]byte(reqs[0].Body), &cmds); err != nil {
		t.Fatalf("body = %s (%v)", reqs[0].Body, err)
	}
	var names []string
	for _, c := range cmds {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ","); got != "ping,link-lastfm,manual-trigger" {
		t.Fatalf("registered commands = %s", got)
	}
}

func TestHealth(t *testing.T) {
	h := newHarness(t)
	srv := httptest.NewServer(h.bot.HealthHandler())
	defer srv.Close()

	get := func() (int, Status) {
		resp, err := http.Get(srv.URL + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var st Status
		b, _ := io.ReadAll(resp.Body)
		if err := json.NewDecoder(bytes.NewReader(b)).Decode(&st); err != nil {
			t.Fatalf("decoding %q: %v", b, err)
		}
		return resp.StatusCode, st
	}

	if code, st := get(); code != http.StatusServiceUnavailable || st.Connected {
		t.Errorf("disconnected session: got %d %+v", code, st)
	}

	h.bot.s.Lock()
	h.bot.s.DataReady = true
	h.bot.s.LastHeartbeatAck = time.Now().UTC()
	h.bot.s.Unlock()
	if code, st := get(); code != http.StatusOK || !st.Connected {
		t.Errorf("healthy session: got %d %+v", code, st)
	}

	h.bot.s.Lock()
	h.bot.s.LastHeartbeatAck = time.Now().Add(-10 * time.Minute)
	h.bot.s.Unlock()
	if code, st := get(); code != http.StatusServiceUnavailable || !strings.Contains(st.Reason, "heartbeat") {
		t.Errorf("stale heartbeat: got %d %+v", code, st)
	}
}

func TestHealthDoesNotHangOnLockedSession(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the 10s lock timeout")
	}
	h := newHarness(t)
	h.bot.s.Lock() // simulate discordgo stuck inside Open()
	defer h.bot.s.Unlock()
	start := time.Now()
	st := h.bot.Healthy()
	if st.Connected || !strings.Contains(st.Reason, "locked") {
		t.Fatalf("got %+v", st)
	}
	if time.Since(start) > 15*time.Second {
		t.Fatal("health check blocked")
	}
}
