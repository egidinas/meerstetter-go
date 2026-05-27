package mecomserver

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
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

func TestExchangeCancelsBlockedDownstreamRead(t *testing.T) {
	downstreamClient, downstreamServer := net.Pipe()
	defer downstreamServer.Close()

	seen := make(chan []byte, 1)
	serverClosed := make(chan struct{})
	go func() {
		defer close(serverClosed)
		reader := bufio.NewReader(downstreamServer)
		frame, err := reader.ReadBytes(mecom.FrameTerminator)
		if err == nil {
			seen <- append([]byte(nil), frame...)
		}
		_, _ = reader.ReadByte()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	req := []byte("#500001?VR03E8010000\r")
	go func() {
		_, err := exchange(ctx, downstreamClient, bufio.NewReader(downstreamClient), req, time.Second)
		done <- err
	}()

	if got := receiveSeen(t, seen); !bytes.Equal(got, req) {
		t.Fatalf("downstream got %q, want %q", got, req)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("exchange error = %v, want context.Canceled", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("exchange did not return after request context cancellation")
	}
	select {
	case <-serverClosed:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("downstream connection was not closed after cancellation")
	}
}

func TestExchangeKeepsDownstreamOpenAfterSuccessfulReply(t *testing.T) {
	downstreamClient, downstreamServer := net.Pipe()
	defer downstreamClient.Close()
	defer downstreamServer.Close()

	go func() {
		reader := bufio.NewReader(downstreamServer)
		for {
			frame, err := reader.ReadBytes(mecom.FrameTerminator)
			if err != nil {
				return
			}
			reply := []byte("!" + string(frame[1:len(frame)-1]) + "+0000\r")
			_, _ = downstreamServer.Write(reply)
		}
	}()

	reader := bufio.NewReader(downstreamClient)
	req := []byte("#500001?VR03E8010000\r")
	for i := 0; i < 50; i++ {
		resp, err := exchange(context.Background(), downstreamClient, reader, req, time.Second)
		if err != nil {
			t.Fatalf("exchange %d returned error: %v", i, err)
		}
		if !bytes.Contains(resp, []byte("500001?VR03E8010000+")) {
			t.Fatalf("exchange %d got wrong response: %q", i, resp)
		}
		time.Sleep(time.Millisecond)
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
	addr, err = RequestAddress([]byte("?VR03E8010000\r"))
	if err != nil {
		t.Fatalf("RequestAddress rejected unaddressed request: %v", err)
	}
	if addr != 0 {
		t.Fatalf("unaddressed request address = 0x%02X, want 0", addr)
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

func TestServeRouterRoutesAddressZeroToDefaultAddress(t *testing.T) {
	routeClient, routeServer := net.Pipe()
	defer routeClient.Close()
	defer routeServer.Close()
	seen := serveDownstreamPipe(t, routeServer, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- ServeRouter(ctx, ln, &RouterConfig{
			DefaultAddress: 0x4C,
			Routes: []Route{
				{Address: 0x4C, Downstream: func(context.Context) (net.Conn, string, error) {
					return routeClient, "pipe-default", nil
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

	req := []byte("#000001?VR03E8010000\r")
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, req) {
		t.Fatalf("default route got %q, want unchanged frame %q", got, req)
	}
	if reply := readFrame(t, client); !bytes.Contains(reply, []byte("000001?VR03E8010000+")) {
		t.Fatalf("client got wrong default route reply: %q", reply)
	}

	bareReq := []byte("?VR03E8010000\r")
	if _, err := client.Write(bareReq); err != nil {
		t.Fatalf("write bare request: %v", err)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, bareReq) {
		t.Fatalf("default route got bare request %q, want %q", got, bareReq)
	}
	if reply := readFrame(t, client); !bytes.Contains(reply, []byte("VR03E8010000+")) {
		t.Fatalf("client got wrong bare-request default route reply: %q", reply)
	}
}

func TestServeRouterRoutesAddressZeroByConnectionRouteOrder(t *testing.T) {
	routeAClient, routeAServer := net.Pipe()
	defer routeAClient.Close()
	defer routeAServer.Close()
	routeBClient, routeBServer := net.Pipe()
	defer routeBClient.Close()
	defer routeBServer.Close()

	seenA := serveDownstreamPipe(t, routeAServer, 8)
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
			AddressZeroOrder: []byte{0x50, 0x51},
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

	reqA := []byte("#000001?VR03E8010000\r")
	clientA := dialClient(t, ln.Addr().String())
	defer clientA.Close()
	if _, err := clientA.Write(reqA); err != nil {
		t.Fatalf("write A: %v", err)
	}
	if got := receiveSeen(t, seenA); !bytes.Equal(got, reqA) {
		t.Fatalf("route A got %q, want unchanged frame %q", got, reqA)
	}
	_ = readFrame(t, clientA)
	clientA.Close()

	reqB := []byte("#000002?VR03E8010000\r")
	for attempt := 1; attempt <= 8; attempt++ {
		clientB := dialClient(t, ln.Addr().String())
		if _, err := clientB.Write(reqB); err != nil {
			_ = clientB.Close()
			t.Fatalf("write B attempt %d: %v", attempt, err)
		}
		select {
		case got := <-seenB:
			if !bytes.Equal(got, reqB) {
				_ = clientB.Close()
				t.Fatalf("route B got %q, want unchanged frame %q", got, reqB)
			}
			_ = readFrame(t, clientB)
			_ = clientB.Close()
			return
		case got := <-seenA:
			_ = readFrame(t, clientB)
			_ = clientB.Close()
			if !bytes.Equal(got, reqB) {
				t.Fatalf("route A got %q during release wait, want unchanged frame %q", got, reqB)
			}
			time.Sleep(10 * time.Millisecond)
		case <-time.After(250 * time.Millisecond):
			_ = clientB.Close()
			t.Fatal("timed out waiting for downstream frame")
		}
	}
	t.Fatal("address-zero lease stayed on route A after client close")
}

func TestServeRouterKeepsAddressZeroStickyForConcurrentRemoteConnections(t *testing.T) {
	routeAClient, routeAServer := net.Pipe()
	defer routeAClient.Close()
	defer routeAServer.Close()
	routeBClient, routeBServer := net.Pipe()
	defer routeBClient.Close()
	defer routeBServer.Close()

	seenA := serveDownstreamPipe(t, routeAServer, 2)
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
			AddressZeroOrder: []byte{0x50, 0x51},
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

	reqA := []byte("#000001?VR03E8010000\r")
	clientA := dialClient(t, ln.Addr().String())
	defer clientA.Close()
	if _, err := clientA.Write(reqA); err != nil {
		t.Fatalf("write A: %v", err)
	}
	if got := receiveSeen(t, seenA); !bytes.Equal(got, reqA) {
		t.Fatalf("route A got %q, want unchanged frame %q", got, reqA)
	}

	reqB := []byte("#000002?VR03E8010000\r")
	clientB := dialClient(t, ln.Addr().String())
	defer clientB.Close()
	if _, err := clientB.Write(reqB); err != nil {
		t.Fatalf("write B: %v", err)
	}
	if got := receiveSeen(t, seenA); !bytes.Equal(got, reqB) {
		t.Fatalf("second concurrent route got %q, want sticky route A frame %q", got, reqB)
	}
	select {
	case got := <-seenB:
		t.Fatalf("route B unexpectedly received concurrent address-zero frame %q", got)
	case <-time.After(50 * time.Millisecond):
	}

	_ = readFrame(t, clientA)
	_ = readFrame(t, clientB)
}

func TestPrepareRoutesRejectsDefaultAddressWithoutRoute(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := prepareRoutes(ctx, &RouterConfig{
		DefaultAddress: 0x4C,
		Routes: []Route{
			{Address: 0x4B, Downstream: func(context.Context) (net.Conn, string, error) {
				client, _ := net.Pipe()
				return client, "unused", nil
			}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "default address 0x4C") {
		t.Fatalf("prepareRoutes error = %v, want default address route error", err)
	}
}

func TestServeRouterFallsBackToDuplicateRouteForRead(t *testing.T) {
	fallbackClient, fallbackServer := net.Pipe()
	defer fallbackClient.Close()
	defer fallbackServer.Close()
	seen := serveDownstreamPipe(t, fallbackServer, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- ServeRouter(ctx, ln, &RouterConfig{
			RequestTimeout: time.Second,
			ReconnectDelay: time.Millisecond,
			Routes: []Route{
				{Address: 0x50, Target: "serial-primary", Downstream: func(context.Context) (net.Conn, string, error) {
					return nil, "", errors.New("serial offline")
				}},
				{Address: 0x50, Target: "can-fallback", Downstream: func(context.Context) (net.Conn, string, error) {
					return fallbackClient, "can-fallback-live", nil
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

	req := []byte("#500001?VR03E8010000\r")
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, req) {
		t.Fatalf("fallback route got %q, want %q", got, req)
	}
	if reply := readFrame(t, client); !bytes.Contains(reply, []byte("500001?VR03E8010000+")) {
		t.Fatalf("client got wrong fallback reply: %q", reply)
	}
}

func TestServeRouterDoesNotReplayWriteToDuplicateRoute(t *testing.T) {
	fallbackClient, fallbackServer := net.Pipe()
	defer fallbackClient.Close()
	defer fallbackServer.Close()
	seen := serveDownstreamPipe(t, fallbackServer, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- ServeRouter(ctx, ln, &RouterConfig{
			RequestTimeout: time.Second,
			ReconnectDelay: time.Millisecond,
			Routes: []Route{
				{Address: 0x50, Target: "serial-primary", Downstream: func(context.Context) (net.Conn, string, error) {
					return nil, "", errors.New("serial offline")
				}},
				{Address: 0x50, Target: "can-fallback", Downstream: func(context.Context) (net.Conn, string, error) {
					return fallbackClient, "can-fallback-live", nil
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

	req := []byte("#500001VS07DA0100000000\r")
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write: %v", err)
	}
	if reply := readFrame(t, client); !bytes.HasPrefix(reply, []byte("!500001-")) {
		t.Fatalf("client did not receive primary-route NACK: %q", reply)
	}
	select {
	case got := <-seen:
		t.Fatalf("fallback route unexpectedly received write frame %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestServeRouterHandlesVirtualWritesLocallyAndRoutesVirtualReads(t *testing.T) {
	deviceBridgeVirtualParameters.reset()

	primaryClient, primaryServer := net.Pipe()
	defer primaryClient.Close()
	defer primaryServer.Close()
	fallbackClient, fallbackServer := net.Pipe()
	defer fallbackClient.Close()
	defer fallbackServer.Close()

	primarySeen := make(chan []byte, 2)
	go func() {
		reader := bufio.NewReader(primaryServer)
		for {
			frame, err := reader.ReadBytes(mecom.FrameTerminator)
			if err != nil {
				return
			}
			primarySeen <- append([]byte(nil), frame...)
			req, err := parseDeviceBridgeRequest(frame)
			if err != nil {
				return
			}
			if req.payload == "?VRCBE801" {
				_, _ = primaryServer.Write(deviceBridgeInfoTestFrame(req.address, req.seq, "41480000"))
				continue
			}
			_, _ = primaryServer.Write(deviceBridgeNACK(req.address, req.seq, 0x03))
		}
	}()
	fallbackSeen := serveDownstreamPipe(t, fallbackServer, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- ServeRouter(ctx, ln, &RouterConfig{
			AddressZeroOrder: []byte{0x50, 0x51},
			RouteSelection:   RouteSelectionDynamic,
			RequestTimeout:   time.Second,
			Routes: []Route{
				{Address: 0x50, Target: "serial-primary", Downstream: func(context.Context) (net.Conn, string, error) {
					return primaryClient, "serial-primary-live", nil
				}},
				{Address: 0x51, Target: "can-fallback", Downstream: func(context.Context) (net.Conn, string, error) {
					return fallbackClient, "can-fallback-live", nil
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

	writeReq := deviceBridgeTestFrame(0, 1, "VSCBE80141200000")
	if _, err := client.Write(writeReq); err != nil {
		t.Fatalf("write virtual parameter: %v", err)
	}
	if reply := readFrame(t, client); !bytes.Equal(reply, deviceBridgeOK(0, 1, "")) {
		t.Fatalf("virtual write reply = %q, want router-local OK", reply)
	}
	select {
	case got := <-primarySeen:
		t.Fatalf("virtual write reached primary route: %q", got)
	case got := <-fallbackSeen:
		t.Fatalf("virtual write reached fallback route: %q", got)
	case <-time.After(50 * time.Millisecond):
	}

	readReq := deviceBridgeTestFrame(0, 2, "?VRCBE801")
	if _, err := client.Write(readReq); err != nil {
		t.Fatalf("read virtual parameter: %v", err)
	}
	if reply := readFrame(t, client); !bytes.Equal(reply, deviceBridgeInfoTestFrame(0, 2, "41480000")) {
		t.Fatalf("virtual read reply = %q, want downstream live value", reply)
	}

	select {
	case got := <-primarySeen:
		want := deviceBridgeTestFrame(0x50, 2, "?VRCBE801")
		if !bytes.Equal(got, want) {
			t.Fatalf("virtual read reached primary route as %q, want %q", got, want)
		}
	case got := <-fallbackSeen:
		t.Fatalf("virtual read reached fallback route: %q", got)
	case <-time.After(time.Second):
		t.Fatalf("virtual read did not reach primary route")
	}
}

func TestServeRouterDoesNotForwardDuplicateWriteFrame(t *testing.T) {
	routeClient, routeServer := net.Pipe()
	defer routeClient.Close()
	defer routeServer.Close()
	seen := serveDownstreamPipe(t, routeServer, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- ServeRouter(ctx, ln, &RouterConfig{
			RequestTimeout: time.Second,
			ReconnectDelay: time.Millisecond,
			Routes: []Route{
				{Address: 0x50, Target: "serial-primary", Downstream: func(context.Context) (net.Conn, string, error) {
					return routeClient, "serial-primary-live", nil
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

	req := []byte("#500001VS07DA0100000000\r")
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, req) {
		t.Fatalf("route got %q, want %q", got, req)
	}
	replyA := readFrame(t, client)

	if _, err := client.Write(req); err != nil {
		t.Fatalf("write duplicate: %v", err)
	}
	replyB := readFrame(t, client)
	if !bytes.Equal(replyA, replyB) {
		t.Fatalf("duplicate reply = %q, want cached %q", replyB, replyA)
	}

	select {
	case got := <-seen:
		t.Fatalf("duplicate write was forwarded to downstream: %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestServeRouterCoalescesInflightDuplicateWriteFrames(t *testing.T) {
	routeClient, routeServer := net.Pipe()
	defer routeClient.Close()
	defer routeServer.Close()
	seen := make(chan []byte, 2)
	release := make(chan struct{})
	go func() {
		reader := bufio.NewReader(routeServer)
		frame, err := reader.ReadBytes('\r')
		if err != nil {
			return
		}
		seen <- append([]byte(nil), frame...)
		<-release
		reply := []byte("!" + string(frame[1:len(frame)-1]) + "+0000\r")
		_, _ = routeServer.Write(reply)

		frame, err = reader.ReadBytes('\r')
		if err != nil {
			return
		}
		seen <- append([]byte(nil), frame...)
		_, _ = routeServer.Write(reply)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- ServeRouter(ctx, ln, &RouterConfig{
			RequestTimeout: time.Second,
			ReconnectDelay: time.Millisecond,
			Routes: []Route{
				{Address: 0x50, Target: "serial-primary", Downstream: func(context.Context) (net.Conn, string, error) {
					return routeClient, "serial-primary-live", nil
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

	clientA := dialClient(t, ln.Addr().String())
	defer clientA.Close()
	clientB := dialClient(t, ln.Addr().String())
	defer clientB.Close()

	req := []byte("#500001VS07DA0100000000\r")
	if _, err := clientA.Write(req); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, req) {
		t.Fatalf("route got %q, want %q", got, req)
	}
	if _, err := clientB.Write(req); err != nil {
		t.Fatalf("write duplicate: %v", err)
	}
	close(release)

	replyA := readFrame(t, clientA)
	replyB := readFrame(t, clientB)
	if !bytes.Equal(replyA, replyB) {
		t.Fatalf("in-flight duplicate reply = %q, want cached %q", replyB, replyA)
	}
	select {
	case got := <-seen:
		t.Fatalf("in-flight duplicate write was forwarded to downstream: %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestOrderRouteCandidatesFixedPreferenceKeepsConfiguredPrimary(t *testing.T) {
	primary := testRouteBroker(0x50, "serial-primary")
	fallback := testRouteBroker(0x50, "can-fallback")
	primary.stats.markError(errors.New("recent serial timeout"))
	fallback.stats.markConnected("can-fallback-live")

	got := orderRouteCandidates([]*routeBroker{primary, fallback}, RouteSelectionFixedPreference, true)
	if got[0] != primary {
		t.Fatalf("fixed preference chose %q first, want configured primary %q", got[0].target, primary.target)
	}
}

func TestOrderRouteCandidatesDynamicCanPreferHealthyReadFallback(t *testing.T) {
	primary := testRouteBroker(0x50, "serial-primary")
	fallback := testRouteBroker(0x50, "can-fallback")
	primary.stats.markError(errors.New("recent serial timeout"))
	fallback.stats.markConnected("can-fallback-live")

	got := orderRouteCandidates([]*routeBroker{primary, fallback}, RouteSelectionDynamic, true)
	if got[0] != fallback {
		t.Fatalf("dynamic routing chose %q first, want healthy fallback %q", got[0].target, fallback.target)
	}
}

func TestOrderRouteCandidatesDynamicKeepsHealthyConfiguredPrimaryForReads(t *testing.T) {
	primary := testRouteBroker(0x50, "can-primary")
	fallback := testRouteBroker(0x50, "serial-fallback")
	fallback.priority = 1
	fallback.stats.markConnected("serial-fallback-live")

	got := orderRouteCandidates([]*routeBroker{primary, fallback}, RouteSelectionDynamic, true)
	if got[0] != primary {
		t.Fatalf("dynamic read routing chose %q first, want configured primary %q", got[0].target, primary.target)
	}
}

func TestOrderRouteCandidatesDynamicKeepsConfiguredPrimaryForWrites(t *testing.T) {
	primary := testRouteBroker(0x50, "serial-primary")
	fallback := testRouteBroker(0x50, "can-fallback")
	primary.stats.markError(errors.New("recent serial timeout"))
	fallback.stats.markConnected("can-fallback-live")

	got := orderRouteCandidates([]*routeBroker{primary, fallback}, RouteSelectionDynamic, false)
	if got[0] != primary {
		t.Fatalf("dynamic write routing chose %q first, want configured primary %q", got[0].target, primary.target)
	}
}

func TestRouteFrameRetriesTransientReadOnSameCandidate(t *testing.T) {
	candidate := testRouteBroker(0x50, "can-primary")
	reqFrame := []byte("#500001?IF03E8010000\r")
	reply := []byte("!500001-010000\r")
	seen := make(chan []byte, 2)
	go func() {
		for i := 0; i < 2; i++ {
			req := <-candidate.requests
			seen <- append([]byte(nil), req.frame...)
			if i == 0 {
				req.result <- response{err: io.ErrClosedPipe}
				continue
			}
			req.result <- response{frame: reply}
		}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{candidate}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, reply) {
		t.Fatalf("routeFrame reply = %q, want %q", got, reply)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, reqFrame) {
		t.Fatalf("first routed frame = %q, want %q", got, reqFrame)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, reqFrame) {
		t.Fatalf("retry routed frame = %q, want %q", got, reqFrame)
	}
}

func TestRouteFrameRetriesStaleTCPWriteOnReadCandidate(t *testing.T) {
	candidate := testRouteBroker(0x4b, "serial-fallback")
	reqFrame := testMeComFrame(0x4b, 1, "?VR006601")
	reply := deviceBridgeInfoTestFrame(0x4b, 1, "0000004B")
	seen := make(chan []byte, 2)
	go func() {
		for i := 0; i < 2; i++ {
			req := <-candidate.requests
			seen <- append([]byte(nil), req.frame...)
			if i == 0 {
				req.result <- response{err: fmt.Errorf("write tcp: %w", syscall.EPIPE)}
				continue
			}
			req.result <- response{frame: reply}
		}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{candidate}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, reply) {
		t.Fatalf("routeFrame reply = %q, want %q", got, reply)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, reqFrame) {
		t.Fatalf("first routed frame = %q, want %q", got, reqFrame)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, reqFrame) {
		t.Fatalf("retry routed frame = %q, want %q", got, reqFrame)
	}
}

func TestRouteFramePrefersNativeMeComForVirtualReads(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		reply   string
	}{
		{name: "metadata", payload: "?VMCBE801", reply: "00030100000001FF8000007F80000041480000"},
		{name: "single", payload: "?VRCBE801", reply: "41480000"},
		{name: "bulk", payload: "?VX01CBE801", reply: "41480000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			primary := testRouteBroker(0x4b, "can:can0/0x4b")
			fallback := testRouteBroker(0x4b, "tcp:127.0.0.1:51075")
			reqFrame := testMeComFrame(0x4b, 1, tc.payload)
			reply := deviceBridgeInfoTestFrame(0x4b, 1, tc.reply)

			go func() {
				req := <-fallback.requests
				if !bytes.Equal(req.frame, reqFrame) {
					req.result <- response{err: fmt.Errorf("fallback request frame = %q, want %q", string(req.frame), string(reqFrame))}
					return
				}
				req.result <- response{frame: reply}
			}()

			got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{primary, fallback}, time.Second, false, nil, "test")
			if err != nil {
				t.Fatalf("routeFrame returned error: %v", err)
			}
			if !bytes.Equal(got, reply) {
				t.Fatalf("routeFrame reply = %q, want %q", got, reply)
			}
			select {
			case req := <-primary.requests:
				t.Fatalf("CAN route unexpectedly received virtual read %q", string(req.frame))
			default:
			}
		})
	}
}

func TestRouteFrameObservesVirtualMetadataActualIntoDeviceCache(t *testing.T) {
	dir := withDeviceBridgeCacheDir(t)
	candidate := testRouteBroker(0x4b, "tcp:127.0.0.1:51075")
	reqFrame := testMeComFrame(0x4b, 1, "?VMCBE801")
	payload := "00030100000001FF8000007F80000041480000"
	reply := deviceBridgeInfoTestFrame(0x4b, 1, payload)

	go func() {
		req := <-candidate.requests
		if !bytes.Equal(req.frame, reqFrame) {
			req.result <- response{err: fmt.Errorf("request frame = %q, want %q", string(req.frame), string(reqFrame))}
			return
		}
		req.result <- response{frame: reply}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{candidate}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, reply) {
		t.Fatalf("routeFrame reply = %q, want %q", got, reply)
	}

	snap := readSingleDeviceBridgeCacheSnapshot(t, dir)
	param := findDeviceBridgeCacheParam(t, snap, 52200, 1)
	if param.Float32 == nil || *param.Float32 != 12.5 {
		t.Fatalf("observed virtual metadata cache float32 = %v, want 12.5", param.Float32)
	}
	if param.Source != deviceBridgeCacheSourceDownstream || !param.LiveRefresh || param.UpdatedAt == "" {
		t.Fatalf("cache metadata = source %q live %v updated %q, want downstream live timestamped", param.Source, param.LiveRefresh, param.UpdatedAt)
	}
}

func TestRouteFrameRewritesLegacyFirmwareVersionProbe(t *testing.T) {
	candidate := testRouteBroker(0x50, "serial-primary")
	reqFrame := testMeComFrame(0x50, 1, "?VI")
	wantFrame := testMeComFrame(0x50, 1, "?IF")
	reply := []byte("!5000018065-TEC SW G01     0907\r")
	seen := make(chan []byte, 1)
	go func() {
		req := <-candidate.requests
		seen <- append([]byte(nil), req.frame...)
		req.result <- response{frame: reply}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{candidate}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, reply) {
		t.Fatalf("routeFrame reply = %q, want %q", got, reply)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, wantFrame) {
		t.Fatalf("routed firmware probe = %q, want %q", got, wantFrame)
	}
}

func TestRouteFrameRewritesBareLegacyFirmwareVersionProbe(t *testing.T) {
	candidate := testRouteBroker(0x50, "serial-primary")
	reqFrame := []byte("?VI\r")
	wantFrame := []byte("?IF\r")
	reply := []byte("!0000008065-TEC SW G01     50F5\r")
	seen := make(chan []byte, 1)
	go func() {
		req := <-candidate.requests
		seen <- append([]byte(nil), req.frame...)
		req.result <- response{frame: reply}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{candidate}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, reply) {
		t.Fatalf("routeFrame reply = %q, want %q", got, reply)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, wantFrame) {
		t.Fatalf("bare routed firmware probe = %q, want %q", got, wantFrame)
	}
}

func TestRouteFrameDoesNotRetryTransientWriteOnSameCandidate(t *testing.T) {
	candidate := testRouteBroker(0x50, "can-primary")
	reqFrame := []byte("#500001VS07DA0100000000\r")
	seen := make(chan []byte, 2)
	go func() {
		req := <-candidate.requests
		seen <- append([]byte(nil), req.frame...)
		req.result <- response{err: io.ErrClosedPipe}
	}()

	_, err := routeFrame(context.Background(), reqFrame, []*routeBroker{candidate}, time.Second, false, nil, "test")
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("routeFrame error = %v, want io.ErrClosedPipe", err)
	}
	if got := receiveSeen(t, seen); !bytes.Equal(got, reqFrame) {
		t.Fatalf("routed write frame = %q, want %q", got, reqFrame)
	}
	select {
	case got := <-seen:
		t.Fatalf("write was retried: %q", got)
	case <-time.After(50 * time.Millisecond):
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
	reply := readFrame(t, client)
	if !bytes.HasPrefix(reply, []byte("!520001-")) {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestServeRouterRejectsUnknownAddressWithMeComNACK(t *testing.T) {
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
	reply := readFrame(t, client)
	if !bytes.HasPrefix(reply, []byte("!520001-")) {
		t.Fatalf("reply is not addressed MeCom NACK: %q", reply)
	}
	if err := mecom.ParseWriteResponse(reply); err == nil || !strings.Contains(err.Error(), "GENERAL_COM") {
		t.Fatalf("reply is not parseable GENERAL_COM NACK, err=%v frame=%q", err, reply)
	}
}

func TestHandleClientTimesOutIdleConnection(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	requests := make(chan request)
	done := make(chan struct{})
	go func() {
		handleClient(context.Background(), server, requests, Config{ClientIdleTimeout: 20 * time.Millisecond})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("idle client handler did not return after configured timeout")
	}
}

func TestReadBoundedFrameRejectsOversizedUnterminatedFrame(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", maxClientFrameBytes+1)), 32)
	if _, err := readBoundedFrame(reader, maxClientFrameBytes); err == nil {
		t.Fatal("readBoundedFrame accepted oversized unterminated frame")
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
	if reply := readFrame(t, client); !bytes.HasPrefix(reply, []byte("!520001-")) {
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

func testRouteBroker(addr byte, target string) *routeBroker {
	routeID := fmt.Sprintf("0x%02X:0:%s", addr, target)
	return &routeBroker{
		address:  addr,
		target:   target,
		requests: make(chan request, 256),
		stats:    newBrokerStatsRecorder(addr, target, routeID, 0),
		state:    newDeviceBridgeState(routeID),
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

func testMeComFrame(addr byte, seq uint16, payload string) []byte {
	prefix := []byte(fmt.Sprintf("#%02X%04X%s", addr, seq, payload))
	return []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16(prefix), mecom.FrameTerminator))
}

func TestRouteFrameObservesSuccessfulReadIntoDeviceCache(t *testing.T) {
	dir := withDeviceBridgeCacheDir(t)
	candidate := testRouteBroker(0x4b, "serial:/dev/ttyUSB0@57600")

	reqFrame := testMeComFrame(0x4b, 1, "?VRCB2203")
	okReply := deviceBridgeInfoTestFrame(0x4b, 1, "00000007")

	go func() {
		req := <-candidate.requests
		if !bytes.Equal(req.frame, reqFrame) {
			req.result <- response{err: fmt.Errorf("request frame = %q, want %q", string(req.frame), string(reqFrame))}
			return
		}
		req.result <- response{frame: okReply}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{candidate}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, okReply) {
		t.Fatalf("routeFrame reply = %q, want %q", string(got), string(okReply))
	}

	snap := readSingleDeviceBridgeCacheSnapshot(t, dir)
	param := findDeviceBridgeCacheParam(t, snap, 52002, 3)
	if param.Int32 == nil || *param.Int32 != 7 {
		t.Fatalf("observed route cache int32 = %v, want 7", param.Int32)
	}
	if param.Source != deviceBridgeCacheSourceDownstream || !param.LiveRefresh || param.UpdatedAt == "" {
		t.Fatalf("cache metadata = source %q live %v updated %q, want downstream live timestamped", param.Source, param.LiveRefresh, param.UpdatedAt)
	}
}

// TestRouteFrameFallsThroughOnCMDNotAvailable verifies that a read frame which
// receives NACK 0x01 (CMD_NOT_AVAILABLE) from the primary route is retried on
// the next route rather than returned to the client. This covers the serial →
// CAN fallback for commands (e.g. ?VX bulk read) that serial hardware does not
// implement but the CAN bridge does.
func TestRouteFrameFallsThroughOnCMDNotAvailable(t *testing.T) {
	primary := testRouteBroker(0x50, "serial-primary")
	fallback := testRouteBroker(0x50, "can-fallback")

	reqFrame := testMeComFrame(0x50, 1, "?VX010068010000")
	// Primary returns NACK 0x01 (CMD_NOT_AVAILABLE): serial hw doesn't support ?VX.
	nackReply := []byte("!500001-010000\r")
	// Fallback returns a valid bulk-read response.
	okReply := []byte("!50000100000000ABCD\r")

	go func() {
		req := <-primary.requests
		req.result <- response{frame: nackReply}
	}()
	go func() {
		req := <-fallback.requests
		req.result <- response{frame: okReply}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{primary, fallback}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, okReply) {
		t.Fatalf("routeFrame reply = %q, want %q (fallback)", got, okReply)
	}
}

// TestRouteFrameFallsThroughOnIdentityParameterUnavailable verifies CoSo's
// serial-number device-selection read can fall through from a transport that
// cannot answer the parameter to one that can.
func TestRouteFrameFallsThroughOnIdentityParameterUnavailable(t *testing.T) {
	primary := testRouteBroker(0x4b, "can-primary")
	fallback := testRouteBroker(0x4b, "serial-fallback")

	reqFrame := testMeComFrame(0x4b, 1, "?VR006601")
	nackReply := deviceBridgeNACK(0x4b, 1, 0x03)
	okReply := deviceBridgeInfoTestFrame(0x4b, 1, "0000004B")

	go func() {
		req := <-primary.requests
		if !bytes.Equal(req.frame, reqFrame) {
			req.result <- response{err: fmt.Errorf("primary request frame = %q, want %q", string(req.frame), string(reqFrame))}
			return
		}
		req.result <- response{frame: nackReply}
	}()
	go func() {
		req := <-fallback.requests
		if !bytes.Equal(req.frame, reqFrame) {
			req.result <- response{err: fmt.Errorf("fallback request frame = %q, want %q", string(req.frame), string(reqFrame))}
			return
		}
		req.result <- response{frame: okReply}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{primary, fallback}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, okReply) {
		t.Fatalf("routeFrame reply = %q, want %q (fallback)", got, okReply)
	}
}

// TestRouteFrameFallsThroughOnCatalogueParameterUnavailable verifies CoSo's
// early firmware-version gate can fall through from a CAN route that cannot
// expose the scalar to a serial route that can, but only because the parameter
// is present in the canonical catalogue.
func TestRouteFrameFallsThroughOnCatalogueParameterUnavailable(t *testing.T) {
	primary := testRouteBroker(0x4b, "can-primary")
	fallback := testRouteBroker(0x4b, "serial-fallback")

	reqFrame := testMeComFrame(0x4b, 1, "?VR007001")
	nackReply := deviceBridgeNACK(0x4b, 1, 0x03)
	okReply := deviceBridgeInfoTestFrame(0x4b, 1, "40C9EB85")

	go func() {
		req := <-primary.requests
		if !bytes.Equal(req.frame, reqFrame) {
			req.result <- response{err: fmt.Errorf("primary request frame = %q, want %q", string(req.frame), string(reqFrame))}
			return
		}
		req.result <- response{frame: nackReply}
	}()
	go func() {
		req := <-fallback.requests
		if !bytes.Equal(req.frame, reqFrame) {
			req.result <- response{err: fmt.Errorf("fallback request frame = %q, want %q", string(req.frame), string(reqFrame))}
			return
		}
		req.result <- response{frame: okReply}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{primary, fallback}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, okReply) {
		t.Fatalf("routeFrame reply = %q, want %q (fallback)", got, okReply)
	}
}

func TestRouteFrameDoesNotTreatCANopenNVCByteConfigAsSerialFallback(t *testing.T) {
	for _, payload := range []string{
		"?VM086601",
		"?VR086601",
		"?VX010866010000",
	} {
		if isRouteUnsupportedSerialFallbackRead(testMeComFrame(0x4b, 1, payload)) {
			t.Fatalf("payload %q unexpectedly treated CANopen NVC byte/PDO config as serial fallback", payload)
		}
	}
}

func TestRouteFrameRewritesAddressZeroToCandidateAndRestoresResponse(t *testing.T) {
	candidate := testRouteBroker(0x4b, "serial")

	clientReq := testMeComFrame(0x00, 1, "?VR006601")
	downstreamReq := testMeComFrame(0x4b, 1, "?VR006601")
	downstreamReply := deviceBridgeInfoTestFrame(0x4b, 1, "0000004B")
	clientReply := deviceBridgeInfoTestFrame(0x00, 1, "0000004B")

	go func() {
		req := <-candidate.requests
		if !bytes.Equal(req.frame, downstreamReq) {
			req.result <- response{err: fmt.Errorf("downstream request frame = %q, want %q", string(req.frame), string(downstreamReq))}
			return
		}
		req.result <- response{frame: downstreamReply}
	}()

	got, err := routeFrame(context.Background(), clientReq, []*routeBroker{candidate}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, clientReply) {
		t.Fatalf("routeFrame reply = %q, want %q", got, clientReply)
	}
}

func TestRouteFrameDoesNotFallThroughOnParameterUnavailableForUnknownRead(t *testing.T) {
	primary := testRouteBroker(0x50, "can-primary")
	fallback := testRouteBroker(0x50, "serial-fallback")

	reqFrame := testMeComFrame(0x50, 1, "?VRC35001")
	nackReply := deviceBridgeNACK(0x50, 1, 0x03)

	go func() {
		req := <-primary.requests
		if !bytes.Equal(req.frame, reqFrame) {
			req.result <- response{err: fmt.Errorf("primary request frame = %q, want %q", string(req.frame), string(reqFrame))}
			return
		}
		req.result <- response{frame: nackReply}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{primary, fallback}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, nackReply) {
		t.Fatalf("routeFrame reply = %q, want %q (primary NACK passed through)", got, nackReply)
	}
}

func TestRouteFrameDoesNotFallThroughOnUnsupportedDebugFlashMetadata(t *testing.T) {
	primary := testRouteBroker(0x50, "can-primary")
	fallback := testRouteBroker(0x50, "serial-fallback")

	reqFrame := testMeComFrame(0x50, 1, "?VMC35001")
	nackReply := deviceBridgeNACK(0x50, 1, 0x05)

	go func() {
		req := <-primary.requests
		if !bytes.Equal(req.frame, reqFrame) {
			req.result <- response{err: fmt.Errorf("primary request frame = %q, want %q", string(req.frame), string(reqFrame))}
			return
		}
		req.result <- response{frame: nackReply}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{primary, fallback}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, nackReply) {
		t.Fatalf("routeFrame reply = %q, want %q (primary NACK passed through)", got, nackReply)
	}
	select {
	case req := <-fallback.requests:
		t.Fatalf("fallback unexpectedly received request %q", string(req.frame))
	default:
	}
}

// TestRouteFrameDoesNotFallThroughOnOtherNACKCodesForOrdinaryRead verifies
// non-whitelisted NACK codes are returned directly without trying the next route.
func TestRouteFrameDoesNotFallThroughOnOtherNACKCodes(t *testing.T) {
	primary := testRouteBroker(0x50, "serial-primary")
	fallback := testRouteBroker(0x50, "can-fallback")

	reqFrame := testMeComFrame(0x50, 1, "?VR006801")
	// NACK 0x05 = unknown parameter — a real device error, not a transport gap.
	nackReply := []byte("!500001-050000\r")

	go func() {
		req := <-primary.requests
		req.result <- response{frame: nackReply}
	}()

	got, err := routeFrame(context.Background(), reqFrame, []*routeBroker{primary, fallback}, time.Second, false, nil, "test")
	if err != nil {
		t.Fatalf("routeFrame returned error: %v", err)
	}
	if !bytes.Equal(got, nackReply) {
		t.Fatalf("routeFrame reply = %q, want %q (primary NACK passed through)", got, nackReply)
	}
}
