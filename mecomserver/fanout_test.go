package mecomserver

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

func TestRouterRuntimeDevicesGroupsDuplicateRoutes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := NewRouterRuntime(ctx, &RouterConfig{
		Routes: []Route{
			{Address: 0x50, Target: "serial:/dev/ttyUSB0@57600", Downstream: unusedDownstream},
			{Address: 0x50, Target: "can:can0/0x50", Downstream: unusedDownstream},
			{Address: 0x51, Target: "tcp:pixtend:50010", Downstream: unusedDownstream},
		},
	})
	if err != nil {
		t.Fatalf("NewRouterRuntime: %v", err)
	}

	devices := rt.Devices()
	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2: %+v", len(devices), devices)
	}
	if devices[0].Address != 0x50 || len(devices[0].Routes) != 2 {
		t.Fatalf("first device = %+v, want 0x50 with two routes", devices[0])
	}
	if devices[0].Routes[0].Priority != 0 || devices[0].Routes[1].Priority != 1 {
		t.Fatalf("duplicate priorities = %+v, want configured order 0,1", devices[0].Routes)
	}
	if devices[1].Address != 0x51 || len(devices[1].Routes) != 1 {
		t.Fatalf("second device = %+v, want 0x51 with one route", devices[1])
	}
}

func TestRouterRuntimeFanoutReadAddressesAllConfiguredDevices(t *testing.T) {
	routeAClient, routeAServer := net.Pipe()
	defer routeAClient.Close()
	defer routeAServer.Close()
	seenA := serveDownstreamPipe(t, routeAServer, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt, err := NewRouterRuntime(ctx, &RouterConfig{
		RequestTimeout: time.Second,
		Routes: []Route{
			{Address: 0x50, Target: "serial-a", Downstream: func(context.Context) (net.Conn, string, error) {
				return routeAClient, "serial-a-live", nil
			}},
			{Address: 0x51, Target: "serial-b", Downstream: func(context.Context) (net.Conn, string, error) {
				return nil, "", errors.New("device offline")
			}},
		},
	})
	if err != nil {
		t.Fatalf("NewRouterRuntime: %v", err)
	}

	result, err := rt.Fanout(ctx, FanoutRequest{
		Frame: mecom.BuildSingleGetFrame(0, 1, 1000, 1),
	})
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	if !result.Read {
		t.Fatalf("Fanout result Read = false, want true")
	}
	if len(result.Responses) != 2 {
		t.Fatalf("responses = %d, want 2: %+v", len(result.Responses), result.Responses)
	}

	wantA := mecom.BuildSingleGetFrame(0x50, 1, 1000, 1)
	if got := receiveSeen(t, seenA); !bytes.Equal(got, wantA) {
		t.Fatalf("route A got %q, want addressed frame %q", got, wantA)
	}
	if result.Responses[0].Address != 0x50 || result.Responses[0].Error != "" {
		t.Fatalf("response A = %+v, want success for 0x50", result.Responses[0])
	}
	if !strings.Contains(result.Responses[0].ResponseFrame, "!500001") {
		t.Fatalf("response A frame = %q, want addressed reply", result.Responses[0].ResponseFrame)
	}
	if result.Responses[1].Address != 0x51 || !strings.Contains(result.Responses[1].Error, "device offline") {
		t.Fatalf("response B = %+v, want partial offline error", result.Responses[1])
	}
}

func TestRouterRuntimeFanoutRejectsWritesByDefault(t *testing.T) {
	routeClient, routeServer := net.Pipe()
	defer routeClient.Close()
	defer routeServer.Close()
	seen := serveDownstreamPipe(t, routeServer, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt, err := NewRouterRuntime(ctx, &RouterConfig{
		Routes: []Route{
			{Address: 0x50, Target: "serial-a", Downstream: func(context.Context) (net.Conn, string, error) {
				return routeClient, "serial-a-live", nil
			}},
		},
	})
	if err != nil {
		t.Fatalf("NewRouterRuntime: %v", err)
	}

	_, err = rt.Fanout(ctx, FanoutRequest{
		Frame: mecom.BuildWriteFloat32Frame(0, 1, 1000, 1, 12.5),
	})
	if err == nil || !strings.Contains(err.Error(), "write fanout") {
		t.Fatalf("Fanout error = %v, want write fanout rejection", err)
	}
	select {
	case got := <-seen:
		t.Fatalf("write fanout was forwarded despite default rejection: %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRouterRuntimeFanoutWritePolicyUsesCommandIdempotency(t *testing.T) {
	routeClient, routeServer := net.Pipe()
	defer routeClient.Close()
	defer routeServer.Close()
	seen := serveDownstreamPipe(t, routeServer, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt, err := NewRouterRuntime(ctx, &RouterConfig{
		CommandIdempotencyTTL: time.Minute,
		RequestTimeout:        time.Second,
		Routes: []Route{
			{Address: 0x50, Target: "serial-a", Downstream: func(context.Context) (net.Conn, string, error) {
				return routeClient, "serial-a-live", nil
			}},
		},
	})
	if err != nil {
		t.Fatalf("NewRouterRuntime: %v", err)
	}

	req := FanoutRequest{
		Frame:       mecom.BuildWriteFloat32Frame(0, 1, 1000, 1, 12.5),
		WritePolicy: FanoutWriteAllowAddressed,
	}
	first, err := rt.Fanout(ctx, req)
	if err != nil {
		t.Fatalf("first Fanout: %v", err)
	}
	if len(first.Responses) != 1 || first.Responses[0].Error != "" {
		t.Fatalf("first responses = %+v, want one successful write", first.Responses)
	}
	want := mecom.BuildWriteFloat32Frame(0x50, 1, 1000, 1, 12.5)
	if got := receiveSeen(t, seen); !bytes.Equal(got, want) {
		t.Fatalf("route got %q, want addressed write %q", got, want)
	}

	second, err := rt.Fanout(ctx, req)
	if err != nil {
		t.Fatalf("second Fanout: %v", err)
	}
	if len(second.Responses) != 1 || second.Responses[0].ResponseFrame != first.Responses[0].ResponseFrame {
		t.Fatalf("second responses = %+v, want cached reply %+v", second.Responses, first.Responses)
	}
	select {
	case got := <-seen:
		t.Fatalf("duplicate fanout write was forwarded: %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func unusedDownstream(context.Context) (net.Conn, string, error) {
	return nil, "", errors.New("unused downstream")
}
