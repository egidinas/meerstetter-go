// mecomtcppoll continuously polls Meerstetter TEC controllers via the MeCom
// ASCII protocol over TCP (or direct serial). Designed for use with a socat
// TCP-to-serial bridge as a backup transport when CAN is unavailable.
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

// polledParams are the MeCom parameter IDs to read on each cycle.
// These match the CANopen SDO mapping in mecomcanpoll for easy comparison.
var polledParams = []mecom.Parameter{
	{ID: 1000, Instance: 1, Name: "object_temp_ch1", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 1000, Instance: 2, Name: "object_temp_ch2", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 1001, Instance: 1, Name: "sink_temp_ch1", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 1001, Instance: 2, Name: "sink_temp_ch2", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 3000, Instance: 1, Name: "target_ch1", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 3000, Instance: 2, Name: "target_ch2", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 1020, Instance: 1, Name: "current_ch1", Unit: "A", Type: mecom.DataTypeFloat32},
	{ID: 1020, Instance: 2, Name: "current_ch2", Unit: "A", Type: mecom.DataTypeFloat32},
	{ID: 1021, Instance: 1, Name: "voltage_ch1", Unit: "V", Type: mecom.DataTypeFloat32},
	{ID: 1021, Instance: 2, Name: "voltage_ch2", Unit: "V", Type: mecom.DataTypeFloat32},
}

type deviceTarget struct {
	target  string
	address byte
}

type pollResult struct {
	dt     deviceTarget
	values []float64
	err    error
	at     time.Time
}

func main() {
	targetsArg := flag.String("targets", "", "comma-separated target=address pairs, e.g. tcp:127.0.0.1:50002=75,tcp:127.0.0.1:50003=76")
	interval := flag.Duration("interval", 2*time.Second, "polling interval")
	timeout := flag.Duration("timeout", 800*time.Millisecond, "per-request timeout")
	flag.Parse()

	devices, err := parseTargets(*targetsArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Printf("mecomtcppoll targets=%d interval=%s\n", len(devices), *interval)
	for _, d := range devices {
		fmt.Printf("  %s addr=0x%02X\n", d.target, d.address)
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		results := pollAll(ctx, devices, *timeout)
		printResults(results)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func pollAll(ctx context.Context, devices []deviceTarget, timeout time.Duration) []pollResult {
	results := make([]pollResult, len(devices))
	// Serial protocol is not safe for concurrent access on the same link;
	// poll sequentially. Each target is its own TCP connection so concurrent
	// calls are fine across different targets.
	for i, dt := range devices {
		results[i] = pollOne(ctx, dt, timeout)
	}
	return results
}

func pollOne(ctx context.Context, dt deviceTarget, timeout time.Duration) pollResult {
	res := pollResult{dt: dt, at: time.Now()}

	ep, ok := mecom.ParseEndpoint(dt.target)
	if !ok {
		res.err = fmt.Errorf("invalid target %q", dt.target)
		return res
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout*time.Duration(len(polledParams)+2))
	defer cancel()

	conn, err := mecom.Dial(reqCtx, ep, timeout)
	if err != nil {
		res.err = err
		return res
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: dt.address, Timeout: timeout})
	values, err := client.ReadBulk(reqCtx, polledParams)
	res.values = values
	res.err = err
	return res
}

func printResults(results []pollResult) {
	fmt.Printf("\n%-30s  %-6s  %-12s  %-12s  %-10s  %-10s  %-10s  %-10s  %-10s  %-10s  %s\n",
		"target", "addr",
		"obj_ch1(°C)", "obj_ch2(°C)",
		"sink_ch1", "sink_ch2",
		"tgt_ch1", "tgt_ch2",
		"curr_ch1(A)", "curr_ch2(A)",
		"at")
	for _, r := range results {
		addrStr := fmt.Sprintf("0x%02X", r.dt.address)
		if r.err != nil {
			fmt.Printf("%-30s  %-6s  error: %v\n", r.dt.target, addrStr, r.err)
			continue
		}
		fmtF := func(i int) string {
			if i >= len(r.values) {
				return "?"
			}
			v := r.values[i]
			if math.IsNaN(v) {
				return "NaN"
			}
			return fmt.Sprintf("%.3f", v)
		}
		fmt.Printf("%-30s  %-6s  %-12s  %-12s  %-10s  %-10s  %-10s  %-10s  %-10s  %-10s  %s\n",
			r.dt.target, addrStr,
			fmtF(0), fmtF(1),
			fmtF(2), fmtF(3),
			fmtF(4), fmtF(5),
			fmtF(6), fmtF(7),
			r.at.Format("15:04:05.000"))
	}
}

func parseTargets(v string) ([]deviceTarget, error) {
	var devices []deviceTarget
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Each entry is target=address, e.g. tcp:127.0.0.1:50002=75
		// The target itself may contain colons, so split on the last '='.
		eq := strings.LastIndex(part, "=")
		if eq < 0 {
			return nil, fmt.Errorf("target %q missing =address suffix", part)
		}
		target := strings.TrimSpace(part[:eq])
		addrStr := strings.TrimSpace(part[eq+1:])
		n, err := strconv.ParseUint(addrStr, 0, 8)
		if err != nil || n == 0 || n > 254 {
			return nil, fmt.Errorf("invalid address %q in %q", addrStr, part)
		}
		devices = append(devices, deviceTarget{target: target, address: byte(n)})
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no targets specified; use -targets tcp:127.0.0.1:PORT=ADDRESS,...")
	}
	return devices, nil
}
