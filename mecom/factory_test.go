package mecom

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseCANEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantIface string
		wantNode  byte
	}{
		{name: "decimal", input: "can0/75", wantIface: "can0", wantNode: 75},
		{name: "hex", input: "can1/0x4c", wantIface: "can1", wantNode: 0x4c},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface, node, err := parseCANEndpoint(tt.input)
			if err != nil {
				t.Fatalf("parseCANEndpoint returned error: %v", err)
			}
			if iface != tt.wantIface || node != tt.wantNode {
				t.Fatalf("parseCANEndpoint(%q) = %q, %d; want %q, %d", tt.input, iface, node, tt.wantIface, tt.wantNode)
			}
		})
	}
}

func TestParseCANEndpointRejectsInvalidNode(t *testing.T) {
	for _, input := range []string{"can0", "can0/0", "can0/128", "can0/nope"} {
		if _, _, err := parseCANEndpoint(input); err == nil || !errors.Is(err, ErrBadAddress) {
			t.Fatalf("parseCANEndpoint(%q) returned nil error", input)
		}
	}
}

func TestNewForEndpointCANUsesInjectedDialer(t *testing.T) {
	var capturedIface string
	closed := false
	fake := &fakeCANTransceiver{}
	client, err := NewForEndpoint(context.Background(), Endpoint{
		Network: "can",
		Address: "canX/0x4b",
	}, ClientConfig{Address: 1, Timeout: time.Second}, func(ctx context.Context, iface string) (CANTransceiver, func() error, error) {
		capturedIface = iface
		return fake, func() error {
			closed = true
			return nil
		}, nil
	})
	if err != nil {
		t.Fatalf("NewForEndpoint returned error: %v", err)
	}
	if capturedIface != "canX" {
		t.Fatalf("dialer iface = %q, want canX", capturedIface)
	}
	canClient, ok := client.(*closingCAN)
	if !ok {
		t.Fatalf("client type = %T, want *closingCAN", client)
	}
	if canClient.node != 0x4b {
		t.Fatalf("CANopen node = 0x%02X, want 0x4B", canClient.node)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !closed {
		t.Fatal("Close did not call injected closer")
	}
}

func TestNewForEndpointCANRequiresDialer(t *testing.T) {
	_, err := NewForEndpoint(context.Background(), Endpoint{Network: "can", Address: "can0/1"}, ClientConfig{}, nil)
	if err == nil {
		t.Fatal("NewForEndpoint returned nil error without CAN dialer")
	}
	if !errors.Is(err, ErrTransportNotSupported) {
		t.Fatalf("error = %v, want ErrTransportNotSupported", err)
	}
}

func TestNewForEndpointCANPropagatesDialerError(t *testing.T) {
	want := errors.New("adapter unavailable")
	_, err := NewForEndpoint(context.Background(), Endpoint{
		Network: "can",
		Address: "can0/1",
	}, ClientConfig{}, func(ctx context.Context, iface string) (CANTransceiver, func() error, error) {
		return nil, nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
