package digest

import (
	"bytes"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

// Discord limits per message.
const (
	maxEmbedsPerMessage = 10
	maxEmbedCharacters  = 6000
)

// Messages splits the digest into messages that fit Discord's limits (at most
// 10 embeds and 6000 embed characters each). Attachments travel with the
// message that contains the embed referencing them. Each call builds fresh
// file readers, so the result can be used for a retry.
func (d *Digest) Messages() []*discordgo.MessageSend {
	var (
		out   []*discordgo.MessageSend
		cur   []*discordgo.MessageEmbed
		chars int
	)
	flush := func() {
		if len(cur) > 0 {
			out = append(out, &discordgo.MessageSend{Embeds: cur, Files: d.filesFor(cur)})
		}
		cur, chars = nil, 0
	}
	for _, e := range d.Embeds {
		n := embedLength(e)
		if len(cur) == maxEmbedsPerMessage || (len(cur) > 0 && chars+n > maxEmbedCharacters) {
			flush()
		}
		cur = append(cur, e)
		chars += n
	}
	flush()
	return out
}

func (d *Digest) filesFor(embeds []*discordgo.MessageEmbed) []*discordgo.File {
	var files []*discordgo.File
	for _, f := range d.Files {
		ref := "attachment://" + f.Name
		for _, e := range embeds {
			if (e.Image != nil && e.Image.URL == ref) || (e.Thumbnail != nil && e.Thumbnail.URL == ref) {
				files = append(files, &discordgo.File{Name: f.Name, ContentType: f.ContentType, Reader: bytes.NewReader(f.Data)})
				break
			}
		}
	}
	return files
}

// embedLength counts the characters Discord includes in its 6000 limit.
func embedLength(e *discordgo.MessageEmbed) int {
	n := runeLen(e.Title) + runeLen(e.Description)
	if e.Footer != nil {
		n += runeLen(e.Footer.Text)
	}
	if e.Author != nil {
		n += runeLen(e.Author.Name)
	}
	for _, f := range e.Fields {
		n += runeLen(f.Name) + runeLen(f.Value)
	}
	return n
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }
