package dockercontrol

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/moby/moby/client"
)

type Commander interface {
	SendCommand(ctx context.Context, containerID, command string) error
}

type Client struct {
	cli *client.Client
}

func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	return &Client{cli: cli}, nil
}

func (c *Client) Close() error {
	return c.cli.Close()
}

func (c *Client) SendCommand(ctx context.Context, containerID, command string) error {
	containerID = strings.TrimSpace(containerID)
	command = strings.TrimSpace(command)
	if containerID == "" {
		return fmt.Errorf("container_id is required")
	}
	if command == "" {
		return fmt.Errorf("command is required")
	}

	inspected, err := c.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect container %s: %w", containerID, err)
	}
	if inspected.Container.State == nil || !inspected.Container.State.Running {
		return fmt.Errorf("container %s is not running", containerID)
	}

	resp, err := c.cli.ContainerAttach(ctx, containerID, client.ContainerAttachOptions{
		Stream: true,
		Stdin:  true,
		Logs:   false,
	})
	if err != nil {
		return fmt.Errorf("attach container %s: %w", containerID, err)
	}
	defer resp.Close()

	if _, err := io.WriteString(resp.Conn, command+"\n"); err != nil {
		return fmt.Errorf("write command to %s: %w", containerID, err)
	}

	if closer, ok := resp.Conn.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
	}

	return nil
}
