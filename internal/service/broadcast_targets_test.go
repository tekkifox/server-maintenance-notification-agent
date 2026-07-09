package service

import (
	"context"
	"testing"

	"server-maintenance-notification-agent/internal/config"
)

type recordingCommander struct {
	calls []recordingCall
}

type recordingRCONExecutor struct {
	calls []recordingRCONCall
}

type recordingTelnetExecutor struct {
	calls []recordingTelnetCall
}

type failingTelnetExecutor struct {
	calls []recordingTelnetCall
	err   error
}

type staticGameResolver map[string]string

type recordingRCONCall struct {
	address  string
	password string
	command  string
}

type recordingTelnetCall struct {
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

func (r *recordingTelnetExecutor) Execute(_ context.Context, address, password, command string) error {
	r.calls = append(r.calls, recordingTelnetCall{address: address, password: password, command: command})
	return nil
}

func (r *failingTelnetExecutor) Execute(_ context.Context, address, password, command string) error {
	r.calls = append(r.calls, recordingTelnetCall{address: address, password: password, command: command})
	if r.err != nil {
		return r.err
	}
	return context.DeadlineExceeded
}

func (r staticGameResolver) ResolveGame(_ context.Context, containerRef string) (string, error) {
	if value, ok := r[containerRef]; ok {
		return value, nil
	}
	return "", nil
}

func TestBroadcastUsesDefaultContainerIDs(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, nil, []string{"alpha", "beta"}, nil, nil, nil, nil, nil)

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

func TestCommandUsesDefaultContainerNames(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, nil, nil, []string{"alpha", "beta"}, nil, nil, nil, nil)

	result, err := commander.Send(context.Background(), DockerCommandRequest{
		Command: "status",
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
	if recorder.calls[0].command != "status" || recorder.calls[1].command != "status" {
		t.Fatalf("unexpected commands: %+v", recorder.calls)
	}
}

func TestCommandDryRunDoesNotSend(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, nil, nil, []string{"alpha"}, nil, nil, nil, nil)

	result, err := commander.Send(context.Background(), DockerCommandRequest{
		DryRun:  true,
		Command: "status",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.DryRun {
		t.Fatalf("expected dry run result")
	}
	if result.Sent {
		t.Fatalf("expected sent=false on dry run")
	}
	if len(result.Deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(result.Deliveries))
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("expected no docker calls, got %d", len(recorder.calls))
	}
	if result.Deliveries[0].Command != "status" || result.Deliveries[0].Sent {
		t.Fatalf("unexpected dry-run delivery: %+v", result.Deliveries[0])
	}
}

func TestBroadcastDryRunDoesNotSend(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, nil, []string{"alpha"}, nil, nil, nil, nil, nil)

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		DryRun:  true,
		Game:    "Minecraft",
		Message: "Restart in 10 minutes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.DryRun {
		t.Fatalf("expected dry run result")
	}
	if result.Sent {
		t.Fatalf("expected sent=false on dry run")
	}
	if len(result.Deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(result.Deliveries))
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("expected no docker calls, got %d", len(recorder.calls))
	}
	if result.Deliveries[0].Sent {
		t.Fatalf("expected dry-run delivery to be marked unsent")
	}
}

func TestBroadcastUsesRCONTransport(t *testing.T) {
	console := &recordingCommander{}
	rcon := &recordingRCONExecutor{}
	commander := NewDockerCommander(console, rcon, nil, nil, []string{"minecraft-server"}, []string{"minecraft-server"}, []string{"rcon"}, []string{"127.0.0.1:27015"}, []string{"secret"})

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

func TestCommandUsesRCONTransport(t *testing.T) {
	console := &recordingCommander{}
	rcon := &recordingRCONExecutor{}
	commander := NewDockerCommander(console, rcon, nil, nil, nil, []string{"minecraft-server"}, []string{"rcon"}, []string{"127.0.0.1:27015"}, []string{"secret"})

	result, err := commander.Send(context.Background(), DockerCommandRequest{
		Command: "status",
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
	if rcon.calls[0].command != "status" {
		t.Fatalf("unexpected rcon command: %+v", rcon.calls[0])
	}
	if result.Deliveries[0].Output != "ok" {
		t.Fatalf("expected rcon output to be returned, got %+v", result.Deliveries[0])
	}
}

func TestBroadcastUsesDockerTransportWhenConfigured(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, nil, nil, nil, []string{"docker-server"}, []string{"docker"}, nil, nil)

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Message: "Server restart in 10 minutes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(result.Deliveries))
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("expected 1 docker call, got %d", len(recorder.calls))
	}
	if recorder.calls[0].containerRef != "docker-server" {
		t.Fatalf("unexpected docker target: %+v", recorder.calls[0])
	}
	if result.Deliveries[0].Transport != BroadcastTransportDocker {
		t.Fatalf("expected docker delivery, got %+v", result.Deliveries[0])
	}
	if recorder.calls[0].command != "say \"Server restart in 10 minutes\"" {
		t.Fatalf("unexpected docker command: %+v", recorder.calls[0])
	}
}

func TestBroadcastPrefersRCONWhenNamesOverlap(t *testing.T) {
	console := &recordingCommander{}
	rcon := &recordingRCONExecutor{}
	commander := NewDockerCommander(console, rcon, nil, nil, []string{"shared-server"}, []string{"shared-server"}, []string{"rcon"}, []string{"127.0.0.1:27015"}, []string{"secret"})

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Message: "Restart in 10 minutes",
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
	if result.Deliveries[0].Transport != BroadcastTransportRCON {
		t.Fatalf("expected rcon delivery, got %+v", result.Deliveries[0])
	}
}

func TestBroadcastUsesMixedDefaultTransports(t *testing.T) {
	console := &recordingCommander{}
	rcon := &recordingRCONExecutor{}
	telnet := &recordingTelnetExecutor{}
	commander := NewDockerCommander(console, rcon, telnet, nil, nil, []string{"vrising-rcon", "vrising-telnet"}, []string{"rcon", "telnet"}, []string{"127.0.0.1:27015", "127.0.0.1:9876"}, []string{"secret", "telnet-pass"})

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Message: "Server restart in 10 minutes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Deliveries) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(result.Deliveries))
	}
	if len(console.calls) != 0 {
		t.Fatalf("expected no console calls, got %d", len(console.calls))
	}
	if len(rcon.calls) != 1 {
		t.Fatalf("expected 1 rcon call, got %d", len(rcon.calls))
	}
	if len(telnet.calls) != 1 {
		t.Fatalf("expected 1 telnet call, got %d", len(telnet.calls))
	}
	if rcon.calls[0].address != "127.0.0.1:27015" || rcon.calls[0].password != "secret" {
		t.Fatalf("unexpected rcon connection details: %+v", rcon.calls[0])
	}
	if telnet.calls[0].address != "127.0.0.1:9876" || telnet.calls[0].password != "telnet-pass" {
		t.Fatalf("unexpected telnet connection details: %+v", telnet.calls[0])
	}
	if result.Deliveries[0].Transport != BroadcastTransportRCON && result.Deliveries[1].Transport != BroadcastTransportRCON {
		t.Fatalf("expected an rcon delivery, got %+v", result.Deliveries)
	}
	if result.Deliveries[0].Transport != BroadcastTransportTelnet && result.Deliveries[1].Transport != BroadcastTransportTelnet {
		t.Fatalf("expected a telnet delivery, got %+v", result.Deliveries)
	}
	if result.Deliveries[0].Transport == result.Deliveries[1].Transport {
		t.Fatalf("expected mixed transports, got %+v", result.Deliveries)
	}
}

func TestBroadcastFiltersByExplicitTransport(t *testing.T) {
	console := &recordingCommander{}
	rcon := &recordingRCONExecutor{}
	telnet := &recordingTelnetExecutor{}
	commander := NewDockerCommander(console, rcon, telnet, nil, nil, []string{"vrising-rcon", "vrising-telnet"}, []string{"rcon", "telnet"}, []string{"127.0.0.1:27015", "127.0.0.1:9876"}, []string{"secret", "telnet-pass"})

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Transport: BroadcastTransportRCON,
		Message:   "Server restart in 10 minutes",
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
	if len(telnet.calls) != 0 {
		t.Fatalf("expected no telnet calls, got %d", len(telnet.calls))
	}
	if result.Deliveries[0].Transport != BroadcastTransportRCON {
		t.Fatalf("expected rcon delivery, got %+v", result.Deliveries)
	}
}

func TestBroadcastPrefersExplicitContainerIDs(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, nil, []string{"alpha", "beta"}, nil, nil, nil, nil, nil)

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

func TestBroadcastUsesDefaultContainerNames(t *testing.T) {
	recorder := &recordingCommander{}
	commander := NewDockerCommander(recorder, nil, nil, nil, []string{"minecraft-server", "vrising-server"}, nil, nil, nil, nil)
	commander.SetGameResolver(staticGameResolver{
		"minecraft-server": "minecraft",
		"vrising-server":   "vrising",
	})

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
	commander := NewDockerCommander(recorder, nil, nil, nil, []string{"minecraft-server"}, nil, nil, nil, nil)

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
	commander := NewDockerCommander(recorder, nil, nil, nil, []string{"minecraft-server"}, nil, nil, nil, nil)

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
	commander := NewDockerCommander(recorder, nil, nil, []string{"alpha"}, nil, nil, nil, nil, nil)

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

func TestBroadcastUsesTelnetTransport(t *testing.T) {
	console := &recordingCommander{}
	telnet := &recordingTelnetExecutor{}
	commander := NewDockerCommander(console, nil, telnet, nil, []string{"vrising-server"}, []string{"vrising-server"}, []string{"telnet"}, []string{"127.0.0.1:9876"}, []string{"telnet-pass"})
	commander.SetGameResolver(staticGameResolver{"vrising-server": "vrising"})

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Transport: BroadcastTransportTelnet,
		Message:   "Server restart in 10 minutes",
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
	if len(telnet.calls) != 1 {
		t.Fatalf("expected 1 telnet call, got %d", len(telnet.calls))
	}
	if telnet.calls[0].address != "127.0.0.1:9876" || telnet.calls[0].password != "telnet-pass" {
		t.Fatalf("unexpected telnet connection details: %+v", telnet.calls[0])
	}
	if telnet.calls[0].command != "announce \"Server restart in 10 minutes\"" {
		t.Fatalf("unexpected telnet command: %+v", telnet.calls[0])
	}
}

func TestBroadcastFallsBackToDockerAfterTelnetFailure(t *testing.T) {
	console := &recordingCommander{}
	telnet := &failingTelnetExecutor{err: context.DeadlineExceeded}
	commander := NewDockerCommander(console, nil, telnet, nil, []string{"shared-server"}, []string{"shared-server"}, []string{"telnet"}, []string{"127.0.0.1:9876"}, []string{"telnet-pass"})
	commander.SetGameResolver(staticGameResolver{"shared-server": "minecraft"})

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Message: "Server restart in 10 minutes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(result.Deliveries))
	}
	if len(telnet.calls) != 1 {
		t.Fatalf("expected 1 telnet call, got %d", len(telnet.calls))
	}
	if len(console.calls) != 1 {
		t.Fatalf("expected 1 docker fallback call, got %d", len(console.calls))
	}
	if console.calls[0].containerRef != "shared-server" {
		t.Fatalf("unexpected docker fallback target: %+v", console.calls[0])
	}
	if result.Deliveries[0].Transport != BroadcastTransportDocker {
		t.Fatalf("expected docker delivery after fallback, got %+v", result.Deliveries[0])
	}
}

func TestBroadcastDoesNotFallbackWhenGameNotAllowed(t *testing.T) {
	console := &recordingCommander{}
	telnet := &failingTelnetExecutor{err: context.DeadlineExceeded}
	commander := NewDockerCommander(console, nil, telnet, nil, []string{"shared-server"}, []string{"shared-server"}, []string{"telnet"}, []string{"127.0.0.1:9876"}, []string{"telnet-pass"})
	commander.SetGameResolver(staticGameResolver{"shared-server": "vrising"})
	commander.SetDockerFallbackGameTypes([]string{"minecraft"})

	_, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Message: "Restart in 10 minutes",
	})
	if err == nil {
		t.Fatalf("expected telnet error when fallback is disallowed")
	}
	if len(telnet.calls) != 1 {
		t.Fatalf("expected 1 telnet call, got %d", len(telnet.calls))
	}
	if len(console.calls) != 0 {
		t.Fatalf("expected no docker fallback call, got %d", len(console.calls))
	}
}

func TestBroadcastUsesDockerFallbackGameTypesFromEnv(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "test-token")
	t.Setenv("DOCKER_FALLBACK_GAME_TYPES", "minecraft")

	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	console := &recordingCommander{}
	telnet := &failingTelnetExecutor{err: context.DeadlineExceeded}
	commander := NewDockerCommander(console, nil, telnet, nil, []string{"shared-server"}, []string{"shared-server"}, []string{"telnet"}, []string{"127.0.0.1:9876"}, []string{"telnet-pass"})
	commander.SetGameResolver(staticGameResolver{"shared-server": "minecraft"})
	commander.SetDockerFallbackGameTypes(cfg.DefaultDockerFallbackGameTypes)

	result, err := commander.Broadcast(context.Background(), BroadcastRequest{
		Game:    "Minecraft Dedicated Server",
		Message: "Restart in 10 minutes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(result.Deliveries))
	}
	if len(telnet.calls) != 1 {
		t.Fatalf("expected 1 telnet call, got %d", len(telnet.calls))
	}
	if len(console.calls) != 1 {
		t.Fatalf("expected 1 docker fallback call, got %d", len(console.calls))
	}
	if result.Deliveries[0].Transport != BroadcastTransportDocker {
		t.Fatalf("expected docker delivery after env-configured fallback, got %+v", result.Deliveries[0])
	}
}
