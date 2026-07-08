package service

type DockerCommandRequest struct {
	ContainerID string `json:"container_id"`
	Command     string `json:"command"`
}

type DockerCommandResult struct {
	ContainerID string `json:"container_id"`
	Command     string `json:"command"`
	Sent        bool   `json:"sent"`
}

type BroadcastRequest struct {
	ContainerID    string   `json:"container_id"`
	ContainerIDs   []string `json:"container_ids,omitempty"`
	ContainerName  string   `json:"container_name,omitempty"`
	ContainerNames []string `json:"container_names,omitempty"`
	Game           string   `json:"game,omitempty"`
	Message        string   `json:"message"`
	Command        string   `json:"command,omitempty"`
}

type BroadcastDelivery struct {
	ContainerRef string `json:"container_ref"`
	Game         string `json:"game,omitempty"`
	Command      string `json:"command"`
	Sent         bool   `json:"sent"`
}

type BroadcastResult struct {
	Deliveries []BroadcastDelivery `json:"deliveries"`
	Sent       bool                `json:"sent"`
}

type BroadcastTarget struct {
	Ref  string
	Game string
}
