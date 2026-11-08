package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/andrewcgraves/lastfm-stats-discord/internal/digest"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/lastfm"
	"github.com/andrewcgraves/lastfm-stats-discord/internal/store"
	"github.com/bwmarrin/discordgo"
)

type commandHandler func(s *discordgo.Session, i *discordgo.InteractionCreate, log *slog.Logger)

// Commands are the slash commands registered in every guild.
var Commands = []*discordgo.ApplicationCommand{
	{
		Name:        "ping",
		Description: "Ping test command",
	},
	{
		Name:        "link-lastfm",
		Description: "Register your lastfm username with your discord account",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "lastfm-username",
				Description: "Last.fm Username",
				Required:    true,
				MaxLength:   64,
			},
		},
	},
	{
		Name:        "manual-trigger",
		Description: "get the weekly lastfm stats for registered users",
	},
}

// Interaction tokens are valid for 15 minutes; leave headroom to reply.
const interactionBudget = 13 * time.Minute

func (b *Bot) handlePing(s *discordgo.Session, i *discordgo.InteractionCreate, log *slog.Logger) {
	content := fmt.Sprintf("Pong... (gateway latency %s)", s.HeartbeatLatency().Round(time.Millisecond))
	b.respond(s, i, log, &discordgo.InteractionResponseData{Content: content})
}

func (b *Bot) handleLinkLastFM(s *discordgo.Session, i *discordgo.InteractionCreate, log *slog.Logger) {
	if !b.deferResponse(s, i, log, false) {
		return
	}
	ctx, cancel := context.WithTimeout(b.ctx, time.Minute)
	defer cancel()

	user := interactionUser(i)
	discordID, err := strconv.ParseInt(user.ID, 10, 64)
	if err != nil {
		log.Error("parsing discord user id", "id", user.ID, "err", err)
		b.editResponse(s, i, log, "Couldn't work out who you are. Please try again.")
		return
	}

	var name string
	if opts := i.ApplicationCommandData().Options; len(opts) > 0 {
		name = strings.TrimSpace(opts[0].StringValue())
	}
	if name == "" {
		b.editResponse(s, i, log, "Please give me your Last.fm username.")
		return
	}

	info, err := b.lastfm.UserInfo(ctx, name)
	switch {
	case errors.Is(err, lastfm.ErrUserNotFound):
		b.editResponse(s, i, log, fmt.Sprintf("I couldn't find a Last.fm user called `%s`. Check the spelling and try again.", name))
		return
	case err != nil:
		log.Error("looking up last.fm user", "lastfm_user", name, "err", err)
		b.editResponse(s, i, log, "Last.fm isn't responding right now. Please try again in a few minutes.")
		return
	}
	if info.Name != "" {
		name = info.Name // use Last.fm's canonical capitalisation
	}

	if err := b.store.LinkUser(ctx, store.User{DiscordID: discordID, LastFMName: name}); err != nil {
		log.Error("saving link", "lastfm_user", name, "err", err)
		b.editResponse(s, i, log, "There was an unexpected error saving your link. Please try again.")
		return
	}
	log.Info("linked last.fm account", "lastfm_user", name)
	b.editResponse(s, i, log, fmt.Sprintf("(<@%d>) :link: (%s)", discordID, digest.Escape(name)))
}

func (b *Bot) handleManualTrigger(s *discordgo.Session, i *discordgo.InteractionCreate, log *slog.Logger) {
	if !b.digestMu.TryLock() {
		b.respond(s, i, log, &discordgo.InteractionResponseData{
			Content: "A digest is already being generated, hang tight.",
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		return
	}
	defer b.digestMu.Unlock()

	if !b.deferResponse(s, i, log, false) {
		return
	}
	ctx, cancel := context.WithTimeout(b.ctx, interactionBudget)
	defer cancel()

	d, err := b.digest.Build(ctx)
	if err != nil {
		log.Error("building digest", "err", err)
		b.editResponse(s, i, log, "I couldn't build the digest right now. Please try again in a few minutes.")
		return
	}

	msgs := d.Messages()
	if len(msgs) == 0 {
		b.editResponse(s, i, log, "Nothing to report this week.")
		return
	}
	first := msgs[0]
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &first.Embeds,
		Files:  first.Files,
	}, discordgo.WithContext(ctx)); err != nil {
		log.Error("sending digest response", "err", err)
		return
	}
	for _, m := range msgs[1:] {
		if _, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Embeds: m.Embeds,
			Files:  m.Files,
		}, discordgo.WithContext(ctx)); err != nil {
			log.Error("sending digest follow-up", "err", err)
			return
		}
	}
}

// RunScheduledDigest builds the digest and posts it to the configured channel.
// It is safe to call from the scheduler: it never panics and logs every failure.
func (b *Bot) RunScheduledDigest(ctx context.Context) {
	log := b.log.With("job", "weekly-digest")
	defer func() {
		if r := recover(); r != nil {
			log.Error("weekly digest panicked", "panic", r)
		}
	}()

	b.digestMu.Lock()
	defer b.digestMu.Unlock()

	log.Info("weekly digest started")
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	var d *digest.Digest
	err := retry(ctx, log, "building weekly digest", func() (bool, error) {
		var err error
		d, err = b.digest.Build(ctx)
		return true, err
	}, maxAttempts(3), backoffFrom(time.Minute, 5*time.Minute))
	if err != nil {
		log.Error("weekly digest failed", "err", err)
		notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, _ = b.s.ChannelMessageSend(b.channelID,
			"I couldn't build this week's digest (Last.fm or the database is unavailable). Try `/manual-trigger` later.",
			discordgo.WithContext(notifyCtx))
		return
	}
	if err := b.PostDigest(ctx, b.channelID, d); err != nil {
		log.Error("posting weekly digest", "err", err)
		return
	}
	log.Info("weekly digest posted", "embeds", len(d.Embeds))
}

// PostDigest sends d to channelID, retrying each message on failure.
func (b *Bot) PostDigest(ctx context.Context, channelID string, d *digest.Digest) error {
	for idx := range d.Messages() {
		err := retry(ctx, b.log, "posting digest message", func() (bool, error) {
			// Rebuild so attachment readers start from the beginning on retries.
			msg := d.Messages()[idx]
			_, err := b.s.ChannelMessageSendComplex(channelID, msg, discordgo.WithContext(ctx))
			var rest *discordgo.RESTError
			if errors.As(err, &rest) && rest.Response != nil && rest.Response.StatusCode >= 400 && rest.Response.StatusCode < 500 && rest.Response.StatusCode != 429 {
				return false, err // bad request / missing permissions: retrying won't help
			}
			return true, err
		}, maxAttempts(5), backoffFrom(5*time.Second, time.Minute))
		if err != nil {
			return fmt.Errorf("message %d: %w", idx+1, err)
		}
	}
	return nil
}

func (b *Bot) respond(s *discordgo.Session, i *discordgo.InteractionCreate, log *slog.Logger, data *discordgo.InteractionResponseData) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: data,
	}, discordgo.WithContext(b.ctx))
	if err != nil {
		log.Error("responding to interaction", "err", err)
	}
}

// deferResponse acknowledges the interaction within Discord's 3 second window
// so slow work can follow. It reports whether the acknowledgement succeeded.
func (b *Bot) deferResponse(s *discordgo.Session, i *discordgo.InteractionCreate, log *slog.Logger, ephemeral bool) bool {
	data := &discordgo.InteractionResponseData{}
	if ephemeral {
		data.Flags = discordgo.MessageFlagsEphemeral
	}
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: data,
	}, discordgo.WithContext(b.ctx))
	if err != nil {
		log.Error("deferring interaction response", "err", err)
		return false
	}
	return true
}

func (b *Bot) editResponse(s *discordgo.Session, i *discordgo.InteractionCreate, log *slog.Logger, content string) {
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content}, discordgo.WithContext(b.ctx)); err != nil {
		log.Error("editing interaction response", "err", err)
	}
}

// respondError tells the user something failed, whether or not the
// interaction has already been acknowledged.
func (b *Bot) respondError(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content, Flags: discordgo.MessageFlagsEphemeral},
	})
	if err != nil {
		_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content})
	}
}
