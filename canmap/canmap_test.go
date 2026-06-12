package canmap

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func benchRegistry() *Registry {
	return &Registry{
		SchemaVersion: SchemaVersion,
		Name:          "bench-a",
		Nodes: []Node{
			{Role: "rmm", NodeID: 0x10, Family: "rmm-1182"},
			{Role: "tec-a", NodeID: 0x4B, Family: "tec-v6.32"},
			{Role: "tec-b", NodeID: 0x4C, Family: "tec-v6.32"},
		},
		Signals: []Signal{{
			COBID: 0x1A1,
			Name:  "bench_a_object_temp",
			Producer: Producer{Role: "rmm", TPDO: 2, Mapping: []MapEntry{
				{Index: 0x4000, SubIndex: 2, Bits: 32, Comment: "RMM ch2 converted temp"},
			}},
			Consumers: []Consumer{
				{Role: "tec-a", RPDO: 1,
					Mapping:       []MapEntry{{Index: 0x4200, SubIndex: 1, Bits: 32}},
					SourceSelects: []SDOWrite{{Index: 0x3300, SubIndex: 1, Value: 7}}},
				{Role: "tec-b", RPDO: 1,
					Mapping:       []MapEntry{{Index: 0x4200, SubIndex: 1, Bits: 32}},
					SourceSelects: []SDOWrite{{Index: 0x3300, SubIndex: 1, Value: 7}}},
			},
			RateMS: 50,
		}},
	}
}

func TestRegistryEncodeParseRoundTrip(t *testing.T) {
	r := benchRegistry()
	data, err := r.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(string(data), `"0x1A1"`) || !strings.Contains(string(data), `"0x4200"`) {
		t.Fatalf("expected hex-string encoding, got:\n%s", data)
	}
	back, err := ParseRegistry(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if back.Signals[0].COBID != 0x1A1 || back.Signals[0].Producer.Mapping[0].Raw() != 0x40000220 {
		t.Fatalf("round trip mismatch: %+v", back.Signals[0])
	}
}

func TestNumericHexFieldsRejectOutOfRange(t *testing.T) {
	// A numeric (non-string) index above 16 bits must error, not wrap.
	bad := []byte(`{"index": 70000, "subindex": 1, "bits": 32}`)
	var m MapEntry
	if err := json.Unmarshal(bad, &m); err == nil {
		t.Fatalf("expected out-of-range numeric index to error, got index=0x%X", uint16(m.Index))
	}
	// A numeric value within range still decodes.
	ok := []byte(`{"index": 16896, "subindex": 1, "bits": 32}`) // 16896 = 0x4200
	if err := json.Unmarshal(ok, &m); err != nil || m.Index != 0x4200 {
		t.Fatalf("in-range numeric index should decode: err=%v index=0x%X", err, uint16(m.Index))
	}
}

func TestValidateCatchesContractViolations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Registry)
		want   string
	}{
		{"duplicate cob-id", func(r *Registry) {
			s := r.Signals[0]
			s.Name = "other"
			r.Signals = append(r.Signals, s)
		}, "COB-ID 0x1A1 already produced"},
		{"unknown producer role", func(r *Registry) {
			r.Signals[0].Producer.Role = "ghost"
		}, `producer role "ghost" not in nodes`},
		{"duplicate node id", func(r *Registry) {
			r.Nodes[2].NodeID = 0x4B
		}, "node_id 0x4B bound to both"},
		{"slot length mismatch", func(r *Registry) {
			r.Signals[0].Consumers[0].Mapping[0].Bits = 16
		}, "do not mirror producer"},
		{"payload overflow", func(r *Registry) {
			m := r.Signals[0].Producer.Mapping[0]
			r.Signals[0].Producer.Mapping = []MapEntry{m, m, m}
		}, "exceeds 64-bit PDO payload"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := benchRegistry()
			tc.mutate(r)
			errs := r.Validate()
			if len(errs) == 0 {
				t.Fatal("expected validation errors")
			}
			if !strings.Contains(joinErrors(errs), tc.want) {
				t.Fatalf("want error containing %q, got: %s", tc.want, joinErrors(errs))
			}
		})
	}
	if errs := benchRegistry().Validate(); len(errs) != 0 {
		t.Fatalf("clean registry should validate: %s", joinErrors(errs))
	}
}

func TestPatternExportStripsHardwareAndInstantiateRebinds(t *testing.T) {
	r := benchRegistry()
	r.Signals[0].Verified = "2026-06-12"
	r.Signals[0].SavedToFlash = true

	p := ExportPattern(r)
	if !p.IsPattern() {
		t.Fatal("export should produce a pattern")
	}
	if p.Signals[0].Verified != "" || p.Signals[0].SavedToFlash {
		t.Fatal("pattern must not carry verification state")
	}
	if r.Nodes[0].NodeID != 0x10 {
		t.Fatal("export must not mutate the source registry")
	}

	copyReg, err := Instantiate(p, "bench-b", []Binding{
		{Role: "rmm", NodeID: 0x20},
		{Role: "tec-a", NodeID: 0x51, Label: "bench B left"},
		{Role: "tec-b", NodeID: 0x52},
	})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if copyReg.Name != "bench-b" || copyReg.IsPattern() {
		t.Fatalf("expected concrete bench-b registry, got %q pattern=%v", copyReg.Name, copyReg.IsPattern())
	}
	n, _ := copyReg.NodeByRole("tec-a")
	if n.NodeID != 0x51 || n.Label != "bench B left" {
		t.Fatalf("binding not applied: %+v", n)
	}
	if copyReg.Signals[0].COBID != 0x1A1 {
		t.Fatal("signal contract must survive instantiation")
	}

	if _, err := Instantiate(p, "x", []Binding{{Role: "rmm", NodeID: 0x20}}); err == nil ||
		!strings.Contains(err.Error(), "unbound pattern roles") {
		t.Fatalf("want unbound-roles error, got %v", err)
	}
	if _, err := Instantiate(p, "x", []Binding{
		{Role: "rmm", NodeID: 0x20}, {Role: "tec-a", NodeID: 0x20}, {Role: "tec-b", NodeID: 0x21},
	}); err == nil || !strings.Contains(err.Error(), "bound to both") {
		t.Fatalf("want duplicate-node error, got %v", err)
	}
	if _, err := Instantiate(p, "x", []Binding{
		{Role: "rmm", NodeID: 0x20}, {Role: "tec-a", NodeID: 0x21},
		{Role: "tec-b", NodeID: 0x22}, {Role: "ghost", NodeID: 0x23},
	}); err == nil || !strings.Contains(err.Error(), "unknown roles") {
		t.Fatalf("want unknown-roles error, got %v", err)
	}
}

// fakeSDO simulates one node's object dictionary for read-back tests.
type fakeSDO map[string][]byte

func sdoKey(index uint16, sub byte) string { return fmt.Sprintf("%04X:%02X", index, sub) }

func (f fakeSDO) ReadSDORaw(_ context.Context, index uint16, sub byte) ([]byte, error) {
	if data, ok := f[sdoKey(index, sub)]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("SDO abort 0x06020000 (object does not exist)")
}

func le32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// tecLike builds a fake TEC consumer with RPDO1 on the given COB-ID mapped to
// 0x4200:01 and source select 0x3300:01 = sel.
func tecLike(cob uint32, sel int32) fakeSDO {
	return fakeSDO{
		sdoKey(0x1400, 1): le32(cob),
		sdoKey(0x1400, 2): {0xFE},
		sdoKey(0x1600, 0): {1},
		sdoKey(0x1600, 1): le32(0x42000120),
		sdoKey(0x1800, 1): le32(cobIDInvalidBit | 0x1CB),
		sdoKey(0x1A00, 0): {0},
		sdoKey(0x3300, 1): le32(uint32(sel)),
	}
}

func TestObserveNodeReadsPDOTablesAndSourceSelects(t *testing.T) {
	dev := tecLike(0x1A1, 7)
	obs, err := ObserveNode(context.Background(), dev, 0x4B, []SDOWrite{{Index: 0x3300, SubIndex: 1, Value: 7}})
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(obs.RPDOs) != 1 || obs.RPDOs[0].COBID != 0x1A1 || !obs.RPDOs[0].Enabled {
		t.Fatalf("unexpected RPDOs: %+v", obs.RPDOs)
	}
	if obs.RPDOs[0].TransmissionType != 0xFE {
		t.Fatalf("transmission type: %+v", obs.RPDOs[0])
	}
	if got := obs.RPDOs[0].Mapping; len(got) != 1 || got[0].Raw() != 0x42000120 {
		t.Fatalf("unexpected mapping: %+v", got)
	}
	if len(obs.TPDOs) != 1 || obs.TPDOs[0].Enabled || obs.TPDOs[0].COBID != 0x1CB {
		t.Fatalf("invalid-bit handling wrong: %+v", obs.TPDOs)
	}
	if obs.SourceSelects["0x3300:01"] != 7 {
		t.Fatalf("source select not read: %+v", obs.SourceSelects)
	}
}

func TestDiffReportsMatchDriftAndUnknown(t *testing.T) {
	r := benchRegistry()
	rmm := fakeSDO{
		sdoKey(0x1800, 1): le32(cobIDInvalidBit | 0x190),
		sdoKey(0x1A00, 0): {0},
		sdoKey(0x1801, 1): le32(0x1A1),
		sdoKey(0x1801, 2): {0xFE},
		sdoKey(0x1A01, 0): {1},
		sdoKey(0x1A01, 1): le32(0x40000220),
		sdoKey(0x1400, 1): nil, // absent: triggers scan stop on first read error
	}
	delete(rmm, sdoKey(0x1400, 1))

	observed := map[byte]*ObservedNode{}
	for nodeID, dev := range map[byte]fakeSDO{
		0x10: rmm,
		0x4B: tecLike(0x1A1, 7), // matches registry
		0x4C: tecLike(0x2A2, 5), // wrong COB-ID and source select
	} {
		wants := []SDOWrite{{Index: 0x3300, SubIndex: 1, Value: 7}}
		obs, err := ObserveNode(context.Background(), dev, nodeID, wants)
		if err != nil {
			t.Fatalf("observe 0x%02X: %v", nodeID, err)
		}
		observed[nodeID] = obs
	}

	statuses := Diff(r, observed)
	if len(statuses) != 1 {
		t.Fatalf("want one signal status, got %d", len(statuses))
	}
	st := statuses[0]
	if st.Verdict != VerdictDrift {
		t.Fatalf("want drift verdict, got %s: %+v", st.Verdict, st.Findings)
	}
	byKey := map[string]Finding{}
	for _, f := range st.Findings {
		byKey[f.Role+"/"+f.Aspect] = f
	}
	if byKey["rmm/tpdo"].Verdict != VerdictMatch {
		t.Fatalf("rmm producer should match: %+v", byKey["rmm/tpdo"])
	}
	if byKey["tec-a/rpdo"].Verdict != VerdictMatch || byKey["tec-a/source-select 0x3300:01"].Verdict != VerdictMatch {
		t.Fatalf("tec-a should match: %+v", st.Findings)
	}
	if f := byKey["tec-b/rpdo cob-id"]; f.Verdict != VerdictDrift || f.Got != "0x2A2" {
		t.Fatalf("tec-b cob-id drift not reported: %+v", f)
	}
	if f := byKey["tec-b/source-select 0x3300:01"]; f.Verdict != VerdictDrift || f.Got != "5" {
		t.Fatalf("tec-b source-select drift not reported: %+v", f)
	}

	// Unobserved node downgrades to unknown, never errors.
	delete(observed, 0x10)
	st = Diff(r, observed)[0]
	if byRole := findingsFor(st, "rmm"); len(byRole) != 1 || byRole[0].Verdict != VerdictUnknown {
		t.Fatalf("missing node should be unknown: %+v", byRole)
	}
}

func findingsFor(st SignalStatus, role string) []Finding {
	var out []Finding
	for _, f := range st.Findings {
		if f.Role == role {
			out = append(out, f)
		}
	}
	return out
}
