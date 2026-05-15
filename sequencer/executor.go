package sequencer

import (
	"context"
	"fmt"
	"time"

	"github.com/egidinas/meerstetter-go/tmtc"
)

// Run executes script against commander, walking each step in order.
// Steps of kind wait_stable sleep for their Duration as a conservative
// approximation (no stability oracle is available at this layer).
// Steps of kind assert and log are no-ops that always succeed.
// The returned Result reports per-step outcomes; the error is non-nil only for
// unexpected executor failures (not individual step errors).
func Run(ctx context.Context, script Script, commander tmtc.Commander) (Result, error) {
	runID := fmt.Sprintf("%s-run-%d", script.ID, time.Now().UnixNano())
	res := Result{
		ScriptID: script.ID,
		RunID:    runID,
		OK:       true,
	}

	scriptCtx := ctx
	if script.Timeout > 0 {
		var cancel context.CancelFunc
		scriptCtx, cancel = context.WithTimeout(ctx, script.Timeout)
		defer cancel()
	}

	for _, step := range script.Steps {
		if err := scriptCtx.Err(); err != nil {
			res.Steps = append(res.Steps, StepResult{StepID: step.ID, OK: false, Error: err.Error()})
			res.OK = false
			break
		}
		sr := runStep(scriptCtx, step, commander)
		res.Steps = append(res.Steps, sr)
		if !sr.OK {
			res.OK = false
			break
		}
	}
	return res, nil
}

func runStep(ctx context.Context, step Step, commander tmtc.Commander) StepResult {
	switch step.Kind {
	case StepSendCommand:
		return runSendCommand(ctx, step, commander)
	case StepWait, StepWaitStable:
		return runWait(ctx, step)
	case StepAssert, StepLog:
		return StepResult{StepID: step.ID, OK: true}
	default:
		return StepResult{StepID: step.ID, OK: false, Error: fmt.Sprintf("unknown step kind %q", step.Kind)}
	}
}

func runSendCommand(ctx context.Context, step Step, commander tmtc.Commander) StepResult {
	if step.CommandName == "" {
		return StepResult{StepID: step.ID, OK: false, Error: "send_command step has no command_name"}
	}
	tc := tmtc.Telecommand{
		TargetID:       step.TargetID,
		Name:           step.CommandName,
		Arguments:      step.Arguments,
		RequiresAck:    step.AwaitAck,
		IdempotencyKey: step.IdempotencyKey,
		Time:           time.Now(),
	}
	tc.EnsureIdempotencyKey()

	done := make(chan struct {
		ev  tmtc.CommandEvent
		err error
	}, 1)
	go func() {
		ev, err := commander.Send(tc)
		done <- struct {
			ev  tmtc.CommandEvent
			err error
		}{ev, err}
	}()
	select {
	case <-ctx.Done():
		return StepResult{StepID: step.ID, OK: false, Error: ctx.Err().Error()}
	case r := <-done:
		if r.err != nil {
			return StepResult{StepID: step.ID, OK: false, Error: r.err.Error()}
		}
		if r.ev.Status == tmtc.CommandRejected || r.ev.Status == tmtc.CommandFailed {
			msg := r.ev.Error
			if msg == "" {
				msg = string(r.ev.Status)
			}
			return StepResult{StepID: step.ID, OK: false, Error: msg}
		}
		return StepResult{StepID: step.ID, OK: true}
	}
}

func runWait(ctx context.Context, step Step) StepResult {
	if step.Duration <= 0 {
		return StepResult{StepID: step.ID, OK: true}
	}
	select {
	case <-ctx.Done():
		return StepResult{StepID: step.ID, OK: false, Error: ctx.Err().Error()}
	case <-time.After(step.Duration):
		return StepResult{StepID: step.ID, OK: true}
	}
}
