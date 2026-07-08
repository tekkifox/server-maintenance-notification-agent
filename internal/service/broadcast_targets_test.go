package service

import (
	"context"
	"testing"
)

type recordingCommander struct {
	calls []recordingCall
}

type recordingRCONExecutor struct {
	calls []recordingRCONCall
}

type recordingRCONCall struct {
	address  string
	password string
	command  string
}

type recordingCall struct {
	containerRef string
	command      string
}

func (r *recordingCommander) SendCommand(_ context.Context, containerID, command string) error {
	r.calls = append(r.calls, recordingCall{containerRef: containerID, command: command})
	return nil
}

func (r *recordingRCONExecutor) Execute(_ context.Context, address, password, command string) (string, error) {
	r.calls = append(r.calls, recordingRCONCall{address: address, password: password, command: command})
	return "ok", nil
}

func TestBroadcastUsesDefaultContainerIDs(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, []string{"alpha", "beta"}, nil, nil, nil, nil, nil, nil)

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Game:    "Minecraft",
		Message: "Restart in 10 minutes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Sent {
		t.Fatalf("expected sent to be true")
	}
	if len(result.Deliveries) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(result.Deliveries))
	}
	if len(recorder.calls) != 2 {
		t.Fatalf("expected 2 docker calls, got %d", len(recorder.calls))
	}
	if recorder.calls[0].containerRef != "alpha" || recorder.calls[1].containerRef != "beta" {
		t.Fatalf("unexpected container targets: %+v", recorder.calls)
	}
	if recorder.calls[0].command != "say \"Restart in 10 minutes\"" || recorder.calls[1].command != "say \"Restart in 10 minutes\"" {
		t.Fatalf("unexpected command broadcast: %+v", recorder.calls)
	}
}

func TestBroadcastUsesRCONTransport(t *testing.T) {
	console := &recordingCommander{}
	rcon := &recordingRCONExecutor{}
	commander := NewDockerCommander(console, rcon, nil, []string{"minecraft-server"}, []string{"minecraft"}, []string{"minecraft-server"}, []string{"minecraft"}, []string{"127.0.0.1:27015"}, []string{"secret"})

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Transport: BroadcastTransportRCON,
		Message:   "Restart in 10 minutes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(result.Deliveries))
	}
	if len(console.calls) != 0 {
		t.Fatalf("expected no console calls, got %d", len(console.calls))
	}
	if len(rcon.calls) != 1 {
		t.Fatalf("expected 1 rcon call, got %d", len(rcon.calls))
	}
	if rcon.calls[0].address != "127.0.0.1:27015" {
		t.Fatalf("unexpected rcon address: %+v", rcon.calls[0])
	}
	if rcon.calls[0].password != "secret" {
		t.Fatalf("unexpected rcon password: %+v", rcon.calls[0])
	}
	if rcon.calls[0].command != "say \"Restart in 10 minutes\"" {
		t.Fatalf("unexpected rcon command: %+v", rcon.calls[0])
	}
}

func TestBroadcastPrefersExplicitContainerIDs(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, []string{"alpha", "beta"}, nil, nil, nil, nil, nil, nil)

	_, err := commander.Broadcast(context.Background(), BroadcastRequest{
		ContainerIDs: []string{"gamma", "delta"},
		Game:         "VRising",
		Message:      "Arena open",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(recorder.calls) != 2 {
		t.Fatalf("expected 2 docker calls, got %d", len(recorder.calls))
	}
	if recorder.calls[0].containerRef != "gamma" || recorder.calls[1].containerRef != "delta" {
		t.Fatalf("unexpected container targets: %+v", recorder.calls)
	}
}

func TestBroadcastUsesDefaultContainerNamesAndGames(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, nil, []string{"minecraft-server", "vrising-server"}, []string{"minecraft", "vrising"}, nil, nil, nil, nil)

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Message: "Maintenance starts in 10 minutes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Deliveries) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(result.Deliveries))
	}
	if len(recorder.calls) != 2 {
		t.Fatalf("expected 2 docker calls, got %d", len(recorder.calls))
	}
	if recorder.calls[0].containerRef != "minecraft-server" || recorder.calls[1].containerRef != "vrising-server" {
		t.Fatalf("unexpected container targets: %+v", recorder.calls)
	}
	if recorder.calls[0].command != "say \"Maintenance starts in 10 minutes\"" {
		t.Fatalf("unexpected minecraft command: %q", recorder.calls[0].command)
	}
	if recorder.calls[1].command != "announce \"Maintenance starts in 10 minutes\"" {
		t.Fatalf("unexpected vrising command: %q", recorder.calls[1].command)
	}
}

func TestBroadcastUsesExplicitContainerNames(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, nil, []string{"minecraft-server"}, []string{"minecraft"}, nil, nil, nil, nil)

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		ContainerNames: []string{"minecraft-server", "vrising-server"},
		Message:        "Hello from the API",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Deliveries) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(result.Deliveries))
	}
	if recorder.calls[0].containerRef != "minecraft-server" || recorder.calls[1].containerRef != "vrising-server" {
		t.Fatalf("unexpected container targets: %+v", recorder.calls)
	}
	if recorder.calls[0].command != "say \"Hello from the API\"" {
		t.Fatalf("unexpected minecraft command: %q", recorder.calls[0].command)
	}
	if recorder.calls[1].command != "say \"Hello from the API\"" {
		t.Fatalf("unexpected fallback command: %q", recorder.calls[1].command)
	}
}

func TestBroadcastUsesSingleContainerName(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, nil, []string{"minecraft-server"}, []string{"minecraft"}, nil, nil, nil, nil)

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		ContainerName: "minecraft-server",
		Message:       "Hello from singular name",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(result.Deliveries))
	}
	if recorder.calls[0].containerRef != "minecraft-server" {
		t.Fatalf("unexpected container target: %+v", recorder.calls)
	}
	if recorder.calls[0].command != "say \"Hello from singular name\"" {
		t.Fatalf("unexpected command: %q", recorder.calls[0].command)
	}
}

func TestBroadcastMessageOnlyUsesSay(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, []string{"alpha"}, nil, nil, nil, nil, nil, nil)

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Message: "Message only",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(result.Deliveries))
	}
	if recorder.calls[0].containerRef != "alpha" {
		t.Fatalf("unexpected container target: %+v", recorder.calls)
	}
	if recorder.calls[0].command != "say \"Message only\"" {
		t.Fatalf("unexpected command: %q", recorder.calls[0].command)
	}
}
