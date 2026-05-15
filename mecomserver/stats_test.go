package mecomserver

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestRouterStatsReflectsConnectAndFrames(t *testing.T) {
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
		Routes: []Route{
			{Address: 0x4b, Target: "pipe-stats", Downstream: func(context.Context) (net.Conn, string, error) {
				return routeClient, "pipe-stats", nil
			}},
		},
	}
	done := make(chan error, 1)
	go func() { done <- ServeRouter(ctx, ln, cfg) }()
	defer func() {
		cancel()
		_ = ln.Close()
		if err := <-done; err != nil {
			t.Fatalf("ServeRouter: %v", err)
		}
	}()

	client := dialClient(t, ln.Addr().String())
	defer client.Close()
	req := []byte("#4B0001?VR03E8010000\r")
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, req) {
		t.Fatalf("seen=%q, want %q", got, req)
	}
	_ = readFrame(t, client)

	stats := cfg.Stats()
	if len(stats) != 1 {
		t.Fatalf("stats len=%d, want 1", len(stats))
	}
	bs, ok := stats[0x4b]
	if !ok {
		t.Fatalf("no stats for 0x4B")
	}
	if !bs.Connected {
		t.Fatalf("connected=false, want true")
	}
	if bs.FramesIn != 1 {
		t.Fatalf("FramesIn=%d, want 1", bs.FramesIn)
	}
	if bs.FramesOut != 1 {
		t.Fatalf("FramesOut=%d, want 1", bs.FramesOut)
	}
	if bs.LastConnectAt.IsZero() {
		t.Fatalf("LastConnectAt is zero")
	}
}

func TestRouterStatsRecordsDialError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	cfg := &RouterConfig{
		ReconnectDelay: 10 * time.Millisecond,
		Routes: []Route{
			{Address: 0x4b, Target: "always-fails", Downstream: func(context.Context) (net.Conn, string, error) {
				return nil, "", errors.New("downstream dial blew up")
			}},
		},
	}
	done := make(chan error, 1)
	go func() { done <- ServeRouter(ctx, ln, cfg) }()
	defer func() {
		cancel()
		_ = ln.Close()
		<-done
	}()

	client := dialClient(t, ln.Addr().String())
	defer client.Close()
	if _, err := client.Write([]byte("#4B0001?VR03E8010000\r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// The router emits a ME-Device-Server-Error frame back on the client; consume it.
	_ = readFrame(t, client)

	// Give the broker a moment to record the error.
	time.Sleep(50 * time.Millisecond)
	stats := cfg.Stats()
	bs, ok := stats[0x4b]
	if !ok {
		t.Fatalf("no stats")
	}
	if bs.Connected {
		t.Fatalf("connected=true, want false after dial failure")
	}
	if bs.ErrorCount == 0 {
		t.Fatalf("ErrorCount=0, want >0")
	}
	if bs.LastError == "" {
		t.Fatalf("LastError empty")
	}
}
