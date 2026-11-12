package lastfm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient points a client at handler with retries and rate limiting
// made instant.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New("test-key")
	c.baseURL = srv.URL + "/2.0/"
	c.backoff = func(int) time.Duration { return 0 }
	c.gap = 0
	return c
}

func TestTopTracks(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("method") != "user.getTopTracks" || q.Get("user") != "alice" || q.Get("period") != "7day" ||
			q.Get("limit") != "5" || q.Get("api_key") != "test-key" || q.Get("format") != "json" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"toptracks":{"track":[
			{"name":"Song A","playcount":"12","url":"u1","artist":{"name":"Artist A"},"@attr":{"rank":"1"}},
			{"name":"Song B","playcount":7,"url":"u2","artist":{"name":"Artist B"},"@attr":{"rank":2}}
		],"@attr":{"user":"alice","total":"2"}}}`)
	})

	tracks, err := c.TopTracks(context.Background(), "alice", PeriodWeek, 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []Track{
		{Rank: 1, Name: "Song A", Artist: "Artist A", PlayCount: 12, URL: "u1"},
		{Rank: 2, Name: "Song B", Artist: "Artist B", PlayCount: 7, URL: "u2"},
	}
	if fmt.Sprint(tracks) != fmt.Sprint(want) {
		t.Fatalf("got %+v\nwant %+v", tracks, want)
	}
}

func TestTopTracksSingleObjectAndEmpty(t *testing.T) {
	tests := map[string]struct {
		body string
		want int
	}{
		"single object":  {`{"toptracks":{"track":{"name":"Only","playcount":"3","artist":{"name":"X"},"@attr":{"rank":"1"}}}}`, 1},
		"empty array":    {`{"toptracks":{"track":[],"@attr":{"total":"0"}}}`, 0},
		"missing tracks": {`{"toptracks":{"@attr":{"total":"0"}}}`, 0},
		"null tracks":    {`{"toptracks":{"track":null}}`, 0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
			tracks, err := c.TopTracks(context.Background(), "u", PeriodWeek, 5)
			if err != nil {
				t.Fatal(err)
			}
			if len(tracks) != tc.want {
				t.Fatalf("got %d tracks, want %d", len(tracks), tc.want)
			}
		})
	}
}

func TestRankDefaultsToPosition(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"topartists":{"artist":[{"name":"A","playcount":"9"},{"name":"B","playcount":""}]}}`)
	})
	artists, err := c.TopArtists(context.Background(), "u", PeriodWeek, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) != 2 || artists[0].Rank != 1 || artists[1].Rank != 2 || artists[0].PlayCount != 9 || artists[1].PlayCount != 0 {
		t.Fatalf("unexpected artists: %+v", artists)
	}
}

func TestUserNotFound(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":6,"message":"User not found","links":[]}`)
	})
	_, err := c.UserInfo(context.Background(), "nobody")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("got %v, want ErrUserNotFound", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 6 {
		t.Fatalf("error should carry the API error, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("a missing user must not be retried, got %d calls", calls.Load())
	}
}

func TestUserInfo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"user":{"name":"Alice","playcount":"1234","url":"https://www.last.fm/user/Alice"}}`)
	})
	info, err := c.UserInfo(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "Alice" || info.PlayCount != 1234 {
		t.Fatalf("unexpected info %+v", info)
	}
}

func TestRetriesTransientFailures(t *testing.T) {
	failures := []func(w http.ResponseWriter){
		func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) },
		func(w http.ResponseWriter) { fmt.Fprint(w, `{"error":29,"message":"Rate limit exceeded"}`) },
		func(w http.ResponseWriter) { fmt.Fprint(w, `<html>oops</html>`) },
	}
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		if n <= len(failures) {
			failures[n-1](w)
			return
		}
		fmt.Fprint(w, `{"user":{"name":"Alice"}}`)
	})
	info, err := c.UserInfo(context.Background(), "alice")
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if info.Name != "Alice" || calls.Load() != 4 {
		t.Fatalf("got %+v after %d calls", info, calls.Load())
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	if _, err := c.UserInfo(context.Background(), "alice"); err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() != 4 {
		t.Fatalf("got %d calls, want 4", calls.Load())
	}
}

func TestDoesNotRetryPermanentErrors(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":10,"message":"Invalid API key"}`)
	})
	_, err := c.TopTracks(context.Background(), "alice", PeriodWeek, 5)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 10 {
		t.Fatalf("got %v, want API error 10", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("got %d calls, want 1", calls.Load())
	}
}

func TestScrobblesPaginatesAndSkipsNowPlaying(t *testing.T) {
	from := time.Unix(1_700_000_000, 0)
	to := from.Add(7 * 24 * time.Hour)
	var pages []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("from") != strconv.FormatInt(from.Unix(), 10) || q.Get("to") != strconv.FormatInt(to.Unix(), 10) {
			t.Errorf("bad window: %s", r.URL.RawQuery)
		}
		if q.Get("limit") != "200" {
			t.Errorf("limit = %s, want 200 (the API maximum)", q.Get("limit"))
		}
		pages = append(pages, q.Get("page"))
		switch q.Get("page") {
		case "1":
			fmt.Fprint(w, `{"recenttracks":{"track":[
				{"name":"playing","@attr":{"nowplaying":"true"}},
				{"name":"a","date":{"uts":"1700000100","#text":"x"}},
				{"name":"b","date":{"uts":"1700000200","#text":"x"}}
			],"@attr":{"page":"1","totalPages":"2","total":"3"}}}`)
		case "2":
			fmt.Fprint(w, `{"recenttracks":{"track":{"name":"c","date":{"uts":"1700000300"}},"@attr":{"page":"2","totalPages":"2","total":"3"}}}`)
		default:
			t.Errorf("unexpected page %s", q.Get("page"))
		}
	})
	got, err := c.Scrobbles(context.Background(), "alice", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Unix() != 1700000100 || got[2].Unix() != 1700000300 {
		t.Fatalf("unexpected scrobbles %v", got)
	}
	if fmt.Sprint(pages) != "[1 2]" {
		t.Fatalf("fetched pages %v", pages)
	}
}

func TestScrobbleCount(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "1" {
			t.Errorf("count should only fetch one track")
		}
		fmt.Fprint(w, `{"recenttracks":{"track":[],"@attr":{"total":"321","totalPages":"321"}}}`)
	})
	n, err := c.ScrobbleCount(context.Background(), "alice", time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 321 {
		t.Fatalf("got %d, want 321", n)
	}
}

func TestContextCancellationStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		cancel()
		w.WriteHeader(http.StatusInternalServerError)
	})
	c.backoff = func(int) time.Duration { return time.Hour }
	done := make(chan error, 1)
	go func() { _, err := c.UserInfo(ctx, "alice"); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("call did not return after the context was cancelled")
	}
	if calls.Load() != 1 {
		t.Fatalf("got %d calls, want 1", calls.Load())
	}
}

func TestRequestTimeout(t *testing.T) {
	block := make(chan struct{})
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	})
	defer close(block)
	c.http.Timeout = 50 * time.Millisecond
	start := time.Now()
	if _, err := c.UserInfo(context.Background(), "alice"); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("hung request was not cut off")
	}
}

func TestRateLimitSpacesRequests(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"user":{}}`) })
	c.gap = 40 * time.Millisecond
	start := time.Now()
	for range 4 {
		if _, err := c.UserInfo(context.Background(), "a"); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 3*c.gap {
		t.Fatalf("4 requests took %s, expected at least %s", elapsed, 3*c.gap)
	}
}

func TestGarbageNumbersAreAnError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"toptracks":{"track":[{"name":"S","playcount":"12abc","artist":{"name":"A"}}]}}`)
	})
	if tracks, err := c.TopTracks(context.Background(), "u", PeriodWeek, 5); err == nil {
		t.Fatalf("expected an error, got %+v", tracks)
	}
}
