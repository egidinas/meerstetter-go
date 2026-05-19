package mecomautomation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/egidinas/signalforge/sequencer"
	tmtc "github.com/egidinas/signalforge/contracts"
	"github.com/egidinas/signalforge/controlprogram"
)

const (
	CommandLUTPreload  = "mecom.lut.preload"
	AutomationMeComLUT = "mecom_lut"
)

func DefaultSampleTargetIDs() []string {
	return []string{"tec-31", "tec-32", "tec-33", "tec-34"}
}

func FourCycleSampleProgram(targetIDs ...string) controlprogram.Program {
	if len(targetIDs) == 0 {
		targetIDs = DefaultSampleTargetIDs()
	}
	targetIDs = append([]string(nil), targetIDs...)

	return controlprogram.Program{
		ID:         "mecom-four-cycle-sample",
		Name:       "MeCom LUT four-cycle sample",
		TargetIDs:  targetIDs,
		CycleCount: 4,
		Metadata: map[string]string{
			"automation": AutomationMeComLUT,
			"safe_start": "preload_only_no_output_enable",
		},
		Steps: []controlprogram.Step{
			{
				ID:   "low-temperature-hold",
				Hold: 30 * time.Second,
				Setpoints: []controlprogram.Setpoint{{
					Channel: "temperature.object",
					Value:   20,
					Unit:    "degC",
				}},
			},
			{
				ID:   "high-temperature-hold",
				Hold: 30 * time.Second,
				Setpoints: []controlprogram.Setpoint{{
					Channel: "temperature.object",
					Value:   25,
					Unit:    "degC",
				}},
			},
		},
	}
}

func PreloadScript(program controlprogram.Program, targetID string) (sequencer.Script, error) {
	if targetID == "" {
		return sequencer.Script{}, fmt.Errorf("target id is required")
	}
	if err := program.Validate(); err != nil {
		return sequencer.Script{}, err
	}
	if !programTargets(program, targetID) {
		return sequencer.Script{}, fmt.Errorf("program %q does not include target %q", program.ID, targetID)
	}

	args := programArguments(program)
	key := preloadIdempotencyKey(program.ID, targetID, args)
	return sequencer.Script{
		ID:      key,
		Name:    fmt.Sprintf("preload %s on %s", program.ID, targetID),
		Timeout: 15 * time.Second,
		Steps: []sequencer.Step{{
			ID:             "preload-lut",
			Kind:           sequencer.StepSendCommand,
			TargetID:       targetID,
			CommandName:    CommandLUTPreload,
			Arguments:      args,
			AwaitAck:       true,
			IdempotencyKey: key,
		}},
	}, nil
}

func PreloadTelecommands(program controlprogram.Program, at time.Time) ([]tmtc.Telecommand, error) {
	if err := program.Validate(); err != nil {
		return nil, err
	}

	commands := make([]tmtc.Telecommand, 0, len(program.TargetIDs))
	for _, targetID := range program.TargetIDs {
		args := programArguments(program)
		key := preloadIdempotencyKey(program.ID, targetID, args)
		commands = append(commands, tmtc.Telecommand{
			ID:             key,
			TargetID:       targetID,
			Time:           at,
			Name:           CommandLUTPreload,
			Arguments:      args,
			IdempotencyKey: key,
			RequiresAck:    true,
			Metadata: map[string]string{
				"automation": AutomationMeComLUT,
				"program_id": program.ID,
				"safe_start": "preload_only_no_output_enable",
			},
		})
	}
	return commands, nil
}

func programTargets(program controlprogram.Program, targetID string) bool {
	for _, candidate := range program.TargetIDs {
		if candidate == targetID {
			return true
		}
	}
	return false
}

func preloadIdempotencyKey(programID, targetID string, args map[string]any) string {
	payload, err := json.Marshal(args)
	if err != nil {
		payload = []byte(programID)
	}
	sum := sha256.Sum256(payload)
	return programID + ":" + targetID + ":lut-preload:" + hex.EncodeToString(sum[:8])
}

func programArguments(program controlprogram.Program) map[string]any {
	steps := make([]map[string]any, 0, len(program.Steps))
	for _, step := range program.Steps {
		setpoints := make([]map[string]any, 0, len(step.Setpoints))
		for _, setpoint := range step.Setpoints {
			setpoints = append(setpoints, map[string]any{
				"channel": setpoint.Channel,
				"value":   setpoint.Value,
				"unit":    setpoint.Unit,
			})
		}
		steps = append(steps, map[string]any{
			"id":        step.ID,
			"hold_ms":   int64(step.Hold / time.Millisecond),
			"setpoints": setpoints,
		})
	}

	return map[string]any{
		"program_id":        program.ID,
		"cycles":            program.CycleCount,
		"target_count":      len(program.TargetIDs),
		"steps":             steps,
		"total_duration_ms": int64(program.TotalDuration() / time.Millisecond),
	}
}
