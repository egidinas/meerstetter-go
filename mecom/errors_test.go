package mecom

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestSentinelsSurface pins every sentinel to a trigger path so a consumer's
// errors.Is checks remain stable across refactors.
func TestSentinelsSurface(t *testing.T) {
	t.Run("ErrUnreachable from serial open", func(t *testing.T) {
		_, err := Dial(context.Background(), Endpoint{Network: "serial", Address: "/dev/does-not-exist-xyz", Baud: 57600}, time.Second)
		if !errors.Is(err, ErrUnreachable) {
			t.Fatalf("Dial bad serial = %v, want wrap ErrUnreachable", err)
		}
	})
	t.Run("ErrTransportNotSupported from raw can: in Dial", func(t *testing.T) {
		_, err := Dial(context.Background(), Endpoint{Network: "can", Address: "can0"}, time.Second)
		if !errors.Is(err, ErrTransportNotSupported) {
			t.Fatalf("Dial raw can = %v, want wrap ErrTransportNotSupported", err)
		}
	})
	t.Run("ErrTransportNotSupported from missing CANDialer", func(t *testing.T) {
		_, err := NewForEndpoint(context.Background(), Endpoint{Network: "can", Address: "can0/0x4b"}, ClientConfig{Timeout: time.Second}, nil)
		if !errors.Is(err, ErrTransportNotSupported) {
			t.Fatalf("NewForEndpoint without dialer = %v, want wrap ErrTransportNotSupported", err)
		}
	})
	t.Run("ErrBadAddress from invalid CAN node", func(t *testing.T) {
		_, _, err := parseCANEndpoint("can0/0")
		if !errors.Is(err, ErrBadAddress) {
			t.Fatalf("parseCANEndpoint(can0/0) = %v, want wrap ErrBadAddress", err)
		}
	})
	t.Run("ErrParameterReadOnly from sensor write", func(t *testing.T) {
		client := NewCANopenClient(&fakeCANTransceiver{}, ClientConfig{Address: 0x4b, Timeout: time.Second})
		err := client.WriteFloat32(context.Background(), 1000, 1, 25.0)
		if !errors.Is(err, ErrParameterReadOnly) {
			t.Fatalf("write sensor 1000 = %v, want wrap ErrParameterReadOnly", err)
		}
	})
	t.Run("ErrUnknownParameter from unmapped read", func(t *testing.T) {
		client := NewCANopenClient(&fakeCANTransceiver{}, ClientConfig{Address: 0x4b, Timeout: time.Second})
		_, err := client.ReadFloat32(context.Background(), 999999, 1)
		if !errors.Is(err, ErrUnknownParameter) {
			t.Fatalf("read unmapped param = %v, want wrap ErrUnknownParameter", err)
		}
	})
	t.Run("ErrTransportNotSupported from CAN ring features", func(t *testing.T) {
		client := NewCANClient(&fakeCANTransceiver{}, ClientConfig{Address: 0x4b, Timeout: time.Second})
		if err := client.ConfigureRingCapture(context.Background(), 0, nil); !errors.Is(err, ErrTransportNotSupported) {
			t.Fatalf("CANClient.ConfigureRingCapture = %v, want wrap ErrTransportNotSupported", err)
		}
		if err := client.TriggerRingSync(context.Background()); !errors.Is(err, ErrTransportNotSupported) {
			t.Fatalf("CANClient.TriggerRingSync = %v, want wrap ErrTransportNotSupported", err)
		}
	})
}
