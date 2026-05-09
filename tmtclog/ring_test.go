package tmtclog

import (
	"testing"

	"github.com/egidinas/meerstetter-go/tmtc"
)

func TestRingKeepsNewestEntries(t *testing.T) {
	ring := New(2)
	ring.Append(Entry{Kind: KindTelemetry, TM: &tmtc.Telemetry{ID: "1"}})
	ring.Append(Entry{Kind: KindTelemetry, TM: &tmtc.Telemetry{ID: "2"}})
	ring.Append(Entry{Kind: KindTelemetry, TM: &tmtc.Telemetry{ID: "3"}})
	got := ring.Snapshot()
	if len(got) != 2 {
		t.Fatalf("entries = %d", len(got))
	}
	if got[0].TM.ID != "2" || got[1].TM.ID != "3" {
		t.Fatalf("snapshot = %#v", got)
	}
	if got[0].Seq != 2 || got[1].Seq != 3 {
		t.Fatalf("seq = %d, %d", got[0].Seq, got[1].Seq)
	}
}

func TestRingSinceReplaysAfterSequence(t *testing.T) {
	ring := New(4)
	ring.Append(Entry{Kind: KindTelemetry, TM: &tmtc.Telemetry{ID: "1"}})
	ring.Append(Entry{Kind: KindTelemetry, TM: &tmtc.Telemetry{ID: "2"}})
	ring.Append(Entry{Kind: KindTelemetry, TM: &tmtc.Telemetry{ID: "3"}})
	got := ring.Since(1)
	if len(got) != 2 || got[0].TM.ID != "2" || got[1].TM.ID != "3" {
		t.Fatalf("replay = %#v", got)
	}
	if ring.LatestSeq() != 3 {
		t.Fatalf("latest seq = %d", ring.LatestSeq())
	}
}

func TestRecorderStoresBeforeForwarding(t *testing.T) {
	downstream := &capturePublisher{}
	recorder := NewRecorder(New(4), downstream)
	tm := tmtc.Telemetry{ID: "tm-1"}
	if err := recorder.PublishTelemetry(tm); err != nil {
		t.Fatal(err)
	}
	event := tmtc.CommandEvent{ID: "event-1"}
	if err := recorder.PublishCommandEvent(event); err != nil {
		t.Fatal(err)
	}
	replay := recorder.ReplaySince(0)
	if len(replay) != 2 {
		t.Fatalf("replay = %#v", replay)
	}
	if len(downstream.tm) != 1 || len(downstream.events) != 1 {
		t.Fatalf("downstream tm=%d events=%d", len(downstream.tm), len(downstream.events))
	}
}

type capturePublisher struct {
	tm     []tmtc.Telemetry
	events []tmtc.CommandEvent
}

func (c *capturePublisher) PublishTelemetry(tm tmtc.Telemetry) error {
	c.tm = append(c.tm, tm)
	return nil
}

func (c *capturePublisher) PublishCommandEvent(event tmtc.CommandEvent) error {
	c.events = append(c.events, event)
	return nil
}
