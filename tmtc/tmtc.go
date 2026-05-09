package tmtc

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type Direction string

const (
	DirectionTM Direction = "tm"
	DirectionTC Direction = "tc"
)

type CommandStatus string

const (
	CommandAccepted  CommandStatus = "accepted"
	CommandSent      CommandStatus = "sent"
	CommandAcked     CommandStatus = "acked"
	CommandCompleted CommandStatus = "completed"
	CommandRejected  CommandStatus = "rejected"
	CommandFailed    CommandStatus = "failed"
)

// Telemetry is the rich decoded default. Raw bytes are optional compatibility
// evidence, not the primary application contract.
type Telemetry struct {
	ID        string            `json:"id"`
	TargetID  string            `json:"target_id"`
	SessionID string            `json:"session_id,omitempty"`
	Time      time.Time         `json:"time"`
	Name      string            `json:"name"`
	Value     any               `json:"value"`
	Unit      string            `json:"unit,omitempty"`
	Quality   string            `json:"quality,omitempty"`
	Raw       []byte            `json:"raw,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Telecommand is idempotent by default through IdempotencyKey.
type Telecommand struct {
	ID             string            `json:"id"`
	TargetID       string            `json:"target_id"`
	SessionID      string            `json:"session_id,omitempty"`
	Time           time.Time         `json:"time"`
	Name           string            `json:"name"`
	Payload        []byte            `json:"payload,omitempty"`
	Arguments      map[string]any    `json:"arguments,omitempty"`
	IdempotencyKey string            `json:"idempotency_key"`
	RequiresAck    bool              `json:"requires_ack"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// EnsureIdempotencyKey fills a stable key when the caller did not provide one.
func (tc *Telecommand) EnsureIdempotencyKey() {
	if tc.IdempotencyKey != "" {
		return
	}
	h := sha256.New()
	h.Write([]byte(tc.TargetID))
	h.Write([]byte{0})
	h.Write([]byte(tc.Name))
	h.Write([]byte{0})
	h.Write(tc.Payload)
	tc.IdempotencyKey = hex.EncodeToString(h.Sum(nil))
}

type CommandEvent struct {
	ID             string            `json:"id"`
	CommandID      string            `json:"command_id"`
	SessionID      string            `json:"session_id,omitempty"`
	Time           time.Time         `json:"time"`
	Status         CommandStatus     `json:"status"`
	Transport      string            `json:"transport,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Result         any               `json:"result,omitempty"`
	Error          string            `json:"error,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// Publisher is intentionally transport-neutral; NATS, REST, HTTP, SSH, or direct
// in-process adapters can implement the same primitive.
type Publisher interface {
	PublishTelemetry(Telemetry) error
	PublishCommandEvent(CommandEvent) error
}

type Commander interface {
	Send(Telecommand) (CommandEvent, error)
}
