package tmtc

import "testing"

func TestEnsureIdempotencyKeyIsStable(t *testing.T) {
	a := Telecommand{TargetID: "tec-1", Name: "set_temperature", Payload: []byte{1, 2, 3}}
	b := Telecommand{TargetID: "tec-1", Name: "set_temperature", Payload: []byte{1, 2, 3}}
	a.EnsureIdempotencyKey()
	b.EnsureIdempotencyKey()
	if a.IdempotencyKey == "" {
		t.Fatal("empty idempotency key")
	}
	if a.IdempotencyKey != b.IdempotencyKey {
		t.Fatalf("keys differ: %q != %q", a.IdempotencyKey, b.IdempotencyKey)
	}
}
