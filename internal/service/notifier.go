package service

import (
	"context"
	"fmt"
	"strings"

	"server-maintenance-notification-agent/internal/discord"
)

type Notifier struct {
	messenger         discord.Messenger
	defaultChannelIDs []string
}

func NewNotifier(messenger discord.Messenger, defaultChannelIDs []string) *Notifier {
	return &Notifier{messenger: messenger, defaultChannelIDs: normalizeChannelIDs(defaultChannelIDs...)}
}

type TriggerRequest struct {
	Message    string   `json:"message"`
	ChannelID  string   `json:"channel_id,omitempty"`
	ChannelIDs []string `json:"channel_ids,omitempty"`
}

type TriggerResult struct {
	Delivered []string `json:"delivered"`
}

func (n *Notifier) Trigger(ctx context.Context, req TriggerRequest) (TriggerResult, error) {
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return TriggerResult{}, fmt.Errorf("message is required")
	}

	channelIDs := normalizeChannelIDs(append([]string{req.ChannelID}, req.ChannelIDs...)...)
	if len(channelIDs) == 0 {
		if len(n.defaultChannelIDs) == 0 {
			return TriggerResult{}, fmt.Errorf("channel_id or channel_ids is required")
		}
		channelIDs = append(channelIDs, n.defaultChannelIDs...)
	}

	delivered := make([]string, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		if err := ctx.Err(); err != nil {
			return TriggerResult{}, err
		}
		if err := n.messenger.SendMessage(channelID, message); err != nil {
			return TriggerResult{}, fmt.Errorf("send to %s: %w", channelID, err)
		}
		delivered = append(delivered, channelID)
	}

	return TriggerResult{Delivered: delivered}, nil
}

func normalizeChannelIDs(values ...string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))

	add := func(channelID string) {
		channelID = strings.TrimSpace(channelID)
		if channelID == "" {
			return
		}
		if _, ok := seen[channelID]; ok {
			return
		}
		seen[channelID] = struct{}{}
		result = append(result, channelID)
	}

	for _, value := range values {
		for _, channelID := range strings.Split(value, ",") {
			add(channelID)
		}
	}

	return result
}
