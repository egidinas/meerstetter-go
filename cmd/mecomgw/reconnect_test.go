package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

type reconnectFailingDevice struct {
	err    error
	closed bool
}

func (f *reconnectFailingDevice) ReadBulk(context.Context, []mecom.Parameter) ([]float64, error) {
	return nil, f.err
}

func (f *reconnectFailingDevice) WriteFloat32(context.Context, int, int, float32) error {
	return mecom.ErrTransportNotSupported
}

func (f *reconnectFailingDevice) WriteInt32(context.Context, int, int, int32) error {
	return mecom.ErrTransportNotSupported
}

func (f *reconnectFailingDevice) ConfigureRingCapture(context.Context, uint16, []mecom.RingCaptureParameter) error {
	return mecom.ErrTransportNotSupported
}

func (f *reconnectFailingDevice) TriggerRingSync(context.Context) error {
	return mecom.ErrTransportNotSupported
}

func (f *reconnectFailingDevice) ReadRingPointer(context.Context) (uint32, error) {
	return 0, mecom.ErrTransportNotSupported
}

func (f *reconnectFailingDevice) ReadRingChunk(context.Context, uint32, uint16) (mecom.RingReadResponse, error) {
	return mecom.RingReadResponse{}, mecom.ErrTransportNotSupported
}

func (f *reconnectFailingDevice) Close() error {
	f.closed = true
	return nil
}

func TestGatewayReadTransportErrorClearsMemoizedClientAfterFailure(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	failing := &reconnectFailingDevice{err: fmt.Errorf("%w: link dropped", mecom.ErrUnreachable)}
	s.devices["tec-75"].client = failing
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	getJSON(t, ts.URL+"/api/devices/tec-75/read?params=1000:1", http.StatusServiceUnavailable, nil)
	b := s.devices["tec-75"]
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != nil {
		t.Fatalf("client still memoized after transport failure: %#v", b.client)
	}
	if b.commander != nil {
		t.Fatalf("commander still memoized after transport failure: %#v", b.commander)
	}
	if !failing.closed {
		t.Fatal("failing client was not closed")
	}
}

type reconnectFailingWriter struct {
	err    error
	closed bool
}

func (f *reconnectFailingWriter) ReadBulk(context.Context, []mecom.Parameter) ([]float64, error) {
	return nil, mecom.ErrTransportNotSupported
}

func (f *reconnectFailingWriter) WriteFloat32(context.Context, int, int, float32) error {
	return f.err
}

func (f *reconnectFailingWriter) WriteInt32(context.Context, int, int, int32) error {
	return f.err
}

func (f *reconnectFailingWriter) ConfigureRingCapture(context.Context, uint16, []mecom.RingCaptureParameter) error {
	return mecom.ErrTransportNotSupported
}

func (f *reconnectFailingWriter) TriggerRingSync(context.Context) error {
	return mecom.ErrTransportNotSupported
}

func (f *reconnectFailingWriter) ReadRingPointer(context.Context) (uint32, error) {
	return 0, mecom.ErrTransportNotSupported
}

func (f *reconnectFailingWriter) ReadRingChunk(context.Context, uint32, uint16) (mecom.RingReadResponse, error) {
	return mecom.RingReadResponse{}, mecom.ErrTransportNotSupported
}

func (f *reconnectFailingWriter) Close() error {
	f.closed = true
	return nil
}

func TestGatewayWriteTransportErrorClearsMemoizedClientAfterFailure(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	failing := &reconnectFailingWriter{err: errors.New("broken pipe")}
	s.devices["tec-75"].client = failing
	cmdr := mecom.NewCommander(failing, 10*time.Millisecond)
	cmdr.TargetID = "tec-75"
	cmdr.Authorizer = mecom.AuthorizerFunc(s.authorize)
	s.devices["tec-75"].commander = cmdr
	lease, err := s.leases.Acquire("tec-75", "operator", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	reqBody := map[string]any{
		"name":      "write_int32",
		"arguments": map[string]any{"param": 1000, "instance": 1, "value": 7},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/devices/tec-75/write", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Lease-Token", lease.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST write status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	b := s.devices["tec-75"]
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != nil {
		t.Fatalf("client still memoized after write transport failure: %#v", b.client)
	}
	if b.commander != nil {
		t.Fatalf("commander still memoized after write transport failure: %#v", b.commander)
	}
	if !failing.closed {
		t.Fatal("failing writer was not closed")
	}
}

func TestGatewayPollTransportErrorClearsMemoizedClientAfterFailure(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	failing := &reconnectFailingDevice{err: errors.New("serial read: broken pipe")}
	s.devices["tec-75"].client = failing
	cmdr := mecom.NewCommander(failing, 10*time.Millisecond)
	cmdr.TargetID = "tec-75"
	cmdr.Authorizer = mecom.AuthorizerFunc(s.authorize)
	s.devices["tec-75"].commander = cmdr
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/devices/tec-75/poll?params=1000:1&interval=1h", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET poll status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") {
			break
		}
	}
	cancel()

	b := s.devices["tec-75"]
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != nil {
		t.Fatalf("client still memoized after poll transport failure: %#v", b.client)
	}
	if b.commander != nil {
		t.Fatalf("commander still memoized after poll transport failure: %#v", b.commander)
	}
	if !failing.closed {
		t.Fatal("failing poll client was not closed")
	}
}
