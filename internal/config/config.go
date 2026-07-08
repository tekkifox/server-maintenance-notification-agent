package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr                       string
	DiscordBotToken                string
	PelicanAPIURL                  string
	PelicanAPIToken                string
	DefaultChannelIDs              []string
	DefaultDockerContainerIDs      []string
	DefaultDockerContainerNames    []string
	DefaultDockerFallbackGameTypes []string
	DefaultRCONContainerNames      []string
	DefaultRCONContainerTransports []string
	DefaultRCONContainerAddresses  []string
	DefaultRCONContainerPasswords  []string
}

func LoadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		value = strings.TrimSpace(value)
		value = trimQuotes(value)
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

func FromEnv() (Config, error) {
	cfg := Config{
		HTTPAddr:                       envOrDefault("HTTP_ADDR", ":8080"),
		DiscordBotToken:                strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")),
		PelicanAPIURL:                  strings.TrimSpace(os.Getenv("PELICAN_API_URL")),
		PelicanAPIToken:                strings.TrimSpace(os.Getenv("PELICAN_API_TOKEN")),
		DefaultChannelIDs:              parseDelimitedList(os.Getenv("DISCORD_DEFAULT_CHANNEL_IDS"), os.Getenv("DISCORD_DEFAULT_CHANNEL_ID")),
		DefaultDockerContainerNames:    parseDelimitedList(os.Getenv("DOCKER_DEFAULT_CONTAINER_NAMES"), os.Getenv("DOCKER_DEFAULT_CONTAINER_NAME")),
		DefaultDockerFallbackGameTypes: parseDelimitedList(os.Getenv("DOCKER_FALLBACK_GAME_TYPES"), os.Getenv("DOCKER_FALLBACK_GAME_TYPE")),
		DefaultRCONContainerNames:      parseDelimitedList(os.Getenv("RCON_DEFAULT_CONTAINER_NAMES"), os.Getenv("RCON_DEFAULT_CONTAINER_NAME")),
		DefaultRCONContainerTransports: parseDelimitedList(os.Getenv("RCON_DEFAULT_CONTAINER_TRANSPORTS"), os.Getenv("RCON_DEFAULT_CONTAINER_TRANSPORT")),
		DefaultRCONContainerAddresses:  parseDelimitedList(os.Getenv("RCON_DEFAULT_CONTAINER_ADDRESSES"), os.Getenv("RCON_DEFAULT_CONTAINER_ADDRESS")),
		DefaultRCONContainerPasswords:  parseDelimitedList(os.Getenv("RCON_DEFAULT_CONTAINER_PASSWORDS"), os.Getenv("RCON_DEFAULT_CONTAINER_PASSWORD")),
	}

	if cfg.DiscordBotToken == "" {
		return Config{}, fmt.Errorf("DISCORD_BOT_TOKEN is required")
	}

	return cfg, nil
}

func parseDelimitedList(values ...string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))

	add := func(raw string) {
		for _, part := range strings.Split(raw, ",") {
			channelID := strings.TrimSpace(part)
			if channelID == "" {
				continue
			}
			if _, ok := seen[channelID]; ok {
				continue
			}
			seen[channelID] = struct{}{}
			result = append(result, channelID)
		}
	}

	for _, raw := range values {
		add(raw)
	}

	return result
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func trimQuotes(value string) string {
	if len(value) < 2 {
		return value
	}

	if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
		(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
		return value[1 : len(value)-1]
	}

	return value
}
