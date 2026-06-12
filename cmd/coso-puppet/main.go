package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

var (
	traceLineRE = regexp.MustCompile(`^\[([^\]]+)\]\s+-\s+(\S+)\s+-\s+(.+?)\s+-\s+(.*)$`)
	meParIDRE   = regexp.MustCompile(`\bMeParID[: ]+\s*(\d+)(?:\.(\d+))?`)
	idRE        = regexp.MustCompile(`\bID:\s*(\d+)`)
	instRE      = regexp.MustCompile(`\bInst:\s*(\d+)`)
	detailRE    = regexp.MustCompile(`\bDetail:?\s*(.*)$`)
)

type traceEvent struct {
	Timestamp   string
	Level       string
	Source      string
	Message     string
	ParameterID int
	Instance    int
	Detail      string
}

type rawResponse struct {
	Raw      string
	Address  byte
	Sequence uint16
	Status   string
	Payload  string
}

type metadataInfo struct {
	Type         string
	Flags        byte
	MaxInstances byte
	MaxElements  uint32
	Minimum      string
	Maximum      string
	Actual       string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "trace":
		return runTrace(args[1:], stdout, stderr)
	case "metadata":
		return runMetadata(args[1:], stdout, stderr)
	case "raw":
		return runRaw(args[1:], stdout, stderr)
	case "oracle":
		return runOracle(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "coso-puppet: unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  coso-puppet trace -file Trace.txt [-all] [-events]")
	fmt.Fprintln(w, "  coso-puppet metadata -endpoint host:port -address N -id N [-instance N]")
	fmt.Fprintln(w, "  coso-puppet raw -endpoint host:port -address N -payload '?VM041501'")
	fmt.Fprintln(w, "  coso-puppet oracle -file coso_tec_v631_oracle.json [-requests]")
}

func runTrace(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trace", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("file", "", "CoSo Trace.txt path")
	all := fs.Bool("all", false, "include non-error and non-warning trace lines")
	eventsFlag := fs.Bool("events", false, "print individual matching events before the summary")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" {
		fmt.Fprintln(stderr, "coso-puppet trace: -file is required")
		return 2
	}
	events, err := readTraceEvents(*path)
	if err != nil {
		fmt.Fprintf(stderr, "coso-puppet trace: %v\n", err)
		return 1
	}
	events = filterTraceEvents(events, !*all)
	writeTraceReport(stdout, events, *eventsFlag)
	return 0
}

func runMetadata(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("metadata", flag.ContinueOnError)
	fs.SetOutput(stderr)
	endpoint := fs.String("endpoint", "", "TCP endpoint, for example 127.0.0.1:50000")
	address := fs.Int("address", 0, "MeCom address in range 0..255")
	seq := fs.Int("seq", 1, "MeCom sequence in range 0..65535")
	id := fs.Int("id", 0, "parameter ID in range 1..65535")
	instance := fs.Int("instance", 1, "parameter instance in range 1..255")
	timeout := fs.Duration("timeout", 800*time.Millisecond, "TCP dial and response timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id <= 0 || *id > 0xFFFF {
		fmt.Fprintln(stderr, "coso-puppet metadata: -id must be in range 1..65535")
		return 2
	}
	if *instance <= 0 || *instance > 0xFF {
		fmt.Fprintln(stderr, "coso-puppet metadata: -instance must be in range 1..255")
		return 2
	}
	payload := fmt.Sprintf("?VM%04X%02X", *id, *instance)
	return sendAndReport(*endpoint, *address, *seq, payload, *timeout, stdout, stderr)
}

func runRaw(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("raw", flag.ContinueOnError)
	fs.SetOutput(stderr)
	endpoint := fs.String("endpoint", "", "TCP endpoint, for example 127.0.0.1:50000")
	address := fs.Int("address", 0, "MeCom address in range 0..255")
	seq := fs.Int("seq", 1, "MeCom sequence in range 0..65535")
	payload := fs.String("payload", "", "raw MeCom payload, for example ?VM041501")
	timeout := fs.Duration("timeout", 800*time.Millisecond, "TCP dial and response timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *payload == "" {
		fmt.Fprintln(stderr, "coso-puppet raw: -payload is required")
		return 2
	}
	return sendAndReport(*endpoint, *address, *seq, *payload, *timeout, stdout, stderr)
}

func sendAndReport(endpoint string, address, seq int, payload string, timeout time.Duration, stdout, stderr io.Writer) int {
	if endpoint == "" {
		fmt.Fprintln(stderr, "coso-puppet: -endpoint is required")
		return 2
	}
	addr, err := byteFlag(address, "address")
	if err != nil {
		fmt.Fprintf(stderr, "coso-puppet: %v\n", err)
		return 2
	}
	sequence, err := uint16Flag(seq, "seq")
	if err != nil {
		fmt.Fprintf(stderr, "coso-puppet: %v\n", err)
		return 2
	}
	if timeout <= 0 {
		fmt.Fprintln(stderr, "coso-puppet: -timeout must be positive")
		return 2
	}
	request, resp, err := sendRawRequest(endpoint, addr, sequence, payload, timeout)
	if err != nil {
		fmt.Fprintf(stderr, "coso-puppet: %v\n", err)
		return 1
	}
	printRawReport(stdout, request, resp)
	return 0
}

func readTraceEvents(path string) ([]traceEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var events []traceEvent
	for scanner.Scan() {
		if event, ok := parseTraceLine(scanner.Text()); ok {
			events = append(events, event)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func parseTraceLine(line string) (traceEvent, bool) {
	m := traceLineRE.FindStringSubmatch(line)
	if m == nil {
		return traceEvent{}, false
	}
	event := traceEvent{
		Timestamp: strings.TrimSpace(m[1]),
		Level:     strings.TrimSpace(m[2]),
		Source:    strings.TrimSpace(m[3]),
		Message:   strings.TrimSpace(m[4]),
	}
	if idMatch := meParIDRE.FindStringSubmatch(event.Message); idMatch != nil {
		event.ParameterID = atoi(idMatch[1])
		if len(idMatch) > 2 && idMatch[2] != "" {
			event.Instance = atoi(idMatch[2])
		}
	} else if idMatch := idRE.FindStringSubmatch(event.Message); idMatch != nil {
		event.ParameterID = atoi(idMatch[1])
	}
	if event.Instance == 0 {
		if instMatch := instRE.FindStringSubmatch(event.Message); instMatch != nil {
			event.Instance = atoi(instMatch[1])
		}
	}
	if detailMatch := detailRE.FindStringSubmatch(event.Message); detailMatch != nil {
		event.Detail = strings.TrimSpace(detailMatch[1])
	}
	return event, true
}

func filterTraceEvents(events []traceEvent, errorsOnly bool) []traceEvent {
	if !errorsOnly {
		return events
	}
	out := events[:0]
	for _, event := range events {
		level := strings.ToUpper(event.Level)
		if level == "ERROR" || level == "WARN" {
			out = append(out, event)
		}
	}
	return out
}

func writeTraceReport(w io.Writer, events []traceEvent, printEvents bool) {
	fmt.Fprintf(w, "matching_events=%d\n", len(events))
	if printEvents {
		fmt.Fprintln(w, "timestamp\tlevel\tsource\tparam\tdetail")
		for _, event := range events {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", event.Timestamp, event.Level, event.Source, formatParam(event), event.Detail)
		}
	}
	fmt.Fprintln(w, "summary")
	type summary struct {
		event traceEvent
		count int
		first string
		last  string
	}
	summaries := map[string]*summary{}
	var order []string
	for _, event := range events {
		key := fmt.Sprintf("%s|%s|%d|%d|%s", event.Level, event.Source, event.ParameterID, event.Instance, event.Detail)
		if summaries[key] == nil {
			summaries[key] = &summary{event: event, first: event.Timestamp}
			order = append(order, key)
		}
		s := summaries[key]
		s.count++
		s.last = event.Timestamp
	}
	for _, key := range order {
		s := summaries[key]
		fmt.Fprintf(w, "%dx\t%s\t%s\t%s\tfirst=%s\tlast=%s\tdetail=%s\n", s.count, s.event.Level, s.event.Source, formatParam(s.event), s.first, s.last, s.event.Detail)
	}
}

func formatParam(event traceEvent) string {
	if event.ParameterID == 0 {
		return "-"
	}
	if event.Instance == 0 {
		return strconv.Itoa(event.ParameterID)
	}
	return fmt.Sprintf("%d.%d", event.ParameterID, event.Instance)
}

func buildMeComFrame(address byte, seq uint16, payload string) []byte {
	prefix := fmt.Sprintf("#%02X%04X%s", address, seq, payload)
	return []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator))
}

func sendRawRequest(endpoint string, address byte, seq uint16, payload string, timeout time.Duration) ([]byte, rawResponse, error) {
	request := buildMeComFrame(address, seq, payload)
	conn, err := net.DialTimeout("tcp", normalizeTCPEndpoint(endpoint), timeout)
	if err != nil {
		return request, rawResponse{}, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return request, rawResponse{}, err
	}
	if _, err := conn.Write(request); err != nil {
		return request, rawResponse{}, err
	}
	raw, err := bufio.NewReader(conn).ReadBytes(mecom.FrameTerminator)
	if err != nil {
		return request, rawResponse{}, err
	}
	resp, err := parseMeComResponse(raw, request)
	if err != nil {
		return request, rawResponse{}, err
	}
	return request, resp, nil
}

func normalizeTCPEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	endpoint = strings.TrimPrefix(endpoint, "tcp://")
	endpoint = strings.TrimPrefix(endpoint, "tcp:")
	return endpoint
}

func parseMeComResponse(raw, request []byte) (rawResponse, error) {
	frame := bytes.TrimRight(raw, "\r\n")
	if len(frame) < 11 || frame[0] != '!' {
		return rawResponse{}, fmt.Errorf("invalid MeCom response %q", string(frame))
	}
	payloadEnd := len(frame) - 4
	got, err := strconv.ParseUint(string(frame[payloadEnd:]), 16, 16)
	if err != nil {
		return rawResponse{}, fmt.Errorf("invalid response CRC %q: %w", string(frame[payloadEnd:]), err)
	}
	if want := mecom.CRC16(frame[:payloadEnd]); uint16(got) != want {
		if isBareRequestCRCAck(frame, request) {
			addr, seq, err := parseResponseAddressSequence(frame)
			if err != nil {
				return rawResponse{}, err
			}
			return rawResponse{
				Raw:      string(raw),
				Address:  addr,
				Sequence: seq,
				Status:   "ok",
			}, nil
		}
		return rawResponse{}, fmt.Errorf("response CRC mismatch got %04X want %04X", got, want)
	}
	addr, seq, err := parseResponseAddressSequence(frame)
	if err != nil {
		return rawResponse{}, err
	}
	if len(request) > 0 {
		reqAddr, reqSeq, err := parseRequestAddressSequence(request)
		if err != nil {
			return rawResponse{}, err
		}
		if addr != reqAddr || seq != reqSeq {
			return rawResponse{}, fmt.Errorf(
				"response address/sequence mismatch got 0x%02X/%d want 0x%02X/%d",
				addr, seq, reqAddr, reqSeq,
			)
		}
	}
	status := "data"
	payloadStart := 7
	if payloadStart < payloadEnd {
		switch frame[payloadStart] {
		case '+':
			status = "ok"
			payloadStart++
		case '-':
			status = "nack"
			payloadStart++
		}
	}
	if payloadStart > payloadEnd {
		return rawResponse{}, fmt.Errorf("invalid response payload bounds")
	}
	return rawResponse{
		Raw:      string(raw),
		Address:  addr,
		Sequence: seq,
		Status:   status,
		Payload:  string(frame[payloadStart:payloadEnd]),
	}, nil
}

func parseResponseAddressSequence(frame []byte) (byte, uint16, error) {
	addr, err := strconv.ParseUint(string(frame[1:3]), 16, 8)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid response address: %w", err)
	}
	seq, err := strconv.ParseUint(string(frame[3:7]), 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid response sequence: %w", err)
	}
	return byte(addr), uint16(seq), nil
}

func parseRequestAddressSequence(frame []byte) (byte, uint16, error) {
	frame = bytes.TrimRight(frame, "\r\n")
	if len(frame) < 11 || frame[0] != '#' {
		return 0, 0, fmt.Errorf("invalid MeCom request %q", string(frame))
	}
	addr, err := strconv.ParseUint(string(frame[1:3]), 16, 8)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid request address: %w", err)
	}
	seq, err := strconv.ParseUint(string(frame[3:7]), 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid request sequence: %w", err)
	}
	return byte(addr), uint16(seq), nil
}

func isBareRequestCRCAck(frame, request []byte) bool {
	req := bytes.TrimSuffix(request, []byte{mecom.FrameTerminator})
	if len(frame) != 11 || len(req) < 11 || req[0] != '#' {
		return false
	}
	if !bytes.Equal(frame[1:7], req[1:7]) {
		return false
	}
	reqPayloadEnd := len(req) - 4
	reqCRC, err := strconv.ParseUint(string(req[reqPayloadEnd:]), 16, 16)
	if err != nil || mecom.CRC16(req[:reqPayloadEnd]) != uint16(reqCRC) {
		return false
	}
	respCRC, err := strconv.ParseUint(string(frame[7:]), 16, 16)
	return err == nil && uint16(respCRC) == uint16(reqCRC)
}

func printRawReport(w io.Writer, request []byte, resp rawResponse) {
	fmt.Fprintf(w, "request=%q\n", strings.TrimRight(string(request), "\r\n"))
	fmt.Fprintf(w, "response=%q\n", strings.TrimRight(resp.Raw, "\r\n"))
	fmt.Fprintf(w, "address=%d sequence=%d status=%s payload=%s\n", resp.Address, resp.Sequence, resp.Status, resp.Payload)
	if meta, ok := decodeMetadataPayload(resp.Payload); ok {
		fmt.Fprintf(w, "metadata_type=%s flags=0x%02X max_instances=%d max_elements=%d min=%s max=%s actual=%s\n",
			meta.Type, meta.Flags, meta.MaxInstances, meta.MaxElements, meta.Minimum, meta.Maximum, meta.Actual)
	}
}

func decodeMetadataPayload(payload string) (metadataInfo, bool) {
	if len(payload) < 14 {
		return metadataInfo{}, false
	}
	meParType, ok := parseHexByte(payload[0:2])
	if !ok {
		return metadataInfo{}, false
	}
	flags, ok := parseHexByte(payload[2:4])
	if !ok {
		return metadataInfo{}, false
	}
	maxInstances, ok := parseHexByte(payload[4:6])
	if !ok {
		return metadataInfo{}, false
	}
	maxElements, err := strconv.ParseUint(payload[6:14], 16, 32)
	if err != nil {
		return metadataInfo{}, false
	}
	meta := metadataInfo{
		Type:         metadataTypeName(meParType),
		Flags:        flags,
		MaxInstances: maxInstances,
		MaxElements:  uint32(maxElements),
	}
	rest := payload[14:]
	if len(rest) >= 24 {
		meta.Minimum = rest[0:8]
		meta.Maximum = rest[8:16]
		meta.Actual = rest[16:24]
	} else if len(rest) >= 6 {
		meta.Minimum = rest[0:2]
		meta.Maximum = rest[2:4]
		meta.Actual = rest[4:6]
	}
	return meta, true
}

func metadataTypeName(v byte) string {
	switch v {
	case 0:
		return "FLOAT32"
	case 1:
		return "INT32"
	case 3:
		return "LATIN1"
	default:
		return fmt.Sprintf("TYPE_%02X", v)
	}
}

func parseHexByte(v string) (byte, bool) {
	out, err := strconv.ParseUint(v, 16, 8)
	if err != nil {
		return 0, false
	}
	return byte(out), true
}

func byteFlag(v int, name string) (byte, error) {
	if v < 0 || v > 0xFF {
		return 0, fmt.Errorf("-%s must be in range 0..255", name)
	}
	return byte(v), nil
}

func uint16Flag(v int, name string) (uint16, error) {
	if v < 0 || v > 0xFFFF {
		return 0, fmt.Errorf("-%s must be in range 0..65535", name)
	}
	return uint16(v), nil
}

func atoi(v string) int {
	out, _ := strconv.Atoi(v)
	return out
}
