package utility

import (
	"net/http"
	"time"

	"github.com/egidinas/loom-gossamer-shared/go/telemetrytiles"
	"github.com/egidinas/loom-gossamer-shared/go/discovery"
)

func (s *Server) handleTiles(w http.ResponseWriter, r *http.Request) {
	req := telemetrytiles.NormalizeRequest(telemetrytiles.Request{
		FromMS:    parseInt64Query(r, "from_ms"),
		ToMS:      parseInt64Query(r, "to_ms"),
		Width:     parseIntQuery(r, "width"),
		LatestSeq: s.recorder.Ring().LatestSeq(),
	}, time.Now())

	writeJSON(w, telemetrytiles.Build(
		s.recorder.Ring().Snapshot(),
		s.tileTargets(r),
		req,
	))
}

func (s *Server) tileTargets(r *http.Request) []discovery.Target {
	targetID := r.URL.Query().Get("target_id")
	aggregate := r.URL.Query().Get("aggregate")
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]discovery.Target, 0, len(s.targets))
	for _, target := range s.targets {
		if targetID != "" {
			if target.ID == targetID {
				return []discovery.Target{target}
			}
			continue
		}
		if aggregate != "" && graphGroupForTarget(target) == aggregate {
			out = append(out, target)
		}
	}
	return out
}
