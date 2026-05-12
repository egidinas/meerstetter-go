package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/loom-gossamer-shared/go/transport"
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
		paramsPath    = flag.String("params", "/home/svc_pmg_testbed_b/loom/docs/meerstetter/code_reference/pyMeCom_parameters.go", "Go parameter registry to scan")
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
	params, err := loadParameters(*paramsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *limitFlag > 0 && *limitFlag < len(params) {
		params = params[:*limitFlag]
	}

	results := scan(context.Background(), targets, *addressFlag, instances, params, *modeFlag, *chunkFlag, *timeoutFlag)
	if err := writeResults(*outPath, results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printSummary(results)
}

func scan(ctx context.Context, targets []string, address int, instances []int, params []parameterDef, mode string, chunkSize int, timeout time.Duration) []result {
	if chunkSize <= 0 {
		chunkSize = 8
	}
	var results []result
	for _, target := range targets {
		conn, err := dial(ctx, target, timeout)
		if err != nil {
			for _, param := range params {
				results = append(results, result{Target: target, Address: address, Instance: instances[0], Param: param, Error: err.Error()})
			}
			continue
		}
		client := mecom.NewClient(conn, mecom.ClientConfig{Address: byte(address), Timeout: timeout})
		for _, instance := range instances {
			if strings.EqualFold(mode, "single") {
				for _, param := range params {
					results = append(results, readOne(ctx, client, target, address, instance, param))
				}
			} else {
				for start := 0; start < len(params); start += chunkSize {
					end := start + chunkSize
					if end > len(params) {
						end = len(params)
					}
					results = append(results, readChunk(ctx, client, target, address, instance, params[start:end])...)
				}
			}
		}
		_ = conn.Close()
	}
	return results
}

func readChunk(ctx context.Context, client *mecom.Client, target string, address, instance int, params []parameterDef) []result {
	out := make([]result, 0, len(params))
	req := make([]mecom.Parameter, 0, len(params))
	for _, param := range params {
		req = append(req, mecom.Parameter{ID: param.ID, Instance: instance, Type: dataTypeForFormat(param.Format)})
		out = append(out, result{Target: target, Address: address, Instance: instance, Param: param})
	}
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
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
	ep, ok := transport.ParseEndpoint(target)
	if !ok {
		return nil, fmt.Errorf("invalid target %q", target)
	}
	return transport.Dial(ctx, ep, timeout)
}

func dataTypeForFormat(format string) mecom.DataType {
	if strings.EqualFold(format, "INT32") {
		return mecom.DataTypeInt32
	}
	return mecom.DataTypeFloat32
}

func formatValue(value float64, format string) string {
	if strings.EqualFold(format, "INT32") {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func readOne(ctx context.Context, client *mecom.Client, target string, address, instance int, param parameterDef) result {
	res := result{Target: target, Address: address, Instance: instance, Param: param}
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	switch strings.ToUpper(param.Format) {
	case "INT32":
		v, err := client.ReadInt32(reqCtx, param.ID, instance)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Value = strconv.FormatInt(int64(v), 10)
	default:
		v, err := client.ReadFloat32(reqCtx, param.ID, instance)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Value = formatValue(v, param.Format)
	}
	return res
}

func loadParameters(path string) ([]parameterDef, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`\{ID:\s*([0-9]+),\s*Name:\s*"([^"]+)",\s*Format:\s*"([^"]+)"\}`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	byID := make(map[int]parameterDef)
	for _, match := range matches {
		id, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, err
		}
		if _, ok := byID[id]; ok {
			continue
		}
		byID[id] = parameterDef{ID: id, Name: match[2], Format: match[3]}
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
	if path != "" {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	cw := csv.NewWriter(w)
	defer cw.Flush()
	if err := cw.Write([]string{"target", "address", "instance", "parameter_id", "name", "format", "value", "error"}); err != nil {
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
			return err
		}
	}
	return cw.Error()
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
