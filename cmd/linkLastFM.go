package cmd

import (
	"fmt"
	"strconv"

	"github.com/andrewcgraves/lastfm-stats-discord/framework"
	"github.com/bwmarrin/discordgo"
)

func LinkLastFM(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	userId, err := strconv.Atoi(i.Member.User.ID)
	framework.Check(err)
	err = framework.SaveUserConfig(framework.LastFMEntry{DiscordID: userId, LastFMName: options[0].StringValue()})
	if err == nil {
		content := fmt.Sprintf("(<@%d>) :link: (%s)", userId, options[0].StringValue())
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: content,
			},
		})
	} else {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "There was an unexpected error...",
			},
		})
		print(err)
	}
}
