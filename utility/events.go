package utility

import (
	"strings"
	"time"

	"github.com/egidinas/loom-gossamer-shared/go/tmtclog"
)

type SwimlaneEvent struct {
	Seq      uint64    `json:"seq"`
	Time     time.Time `json:"time"`
	Lane     string    `json:"lane"`
	Severity string    `json:"severity"`
	TargetID string    `json:"target_id,omitempty"`
	Title    string    `json:"title"`
	Detail   string    `json:"detail,omitempty"`
}

func swimlaneEvents(entries []tmtclog.Entry) []SwimlaneEvent {
	out := make([]SwimlaneEvent, 0, len(entries))
	for _, entry := range entries {
		switch {
		case entry.Event != nil:
			severity := "info"
			if entry.Event.Error != "" || strings.Contains(string(entry.Event.Status), "failed") || strings.Contains(string(entry.Event.Status), "rejected") {
				severity = "error"
			}
			out = append(out, SwimlaneEvent{
				Seq:      entry.Seq,
				Time:     entry.Time,
				Lane:     "commands",
				Severity: severity,
				Title:    string(entry.Event.Status),
				Detail:   entry.Event.Error,
			})
		case entry.TM != nil && entry.TM.Quality != "" && entry.TM.Quality != "ok":
			severity := "warning"
			if strings.Contains(strings.ToLower(entry.TM.Quality), "error") || strings.Contains(strings.ToLower(entry.TM.Quality), "fault") {
				severity = "error"
			}
			out = append(out, SwimlaneEvent{
				Seq:      entry.Seq,
				Time:     entry.Time,
				Lane:     "telemetry",
				Severity: severity,
				TargetID: entry.TM.TargetID,
				Title:    entry.TM.Name,
				Detail:   entry.TM.Quality,
			})
		}
	}
	return out
}
