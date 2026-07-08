package service

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"server-maintenance-notification-agent/internal/dockercontrol"
	"server-maintenance-notification-agent/internal/rconcontrol"
)

type broadcastTemplate struct {
	Game     string
	Template string
}

var broadcastTemplates = map[string]broadcastTemplate{
	"minecraft":      {Game: "minecraft", Template: "say {{consoleMessage .Message}}"},
	"unturned":       {Game: "unturned", Template: "say {{consoleMessage .Message}}"},
	"starbound":      {Game: "starbound", Template: "say {{consoleMessage .Message}}"},
	"satisfactory":   {Game: "satisfactory", Template: "Broadcast {{consoleMessage .Message}}"},
	"vrising":        {Game: "vrising", Template: "announce {{consoleMessage .Message}}"},
	"7dtd":           {Game: "7 days to die", Template: "say {{consoleMessage .Message}}"},
	"7daystodie":     {Game: "7 days to die", Template: "say {{consoleMessage .Message}}"},
	"palworld":       {Game: "palworld", Template: "Broadcast {{consoleMessage .Message}}"},
	"terraria":       {Game: "terraria", Template: "say {{consoleMessage .Message}}"},
	"rust":           {Game: "rust", Template: "say {{consoleMessage .Message}}"},
	"projectzomboid": {Game: "project zomboid", Template: "servermsg {{consoleMessage .Message}}"},
	"factorio":       {Game: "factorio", Template: "game.print({{quoteLua .Message}})"},
}

type broadcastTemplateData struct {
	Message string
}

type DockerCommander struct {
	commander             dockercontrol.Commander
	rconExecutor          rconcontrol.Executor
	defaultConsoleTargets []BroadcastTarget
	defaultRCONTargets    []BroadcastTarget
	defaultGameByRef      map[string]string
	defaultRCONByRef      map[string]BroadcastTarget
}

func NewDockerCommander(commander dockercontrol.Commander, rconExecutor rconcontrol.Executor, defaultContainerIDs, defaultContainerNames, defaultContainerGames, defaultRCONContainerNames, defaultRCONContainerGames, defaultRCONContainerAddresses, defaultRCONContainerPasswords []string) *DockerCommander {
	consoleTargets := buildDefaultBroadcastTargets(defaultContainerIDs, defaultContainerNames, defaultContainerGames)
	rconTargets := buildDefaultRCONTargets(defaultRCONContainerNames, defaultRCONContainerGames, defaultRCONContainerAddresses, defaultRCONContainerPasswords)
	return &DockerCommander{
		commander:             commander,
		rconExecutor:          rconExecutor,
		defaultConsoleTargets: consoleTargets,
		defaultRCONTargets:    rconTargets,
		defaultGameByRef:      buildBroadcastGameLookup(consoleTargets, rconTargets),
		defaultRCONByRef:      buildRCONTargetLookup(rconTargets),
	}
}

func (d *DockerCommander) Send(ctx context.Context, req DockerCommandRequest) (DockerCommandResult, error) {
	if d.commander == nil {
		return DockerCommandResult{}, fmt.Errorf("docker control is not configured")
	}
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
	transport := normalizeBroadcastTransport(req.Transport)
	targets, err := d.resolveBroadcastTargets(req, transport)
	if err != nil {
		return BroadcastResult{}, err
	}
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
		if transport == BroadcastTransportRCON {
			if d.rconExecutor == nil {
				return BroadcastResult{}, fmt.Errorf("rcon transport is not configured")
			}
			if target.RCONAddress == "" || target.RCONPassword == "" {
				return BroadcastResult{}, fmt.Errorf("rcon address and password are required for %s", target.Ref)
			}
			if _, err := d.rconExecutor.Execute(ctx, target.RCONAddress, target.RCONPassword, command); err != nil {
				return BroadcastResult{}, fmt.Errorf("send rcon to %s: %w", target.Ref, err)
			}
		} else {
			if d.commander == nil {
				return BroadcastResult{}, fmt.Errorf("docker control is not configured")
			}
			if err := d.commander.SendCommand(ctx, target.Ref, command); err != nil {
				return BroadcastResult{}, fmt.Errorf("send to %s: %w", target.Ref, err)
			}
		}
		deliveries = append(deliveries, BroadcastDelivery{
			Transport:    transport,
			ContainerRef: target.Ref,
			Game:         resolvedGame,
			Command:      command,
			Sent:         true,
		})
	}

	return BroadcastResult{Deliveries: deliveries, Sent: true}, nil
}

func (d *DockerCommander) resolveBroadcastTargets(req BroadcastRequest, transport BroadcastTransport) ([]BroadcastTarget, error) {
	refs := normalizeContainerRefs(append(append(append([]string{req.ContainerID}, req.ContainerIDs...), req.ContainerName), req.ContainerNames...)...)
	requestGame := strings.TrimSpace(req.Game)
	switch transport {
	case BroadcastTransportRCON:
		if len(refs) > 0 {
			return buildRCONTargetsFromRefs(refs, requestGame, req.RCON, d.defaultRCONByRef), nil
		}
		if len(d.defaultRCONTargets) == 0 {
			return nil, nil
		}
		if requestGame == "" {
			return append([]BroadcastTarget(nil), d.defaultRCONTargets...), nil
		}
		targets := make([]BroadcastTarget, 0, len(d.defaultRCONTargets))
		for _, target := range d.defaultRCONTargets {
			target.Game = requestGame
			targets = append(targets, target)
		}
		return targets, nil
	default:
		if len(refs) > 0 {
			return buildTargetsFromRefs(refs, requestGame, d.defaultGameByRef), nil
		}
		if len(d.defaultConsoleTargets) == 0 {
			return nil, nil
		}
		if requestGame == "" {
			return append([]BroadcastTarget(nil), d.defaultConsoleTargets...), nil
		}
		targets := make([]BroadcastTarget, 0, len(d.defaultConsoleTargets))
		for _, target := range d.defaultConsoleTargets {
			target.Game = requestGame
			targets = append(targets, target)
		}
		return targets, nil
	}
}

func buildBroadcastCommand(game, override, message string) (string, string, error) {
	override = strings.TrimSpace(override)
	if override != "" {
		return override, strings.TrimSpace(game), nil
	}

	key := normalizeGameName(game)
	tpl, ok := broadcastTemplates[key]
	if !ok {
		tpl = broadcastTemplate{Game: strings.TrimSpace(game), Template: "say {{consoleMessage .Message}}"}
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

func buildDefaultRCONTargets(names, games, addresses, passwords []string) []BroadcastTarget {
	targets := make([]BroadcastTarget, 0, len(names))
	seen := make(map[string]struct{})
	for i, name := range normalizeContainerRefs(names...) {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		target := BroadcastTarget{Ref: name}
		if i < len(games) {
			target.Game = strings.TrimSpace(games[i])
		}
		if i < len(addresses) {
			target.RCONAddress = strings.TrimSpace(addresses[i])
		}
		if i < len(passwords) {
			target.RCONPassword = strings.TrimSpace(passwords[i])
		}
		targets = append(targets, target)
	}
	return targets
}

func buildBroadcastGameLookup(consoleTargets, rconTargets []BroadcastTarget) map[string]string {
	lookup := make(map[string]string, len(consoleTargets)+len(rconTargets))
	for _, target := range consoleTargets {
		if target.Game == "" {
			continue
		}
		lookup[target.Ref] = target.Game
	}
	for _, target := range rconTargets {
		if target.Game == "" {
			continue
		}
		lookup[target.Ref] = target.Game
	}
	return lookup
}

func buildRCONTargetLookup(targets []BroadcastTarget) map[string]BroadcastTarget {
	lookup := make(map[string]BroadcastTarget, len(targets))
	for _, target := range targets {
		lookup[target.Ref] = target
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

func buildRCONTargetsFromRefs(refs []string, requestGame string, requestRCON *RCONRequest, defaultTargets map[string]BroadcastTarget) []BroadcastTarget {
	targets := make([]BroadcastTarget, 0, len(refs))
	for _, ref := range refs {
		target := BroadcastTarget{Ref: ref}
		if requestGame != "" {
			target.Game = requestGame
		} else if defaultTarget, ok := defaultTargets[ref]; ok {
			target.Game = defaultTarget.Game
		}
		if requestRCON != nil {
			target.RCONAddress = strings.TrimSpace(requestRCON.Address)
			target.RCONPassword = strings.TrimSpace(requestRCON.Password)
		} else if defaultTarget, ok := defaultTargets[ref]; ok {
			target.RCONAddress = strings.TrimSpace(defaultTarget.RCONAddress)
			target.RCONPassword = strings.TrimSpace(defaultTarget.RCONPassword)
		}
		targets = append(targets, target)
	}
	return targets
}

func normalizeBroadcastTransport(value BroadcastTransport) BroadcastTransport {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(BroadcastTransportRCON):
		return BroadcastTransportRCON
	default:
		return BroadcastTransportConsole
	}
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
		"consoleMessage": quoteConsoleMessage,
		"quoteLua":       quoteLuaString,
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

func quoteConsoleMessage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(strings.Fields(value)) <= 1 {
		return value
	}
	return strconv.Quote(value)
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
