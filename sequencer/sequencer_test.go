package sequencer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/tmtc"
)

type recordingCommander struct {
	sent  []tmtc.Telecommand
	event tmtc.CommandEvent
	err   error
}

func (c *recordingCommander) Send(tc tmtc.Telecommand) (tmtc.CommandEvent, error) {
	c.sent = append(c.sent, tc)
	if c.err != nil {
		return tmtc.CommandEvent{}, c.err
	}
	if c.event.Status != "" {
		return c.event, nil
	}
	return tmtc.CommandEvent{Status: tmtc.CommandCompleted}, nil
}

type blockingContextCommander struct {
	entered  chan struct{}
	canceled chan error
}

func (c *blockingContextCommander) Send(tmtc.Telecommand) (tmtc.CommandEvent, error) {
	return tmtc.CommandEvent{Status: tmtc.CommandCompleted}, nil
}

func (c *blockingContextCommander) SendContext(ctx context.Context, _ tmtc.Telecommand) (tmtc.CommandEvent, error) {
	close(c.entered)
	<-ctx.Done()
	err := ctx.Err()
	c.canceled <- err
	return tmtc.CommandEvent{Status: tmtc.CommandFailed, Error: err.Error()}, err
}

func TestScriptUnmarshalAcceptsDurationStrings(t *testing.T) {
	var script Script
	raw := []byte(`{
		"id":"cycle",
		"timeout":"5s",
		"steps":[{"id":"settle","kind":"wait","duration":"25ms"}]
	}`)
	if err := json.Unmarshal(raw, &script); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if script.Timeout != 5*time.Second {
		t.Fatalf("timeout = %s, want 5s", script.Timeout)
	}
	if got := script.Steps[0].Duration; got != 25*time.Millisecond {
		t.Fatalf("step duration = %s, want 25ms", got)
	}
}

func TestRunSendsCommandWithIdempotencyAndArguments(t *testing.T) {
	cmdr := &recordingCommander{}
	script := Script{
		ID: "cycle",
		Steps: []Step{
			{
				ID:          "set-target",
				Kind:        StepSendCommand,
				TargetID:    "tec-75",
				CommandName: "set_float32",
				Arguments:   map[string]any{"param": 3000, "instance": 1, "value": 25.0},
				AwaitAck:    true,
			},
			{ID: "log", Kind: StepLog},
		},
	}
	result, err := Run(context.Background(), script, cmdr)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.OK || len(result.Steps) != 2 {
		t.Fatalf("result = %+v, want OK with 2 steps", result)
	}
	if len(cmdr.sent) != 1 {
		t.Fatalf("sent commands = %d, want 1", len(cmdr.sent))
	}
	got := cmdr.sent[0]
	if got.TargetID != "tec-75" || got.Name != "set_float32" || !got.RequiresAck {
		t.Fatalf("telecommand = %+v", got)
	}
	if got.IdempotencyKey == "" {
		t.Fatal("idempotency key was not filled")
	}
	if got.Arguments["param"] != 3000 {
		t.Fatalf("arguments = %#v", got.Arguments)
	}
}

func TestRunStopsOnRejectedCommand(t *testing.T) {
	cmdr := &recordingCommander{event: tmtc.CommandEvent{Status: tmtc.CommandRejected, Error: "limit"}}
	result, err := Run(context.Background(), Script{
		ID: "cycle",
		Steps: []Step{
			{ID: "bad", Kind: StepSendCommand, CommandName: "set_float32"},
			{ID: "later", Kind: StepLog},
		},
	}, cmdr)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.OK || len(result.Steps) != 1 || result.Steps[0].Error != "limit" {
		t.Fatalf("result = %+v, want rejected first step only", result)
	}
}

func TestRunReportsCommanderError(t *testing.T) {
	cmdr := &recordingCommander{err: errors.New("offline")}
	result, err := Run(context.Background(), Script{
		ID:    "cycle",
		Steps: []Step{{ID: "send", Kind: StepSendCommand, CommandName: "reset"}},
	}, cmdr)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.OK || result.Steps[0].Error != "offline" {
		t.Fatalf("result = %+v, want commander error", result)
	}
}

func TestRunCancelsContextAwareCommanderOnTimeout(t *testing.T) {
	cmdr := &blockingContextCommander{
		entered:  make(chan struct{}),
		canceled: make(chan error, 1),
	}
	result, err := Run(context.Background(), Script{
		ID:      "cycle",
		Timeout: 10 * time.Millisecond,
		Steps:   []Step{{ID: "send", Kind: StepSendCommand, CommandName: "reset"}},
	}, cmdr)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.OK || len(result.Steps) != 1 || result.Steps[0].Error != context.DeadlineExceeded.Error() {
		t.Fatalf("result = %+v, want deadline failure", result)
	}
	select {
	case err := <-cmdr.canceled:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("canceled with %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("commander did not observe cancellation")
	}
}

func TestRunRejectsUnknownStepKind(t *testing.T) {
	result, err := Run(context.Background(), Script{
		ID:    "cycle",
		Steps: []Step{{ID: "unknown", Kind: StepKind("custom")}},
	}, &recordingCommander{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.OK || len(result.Steps) != 1 {
		t.Fatalf("result = %+v, want failed unknown step", result)
	}
}
