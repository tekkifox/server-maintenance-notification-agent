package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"server-maintenance-notification-agent/internal/dockercontrol"
)

type broadcastTemplate struct {
	Game     string
	Template string
}

var broadcastTemplates = map[string]broadcastTemplate{
	"minecraft":      {Game: "minecraft", Template: "say {{.Message}}"},
	"vrising":        {Game: "vrising", Template: "announce {{.Message}}"},
	"7dtd":           {Game: "7 days to die", Template: "say {{.Message}}"},
	"7daystodie":     {Game: "7 days to die", Template: "say {{.Message}}"},
	"palworld":       {Game: "palworld", Template: "Broadcast {{.Message}}"},
	"terraria":       {Game: "terraria", Template: "say {{.Message}}"},
	"rust":           {Game: "rust", Template: "say {{.Message}}"},
	"projectzomboid": {Game: "project zomboid", Template: "servermsg {{.Message}}"},
	"factorio":       {Game: "factorio", Template: "game.print({{quoteLua .Message}})"},
}

type broadcastTemplateData struct {
	Message string
}

type DockerCommander struct {
	commander        dockercontrol.Commander
	defaultTargets   []BroadcastTarget
	defaultGameByRef map[string]string
}

func NewDockerCommander(commander dockercontrol.Commander, defaultContainerIDs, defaultContainerNames, defaultContainerGames []string) *DockerCommander {
	targets := buildDefaultBroadcastTargets(defaultContainerIDs, defaultContainerNames, defaultContainerGames)
	return &DockerCommander{
		commander:        commander,
		defaultTargets:   targets,
		defaultGameByRef: buildBroadcastGameLookup(targets),
	}
}

func (d *DockerCommander) Send(ctx context.Context, req DockerCommandRequest) (DockerCommandResult, error) {
	containerID := strings.TrimSpace(req.ContainerID)
	command := strings.TrimSpace(req.Command)
	if containerID == "" {
		return DockerCommandResult{}, fmt.Errorf("container_id is required")
	}
	if command == "" {
		return DockerCommandResult{}, fmt.Errorf("command is required")
	}

	if err := d.commander.SendCommand(ctx, containerID, command); err != nil {
		return DockerCommandResult{}, err
	}

	return DockerCommandResult{ContainerID: containerID, Command: command, Sent: true}, nil
}

func (d *DockerCommander) Broadcast(ctx context.Context, req BroadcastRequest) (BroadcastResult, error) {
	targets := d.resolveBroadcastTargets(req)
	if len(targets) == 0 {
		return BroadcastResult{}, fmt.Errorf("container_id, container_ids, container_name, or container_names is required")
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		return BroadcastResult{}, fmt.Errorf("message is required")
	}

	deliveries := make([]BroadcastDelivery, 0, len(targets))
	for _, target := range targets {
		command, resolvedGame, err := buildBroadcastCommand(target.Game, req.Command, message)
		if err != nil {
			return BroadcastResult{}, err
		}
		if err := d.commander.SendCommand(ctx, target.Ref, command); err != nil {
			return BroadcastResult{}, fmt.Errorf("send to %s: %w", target.Ref, err)
		}
		deliveries = append(deliveries, BroadcastDelivery{
			ContainerRef: target.Ref,
			Game:         resolvedGame,
			Command:      command,
			Sent:         true,
		})
	}

	return BroadcastResult{Deliveries: deliveries, Sent: true}, nil
}

func (d *DockerCommander) resolveBroadcastTargets(req BroadcastRequest) []BroadcastTarget {
	refs := normalizeContainerRefs(append(append(append([]string{req.ContainerID}, req.ContainerIDs...), req.ContainerName), req.ContainerNames...)...)
	requestGame := strings.TrimSpace(req.Game)
	if len(refs) > 0 {
		return buildTargetsFromRefs(refs, requestGame, d.defaultGameByRef)
	}
	if len(d.defaultTargets) == 0 {
		return nil
	}
	if requestGame == "" {
		return append([]BroadcastTarget(nil), d.defaultTargets...)
	}
	targets := make([]BroadcastTarget, 0, len(d.defaultTargets))
	for _, target := range d.defaultTargets {
		targets = append(targets, BroadcastTarget{Ref: target.Ref, Game: requestGame})
	}
	return targets
}

func buildBroadcastCommand(game, override, message string) (string, string, error) {
	override = strings.TrimSpace(override)
	if override != "" {
		return override, strings.TrimSpace(game), nil
	}

	key := normalizeGameName(game)
	tpl, ok := broadcastTemplates[key]
	if !ok {
		tpl = broadcastTemplate{Game: strings.TrimSpace(game), Template: "say {{.Message}}"}
		if tpl.Game == "" {
			tpl.Game = "default"
		}
	}

	command, err := executeBroadcastTemplate(tpl.Template, message)
	if err != nil {
		return "", "", err
	}

	return command, tpl.Game, nil
}

func buildDefaultBroadcastTargets(ids, names, games []string) []BroadcastTarget {
	targets := make([]BroadcastTarget, 0, len(ids)+len(names))
	seen := make(map[string]struct{})

	add := func(ref, game string) {
		ref = strings.TrimSpace(ref)
		game = strings.TrimSpace(game)
		if ref == "" {
			return
		}
		if _, ok := seen[ref]; ok {
			return
		}
		seen[ref] = struct{}{}
		targets = append(targets, BroadcastTarget{Ref: ref, Game: game})
	}

	for _, id := range normalizeContainerRefs(ids...) {
		add(id, "")
	}

	for i, name := range normalizeContainerRefs(names...) {
		game := ""
		if i < len(games) {
			game = strings.TrimSpace(games[i])
		}
		add(name, game)
	}

	return targets
}

func buildBroadcastGameLookup(targets []BroadcastTarget) map[string]string {
	lookup := make(map[string]string, len(targets))
	for _, target := range targets {
		if target.Game == "" {
			continue
		}
		lookup[target.Ref] = target.Game
	}
	return lookup
}

func buildTargetsFromRefs(refs []string, requestGame string, defaultGames map[string]string) []BroadcastTarget {
	targets := make([]BroadcastTarget, 0, len(refs))
	for _, ref := range refs {
		game := requestGame
		if game == "" {
			game = defaultGames[ref]
		}
		targets = append(targets, BroadcastTarget{Ref: ref, Game: game})
	}
	return targets
}

func normalizeContainerRefs(values ...string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))

	add := func(raw string) {
		for _, part := range strings.Split(raw, ",") {
			id := strings.TrimSpace(part)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}

	for _, value := range values {
		add(value)
	}

	return result
}

func executeBroadcastTemplate(raw string, message string) (string, error) {
	tpl, err := template.New("broadcast").Funcs(template.FuncMap{
		"quoteLua": quoteLuaString,
	}).Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse broadcast template: %w", err)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, broadcastTemplateData{Message: message}); err != nil {
		return "", fmt.Errorf("render broadcast template: %w", err)
	}

	return buf.String(), nil
}

func normalizeGameName(game string) string {
	game = strings.ToLower(strings.TrimSpace(game))
	if game == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(game))
	for _, r := range game {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func quoteLuaString(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
