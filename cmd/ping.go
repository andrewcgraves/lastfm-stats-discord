package cmd

import (
	"github.com/bwmarrin/discordgo"
)

func Ping(s *discordgo.Session, i *discordgo.InteractionCreate) {
	print("Executed Ping Command...")
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Pong...",
		},
	})
}
