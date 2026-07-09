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
	bridge := NewDiscordRCONBridge(session, commander, []string{"chan-1", "chan-2"}, []string{"minecraft-server", "vrising-server"}, nil)
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
	bridge := NewDiscordRCONBridge(session, commander, []string{"chan-ingest"}, nil, nil)
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

func TestDiscordRCONBridge_FilteringPhrases(t *testing.T) {
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
		[]string{"minecraft-server"},
		[]string{"rcon"},
		[]string{"127.0.0.1:27115"},
		[]string{"rcon-pass"},
	)

	// Ingest channel configured, extra filtered phrases: "custom ban"
	bridge := NewDiscordRCONBridge(session, commander, []string{"chan-ingest"}, nil, []string{"custom ban"})
	bridge.Start()

	// 1. A message that doesn't trigger any filters should be broadcasted
	mCreateOK := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "chan-ingest",
			Content:   "Hello server!",
			Author: &discordgo.User{
				ID:       "some-user-id",
				Username: "Alice",
			},
		},
	}
	bridge.handleMessage(session, mCreateOK)
	if len(rcon.calls) != 1 {
		t.Fatalf("expected 1 RCON call, got %d", len(rcon.calls))
	}

	// Reset calls
	rcon.calls = nil

	// 2. A message containing a default filtered phrase (e.g. "has killed") should be ignored
	mCreateKilled := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "chan-ingest",
			Content:   "Player1 has killed Player2",
			Author: &discordgo.User{
				ID:       "some-user-id",
				Username: "Alice",
			},
		},
	}
	bridge.handleMessage(session, mCreateKilled)
	if len(rcon.calls) != 0 {
		t.Fatalf("expected message with default filtered phrase to be ignored, got RCON call")
	}

	// 3. A message containing a default filtered phrase with different casing (e.g. "Has Died") should be ignored case-insensitively
	mCreateDied := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "chan-ingest",
			Content:   "Player1 Has Died!",
			Author: &discordgo.User{
				ID:       "some-user-id",
				Username: "Alice",
			},
		},
	}
	bridge.handleMessage(session, mCreateDied)
	if len(rcon.calls) != 0 {
		t.Fatalf("expected message with case-insensitive default filtered phrase to be ignored, got RCON call")
	}

	// 4. A message containing a custom filtered phrase (e.g. "Custom Ban") should be ignored case-insensitively
	mCreateCustom := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "chan-ingest",
			Content:   "You got a Custom Ban",
			Author: &discordgo.User{
				ID:       "some-user-id",
				Username: "Alice",
			},
		},
	}
	bridge.handleMessage(session, mCreateCustom)
	if len(rcon.calls) != 0 {
		t.Fatalf("expected message with custom filtered phrase to be ignored, got RCON call")
	}
}
