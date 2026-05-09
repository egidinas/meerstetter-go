package tmtclog

import "github.com/egidinas/meerstetter-go/tmtc"

// Recorder is the default TM/TC readout primitive: every telemetry sample and
// command event is committed to the local ring before it is forwarded to the
// live transport. Reconnecting consumers should resume with ReplaySince.
type Recorder struct {
	ring       *Ring
	downstream tmtc.Publisher
}

func NewRecorder(ring *Ring, downstream tmtc.Publisher) *Recorder {
	if ring == nil {
		ring = NewDefault()
	}
	return &Recorder{ring: ring, downstream: downstream}
}

func (r *Recorder) Ring() *Ring {
	return r.ring
}

func (r *Recorder) PublishTelemetry(tm tmtc.Telemetry) error {
	r.ring.Append(Entry{Kind: KindTelemetry, TM: &tm})
	if r.downstream == nil {
		return nil
	}
	return r.downstream.PublishTelemetry(tm)
}

func (r *Recorder) PublishCommandEvent(event tmtc.CommandEvent) error {
	r.ring.Append(Entry{Kind: KindCommandEvent, Event: &event})
	if r.downstream == nil {
		return nil
	}
	return r.downstream.PublishCommandEvent(event)
}

func (r *Recorder) RecordTelecommand(tc tmtc.Telecommand) Entry {
	return r.ring.Append(Entry{Kind: KindTelecommand, TC: &tc})
}

func (r *Recorder) ReplaySince(afterSeq uint64) []Entry {
	return r.ring.Since(afterSeq)
}
