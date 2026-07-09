package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"server-maintenance-notification-agent/internal/service"
)

func TestDiscordBroadcastDryRunQuery(t *testing.T) {
	messenger := &serviceTestMessenger{}
	notifier := service.NewNotifier(messenger, []string{"chan-default"})
	server := NewServer(notifier, nil)

	body, err := json.Marshal(service.TriggerRequest{Message: "test"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/discord/broadcast?dry_run=true", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.discordBroadcast(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	var result service.TriggerResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !result.DryRun || result.Sent {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Delivered) != 1 || result.Delivered[0] != "chan-default" {
		t.Fatalf("unexpected delivered channels: %+v", result.Delivered)
	}
	if len(messenger.calls) != 0 {
		t.Fatalf("expected no discord sends, got %d", len(messenger.calls))
	}
}

func TestConsoleCommandDryRunQuery(t *testing.T) {
	commander := service.NewDockerCommander(nil, nil, nil, nil, []string{"alpha"}, nil, nil, nil, nil)
	server := NewServer(nil, commander)

	body, err := json.Marshal(service.DockerCommandRequest{Command: "status"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/console/command?dry_run=true", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.dockerCommand(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	var result service.DockerCommandResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !result.DryRun || result.Sent {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Deliveries) != 1 || result.Deliveries[0].ContainerRef != "alpha" {
		t.Fatalf("unexpected deliveries: %+v", result.Deliveries)
	}
}

func TestConsoleCommandReturnsOutput(t *testing.T) {
	rcon := &testRCONExecutor{response: "done"}
	commander := service.NewDockerCommander(nil, rcon, nil, nil, nil, []string{"minecraft-server"}, []string{"rcon"}, []string{"127.0.0.1:27015"}, []string{"secret"})
	server := NewServer(nil, commander)

	body, err := json.Marshal(service.DockerCommandRequest{Command: "status"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/console/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.dockerCommand(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	var result service.DockerCommandResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !result.Sent || result.DryRun {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Deliveries) != 1 || result.Deliveries[0].Output != "done" {
		t.Fatalf("expected command output to be returned, got %+v", result.Deliveries)
	}
	if len(rcon.calls) != 1 || rcon.calls[0].command != "status" {
		t.Fatalf("unexpected rcon calls: %+v", rcon.calls)
	}
}

func TestSwaggerRouteServesDocs(t *testing.T) {
	server := NewServer(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	rr := httptest.NewRecorder()

	server.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if rr.Body.Len() == 0 {
		t.Fatalf("expected swagger response body")
	}
}

type serviceTestMessenger struct {
	calls []struct{ channelID, message string }
}

func (m *serviceTestMessenger) Open() error  { return nil }
func (m *serviceTestMessenger) Close() error { return nil }
func (m *serviceTestMessenger) SendMessage(channelID, message string) error {
	m.calls = append(m.calls, struct{ channelID, message string }{channelID: channelID, message: message})
	return nil
}

type testRCONExecutor struct {
	calls    []struct{ address, password, command string }
	response string
}

func (e *testRCONExecutor) Execute(_ context.Context, address, password, command string) (string, error) {
	e.calls = append(e.calls, struct{ address, password, command string }{address: address, password: password, command: command})
	return e.response, nil
}
