package sequencer

import "time"

type StepKind string

const (
	StepSendCommand StepKind = "send_command"
	StepWait        StepKind = "wait"
	StepWaitStable  StepKind = "wait_stable"
	StepAssert      StepKind = "assert"
	StepLog         StepKind = "log"
)

type Script struct {
	ID      string        `json:"id"`
	Name    string        `json:"name,omitempty"`
	Steps   []Step        `json:"steps"`
	Timeout time.Duration `json:"timeout,omitempty"`
}

type Step struct {
	ID             string         `json:"id"`
	Kind           StepKind       `json:"kind"`
	TargetID       string         `json:"target_id,omitempty"`
	CommandName    string         `json:"command_name,omitempty"`
	Arguments      map[string]any `json:"arguments,omitempty"`
	AwaitAck       bool           `json:"await_ack,omitempty"`
	Duration       time.Duration  `json:"duration,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
}

type StepResult struct {
	StepID string `json:"step_id"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

type Result struct {
	ScriptID string       `json:"script_id"`
	RunID    string       `json:"run_id"`
	OK       bool         `json:"ok"`
	Steps    []StepResult `json:"steps"`
}
