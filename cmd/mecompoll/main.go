// mecompoll is the unified continuous poller for Meerstetter TEC controllers.
// It auto-selects the correct concrete client (ASCII MeCom over serial/TCP or
// CANopen SDO over SocketCAN) based on each target's endpoint scheme.
//
// Each target is a TARGET=ADDRESS pair separated by '='. The address is the
// MeCom device address (1..254) for serial/TCP endpoints and the CANopen node
// ID (1..127) for can: endpoints.
//
// Example: poll two CAN devices and two devices behind a TCP device server:
//
//	mecompoll -targets \
//	  "can:can0/0x4b=75,can:can0/0x4c=76,tcp:127.0.0.1:50000=81,tcp:127.0.0.1:50000=84"
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
	"sync"
	"syscall"
	"time"

	"github.com/egidinas/meerstetter-go/canopen"
	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/socketcan"
)

// polledParams matches the 5 SDO-mapped parameters that both ASCII and
// CANopen transports can read. The intersection keeps the table consistent
// across transports.
var polledParams = []mecom.Parameter{
	{ID: 1000, Instance: 1, Name: "object_ch1", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 1000, Instance: 2, Name: "object_ch2", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 1001, Instance: 1, Name: "sink_ch1", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 1001, Instance: 2, Name: "sink_ch2", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 3000, Instance: 1, Name: "target_ch1", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 3000, Instance: 2, Name: "target_ch2", Unit: "°C", Type: mecom.DataTypeFloat32},
	{ID: 1020, Instance: 1, Name: "current_ch1", Unit: "A", Type: mecom.DataTypeFloat32},
	{ID: 1020, Instance: 2, Name: "current_ch2", Unit: "A", Type: mecom.DataTypeFloat32},
	{ID: 1021, Instance: 1, Name: "voltage_ch1", Unit: "V", Type: mecom.DataTypeFloat32},
	{ID: 1021, Instance: 2, Name: "voltage_ch2", Unit: "V", Type: mecom.DataTypeFloat32},
}

type deviceTarget struct {
	raw     string
	endpoint mecom.Endpoint
	address byte
}

type cycleResult struct {
	target deviceTarget
	values []float64
	err    error
	at     time.Time
}

func main() {
	targetsArg := flag.String("targets", "", "comma-separated TARGET=ADDRESS pairs")
	interval := flag.Duration("interval", 2*time.Second, "polling interval")
	timeout := flag.Duration("timeout", 800*time.Millisecond, "per-request timeout")
	once := flag.Bool("once", false, "poll once and exit (useful for scripts)")
	flag.Parse()

	devices, err := parseTargets(*targetsArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Printf("mecompoll targets=%d interval=%s once=%v\n", len(devices), *interval, *once)
	for _, d := range devices {
		fmt.Printf("  %s addr=0x%02X (%s)\n", d.raw, d.address, d.endpoint.Network)
	}

	if *once {
		results := pollAll(ctx, devices, *timeout)
		printTable(results)
		if anyErrors(results) {
			os.Exit(1)
		}
		return
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		results := pollAll(ctx, devices, *timeout)
		printTable(results)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func pollAll(ctx context.Context, devices []deviceTarget, timeout time.Duration) []cycleResult {
	results := make([]cycleResult, len(devices))
	// Each device gets its own goroutine. Distinct endpoints are independent;
	// shared endpoints (multiple addresses behind one TCP server) serialize
	// inside that server's broker, so concurrent client goroutines are safe.
	var wg sync.WaitGroup
	for i := range devices {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			results[i] = pollOne(ctx, devices[i], timeout)
		}()
	}
	wg.Wait()
	return results
}

func pollOne(ctx context.Context, dt deviceTarget, timeout time.Duration) cycleResult {
	res := cycleResult{target: dt, at: time.Now()}
	cfg := mecom.ClientConfig{Address: dt.address, Timeout: timeout}

	cycleCtx, cancel := context.WithTimeout(ctx, timeout*time.Duration(len(polledParams)+2))
	defer cancel()

	client, err := mecom.NewForEndpoint(cycleCtx, dt.endpoint, cfg, socketCANDialer)
	if err != nil {
		res.err = err
		return res
	}
	defer client.Close()

	values, err := client.ReadBulk(cycleCtx, polledParams)
	res.values = values
	res.err = err
	return res
}

func printTable(results []cycleResult) {
	fmt.Printf("\n%-34s  %-6s  %-10s  %-10s  %-10s  %-10s  %-10s  %-10s  %-10s  %-10s  %s\n",
		"target", "addr",
		"obj_ch1", "obj_ch2",
		"sink_ch1", "sink_ch2",
		"tgt_ch1", "tgt_ch2",
		"I_ch1(A)", "I_ch2(A)",
		"at")
	for _, r := range results {
		addrStr := fmt.Sprintf("0x%02X", r.target.address)
		if r.err != nil {
			fmt.Printf("%-34s  %-6s  error: %v\n", r.target.raw, addrStr, r.err)
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
		fmt.Printf("%-34s  %-6s  %-10s  %-10s  %-10s  %-10s  %-10s  %-10s  %-10s  %-10s  %s\n",
			r.target.raw, addrStr,
			fmtF(0), fmtF(1),
			fmtF(2), fmtF(3),
			fmtF(4), fmtF(5),
			fmtF(6), fmtF(7),
			r.at.Format("15:04:05.000"))
	}
}

func anyErrors(results []cycleResult) bool {
	for _, r := range results {
		if r.err != nil {
			return true
		}
	}
	return false
}

func parseTargets(v string) ([]deviceTarget, error) {
	var devices []deviceTarget
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.LastIndex(part, "=")
		if eq < 0 {
			return nil, fmt.Errorf("target %q missing =ADDRESS suffix", part)
		}
		targetStr := strings.TrimSpace(part[:eq])
		addrStr := strings.TrimSpace(part[eq+1:])
		n, err := strconv.ParseUint(addrStr, 0, 8)
		if err != nil || n == 0 || n > 254 {
			return nil, fmt.Errorf("invalid address %q in %q", addrStr, part)
		}
		ep, ok := mecom.ParseEndpoint(targetStr)
		if !ok {
			return nil, fmt.Errorf("invalid endpoint %q", targetStr)
		}
		devices = append(devices, deviceTarget{raw: targetStr, endpoint: ep, address: byte(n)})
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no targets specified; use -targets TARGET=ADDRESS,...")
	}
	return devices, nil
}

func socketCANDialer(ctx context.Context, iface string) (mecom.CANTransceiver, func() error, error) {
	conn, err := socketcan.Open(iface)
	if err != nil {
		return nil, nil, err
	}
	return socketCANTransceiver{conn: conn}, conn.Close, nil
}

type socketCANTransceiver struct {
	conn *socketcan.Conn
}

func (t socketCANTransceiver) Send(f canopen.Frame) error {
	return t.conn.Send(f)
}

func (t socketCANTransceiver) Recv(timeout time.Duration) (canopen.Frame, error) {
	return t.conn.Recv(timeout)
}
