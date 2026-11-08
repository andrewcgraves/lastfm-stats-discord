package digest

import (
	"io"
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
		if len(m.Embeds) > maxEmbedsPerMessage {
			t.Fatalf("message has %d embeds", len(m.Embeds))
		}
		total += len(m.Embeds)
	}
	if total != 23 {
		t.Fatalf("lost embeds: %d", total)
	}
}

func TestMessagesRespectCharacterLimit(t *testing.T) {
	d := &Digest{Embeds: embeds(5, 2000)} // 5 × 2001 characters
	msgs := d.Messages()
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	for _, m := range msgs {
		n := 0
		for _, e := range m.Embeds {
			n += embedLength(e)
		}
		if n > maxEmbedCharacters {
			t.Fatalf("message has %d characters", n)
		}
	}
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
