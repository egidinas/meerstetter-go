package mecom

import (
	"testing"
	"time"
)

func TestProxyServerStopIsIdempotent(t *testing.T) {
	srv := NewProxyServer("127.0.0.1:0", &Client{})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	srv.Stop()
	done := make(chan struct{})
	go func() {
		srv.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second Stop() did not return")
	}
}
