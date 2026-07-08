package rconcontrol

import (
	"context"
	"fmt"
	"time"

	"github.com/gorcon/rcon"
)

type Executor interface {
	Execute(ctx context.Context, address, password, command string) (string, error)
}

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Execute(ctx context.Context, address, password, command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	conn, err := rcon.Dial(address, password, rcon.SetDialTimeout(5*time.Second), rcon.SetDeadline(5*time.Second))
	if err != nil {
		return "", fmt.Errorf("dial rcon %s: %w", address, err)
	}
	defer conn.Close()

	if err := ctx.Err(); err != nil {
		return "", err
	}

	response, err := conn.Execute(command)
	if err != nil {
		return "", fmt.Errorf("execute rcon command: %w", err)
	}

	return response, nil
}
