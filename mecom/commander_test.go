package mecom

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	tmtc "github.com/egidinas/signalforge/contracts"
)

type fakeWriteClient struct {
	float32Calls []writeFloat32Call
	int32Calls   []writeInt32Call
	stringCalls  []writeStringCall
	bigDataCalls []writeStringCall
	resetCalls   int
	saveCalls    int
	failNext     bool
}

type writeFloat32Call struct {
	paramID, instance int
	value             float32
}
type writeInt32Call struct {
	paramID, instance int
	value             int32
}
type writeStringCall struct {
	paramID, instance int
	value             string
}

func (f *fakeWriteClient) WriteFloat32(_ context.Context, paramID, instance int, value float32) error {
	f.float32Calls = append(f.float32Calls, writeFloat32Call{paramID, instance, value})
	if f.failNext {
		f.failNext = false
		return fakeErr("forced write failure")
	}
	return nil
}

func (f *fakeWriteClient) WriteInt32(_ context.Context, paramID, instance int, value int32) error {
	f.int32Calls = append(f.int32Calls, writeInt32Call{paramID, instance, value})
	return nil
}

func (f *fakeWriteClient) WriteString(_ context.Context, paramID, instance int, value string) error {
	f.stringCalls = append(f.stringCalls, writeStringCall{paramID, instance, value})
	return nil
}

func (f *fakeWriteClient) WriteBigDataString(_ context.Context, paramID, instance int, value string) error {
	f.bigDataCalls = append(f.bigDataCalls, writeStringCall{paramID, instance, value})
	return nil
}

func (f *fakeWriteClient) Reset(_ context.Context) error       { f.resetCalls++; return nil }
func (f *fakeWriteClient) SaveToFlash(_ context.Context) error { f.saveCalls++; return nil }

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

func TestCommanderRoutesWriteFloat32(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	ev, err := cmdr.Send(tmtc.Telecommand{
		Name:      "set_float32",
		Arguments: map[string]any{"param": 3000, "instance": 1, "value": 25.0},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if ev.Status != tmtc.CommandCompleted {
		t.Fatalf("status=%v, want completed", ev.Status)
	}
	if got := len(fw.float32Calls); got != 1 {
		t.Fatalf("float32 calls=%d, want 1", got)
	}
	call := fw.float32Calls[0]
	if call.paramID != 3000 || call.instance != 1 || call.value != 25.0 {
		t.Fatalf("got %+v, want {3000,1,25.0}", call)
	}
}

func TestCommanderRoutesWriteInt32WithStringArgs(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	ev, err := cmdr.Send(tmtc.Telecommand{
		Name:      "set_int32",
		Arguments: map[string]any{"param": "2010", "instance": "2", "value": "1"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if ev.Status != tmtc.CommandCompleted {
		t.Fatalf("status=%v, want completed", ev.Status)
	}
	if got := fw.int32Calls; len(got) != 1 || got[0].paramID != 2010 || got[0].instance != 2 || got[0].value != 1 {
		t.Fatalf("int32 calls=%+v, want one {2010,2,1}", got)
	}
}

func TestCommanderRoutesWriteInt32WithJSONNumberArgs(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	_, err := cmdr.Send(tmtc.Telecommand{
		Name:      "set_int32",
		Arguments: map[string]any{"param": json.Number("2010"), "instance": json.Number("1"), "value": json.Number("5")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := fw.int32Calls[0]; got != (writeInt32Call{paramID: 2010, instance: 1, value: 5}) {
		t.Fatalf("int32 call = %+v", got)
	}
}

func TestCommanderRoutesWriteString(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	ev, err := cmdr.Send(tmtc.Telecommand{
		Name:      "write_string",
		Arguments: map[string]any{"param": 6024, "instance": 1, "value": "Line 1"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if ev.Status != tmtc.CommandCompleted {
		t.Fatalf("status=%v, want completed", ev.Status)
	}
	if got := fw.stringCalls; len(got) != 1 || got[0] != (writeStringCall{paramID: 6024, instance: 1, value: "Line 1"}) {
		t.Fatalf("string calls=%+v", got)
	}
}

func TestCommanderRoutesWriteBigDataString(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	_, err := cmdr.Send(tmtc.Telecommand{
		Name:      "write_big_data_string",
		Arguments: map[string]any{"param": 120, "instance": 1, "value": "SN76 note"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := fw.bigDataCalls; len(got) != 1 || got[0] != (writeStringCall{paramID: 120, instance: 1, value: "SN76 note"}) {
		t.Fatalf("big-data calls=%+v", got)
	}
}

func TestCommanderRoutesWriteUserNoteToParam120(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	_, err := cmdr.Send(tmtc.Telecommand{
		Name:      "write_user_note",
		Arguments: map[string]any{"instance": 3, "value": "Back zone"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := fw.bigDataCalls; len(got) != 1 || got[0] != (writeStringCall{paramID: 120, instance: 3, value: "Back zone"}) {
		t.Fatalf("user-note calls=%+v", got)
	}
}

func TestCommanderRejectsFractionalIntArg(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	ev, err := cmdr.Send(tmtc.Telecommand{
		Name:      "set_int32",
		Arguments: map[string]any{"param": 2010.5, "instance": 1, "value": 5},
	})
	if err == nil {
		t.Fatal("Send accepted fractional integer argument")
	}
	if ev.Status != tmtc.CommandFailed {
		t.Fatalf("status=%v, want failed", ev.Status)
	}
	if got := len(fw.int32Calls); got != 0 {
		t.Fatalf("int32 calls=%d, want 0", got)
	}
}

func TestCommanderControlActionsRoute(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	if _, err := cmdr.Send(tmtc.Telecommand{Name: "reset"}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := cmdr.Send(tmtc.Telecommand{Name: "save_to_flash"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if fw.resetCalls != 1 {
		t.Fatalf("resetCalls=%d, want 1", fw.resetCalls)
	}
	if fw.saveCalls != 1 {
		t.Fatalf("saveCalls=%d, want 1", fw.saveCalls)
	}
}

func TestCommanderUnknownCommandReturnsFailedEvent(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	ev, err := cmdr.Send(tmtc.Telecommand{Name: "wave_hands"})
	if err == nil {
		t.Fatalf("expected error for unknown command")
	}
	if ev.Status != tmtc.CommandFailed {
		t.Fatalf("status=%v, want failed", ev.Status)
	}
}

func TestCommanderWriteFailurePropagates(t *testing.T) {
	fw := &fakeWriteClient{failNext: true}
	cmdr := NewCommander(fw, time.Second)
	ev, err := cmdr.Send(tmtc.Telecommand{
		Name:      "set_float32",
		Arguments: map[string]any{"param": 3000, "instance": 1, "value": 25.0},
	})
	if err == nil {
		t.Fatalf("expected propagated error")
	}
	if ev.Status != tmtc.CommandFailed || ev.Error == "" {
		t.Fatalf("event=%+v, want Failed+Error", ev)
	}
}

func TestCommanderAuthorizerBlocksWriteBeforeClientCall(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	cmdr.TargetID = "tec-75"
	deny := errors.New("lease required")
	var gotTarget string
	cmdr.Authorizer = AuthorizerFunc(func(targetID string, tc tmtc.Telecommand) error {
		gotTarget = targetID
		if tc.Name != "set_float32" {
			t.Fatalf("authorized command name=%q, want set_float32", tc.Name)
		}
		return deny
	})

	ev, err := cmdr.Send(tmtc.Telecommand{
		Name:      "set_float32",
		Arguments: map[string]any{"param": 3000, "instance": 1, "value": 25.0},
	})
	if !errors.Is(err, deny) {
		t.Fatalf("err=%v, want %v", err, deny)
	}
	if ev.Status != tmtc.CommandRejected || ev.Error != deny.Error() {
		t.Fatalf("event=%+v, want rejected with denial", ev)
	}
	if gotTarget != "tec-75" {
		t.Fatalf("target=%q, want commander target", gotTarget)
	}
	if got := len(fw.float32Calls); got != 0 {
		t.Fatalf("float32 calls=%d, want 0", got)
	}
}

func TestCommanderAuthorizerUsesTelecommandTarget(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	cmdr.TargetID = "tec-75"
	var gotTarget string
	cmdr.Authorizer = AuthorizerFunc(func(targetID string, _ tmtc.Telecommand) error {
		gotTarget = targetID
		return nil
	})

	_, err := cmdr.Send(tmtc.Telecommand{
		TargetID:  "tec-81",
		Name:      "set_int32",
		Arguments: map[string]any{"param": 2010, "instance": 1, "value": 1},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotTarget != "tec-81" {
		t.Fatalf("target=%q, want telecommand target", gotTarget)
	}
	if got := len(fw.int32Calls); got != 1 {
		t.Fatalf("int32 calls=%d, want 1", got)
	}
}

func TestCommanderAuthorizerNotCalledForUnknownCommand(t *testing.T) {
	fw := &fakeWriteClient{}
	cmdr := NewCommander(fw, time.Second)
	called := false
	cmdr.Authorizer = AuthorizerFunc(func(string, tmtc.Telecommand) error {
		called = true
		return nil
	})

	ev, err := cmdr.Send(tmtc.Telecommand{Name: "wave_hands"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	if ev.Status != tmtc.CommandFailed {
		t.Fatalf("status=%v, want failed", ev.Status)
	}
	if called {
		t.Fatalf("authorizer called for unknown command")
	}
}
