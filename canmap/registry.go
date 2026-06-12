// Package canmap defines the CAN signal registry: a version-controlled,
// machine-checkable description of every PDO contract on a bus — which node
// produces which value under which COB-ID, and which nodes consume it into
// which object-dictionary cells.
//
// The registry exists in two forms:
//
//   - A concrete registry binds every role to a real CANopen node ID. This is
//     what one physical testbed runs.
//   - A pattern is the same registry with the node IDs stripped: only roles
//     remain. A pattern can be exported from one testbed and imported on a
//     copy by supplying new role-to-node bindings.
//
// The package is dependency-free by design: live read-back only needs an
// SDOReader, which mecom.CANopenClient satisfies structurally.
package canmap

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SchemaVersion identifies the registry JSON schema produced by this package.
const SchemaVersion = 1

// Registry is the single source of truth for the PDO contracts on one bus.
type Registry struct {
	SchemaVersion int      `json:"schema_version"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Nodes         []Node   `json:"nodes"`
	Signals       []Signal `json:"signals"`
}

// Node binds a symbolic role to a concrete device. In a pattern export the
// NodeID is zero and only the role and device family survive.
type Node struct {
	// Role is the stable symbolic name used by signals, e.g. "rmm" or
	// "tec-a". Roles outlive hardware swaps and testbed copies.
	Role string `json:"role"`
	// NodeID is the CANopen node ID (1..127). Zero in a pattern.
	NodeID byte `json:"node_id,omitempty"`
	// Family is the device family, e.g. "tec-v6.32" or "rmm-1182". It is
	// carried into patterns so an import can sanity-check the binding.
	Family string `json:"family,omitempty"`
	// Label is free-form human context ("bench A, left controller").
	Label string `json:"label,omitempty"`
}

// Signal is one PDO contract: a producer, a wire identifier, a payload
// layout, and any number of consumers. The COB-ID is the primary key.
type Signal struct {
	// COBID is the CAN identifier the producer transmits under.
	COBID HexUint32 `json:"cob_id"`
	// Name is a stable human name for the signal ("bench_a_object_temp").
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Producer describes the transmitting side.
	Producer Producer `json:"producer"`
	// Consumers describe every node configured to receive this COB-ID.
	Consumers []Consumer `json:"consumers"`
	// RateMS is the intended publish period in milliseconds. For TEC
	// external-temperature targets this must be 100 or less.
	RateMS int `json:"rate_ms,omitempty"`
	// SavedToFlash records whether the configuration was persisted.
	SavedToFlash bool `json:"saved_to_flash,omitempty"`
	// Verified is the date (YYYY-MM-DD) of the last successful live
	// read-back against this row, maintained by the verify tooling.
	Verified string `json:"verified,omitempty"`
}

// Producer is the transmitting half of a signal.
type Producer struct {
	// Role references a Node role.
	Role string `json:"role"`
	// TPDO is the 1-based transmit PDO number on the producer.
	TPDO int `json:"tpdo"`
	// Mapping lists the TPDO mapping entries in payload order.
	Mapping []MapEntry `json:"mapping"`
}

// Consumer is one receiving half of a signal.
type Consumer struct {
	// Role references a Node role.
	Role string `json:"role"`
	// RPDO is the 1-based receive PDO number on the consumer.
	RPDO int `json:"rpdo"`
	// Mapping lists the RPDO mapping entries in payload order. The bit
	// lengths must mirror the producer mapping slot for slot.
	Mapping []MapEntry `json:"mapping"`
	// SourceSelects are SDO writes that route the received value into the
	// control loop, e.g. object 0x3300:01 value 7 on a TEC.
	SourceSelects []SDOWrite `json:"source_selects,omitempty"`
}

// MapEntry is one PDO mapping slot: which object-dictionary cell occupies
// which bits of the payload.
type MapEntry struct {
	Index    HexUint16 `json:"index"`
	SubIndex byte      `json:"subindex"`
	Bits     byte      `json:"bits"`
	// Comment carries the human meaning ("RMM ch2 converted temp, °C").
	Comment string `json:"comment,omitempty"`
}

// SDOWrite is a documented configuration write, used for source selection.
type SDOWrite struct {
	Index    HexUint16 `json:"index"`
	SubIndex byte      `json:"subindex"`
	Value    int32     `json:"value"`
	Comment  string    `json:"comment,omitempty"`
}

// Raw encodes the mapping entry in the CANopen on-wire form
// (index<<16 | subindex<<8 | bits).
func (m MapEntry) Raw() uint32 {
	return uint32(m.Index)<<16 | uint32(m.SubIndex)<<8 | uint32(m.Bits)
}

// DecodeMapEntry splits a raw 32-bit CANopen mapping value.
func DecodeMapEntry(raw uint32) MapEntry {
	return MapEntry{
		Index:    HexUint16(raw >> 16),
		SubIndex: byte(raw >> 8),
		Bits:     byte(raw),
	}
}

// HexUint16 marshals as a "0x...." JSON string so registry files stay
// readable next to bus captures and the handbook.
type HexUint16 uint16

// HexUint32 marshals as a "0x..." JSON string.
type HexUint32 uint32

func (h HexUint16) MarshalJSON() ([]byte, error) {
	return json.Marshal(fmt.Sprintf("0x%04X", uint16(h)))
}

func (h HexUint32) MarshalJSON() ([]byte, error) {
	return json.Marshal(fmt.Sprintf("0x%X", uint32(h)))
}

func (h *HexUint16) UnmarshalJSON(data []byte) error {
	v, err := unmarshalHex(data, 16)
	*h = HexUint16(v)
	return err
}

func (h *HexUint32) UnmarshalJSON(data []byte) error {
	v, err := unmarshalHex(data, 32)
	*h = HexUint32(v)
	return err
}

func unmarshalHex(data []byte, bits int) (uint64, error) {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		// Tolerate plain JSON numbers for hand-written files, but hold them to
		// the same bit width as the string path so an out-of-range index or
		// COB-ID is rejected rather than silently wrapping on the cast.
		var n uint64
		if errNum := json.Unmarshal(data, &n); errNum == nil {
			if bits < 64 && n > (uint64(1)<<bits)-1 {
				return 0, fmt.Errorf("canmap: numeric value %d exceeds %d-bit field", n, bits)
			}
			return n, nil
		}
		return 0, err
	}
	s = strings.TrimPrefix(strings.TrimSpace(s), "0x")
	v, err := strconv.ParseUint(s, 16, bits)
	if err != nil {
		return 0, fmt.Errorf("canmap: invalid hex value %q: %w", s, err)
	}
	return v, nil
}

// ParseRegistry decodes and validates a registry JSON document.
func ParseRegistry(data []byte) (*Registry, error) {
	var r Registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("canmap: parse registry: %w", err)
	}
	if r.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("canmap: unsupported schema_version %d (want %d)", r.SchemaVersion, SchemaVersion)
	}
	if errs := r.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("canmap: invalid registry: %s", joinErrors(errs))
	}
	return &r, nil
}

// Encode renders the registry as stable, human-diffable JSON.
func (r *Registry) Encode() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// NodeByRole returns the node bound to a role.
func (r *Registry) NodeByRole(role string) (Node, bool) {
	for _, n := range r.Nodes {
		if n.Role == role {
			return n, true
		}
	}
	return Node{}, false
}

// IsPattern reports whether the registry has no concrete node bindings.
func (r *Registry) IsPattern() bool {
	for _, n := range r.Nodes {
		if n.NodeID != 0 {
			return false
		}
	}
	return len(r.Nodes) > 0
}

// Validate returns every structural problem found. A registry with zero
// problems is safe to use as a configuration source of truth; validation
// does not require concrete node bindings, so patterns validate too.
func (r *Registry) Validate() []error {
	var errs []error
	roles := map[string]Node{}
	for i, n := range r.Nodes {
		if n.Role == "" {
			errs = append(errs, fmt.Errorf("nodes[%d]: empty role", i))
			continue
		}
		if _, dup := roles[n.Role]; dup {
			errs = append(errs, fmt.Errorf("nodes[%d]: duplicate role %q", i, n.Role))
		}
		if n.NodeID > 127 {
			errs = append(errs, fmt.Errorf("nodes[%d] (%s): node_id %d out of CANopen range 1..127", i, n.Role, n.NodeID))
		}
		roles[n.Role] = n
	}
	nodeIDs := map[byte]string{}
	for _, n := range r.Nodes {
		if n.NodeID == 0 {
			continue
		}
		if prev, dup := nodeIDs[n.NodeID]; dup {
			errs = append(errs, fmt.Errorf("node_id 0x%02X bound to both roles %q and %q", n.NodeID, prev, n.Role))
		}
		nodeIDs[n.NodeID] = n.Role
	}

	cobIDs := map[HexUint32]string{}
	names := map[string]bool{}
	for i, sig := range r.Signals {
		where := fmt.Sprintf("signals[%d] (%s)", i, sig.Name)
		if sig.Name == "" {
			errs = append(errs, fmt.Errorf("signals[%d]: empty name", i))
		} else if names[sig.Name] {
			errs = append(errs, fmt.Errorf("%s: duplicate signal name", where))
		}
		names[sig.Name] = true
		if prev, dup := cobIDs[sig.COBID]; dup {
			errs = append(errs, fmt.Errorf("%s: COB-ID 0x%X already produced by signal %q", where, uint32(sig.COBID), prev))
		}
		cobIDs[sig.COBID] = sig.Name
		if _, ok := roles[sig.Producer.Role]; !ok {
			errs = append(errs, fmt.Errorf("%s: producer role %q not in nodes", where, sig.Producer.Role))
		}
		if sig.Producer.TPDO < 1 {
			errs = append(errs, fmt.Errorf("%s: producer tpdo must be 1-based", where))
		}
		prodBits, err := mappingBits(sig.Producer.Mapping)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: producer mapping: %w", where, err))
		}
		for j, c := range sig.Consumers {
			cw := fmt.Sprintf("%s consumers[%d]", where, j)
			if _, ok := roles[c.Role]; !ok {
				errs = append(errs, fmt.Errorf("%s: role %q not in nodes", cw, c.Role))
			}
			if c.RPDO < 1 {
				errs = append(errs, fmt.Errorf("%s: rpdo must be 1-based", cw))
			}
			consBits, err := mappingBits(c.Mapping)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: mapping: %w", cw, err))
				continue
			}
			if err == nil && prodBits != nil && !slotLengthsMatch(sig.Producer.Mapping, c.Mapping) {
				errs = append(errs, fmt.Errorf("%s: slot bit lengths %v do not mirror producer %v", cw, bitsOf(c.Mapping), bitsOf(sig.Producer.Mapping)))
			}
			_ = consBits
		}
	}
	return errs
}

// mappingBits checks one PDO mapping fits a classic 8-byte frame and returns
// the per-slot bit lengths.
func mappingBits(entries []MapEntry) ([]byte, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("empty mapping")
	}
	total := 0
	bits := make([]byte, len(entries))
	for i, e := range entries {
		if e.Bits == 0 {
			return nil, fmt.Errorf("slot %d has zero bit length", i+1)
		}
		bits[i] = e.Bits
		total += int(e.Bits)
	}
	if total > 64 {
		return nil, fmt.Errorf("mapping is %d bits, exceeds 64-bit PDO payload", total)
	}
	return bits, nil
}

func slotLengthsMatch(a, b []MapEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Bits != b[i].Bits {
			return false
		}
	}
	return true
}

func bitsOf(entries []MapEntry) []byte {
	out := make([]byte, len(entries))
	for i, e := range entries {
		out[i] = e.Bits
	}
	return out
}

func joinErrors(errs []error) string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}
