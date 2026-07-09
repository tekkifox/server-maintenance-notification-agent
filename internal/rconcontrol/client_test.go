package rconcontrol

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gorcon/rcon"
)

func TestAuthenticateAndExecuteCollectsMultiPacketOutput(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	serverDone := make(chan error, 1)
	go func() {
		defer serverConn.Close()

		packet, err := readTestPacket(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if packet.Type != rcon.SERVERDATA_AUTH || packet.ID != rcon.SERVERDATA_AUTH_ID || packet.Body() != "secret" {
			serverDone <- fmt.Errorf("unexpected auth packet: type=%d id=%d body=%q", packet.Type, packet.ID, packet.Body())
			return
		}

		if _, err := rcon.NewPacket(rcon.SERVERDATA_RESPONSE_VALUE, rcon.SERVERDATA_AUTH_ID, "").WriteTo(serverConn); err != nil {
			serverDone <- err
			return
		}
		if _, err := rcon.NewPacket(rcon.SERVERDATA_AUTH_RESPONSE, rcon.SERVERDATA_AUTH_ID, "").WriteTo(serverConn); err != nil {
			serverDone <- err
			return
		}

		packet, err = readTestPacket(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if packet.Type != rcon.SERVERDATA_EXECCOMMAND || packet.ID != rcon.SERVERDATA_EXECCOMMAND_ID || packet.Body() != "status" {
			serverDone <- fmt.Errorf("unexpected exec packet: type=%d id=%d body=%q", packet.Type, packet.ID, packet.Body())
			return
		}

		if _, err := rcon.NewPacket(rcon.SERVERDATA_RESPONSE_VALUE, rcon.SERVERDATA_EXECCOMMAND_ID, "line1").WriteTo(serverConn); err != nil {
			serverDone <- err
			return
		}
		if _, err := rcon.NewPacket(rcon.SERVERDATA_RESPONSE_VALUE, rcon.SERVERDATA_EXECCOMMAND_ID, "line2").WriteTo(serverConn); err != nil {
			serverDone <- err
			return
		}

		serverDone <- nil
	}()

	if err := authenticate(context.Background(), clientConn, "secret", time.Second); err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}

	output, err := executeCommand(context.Background(), clientConn, "status", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if output != "line1line2" {
		t.Fatalf("unexpected output: %q", output)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func readTestPacket(conn net.Conn) (*rcon.Packet, error) {
	packet := &rcon.Packet{}
	if _, err := packet.ReadFrom(conn); err != nil {
		return nil, err
	}
	return packet, nil
}
