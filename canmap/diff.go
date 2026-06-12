package canmap

import (
	"fmt"
	"sort"
)

// Verdict classifies one checked aspect of a signal.
type Verdict string

const (
	// VerdictMatch means the live device state equals the registry.
	VerdictMatch Verdict = "match"
	// VerdictDrift means the device disagrees with the registry.
	VerdictDrift Verdict = "drift"
	// VerdictUnknown means the device was not observable (offline, no
	// CANopen endpoint, or the role is unbound).
	VerdictUnknown Verdict = "unknown"
)

// Finding is one comparison result for one role within one signal.
type Finding struct {
	Signal  string  `json:"signal"`
	Role    string  `json:"role"`
	NodeID  byte    `json:"node_id,omitempty"`
	Aspect  string  `json:"aspect"`
	Verdict Verdict `json:"verdict"`
	Want    string  `json:"want,omitempty"`
	Got     string  `json:"got,omitempty"`
}

// SignalStatus aggregates the findings for one signal.
type SignalStatus struct {
	Signal   string    `json:"signal"`
	COBID    HexUint32 `json:"cob_id"`
	Verdict  Verdict   `json:"verdict"`
	Findings []Finding `json:"findings"`
}

// Diff compares a concrete registry against live observations, keyed by node
// ID. Roles whose nodes are missing from observed yield VerdictUnknown
// findings rather than errors, so a partially connected bench still gets a
// useful report.
func Diff(r *Registry, observed map[byte]*ObservedNode) []SignalStatus {
	out := make([]SignalStatus, 0, len(r.Signals))
	for _, sig := range r.Signals {
		st := SignalStatus{Signal: sig.Name, COBID: sig.COBID, Verdict: VerdictMatch}
		st.Findings = append(st.Findings, diffProducer(r, sig, observed)...)
		for _, c := range sig.Consumers {
			st.Findings = append(st.Findings, diffConsumer(r, sig, c, observed)...)
		}
		for _, f := range st.Findings {
			if f.Verdict == VerdictDrift {
				st.Verdict = VerdictDrift
				break
			}
			if f.Verdict == VerdictUnknown && st.Verdict == VerdictMatch {
				st.Verdict = VerdictUnknown
			}
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Signal < out[j].Signal })
	return out
}

func diffProducer(r *Registry, sig Signal, observed map[byte]*ObservedNode) []Finding {
	node, obs, f := resolve(r, sig.Name, sig.Producer.Role, "tpdo", observed)
	if f != nil {
		return []Finding{*f}
	}
	pdo, ok := pickPDO(obs.TPDOs, sig.Producer.TPDO)
	if !ok {
		return []Finding{drift(sig.Name, sig.Producer.Role, node.NodeID, "tpdo",
			fmt.Sprintf("TPDO%d configured", sig.Producer.TPDO), "PDO not present on device")}
	}
	return diffPDO(sig.Name, sig.Producer.Role, node.NodeID, "tpdo", pdo, sig.COBID, sig.Producer.Mapping)
}

func diffConsumer(r *Registry, sig Signal, c Consumer, observed map[byte]*ObservedNode) []Finding {
	node, obs, f := resolve(r, sig.Name, c.Role, "rpdo", observed)
	if f != nil {
		return []Finding{*f}
	}
	var findings []Finding
	pdo, ok := pickPDO(obs.RPDOs, c.RPDO)
	if !ok {
		findings = append(findings, drift(sig.Name, c.Role, node.NodeID, "rpdo",
			fmt.Sprintf("RPDO%d configured", c.RPDO), "PDO not present on device"))
	} else {
		findings = append(findings, diffPDO(sig.Name, c.Role, node.NodeID, "rpdo", pdo, sig.COBID, c.Mapping)...)
	}
	for _, w := range c.SourceSelects {
		key := fmt.Sprintf("0x%04X:%02X", uint16(w.Index), w.SubIndex)
		got, ok := obs.SourceSelects[key]
		switch {
		case !ok:
			findings = append(findings, Finding{Signal: sig.Name, Role: c.Role, NodeID: node.NodeID,
				Aspect: "source-select " + key, Verdict: VerdictUnknown,
				Want: fmt.Sprint(w.Value), Got: "not read"})
		case got != w.Value:
			findings = append(findings, drift(sig.Name, c.Role, node.NodeID, "source-select "+key,
				fmt.Sprint(w.Value), fmt.Sprint(got)))
		default:
			findings = append(findings, match(sig.Name, c.Role, node.NodeID, "source-select "+key))
		}
	}
	return findings
}

func diffPDO(signal, role string, nodeID byte, kind string, pdo ObservedPDO, wantCOB HexUint32, wantMap []MapEntry) []Finding {
	var findings []Finding
	if !pdo.Enabled {
		findings = append(findings, drift(signal, role, nodeID, kind+" enabled", "enabled", "disabled (COB-ID invalid bit set)"))
	}
	if pdo.COBID != wantCOB {
		findings = append(findings, drift(signal, role, nodeID, kind+" cob-id",
			fmt.Sprintf("0x%X", uint32(wantCOB)), fmt.Sprintf("0x%X", uint32(pdo.COBID))))
	}
	if !mapsEqual(pdo.Mapping, wantMap) {
		findings = append(findings, drift(signal, role, nodeID, kind+" mapping",
			formatMapping(wantMap), formatMapping(pdo.Mapping)))
	}
	if len(findings) == 0 {
		findings = append(findings, match(signal, role, nodeID, kind))
	}
	return findings
}

func resolve(r *Registry, signal, role, aspect string, observed map[byte]*ObservedNode) (Node, *ObservedNode, *Finding) {
	node, ok := r.NodeByRole(role)
	if !ok || node.NodeID == 0 {
		f := Finding{Signal: signal, Role: role, Aspect: aspect, Verdict: VerdictUnknown, Got: "role has no bound node"}
		return node, nil, &f
	}
	obs, ok := observed[node.NodeID]
	if !ok {
		f := Finding{Signal: signal, Role: role, NodeID: node.NodeID, Aspect: aspect, Verdict: VerdictUnknown, Got: "node not observed"}
		return node, nil, &f
	}
	return node, obs, nil
}

func pickPDO(pdos []ObservedPDO, number int) (ObservedPDO, bool) {
	for _, p := range pdos {
		if p.Number == number {
			return p, true
		}
	}
	return ObservedPDO{}, false
}

func mapsEqual(a, b []MapEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Raw() != b[i].Raw() {
			return false
		}
	}
	return true
}

func formatMapping(entries []MapEntry) string {
	if len(entries) == 0 {
		return "(empty)"
	}
	s := ""
	for i, e := range entries {
		if i > 0 {
			s += " "
		}
		s += fmt.Sprintf("0x%08X", e.Raw())
	}
	return s
}

func drift(signal, role string, nodeID byte, aspect, want, got string) Finding {
	return Finding{Signal: signal, Role: role, NodeID: nodeID, Aspect: aspect, Verdict: VerdictDrift, Want: want, Got: got}
}

func match(signal, role string, nodeID byte, aspect string) Finding {
	return Finding{Signal: signal, Role: role, NodeID: nodeID, Aspect: aspect, Verdict: VerdictMatch}
}
