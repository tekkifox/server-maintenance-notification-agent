package service

type DockerCommandRequest struct {
	ContainerID    string       `json:"container_id"`
	ContainerIDs   []string     `json:"container_ids,omitempty"`
	ContainerName  string       `json:"container_name,omitempty"`
	ContainerNames []string     `json:"container_names,omitempty"`
	DryRun         bool         `json:"dry_run,omitempty"`
	Command        string       `json:"command"`
	RCON           *RCONRequest `json:"rcon,omitempty"`
}

type DockerCommandResult struct {
	Deliveries []BroadcastDelivery `json:"deliveries"`
	DryRun     bool                `json:"dry_run,omitempty"`
	Sent       bool                `json:"sent"`
}

type BroadcastTransport string

const (
	BroadcastTransportDocker BroadcastTransport = "docker"
	BroadcastTransportRCON   BroadcastTransport = "rcon"
	BroadcastTransportTelnet BroadcastTransport = "telnet"
)

type RCONRequest struct {
	Address  string `json:"address,omitempty"`
	Password string `json:"password,omitempty"`
}

type BroadcastRequest struct {
	Transport      BroadcastTransport `json:"transport,omitempty"`
	DryRun         bool               `json:"dry_run,omitempty"`
	ContainerID    string             `json:"container_id"`
	ContainerIDs   []string           `json:"container_ids,omitempty"`
	ContainerName  string             `json:"container_name,omitempty"`
	ContainerNames []string           `json:"container_names,omitempty"`
	Game           string             `json:"game,omitempty"`
	Message        string             `json:"message"`
	Command        string             `json:"command,omitempty"`
	RCON           *RCONRequest       `json:"rcon,omitempty"`
}

type BroadcastDelivery struct {
	Transport    BroadcastTransport `json:"transport"`
	ContainerRef string             `json:"container_ref"`
	Game         string             `json:"game,omitempty"`
	Command      string             `json:"command"`
	Output       string             `json:"output,omitempty"`
	Sent         bool               `json:"sent"`
}

type BroadcastResult struct {
	Deliveries []BroadcastDelivery `json:"deliveries"`
	DryRun     bool                `json:"dry_run,omitempty"`
	Sent       bool                `json:"sent"`
}

type BroadcastTarget struct {
	Ref          string
	Transport    BroadcastTransport
	Game         string
	RCONAddress  string
	RCONPassword string
}
