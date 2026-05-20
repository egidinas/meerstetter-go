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
	clientB := dialClient(t, ln.Addr().String())
	defer clientB.Close()
	if _, err := clientB.Write(reqB); err != nil {
		t.Fatalf("write B: %v", err)
	}
	if got := receiveSeen(t, seenB); !bytes.Equal(got, reqB) {
		t.Fatalf("route B got %q, want unchanged frame %q", got, reqB)
	}
	_ = readFrame(t, clientB)
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
	return &routeBroker{
		address:  addr,
		target:   target,
		requests: make(chan request, 256),
		stats:    newBrokerStatsRecorder(addr, target, fmt.Sprintf("0x%02X:0:%s", addr, target), 0),
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
