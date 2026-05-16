package sequencer

import (
	"context"
	"fmt"
	"time"

	"github.com/egidinas/meerstetter-go/tmtc"
)

type contextCommander interface {
	SendContext(context.Context, tmtc.Telecommand) (tmtc.CommandEvent, error)
}

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

	var (
		ev  tmtc.CommandEvent
		err error
	)
	if cancellable, ok := commander.(contextCommander); ok {
		ev, err = cancellable.SendContext(ctx, tc)
	} else {
		if err := ctx.Err(); err != nil {
			return StepResult{StepID: step.ID, OK: false, Error: err.Error()}
		}
		ev, err = commander.Send(tc)
		if err == nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return StepResult{StepID: step.ID, OK: false, Error: ctxErr.Error()}
			}
		}
	}
	return commandStepResult(step.ID, ev, err)
}

func commandStepResult(stepID string, ev tmtc.CommandEvent, err error) StepResult {
	if err != nil {
		return StepResult{StepID: stepID, OK: false, Error: err.Error()}
	}
	if ev.Status == tmtc.CommandRejected || ev.Status == tmtc.CommandFailed {
		msg := ev.Error
		if msg == "" {
			msg = string(ev.Status)
		}
		return StepResult{StepID: stepID, OK: false, Error: msg}
	}
	return StepResult{StepID: stepID, OK: true}
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
