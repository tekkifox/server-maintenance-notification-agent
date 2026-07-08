package telnetcontrol

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

type Executor interface {
	Execute(ctx context.Context, address, password, command string) error
}

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Execute(ctx context.Context, address, password, command string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dial telnet %s: %w", address, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	}

	reader := bufio.NewReader(conn)
	if err := drainTelnetGreeting(conn, reader); err != nil && !isTimeoutError(err) {
		return fmt.Errorf("read telnet greeting: %w", err)
	}

	if password = strings.TrimSpace(password); password != "" {
		if _, err := fmt.Fprintf(conn, "%s\r\n", password); err != nil {
			return fmt.Errorf("write telnet password: %w", err)
		}
		if err := drainTelnetGreeting(conn, reader); err != nil && !isTimeoutError(err) {
			return fmt.Errorf("read telnet password response: %w", err)
		}
	}

	if command = strings.TrimSpace(command); command == "" {
		return fmt.Errorf("command is required")
	}
	if _, err := fmt.Fprintf(conn, "%s\r\n", command); err != nil {
		return fmt.Errorf("write telnet command: %w", err)
	}

	return nil
}

func drainTelnetGreeting(conn net.Conn, reader *bufio.Reader) error {
	_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
	defer func() {
		_ = conn.SetReadDeadline(time.Time{})
	}()

	for {
		if _, err := reader.ReadString('\n'); err != nil {
			if err == io.EOF || isTimeoutError(err) {
				return nil
			}
			return err
		}
	}
}

func isTimeoutError(err error) bool {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	return false
}
