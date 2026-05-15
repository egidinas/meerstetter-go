package sequencer

import (
	"encoding/json"
	"fmt"
	"time"
)

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

// UnmarshalJSON lets Script.timeout accept either a Go duration string
// ("10s", "1m30s") or a numeric nanosecond value.
func (s *Script) UnmarshalJSON(data []byte) error {
	type alias Script
	aux := &struct {
		Timeout any `json:"timeout,omitempty"`
		*alias
	}{alias: (*alias)(s)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	d, err := parseFlexibleDuration(aux.Timeout)
	if err != nil {
		return fmt.Errorf("script.timeout: %w", err)
	}
	s.Timeout = d
	return nil
}

// UnmarshalJSON lets Step.duration accept either a Go duration string or a
// numeric nanosecond value.
func (s *Step) UnmarshalJSON(data []byte) error {
	type alias Step
	aux := &struct {
		Duration any `json:"duration,omitempty"`
		*alias
	}{alias: (*alias)(s)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	d, err := parseFlexibleDuration(aux.Duration)
	if err != nil {
		return fmt.Errorf("step.duration: %w", err)
	}
	s.Duration = d
	return nil
}

func parseFlexibleDuration(v any) (time.Duration, error) {
	switch x := v.(type) {
	case nil:
		return 0, nil
	case string:
		if x == "" {
			return 0, nil
		}
		return time.ParseDuration(x)
	case float64:
		return time.Duration(x), nil
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return 0, err
		}
		return time.Duration(n), nil
	default:
		return 0, fmt.Errorf("unsupported duration %T", v)
	}
}
