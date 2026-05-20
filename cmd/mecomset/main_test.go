package main

import (
	"bufio"
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

func TestRunVerifyFailsOnMismatchedReadback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		if _, err := reader.ReadString(mecom.FrameTerminator); err != nil {
			done <- err
			return
		}
		if _, err := conn.Write(testResponseFrame("!000001+")); err != nil {
			done <- err
			return
		}
		if _, err := reader.ReadString(mecom.FrameTerminator); err != nil {
			done <- err
			return
		}
		_, err = conn.Write(testResponseFrame("!000002+0000002A"))
		done <- err
	}()

	err = run(context.Background(), []string{"tcp:" + ln.Addr().String()}, 0, []setSpec{intSetSpec(1000, 1, 7)}, 500*time.Millisecond, true, false, false)
	if err == nil || !strings.Contains(err.Error(), "operation") {
		t.Fatalf("run verify mismatch err = %v, want operation failure", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("test server failed: %v", err)
	}
}

func TestRunWritesCatalogueFloatParameterAsFloatFrame(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	requests := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		req, err := reader.ReadString(mecom.FrameTerminator)
		if err != nil {
			done <- err
			return
		}
		requests <- req
		_, err = conn.Write(testResponseFrame("!000001+"))
		done <- err
	}()

	specs, err := parseSetSpecs(setFlags{"2021:2=28.5"})
	if err != nil {
		t.Fatalf("parseSetSpecs: %v", err)
	}
	err = run(context.Background(), []string{"tcp:" + ln.Addr().String()}, 0, specs, 500*time.Millisecond, false, false, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("test server failed: %v", err)
	}
	got := <-requests
	want := string(mecom.BuildWriteFloat32Frame(0, 1, 2021, 2, 28.5))
	if got != want {
		t.Fatalf("write request = %q, want float32 frame %q", got, want)
	}
}

func TestRunSkipsSaveAndResetAfterVerifyFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	requests := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		req, err := reader.ReadString(mecom.FrameTerminator)
		if err != nil {
			done <- err
			return
		}
		requests <- req
		if _, err := conn.Write(testResponseFrame("!000001+")); err != nil {
			done <- err
			return
		}
		req, err = reader.ReadString(mecom.FrameTerminator)
		if err != nil {
			done <- err
			return
		}
		requests <- req
		if _, err := conn.Write(testResponseFrame("!000002+0000002A")); err != nil {
			done <- err
			return
		}
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, err = reader.ReadString(mecom.FrameTerminator)
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			done <- nil
			return
		}
		if errors.Is(err, io.EOF) {
			done <- nil
			return
		}
		done <- fmt.Errorf("unexpected request after failed verify: %v", err)
	}()

	err = run(context.Background(), []string{"tcp:" + ln.Addr().String()}, 0, []setSpec{intSetSpec(1000, 1, 7)}, 500*time.Millisecond, true, true, true)
	if err == nil || !strings.Contains(err.Error(), "operation") {
		t.Fatalf("run err = %v, want operation failure", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("test server failed: %v", err)
	}
	if got := len(requests); got != 2 {
		t.Fatalf("server saw %d request(s), want write+verify only", got)
	}
}

func TestRunSkipsResetAfterSaveFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	requests := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		req, err := reader.ReadString(mecom.FrameTerminator)
		if err != nil {
			done <- err
			return
		}
		requests <- req
		if _, err := conn.Write(testResponseFrame("!000001+")); err != nil {
			done <- err
			return
		}
		req, err = reader.ReadString(mecom.FrameTerminator)
		if err != nil {
			done <- err
			return
		}
		requests <- req
		if _, err := conn.Write(testResponseFrame("!000002-05")); err != nil {
			done <- err
			return
		}
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, err = reader.ReadString(mecom.FrameTerminator)
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			done <- nil
			return
		}
		if errors.Is(err, io.EOF) {
			done <- nil
			return
		}
		done <- fmt.Errorf("unexpected request after failed save: %v", err)
	}()

	err = run(context.Background(), []string{"tcp:" + ln.Addr().String()}, 0, []setSpec{intSetSpec(1000, 1, 7)}, 500*time.Millisecond, false, true, true)
	if err == nil || !strings.Contains(err.Error(), "operation") {
		t.Fatalf("run err = %v, want operation failure", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("test server failed: %v", err)
	}
	if got := len(requests); got != 2 {
		t.Fatalf("server saw %d request(s), want write+save only", got)
	}
}

func TestValidateSetOptionsRejectsNonPositiveTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Millisecond} {
		if err := validateSetOptions(timeout); err == nil {
			t.Fatalf("validateSetOptions(%s) returned nil error", timeout)
		}
	}
	if err := validateSetOptions(time.Millisecond); err != nil {
		t.Fatalf("validateSetOptions valid timeout: %v", err)
	}
}

func testResponseFrame(prefix string) []byte {
	return []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator))
}

func intSetSpec(paramID, instance int, value int32) setSpec {
	return setSpec{ParamID: paramID, Instance: instance, Type: mecom.DataTypeInt32, IntValue: value}
}
