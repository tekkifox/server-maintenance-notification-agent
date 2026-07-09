package service

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type DiscordRCONBridge struct {
	session            *discordgo.Session
	dockerCommander    *DockerCommander
	channelToContainer map[string]string
	channelIDs         []string
}

func NewDiscordRCONBridge(session *discordgo.Session, dockerCommander *DockerCommander, channelIDs []string, containerNames []string) *DiscordRCONBridge {
	channelToContainer := make(map[string]string)
	for i, chID := range channelIDs {
		chID = strings.TrimSpace(chID)
		if chID == "" {
			continue
		}
		if i < len(containerNames) {
			containerName := strings.TrimSpace(containerNames[i])
			if containerName != "" {
				channelToContainer[chID] = containerName
			}
		}
	}

	return &DiscordRCONBridge{
		session:            session,
		dockerCommander:    dockerCommander,
		channelToContainer: channelToContainer,
		channelIDs:         channelIDs,
	}
}

func (b *DiscordRCONBridge) Start() {
	if len(b.channelIDs) == 0 {
		log.Println("Discord RCON bridge is disabled (DISCORD_INGEST_CHANNEL_IDS not configured)")
		return
	}

	log.Printf("Starting Discord RCON bridge for channels: %v (mapped to containers: %+v)", b.channelIDs, b.channelToContainer)
	b.session.AddHandler(b.handleMessage)
}

func (b *DiscordRCONBridge) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore messages from the bot itself
	if s.State != nil && s.State.User != nil && m.Author.ID == s.State.User.ID {
		return
	}

	// Verify if the message is from one of the monitored channels
	isMonitored := false
	for _, id := range b.channelIDs {
		if m.ChannelID == id {
			isMonitored = true
			break
		}
	}
	if !isMonitored {
		return
	}

	content := strings.TrimSpace(m.Content)
	if content == "" {
		return
	}

	log.Printf("Discord RCON bridge: ingesting message from %s in channel %s: %s", m.Author.Username, m.ChannelID, content)

	// Format message as [Discord] User: Message
	formattedMessage := fmt.Sprintf("[Discord] %s: %s", m.Author.Username, content)

	// Broadcast the message using DockerCommander targeting RCON transport
	req := BroadcastRequest{
		Transport: BroadcastTransportRCON,
		Message:   formattedMessage,
	}

	// Map specific container ref if configured
	if containerRef, ok := b.channelToContainer[m.ChannelID]; ok && containerRef != "" {
		req.ContainerID = containerRef
	}

	ctx := context.Background()
	result, err := b.dockerCommander.Broadcast(ctx, req)
	if err != nil {
		log.Printf("Discord RCON bridge error broadcasting message: %v", err)
		return
	}

	log.Printf("Discord RCON bridge: broadcast complete. Deliveries: %+v", result.Deliveries)
}
