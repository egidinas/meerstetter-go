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
