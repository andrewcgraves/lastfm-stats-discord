package artwork

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/zmb3/spotify/v2"
)

func TestArtistImagePrefersExactMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"artists":{"items":[
			{"name":"No Image","images":[]},
			{"name":"Tribute Band","images":[{"url":"tribute"}]},
			{"name":"The Band","images":[{"url":"real"}]}
		]}}`)
	}))
	defer srv.Close()
	s := newSpotify(srv.Client(), spotify.WithBaseURL(srv.URL+"/"))

	for name, want := range map[string]string{
		"the band":     "real",    // exact match, ignoring case
		"someone else": "tribute", // otherwise the first result with an image
	} {
		if got, err := s.ArtistImage(context.Background(), name); err != nil || got != want {
			t.Errorf("ArtistImage(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestArtistImageNoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"artists":{"items":[]}}`)
	}))
	defer srv.Close()
	s := newSpotify(srv.Client(), spotify.WithBaseURL(srv.URL+"/"))
	if got, err := s.ArtistImage(context.Background(), "x"); err != nil || got != "" {
		t.Fatalf("got %q, %v; want no image", got, err)
	}
}

func TestSpotifyArtistImageCaches(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/search" || r.URL.Query().Get("type") != "artist" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"artists":{"items":[{"name":"Radiohead","images":[{"url":"https://i.scdn.co/rh.jpg"}]}]}}`)
	}))
	defer srv.Close()

	s := newSpotify(srv.Client(), spotify.WithBaseURL(srv.URL+"/"))
	for range 3 {
		url, err := s.ArtistImage(context.Background(), "Radiohead")
		if err != nil {
			t.Fatal(err)
		}
		if url != "https://i.scdn.co/rh.jpg" {
			t.Fatalf("url = %q", url)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 API call thanks to caching, got %d", calls.Load())
	}
	if url, _ := s.ArtistImage(context.Background(), "  "); url != "" {
		t.Fatal("blank names should not be searched")
	}
}

func TestSpotifyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"status":500,"message":"down"}}`)
	}))
	defer srv.Close()
	s := newSpotify(srv.Client(), spotify.WithBaseURL(srv.URL+"/"))
	if _, err := s.ArtistImage(context.Background(), "x"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestNone(t *testing.T) {
	if url, err := (None{}).ArtistImage(context.Background(), "x"); url != "" || err != nil {
		t.Fatal("None should find nothing")
	}
}
