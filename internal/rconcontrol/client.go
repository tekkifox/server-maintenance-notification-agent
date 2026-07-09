package rconcontrol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/gorcon/rcon"
)

const (
	defaultDialTimeout     = 5 * time.Second
	defaultAuthTimeout     = 5 * time.Second
	defaultResponseTimeout = 500 * time.Millisecond
)

type Executor interface {
	Execute(ctx context.Context, address, password, command string) (string, error)
}

type Client struct {
	dialTimeout     time.Duration
	authTimeout     time.Duration
	responseTimeout time.Duration
}

func NewClient() *Client {
	return &Client{
		dialTimeout:     defaultDialTimeout,
		authTimeout:     defaultAuthTimeout,
		responseTimeout: defaultResponseTimeout,
	}
}

func (c *Client) Execute(ctx context.Context, address, password, command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(command) == "" {
		return "", rcon.ErrCommandEmpty
	}
	if len(command) > rcon.MaxCommandLen {
		return "", rcon.ErrCommandTooLong
	}

	conn, err := net.DialTimeout("tcp", address, c.dialTimeout)
	if err != nil {
		return "", fmt.Errorf("dial rcon %s: %w", address, err)
	}
	defer conn.Close()

	if err := authenticate(ctx, conn, password, c.authTimeout); err != nil {
		return "", fmt.Errorf("rcon: %w", err)
	}

	return executeCommand(ctx, conn, command, c.responseTimeout)
}

func authenticate(ctx context.Context, conn net.Conn, password string, timeout time.Duration) error {
	if err := writePacket(ctx, conn, timeout, rcon.NewPacket(rcon.SERVERDATA_AUTH, rcon.SERVERDATA_AUTH_ID, password)); err != nil {
		return err
	}

	packet, err := readPacket(ctx, conn, timeout)
	if err != nil {
		return err
	}

	// Some servers emit an empty SERVERDATA_RESPONSE_VALUE before the auth response.
	if packet.Type == rcon.SERVERDATA_RESPONSE_VALUE {
		packet, err = readPacket(ctx, conn, timeout)
		if err != nil {
			return err
		}
	}

	if packet.Type != rcon.SERVERDATA_AUTH_RESPONSE {
		return rcon.ErrInvalidAuthResponse
	}
	if packet.ID == -1 {
		return rcon.ErrAuthFailed
	}
	if packet.ID != rcon.SERVERDATA_AUTH_ID {
		return rcon.ErrInvalidPacketID
	}

	return nil
}

func executeCommand(ctx context.Context, conn net.Conn, command string, timeout time.Duration) (string, error) {
	if err := writePacket(ctx, conn, timeout, rcon.NewPacket(rcon.SERVERDATA_EXECCOMMAND, rcon.SERVERDATA_EXECCOMMAND_ID, command)); err != nil {
		return "", err
	}

	var output strings.Builder
	readAny := false
	for {
		packet, err := readPacket(ctx, conn, timeout)
		if err != nil {
			if isEndOfResponseError(err) {
				return output.String(), nil
			}
			return output.String(), err
		}

		if packet.Type == 4 {
			packet, err = readPacket(ctx, conn, timeout)
			if err != nil {
				if isEndOfResponseError(err) {
					return output.String(), nil
				}
				return output.String(), err
			}

			// Workaround for Rust servers. Their follow-up packet may still carry
			// the previous packet id.
			if packet.ID == -1 {
				packet.ID = rcon.SERVERDATA_EXECCOMMAND_ID
			}
		}

		if packet.ID != rcon.SERVERDATA_EXECCOMMAND_ID {
			if readAny {
				return output.String(), nil
			}
			return "", rcon.ErrInvalidPacketID
		}

		body := packet.Body()
		if body != "" {
			output.WriteString(body)
		}
		readAny = true
	}
}

func writePacket(ctx context.Context, conn net.Conn, timeout time.Duration, packet *rcon.Packet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := setDeadline(ctx, conn, timeout, true); err != nil {
		return err
	}
	if _, err := packet.WriteTo(conn); err != nil {
		return fmt.Errorf("rcon: write packet: %w", err)
	}
	return nil
}

func readPacket(ctx context.Context, conn net.Conn, timeout time.Duration) (*rcon.Packet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := setDeadline(ctx, conn, timeout, false); err != nil {
		return nil, err
	}

	packet := &rcon.Packet{}
	if _, err := packet.ReadFrom(conn); err != nil {
		return packet, err
	}

	return packet, nil
}

func setDeadline(ctx context.Context, conn net.Conn, timeout time.Duration, write bool) error {
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	if write {
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return fmt.Errorf("rcon: %w", err)
		}
		return nil
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("rcon: %w", err)
	}
	return nil
}

func isTimeoutError(err error) bool {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	return false
}

func isEndOfResponseError(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || isTimeoutError(err)
}
