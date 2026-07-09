package dockercontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	clientapi "github.com/moby/moby/client"
)

func TestCollectAttachOutputDemuxesStdStreams(t *testing.T) {
	reader := bufio.NewReader(bytes.NewReader(muxedStream(
		stdcopy.StdType(1), "stdout-line\n",
		stdcopy.StdType(2), "stderr-line\n",
	)))
	client := &Client{outputTimeout: time.Second}
	output, err := client.collectAttachOutput(context.Background(), clientapi.ContainerAttachResult{
		HijackedResponse: clientapi.HijackedResponse{Conn: &deadlineOnlyConn{}, Reader: reader},
	}, false)
	if err != nil {
		t.Fatalf("collectAttachOutput: %v", err)
	}
	if output == "" || !(bytes.Contains([]byte(output), []byte("stdout-line")) && bytes.Contains([]byte(output), []byte("stderr-line"))) {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestCollectAttachOutputTTY(t *testing.T) {
	reader := bufio.NewReader(bytes.NewReader([]byte("line1\nline2\n")))
	client := &Client{outputTimeout: time.Second}
	output, err := client.collectAttachOutput(context.Background(), clientapi.ContainerAttachResult{
		HijackedResponse: clientapi.HijackedResponse{Conn: &deadlineOnlyConn{}, Reader: reader},
	}, true)
	if err != nil {
		t.Fatalf("collectAttachOutput: %v", err)
	}
	if output != "line1\nline2\n" {
		t.Fatalf("unexpected output: %q", output)
	}
}

type deadlineOnlyConn struct{}

func (d *deadlineOnlyConn) Read(_ []byte) (int, error)       { return 0, io.EOF }
func (d *deadlineOnlyConn) Write(p []byte) (int, error)      { return len(p), nil }
func (d *deadlineOnlyConn) Close() error                     { return nil }
func (d *deadlineOnlyConn) LocalAddr() net.Addr              { return dummyAddr("local") }
func (d *deadlineOnlyConn) RemoteAddr() net.Addr             { return dummyAddr("remote") }
func (d *deadlineOnlyConn) SetDeadline(time.Time) error      { return nil }
func (d *deadlineOnlyConn) SetReadDeadline(time.Time) error  { return nil }
func (d *deadlineOnlyConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr string

func (d dummyAddr) Network() string { return string(d) }
func (d dummyAddr) String() string  { return string(d) }

func muxedStream(streams ...any) []byte {
	var buf bytes.Buffer
	for i := 0; i < len(streams); i += 2 {
		typeByte := streams[i].(stdcopy.StdType)
		payload := []byte(streams[i+1].(string))
		buf.WriteByte(byte(typeByte))
		buf.Write([]byte{0, 0, 0})
		_ = binary.Write(&buf, binary.BigEndian, uint32(len(payload)))
		buf.Write(payload)
	}
	return buf.Bytes()
}
