package writelease

import (
	"errors"
	"testing"
	"time"
)

func TestAcquireAndValidate(t *testing.T) {
	r := NewRegistry()
	lease, err := r.Acquire("tec-75", "alice", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := r.Validate("tec-75", lease.Token); err != nil {
		t.Fatalf("Validate own token: %v", err)
	}
}

func TestAcquireDeniedWhenHeldByOther(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Acquire("tec-75", "alice", time.Minute); err != nil {
		t.Fatalf("Acquire alice: %v", err)
	}
	_, err := r.Acquire("tec-75", "bob", time.Minute)
	if !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("Acquire bob = %v, want ErrLeaseHeld", err)
	}
}

func TestAcquireRenewsSameHolder(t *testing.T) {
	r := NewRegistry()
	first, err := r.Acquire("tec-75", "alice", time.Minute)
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}
	second, err := r.Acquire("tec-75", "alice", 2*time.Minute)
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}
	if first.Token != second.Token {
		t.Fatalf("renewal changed token: %s vs %s", first.Token, second.Token)
	}
	if !second.ExpiresAt.After(first.ExpiresAt) {
		t.Fatalf("renewal did not extend ExpiresAt")
	}
}

func TestExpiryReleasesAutomatically(t *testing.T) {
	now := time.Unix(1000, 0)
	clock := &now
	r := NewRegistry().WithClock(func() time.Time { return *clock })

	if _, err := r.Acquire("tec-75", "alice", time.Second); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	*clock = now.Add(2 * time.Second)
	// Bob can grab it now that alice's lease expired
	if _, err := r.Acquire("tec-75", "bob", time.Second); err != nil {
		t.Fatalf("Acquire bob after expiry: %v", err)
	}
}

func TestValidateExpiredLeaseReturnsErrExpired(t *testing.T) {
	now := time.Unix(1000, 0)
	clock := &now
	r := NewRegistry().WithClock(func() time.Time { return *clock })

	lease, err := r.Acquire("tec-75", "alice", time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	*clock = now.Add(2 * time.Second)
	if err := r.Validate("tec-75", lease.Token); !errors.Is(err, ErrExpired) {
		t.Fatalf("Validate expired lease = %v, want ErrExpired", err)
	}
	if err := r.Validate("tec-75", lease.Token); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("Validate swept lease = %v, want ErrUnknownDevice", err)
	}
}

func TestValidateRejectsWrongToken(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Acquire("tec-75", "alice", time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := r.Validate("tec-75", "deadbeef"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Validate wrong token = %v, want ErrInvalidToken", err)
	}
}

func TestValidateUnknownDevice(t *testing.T) {
	r := NewRegistry()
	if err := r.Validate("tec-99", "any"); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("Validate unknown device = %v, want ErrUnknownDevice", err)
	}
}

func TestRelease(t *testing.T) {
	r := NewRegistry()
	lease, _ := r.Acquire("tec-75", "alice", time.Minute)
	if err := r.Release(lease.Token); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := r.Validate("tec-75", lease.Token); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("Validate after release = %v, want ErrUnknownDevice", err)
	}
}

func TestListSortedByDeviceID(t *testing.T) {
	r := NewRegistry()
	r.Acquire("tec-84", "a", time.Minute)
	r.Acquire("tec-75", "b", time.Minute)
	r.Acquire("tec-81", "c", time.Minute)
	got := r.List()
	if len(got) != 3 || got[0].DeviceID != "tec-75" || got[1].DeviceID != "tec-81" || got[2].DeviceID != "tec-84" {
		t.Fatalf("List=%v, want sorted by DeviceID", got)
	}
}
