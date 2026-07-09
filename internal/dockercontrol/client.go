package dockercontrol

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

type Commander interface {
	SendCommand(ctx context.Context, containerID, command string) (string, error)
}

type Client struct {
	cli           *client.Client
	outputTimeout time.Duration
}

func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	return &Client{cli: cli, outputTimeout: 10 * time.Second}, nil
}

func (c *Client) Close() error {
	return c.cli.Close()
}

func (c *Client) SendCommand(ctx context.Context, containerID, command string) (string, error) {
	containerID = strings.TrimSpace(containerID)
	command = strings.TrimSpace(command)
	if containerID == "" {
		return "", fmt.Errorf("container_id is required")
	}
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	inspected, err := c.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect container %s: %w", containerID, err)
	}
	if inspected.Container.State == nil || !inspected.Container.State.Running {
		return "", fmt.Errorf("container %s is not running", containerID)
	}

	resp, err := c.cli.ContainerAttach(ctx, containerID, client.ContainerAttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
		Logs:   false,
	})
	if err != nil {
		return "", fmt.Errorf("attach container %s: %w", containerID, err)
	}
	defer resp.Close()

	if _, err := io.WriteString(resp.Conn, command+"\n"); err != nil {
		return "", fmt.Errorf("write command to %s: %w", containerID, err)
	}

	if closer, ok := resp.Conn.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
	}

	return c.collectAttachOutput(ctx, resp, inspected.Container.Config != nil && inspected.Container.Config.Tty)
}

func (c *Client) collectAttachOutput(ctx context.Context, resp client.ContainerAttachResult, tty bool) (string, error) {
	if err := setDeadline(ctx, resp.Conn, c.outputTimeout); err != nil {
		return "", err
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var err error
	if tty {
		_, err = io.Copy(&stdout, resp.Reader)
	} else {
		_, err = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		output += stderr.String()
	}
	if err != nil && !isEndOfStreamError(err) {
		return output, fmt.Errorf("read container output: %w", err)
	}

	return output, nil
}

func setDeadline(ctx context.Context, conn io.Writer, timeout time.Duration) error {
	deadlineConn, ok := conn.(interface{ SetReadDeadline(time.Time) error })
	if !ok {
		return nil
	}
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := deadlineConn.SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("docker: %w", err)
	}
	return nil
}

func isEndOfStreamError(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, io.ErrUnexpectedEOF) || isTimeoutError(err)
}

func isTimeoutError(err error) bool {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	return false
}
