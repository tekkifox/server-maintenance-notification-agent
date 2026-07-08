package service

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"server-maintenance-notification-agent/internal/dockercontrol"
	"server-maintenance-notification-agent/internal/rconcontrol"
	"server-maintenance-notification-agent/internal/telnetcontrol"
)

type GameResolver interface {
	ResolveGame(ctx context.Context, containerRef string) (string, error)
}

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

var ignoredGameTokens = map[string]struct{}{
	"dedicated": {},
	"server":    {},
	"servers":   {},
	"headless":  {},
	"edition":   {},
	"editions":  {},
	"game":      {},
}

var broadcastTemplateKeys = sortedBroadcastTemplateKeys()
var broadcastGameAliases = buildBroadcastGameAliases()
var broadcastGameAliasKeys = sortedBroadcastGameAliasKeys()

type broadcastTemplateData struct {
	Message string
}

type DockerCommander struct {
	commander             dockercontrol.Commander
	rconExecutor          rconcontrol.Executor
	telnetExecutor        telnetcontrol.Executor
	gameResolver          GameResolver
	defaultConsoleTargets []BroadcastTarget
	defaultRCONTargets    []BroadcastTarget
	defaultConsoleByRef   map[string]BroadcastTarget
	defaultRCONByRef      map[string]BroadcastTarget
	dockerFallbackGames   map[string]struct{}
}

func NewDockerCommander(commander dockercontrol.Commander, rconExecutor rconcontrol.Executor, telnetExecutor telnetcontrol.Executor, defaultContainerIDs, defaultContainerNames, defaultRCONContainerNames, defaultRCONContainerGames, defaultRCONContainerTransports, defaultRCONContainerAddresses, defaultRCONContainerPasswords []string) *DockerCommander {
	consoleTargets := buildDefaultBroadcastTargets(defaultContainerIDs, defaultContainerNames)
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

func (d *DockerCommander) SetGameResolver(resolver GameResolver) {
	d.gameResolver = resolver
}

func (d *DockerCommander) SetDockerFallbackGameTypes(values []string) {
	d.dockerFallbackGames = buildGameAllowlist(values)
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
		if strings.TrimSpace(target.Game) == "" && d.gameResolver != nil {
			if resolvedGame, err := d.gameResolver.ResolveGame(ctx, target.Ref); err == nil {
				if strings.TrimSpace(resolvedGame) != "" {
					log.Printf("game match: pelican egg resolved ref=%s game=%q", target.Ref, resolvedGame)
					target.Game = resolvedGame
				}
			} else {
				log.Printf("pelican game lookup failed for ref=%s: %v", target.Ref, err)
			}
		}
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
				if !d.shouldFallbackToDocker(resolvedGame) {
					return BroadcastResult{}, fmt.Errorf("send telnet to %s: %w", target.Ref, err)
				}
				fallbackTarget, ok := d.defaultConsoleByRef[target.Ref]
				if !ok {
					return BroadcastResult{}, fmt.Errorf("send telnet to %s: %w", target.Ref, err)
				}
				if d.commander == nil {
					return BroadcastResult{}, fmt.Errorf("telnet to %s failed and docker control is not configured: %w", target.Ref, err)
				}
				log.Printf("broadcast fallback: telnet failed for ref=%s, retrying docker transport", target.Ref)
				if fallbackErr := d.commander.SendCommand(ctx, fallbackTarget.Ref, command); fallbackErr != nil {
					return BroadcastResult{}, fmt.Errorf("telnet to %s failed: %v; docker fallback to %s failed: %w", target.Ref, err, fallbackTarget.Ref, fallbackErr)
				}
				actualTransport = BroadcastTransportDocker
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
		log.Printf("game match: override provided game=%q override=%q", game, override)
		return override, strings.TrimSpace(game), nil
	}

	logGameMatching(game)
	tpl, ok, matchedKey, matchedBy := lookupBroadcastTemplate(game)
	if !ok {
		log.Printf("game match: no template match raw=%q using=default", game)
		tpl = broadcastTemplate{Game: strings.TrimSpace(game), Template: "say {{consoleMessage .Message}}"}
		if tpl.Game == "" {
			tpl.Game = "default"
		}
	} else {
		log.Printf("game match: matched raw=%q key=%q via=%s template_game=%q", game, matchedKey, matchedBy, tpl.Game)
	}

	command, err := executeBroadcastTemplate(tpl.Template, message)
	if err != nil {
		return "", "", err
	}

	return command, tpl.Game, nil
}

func buildDefaultBroadcastTargets(ids, names []string) []BroadcastTarget {
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

	for _, name := range normalizeContainerRefs(names...) {
		add(name, "")
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

func buildGameAllowlist(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	allowlist := make(map[string]struct{}, len(values))
	for _, value := range values {
		for _, key := range normalizeGameNameCandidates(value) {
			allowlist[key] = struct{}{}
		}
	}
	if len(allowlist) == 0 {
		return nil
	}
	return allowlist
}

func (d *DockerCommander) shouldFallbackToDocker(game string) bool {
	if len(d.dockerFallbackGames) == 0 {
		return true
	}
	for key := range d.dockerFallbackGames {
		if gameMatchesNormalizedKey(game, key) {
			return true
		}
	}
	return false
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

func normalizeGameNameCandidates(game string) []string {
	base := normalizeGameName(game)
	if base == "" {
		return nil
	}

	candidates := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}

	add(base)

	tokens := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(game)), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if _, ignored := ignoredGameTokens[token]; ignored {
			continue
		}
		filtered = append(filtered, token)
	}
	if len(filtered) > 0 {
		add(strings.Join(filtered, ""))
	}

	return candidates
}

func lookupBroadcastTemplate(game string) (broadcastTemplate, bool, string, string) {
	for _, key := range normalizeGameNameCandidates(game) {
		if tpl, ok := broadcastTemplates[key]; ok {
			return tpl, true, key, "exact"
		}
	}

	for _, candidate := range normalizeGameNameCandidates(game) {
		if key, alias, ok := matchBroadcastGameAlias(candidate); ok {
			return broadcastTemplates[key], true, key, "alias=" + alias
		}
	}

	for _, candidate := range normalizeGameNameCandidates(game) {
		if key, ok := matchBroadcastTemplateKey(candidate); ok {
			return broadcastTemplates[key], true, key, "contains"
		}
	}
	return broadcastTemplate{}, false, "", ""
}

func logGameMatching(game string) {
	candidates := normalizeGameNameCandidates(game)
	if len(candidates) == 0 {
		log.Printf("game match: raw=%q normalized=<none>", game)
		return
	}
	log.Printf("game match: raw=%q normalized=%v", game, candidates)
}

func gameMatchesNormalizedKey(game, key string) bool {
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return false
	}
	for _, candidate := range normalizeGameNameCandidates(game) {
		if candidate == key || strings.Contains(candidate, key) || strings.Contains(key, candidate) {
			return true
		}
	}
	return false
}

func matchBroadcastTemplateKey(candidate string) (string, bool) {
	for _, key := range broadcastTemplateKeys {
		if candidate == key || strings.Contains(candidate, key) || strings.Contains(key, candidate) {
			return key, true
		}
	}
	return "", false
}

func matchBroadcastGameAlias(candidate string) (string, string, bool) {
	for _, alias := range broadcastGameAliasKeys {
		if candidate == alias || strings.Contains(candidate, alias) || strings.Contains(alias, candidate) {
			return broadcastGameAliases[alias], alias, true
		}
	}
	return "", "", false
}

func sortedBroadcastTemplateKeys() []string {
	keys := make([]string, 0, len(broadcastTemplates))
	for key := range broadcastTemplates {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	return keys
}

func sortedBroadcastGameAliasKeys() []string {
	keys := make([]string, 0, len(broadcastGameAliases))
	for key := range broadcastGameAliases {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	return keys
}

func buildBroadcastGameAliases() map[string]string {
	aliases := map[string]string{
		"paper":         "minecraft",
		"papermc":       "minecraft",
		"purpur":        "minecraft",
		"spigot":        "minecraft",
		"bukkit":        "minecraft",
		"vanilla":       "minecraft",
		"forge":         "minecraft",
		"fabric":        "minecraft",
		"neoforge":      "minecraft",
		"papermcserver": "minecraft",
	}

	normalized := make(map[string]string, len(aliases))
	for alias, key := range aliases {
		normalized[normalizeGameName(alias)] = key
	}
	return normalized
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
