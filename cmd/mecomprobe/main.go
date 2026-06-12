package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/mecomdict"
)

type parameterDef struct {
	ID     int
	Name   string
	Format string
}

type result struct {
	Target   string
	Address  int
	Instance int
	Param    parameterDef
	Value    string
	Error    string
}

func main() {
	var (
		targetsFlag   = flag.String("targets", "", "comma-separated targets, for example serial:/dev/ttyUSB0@57600,tcp:127.0.0.1:50000")
		addressFlag   = flag.Int("address", 0, "MeCom device address; 0 is broadcast and works for one device per link")
		instancesFlag = flag.String("instances", "1", "comma-separated parameter instances")
		paramsPath    = flag.String("params", mecomdict.DefaultParameterRegistryPath(), "Go parameter registry to scan; defaults to MECOM_PARAMETER_REGISTRY")
		presetFlag    = flag.String("preset", "", "optional built-in read-only parameter preset, for example rmm-1182-hr1-pt100")
		limitFlag     = flag.Int("limit", 0, "maximum number of unique parameters to read; 0 reads all parsed parameters")
		modeFlag      = flag.String("mode", "bulk", "read mode: bulk uses ?VX round-robin chunks; single keeps legacy ?VR reads")
		chunkFlag     = flag.Int("chunk", 8, "maximum parameters per ?VX bulk chunk")
		timeoutFlag   = flag.Duration("timeout", 800*time.Millisecond, "per-request timeout")
		outPath       = flag.String("out", "", "optional CSV output path")
	)
	flag.Parse()

	targets := splitList(*targetsFlag)
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "mecomprobe: -targets is required")
		os.Exit(2)
	}
	instances, err := parseInstances(*instancesFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	mode := strings.ToLower(strings.TrimSpace(*modeFlag))
	if err := validateProbeOptions(*addressFlag, mode, *chunkFlag, *timeoutFlag); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	params, err := loadProbeParameters(*paramsPath, *presetFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *limitFlag > 0 && *limitFlag < len(params) {
		params = params[:*limitFlag]
	}

	results := scan(context.Background(), targets, *addressFlag, instances, params, mode, *chunkFlag, *timeoutFlag)
	if err := writeResults(*outPath, results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printSummary(results)
}

func validateProbeOptions(address int, mode string, chunkSize int, timeout time.Duration) error {
	if address < 0 || address > 255 {
		return fmt.Errorf("mecomprobe: -address must be in range 0..255")
	}
	if mode != "bulk" && mode != "single" {
		return fmt.Errorf("mecomprobe: -mode must be bulk or single")
	}
	if chunkSize <= 0 || chunkSize > 255 {
		return fmt.Errorf("mecomprobe: -chunk must be in range 1..255")
	}
	if timeout <= 0 {
		return fmt.Errorf("mecomprobe: -timeout must be positive")
	}
	return nil
}

func scan(ctx context.Context, targets []string, address int, instances []int, params []parameterDef, mode string, chunkSize int, timeout time.Duration) []result {
	if chunkSize <= 0 {
		chunkSize = 8
	}
	var results []result
	for _, target := range targets {
		conn, err := dial(ctx, target, timeout)
		if err != nil {
			for _, instance := range instances {
				for _, param := range params {
					results = append(results, result{Target: target, Address: address, Instance: instance, Param: param, Error: err.Error()})
				}
			}
			continue
		}
		client := mecom.NewClient(conn, mecom.ClientConfig{Address: byte(address), Timeout: timeout})
		for _, instance := range instances {
			if strings.EqualFold(mode, "single") {
				for _, param := range params {
					results = append(results, readOne(ctx, client, target, address, instance, param, timeout))
				}
			} else {
				for start := 0; start < len(params); start += chunkSize {
					end := start + chunkSize
					if end > len(params) {
						end = len(params)
					}
					results = append(results, readChunk(ctx, client, target, address, instance, params[start:end], timeout)...)
				}
			}
		}
		_ = conn.Close()
	}
	return results
}

func readChunk(ctx context.Context, client *mecom.Client, target string, address, instance int, params []parameterDef, timeout time.Duration) []result {
	out := make([]result, 0, len(params))
	req := make([]mecom.Parameter, 0, len(params))
	hasUnsupported := false
	for _, param := range params {
		out = append(out, result{Target: target, Address: address, Instance: instance, Param: param})
		dataType, ok := dataTypeForFormat(param.Format)
		if !ok {
			out[len(out)-1].Error = fmt.Sprintf("unsupported registry format %q", param.Format)
			hasUnsupported = true
			continue
		}
		req = append(req, mecom.Parameter{ID: param.ID, Instance: instance, Type: dataType})
	}
	if hasUnsupported {
		for i := range out {
			if out[i].Error == "" {
				out[i].Error = "bulk read skipped because chunk contains unsupported registry format"
			}
		}
		return out
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	values, err := client.ReadBulk(reqCtx, req)
	if err != nil {
		for i := range out {
			out[i].Error = err.Error()
		}
		return out
	}
	for i := range out {
		if i >= len(values) || math.IsNaN(values[i]) {
			out[i].Error = "no value in bulk response"
			continue
		}
		out[i].Value = formatValue(values[i], params[i].Format)
	}
	return out
}

func dial(ctx context.Context, target string, timeout time.Duration) (net.Conn, error) {
	ep, ok := mecom.ParseEndpoint(target)
	if !ok {
		return nil, fmt.Errorf("invalid target %q", target)
	}
	return mecom.Dial(ctx, ep, timeout)
}

func dataTypeForFormat(format string) (mecom.DataType, bool) {
	switch strings.ToUpper(format) {
	case "INT32":
		return mecom.DataTypeInt32, true
	case "FLOAT32":
		return mecom.DataTypeFloat32, true
	default:
		return "", false
	}
}

func formatValue(value float64, format string) string {
	if strings.EqualFold(format, "INT32") {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func readOne(ctx context.Context, client *mecom.Client, target string, address, instance int, param parameterDef, timeout time.Duration) result {
	res := result{Target: target, Address: address, Instance: instance, Param: param}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	switch strings.ToUpper(param.Format) {
	case "INT32":
		v, err := client.ReadInt32(reqCtx, param.ID, instance)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Value = strconv.FormatInt(int64(v), 10)
	case "FLOAT32":
		v, err := client.ReadFloat32(reqCtx, param.ID, instance)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Value = formatValue(v, param.Format)
	default:
		res.Error = fmt.Sprintf("unsupported registry format %q", param.Format)
	}
	return res
}

func loadProbeParameters(path, preset string) ([]parameterDef, error) {
	if strings.TrimSpace(preset) != "" {
		return presetParameterDefs(preset)
	}
	return loadParameters(path)
}

func presetParameterDefs(name string) ([]parameterDef, error) {
	switch normalizePresetName(name) {
	case "rmm-1182-hr1-pt100", "rmm-hr1-pt100":
		return parameterDefsFromMeComParameters(mecom.DefaultRMM1182HR1Pt100Parameters())
	default:
		return nil, fmt.Errorf("mecomprobe: unknown preset %q", name)
	}
}

func normalizePresetName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, "_", "-")
	return name
}

func parameterDefsFromMeComParameters(params []mecom.Parameter) ([]parameterDef, error) {
	out := make([]parameterDef, 0, len(params))
	for _, param := range params {
		if param.Instance != 1 {
			return nil, fmt.Errorf("mecomprobe: preset parameter %d uses fixed instance %d; only instance 1 presets are supported", param.ID, param.Instance)
		}
		format, ok := formatForDataType(param.Type)
		if !ok {
			return nil, fmt.Errorf("mecomprobe: preset parameter %d uses unsupported data type %q", param.ID, param.Type)
		}
		out = append(out, parameterDef{ID: param.ID, Name: param.Name, Format: format})
	}
	return out, nil
}

func formatForDataType(dataType mecom.DataType) (string, bool) {
	switch dataType {
	case mecom.DataTypeInt32:
		return "INT32", true
	case mecom.DataTypeFloat32:
		return "FLOAT32", true
	default:
		return "", false
	}
}

func loadParameters(path string) ([]parameterDef, error) {
	defs, err := mecomdict.LoadParameterRegistry(path)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return nil, fmt.Errorf("mecomprobe: no parameters parsed from %s", path)
	}
	byID := make(map[int]parameterDef)
	for _, def := range defs {
		if _, ok := byID[def.ID]; ok {
			continue
		}
		byID[def.ID] = parameterDef{ID: def.ID, Name: def.Name, Format: def.Format}
	}
	params := make([]parameterDef, 0, len(byID))
	for _, param := range byID {
		params = append(params, param)
	}
	sort.Slice(params, func(i, j int) bool { return params[i].ID < params[j].ID })
	return params, nil
}

func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseInstances(v string) ([]int, error) {
	parts := splitList(v)
	if len(parts) == 0 {
		return nil, fmt.Errorf("mecomprobe: at least one instance is required")
	}
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		instance, err := strconv.Atoi(part)
		if err != nil || instance < 0 || instance > 255 {
			return nil, fmt.Errorf("mecomprobe: invalid instance %q", part)
		}
		out = append(out, instance)
	}
	return out, nil
}

func writeResults(path string, results []result) error {
	w := os.Stdout
	var file *os.File
	if path != "" {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		file = f
		w = f
	}
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"target", "address", "instance", "parameter_id", "name", "format", "value", "error"}); err != nil {
		if file != nil {
			_ = file.Close()
		}
		return err
	}
	for _, res := range results {
		if err := cw.Write([]string{
			res.Target,
			strconv.Itoa(res.Address),
			strconv.Itoa(res.Instance),
			strconv.Itoa(res.Param.ID),
			res.Param.Name,
			res.Param.Format,
			res.Value,
			res.Error,
		}); err != nil {
			if file != nil {
				_ = file.Close()
			}
			return err
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		if file != nil {
			_ = file.Close()
		}
		return err
	}
	if file != nil {
		return file.Close()
	}
	return nil
}

func printSummary(results []result) {
	byTarget := map[string][2]int{}
	for _, res := range results {
		counts := byTarget[res.Target]
		if res.Error == "" {
			counts[0]++
		} else {
			counts[1]++
		}
		byTarget[res.Target] = counts
	}
	for target, counts := range byTarget {
		fmt.Fprintf(os.Stderr, "%s ok=%d error=%d\n", target, counts[0], counts[1])
	}
}
