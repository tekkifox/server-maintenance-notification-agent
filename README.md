# Server Maintenance Notification Agent

Go service for sending Discord maintenance notifications and triggering remote console commands in game servers or containers.

It exposes a small HTTP API, loads configuration from environment variables or a local `.env` file, logs incoming requests, and supports Discord plus container console transports through Docker, RCON, and telnet.

## Features

- Send Discord messages to one or more channels.
- Trigger container commands directly through the Docker API.
- Broadcast maintenance messages through Docker, RCON, or telnet targets.
- Use per-game fallback rules when telnet delivery fails.
- Run locally with `.env` or in Docker/Portainer with `stack.env`.

## Requirements

- Go 1.24 or newer for local development.
- A Discord bot token.
- Docker access if you plan to use the container command or broadcast endpoints.

## Quick Start

1. Copy the example env file to `.env`.
2. Fill in the required values.
3. Start the service.

```bash
cp .env.example .env
go run ./cmd/notification-agent
```

The service listens on `HTTP_ADDR` and loads `.env` automatically at startup. Existing process environment variables take precedence over values from `.env`.

## Environment Variables

The app reads the variables below. List values can be comma-separated. Single-value aliases are also accepted for backward compatibility.

### Required

- `DISCORD_BOT_TOKEN`: Discord bot token used to open the bot session. The app fails to start without it.

### HTTP

- `HTTP_ADDR`: Address to bind the HTTP server to. Default: `:8080`.

### Discord Defaults

- `DISCORD_DEFAULT_CHANNEL_IDS`: Default Discord channel IDs to use when a request does not provide `channel_id` or `channel_ids`.
- `DISCORD_DEFAULT_CHANNEL_ID`: Single-value alias for `DISCORD_DEFAULT_CHANNEL_IDS`.

### Docker Broadcast Defaults

- `DOCKER_DEFAULT_CONTAINER_NAMES`: Default container names for Docker broadcast targets.
- `DOCKER_DEFAULT_CONTAINER_NAME`: Single-value alias for `DOCKER_DEFAULT_CONTAINER_NAMES`.
- `DOCKER_FALLBACK_GAME_TYPES`: Game names that are allowed to fall back from telnet to Docker broadcast delivery.
- `DOCKER_FALLBACK_GAME_TYPE`: Single-value alias for `DOCKER_FALLBACK_GAME_TYPES`.

### Pelican API

- `PELICAN_API_URL`: Base URL of the Pelican panel, for example `https://panel.example.com`.
- `PELICAN_API_TOKEN`: Pelican application API token used to look up server and egg metadata.
- `PELICAN_LOG_API_OUTPUT`: Set to `true` to log Pelican API requests and lookup output.
- When both values are set, the service resolves game names from Pelican before choosing a broadcast template.

### RCON Defaults

- `RCON_DEFAULT_CONTAINER_NAMES`: Default container names for RCON targets.
- `RCON_DEFAULT_CONTAINER_NAME`: Single-value alias for `RCON_DEFAULT_CONTAINER_NAMES`.
- `RCON_DEFAULT_CONTAINER_TRANSPORTS`: Transport type for each RCON target. Supported values: `rcon`, `telnet`, `docker`.
- `RCON_DEFAULT_CONTAINER_TRANSPORT`: Single-value alias for `RCON_DEFAULT_CONTAINER_TRANSPORTS`.
- `RCON_DEFAULT_CONTAINER_ADDRESSES`: RCON or telnet host:port values aligned by position with `RCON_DEFAULT_CONTAINER_NAMES`.
- `RCON_DEFAULT_CONTAINER_ADDRESS`: Single-value alias for `RCON_DEFAULT_CONTAINER_ADDRESSES`.
- `RCON_DEFAULT_CONTAINER_PASSWORDS`: Passwords aligned by position with `RCON_DEFAULT_CONTAINER_NAMES`.
- `RCON_DEFAULT_CONTAINER_PASSWORD`: Single-value alias for `RCON_DEFAULT_CONTAINER_PASSWORDS`.

RCON targets also resolve game names from Pelican egg metadata when `PELICAN_API_URL` and `PELICAN_API_TOKEN` are set.

### Docker Runtime

- `DOCKER_HOST`: Optional Docker daemon address used by the Docker client library. Examples: `unix:///var/run/docker.sock`, `tcp://host.docker.internal:2375`.

### Notes

- The sample `.env.example` also contains a few legacy placeholder keys such as `*_NAMES` and `*_DESCRIPTIVE_NAMES`. The current binary does not read those fields, so they can be left blank.
- `.env` parsing supports comments, quoted values, and `export KEY=value` lines.

## HTTP API

All responses are JSON.

Swagger UI is available at `/swagger/index.html` when the service is running.

### `GET /healthz`

Returns a basic health check.

```json
{ "status": "ok" }
```

### `POST /v1/discord/broadcast`

Sends a Discord message.

Request body:

```json
{
  "message": "Server restarting in 10 minutes",
  "channel_ids": ["123456789012345678", "234567890123456789"],
  "dry_run": true
}
```

Fields:

- `message`: Required message text.
- `channel_id`: Optional single channel ID.
- `channel_ids`: Optional list of channel IDs.
- `dry_run`: Optional boolean. When `true`, the service resolves the target channels but does not send messages.

If no channel is provided, the service uses `DISCORD_DEFAULT_CHANNEL_IDS`.

Response fields:

- `delivered`: The resolved channel IDs.
- `dry_run`: `true` when the request ran in dry-run mode.
- `sent`: `true` only when messages were actually sent.

Example:

```bash
curl -X POST 'http://localhost:8080/v1/discord/broadcast?dry_run=true' \
  -H 'Content-Type: application/json' \
  -d '{"message":"Server restarting in 10 minutes"}'
```

### `POST /v1/console/command`

Sends a raw command to one or more containers without game templating.

Request body:

```json
{
  "container_names": ["minecraft-server", "vrising-server"],
  "command": "status",
  "dry_run": true
}
```

Fields:

- `container_id`: Optional single container ID or name.
- `container_ids`: Optional list of container IDs.
- `command`: Required command to send.
- `container_name`: Optional single container name.
- `container_names`: Optional list of container names.
- `dry_run`: Optional boolean. When `true`, the service resolves targets but does not send the command.
- `rcon`: Optional override object with `address` and `password`.

Response fields:

- `deliveries`: Per-target delivery results, including `output` when the transport returns one.
- `dry_run`: `true` when the request ran in dry-run mode.
- `sent`: `true` only when the command was actually sent.

If no container target is provided, the service uses the configured default console and RCON targets.

Example:

```bash
curl -X POST 'http://localhost:8080/v1/console/command?dry_run=true' \
  -H 'Content-Type: application/json' \
  -d '{"container_names":["minecraft-server"],"command":"status"}'
```

### `POST /v1/message/broadcast`

Builds and sends a broadcast command for one or more containers.

Request body:

```json
{
  "container_names": ["minecraft-server", "vrising-server"],
  "game": "minecraft",
  "message": "Server restarting in 10 minutes",
  "transport": "docker"
}
```

Fields:

- `container_id`: Optional single target.
- `container_ids`: Optional list of targets.
- `container_name`: Optional single target alias.
- `container_names`: Optional list of target aliases.
- `game`: Optional game name used to choose a broadcast template.
- `message`: Required broadcast message.
- `command`: Optional command override. If omitted, the service generates a game-specific command.
- `transport`: Optional transport filter. Supported values: `docker`, `rcon`, `telnet`.
- `dry_run`: Optional boolean. When `true`, the service returns the commands it would send without sending them.
- `rcon`: Optional override object with `address` and `password`.

If `transport` is omitted, the service infers the transport from the configured target.

The `dry_run` query parameter is also supported, for example `?dry_run=true`.

Example:

```bash
curl -X POST 'http://localhost:8080/v1/message/broadcast?dry_run=true' \
  -H 'Content-Type: application/json' \
  -d '{"container_names":["minecraft-server"],"game":"minecraft","message":"Server restarting in 10 minutes"}'
```

The old `/v1/console/broadcast` path is still accepted as an alias.

## Docker

Build and run with Docker using the provided image or compose files.

For Portainer stacks, use `docker-compose.portainer.yml` or `docker-compose.portainer.socket-proxy.yml` and provide a `stack.env` file with the same values you would normally place in `.env`.

Example `stack.env`:

```env
HTTP_ADDR=:8080
DISCORD_BOT_TOKEN=your-discord-bot-token
DISCORD_DEFAULT_CHANNEL_IDS=123456789012345678,234567890123456789
PELICAN_API_URL=https://panel.example.com
PELICAN_API_TOKEN=your-pelican-api-token
DOCKER_DEFAULT_CONTAINER_NAMES=minecraft-server,vrising-server
DOCKER_FALLBACK_GAME_TYPES=minecraft,vrising,7dtd,terraria,unturned,starbound,satisfactory,palworld,rust,projectzomboid,factorio
RCON_DEFAULT_CONTAINER_NAMES=minecraft-server
RCON_DEFAULT_CONTAINER_TRANSPORTS=rcon
RCON_DEFAULT_CONTAINER_ADDRESSES=127.0.0.1:27015
RCON_DEFAULT_CONTAINER_PASSWORDS=change-me
```

## Validation

Run the test suite with:

```bash
go test ./...
```

## License

Add project licensing information here if needed.
