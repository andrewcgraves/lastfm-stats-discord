package framework

import (
	"fmt"
	"log"
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
		defer func() {
			if r := recover(); r != nil {
				log.Printf("recovered from panic in command handler %q: %v", i.ApplicationCommandData().Name, r)
			}
		}()

		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})

	dSession.Open()
	time.Sleep(time.Second * 2)

	// Command (re)registration is best-effort: a transient Discord API error
	// here shouldn't take down the whole bot, since the scheduled digest and
	// any already-registered commands don't depend on it succeeding.
	activeGlobalCommands, err := dSession.ApplicationCommands(dSession.State.User.ID, "")
	if err != nil {
		log.Printf("failed to fetch global commands: %s", err)
	}
	for _, cmd := range activeGlobalCommands {
		if err := dSession.ApplicationCommandDelete(dSession.State.User.ID, "", cmd.ID); err != nil {
			log.Printf("failed to delete global command %q: %s", cmd.Name, err)
		}
	}

	for _, guild := range dSession.State.Guilds {
		activeCommands, err := dSession.ApplicationCommands(dSession.State.User.ID, guild.ID)
		if err != nil {
			log.Printf("failed to fetch commands for guild %q: %s", guild.ID, err)
		}
		for _, cmd := range activeCommands {
			if err := dSession.ApplicationCommandDelete(dSession.State.User.ID, guild.ID, cmd.ID); err != nil {
				log.Printf("failed to delete command %q for guild %q: %s", cmd.Name, guild.ID, err)
			}
		}

		for _, v := range commands {
			if _, err := dSession.ApplicationCommandCreate(dSession.State.User.ID, guild.ID, v); err != nil {
				log.Printf("failed to create command %q for guild %q: %s", v.Name, guild.ID, err)
			}
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
