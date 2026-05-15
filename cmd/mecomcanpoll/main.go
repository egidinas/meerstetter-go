// mecomcanpoll continuously polls Meerstetter TEC controllers via CANopen SDO
// and prints a live table of the mapped parameters. One SocketCAN socket is
// opened per device so concurrent SDO exchanges do not cross-contaminate.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/socketcan"
)

// canParams are the MeCom parameter IDs that have a CANopen SDO mapping.
var canParams = []mecom.Parameter{
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

type deviceResult struct {
	node   byte
	values []float64
	err    error
	at     time.Time
}

func main() {
	iface := flag.String("if", "can0", "SocketCAN interface")
	nodesArg := flag.String("nodes", "75,76,81,84", "comma-separated CANopen node IDs")
	interval := flag.Duration("interval", 2*time.Second, "polling interval")
	timeout := flag.Duration("timeout", 800*time.Millisecond, "per-request SDO timeout")
	flag.Parse()

	nodes, err := parseNodes(*nodesArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Printf("mecomcanpoll if=%s nodes=%s interval=%s\n", *iface, *nodesArg, *interval)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		results := pollAll(ctx, *iface, nodes, *timeout)
		printResults(results)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func pollAll(ctx context.Context, iface string, nodes []byte, timeout time.Duration) []deviceResult {
	results := make([]deviceResult, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Add(1)
		go func(i int, node byte) {
			defer wg.Done()
			results[i] = pollDevice(ctx, iface, node, timeout)
		}(i, node)
	}
	wg.Wait()
	return results
}

func pollDevice(ctx context.Context, iface string, node byte, timeout time.Duration) deviceResult {
	res := deviceResult{node: node, at: time.Now()}
	conn, err := socketcan.Open(iface)
	if err != nil {
		res.err = err
		return res
	}
	defer conn.Close()

	client := mecom.NewCANopenClient(conn, mecom.ClientConfig{Address: node, Timeout: timeout})
	values, err := client.ReadBulk(ctx, canParams)
	res.values = values
	res.err = err
	return res
}

func printResults(results []deviceResult) {
	fmt.Printf("\n%-6s  %-22s  %-22s  %-10s  %-10s  %-10s  %-10s  %-10s  %-10s  %s\n",
		"node",
		"object_ch1(°C)", "object_ch2(°C)",
		"sink_ch1", "sink_ch2",
		"target_ch1", "target_ch2",
		"curr_ch1(A)", "curr_ch2(A)",
		"at")
	for _, r := range results {
		if r.err != nil {
			fmt.Printf("0x%02X    error: %v\n", r.node, r.err)
			continue
		}
		fmtF := func(i int) string {
			if i >= len(r.values) {
				return "?"
			}
			v := r.values[i]
			if v != v { // NaN
				return "NaN"
			}
			return fmt.Sprintf("%.3f", v)
		}
		fmt.Printf("0x%02X    %-22s  %-22s  %-10s  %-10s  %-10s  %-10s  %-10s  %-10s  %s\n",
			r.node,
			fmtF(0), fmtF(1),
			fmtF(2), fmtF(3),
			fmtF(4), fmtF(5),
			fmtF(6), fmtF(7),
			r.at.Format("15:04:05.000"))
	}
}

func parseNodes(v string) ([]byte, error) {
	var nodes []byte
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.ParseUint(part, 0, 8)
		if err != nil || n == 0 || n > 127 {
			return nil, fmt.Errorf("invalid node id %q", part)
		}
		nodes = append(nodes, byte(n))
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes specified")
	}
	return nodes, nil
}
