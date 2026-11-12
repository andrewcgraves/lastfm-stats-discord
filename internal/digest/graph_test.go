package digest

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/andrewcgraves/lastfm-stats-discord/internal/store"
)

func TestDailyCounts(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 30, 0, 0, time.UTC)
	scrobbles := []time.Time{
		now,                   // today (the old code indexed past the end here)
		now.Add(-time.Minute), // today
		time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC),   // first day of the window
		time.Date(2026, 9, 12, 23, 59, 0, 0, time.UTC), // just before the window
		time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC),   // middle
		now.Add(time.Hour * 24),                        // future: ignored
	}
	days, counts := DailyCounts(scrobbles, now, time.UTC)
	if len(days) != 7 || len(counts) != 7 {
		t.Fatalf("got %d days, %d counts", len(days), len(counts))
	}
	if !days[0].Equal(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)) || !days[6].Equal(time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("window = %v … %v", days[0], days[6])
	}
	want := []float64{1, 0, 0, 1, 0, 0, 2}
	for i := range want {
		if counts[i] != want[i] {
			t.Fatalf("counts = %v, want %v", counts, want)
		}
	}
}

func TestDailyCountsUsesLocation(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, la)
	// 03:00 UTC on the 19th is still the 18th in Los Angeles.
	ts := time.Date(2026, 9, 19, 3, 0, 0, 0, time.UTC)
	_, counts := DailyCounts([]time.Time{ts}, now, la)
	if counts[5] != 1 {
		t.Fatalf("counts = %v, want the scrobble on the 18th (index 5)", counts)
	}
}

func TestActivityGraph(t *testing.T) {
	lfm := &fakeLastFM{scrobbles: map[string][]time.Time{
		"alice": {fixedNow.Add(-time.Hour), fixedNow.Add(-26 * time.Hour), fixedNow.Add(-26 * time.Hour)},
		"bob":   {fixedNow.Add(-3 * 24 * time.Hour)},
	}}
	b := newBuilder(lfm, []store.User{{LastFMName: "alice"}, {LastFMName: "bob"}})
	b.Graph = true
	d, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 1 {
		t.Fatalf("got %d files", len(d.Files))
	}
	if !bytes.HasPrefix(d.Files[0].Data, []byte("\x89PNG")) {
		t.Fatal("graph is not a PNG")
	}
	last := d.Embeds[len(d.Embeds)-1]
	if last.Image == nil || last.Image.URL != "attachment://"+d.Files[0].Name {
		t.Fatalf("graph embed = %+v", last)
	}
	if msgs := d.Messages(); len(msgs[len(msgs)-1].Files) != 1 {
		t.Fatal("graph file should be attached to the message with its embed")
	}
}

func TestActivityGraphSkippedWhenNothingToDraw(t *testing.T) {
	b := newBuilder(&fakeLastFM{}, []store.User{{LastFMName: "quiet"}})
	b.Graph = true
	d, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 0 || len(d.Embeds) != 1 {
		t.Fatalf("expected no graph, got %d files and %d embeds", len(d.Files), len(d.Embeds))
	}
}
