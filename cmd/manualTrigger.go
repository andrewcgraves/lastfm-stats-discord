package cmd

import (
	"github.com/andrewcgraves/lastfm-stats-discord/framework"
	"github.com/bwmarrin/discordgo"
)

func ManualTrigger(s *discordgo.Session, i *discordgo.InteractionCreate) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "manually triggered... (please wait a few seconds)",
		},
	})
	embeds := framework.TriggerWeeklyDigest()

	s.ChannelMessageSendEmbeds(i.ChannelID, embeds)
}
