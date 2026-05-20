package mecomserver

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

func TestRouterRuntimeReadBulkRoutesOneVXAndParsesReply(t *testing.T) {
	requests := make(chan request, 1)
	rt := &RouterRuntime{
		routes: preparedRoutes{
			0x50: []*routeBroker{{
				address:  0x50,
				target:   "test",
				priority: 0,
				requests: requests,
			}},
		},
		clientCfg: routedClientConfig{
			RequestTimeout: 50 * time.Millisecond,
		},
	}
	go func() {
		req := <-requests
		if got := string(req.frame); !strings.Contains(got, "?VX02") {
			req.result <- response{err: fmt.Errorf("unexpected frame %q", got)}
			return
		}
		req.result <- response{frame: testResponseFrame(0x50, 1, "+3FA0000040200000")}
	}()
	client := rt.ReadClient(0x50)

	got, err := client.ReadBulk(context.Background(), []mecom.Parameter{
		{ID: 1000, Instance: 1, Type: mecom.DataTypeFloat32},
		{ID: 1001, Instance: 1, Type: mecom.DataTypeFloat32},
	})
	if err != nil {
		t.Fatalf("ReadBulk: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d values, want 2", len(got))
	}
	if got[0] != 1.25 || got[1] != 2.5 {
		t.Fatalf("got values %v, want [1.25 2.5]", got)
	}

}

func TestRouterPollSchedulerFrontAndBackgroundShareOnePoll(t *testing.T) {
	rt, requests := testRouterRuntimeForAddress(t, 0x50)
	go func() {
		req := <-requests
		req.result <- response{frame: testResponseFrame(0x50, 1, "+3FA0000040200000")}
	}()
	sched := NewRouterPollScheduler(RouterPollSchedulerConfig{
		Router: rt,
		Devices: []RouterPollDeviceConfig{
			{
				Address:          0x50,
				SupportsRingRead: boolPtr(false),
				Readout: mecom.ReadoutConfig{
					Parameters: []mecom.ReadoutParameter{
						{Parameter: mecom.Parameter{ID: 1000, Instance: 1, Type: mecom.DataTypeFloat32}, Sensor: "bg"},
					},
					BulkChunk: 2,
				},
			},
		},
	})
	sched.EnqueueFront(0x50, mecom.ReadoutParameter{
		Parameter: mecom.Parameter{ID: 2000, Instance: 1, Type: mecom.DataTypeFloat32},
		Sensor:    "front",
	})

	batch := sched.PollOnce(context.Background())
	device := batch[0x50]
	if len(device.Values) != 2 {
		t.Fatalf("got %d values, want 2", len(device.Values))
	}
	if device.Values[0].Parameter.ID != 2000 {
		t.Fatalf("front param was not polled first: %+v", device.Values)
	}
	if device.Values[1].Parameter.ID != 1000 {
		t.Fatalf("background param missing from combined batch: %+v", device.Values)
	}
	if len(device.BackgroundValues) != 2 {
		t.Fatalf("background bookkeeping missing or wrong: %+v", device.BackgroundValues)
	}
}

func TestRouterPollSchedulerFallsBackHighPriorityToVXWhenRingDisabled(t *testing.T) {
	rt, requests := testRouterRuntimeForAddress(t, 0x50)
	go func() {
		req := <-requests
		got := string(req.frame)
		if strings.Contains(got, "?RS") {
			req.result <- response{err: fmt.Errorf("unexpected ring read frame %q", got)}
			return
		}
		if !strings.Contains(got, "?VX01") {
			req.result <- response{err: fmt.Errorf("unexpected frame %q", got)}
			return
		}
		req.result <- response{frame: testResponseFrame(0x50, 1, "+3FA00000")}
	}()
	sched := NewRouterPollScheduler(RouterPollSchedulerConfig{
		Router: rt,
		Devices: []RouterPollDeviceConfig{
			{
				Address: 0x50,
				Readout: mecom.ReadoutConfig{
					Parameters: []mecom.ReadoutParameter{
						{
							Parameter:    mecom.Parameter{ID: 1000, Instance: 1, Type: mecom.DataTypeFloat32},
							Sensor:       "priority",
							HighPriority: true,
						},
					},
					BulkChunk: 1,
				},
			},
		},
	})

	batch := sched.PollOnce(context.Background())[0x50]
	if len(batch.Errors) != 0 {
		t.Fatalf("PollOnce errors = %#v", batch.Errors)
	}
	if len(batch.RingValues) != 0 {
		t.Fatalf("ring values should be disabled by default: %#v", batch.RingValues)
	}
	if got, want := len(batch.BackgroundValues), 1; got != want {
		t.Fatalf("background values = %d, want %d", got, want)
	}
	if batch.BackgroundValues[0].Parameter.ID != 1000 {
		t.Fatalf("wrong fallback value: %#v", batch.BackgroundValues)
	}
}

func TestRouterPollSchedulerSubscriptionReceivesBatch(t *testing.T) {
	rt, requests := testRouterRuntimeForAddress(t, 0x50)
	go func() {
		req := <-requests
		req.result <- response{frame: testResponseFrame(0x50, 1, "+3FA0000040200000")}
	}()
	sched := NewRouterPollScheduler(RouterPollSchedulerConfig{
		Router: rt,
		Devices: []RouterPollDeviceConfig{
			{
				Address:          0x50,
				SupportsRingRead: boolPtr(false),
				Readout: mecom.ReadoutConfig{
					Parameters: []mecom.ReadoutParameter{
						{Parameter: mecom.Parameter{ID: 1000, Instance: 1, Type: mecom.DataTypeFloat32}, Sensor: "bg"},
					},
					BulkChunk: 1,
				},
			},
		},
	})
	ch, cancel := sched.Subscribe(0x50, 1)
	defer cancel()

	if _, err := sched.PollOnce(context.Background())[0x50], error(nil); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	select {
	case batch := <-ch:
		if len(batch.Values) == 0 {
			t.Fatal("subscription delivered empty batch")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not receive a batch")
	}
}

func TestRuntimeReadClientReturnsRingBuildErrorsBeforeRouting(t *testing.T) {
	rt, requests := testRouterRuntimeForAddress(t, 0x50)
	client := rt.NewReadClient(RouterDeviceClientConfig{
		Address:             0x50,
		SupportsRingReadout: true,
	})
	params := make([]mecom.RingCaptureParameter, mecom.RingCaptureLimit+1)

	err := client.ConfigureRingCapture(context.Background(), 1, params)
	if err == nil {
		t.Fatal("ConfigureRingCapture returned nil error for oversized capture config")
	}
	if !strings.Contains(err.Error(), "at most") {
		t.Fatalf("ConfigureRingCapture error = %v", err)
	}
	select {
	case req := <-requests:
		t.Fatalf("invalid ring config should not route frame: %q", string(req.frame))
	default:
	}
}

func TestRouterPollSchedulerPerDeviceErrorsAreIsolated(t *testing.T) {
	sched := NewRouterPollScheduler(RouterPollSchedulerConfig{
		Router: &RouterRuntime{
			routes: preparedRoutes{},
			clientCfg: routedClientConfig{
				RequestTimeout: 50 * time.Millisecond,
			},
		},
		Devices: []RouterPollDeviceConfig{
			{
				Address:          0x50,
				SupportsRingRead: boolPtr(false),
				Readout: mecom.ReadoutConfig{
					Parameters: []mecom.ReadoutParameter{
						{Parameter: mecom.Parameter{ID: 1000, Instance: 1, Type: mecom.DataTypeFloat32}},
					},
					BulkChunk: 1,
				},
			},
		},
	})
	sched.EnqueueFront(0x51, mecom.ReadoutParameter{
		Parameter: mecom.Parameter{ID: 2000, Instance: 1, Type: mecom.DataTypeFloat32},
	})

	batches := sched.PollOnce(context.Background())
	if _, ok := batches[0x50]; !ok {
		t.Fatal("configured device missing from PollOnce output")
	}
	errBatch, ok := batches[0x51]
	if !ok {
		t.Fatal("front-only device missing from PollOnce output")
	}
	if len(errBatch.Errors) == 0 {
		t.Fatal("expected per-device error for unroutable device")
	}
	if got := strings.Join(errorStrings(errBatch.Errors), " "); !strings.Contains(got, "no downstream route") {
		t.Fatalf("unexpected error batch: %s", got)
	}
}

func testRouterRuntimeForAddress(t *testing.T, addr byte) (*RouterRuntime, chan request) {
	t.Helper()
	requests := make(chan request, 1)
	return &RouterRuntime{
		routes: preparedRoutes{
			addr: []*routeBroker{{
				address:  addr,
				target:   "test",
				priority: 0,
				requests: requests,
			}},
		},
		clientCfg: routedClientConfig{
			RequestTimeout: 50 * time.Millisecond,
		},
	}, requests
}

func errorStrings(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, err := range errs {
		out = append(out, err.Error())
	}
	return out
}

func boolPtr(v bool) *bool { return &v }

func testResponseFrame(addr byte, seq uint16, payload string) []byte {
	frame := fmt.Sprintf("!%02X%04X%s", addr, seq, payload)
	return []byte(fmt.Sprintf("%s%04X\r", frame, mecom.CRC16([]byte(frame))))
}
