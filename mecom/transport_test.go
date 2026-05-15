package mecom

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestDialCANEndpointRequiresAdapter(t *testing.T) {
	_, err := Dial(context.Background(), Endpoint{Network: "can", Address: "can0/0x4b"}, time.Second)
	if err == nil {
		t.Fatal("Dial returned nil error for direct CAN endpoint")
	}
	if !errors.Is(err, ErrTransportNotSupported) {
		t.Fatalf("error = %v, want ErrTransportNotSupported", err)
	}
}

func TestDialTCPUnreachableWrapsSentinel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = Dial(ctx, Endpoint{Network: "tcp", Address: addr}, time.Second)
	if err == nil {
		t.Fatalf("Dial unexpectedly connected to closed listener %s", addr)
	}
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("error = %v, want ErrUnreachable", err)
	}
}
