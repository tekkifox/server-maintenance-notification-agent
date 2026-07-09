package telnetcontrol

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestExecuteReturnsTelnetOutput(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()

		if _, err := fmt.Fprint(conn, "welcome\r\n"); err != nil {
			serverDone <- err
			return
		}

		buf := make([]byte, 128)
		n, err := conn.Read(buf)
		if err != nil {
			serverDone <- err
			return
		}
		if got := strings.TrimSpace(string(buf[:n])); got != "status" {
			serverDone <- fmt.Errorf("unexpected command: %q", got)
			return
		}

		if _, err := fmt.Fprint(conn, "line1\nline2\n"); err != nil {
			serverDone <- err
			return
		}
		serverDone <- nil
	}()

	output, err := NewClient().Execute(context.Background(), ln.Addr().String(), "", "status")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(output, "line1") || !strings.Contains(output, "line2") {
		t.Fatalf("unexpected output: %q", output)
	}

	select {
	case serverErr := <-serverDone:
		if serverErr != nil {
			t.Fatalf("server error: %v", serverErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server timeout")
	}
}
