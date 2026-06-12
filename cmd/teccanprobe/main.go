//go:build linux

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/canadapter"
	"github.com/egidinas/meerstetter-go/canopen"
	"github.com/egidinas/meerstetter-go/canring"
	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/socketcan"
)

const defaultSDOReads = "0x1018:1:uint32:identity.vendor,0x2002:1:int32:serial,0x2003:1:int32:firmware,0x2004:1:int32:status,0x2005:1:int32:error,0x2009:1:int32:flash,0x2100:1:float32:object.ch1,0x2100:2:float32:object.ch2,0x2101:1:float32:sink.ch1,0x2101:2:float32:sink.ch2,0x2400:1:int32:ctrl-input.ch1,0x2400:2:int32:ctrl-input.ch2,0x2410:1:int32:output-enable.ch1,0x2410:2:int32:output-enable.ch2,0x2420:1:float32:static-current.ch1,0x2420:2:float32:static-current.ch2,0x2421:1:float32:static-voltage.ch1,0x2421:2:float32:static-voltage.ch2,0x2440:1:int32:operating-mode,0x2600:1:float32:nominal-target.ch1,0x2600:2:float32:nominal-target.ch2"

type result struct {
	frames       int
	heartbeats   map[byte]bool
	canopenSDO   map[byte][]sdoValue
	mecomReplies map[byte]float64
}

type sdoProbe struct {
	Index    uint16
	SubIndex byte
	Kind     string
	Label    string
}

type sdoValue struct {
	Probe sdoProbe
	Value string
	Frame canopen.Frame
}

func main() {
	iface := flag.String("if", "can0", "SocketCAN interface")
	profileID := flag.String("profile", canadapter.ProfilePiXtendV2L, "CAN adapter profile")
	listProfiles := flag.Bool("list-profiles", false, "print known CAN adapter profiles and exit")
	checklist := flag.Bool("checklist", false, "print selected adapter checklist and exit")
	listen := flag.Duration("listen", 2*time.Second, "passive listen window")
	timeout := flag.Duration("timeout", 180*time.Millisecond, "per-request response window")
	nodesArg := flag.String("nodes", "", "CANopen node IDs to probe, e.g. 1-16,0x23")
	sdoArg := flag.String("sdo", defaultSDOReads, "comma-separated SDO reads index:subindex:kind[:label], kind=uint32|int32|float32|byte; empty disables SDO reads")
	addrsArg := flag.String("mecom-addrs", "", "MeCom CAN addresses to probe, e.g. 1-16,0x23")
	paramID := flag.Int("param", 1000, "MeCom parameter ID for read-only value probe")
	paramKindArg := flag.String("param-kind", string(mecom.DataTypeFloat32), "MeCom parameter data type for read-only value probe, kind=float32|int32")
	instance := flag.Int("instance", 1, "MeCom parameter instance")
	active := flag.Bool("active", false, "send bounded read-only SDO/MeCom probes after passive listen")
	ringPath := flag.String("ring-path", "", "optional fixed-size CAN receive ring file on durable storage")
	ringSize := flag.String("ring-size", "512MiB", "CAN ring file size when -ring-path is set")
	ringChunk := flag.String("ring-chunk", "4MiB", "CAN ring chunk size; disk writes happen on chunk boundaries")
	ringSync := flag.Bool("ring-sync", true, "fsync completed CAN ring chunks and metadata")
	fallbackRingPath := flag.String("fallback-ring-path", "", "optional second fixed-size CAN receive ring used as a mirrored fallback")
	fallbackRingSize := flag.String("fallback-ring-size", "2GiB", "fallback CAN ring file size when -fallback-ring-path is set")
	fallbackRingChunk := flag.String("fallback-ring-chunk", "4MiB", "fallback CAN ring chunk size")
	fallbackRingSync := flag.Bool("fallback-ring-sync", true, "fsync completed fallback CAN ring chunks and metadata")
	flag.Parse()

	if *listProfiles {
		printProfiles()
		return
	}
	profile, ok := canadapter.LookupProfile(*profileID)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown -profile %q\n", *profileID)
		printProfiles()
		os.Exit(1)
	}
	if *checklist {
		fmt.Println(canadapter.RenderChecklist(profile))
		return
	}

	conn, err := socketcan.Open(*iface)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s: %v\n", *iface, err)
		os.Exit(1)
	}
	defer conn.Close()

	var sink *ringSink
	if *ringPath != "" {
		writer, err := openRingWriter("primary", *ringPath, *ringSize, *ringChunk, *iface, *ringSync)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open primary CAN ring: %v\n", err)
			os.Exit(1)
		}
		sink = &ringSink{writers: []ringWriterSink{writer}}
	}
	if *fallbackRingPath != "" {
		writer, err := openRingWriter("fallback", *fallbackRingPath, *fallbackRingSize, *fallbackRingChunk, *iface, *fallbackRingSync)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open fallback CAN ring: %v\n", err)
			os.Exit(1)
		}
		if sink == nil {
			sink = &ringSink{}
		}
		sink.writers = append(sink.writers, writer)
	}
	if sink != nil {
		defer sink.Close()
	}

	res := result{
		heartbeats:   make(map[byte]bool),
		canopenSDO:   make(map[byte][]sdoValue),
		mecomReplies: make(map[byte]float64),
	}

	fmt.Printf("probe iface=%s profile=%s transport=%s listen=%s active=%v\n", *iface, profile.ID, profile.Transport, *listen, *active)
	listenFor(conn, *listen, &res, 12, sink)

	nodes, err := parseIDs(*nodesArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse -nodes: %v\n", err)
		os.Exit(1)
	}
	addrs, err := parseIDs(*addrsArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse -mecom-addrs: %v\n", err)
		os.Exit(1)
	}
	sdoReads, err := parseSDOProbes(*sdoArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse -sdo: %v\n", err)
		os.Exit(1)
	}
	paramKind, err := parseMeComDataType(*paramKindArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse -param-kind: %v\n", err)
		os.Exit(1)
	}
	for node := range res.heartbeats {
		if !containsID(nodes, node) {
			nodes = append(nodes, node)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	sort.Slice(addrs, func(i, j int) bool { return addrs[i] < addrs[j] })

	if *active {
		for _, node := range nodes {
			for _, probe := range sdoReads {
				probeCANopenSDO(conn, node, probe, *timeout, &res, sink)
			}
		}
		var mecomSeq uint16
		for _, addr := range addrs {
			mecomSeq = nextMeComSequence(mecomSeq)
			probeMeComValue(conn, addr, mecomSeq, *paramID, *instance, paramKind, *timeout, &res, sink)
		}
	}

	printSummary(&res)
	if len(res.canopenSDO) > 0 || len(res.mecomReplies) > 0 || len(res.heartbeats) > 0 {
		return
	}
	os.Exit(2)
}

func printProfiles() {
	for _, profile := range canadapter.Profiles() {
		tier := string(profile.Maturity)
		if tier == "" {
			tier = "adapter"
		}
		if profile.FirstClass {
			tier = string(canadapter.MaturityFirstClass)
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", profile.ID, tier, profile.Transport, profile.DisplayName)
	}
}

type frameSink interface {
	Record(canopen.Frame)
}

type ringSink struct {
	writers []ringWriterSink
}

type ringWriterSink struct {
	role     string
	sync     bool
	writer   *canring.Writer
	disabled bool
}

func openRingWriter(role, path, sizeArg, chunkArg, iface string, sync bool) (ringWriterSink, error) {
	sizeBytes, err := parseByteSize(sizeArg)
	if err != nil {
		return ringWriterSink{}, fmt.Errorf("parse %s ring size: %w", role, err)
	}
	chunkBytes, err := parseByteSize(chunkArg)
	if err != nil {
		return ringWriterSink{}, fmt.Errorf("parse %s ring chunk: %w", role, err)
	}
	writer, err := canring.OpenWriter(canring.Config{
		Path:        path,
		SizeBytes:   sizeBytes,
		ChunkBytes:  chunkBytes,
		Interface:   iface,
		SyncOnChunk: sync,
	})
	if err != nil {
		return ringWriterSink{}, err
	}
	stats := writer.Stats()
	fmt.Printf("ring role=%s path=%s size=%d chunk=%d chunks=%d sync=%v\n", role, stats.Path, stats.SizeBytes, stats.ChunkBytes, stats.ChunkCount, sync)
	return ringWriterSink{role: role, sync: sync, writer: writer}, nil
}

func (s *ringSink) Record(f canopen.Frame) {
	if s == nil {
		return
	}
	now := time.Now()
	for i := range s.writers {
		w := &s.writers[i]
		if w.disabled || w.writer == nil {
			continue
		}
		if err := w.writer.Append(f, now); err != nil {
			stats := w.writer.Stats()
			fmt.Printf("ring-error role=%s path=%s err=%v; disabling this ring\n", w.role, stats.Path, err)
			w.disabled = true
			_ = w.writer.Close()
		}
	}
}

func (s *ringSink) Close() {
	if s == nil {
		return
	}
	for i := range s.writers {
		w := &s.writers[i]
		if w.disabled || w.writer == nil {
			continue
		}
		stats := w.writer.Stats()
		if err := w.writer.Close(); err != nil {
			fmt.Printf("ring-close-error role=%s path=%s err=%v\n", w.role, stats.Path, err)
		}
	}
}

func listenFor(conn *socketcan.Conn, window time.Duration, res *result, maxPrint int, sink frameSink) {
	deadline := time.Now().Add(window)
	printed := 0
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return
		}
		f, err := conn.Recv(left)
		if errors.Is(err, socketcan.ErrTimeout) {
			return
		}
		if err != nil {
			fmt.Printf("rx-error %v\n", err)
			continue
		}
		res.frames++
		if sink != nil {
			sink.Record(f)
		}
		observe(f, res)
		if printed < maxPrint {
			fmt.Printf("rx %s\n", formatFrame(f))
			printed++
		}
	}
}

func probeCANopenSDO(conn *socketcan.Conn, node byte, probe sdoProbe, timeout time.Duration, res *result, sink frameSink) {
	req, err := canopen.SDOUploadRequest(node, probe.Index, probe.SubIndex)
	if err != nil {
		fmt.Printf("tx-error sdo node=0x%02X 0x%04X:%02X %v\n", node, probe.Index, probe.SubIndex, err)
		return
	}
	fmt.Printf("tx sdo node=0x%02X %s 0x%04X:%02X %s\n", node, probe.Label, probe.Index, probe.SubIndex, formatFrame(req))
	if err := conn.Send(req); err != nil {
		fmt.Printf("tx-error sdo node=0x%02X 0x%04X:%02X %v\n", node, probe.Index, probe.SubIndex, err)
		return
	}
	deadline := time.Now().Add(timeout)
	for {
		f, ok := recvUntil(conn, deadline, res, sink)
		if !ok {
			fmt.Printf("no-reply sdo node=0x%02X 0x%04X:%02X\n", node, probe.Index, probe.SubIndex)
			return
		}
		if f.ID == 0x580+uint32(node) {
			resp, err := canopen.ParseSDOUploadResponse(f)
			if err != nil {
				if !sdoParseErrorMatchesProbe(err, probe) {
					continue
				}
				fmt.Printf("reply sdo node=0x%02X 0x%04X:%02X decode-error=%v %s\n", node, probe.Index, probe.SubIndex, err, formatFrame(f))
				return
			}
			if !sdoResponseMatchesProbe(resp, probe) {
				continue
			}
			value, err := formatSDOValue(resp, probe.Kind)
			if err != nil {
				fmt.Printf("reply sdo node=0x%02X 0x%04X:%02X value-error=%v %s\n", node, probe.Index, probe.SubIndex, err, formatFrame(f))
				return
			}
			res.canopenSDO[node] = append(res.canopenSDO[node], sdoValue{Probe: probe, Value: value, Frame: f})
			fmt.Printf("reply sdo node=0x%02X %s 0x%04X:%02X %s %s\n", node, probe.Label, probe.Index, probe.SubIndex, value, formatFrame(f))
			return
		}
	}
}

func probeMeComValue(conn *socketcan.Conn, addr byte, seq uint16, paramID, instance int, paramKind mecom.DataType, timeout time.Duration, res *result, sink frameSink) {
	payload := mecom.BuildBinarySingleGetFrame(int(addr), seq, paramID, instance)
	var data [8]byte
	copy(data[:], payload)
	req := canopen.Frame{ID: 0x300 + uint32(addr), DLC: uint8(len(payload)), Data: data}
	fmt.Printf("tx mecom addr=0x%02X param=%d/%d kind=%s %s\n", addr, paramID, instance, paramKind, formatFrame(req))
	if err := conn.Send(req); err != nil {
		fmt.Printf("tx-error mecom addr=0x%02X %v\n", addr, err)
		return
	}
	deadline := time.Now().Add(timeout)
	for {
		f, ok := recvUntil(conn, deadline, res, sink)
		if !ok {
			fmt.Printf("no-reply mecom addr=0x%02X\n", addr)
			return
		}
		if f.ID == 0x400+uint32(addr) && mecomResponseMatchesRequest(f, addr, seq, mecom.BinaryCmdQueryValue) {
			value, err := mecom.DecodeBinaryCANFrame(f, paramKind)
			if err != nil {
				fmt.Printf("reply mecom addr=0x%02X decode-error=%v %s\n", addr, err, formatFrame(f))
				return
			}
			res.mecomReplies[addr] = value
			fmt.Printf("reply mecom addr=0x%02X kind=%s value=%0.6g %s\n", addr, paramKind, value, formatFrame(f))
			return
		}
	}
}

func parseMeComDataType(kind string) (mecom.DataType, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case string(mecom.DataTypeFloat32):
		return mecom.DataTypeFloat32, nil
	case string(mecom.DataTypeInt32):
		return mecom.DataTypeInt32, nil
	default:
		return "", fmt.Errorf("unsupported kind %q (want float32 or int32)", kind)
	}
}

func sdoParseErrorMatchesProbe(err error, probe sdoProbe) bool {
	var abort canopen.SDOAbortError
	if errors.As(err, &abort) {
		return abort.Index == probe.Index && abort.SubIndex == probe.SubIndex
	}
	return true
}

func sdoResponseMatchesProbe(resp canopen.SDOUploadResponse, probe sdoProbe) bool {
	return resp.Index == probe.Index && resp.SubIndex == probe.SubIndex
}

func nextMeComSequence(seq uint16) uint16 {
	seq = (seq + 1) & 0x7f
	if seq == 0 {
		return 1
	}
	return seq
}

func mecomResponseMatchesRequest(f canopen.Frame, addr byte, seq, command uint16) bool {
	return mecom.BinaryResponseMatchesRequest(f, addr, seq, command)
}

func recvUntil(conn *socketcan.Conn, deadline time.Time, res *result, sink frameSink) (canopen.Frame, bool) {
	left := time.Until(deadline)
	if left <= 0 {
		return canopen.Frame{}, false
	}
	f, err := conn.Recv(left)
	if errors.Is(err, socketcan.ErrTimeout) {
		return canopen.Frame{}, false
	}
	if err != nil {
		fmt.Printf("rx-error %v\n", err)
		return canopen.Frame{}, false
	}
	res.frames++
	if sink != nil {
		sink.Record(f)
	}
	observe(f, res)
	fmt.Printf("rx %s\n", formatFrame(f))
	return f, true
}

func observe(f canopen.Frame, res *result) {
	if f.ID >= 0x700 && f.ID <= 0x77f {
		res.heartbeats[byte(f.ID-0x700)] = true
	}
}

func printSummary(res *result) {
	fmt.Printf("summary frames=%d heartbeats=%s canopen_sdo=%s mecom=%s\n",
		res.frames,
		formatIDs(keys(res.heartbeats)),
		formatIDs(keys(res.canopenSDO)),
		formatIDs(keys(res.mecomReplies)),
	)
	for addr, value := range res.mecomReplies {
		fmt.Printf("mecom-value addr=0x%02X value=%0.6g\n", addr, value)
	}
	for _, node := range keys(res.canopenSDO) {
		for _, value := range res.canopenSDO[node] {
			fmt.Printf("sdo-value node=0x%02X %s 0x%04X:%02X %s\n", node, value.Probe.Label, value.Probe.Index, value.Probe.SubIndex, value.Value)
		}
	}
}

func parseIDs(v string) ([]byte, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	var ids []byte
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, err := parseID(bounds[0])
			if err != nil {
				return nil, err
			}
			end, err := parseID(bounds[1])
			if err != nil {
				return nil, err
			}
			if end < start {
				return nil, fmt.Errorf("range %q has end before start", part)
			}
			for id := start; id <= end; id++ {
				ids = append(ids, id)
			}
			continue
		}
		id, err := parseID(part)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return dedupe(ids), nil
}

func parseID(v string) (byte, error) {
	v = strings.TrimSpace(v)
	n, err := strconv.ParseUint(v, 0, 8)
	if err != nil {
		return 0, err
	}
	if n == 0 || n > 127 {
		return 0, fmt.Errorf("id %q outside 1..127", v)
	}
	return byte(n), nil
}

func parseByteSize(v string) (int64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, errors.New("empty size")
	}
	lower := strings.ToLower(v)
	multiplier := int64(1)
	for _, suffix := range []struct {
		text string
		mul  int64
	}{
		{"gib", 1024 * 1024 * 1024},
		{"gb", 1000 * 1000 * 1000},
		{"g", 1024 * 1024 * 1024},
		{"mib", 1024 * 1024},
		{"mb", 1000 * 1000},
		{"m", 1024 * 1024},
		{"kib", 1024},
		{"kb", 1000},
		{"k", 1024},
		{"b", 1},
	} {
		if strings.HasSuffix(lower, suffix.text) {
			multiplier = suffix.mul
			v = strings.TrimSpace(v[:len(v)-len(suffix.text)])
			break
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, errors.New("size must be positive")
	}
	return n * multiplier, nil
}

func parseSDOProbes(v string) ([]sdoProbe, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	var probes []sdoProbe
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, ":")
		if len(fields) < 3 || len(fields) > 4 {
			return nil, fmt.Errorf("probe %q must be index:subindex:kind[:label]", part)
		}
		index64, err := strconv.ParseUint(fields[0], 0, 16)
		if err != nil {
			return nil, fmt.Errorf("probe %q index: %w", part, err)
		}
		sub64, err := strconv.ParseUint(fields[1], 0, 8)
		if err != nil {
			return nil, fmt.Errorf("probe %q subindex: %w", part, err)
		}
		kind := strings.ToLower(strings.TrimSpace(fields[2]))
		switch kind {
		case "uint32", "int32", "float32", "byte":
		default:
			return nil, fmt.Errorf("probe %q kind %q is not supported", part, kind)
		}
		label := fmt.Sprintf("0x%04X:%02X", index64, sub64)
		if len(fields) == 4 && strings.TrimSpace(fields[3]) != "" {
			label = strings.TrimSpace(fields[3])
		}
		probes = append(probes, sdoProbe{
			Index:    uint16(index64),
			SubIndex: byte(sub64),
			Kind:     kind,
			Label:    label,
		})
	}
	return probes, nil
}

func formatSDOValue(resp canopen.SDOUploadResponse, kind string) (string, error) {
	switch kind {
	case "uint32":
		v, err := resp.Uint32()
		return fmt.Sprintf("uint32=%d(0x%08X)", v, v), err
	case "int32":
		v, err := resp.Int32()
		return fmt.Sprintf("int32=%d", v), err
	case "float32":
		v, err := resp.Float32()
		return fmt.Sprintf("float32=%0.6g", v), err
	case "byte":
		v, err := resp.Byte()
		return fmt.Sprintf("byte=%d(0x%02X)", v, v), err
	default:
		return "", fmt.Errorf("unsupported SDO kind %q", kind)
	}
}

func dedupe(ids []byte) []byte {
	seen := make(map[byte]bool)
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func containsID(ids []byte, id byte) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func keys[T any](m map[byte]T) []byte {
	ids := make([]byte, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func formatIDs(ids []byte) string {
	if len(ids) == 0 {
		return "-"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("0x%02X", id)
	}
	return strings.Join(parts, ",")
}

func formatFrame(f canopen.Frame) string {
	parts := make([]string, f.DLC)
	for i := uint8(0); i < f.DLC; i++ {
		parts[i] = fmt.Sprintf("%02X", f.Data[i])
	}
	return fmt.Sprintf("%03X#%s", f.ID, strings.Join(parts, ""))
}
