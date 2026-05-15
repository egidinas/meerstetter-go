package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
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
