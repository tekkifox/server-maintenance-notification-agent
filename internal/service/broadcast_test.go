package service

import "testing"

func TestBuildBroadcastCommand(t *testing.T) {
	tests := []struct {
		name     string
		game     string
		override string
		message  string
		wantCmd  string
		wantGame string
		wantErr  bool
	}{
		{
			name:     "minecraft",
			game:     "Minecraft",
			message:  "Server restart in 10 minutes",
			wantCmd:  "say \"Server restart in 10 minutes\"",
			wantGame: "minecraft",
		},
		{
			name:     "unturned",
			game:     "Unturned",
			message:  "Server restart in 10 minutes",
			wantCmd:  "say \"Server restart in 10 minutes\"",
			wantGame: "unturned",
		},
		{
			name:     "starbound",
			game:     "Starbound",
			message:  "Server restart in 10 minutes",
			wantCmd:  "say \"Server restart in 10 minutes\"",
			wantGame: "starbound",
		},
		{
			name:     "satisfactory",
			game:     "Satisfactory",
			message:  "Server restart in 10 minutes",
			wantCmd:  "Broadcast \"Server restart in 10 minutes\"",
			wantGame: "satisfactory",
		},
		{
			name:     "vrising",
			game:     "V Rising",
			message:  "Base raid window closed",
			wantCmd:  "announce \"Base raid window closed\"",
			wantGame: "vrising",
		},
		{
			name:     "vrising dedicated server alias",
			game:     "V Rising Dedicated Server",
			message:  "Base raid window closed",
			wantCmd:  "announce \"Base raid window closed\"",
			wantGame: "vrising",
		},
		{
			name:     "palworld dedicated server alias",
			game:     "Palworld Dedicated Server",
			message:  "Server restart in 10 minutes",
			wantCmd:  "Broadcast \"Server restart in 10 minutes\"",
			wantGame: "palworld",
		},
		{
			name:     "factorio",
			game:     "Factorio",
			message:  `Hello "world"`,
			wantCmd:  `game.print("Hello \"world\"")`,
			wantGame: "factorio",
		},
		{
			name:     "custom override",
			game:     "Unknown",
			override: "say hello everyone",
			message:  "ignored",
			wantCmd:  "say hello everyone",
			wantGame: "Unknown",
		},
		{
			name:     "unknown game",
			game:     "SomeGame",
			message:  "hello",
			wantCmd:  "say hello",
			wantGame: "SomeGame",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotCmd, gotGame, err := buildBroadcastCommand(tc.game, tc.override, tc.message)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotCmd != tc.wantCmd {
				t.Fatalf("command mismatch: got %q want %q", gotCmd, tc.wantCmd)
			}
			if gotGame != tc.wantGame {
				t.Fatalf("game mismatch: got %q want %q", gotGame, tc.wantGame)
			}
		})
	}
}
