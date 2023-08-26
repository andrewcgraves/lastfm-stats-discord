package framework

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
)

type DiscordSession struct {
	*discordgo.Session
}

func InitDiscordConnection(token string, commands []*discordgo.ApplicationCommand, commandHandlers map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate)) DiscordSession {
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
		activeCommands, err := dSession.ApplicationCommands(dSession.State.User.ID, guild.ID)
		Check(err)
		for _, cmd := range activeCommands {
			dSession.ApplicationCommandDelete(dSession.State.User.ID, guild.ID, cmd.ID)
		}

		for _, v := range commands {
			dSession.ApplicationCommandCreate(dSession.State.User.ID, guild.ID, v)
		}
	}

	dSession.UpdateListeningStatus("your music...")
	fmt.Println("Connected to Discord...")

	return DiscordSession{dSession}
}

// func RefreshCommands() {
// 	activeGlobalCommands, err := dSession.ApplicationCommands(dSession.State.User.ID, "")
// 	Check(err)
// 	for _, cmd := range activeGlobalCommands {
// 		err = dSession.ApplicationCommandDelete(dSession.State.User.ID, "", cmd.ID)
// 		Check(err)
// 	}

// 	for _, guild := range dSession.State.Guilds {
// 		activeCommands, err := dSession.ApplicationCommands(dSession.State.User.ID, guild.ID)
// 		Check(err)
// 		for _, cmd := range activeCommands {
// 			dSession.ApplicationCommandDelete(dSession.State.User.ID, guild.ID, cmd.ID)
// 		}

// 		for _, v := range commands {
// 			dSession.ApplicationCommandCreate(dSession.State.User.ID, guild.ID, v)
// 		}
// 	}
// }

func (s *DiscordSession) terminateDiscordConnection() {
	s.Close()
}

func (s *DiscordSession) GetUserInformation(userId string) (*discordgo.User, error) {
	// s.ChannelMessageSend("787217549842055189", "content")
	dUser, err := s.User(userId)
	return dUser, err
}

// Roles will need to be refreshed if we keep them stashed
func (s *DiscordSession) GetUserRoleColor(guildId, string, userId string) (int, error) {
	guild, err := s.Guild(guildId)

	if err != nil {
		return 0, err
	}

	var topRole *discordgo.Role
	// topRole := *discordgo.Role
	// roleMapping := new(map string[]*discordgo.Role)

	// whats the HEX for a role that does not have a color ?
	// for _, role := range(guild.Roles) {
	// 	role.
	// }

	for _, member := range guild.Members {
		if member.User.ID == userId {
			for _, roleId := range member.Roles {
				// TODO: this is bad, replace it with a mapping or something thats generated at runtime or before each call idk
				role, _ := s.State.Role(guildId, roleId)
				if err != nil {
					return 0, err
				}

				if role.Color != 0 && (topRole == nil || topRole.Position > topRole.Position) {
					topRole = role
				}
			}
			break
		}
	}

	return topRole.Color, nil
}

func (s *DiscordSession) SendComplexMessageToChannel(channelId string, embeds []*discordgo.MessageEmbed) {
	s.ChannelMessageSendComplex(channelId, &discordgo.MessageSend{
		Embeds: embeds,
	})
}
