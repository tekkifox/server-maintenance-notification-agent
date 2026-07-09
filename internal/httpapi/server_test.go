package httpapi

import (
	"bytes"
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

type serviceTestMessenger struct {
	calls []struct{ channelID, message string }
}

func (m *serviceTestMessenger) Open() error  { return nil }
func (m *serviceTestMessenger) Close() error { return nil }
func (m *serviceTestMessenger) SendMessage(channelID, message string) error {
	m.calls = append(m.calls, struct{ channelID, message string }{channelID: channelID, message: message})
	return nil
}
