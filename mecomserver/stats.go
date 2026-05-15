package mecomserver

import (
	"sync"
	"time"
)

// BrokerStats describes the live state of one downstream-owning broker. The
// snapshot is suitable for status pills and health endpoints; counters are
// monotonic since the broker started.
type BrokerStats struct {
	Address       byte      `json:"address"`
	Target        string    `json:"target"`
	Connected     bool      `json:"connected"`
	LastConnectAt time.Time `json:"last_connect_at,omitempty"`
	LastErrorAt   time.Time `json:"last_error_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	FramesIn      uint64    `json:"frames_in"`
	FramesOut     uint64    `json:"frames_out"`
	ErrorCount    uint64    `json:"error_count"`
}

// brokerStatsRecorder is the goroutine-safe writer side of BrokerStats. One
// instance per broker. The snapshot returned by Snapshot is a value copy and
// safe to read after release.
type brokerStatsRecorder struct {
	mu    sync.RWMutex
	stats BrokerStats
}

func newBrokerStatsRecorder(addr byte, target string) *brokerStatsRecorder {
	return &brokerStatsRecorder{stats: BrokerStats{Address: addr, Target: target}}
}

func (r *brokerStatsRecorder) markConnected(target string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats.Connected = true
	r.stats.LastConnectAt = time.Now()
	if target != "" {
		r.stats.Target = target
	}
}

func (r *brokerStatsRecorder) markDisconnected(err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats.Connected = false
	if err != nil {
		r.stats.LastErrorAt = time.Now()
		r.stats.LastError = err.Error()
		r.stats.ErrorCount++
	}
}

func (r *brokerStatsRecorder) markFrameIn() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.stats.FramesIn++
	r.mu.Unlock()
}

func (r *brokerStatsRecorder) markFrameOut() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.stats.FramesOut++
	r.mu.Unlock()
}

func (r *brokerStatsRecorder) markError(err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats.LastErrorAt = time.Now()
	r.stats.LastError = err.Error()
	r.stats.ErrorCount++
}

// Snapshot returns a value copy of the current stats.
func (r *brokerStatsRecorder) Snapshot() BrokerStats {
	if r == nil {
		return BrokerStats{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stats
}
