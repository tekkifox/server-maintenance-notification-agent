package service

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"text/template"

	"server-maintenance-notification-agent/internal/dockercontrol"
	"server-maintenance-notification-agent/internal/rconcontrol"
	"server-maintenance-notification-agent/internal/telnetcontrol"
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
	telnetExecutor        telnetcontrol.Executor
	defaultConsoleTargets []BroadcastTarget
	defaultRCONTargets    []BroadcastTarget
	defaultConsoleByRef   map[string]BroadcastTarget
	defaultRCONByRef      map[string]BroadcastTarget
}

func NewDockerCommander(commander dockercontrol.Commander, rconExecutor rconcontrol.Executor, telnetExecutor telnetcontrol.Executor, defaultContainerIDs, defaultContainerNames, defaultContainerGames, defaultRCONContainerNames, defaultRCONContainerGames, defaultRCONContainerTransports, defaultRCONContainerAddresses, defaultRCONContainerPasswords []string) *DockerCommander {
	consoleTargets := buildDefaultBroadcastTargets(defaultContainerIDs, defaultContainerNames, defaultContainerGames)
	rconTargets := buildDefaultRCONTargets(defaultRCONContainerNames, defaultRCONContainerGames, defaultRCONContainerTransports, defaultRCONContainerAddresses, defaultRCONContainerPasswords)
	return &DockerCommander{
		commander:             commander,
		rconExecutor:          rconExecutor,
		telnetExecutor:        telnetExecutor,
		defaultConsoleTargets: consoleTargets,
		defaultRCONTargets:    rconTargets,
		defaultConsoleByRef:   buildBroadcastTargetLookup(consoleTargets),
		defaultRCONByRef:      buildBroadcastTargetLookup(rconTargets),
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
	requestedTransport, hasTransportFilter := parseRequestedTransport(req.Transport)
	targets, err := d.resolveBroadcastTargets(req)
	if err != nil {
		return BroadcastResult{}, err
	}
	if hasTransportFilter {
		targets = filterTargetsByTransport(targets, requestedTransport)
	}
	if len(targets) == 0 {
		return BroadcastResult{}, fmt.Errorf("container_id, container_ids, container_name, or container_names is required")
	}
	log.Printf("broadcast dispatch: targets=%d transport_filter=%s", len(targets), requestedTransport)

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
		targetTransport := normalizeTargetTransport(target.Transport)
		actualTransport := targetTransport
		log.Printf("broadcast target hit: transport=%s ref=%s game=%s", targetTransport, target.Ref, resolvedGame)
		if req.DryRun {
			deliveries = append(deliveries, BroadcastDelivery{
				Transport:    targetTransport,
				ContainerRef: target.Ref,
				Game:         resolvedGame,
				Command:      command,
				Sent:         false,
			})
			continue
		}
		if targetTransport == BroadcastTransportRCON {
			if d.rconExecutor == nil {
				return BroadcastResult{}, fmt.Errorf("rcon transport is not configured")
			}
			if target.RCONAddress == "" || target.RCONPassword == "" {
				return BroadcastResult{}, fmt.Errorf("rcon address and password are required for %s", target.Ref)
			}
			if _, err := d.rconExecutor.Execute(ctx, target.RCONAddress, target.RCONPassword, command); err != nil {
				return BroadcastResult{}, fmt.Errorf("send rcon to %s: %w", target.Ref, err)
			}
		} else if targetTransport == BroadcastTransportTelnet {
			if d.telnetExecutor == nil {
				return BroadcastResult{}, fmt.Errorf("telnet transport is not configured")
			}
			if target.RCONAddress == "" || target.RCONPassword == "" {
				return BroadcastResult{}, fmt.Errorf("telnet address and password are required for %s", target.Ref)
			}
			if err := d.telnetExecutor.Execute(ctx, target.RCONAddress, target.RCONPassword, command); err != nil {
				if fallbackTarget, ok := d.defaultConsoleByRef[target.Ref]; ok {
					if d.commander == nil {
						return BroadcastResult{}, fmt.Errorf("telnet to %s failed and docker control is not configured: %w", target.Ref, err)
					}
					log.Printf("broadcast fallback: telnet failed for ref=%s, retrying docker transport", target.Ref)
					if fallbackErr := d.commander.SendCommand(ctx, fallbackTarget.Ref, command); fallbackErr != nil {
						return BroadcastResult{}, fmt.Errorf("telnet to %s failed: %v; docker fallback to %s failed: %w", target.Ref, err, fallbackTarget.Ref, fallbackErr)
					}
					actualTransport = BroadcastTransportDocker
				} else {
					return BroadcastResult{}, fmt.Errorf("send telnet to %s: %w", target.Ref, err)
				}
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
			Transport:    actualTransport,
			ContainerRef: target.Ref,
			Game:         resolvedGame,
			Command:      command,
			Sent:         true,
		})
	}

	if req.DryRun {
		return BroadcastResult{Deliveries: deliveries, DryRun: true, Sent: false}, nil
	}

	return BroadcastResult{Deliveries: deliveries, Sent: true}, nil
}

func (d *DockerCommander) resolveBroadcastTargets(req BroadcastRequest) ([]BroadcastTarget, error) {
	refs := normalizeContainerRefs(append(append(append([]string{req.ContainerID}, req.ContainerIDs...), req.ContainerName), req.ContainerNames...)...)
	requestGame := strings.TrimSpace(req.Game)
	if len(refs) > 0 {
		return buildTargetsForRefs(refs, requestGame, req.RCON, d.defaultConsoleByRef, d.defaultRCONByRef), nil
	}

	targets := mergeDefaultTargetsPreferRCON(d.defaultConsoleTargets, d.defaultRCONTargets)
	if requestGame == "" {
		return targets, nil
	}
	for i := range targets {
		targets[i].Game = requestGame
	}
	return targets, nil
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
		targets = append(targets, BroadcastTarget{Ref: ref, Transport: BroadcastTransportDocker, Game: game})
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

func mergeDefaultTargetsPreferRCON(consoleTargets, rconTargets []BroadcastTarget) []BroadcastTarget {
	if len(consoleTargets) == 0 {
		return append([]BroadcastTarget(nil), rconTargets...)
	}
	if len(rconTargets) == 0 {
		return append([]BroadcastTarget(nil), consoleTargets...)
	}

	rconByRef := make(map[string]BroadcastTarget, len(rconTargets))
	for _, target := range rconTargets {
		rconByRef[target.Ref] = target
	}

	merged := make([]BroadcastTarget, 0, len(consoleTargets)+len(rconTargets))
	seen := make(map[string]struct{}, len(consoleTargets)+len(rconTargets))
	for _, target := range consoleTargets {
		if replacement, ok := rconByRef[target.Ref]; ok {
			merged = append(merged, replacement)
			seen[target.Ref] = struct{}{}
			continue
		}
		merged = append(merged, target)
		seen[target.Ref] = struct{}{}
	}

	for _, target := range rconTargets {
		if _, ok := seen[target.Ref]; ok {
			continue
		}
		merged = append(merged, target)
	}

	return merged
}

func buildDefaultRCONTargets(names, games, transports, addresses, passwords []string) []BroadcastTarget {
	targets := make([]BroadcastTarget, 0, len(names))
	seen := make(map[string]struct{})
	for i, name := range normalizeContainerRefs(names...) {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		target := BroadcastTarget{Ref: name, Transport: BroadcastTransportDocker}
		if i < len(games) {
			target.Game = strings.TrimSpace(games[i])
		}
		if i < len(transports) {
			target.Transport = normalizeConfiguredConnectionTransport(transports[i])
		}
		if target.Transport == "" {
			target.Transport = BroadcastTransportDocker
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

func buildBroadcastTargetLookup(targets []BroadcastTarget) map[string]BroadcastTarget {
	lookup := make(map[string]BroadcastTarget, len(targets))
	for _, target := range targets {
		lookup[target.Ref] = target
	}
	return lookup
}

func buildTargetsForRefs(refs []string, requestGame string, requestRCON *RCONRequest, consoleTargets, rconTargets map[string]BroadcastTarget) []BroadcastTarget {
	targets := make([]BroadcastTarget, 0, len(refs))
	for _, ref := range refs {
		target, ok := rconTargets[ref]
		if !ok {
			target, ok = consoleTargets[ref]
		}
		if !ok {
			target = BroadcastTarget{Ref: ref, Transport: BroadcastTransportDocker}
		}
		if requestGame != "" {
			target.Game = requestGame
		}
		if target.Transport == "" {
			target.Transport = BroadcastTransportDocker
		}
		if requestRCON != nil && (target.Transport == BroadcastTransportRCON || target.Transport == BroadcastTransportTelnet) {
			if strings.TrimSpace(requestRCON.Address) != "" {
				target.RCONAddress = strings.TrimSpace(requestRCON.Address)
			}
			if strings.TrimSpace(requestRCON.Password) != "" {
				target.RCONPassword = strings.TrimSpace(requestRCON.Password)
			}
		}
		targets = append(targets, target)
	}
	return targets
}

func buildRCONTargetLookup(targets []BroadcastTarget) map[string]BroadcastTarget {
	lookup := make(map[string]BroadcastTarget, len(targets))
	for _, target := range targets {
		lookup[target.Ref] = target
	}
	return lookup
}

func parseRequestedTransport(value BroadcastTransport) (BroadcastTransport, bool) {
	raw := strings.TrimSpace(string(value))
	if raw == "" {
		return BroadcastTransportDocker, false
	}
	return normalizeTargetTransport(BroadcastTransport(raw)), true
}

func normalizeConfiguredConnectionTransport(value string) BroadcastTransport {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(BroadcastTransportTelnet):
		return BroadcastTransportTelnet
	case string(BroadcastTransportRCON):
		return BroadcastTransportRCON
	case string(BroadcastTransportDocker), "":
		return BroadcastTransportDocker
	default:
		return BroadcastTransportDocker
	}
}

func normalizeTargetTransport(value BroadcastTransport) BroadcastTransport {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(BroadcastTransportRCON):
		return BroadcastTransportRCON
	case string(BroadcastTransportTelnet):
		return BroadcastTransportTelnet
	case string(BroadcastTransportDocker), "":
		return BroadcastTransportDocker
	default:
		return BroadcastTransportDocker
	}
}

func filterTargetsByTransport(targets []BroadcastTarget, transport BroadcastTransport) []BroadcastTarget {
	filtered := make([]BroadcastTarget, 0, len(targets))
	for _, target := range targets {
		if normalizeTargetTransport(target.Transport) == transport {
			filtered = append(filtered, target)
		}
	}
	return filtered
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
