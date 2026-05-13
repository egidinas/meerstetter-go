package utility

import (
	"net/http"
	"strings"
	"time"

	"github.com/egidinas/loom-gossamer-shared/go/discovery"
	"github.com/egidinas/loom-gossamer-shared/go/telemetrytiles"
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
	exactTargetID := targetID
	if strings.HasPrefix(exactTargetID, "aggregate:") {
		exactTargetID = ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]discovery.Target, 0, len(s.targets))
	for _, target := range s.targets {
		if exactTargetID != "" {
			if target.ID == exactTargetID {
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
