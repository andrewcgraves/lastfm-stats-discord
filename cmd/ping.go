package cmd

import (
	"github.com/bwmarrin/discordgo"
)

func Ping(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// fmt.Println("Executed Ping Command...")
	// name := framework.GenerateDailyActivityGraph()
	// url, err := framework.UploadFile(name, name)

	// fmt.Printf("%s -> %e", url, err)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Pong...",
			Embeds: []*discordgo.MessageEmbed{
				{
					Type:  discordgo.EmbedTypeArticle,
					Title: "test",
				},
			},
		},
	})
}
