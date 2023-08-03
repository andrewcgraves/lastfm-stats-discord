package cmd

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

func Refresh(s *discordgo.Session, i *discordgo.InteractionCreate) {
	fmt.Println("Executed Refresh Command...")
}
