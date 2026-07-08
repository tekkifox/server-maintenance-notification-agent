package service

import (
	"context"
	"testing"
)

type recordingCommander struct {
	calls []recordingCall
}

type recordingCall struct {
	containerRef string
	command      string
}

func (r *recordingCommander) SendCommand(_ context.Context, containerID, command string) error {
	r.calls = append(r.calls, recordingCall{containerRef: containerID, command: command})
	return nil
}

func TestBroadcastUsesDefaultContainerIDs(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, []string{"alpha", "beta"}, nil, nil)

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
	if recorder.calls[0].command != "say Restart in 10 minutes" || recorder.calls[1].command != "say Restart in 10 minutes" {
		t.Fatalf("unexpected command broadcast: %+v", recorder.calls)
	}
}

func TestBroadcastPrefersExplicitContainerIDs(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, []string{"alpha", "beta"}, nil, nil)

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
	commander := NewDockerCommander(recorder, nil, []string{"minecraft-server", "vrising-server"}, []string{"minecraft", "vrising"})

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
	if recorder.calls[0].command != "say Maintenance starts in 10 minutes" {
		t.Fatalf("unexpected minecraft command: %q", recorder.calls[0].command)
	}
	if recorder.calls[1].command != "announce Maintenance starts in 10 minutes" {
		t.Fatalf("unexpected vrising command: %q", recorder.calls[1].command)
	}
}

func TestBroadcastUsesExplicitContainerNames(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, []string{"minecraft-server"}, []string{"minecraft"})

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
	if recorder.calls[0].command != "say Hello from the API" {
		t.Fatalf("unexpected minecraft command: %q", recorder.calls[0].command)
	}
	if recorder.calls[1].command != "say Hello from the API" {
		t.Fatalf("unexpected fallback command: %q", recorder.calls[1].command)
	}
}

func TestBroadcastUsesSingleContainerName(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, []string{"minecraft-server"}, []string{"minecraft"})

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
	if recorder.calls[0].command != "say Hello from singular name" {
		t.Fatalf("unexpected command: %q", recorder.calls[0].command)
	}
}

func TestBroadcastMessageOnlyUsesSay(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, []string{"alpha"}, nil, nil)

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
	if recorder.calls[0].command != "say Message only" {
		t.Fatalf("unexpected command: %q", recorder.calls[0].command)
	}
}
