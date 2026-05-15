// Package writelease implements a small advisory write-authority registry.
//
// Multiple concurrent clients can read a Meerstetter device without any
// authorization. Writes (setpoint changes, output enable, save-to-flash) are
// dangerous when interleaved; this package provides a token-based serialization
// primitive so a frontend can require explicit ownership before issuing a
// write command.
//
// The lease is advisory inside the library — Commander.Authorizer consults
// it, the HTTP gateway enforces it on the wire. Default Commander behaviour
// is unchanged unless an Authorizer is wired in.
package writelease

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Errors that consumers can match with errors.Is.
var (
	ErrLeaseHeld    = errors.New("writelease: another holder has the lease")
	ErrInvalidToken = errors.New("writelease: token does not match active lease")
	ErrExpired      = errors.New("writelease: lease has expired")
	ErrUnknownDevice = errors.New("writelease: device has no active lease")
)

// Lease describes one active write authorization. The Token is opaque; only
// equality matters. Holder is informational (user name, session ID, etc.).
type Lease struct {
	DeviceID  string    `json:"device_id"`
	Holder    string    `json:"holder"`
	Token     string    `json:"token"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Active reports whether the lease is currently valid against the provided
// "now" timestamp. Use it in pure-tests; production code should let the
// registry sweep handle expiry.
func (l Lease) Active(now time.Time) bool {
	return !l.ExpiresAt.IsZero() && now.Before(l.ExpiresAt)
}

// Registry tracks one lease per device. Methods are safe for concurrent use.
// Expired leases are swept lazily on every public call.
type Registry struct {
	mu     sync.Mutex
	leases map[string]Lease
	now    func() time.Time
}

// NewRegistry returns an empty Registry that uses time.Now.
func NewRegistry() *Registry {
	return &Registry{
		leases: map[string]Lease{},
		now:    time.Now,
	}
}

// WithClock replaces the time source. Useful for deterministic tests.
func (r *Registry) WithClock(now func() time.Time) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = now
	return r
}

// Acquire reserves the write authority for deviceID under holder for ttl.
// Fails with ErrLeaseHeld if another non-expired lease exists. If holder
// already holds an active lease on this device, Acquire renews it (extends
// ExpiresAt) and returns the same Token.
func (r *Registry) Acquire(deviceID, holder string, ttl time.Duration) (Lease, error) {
	if deviceID == "" {
		return Lease{}, fmt.Errorf("writelease: device_id required")
	}
	if holder == "" {
		return Lease{}, fmt.Errorf("writelease: holder required")
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()
	now := r.now()
	if existing, ok := r.leases[deviceID]; ok && existing.Active(now) {
		if existing.Holder == holder {
			existing.ExpiresAt = now.Add(ttl)
			r.leases[deviceID] = existing
			return existing, nil
		}
		return Lease{}, fmt.Errorf("%w: %s holds %s until %s", ErrLeaseHeld, existing.Holder, deviceID, existing.ExpiresAt.Format(time.RFC3339))
	}
	token, err := newToken()
	if err != nil {
		return Lease{}, err
	}
	lease := Lease{
		DeviceID:   deviceID,
		Holder:     holder,
		Token:      token,
		AcquiredAt: now,
		ExpiresAt:  now.Add(ttl),
	}
	r.leases[deviceID] = lease
	return lease, nil
}

// Release drops the lease that matches token. Returns ErrInvalidToken when
// the token does not match the active lease (or no lease exists).
func (r *Registry) Release(token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()
	for id, lease := range r.leases {
		if lease.Token == token {
			delete(r.leases, id)
			return nil
		}
	}
	return ErrInvalidToken
}

// Validate confirms token is the active lease holder for deviceID. Used by
// Commander.Authorizer and gateway write endpoints.
func (r *Registry) Validate(deviceID, token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()
	lease, ok := r.leases[deviceID]
	if !ok {
		return ErrUnknownDevice
	}
	if !lease.Active(r.now()) {
		delete(r.leases, deviceID)
		return ErrExpired
	}
	if lease.Token != token {
		return ErrInvalidToken
	}
	return nil
}

// List returns a snapshot of currently active leases, sorted by DeviceID.
func (r *Registry) List() []Lease {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()
	out := make([]Lease, 0, len(r.leases))
	for _, lease := range r.leases {
		out = append(out, lease)
	}
	// Stable order: by DeviceID lexicographically.
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j-1].DeviceID > out[j].DeviceID {
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}
	return out
}

func (r *Registry) sweepLocked() {
	now := r.now()
	for id, lease := range r.leases {
		if !lease.Active(now) {
			delete(r.leases, id)
		}
	}
}

func newToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("writelease: token gen: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
