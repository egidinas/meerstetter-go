package tmtc

import "testing"

func TestEnsureIdempotencyKeyIncludesArguments(t *testing.T) {
	base := Telecommand{
		TargetID:  "tec-1",
		Name:      "set_temperature",
		Arguments: map[string]any{"setpoint": 21.5, "unit": "C"},
	}
	changed := Telecommand{
		TargetID:  "tec-1",
		Name:      "set_temperature",
		Arguments: map[string]any{"setpoint": 22.0, "unit": "C"},
	}

	base.EnsureIdempotencyKey()
	changed.EnsureIdempotencyKey()
	if base.IdempotencyKey == "" {
		t.Fatal("empty idempotency key")
	}
	if base.IdempotencyKey == changed.IdempotencyKey {
		t.Fatal("argument-only command changes reused the same idempotency key")
	}
}

func TestEnsureIdempotencyKeyArgumentsAreStable(t *testing.T) {
	a := Telecommand{
		TargetID:  "tec-1",
		Name:      "set_temperature",
		Arguments: map[string]any{"setpoint": 21.5, "unit": "C"},
	}
	b := Telecommand{
		TargetID:  "tec-1",
		Name:      "set_temperature",
		Arguments: map[string]any{"unit": "C", "setpoint": 21.5},
	}

	a.EnsureIdempotencyKey()
	b.EnsureIdempotencyKey()
	if a.IdempotencyKey != b.IdempotencyKey {
		t.Fatalf("keys differ for equivalent arguments: %q != %q", a.IdempotencyKey, b.IdempotencyKey)
	}
}

func TestEnsureIdempotencyKeyPreservesExplicitKey(t *testing.T) {
	tc := Telecommand{TargetID: "node-1", Name: "restart", IdempotencyKey: "operator-provided"}
	tc.EnsureIdempotencyKey()
	if tc.IdempotencyKey != "operator-provided" {
		t.Fatalf("idempotency key = %q", tc.IdempotencyKey)
	}
}
