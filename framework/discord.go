package framework

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
)

var dSession *discordgo.Session

func InitDiscordConnection(token string, commands []*discordgo.ApplicationCommand, commandHandlers map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate)) {
	dSession, err := discordgo.New("Bot " + token)
	Check(err)
	dSession.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		fmt.Printf("Logged in as %s\n", s.State.User.Username)
	})

	// handlers for the commands
	dSession.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})

	dSession.Open()
	time.Sleep(time.Second * 2)

	activeGlobalCommands, err := dSession.ApplicationCommands(dSession.State.User.ID, "")
	Check(err)
	for _, cmd := range activeGlobalCommands {
		err = dSession.ApplicationCommandDelete(dSession.State.User.ID, "", cmd.ID)
		Check(err)
	}

	for _, guild := range dSession.State.Guilds {
		// activeCommands, err := dSession.ApplicationCommands(dSession.State.User.ID, guild.ID)
		// Check(err)
		// for _, cmd := range activeCommands {
		// 	dSession.ApplicationCommandDelete(dSession.State.User.ID, guild.ID, cmd.ID)
		// }

		for _, v := range commands {
			dSession.ApplicationCommandCreate(dSession.State.User.ID, guild.ID, v)
		}
	}

	dSession.UpdateListeningStatus("your music...")
	fmt.Println("Connected to Discord...")
}

func TerminateDiscordConnection() {
	dSession.Close()
}

func SendEmbedsToChannel(channelId string, embeds []*discordgo.MessageEmbed) {
	dSession.ChannelMessageSendEmbeds(channelId, embeds)
}
