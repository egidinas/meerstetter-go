package tmtclog

import (
	"sync"
	"time"

	"github.com/egidinas/meerstetter-go/tmtc"
)

type Kind string

const (
	KindTelemetry    Kind = "telemetry"
	KindTelecommand  Kind = "telecommand"
	KindCommandEvent Kind = "command_event"
)

type Entry struct {
	Seq   uint64             `json:"seq"`
	Time  time.Time          `json:"time"`
	Kind  Kind               `json:"kind"`
	TM    *tmtc.Telemetry    `json:"tm,omitempty"`
	TC    *tmtc.Telecommand  `json:"tc,omitempty"`
	Event *tmtc.CommandEvent `json:"event,omitempty"`
}

// Ring is a bounded in-memory loop buffer for troubleshooting and later export.
type Ring struct {
	mu      sync.Mutex
	entries []Entry
	next    int
	filled  bool
	seq     uint64
}

func New(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{entries: make([]Entry, capacity)}
}

func (r *Ring) Append(entry Entry) Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	entry.Seq = r.seq
	if entry.Time.IsZero() {
		entry.Time = time.Now().UTC()
	}
	r.entries[r.next] = entry
	r.next = (r.next + 1) % len(r.entries)
	if r.next == 0 {
		r.filled = true
	}
	return entry
}

func (r *Ring) Snapshot() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.filled {
		return append([]Entry(nil), r.entries[:r.next]...)
	}
	out := make([]Entry, 0, len(r.entries))
	out = append(out, r.entries[r.next:]...)
	out = append(out, r.entries[:r.next]...)
	return out
}
