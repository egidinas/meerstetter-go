//go:build linux

package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/canmap"
	"github.com/egidinas/meerstetter-go/canopen"
	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/socketcan"
)

type objectSpec struct {
	index    uint16
	subIndex byte
}

type pdoPlanInput struct {
	dir              string
	nodeID           byte
	pdoNumber        int
	cobID            uint32
	transmissionType byte
	setEventTimer    bool
	eventTimerMS     uint16
	leaveDisabled    bool
	allowSub0Mapping bool
	mapping          []canmap.MapEntry
}

type pdoPlan struct {
	steps []planStep
}

type planStep struct {
	kind      string
	nodeID    byte
	index     uint16
	subIndex  byte
	valueKind string
	value     []byte
}

func (s planStep) String() string {
	switch s.kind {
	case "nmt-preop", "nmt-start":
		return s.kind
	case "sdo-write":
		return fmt.Sprintf("sdo-write 0x%04X:%02X %s %s", s.index, s.subIndex, s.valueKind, formatValue(s.valueKind, s.value))
	default:
		return s.kind
	}
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "sdo-read":
		err = runSDORead(os.Args[2:])
	case "sdo-write":
		err = runSDOWrite(os.Args[2:])
	case "nmt":
		err = runNMT(os.Args[2:])
	case "pdo-apply":
		err = runPDOApply(os.Args[2:])
	case "pdo-send":
		err = runPDOSend(os.Args[2:])
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: canpdoctl <sdo-read|sdo-write|nmt|pdo-apply|pdo-send> [flags]")
}

func runSDORead(args []string) error {
	fs := flag.NewFlagSet("sdo-read", flag.ContinueOnError)
	iface := fs.String("if", "can0", "SocketCAN interface")
	nodeArg := fs.String("node", "", "CANopen node ID, e.g. 0x51")
	objectArg := fs.String("object", "", "object index:subindex, e.g. 0x4200:1")
	kind := fs.String("type", "uint32", "value type: byte|uint32|int32|float32|uint16")
	timeout := fs.Duration("timeout", 500*time.Millisecond, "SDO response timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	node, err := parseNode(*nodeArg)
	if err != nil {
		return err
	}
	obj, err := parseObjectSpec(*objectArg)
	if err != nil {
		return err
	}
	conn, err := socketcan.Open(*iface)
	if err != nil {
		return err
	}
	defer conn.Close()
	client := mecom.NewCANopenClient(conn, mecom.ClientConfig{Address: node, Timeout: *timeout})
	data, err := client.ReadSDORaw(context.Background(), obj.index, obj.subIndex)
	if err != nil {
		return err
	}
	if err := validateValueLength(*kind, data); err != nil {
		return fmt.Errorf("node=0x%02X object=0x%04X:%02X type=%s: %w", node, obj.index, obj.subIndex, *kind, err)
	}
	fmt.Printf("node=0x%02X object=0x%04X:%02X type=%s value=%s raw=% X\n", node, obj.index, obj.subIndex, *kind, formatValue(*kind, data), data)
	return nil
}

func runSDOWrite(args []string) error {
	fs := flag.NewFlagSet("sdo-write", flag.ContinueOnError)
	iface := fs.String("if", "can0", "SocketCAN interface")
	nodeArg := fs.String("node", "", "CANopen node ID, e.g. 0x51")
	objectArg := fs.String("object", "", "object index:subindex, e.g. 0x3300:1")
	kind := fs.String("type", "uint32", "value type: byte|uint32|int32|float32|uint16")
	valueArg := fs.String("value", "", "value to write")
	timeout := fs.Duration("timeout", 500*time.Millisecond, "SDO response timeout")
	commit := fs.Bool("commit", false, "transmit the write; omitted means dry-run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	node, err := parseNode(*nodeArg)
	if err != nil {
		return err
	}
	obj, err := parseObjectSpec(*objectArg)
	if err != nil {
		return err
	}
	value, err := encodeValue(*kind, *valueArg)
	if err != nil {
		return err
	}
	fmt.Printf("sdo-write node=0x%02X object=0x%04X:%02X type=%s value=%s raw=% X\n", node, obj.index, obj.subIndex, *kind, formatValue(*kind, value), value)
	if !*commit {
		fmt.Println("dry-run: add -commit to transmit")
		return nil
	}
	conn, err := socketcan.Open(*iface)
	if err != nil {
		return err
	}
	defer conn.Close()
	client := mecom.NewCANopenClient(conn, mecom.ClientConfig{Address: node, Timeout: *timeout})
	if err := client.WriteSDORaw(context.Background(), obj.index, obj.subIndex, value); err != nil {
		return err
	}
	fmt.Println("ack=ok")
	return nil
}

func runNMT(args []string) error {
	fs := flag.NewFlagSet("nmt", flag.ContinueOnError)
	iface := fs.String("if", "can0", "SocketCAN interface")
	nodeArg := fs.String("node", "", "CANopen node ID; 0 broadcasts")
	state := fs.String("state", "preop", "state: preop|start|stop|reset-node|reset-comm")
	commit := fs.Bool("commit", false, "transmit the NMT frame; omitted means dry-run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	node64, err := parseUint(*nodeArg, 7, "node")
	if err != nil {
		return err
	}
	cmd, err := parseNMTCommand(*state)
	if err != nil {
		return err
	}
	frame, err := canopen.NMTFrame(cmd, byte(node64))
	if err != nil {
		return err
	}
	fmt.Printf("nmt node=0x%02X state=%s frame=%03X#%02X%02X\n", byte(node64), *state, frame.ID, frame.Data[0], frame.Data[1])
	if !*commit {
		fmt.Println("dry-run: add -commit to transmit")
		return nil
	}
	conn, err := socketcan.Open(*iface)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Send(frame); err != nil {
		return err
	}
	fmt.Println("sent=ok")
	return nil
}

func runPDOApply(args []string) error {
	fs := flag.NewFlagSet("pdo-apply", flag.ContinueOnError)
	iface := fs.String("if", "can0", "SocketCAN interface")
	nodeArg := fs.String("node", "", "CANopen node ID, e.g. 0x51")
	dir := fs.String("dir", "rpdo", "PDO direction: rpdo|tpdo")
	pdoNumber := fs.Int("pdo", 1, "1-based PDO number")
	cobArg := fs.String("cob-id", "", "11-bit COB-ID, e.g. 0x1B8")
	mapArg := fs.String("map", "", "mapping list index:subindex:bits,... or none")
	transmissionArg := fs.String("transmission", "0xFE", "CANopen transmission type byte")
	eventTimerArg := fs.Int("event-ms", -1, "TPDO event timer in milliseconds; -1 leaves it unchanged")
	disabled := fs.Bool("disabled", false, "leave the PDO invalid after applying the mapping")
	allowSub0Mapping := fs.Bool("allow-sub0-map", false, "allow PDO mapping entries with subindex 0")
	timeout := fs.Duration("timeout", 500*time.Millisecond, "SDO response timeout")
	commit := fs.Bool("commit", false, "transmit the plan; omitted means dry-run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	node, err := parseNode(*nodeArg)
	if err != nil {
		return err
	}
	cob, err := parseUint(*cobArg, 11, "cob-id")
	if err != nil {
		return err
	}
	transmission, err := parseUint(*transmissionArg, 8, "transmission")
	if err != nil {
		return err
	}
	if *eventTimerArg < -1 || *eventTimerArg > 0xffff {
		return fmt.Errorf("event-ms must be -1..65535")
	}
	mapping, err := parseMappingList(*mapArg)
	if err != nil {
		return err
	}
	plan, err := buildPDOPlan(pdoPlanInput{
		dir:              *dir,
		nodeID:           node,
		pdoNumber:        *pdoNumber,
		cobID:            uint32(cob),
		transmissionType: byte(transmission),
		setEventTimer:    *eventTimerArg >= 0,
		eventTimerMS:     uint16(*eventTimerArg),
		leaveDisabled:    *disabled,
		allowSub0Mapping: *allowSub0Mapping,
		mapping:          mapping,
	})
	if err != nil {
		return err
	}
	for _, step := range plan.steps {
		fmt.Println(step.String())
	}
	if !*commit {
		fmt.Println("dry-run: add -commit to transmit")
		return nil
	}
	conn, err := socketcan.Open(*iface)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := executePDOPlan(conn, *timeout, plan); err != nil {
		return err
	}
	fmt.Println("apply=ok")
	return nil
}

func runPDOSend(args []string) error {
	fs := flag.NewFlagSet("pdo-send", flag.ContinueOnError)
	iface := fs.String("if", "can0", "SocketCAN interface")
	cobArg := fs.String("cob-id", "", "11-bit COB-ID to transmit")
	kind := fs.String("type", "float32", "value type: byte|uint32|int32|float32|uint16")
	valueArg := fs.String("value", "", "value to transmit")
	rawArg := fs.String("raw", "", "raw PDO payload bytes; overrides -type and -value")
	count := fs.Int("count", 1, "number of PDO frames to send")
	period := fs.Duration("period", 100*time.Millisecond, "delay between frames")
	commit := fs.Bool("commit", false, "transmit frames; omitted means dry-run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cob, err := parseUint(*cobArg, 11, "cob-id")
	if err != nil {
		return err
	}
	payloadKind := *kind
	payload, err := encodeValue(*kind, *valueArg)
	if strings.TrimSpace(*rawArg) != "" {
		payloadKind = "raw"
		payload, err = parseRawPayload(*rawArg)
	}
	if err != nil {
		return err
	}
	if *count < 1 {
		return fmt.Errorf("count must be >= 1")
	}
	if *period < 0 {
		return fmt.Errorf("period must be >= 0")
	}
	if *count > 1 && *period == 0 {
		return fmt.Errorf("period must be > 0 when count > 1")
	}
	frame := canopen.Frame{ID: uint32(cob), DLC: uint8(len(payload))}
	copy(frame.Data[:], payload)
	fmt.Printf("pdo-send cob-id=0x%03X type=%s value=%s count=%d period=%s raw=% X\n", uint32(cob), payloadKind, formatValue(payloadKind, payload), *count, *period, payload)
	if !*commit {
		fmt.Println("dry-run: add -commit to transmit")
		return nil
	}
	conn, err := socketcan.Open(*iface)
	if err != nil {
		return err
	}
	defer conn.Close()
	for i := 0; i < *count; i++ {
		if err := conn.Send(frame); err != nil {
			return err
		}
		if i+1 < *count {
			time.Sleep(*period)
		}
	}
	fmt.Println("sent=ok")
	return nil
}

func executePDOPlan(conn *socketcan.Conn, timeout time.Duration, plan pdoPlan) error {
	var client *mecom.CANopenClient
	for _, step := range plan.steps {
		switch step.kind {
		case "nmt-preop":
			frame, err := canopen.NMTFrame(canopen.NMTEnterPreOperational, step.nodeID)
			if err != nil {
				return err
			}
			if err := conn.Send(frame); err != nil {
				return err
			}
		case "nmt-start":
			frame, err := canopen.NMTFrame(canopen.NMTStartRemoteNode, step.nodeID)
			if err != nil {
				return err
			}
			if err := conn.Send(frame); err != nil {
				return err
			}
		case "sdo-write":
			if client == nil {
				client = mecom.NewCANopenClient(conn, mecom.ClientConfig{Address: step.nodeID, Timeout: timeout})
			}
			if err := client.WriteSDORaw(context.Background(), step.index, step.subIndex, step.value); err != nil {
				return fmt.Errorf("%s: %w", step.String(), err)
			}
		default:
			return fmt.Errorf("unknown plan step %q", step.kind)
		}
	}
	return nil
}

func buildPDOPlan(in pdoPlanInput) (pdoPlan, error) {
	const maxCANopenPDORecords = 512

	if in.nodeID == 0 || in.nodeID > 0x7f {
		return pdoPlan{}, fmt.Errorf("node id 0x%02X outside 1..127", in.nodeID)
	}
	if in.pdoNumber < 1 || in.pdoNumber > maxCANopenPDORecords {
		return pdoPlan{}, fmt.Errorf("pdo number must be 1..%d", maxCANopenPDORecords)
	}
	if in.cobID == 0 || in.cobID > 0x7ff {
		return pdoPlan{}, fmt.Errorf("cob-id 0x%X outside 1..0x7ff", in.cobID)
	}
	if len(in.mapping) > 8 {
		return pdoPlan{}, fmt.Errorf("mapping has %d entries, max 8", len(in.mapping))
	}
	totalBits := 0
	for i, entry := range in.mapping {
		if entry.SubIndex == 0 && !in.allowSub0Mapping {
			return pdoPlan{}, fmt.Errorf("mapping entry %d uses subindex 0; pass -allow-sub0-map only for a confirmed scalar object", i+1)
		}
		if entry.Bits == 0 {
			return pdoPlan{}, fmt.Errorf("mapping entry %d has zero bits", i+1)
		}
		totalBits += int(entry.Bits)
	}
	if totalBits > 64 {
		return pdoPlan{}, fmt.Errorf("mapping is %d bits, max 64", totalBits)
	}
	commBase, mapBase, err := pdoBases(in.dir)
	if err != nil {
		return pdoPlan{}, err
	}
	if in.setEventTimer && commBase != 0x1800 {
		return pdoPlan{}, fmt.Errorf("event-ms is only valid for TPDO communication objects")
	}
	commIndex := commBase + uint16(in.pdoNumber-1)
	mapIndex := mapBase + uint16(in.pdoNumber-1)
	disabledCOBID := in.cobID | (uint32(1) << 31)
	finalCOBID := in.cobID
	if in.leaveDisabled || len(in.mapping) == 0 {
		finalCOBID = disabledCOBID
	}
	steps := []planStep{
		{kind: "nmt-preop", nodeID: in.nodeID},
		sdoStep(in.nodeID, commIndex, 1, "uint32", u32(disabledCOBID)),
		sdoStep(in.nodeID, mapIndex, 0, "byte", []byte{0}),
	}
	for i, entry := range in.mapping {
		steps = append(steps, sdoStep(in.nodeID, mapIndex, byte(i+1), "uint32", u32(entry.Raw())))
	}
	steps = append(steps,
		sdoStep(in.nodeID, commIndex, 2, "byte", []byte{in.transmissionType}),
	)
	if in.setEventTimer {
		steps = append(steps, sdoStep(in.nodeID, commIndex, 5, "uint16", u16(in.eventTimerMS)))
	}
	steps = append(steps,
		sdoStep(in.nodeID, mapIndex, 0, "byte", []byte{byte(len(in.mapping))}),
		sdoStep(in.nodeID, commIndex, 1, "uint32", u32(finalCOBID)),
		planStep{kind: "nmt-start", nodeID: in.nodeID},
	)
	return pdoPlan{steps: steps}, nil
}

func sdoStep(node byte, index uint16, sub byte, kind string, value []byte) planStep {
	return planStep{kind: "sdo-write", nodeID: node, index: index, subIndex: sub, valueKind: kind, value: value}
}

func pdoBases(dir string) (uint16, uint16, error) {
	switch strings.ToLower(strings.TrimSpace(dir)) {
	case "rpdo":
		return 0x1400, 0x1600, nil
	case "tpdo":
		return 0x1800, 0x1A00, nil
	default:
		return 0, 0, fmt.Errorf("dir must be rpdo or tpdo")
	}
}

func parseObjectSpec(s string) (objectSpec, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return objectSpec{}, fmt.Errorf("object must be index:subindex, got %q", s)
	}
	index, err := parseUint(parts[0], 16, "index")
	if err != nil {
		return objectSpec{}, err
	}
	sub, err := parseUint(parts[1], 8, "subindex")
	if err != nil {
		return objectSpec{}, err
	}
	return objectSpec{index: uint16(index), subIndex: byte(sub)}, nil
}

func parseMappingList(s string) ([]canmap.MapEntry, error) {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "none") {
		return nil, nil
	}
	if s == "" {
		return nil, fmt.Errorf("map is required; use -map none to clear the mapping and leave the PDO disabled")
	}
	parts := strings.Split(s, ",")
	out := make([]canmap.MapEntry, 0, len(parts))
	for _, part := range parts {
		fields := strings.Split(strings.TrimSpace(part), ":")
		if len(fields) != 3 {
			return nil, fmt.Errorf("mapping entry must be index:subindex:bits, got %q", part)
		}
		index, err := parseUint(fields[0], 16, "mapping index")
		if err != nil {
			return nil, err
		}
		sub, err := parseUint(fields[1], 8, "mapping subindex")
		if err != nil {
			return nil, err
		}
		bits, err := parseUint(fields[2], 8, "mapping bits")
		if err != nil {
			return nil, err
		}
		out = append(out, canmap.MapEntry{Index: canmap.HexUint16(uint16(index)), SubIndex: byte(sub), Bits: byte(bits)})
	}
	return out, nil
}

func encodeValue(kind, value string) ([]byte, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	value = strings.TrimSpace(value)
	switch kind {
	case "byte":
		v, err := parseUint(value, 8, "byte value")
		if err != nil {
			return nil, err
		}
		return []byte{byte(v)}, nil
	case "uint32":
		v, err := parseUint(value, 32, "uint32 value")
		if err != nil {
			return nil, err
		}
		return u32(uint32(v)), nil
	case "uint16":
		v, err := parseUint(value, 16, "uint16 value")
		if err != nil {
			return nil, err
		}
		return u16(uint16(v)), nil
	case "int32":
		v, err := strconv.ParseInt(value, 0, 32)
		if err != nil {
			return nil, fmt.Errorf("parse int32 value %q: %w", value, err)
		}
		return u32(uint32(int32(v))), nil
	case "float32":
		v, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return nil, fmt.Errorf("parse float32 value %q: %w", value, err)
		}
		return u32(math.Float32bits(float32(v))), nil
	default:
		return nil, fmt.Errorf("unsupported type %q", kind)
	}
}

func parseRawPayload(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("raw payload is required")
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', ',', ':', '-', '_':
			return true
		default:
			return false
		}
	})
	if len(fields) == 1 {
		token := strings.TrimPrefix(strings.TrimPrefix(fields[0], "0x"), "0X")
		if len(token) > 2 {
			if len(token)%2 != 0 {
				return nil, fmt.Errorf("raw payload hex string must have an even number of digits")
			}
			payload, err := hex.DecodeString(token)
			if err != nil {
				return nil, fmt.Errorf("parse raw payload %q: %w", s, err)
			}
			if len(payload) > 8 {
				return nil, fmt.Errorf("raw payload is %d bytes, max 8", len(payload))
			}
			return payload, nil
		}
	}
	payload := make([]byte, 0, len(fields))
	for _, field := range fields {
		token := strings.TrimPrefix(strings.TrimPrefix(field, "0x"), "0X")
		v, err := strconv.ParseUint(token, 16, 8)
		if err != nil {
			return nil, fmt.Errorf("parse raw payload byte %q: %w", field, err)
		}
		payload = append(payload, byte(v))
	}
	if len(payload) > 8 {
		return nil, fmt.Errorf("raw payload is %d bytes, max 8", len(payload))
	}
	return payload, nil
}

func formatValue(kind string, value []byte) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "byte":
		if len(value) < 1 {
			return "<short>"
		}
		return fmt.Sprintf("%d", value[0])
	case "uint32":
		if len(value) < 4 {
			return "<short>"
		}
		return fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[:4]))
	case "uint16":
		if len(value) < 2 {
			return "<short>"
		}
		return fmt.Sprintf("0x%04X", binary.LittleEndian.Uint16(value[:2]))
	case "int32":
		if len(value) < 4 {
			return "<short>"
		}
		return fmt.Sprintf("%d", int32(binary.LittleEndian.Uint32(value[:4])))
	case "float32":
		if len(value) < 4 {
			return "<short>"
		}
		return fmt.Sprintf("%g", math.Float32frombits(binary.LittleEndian.Uint32(value[:4])))
	default:
		return fmt.Sprintf("% X", value)
	}
}

func validateValueLength(kind string, value []byte) error {
	want, ok := valueLength(kind)
	if !ok {
		return fmt.Errorf("unsupported type %q", strings.TrimSpace(kind))
	}
	if len(value) != want {
		return fmt.Errorf("SDO payload has %d bytes, want %d for %s", len(value), want, strings.ToLower(strings.TrimSpace(kind)))
	}
	return nil
}

func valueLength(kind string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "byte":
		return 1, true
	case "uint16":
		return 2, true
	case "uint32", "int32", "float32":
		return 4, true
	default:
		return 0, false
	}
}

func parseNode(s string) (byte, error) {
	v, err := parseUint(s, 7, "node")
	if err != nil {
		return 0, err
	}
	if v == 0 {
		return 0, fmt.Errorf("node 0 is reserved for broadcast NMT; use 1..127")
	}
	return byte(v), err
}

func parseUint(s string, bits int, name string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	v, err := strconv.ParseUint(s, 0, bits)
	if err != nil {
		return 0, fmt.Errorf("parse %s %q: %w", name, s, err)
	}
	return v, nil
}

func parseNMTCommand(state string) (canopen.NMTCommand, error) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "preop", "pre-operational":
		return canopen.NMTEnterPreOperational, nil
	case "start", "operational":
		return canopen.NMTStartRemoteNode, nil
	case "stop", "stopped":
		return canopen.NMTStopRemoteNode, nil
	case "reset-node":
		return canopen.NMTResetNode, nil
	case "reset-comm", "reset-communication":
		return canopen.NMTResetCommunication, nil
	default:
		return 0, fmt.Errorf("unknown NMT state %q", state)
	}
}

func u32(v uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, v)
	return out
}

func u16(v uint16) []byte {
	out := make([]byte, 2)
	binary.LittleEndian.PutUint16(out, v)
	return out
}
