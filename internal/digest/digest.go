// Package digest builds the weekly listening digest that the bot posts.
package digest

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/andrewcgraves/lastfm-stats-discord/internal/artwork"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/lastfm"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/store"
	"github.com/bwmarrin/discordgo"
)

const (
	topN           = 5
	perUserTimeout = 90 * time.Second
	embedColor     = 0xD51007 // Last.fm red
)

// LastFM is the subset of the Last.fm client the digest needs.
type LastFM interface {
	TopTracks(ctx context.Context, user, period string, limit int) ([]lastfm.Track, error)
	TopArtists(ctx context.Context, user, period string, limit int) ([]lastfm.Artist, error)
	ScrobbleCount(ctx context.Context, user string, from, to time.Time) (int, error)
	Scrobbles(ctx context.Context, user string, from, to time.Time) ([]time.Time, error)
}

// Users lists the linked accounts.
type Users interface {
	Users(ctx context.Context) ([]store.User, error)
}

// Builder assembles digests. All fields except Graph and Location are required.
type Builder struct {
	LastFM  LastFM
	Artwork artwork.Finder
	Users   Users
	Log     *slog.Logger

	// Graph adds a chart of daily scrobbles per user.
	Graph bool
	// Location decides where day boundaries fall in the graph. Defaults to UTC.
	Location *time.Location

	now func() time.Time
}

// File is an attachment. Its bytes are kept so a send can be retried.
type File struct {
	Name        string
	ContentType string
	Data        []byte
}

// Digest is a ready-to-send weekly digest.
type Digest struct {
	Embeds []*discordgo.MessageEmbed
	Files  []File
}

// Build creates the digest for every linked user. A failure for one user is
// reported inside that user's embed rather than aborting the whole digest; an
// error is returned only when nothing useful can be produced.
func (b *Builder) Build(ctx context.Context) (*Digest, error) {
	users, err := b.Users.Users(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing linked users: %w", err)
	}
	sort.SliceStable(users, func(i, j int) bool {
		return strings.ToLower(users[i].LastFMName) < strings.ToLower(users[j].LastFMName)
	})

	now := time.Now()
	if b.now != nil {
		now = b.now()
	}
	from := now.Add(-7 * 24 * time.Hour)

	d := &Digest{}
	if len(users) == 0 {
		d.Embeds = append(d.Embeds, &discordgo.MessageEmbed{
			Title:       "Weekly digest",
			Description: "Nobody has linked a Last.fm account yet. Use `/link-lastfm` to join in.",
			Color:       embedColor,
		})
		return d, nil
	}

	for _, u := range users {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d.Embeds = append(d.Embeds, b.userEmbed(ctx, u, from, now))
	}

	if b.Graph {
		if f, err := b.activityGraph(ctx, users, now); err != nil {
			b.Log.Warn("skipping activity graph", "err", err)
		} else if f != nil {
			d.Files = append(d.Files, *f)
			d.Embeds = append(d.Embeds, &discordgo.MessageEmbed{
				Title: "Scrobbles per day",
				Color: embedColor,
				Image: &discordgo.MessageEmbedImage{URL: "attachment://" + f.Name},
			})
		}
	}
	return d, nil
}

func (b *Builder) userEmbed(ctx context.Context, u store.User, from, to time.Time) (embed *discordgo.MessageEmbed) {
	log := b.Log.With("lastfm_user", u.LastFMName, "discord_id", u.DiscordID)
	ctx, cancel := context.WithTimeout(ctx, perUserTimeout)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			log.Error("panic while building user digest", "panic", r, "stack", string(debug.Stack()))
			embed = errorEmbed(u)
		}
	}()

	tracks, err := b.LastFM.TopTracks(ctx, u.LastFMName, lastfm.PeriodWeek, topN)
	if err != nil {
		log.Error("fetching top tracks", "err", err)
		return errorEmbed(u)
	}
	artists, err := b.LastFM.TopArtists(ctx, u.LastFMName, lastfm.PeriodWeek, topN)
	if err != nil {
		log.Error("fetching top artists", "err", err)
		return errorEmbed(u)
	}
	plays, err := b.LastFM.ScrobbleCount(ctx, u.LastFMName, from, to)
	if err != nil {
		// Not fatal: the rest of the embed is still useful.
		log.Warn("fetching scrobble count", "err", err)
		plays = -1
	}

	embed = &discordgo.MessageEmbed{
		Title: truncate(fmt.Sprintf("%s's plays: %s", u.LastFMName, playsLabel(plays)), 256),
		URL:   "https://www.last.fm/user/" + url.PathEscape(u.LastFMName),
		Color: embedColor,
	}

	if len(tracks) == 0 {
		embed.Description = fmt.Sprintf("**<@%d> listened to nothing this week...**", u.DiscordID)
		return embed
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "**<@%d>'s top listens.**", u.DiscordID)
	for _, t := range tracks {
		fmt.Fprintf(&sb, "\n%d. %s by *%s* (%d)", t.Rank, Escape(t.Name), Escape(t.Artist), t.PlayCount)
	}
	if len(artists) > 0 {
		sb.WriteString("\n\n**Top Artists.**")
		for _, a := range artists {
			fmt.Fprintf(&sb, "\n%d. %s (%d)", a.Rank, Escape(a.Name), a.PlayCount)
		}
	}
	embed.Description = truncate(sb.String(), 4096)

	topArtist := tracks[0].Artist
	if len(artists) > 0 {
		topArtist = artists[0].Name
	}
	if img, err := b.Artwork.ArtistImage(ctx, topArtist); err != nil {
		log.Warn("looking up artist image", "artist", topArtist, "err", err)
	} else if img != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: img}
	}
	return embed
}

func errorEmbed(u store.User) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       truncate(u.LastFMName, 256),
		URL:         "https://www.last.fm/user/" + url.PathEscape(u.LastFMName),
		Description: fmt.Sprintf("Couldn't load <@%d>'s stats from Last.fm this time. :warning:", u.DiscordID),
		Color:       0x808080,
	}
}

func playsLabel(n int) string {
	if n < 0 {
		return "?"
	}
	return fmt.Sprint(n)
}

var markdownEscaper = strings.NewReplacer(
	`\`, `\\`, `*`, `\*`, `_`, `\_`, "`", "\\`", `~`, `\~`, `|`, `\|`, `>`, `\>`, `#`, `\#`, `[`, `\[`, `]`, `\]`,
)

// Escape neutralises Discord markdown in user-controlled text such as track
// names, so a title like "*NSYNC" doesn't break the formatting.
func Escape(s string) string { return markdownEscaper.Replace(s) }

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
