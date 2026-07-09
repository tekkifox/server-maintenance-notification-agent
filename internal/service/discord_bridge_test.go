package service

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestDiscordRCONBridge_ProcessAndBroadcast_MultipleChannels(t *testing.T) {
	session, _ := discordgo.New("Bot dummy-token")
	session.State = discordgo.NewState()
	session.State.User = &discordgo.User{ID: "bot-user-id"}

	rcon := &recordingRCONExecutor{}
	commander := NewDockerCommander(
		nil,
		rcon,
		nil,
		nil,
		nil,
		[]string{"minecraft-server", "vrising-server"},
		[]string{"rcon", "rcon"},
		[]string{"127.0.0.1:27115", "127.0.0.1:9876"},
		[]string{"rcon-pass", "vrising-pass"},
	)

	resolver := staticGameResolver{
		"minecraft-server": "minecraft",
		"vrising-server":   "vrising",
	}
	commander.SetGameResolver(resolver)

	// Map chan-1 -> minecraft-server and chan-2 -> vrising-server
	bridge := NewDiscordRCONBridge(session, commander, []string{"chan-1", "chan-2"}, []string{"minecraft-server", "vrising-server"})
	bridge.Start()

	// 1. Process message on chan-1 -> should be routed specifically to minecraft-server (127.0.0.1:27115)
	mCreate1 := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "chan-1",
			Content:   "Hello minecraft!",
			Author: &discordgo.User{
				ID:       "some-user-id",
				Username: "Alice",
			},
		},
	}
	bridge.handleMessage(session, mCreate1)

	if len(rcon.calls) != 1 {
		t.Fatalf("expected 1 RCON call, got %d", len(rcon.calls))
	}
	if rcon.calls[0].address != "127.0.0.1:27115" {
		t.Fatalf("expected call to minecraft-server (127.0.0.1:27115), got %s", rcon.calls[0].address)
	}
	expectedCmd1 := `say "[Discord] Alice: Hello minecraft!"`
	if rcon.calls[0].command != expectedCmd1 {
		t.Fatalf("expected RCON command %q, got %q", expectedCmd1, rcon.calls[0].command)
	}

	// Reset calls
	rcon.calls = nil

	// 2. Process message on chan-2 -> should be routed specifically to vrising-server (127.0.0.1:9876)
	mCreate2 := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "chan-2",
			Content:   "Hello vrising!",
			Author: &discordgo.User{
				ID:       "some-user-id",
				Username: "Bob",
			},
		},
	}
	bridge.handleMessage(session, mCreate2)

	if len(rcon.calls) != 1 {
		t.Fatalf("expected 1 RCON call, got %d", len(rcon.calls))
	}
	if rcon.calls[0].address != "127.0.0.1:9876" {
		t.Fatalf("expected call to vrising-server (127.0.0.1:9876), got %s", rcon.calls[0].address)
	}
	expectedCmd2 := `announce "[Discord] Bob: Hello vrising!"`
	if rcon.calls[0].command != expectedCmd2 {
		t.Fatalf("expected RCON command %q, got %q", expectedCmd2, rcon.calls[0].command)
	}

	// Reset calls
	rcon.calls = nil

	// 3. Process message on unmonitored channel -> should be ignored
	mCreateOther := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "unmonitored-channel",
			Content:   "Hello anyway",
			Author: &discordgo.User{
				ID:       "some-user-id",
				Username: "Alice",
			},
		},
	}
	bridge.handleMessage(session, mCreateOther)
	if len(rcon.calls) != 0 {
		t.Fatalf("expected 0 RCON calls for unmonitored channel, got %d", len(rcon.calls))
	}
}

func TestDiscordRCONBridge_BroadcastAllIfNoMapping(t *testing.T) {
	session, _ := discordgo.New("Bot dummy-token")
	session.State = discordgo.NewState()
	session.State.User = &discordgo.User{ID: "bot-user-id"}

	rcon := &recordingRCONExecutor{}
	commander := NewDockerCommander(
		nil,
		rcon,
		nil,
		nil,
		nil,
		[]string{"minecraft-server", "vrising-server"},
		[]string{"rcon", "rcon"},
		[]string{"127.0.0.1:27115", "127.0.0.1:9876"},
		[]string{"rcon-pass", "vrising-pass"},
	)

	// Ingest channel configured, but NO container mapping provided -> should broadcast to ALL targets!
	bridge := NewDiscordRCONBridge(session, commander, []string{"chan-ingest"}, nil)
	bridge.Start()

	mCreate := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "chan-ingest",
			Content:   "Broadcasting to everyone!",
			Author: &discordgo.User{
				ID:       "some-user-id",
				Username: "Alice",
			},
		},
	}
	bridge.handleMessage(session, mCreate)

	if len(rcon.calls) != 2 {
		t.Fatalf("expected 2 RCON calls (all targets), got %d", len(rcon.calls))
	}
}
