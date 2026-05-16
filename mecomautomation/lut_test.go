package mecomautomation

import (
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/sequencer"
)

func TestFourCycleSampleProgramDefaultsToFourControllers(t *testing.T) {
	program := FourCycleSampleProgram()

	if err := program.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got, want := program.CycleCount, 4; got != want {
		t.Fatalf("CycleCount = %d, want %d", got, want)
	}
	if got, want := program.TargetCount(), 4; got != want {
		t.Fatalf("TargetCount() = %d, want %d", got, want)
	}
	if got, want := program.TotalDuration(), 4*time.Minute; got != want {
		t.Fatalf("TotalDuration() = %s, want %s", got, want)
	}
	if got := program.Metadata["safe_start"]; got != "preload_only_no_output_enable" {
		t.Fatalf("safe_start metadata = %q", got)
	}
}

func TestFourCycleSampleProgramScalesToSixteenControllers(t *testing.T) {
	targets := make([]string, 16)
	for i := range targets {
		targets[i] = "tec-" + string(rune('A'+i))
	}

	program := FourCycleSampleProgram(targets...)

	if err := program.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := program.TargetCount(); got != 16 {
		t.Fatalf("TargetCount() = %d, want 16", got)
	}
}

func TestPreloadScriptContainsOnlyLUTPreloadCommand(t *testing.T) {
	program := FourCycleSampleProgram("tec-31")

	script, err := PreloadScript(program, "tec-31")
	if err != nil {
		t.Fatalf("PreloadScript() error = %v", err)
	}
	if len(script.Steps) != 1 {
		t.Fatalf("script has %d steps, want 1", len(script.Steps))
	}

	step := script.Steps[0]
	if step.Kind != sequencer.StepSendCommand {
		t.Fatalf("step kind = %s, want %s", step.Kind, sequencer.StepSendCommand)
	}
	if step.CommandName != CommandLUTPreload {
		t.Fatalf("command = %q, want %q", step.CommandName, CommandLUTPreload)
	}
	if !step.AwaitAck {
		t.Fatalf("preload step must await ack")
	}
	if step.TargetID != "tec-31" {
		t.Fatalf("target = %q", step.TargetID)
	}
	if got := step.Arguments["cycles"]; got != 4 {
		t.Fatalf("cycles arg = %#v, want 4", got)
	}
	if _, exists := step.Arguments["enable_output"]; exists {
		t.Fatalf("preload must not include enable_output")
	}
}

func TestPreloadTelecommandsFanOutPerTarget(t *testing.T) {
	program := FourCycleSampleProgram("tec-31", "tec-32", "tec-33", "tec-34")
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)

	commands, err := PreloadTelecommands(program, now)
	if err != nil {
		t.Fatalf("PreloadTelecommands() error = %v", err)
	}
	if len(commands) != 4 {
		t.Fatalf("got %d telecommands, want 4", len(commands))
	}

	seenKeys := map[string]bool{}
	for _, command := range commands {
		if command.Name != CommandLUTPreload {
			t.Fatalf("command name = %q", command.Name)
		}
		if !command.RequiresAck {
			t.Fatalf("command for %s must require ack", command.TargetID)
		}
		if command.Metadata["automation"] != AutomationMeComLUT {
			t.Fatalf("automation metadata = %q", command.Metadata["automation"])
		}
		if command.IdempotencyKey == "" {
			t.Fatalf("missing idempotency key for %s", command.TargetID)
		}
		if seenKeys[command.IdempotencyKey] {
			t.Fatalf("duplicate idempotency key %q", command.IdempotencyKey)
		}
		seenKeys[command.IdempotencyKey] = true
		if got := command.Arguments["program_id"]; got != program.ID {
			t.Fatalf("program_id arg = %#v, want %q", got, program.ID)
		}
	}
}

func TestPreloadIdempotencyKeyIncludesProgramContents(t *testing.T) {
	base := FourCycleSampleProgram("tec-31")
	changed := FourCycleSampleProgram("tec-31")
	changed.Steps[0].Setpoints[0].Value = 21

	baseCommands, err := PreloadTelecommands(base, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	changedCommands, err := PreloadTelecommands(changed, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if baseCommands[0].IdempotencyKey == changedCommands[0].IdempotencyKey {
		t.Fatalf("changed program contents reused idempotency key %q", baseCommands[0].IdempotencyKey)
	}

	baseScript, err := PreloadScript(base, "tec-31")
	if err != nil {
		t.Fatal(err)
	}
	changedScript, err := PreloadScript(changed, "tec-31")
	if err != nil {
		t.Fatal(err)
	}
	if baseScript.Steps[0].IdempotencyKey == changedScript.Steps[0].IdempotencyKey {
		t.Fatalf("changed script contents reused idempotency key %q", baseScript.Steps[0].IdempotencyKey)
	}
}
