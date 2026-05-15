package mecomserver

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
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

func TestRequestAddress(t *testing.T) {
	addr, err := RequestAddress([]byte("#4B0001?VR03E8010000\r"))
	if err != nil {
		t.Fatalf("RequestAddress returned error: %v", err)
	}
	if addr != 0x4B {
		t.Fatalf("address = 0x%02X, want 0x4B", addr)
	}
	if _, err := RequestAddress([]byte("!4B0001+0000\r")); err == nil {
		t.Fatal("RequestAddress accepted a response frame")
	}
}

func TestServeRouterRoutesByMeComAddress(t *testing.T) {
	routeAClient, routeAServer := net.Pipe()
	defer routeAClient.Close()
	defer routeAServer.Close()
	routeBClient, routeBServer := net.Pipe()
	defer routeBClient.Close()
	defer routeBServer.Close()

	seenA := serveDownstreamPipe(t, routeAServer, 1)
	seenB := serveDownstreamPipe(t, routeBServer, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- ServeRouter(ctx, ln, &RouterConfig{
			Routes: []Route{
				{Address: 0x50, Downstream: func(context.Context) (net.Conn, string, error) {
					return routeAClient, "pipe-a", nil
				}},
				{Address: 0x51, Downstream: func(context.Context) (net.Conn, string, error) {
					return routeBClient, "pipe-b", nil
				}},
			},
		})
	}()
	defer func() {
		cancel()
		_ = ln.Close()
		if err := <-done; err != nil {
			t.Fatalf("ServeRouter returned error: %v", err)
		}
	}()

	client := dialClient(t, ln.Addr().String())
	defer client.Close()

	reqA := []byte("#500001?VR03E8010000\r")
	reqB := []byte("#510002?VR03E9010000\r")
	if _, err := client.Write(reqA); err != nil {
		t.Fatalf("write A: %v", err)
	}
	if got := receiveSeen(t, seenA); !bytes.Equal(got, reqA) {
		t.Fatalf("route A got %q, want %q", got, reqA)
	}
	if replyA := readFrame(t, client); !bytes.Contains(replyA, []byte("500001?VR03E8010000+")) {
		t.Fatalf("client got wrong route A reply: %q", replyA)
	}

	if _, err := client.Write(reqB); err != nil {
		t.Fatalf("write B: %v", err)
	}
	if got := receiveSeen(t, seenB); !bytes.Equal(got, reqB) {
		t.Fatalf("route B got %q, want %q", got, reqB)
	}
	if replyB := readFrame(t, client); !bytes.Contains(replyB, []byte("510002?VR03E9010000+")) {
		t.Fatalf("client got wrong route B reply: %q", replyB)
	}
}

func TestServeRouterRejectsUnknownAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- ServeRouter(ctx, ln, &RouterConfig{
			Routes: []Route{
				{Address: 0x50, Downstream: func(context.Context) (net.Conn, string, error) {
					client, _ := net.Pipe()
					return client, "unused", nil
				}},
			},
		})
	}()
	defer func() {
		cancel()
		_ = ln.Close()
		if err := <-done; err != nil {
			t.Fatalf("ServeRouter returned error: %v", err)
		}
	}()

	client := dialClient(t, ln.Addr().String())
	defer client.Close()
	if _, err := client.Write([]byte("#520001?VR03E8010000\r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	reply := string(readFrame(t, client))
	if !strings.Contains(reply, "no downstream route for MeCom address 0x52") {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestServeRouterStatsTrackFramesAndErrors(t *testing.T) {
	routeClient, routeServer := net.Pipe()
	defer routeClient.Close()
	defer routeServer.Close()
	seen := serveDownstreamPipe(t, routeServer, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	cfg := &RouterConfig{
		RequestTimeout: time.Second,
		ReconnectDelay: time.Millisecond,
		Routes: []Route{
			{Address: 0x50, Target: "pipe-a", Downstream: func(context.Context) (net.Conn, string, error) {
				return routeClient, "pipe-a-live", nil
			}},
			{Address: 0x52, Target: "down", Downstream: func(context.Context) (net.Conn, string, error) {
				return nil, "", errors.New("dial down")
			}},
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- ServeRouter(ctx, ln, cfg)
	}()
	defer func() {
		cancel()
		_ = ln.Close()
		if err := <-done; err != nil {
			t.Fatalf("ServeRouter returned error: %v", err)
		}
	}()

	client := dialClient(t, ln.Addr().String())
	defer client.Close()

	reqOK := []byte("#500001?VR03E8010000\r")
	if _, err := client.Write(reqOK); err != nil {
		t.Fatalf("write ok: %v", err)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, reqOK) {
		t.Fatalf("route got %q, want %q", got, reqOK)
	}
	_ = readFrame(t, client)

	reqFail := []byte("#520001?VR03E8010000\r")
	if _, err := client.Write(reqFail); err != nil {
		t.Fatalf("write fail: %v", err)
	}
	if reply := string(readFrame(t, client)); !strings.Contains(reply, "dial down") {
		t.Fatalf("unexpected error reply: %q", reply)
	}

	stats := cfg.Stats()
	okStats := stats[0x50]
	if !okStats.Connected || okStats.Target != "pipe-a-live" || okStats.FramesIn != 1 || okStats.FramesOut != 1 || okStats.ErrorCount != 0 {
		t.Fatalf("ok stats=%+v, want connected pipe-a-live with 1 in/1 out", okStats)
	}
	failStats := stats[0x52]
	if failStats.Connected || failStats.FramesIn != 1 || failStats.FramesOut != 0 || failStats.ErrorCount != 1 || !strings.Contains(failStats.LastError, "dial down") {
		t.Fatalf("fail stats=%+v, want disconnected with one dial error", failStats)
	}
}

func serveDownstreamPipe(t *testing.T, conn net.Conn, count int) <-chan []byte {
	t.Helper()
	seen := make(chan []byte, count)
	go func() {
		reader := bufio.NewReader(conn)
		for i := 0; i < count; i++ {
			frame, err := reader.ReadBytes('\r')
			if err != nil {
				return
			}
			seen <- append([]byte(nil), frame...)
			reply := []byte("!" + string(frame[1:len(frame)-1]) + "+0000\r")
			_, _ = conn.Write(reply)
		}
	}()
	return seen
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
