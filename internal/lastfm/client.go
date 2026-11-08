// Package lastfm is a small, defensive client for the read-only parts of the
// Last.fm API that the bot uses. Every request has a timeout, is rate limited,
// and is retried when Last.fm reports a transient failure.
package lastfm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL = "https://ws.audioscrobbler.com/2.0/"
	// Last.fm asks clients to stay under ~5 requests per second.
	minRequestGap  = 250 * time.Millisecond
	maxAttempts    = 4
	recentPageSize = 200 // the maximum Last.fm allows per page
	maxRecentPages = 25
)

// Periods accepted by the user.getTop* methods.
const (
	PeriodWeek = "7day"
)

// ErrUserNotFound is returned when Last.fm does not know the requested user.
var ErrUserNotFound = errors.New("last.fm user not found")

// APIError is an error reported by the Last.fm API itself.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("last.fm error %d: %s", e.Code, e.Message)
}

// temporary reports whether Last.fm documents the error as transient.
// https://www.last.fm/api/errorcodes
func (e *APIError) temporary() bool {
	switch e.Code {
	case 8, 11, 16, 29: // operation failed, service offline, temporarily unavailable, rate limited
		return true
	}
	return false
}

// Client talks to the Last.fm API.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
	backoff func(attempt int) time.Duration
	gap     time.Duration

	mu   sync.Mutex
	next time.Time
}

// New returns a client authenticated with apiKey.
func New(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 20 * time.Second},
		backoff: func(attempt int) time.Duration { return time.Duration(1<<attempt) * time.Second },
		gap:     minRequestGap,
	}
}

// Track is one entry of a user's top tracks.
type Track struct {
	Rank      int
	Name      string
	Artist    string
	PlayCount int
	URL       string
}

// Artist is one entry of a user's top artists.
type Artist struct {
	Rank      int
	Name      string
	PlayCount int
	URL       string
}

// UserInfo is the subset of user.getInfo the bot uses.
type UserInfo struct {
	Name      string
	PlayCount int
	URL       string
}

// UserInfo returns profile information, or ErrUserNotFound.
func (c *Client) UserInfo(ctx context.Context, user string) (*UserInfo, error) {
	var resp struct {
		User struct {
			Name      string  `json:"name"`
			PlayCount flexInt `json:"playcount"`
			URL       string  `json:"url"`
		} `json:"user"`
	}
	if err := c.call(ctx, "user.getInfo", url.Values{"user": {user}}, &resp); err != nil {
		return nil, err
	}
	return &UserInfo{Name: resp.User.Name, PlayCount: int(resp.User.PlayCount), URL: resp.User.URL}, nil
}

// TopTracks returns the user's most played tracks over period.
func (c *Client) TopTracks(ctx context.Context, user, period string, limit int) ([]Track, error) {
	var resp struct {
		TopTracks struct {
			Track oneOrMany[struct {
				Name      string  `json:"name"`
				PlayCount flexInt `json:"playcount"`
				URL       string  `json:"url"`
				Artist    struct {
					Name string `json:"name"`
				} `json:"artist"`
				Attr struct {
					Rank flexInt `json:"rank"`
				} `json:"@attr"`
			}] `json:"track"`
		} `json:"toptracks"`
	}
	params := url.Values{"user": {user}, "period": {period}, "limit": {strconv.Itoa(limit)}}
	if err := c.call(ctx, "user.getTopTracks", params, &resp); err != nil {
		return nil, err
	}
	tracks := make([]Track, 0, len(resp.TopTracks.Track))
	for i, t := range resp.TopTracks.Track {
		rank := int(t.Attr.Rank)
		if rank == 0 {
			rank = i + 1
		}
		tracks = append(tracks, Track{Rank: rank, Name: t.Name, Artist: t.Artist.Name, PlayCount: int(t.PlayCount), URL: t.URL})
	}
	return tracks, nil
}

// TopArtists returns the user's most played artists over period.
func (c *Client) TopArtists(ctx context.Context, user, period string, limit int) ([]Artist, error) {
	var resp struct {
		TopArtists struct {
			Artist oneOrMany[struct {
				Name      string  `json:"name"`
				PlayCount flexInt `json:"playcount"`
				URL       string  `json:"url"`
				Attr      struct {
					Rank flexInt `json:"rank"`
				} `json:"@attr"`
			}] `json:"artist"`
		} `json:"topartists"`
	}
	params := url.Values{"user": {user}, "period": {period}, "limit": {strconv.Itoa(limit)}}
	if err := c.call(ctx, "user.getTopArtists", params, &resp); err != nil {
		return nil, err
	}
	artists := make([]Artist, 0, len(resp.TopArtists.Artist))
	for i, a := range resp.TopArtists.Artist {
		rank := int(a.Attr.Rank)
		if rank == 0 {
			rank = i + 1
		}
		artists = append(artists, Artist{Rank: rank, Name: a.Name, PlayCount: int(a.PlayCount), URL: a.URL})
	}
	return artists, nil
}

type recentTracksResponse struct {
	RecentTracks struct {
		Track oneOrMany[struct {
			Date *struct {
				UTS flexInt `json:"uts"`
			} `json:"date"`
		}] `json:"track"`
		Attr struct {
			Total      flexInt `json:"total"`
			TotalPages flexInt `json:"totalPages"`
		} `json:"@attr"`
	} `json:"recenttracks"`
}

// ScrobbleCount returns how many scrobbles the user recorded in [from, to].
func (c *Client) ScrobbleCount(ctx context.Context, user string, from, to time.Time) (int, error) {
	var resp recentTracksResponse
	if err := c.call(ctx, "user.getRecentTracks", recentParams(user, from, to, 1, 1), &resp); err != nil {
		return 0, err
	}
	return int(resp.RecentTracks.Attr.Total), nil
}

// Scrobbles returns the timestamp of every scrobble in [from, to]. The
// currently-playing track (which has no timestamp) is skipped.
func (c *Client) Scrobbles(ctx context.Context, user string, from, to time.Time) ([]time.Time, error) {
	var out []time.Time
	for page := 1; page <= maxRecentPages; page++ {
		var resp recentTracksResponse
		if err := c.call(ctx, "user.getRecentTracks", recentParams(user, from, to, page, recentPageSize), &resp); err != nil {
			return nil, err
		}
		for _, t := range resp.RecentTracks.Track {
			if t.Date != nil && t.Date.UTS > 0 {
				out = append(out, time.Unix(int64(t.Date.UTS), 0))
			}
		}
		if page >= int(resp.RecentTracks.Attr.TotalPages) {
			break
		}
	}
	return out, nil
}

func recentParams(user string, from, to time.Time, page, limit int) url.Values {
	return url.Values{
		"user":  {user},
		"from":  {strconv.FormatInt(from.Unix(), 10)},
		"to":    {strconv.FormatInt(to.Unix(), 10)},
		"page":  {strconv.Itoa(page)},
		"limit": {strconv.Itoa(limit)},
	}
}

// call performs one API method with retries and decodes the JSON body into out.
func (c *Client) call(ctx context.Context, method string, params url.Values, out any) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return errors.Join(ctx.Err(), lastErr)
			case <-time.After(c.backoff(attempt)):
			}
		}
		retry, err := c.do(ctx, method, params, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry || ctx.Err() != nil {
			break
		}
	}
	return fmt.Errorf("%s: %w", method, lastErr)
}

func (c *Client) do(ctx context.Context, method string, params url.Values, out any) (retry bool, err error) {
	if err := c.wait(ctx); err != nil {
		return false, err
	}

	q := url.Values{}
	for k, v := range params {
		q[k] = v
	}
	q.Set("method", method)
	q.Set("api_key", c.apiKey)
	q.Set("format", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"?"+q.Encode(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "lastfm-stats-discord/2 (+https://github.com/andrewcgraves/lastfm-stats-discord)")

	resp, err := c.http.Do(req)
	if err != nil {
		return true, err // network errors and timeouts are worth retrying
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return true, fmt.Errorf("reading response: %w", err)
	}

	// Last.fm reports API errors in the body, sometimes with a 200 status.
	var apiErr struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != 0 {
		e := &APIError{Code: apiErr.Error, Message: apiErr.Message}
		if e.Code == 6 && strings.Contains(strings.ToLower(e.Message), "user not found") {
			return false, errors.Join(ErrUserNotFound, e)
		}
		return e.temporary(), e
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return true, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	if err := json.Unmarshal(body, out); err != nil {
		// Last.fm occasionally serves truncated or HTML error pages.
		return true, fmt.Errorf("decoding response: %w", err)
	}
	return false, nil
}

// wait enforces a minimum gap between requests across all goroutines.
func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	start := c.next
	if start.Before(now) {
		start = now
	}
	c.next = start.Add(c.gap)
	c.mu.Unlock()

	if d := time.Until(start); d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
	return nil
}

// flexInt decodes Last.fm numbers, which arrive as either JSON strings or
// numbers (and occasionally as empty strings).
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(bytes.TrimSpace(b)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing %q as integer: %w", s, err)
	}
	*f = flexInt(n)
	return nil
}

// oneOrMany decodes a JSON array, but also accepts a single object, which the
// Last.fm API returns when a list has exactly one element.
type oneOrMany[T any] []T

func (o *oneOrMany[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*o = nil
		return nil
	}
	if b[0] == '[' {
		var many []T
		if err := json.Unmarshal(b, &many); err != nil {
			return err
		}
		*o = many
		return nil
	}
	var one T
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*o = []T{one}
	return nil
}
