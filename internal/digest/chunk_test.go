package digest

import (
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func embeds(n, descLen int) []*discordgo.MessageEmbed {
	out := make([]*discordgo.MessageEmbed, n)
	for i := range out {
		out[i] = &discordgo.MessageEmbed{Title: "t", Description: strings.Repeat("d", descLen)}
	}
	return out
}

func TestMessagesRespectEmbedCount(t *testing.T) {
	d := &Digest{Embeds: embeds(23, 10)}
	msgs := d.Messages()
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	total := 0
	for _, m := range msgs {
		if len(m.Embeds) > 10 { // Discord's per-message embed limit
			t.Fatalf("message has %d embeds", len(m.Embeds))
		}
		total += len(m.Embeds)
	}
	if total != 23 {
		t.Fatalf("lost embeds: %d", total)
	}
}

func TestMessagesRespectCharacterLimit(t *testing.T) {
	// 5 × 2001 characters against Discord's 6000-per-message limit: two fit, three don't.
	msgs := (&Digest{Embeds: embeds(5, 2000)}).Messages()
	if got := sizes(msgs); got != "2,2,1" {
		t.Fatalf("message sizes = %s, want 2,2,1", got)
	}
}

func TestCharacterLimitCountsFootersAndFields(t *testing.T) {
	// 3005 characters each once the footer and field count: 6010 won't fit in one message.
	e := func() *discordgo.MessageEmbed {
		return &discordgo.MessageEmbed{
			Description: strings.Repeat("d", 2990),
			Footer:      &discordgo.MessageEmbedFooter{Text: "ffffffffff"},
			Fields:      []*discordgo.MessageEmbedField{{Name: "nn", Value: "vvv"}},
		}
	}
	if got := sizes((&Digest{Embeds: []*discordgo.MessageEmbed{e(), e()}}).Messages()); got != "1,1" {
		t.Fatalf("message sizes = %s, want 1,1", got)
	}
}

func TestCharacterLimitCountsCharactersNotBytes(t *testing.T) {
	// 3 × 1901 characters = 5703 fits, even though it's over 11,000 bytes.
	es := embeds(3, 0)
	for _, e := range es {
		e.Description = strings.Repeat("é", 1900)
	}
	if got := sizes((&Digest{Embeds: es}).Messages()); got != "3" {
		t.Fatalf("message sizes = %s, want 3", got)
	}
}

func sizes(msgs []*discordgo.MessageSend) string {
	var out []string
	for _, m := range msgs {
		out = append(out, strconv.Itoa(len(m.Embeds)))
	}
	return strings.Join(out, ",")
}

func TestMessagesEmpty(t *testing.T) {
	if msgs := (&Digest{}).Messages(); len(msgs) != 0 {
		t.Fatalf("got %d messages for an empty digest", len(msgs))
	}
}

func TestFilesTravelWithTheirEmbed(t *testing.T) {
	es := embeds(12, 1)
	es = append(es, &discordgo.MessageEmbed{Image: &discordgo.MessageEmbedImage{URL: "attachment://g.png"}})
	d := &Digest{Embeds: es, Files: []File{{Name: "g.png", ContentType: "image/png", Data: []byte("png-bytes")}}}

	msgs := d.Messages()
	if len(msgs) != 2 {
		t.Fatalf("got %d messages", len(msgs))
	}
	if len(msgs[0].Files) != 0 {
		t.Errorf("first message should carry no files")
	}
	if len(msgs[1].Files) != 1 || msgs[1].Files[0].Name != "g.png" {
		t.Fatalf("second message files = %+v", msgs[1].Files)
	}

	// Each call must hand out fresh readers so a failed send can be retried.
	for range 2 {
		f := d.Messages()[1].Files[0]
		b, err := io.ReadAll(f.Reader)
		if err != nil || string(b) != "png-bytes" {
			t.Fatalf("read %q, %v", b, err)
		}
	}
}
