package digest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andrewcgraves/lastfm-stats-discord/internal/lastfm"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/store"
	"github.com/bwmarrin/discordgo"
)

type fakeLastFM struct {
	mu        sync.Mutex
	tracks    map[string][]lastfm.Track
	artists   map[string][]lastfm.Artist
	counts    map[string]int
	scrobbles map[string][]time.Time
	errs      map[string]error // keyed by "method:user"
	panics    map[string]bool  // keyed by user
	calls     []string
}

func (f *fakeLastFM) record(method, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+":"+user)
	if f.panics[user] {
		panic("boom")
	}
	return f.errs[method+":"+user]
}

func (f *fakeLastFM) TopTracks(_ context.Context, user, period string, limit int) ([]lastfm.Track, error) {
	if period != lastfm.PeriodWeek || limit != topN {
		panic("unexpected period/limit")
	}
	if err := f.record("tracks", user); err != nil {
		return nil, err
	}
	return f.tracks[user], nil
}

func (f *fakeLastFM) TopArtists(_ context.Context, user, _ string, _ int) ([]lastfm.Artist, error) {
	if err := f.record("artists", user); err != nil {
		return nil, err
	}
	return f.artists[user], nil
}

func (f *fakeLastFM) ScrobbleCount(_ context.Context, user string, from, to time.Time) (int, error) {
	if to.Sub(from) != 7*24*time.Hour {
		panic("count window should be one week")
	}
	if err := f.record("count", user); err != nil {
		return 0, err
	}
	return f.counts[user], nil
}

func (f *fakeLastFM) Scrobbles(_ context.Context, user string, _, _ time.Time) ([]time.Time, error) {
	if err := f.record("scrobbles", user); err != nil {
		return nil, err
	}
	return f.scrobbles[user], nil
}

type fakeUsers struct {
	users []store.User
	err   error
}

func (f fakeUsers) Users(context.Context) ([]store.User, error) { return f.users, f.err }

type fakeArt struct {
	images map[string]string
	err    error
}

func (f fakeArt) ArtistImage(_ context.Context, a string) (string, error) { return f.images[a], f.err }

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var fixedNow = time.Date(2026, 9, 19, 12, 30, 0, 0, time.UTC) // a Saturday

func newBuilder(lfm *fakeLastFM, users []store.User) *Builder {
	return &Builder{
		LastFM:  lfm,
		Artwork: fakeArt{images: map[string]string{"Artist A": "https://img/a.jpg"}},
		Users:   fakeUsers{users: users},
		Log:     quietLog(),
		now:     func() time.Time { return fixedNow },
	}
}

func TestBuildUserEmbed(t *testing.T) {
	lfm := &fakeLastFM{
		tracks: map[string][]lastfm.Track{"alice": {
			{Rank: 1, Name: "Song_One", Artist: "Artist A", PlayCount: 10},
			{Rank: 2, Name: "*Two*", Artist: "Artist B", PlayCount: 4},
		}},
		artists: map[string][]lastfm.Artist{"alice": {{Rank: 1, Name: "Artist A", PlayCount: 14}}},
		counts:  map[string]int{"alice": 42},
	}
	d, err := newBuilder(lfm, []store.User{{DiscordID: 111, LastFMName: "alice"}}).Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Embeds) != 1 {
		t.Fatalf("got %d embeds", len(d.Embeds))
	}
	e := d.Embeds[0]
	if e.Title != "alice's plays: 42" {
		t.Errorf("title = %q (should be the real scrobble count)", e.Title)
	}
	if e.URL != "https://www.last.fm/user/alice" {
		t.Errorf("url = %q", e.URL)
	}
	for _, want := range []string{
		"**<@111>'s top listens.**",
		`1. Song\_One by *Artist A* (10)`,
		`2. \*Two\* by *Artist B* (4)`,
		"**Top Artists.**\n1. Artist A (14)",
	} {
		if !strings.Contains(e.Description, want) {
			t.Errorf("description missing %q:\n%s", want, e.Description)
		}
	}
	if strings.HasSuffix(e.Description, ",") {
		t.Error("description should not end with a stray comma")
	}
	if e.Thumbnail == nil || e.Thumbnail.URL != "https://img/a.jpg" {
		t.Errorf("thumbnail = %+v", e.Thumbnail)
	}
}

func TestNoListensThisWeek(t *testing.T) {
	lfm := &fakeLastFM{counts: map[string]int{"bob": 0}}
	d, err := newBuilder(lfm, []store.User{{DiscordID: 222, LastFMName: "bob"}}).Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e := d.Embeds[0]
	if !strings.Contains(e.Description, "<@222> listened to nothing this week") {
		t.Errorf("description = %q", e.Description)
	}
	if e.Thumbnail != nil {
		t.Errorf("no thumbnail expected, got %+v", e.Thumbnail)
	}
}

// One user's Last.fm failure (or a panic) must not break the digest for others.
func TestFailuresAreIsolatedPerUser(t *testing.T) {
	lfm := &fakeLastFM{
		tracks: map[string][]lastfm.Track{
			"good": {{Rank: 1, Name: "S", Artist: "Artist A", PlayCount: 1}},
		},
		errs:   map[string]error{"tracks:broken": errors.New("last.fm down")},
		panics: map[string]bool{"panicky": true},
	}
	users := []store.User{
		{DiscordID: 1, LastFMName: "broken"},
		{DiscordID: 2, LastFMName: "good"},
		{DiscordID: 3, LastFMName: "panicky"},
	}
	d, err := newBuilder(lfm, users).Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Embeds) != 3 {
		t.Fatalf("got %d embeds, want 3", len(d.Embeds))
	}
	if !strings.Contains(d.Embeds[0].Description, "Couldn't load <@1>'s stats") {
		t.Errorf("broken user embed: %q", d.Embeds[0].Description)
	}
	if !strings.Contains(d.Embeds[1].Description, "<@2>'s top listens") {
		t.Errorf("good user embed: %q", d.Embeds[1].Description)
	}
	if !strings.Contains(d.Embeds[2].Description, "Couldn't load <@3>'s stats") {
		t.Errorf("panicky user embed: %q", d.Embeds[2].Description)
	}
}

func TestDegradesGracefully(t *testing.T) {
	lfm := &fakeLastFM{
		// Tracks but no artists used to index out of range.
		tracks: map[string][]lastfm.Track{"carol": {{Rank: 1, Name: "S", Artist: "Artist A", PlayCount: 3}}},
		errs:   map[string]error{"count:carol": errors.New("timeout")},
	}
	b := newBuilder(lfm, []store.User{{DiscordID: 3, LastFMName: "carol"}})
	b.Artwork = fakeArt{err: errors.New("spotify down")}
	d, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e := d.Embeds[0]
	if e.Title != "carol's plays: ?" {
		t.Errorf("title = %q", e.Title)
	}
	if strings.Contains(e.Description, "Top Artists") {
		t.Errorf("no artist section expected: %q", e.Description)
	}
	if e.Thumbnail != nil {
		t.Errorf("thumbnail should be omitted when the lookup fails, got %+v", e.Thumbnail)
	}
}

func TestThumbnailFallsBackToTopTrackArtist(t *testing.T) {
	lfm := &fakeLastFM{tracks: map[string][]lastfm.Track{"d": {{Rank: 1, Name: "S", Artist: "Artist A"}}}}
	d, err := newBuilder(lfm, []store.User{{LastFMName: "d"}}).Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Embeds[0].Thumbnail == nil || d.Embeds[0].Thumbnail.URL != "https://img/a.jpg" {
		t.Errorf("thumbnail = %+v", d.Embeds[0].Thumbnail)
	}
}

func TestUsersAreSorted(t *testing.T) {
	lfm := &fakeLastFM{}
	users := []store.User{{LastFMName: "zed"}, {LastFMName: "Amy"}, {LastFMName: "bob"}}
	d, err := newBuilder(lfm, users).Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range d.Embeds {
		got = append(got, strings.SplitN(e.Title, "'", 2)[0])
	}
	if strings.Join(got, ",") != "Amy,bob,zed" {
		t.Errorf("order = %v", got)
	}
}

func TestNoUsers(t *testing.T) {
	d, err := newBuilder(&fakeLastFM{}, nil).Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Embeds) != 1 || !strings.Contains(d.Embeds[0].Description, "/link-lastfm") {
		t.Fatalf("unexpected digest %+v", d.Embeds)
	}
}

func TestUserListFailureIsAnError(t *testing.T) {
	b := newBuilder(&fakeLastFM{}, nil)
	b.Users = fakeUsers{err: errors.New("dynamo down")}
	if _, err := b.Build(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newBuilder(&fakeLastFM{}, []store.User{{LastFMName: "a"}}).Build(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestLongTextIsTruncated(t *testing.T) {
	long := strings.Repeat("x", 300)
	lfm := &fakeLastFM{}
	d, err := newBuilder(lfm, []store.User{{LastFMName: long}}).Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(d.Embeds[0].Title)); n > 256 {
		t.Fatalf("title has %d characters, Discord allows 256", n)
	}
}

func TestEscape(t *testing.T) {
	if got := Escape("*NSYNC_ `x` ~y~ |z| [a](b)"); got != "\\*NSYNC\\_ \\`x\\` \\~y\\~ \\|z\\| \\[a\\](b)" {
		t.Errorf("Escape = %q", got)
	}
	if Escape("plain text") != "plain text" {
		t.Error("plain text should be unchanged")
	}
}

func TestEmbedLength(t *testing.T) {
	e := &discordgo.MessageEmbed{
		Title:       "héllo", // multi-byte characters count once
		Description: "abc",
		Footer:      &discordgo.MessageEmbedFooter{Text: "f"},
		Fields:      []*discordgo.MessageEmbedField{{Name: "n", Value: "vv"}},
	}
	if got := embedLength(e); got != 12 {
		t.Fatalf("embedLength = %d, want 12", got)
	}
}
