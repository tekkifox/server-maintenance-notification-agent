package telnetcontrol

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
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

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return "", fmt.Errorf("dial telnet %s: %w", address, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	}

	reader := bufio.NewReader(conn)
	if err := drainTelnetGreeting(conn, reader); err != nil && !isTimeoutError(err) {
		return "", fmt.Errorf("read telnet greeting: %w", err)
	}

	if password = strings.TrimSpace(password); password != "" {
		if _, err := fmt.Fprintf(conn, "%s\r\n", password); err != nil {
			return "", fmt.Errorf("write telnet password: %w", err)
		}
		if err := drainTelnetGreeting(conn, reader); err != nil && !isTimeoutError(err) {
			return "", fmt.Errorf("read telnet password response: %w", err)
		}
	}

	if command = strings.TrimSpace(command); command == "" {
		return "", fmt.Errorf("command is required")
	}
	if _, err := fmt.Fprintf(conn, "%s\r\n", command); err != nil {
		return "", fmt.Errorf("write telnet command: %w", err)
	}
	if closer, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
	}

	return c.readTelnetOutput(ctx, conn, reader)
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

func (c *Client) readTelnetOutput(ctx context.Context, conn net.Conn, reader *bufio.Reader) (string, error) {
	var output bytes.Buffer
	buf := make([]byte, 4096)
	for {
		if err := ctx.Err(); err != nil {
			return output.String(), err
		}
		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return output.String(), fmt.Errorf("set telnet read deadline: %w", err)
		}
		n, err := reader.Read(buf)
		if n > 0 {
			_, _ = output.Write(buf[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) || isTimeoutError(err) || errors.Is(err, io.ErrClosedPipe) {
				return output.String(), nil
			}
			return output.String(), fmt.Errorf("read telnet output: %w", err)
		}
	}
}
