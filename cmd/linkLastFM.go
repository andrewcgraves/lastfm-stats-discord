package cmd

import (
	"fmt"
	"log"
	"strconv"

	"github.com/andrewcgraves/lastfm-stats-discord/framework"
	"github.com/bwmarrin/discordgo"
)

func LinkLastFM(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options

	user := i.User
	if i.Member != nil {
		user = i.Member.User
	}
	if user == nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Couldn't identify who ran this command, please try again.",
			},
		})
		return
	}

	userId, err := strconv.Atoi(user.ID)
	if err != nil {
		log.Printf("failed to parse discord user id %q: %s", user.ID, err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "There was an unexpected error...",
			},
		})
		return
	}

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
		log.Printf("failed to save user config: %s", err)
	}
}
