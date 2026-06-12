package canmap

import (
	"context"
	"encoding/binary"
	"fmt"
)

// SDOReader is the minimal device access needed for live read-back.
// mecom.CANopenClient satisfies it structurally.
type SDOReader interface {
	ReadSDORaw(ctx context.Context, index uint16, subIndex byte) ([]byte, error)
}

// maxScanPDOs bounds the per-node PDO scan. The RMM-1182 exposes 16 PDOs per
// direction, the TEC four; scanning stops at the first absent object anyway.
const maxScanPDOs = 16

// ObservedPDO is the live configuration of one PDO as read from a node.
type ObservedPDO struct {
	// Number is the 1-based PDO number.
	Number int `json:"number"`
	// COBID is the configured identifier with the validity bit masked off.
	COBID HexUint32 `json:"cob_id"`
	// Enabled is false when bit 31 ("PDO invalid") is set in the COB-ID.
	Enabled bool `json:"enabled"`
	// TransmissionType is communication parameter subindex 2.
	TransmissionType byte `json:"transmission_type"`
	// Mapping lists the live mapping entries in payload order.
	Mapping []MapEntry `json:"mapping"`
}

// ObservedNode is everything read back from one node.
type ObservedNode struct {
	NodeID byte          `json:"node_id"`
	RPDOs  []ObservedPDO `json:"rpdos"`
	TPDOs  []ObservedPDO `json:"tpdos"`
	// SourceSelects holds the live values of every source-selection object
	// the registry mentions for this node, keyed "0xIIII:SS".
	SourceSelects map[string]int32 `json:"source_selects,omitempty"`
	// Errors records read failures that did not abort the scan.
	Errors []string `json:"errors,omitempty"`
}

const (
	rpdoCommBase = 0x1400
	rpdoMapBase  = 0x1600
	tpdoCommBase = 0x1800
	tpdoMapBase  = 0x1A00

	cobIDInvalidBit = uint32(1) << 31
)

// ObserveNode reads the live PDO configuration of one node over SDO. It is
// strictly read-only. wants lists the source-selection objects to read in
// addition to the PDO tables (pass the registry's SourceSelects for the
// node's roles); their live values are reported without judgement here —
// Diff does the comparing.
func ObserveNode(ctx context.Context, r SDOReader, nodeID byte, wants []SDOWrite) (*ObservedNode, error) {
	obs := &ObservedNode{NodeID: nodeID}
	var err error
	obs.RPDOs, err = scanPDOs(ctx, r, rpdoCommBase, rpdoMapBase)
	if err != nil {
		return obs, fmt.Errorf("canmap: node 0x%02X RPDO scan: %w", nodeID, err)
	}
	obs.TPDOs, err = scanPDOs(ctx, r, tpdoCommBase, tpdoMapBase)
	if err != nil {
		return obs, fmt.Errorf("canmap: node 0x%02X TPDO scan: %w", nodeID, err)
	}
	for _, w := range wants {
		key := fmt.Sprintf("0x%04X:%02X", uint16(w.Index), w.SubIndex)
		if obs.SourceSelects == nil {
			obs.SourceSelects = map[string]int32{}
		}
		if _, done := obs.SourceSelects[key]; done {
			continue
		}
		data, err := r.ReadSDORaw(ctx, uint16(w.Index), w.SubIndex)
		if err != nil {
			obs.Errors = append(obs.Errors, fmt.Sprintf("read %s: %v", key, err))
			continue
		}
		obs.SourceSelects[key] = int32(leUint32(data))
	}
	return obs, nil
}

// scanPDOs walks consecutive PDO communication/mapping records until the
// first absent record or the scan bound. A read failure on the very first
// record is treated as "no PDOs of this kind", not an error, because devices
// legitimately differ in PDO count.
func scanPDOs(ctx context.Context, r SDOReader, commBase, mapBase uint16) ([]ObservedPDO, error) {
	var out []ObservedPDO
	for i := 0; i < maxScanPDOs; i++ {
		raw, err := r.ReadSDORaw(ctx, commBase+uint16(i), 1)
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			break
		}
		pdo := ObservedPDO{Number: i + 1}
		cob := leUint32(raw)
		pdo.Enabled = cob&cobIDInvalidBit == 0
		pdo.COBID = HexUint32(cob &^ cobIDInvalidBit)
		if tt, err := r.ReadSDORaw(ctx, commBase+uint16(i), 2); err == nil && len(tt) > 0 {
			pdo.TransmissionType = tt[0]
		}
		count, err := r.ReadSDORaw(ctx, mapBase+uint16(i), 0)
		if err == nil && len(count) > 0 {
			n := int(count[0])
			if n > 8 {
				n = 8
			}
			for s := 1; s <= n; s++ {
				entry, err := r.ReadSDORaw(ctx, mapBase+uint16(i), byte(s))
				if err != nil {
					break
				}
				pdo.Mapping = append(pdo.Mapping, DecodeMapEntry(leUint32(entry)))
			}
		}
		out = append(out, pdo)
	}
	return out, nil
}

func leUint32(data []byte) uint32 {
	var buf [4]byte
	copy(buf[:], data)
	return binary.LittleEndian.Uint32(buf[:])
}
