// Package artwork finds artist images. Last.fm stopped serving artist images,
// so they are looked up on Spotify instead.
package artwork

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// Finder returns an image URL for an artist, or "" when none is known.
type Finder interface {
	ArtistImage(ctx context.Context, artist string) (string, error)
}

// None is a Finder that never finds anything. It is used when Spotify
// credentials are not configured.
type None struct{}

// ArtistImage implements Finder.
func (None) ArtistImage(context.Context, string) (string, error) { return "", nil }

// Spotify looks artists up through the Spotify Web API.
type Spotify struct {
	client *spotify.Client

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	url     string
	expires time.Time
}

const cacheTTL = 24 * time.Hour

// NewSpotify builds a Spotify finder using the client-credentials flow. The
// returned client refreshes its access token automatically whenever it
// expires, so it never needs to be rebuilt.
func NewSpotify(clientID, clientSecret string) *Spotify {
	cfg := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     spotifyauth.TokenURL,
	}
	// The token endpoint gets its own timeout via this context's HTTP client.
	tokenCtx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 15 * time.Second})
	httpClient := cfg.Client(tokenCtx)
	httpClient.Timeout = 20 * time.Second

	return newSpotify(httpClient, spotify.WithRetry(true))
}

func newSpotify(httpClient *http.Client, opts ...spotify.ClientOption) *Spotify {
	return &Spotify{client: spotify.New(httpClient, opts...), cache: map[string]cached{}}
}

// ArtistImage implements Finder. It prefers an exact (case-insensitive) name
// match and otherwise falls back to the first result that has an image.
func (s *Spotify) ArtistImage(ctx context.Context, artist string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(artist))
	if key == "" {
		return "", nil
	}

	s.mu.Lock()
	if c, ok := s.cache[key]; ok && time.Now().Before(c.expires) {
		s.mu.Unlock()
		return c.url, nil
	}
	s.mu.Unlock()

	res, err := s.client.Search(ctx, artist, spotify.SearchTypeArtist, spotify.Limit(10))
	if err != nil {
		return "", err
	}

	var url string
	if res != nil && res.Artists != nil {
		url = pickImage(key, res.Artists.Artists)
	}

	s.mu.Lock()
	s.cache[key] = cached{url: url, expires: time.Now().Add(cacheTTL)}
	s.mu.Unlock()
	return url, nil
}

func pickImage(name string, artists []spotify.FullArtist) string {
	var fallback string
	for _, a := range artists {
		if len(a.Images) == 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(a.Name), name) {
			return a.Images[0].URL
		}
		if fallback == "" {
			fallback = a.Images[0].URL
		}
	}
	return fallback
}
