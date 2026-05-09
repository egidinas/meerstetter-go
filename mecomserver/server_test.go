package mecomserver

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestServeSerializesFramesFromMultipleClients(t *testing.T) {
	downstreamClient, downstreamServer := net.Pipe()
	defer downstreamClient.Close()
	defer downstreamServer.Close()

	seen := make(chan []byte, 2)
	go func() {
		reader := bufio.NewReader(downstreamServer)
		for i := 0; i < 2; i++ {
			frame, err := reader.ReadBytes('\r')
			if err != nil {
				return
			}
			seen <- append([]byte(nil), frame...)
			reply := []byte("!" + string(frame[1:len(frame)-1]) + "+0000\r")
			_, _ = downstreamServer.Write(reply)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, Config{
			Downstream: func(context.Context) (net.Conn, string, error) {
				return downstreamClient, "pipe", nil
			},
		})
	}()
	defer func() {
		cancel()
		_ = ln.Close()
		if err := <-done; err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	}()

	clientA := dialClient(t, ln.Addr().String())
	defer clientA.Close()
	clientB := dialClient(t, ln.Addr().String())
	defer clientB.Close()

	reqA := []byte("#500001?VR03E8010000\r")
	reqB := []byte("#510002?VR03E9010000\r")
	if _, err := clientA.Write(reqA); err != nil {
		t.Fatalf("write A: %v", err)
	}
	if _, err := clientB.Write(reqB); err != nil {
		t.Fatalf("write B: %v", err)
	}

	got := [][]byte{receiveSeen(t, seen), receiveSeen(t, seen)}
	if !containsFrame(got, reqA) || !containsFrame(got, reqB) {
		t.Fatalf("downstream did not receive both frames: %q", got)
	}
	if replyA := readFrame(t, clientA); !bytes.Contains(replyA, []byte("500001?VR03E8010000+")) {
		t.Fatalf("client A got wrong reply: %q", replyA)
	}
	if replyB := readFrame(t, clientB); !bytes.Contains(replyB, []byte("510002?VR03E9010000+")) {
		t.Fatalf("client B got wrong reply: %q", replyB)
	}
}

func dialClient(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	return conn
}

func receiveSeen(t *testing.T, seen <-chan []byte) []byte {
	t.Helper()
	select {
	case frame := <-seen:
		return frame
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for downstream frame")
		return nil
	}
}

func readFrame(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	frame, err := bufio.NewReader(conn).ReadBytes('\r')
	if err != nil && err != io.EOF {
		t.Fatalf("read frame: %v", err)
	}
	return frame
}

func containsFrame(frames [][]byte, want []byte) bool {
	for _, frame := range frames {
		if bytes.Equal(frame, want) {
			return true
		}
	}
	return false
}
