package service

import (
	"context"
	"testing"
)

type recordingMessenger struct {
	calls []recordingMessageCall
}

type recordingMessageCall struct {
	channelID string
	message   string
}

func (m *recordingMessenger) Open() error  { return nil }
func (m *recordingMessenger) Close() error { return nil }

func (m *recordingMessenger) SendMessage(channelID, message string) error {
	m.calls = append(m.calls, recordingMessageCall{channelID: channelID, message: message})
	return nil
}

func TestTriggerDryRunDoesNotSend(t *testing.T) {
	messenger := &recordingMessenger{}
	notifier := NewNotifier(messenger, []string{"chan-1", "chan-2"})

	result, err := notifier.Trigger(context.Background(), TriggerRequest{
		Message: "Server restart in 10 minutes",
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.DryRun {
		t.Fatalf("expected dry run result")
	}
	if result.Sent {
		t.Fatalf("expected sent=false for dry run")
	}
	if len(result.Delivered) != 2 {
		t.Fatalf("expected 2 delivered channels, got %d", len(result.Delivered))
	}
	if len(messenger.calls) != 0 {
		t.Fatalf("expected no discord sends, got %d", len(messenger.calls))
	}
}

func TestTriggerSendsMessages(t *testing.T) {
	messenger := &recordingMessenger{}
	notifier := NewNotifier(messenger, nil)

	result, err := notifier.Trigger(context.Background(), TriggerRequest{
		Message:    "Maintenance now",
		ChannelIDs: []string{"chan-1", "chan-2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Sent {
		t.Fatalf("expected sent=true")
	}
	if result.DryRun {
		t.Fatalf("expected dry run=false")
	}
	if len(result.Delivered) != 2 {
		t.Fatalf("expected 2 delivered channels, got %d", len(result.Delivered))
	}
	if len(messenger.calls) != 2 {
		t.Fatalf("expected 2 discord sends, got %d", len(messenger.calls))
	}
	if messenger.calls[0].channelID != "chan-1" || messenger.calls[1].channelID != "chan-2" {
		t.Fatalf("unexpected sent channels: %+v", messenger.calls)
	}
}
