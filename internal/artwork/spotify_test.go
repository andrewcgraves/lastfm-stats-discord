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

func TestPickImage(t *testing.T) {
	artists := []spotify.FullArtist{
		{SimpleArtist: spotify.SimpleArtist{Name: "No Image"}},
		{SimpleArtist: spotify.SimpleArtist{Name: "Tribute Band"}, Images: []spotify.Image{{URL: "tribute"}}},
		{SimpleArtist: spotify.SimpleArtist{Name: "The Band"}, Images: []spotify.Image{{URL: "real"}}},
	}
	if got := pickImage("the band", artists); got != "real" {
		t.Errorf("exact match: got %q", got)
	}
	if got := pickImage("someone else", artists); got != "tribute" {
		t.Errorf("fallback: got %q", got)
	}
	if got := pickImage("x", nil); got != "" {
		t.Errorf("no results: got %q", got)
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
