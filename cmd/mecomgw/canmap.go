package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egidinas/meerstetter-go/canmap"
)

// canmapState holds the gateway's loaded CAN signal registry. The registry
// file is the source of truth; the gateway never writes device configuration
// from it — it only reads device state back and reports drift.
//
// The registry pointer is read by concurrent GET/export/live requests and
// swapped by imports, so all access goes through the mutex. A loaded registry
// is treated as immutable once published, so a snapshot pointer taken under
// the read lock stays safe to use after the lock is released.
type canmapState struct {
	path     string
	mu       sync.RWMutex
	registry *canmap.Registry
}

// current returns the currently loaded registry, or nil. Safe for concurrent use.
func (st *canmapState) current() *canmap.Registry {
	if st == nil {
		return nil
	}
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.registry
}

// replace atomically swaps in a new registry and persists it, rolling back the
// in-memory pointer if the write fails so a failed import never leaves a
// published registry that was not saved.
func (st *canmapState) replace(reg *canmap.Registry) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	prev := st.registry
	st.registry = reg
	if err := st.saveLocked(); err != nil {
		st.registry = prev
		return err
	}
	return nil
}

func loadCanmap(path string) (*canmapState, error) {
	st := &canmapState{path: path}
	if path == "" {
		return st, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, fmt.Errorf("canmap registry %s: %w", path, err)
	}
	st.registry, err = canmap.ParseRegistry(data)
	if err != nil {
		return nil, fmt.Errorf("canmap registry %s: %w", path, err)
	}
	return st, nil
}

// saveLocked persists the current registry. The caller must hold st.mu.
func (st *canmapState) saveLocked() error {
	if st.path == "" {
		return fmt.Errorf("gateway started without -canmap path; registry is read-only")
	}
	data, err := st.registry.Encode()
	if err != nil {
		return err
	}
	return os.WriteFile(st.path, append(data, '\n'), 0o644)
}

// canNodeID extracts the CANopen node ID from a "can:IFACE/NODE" endpoint.
func canNodeID(endpoint string) (byte, bool) {
	rest, ok := strings.CutPrefix(endpoint, "can:")
	if !ok {
		return 0, false
	}
	_, nodeStr, ok := strings.Cut(rest, "/")
	if !ok {
		return 0, false
	}
	nodeStr = strings.TrimSpace(nodeStr)
	base := 10
	if hexStr, isHex := strings.CutPrefix(strings.ToLower(nodeStr), "0x"); isHex {
		nodeStr, base = hexStr, 16
	}
	n, err := strconv.ParseUint(nodeStr, base, 8)
	if err != nil || n == 0 || n > 127 {
		return 0, false
	}
	return byte(n), true
}

// observeRegistryNodes reads live PDO configuration from every connected
// device whose CANopen node ID appears in the registry. Devices without a
// CANopen endpoint or without an SDO-capable client are skipped; Diff
// reports those roles as unknown.
func (s *server) observeRegistryNodes(r *http.Request, reg *canmap.Registry) map[byte]*canmap.ObservedNode {
	wantsByNode := map[byte][]canmap.SDOWrite{}
	roleByNode := map[byte]string{}
	for _, n := range reg.Nodes {
		if n.NodeID != 0 {
			roleByNode[n.NodeID] = n.Role
		}
	}
	for _, sig := range reg.Signals {
		for _, c := range sig.Consumers {
			if n, ok := reg.NodeByRole(c.Role); ok && n.NodeID != 0 {
				wantsByNode[n.NodeID] = append(wantsByNode[n.NodeID], c.SourceSelects...)
			}
		}
	}

	observed := map[byte]*canmap.ObservedNode{}
	for _, b := range s.orderedDeviceBindings() {
		nodeID, ok := canNodeID(b.cfg.Endpoint)
		if !ok {
			continue
		}
		if _, wanted := roleByNode[nodeID]; !wanted {
			continue
		}
		if _, done := observed[nodeID]; done {
			continue
		}
		// Open the device client if it is not already memoized, instead of
		// only reading a client some other endpoint happened to bind. Without
		// this, the first /api/canmap?live=1 after startup reports every node
		// as unknown. A bind failure leaves the node unobserved, which Diff
		// reports as unknown — the correct verdict for an unreachable node.
		snap, err := s.bind(b.cfg.ID)
		if err != nil {
			continue
		}
		reader, ok := snap.client.(canmap.SDOReader)
		if !ok || snap.client == nil {
			continue
		}
		obs, err := canmap.ObserveNode(r.Context(), reader, nodeID, wantsByNode[nodeID])
		if err != nil {
			if obs == nil {
				obs = &canmap.ObservedNode{NodeID: nodeID}
			}
			obs.Errors = append(obs.Errors, err.Error())
		}
		observed[nodeID] = obs
	}
	return observed
}

// handleCanmap serves GET /api/canmap. With ?live=1 it additionally reads
// back the PDO configuration of every reachable registry node over SDO and
// reports per-signal match/drift/unknown verdicts.
func (s *server) handleCanmap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "GET only"})
		return
	}
	reg := s.canmap.current()
	if reg == nil {
		writeJSON(w, http.StatusOK, map[string]any{"registry": nil})
		return
	}
	body := map[string]any{"registry": reg, "is_pattern": reg.IsPattern()}
	if r.URL.Query().Get("live") == "1" && !reg.IsPattern() {
		observed := s.observeRegistryNodes(r, reg)
		body["observed"] = observed
		body["status"] = canmap.Diff(reg, observed)
		body["observed_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, body)
}

// handleCanmapExport serves GET /api/canmap/export. format=registry (default)
// returns the concrete registry; format=pattern strips node bindings so the
// file can seed a copy of this testbed.
func (s *server) handleCanmapExport(w http.ResponseWriter, r *http.Request) {
	reg := s.canmap.current()
	if reg == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no CAN signal registry loaded"})
		return
	}
	name := reg.Name + "-registry"
	if r.URL.Query().Get("format") == "pattern" {
		reg = canmap.ExportPattern(reg)
		name = reg.Name + "-pattern"
	}
	data, err := reg.Encode()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+".json"))
	w.Write(append(data, '\n'))
}

// canmapImportRequest is the POST /api/canmap/import body. Either a concrete
// registry, or a pattern plus bindings that instantiate it for this testbed.
type canmapImportRequest struct {
	Registry *canmap.Registry `json:"registry,omitempty"`
	Pattern  *canmap.Registry `json:"pattern,omitempty"`
	Name     string           `json:"name,omitempty"`
	Bindings []canmap.Binding `json:"bindings,omitempty"`
}

// handleCanmapImport replaces the gateway's registry and persists it to the
// -canmap file. It never writes to devices: after an import, GET
// /api/canmap?live=1 shows the drift between the imported intent and the
// actual device state, which is exactly the to-do list for bringing a copied
// testbed in line.
func (s *server) handleCanmapImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST only"})
		return
	}
	if s.canmap == nil || s.canmap.path == "" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "gateway started without -canmap path; registry is read-only"})
		return
	}
	var req canmapImportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON: " + err.Error()})
		return
	}
	var reg *canmap.Registry
	switch {
	case req.Registry != nil && req.Pattern != nil:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provide either registry or pattern, not both"})
		return
	case req.Registry != nil:
		if req.Registry.IsPattern() {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "registry has no node bindings; import it as pattern with bindings"})
			return
		}
		if errs := req.Registry.Validate(); len(errs) > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("invalid registry: %v", errs)})
			return
		}
		reg = req.Registry
	case req.Pattern != nil:
		instantiated, err := canmap.Instantiate(req.Pattern, req.Name, req.Bindings)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		reg = instantiated
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "body must carry registry or pattern"})
		return
	}
	// Reject schema versions loadCanmap would refuse on the next restart, so a
	// 200 here never persists a file that becomes unusable after a reboot.
	// Covers both the concrete-registry and instantiated-pattern paths.
	if reg.SchemaVersion != canmap.SchemaVersion {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": fmt.Sprintf("unsupported schema_version %d (want %d)", reg.SchemaVersion, canmap.SchemaVersion),
		})
		return
	}
	if err := s.canmap.replace(reg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "persist registry: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"registry": reg, "is_pattern": false})
}
