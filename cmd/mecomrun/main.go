// mecomrun executes a sequencer.Script JSON file against one MeCom device.
// It is the reference end-to-end loop: script → tmtc.Commander → mecom client →
// concrete transport (serial, TCP, or CAN through a CANDialer).
//
// Example: a setpoint-and-verify script for a single device
//
//	{
//	  "id": "warm-up-tec",
//	  "steps": [
//	    {"id":"set", "kind":"send_command", "command_name":"set_float32",
//	     "arguments":{"param":3000,"instance":1,"value":25.0}},
//	    {"id":"settle", "kind":"wait", "duration":"10s"},
//	    {"id":"enable", "kind":"send_command", "command_name":"set_int32",
//	     "arguments":{"param":2010,"instance":1,"value":1}}
//	  ]
//	}
//
// The exit code is 0 when every step reports OK, 1 on any step failure, 2 on
// configuration or transport errors.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/sequencer"
)

func main() {
	target := flag.String("target", "", "MeCom endpoint (serial:/dev/ttyUSB0@57600 | tcp:host:port | can:can0/0x4b)")
	addr := flag.Int("address", 0, "MeCom device address for ASCII; 0 means broadcast; must be omitted for can:")
	scriptPath := flag.String("script", "", "path to a sequencer.Script JSON file (- for stdin)")
	timeout := flag.Duration("timeout", 2*time.Second, "per-command timeout")
	dryRun := flag.Bool("dry-run", false, "parse and print the script, do not execute")
	flag.Parse()

	if *target == "" {
		fmt.Fprintln(os.Stderr, "mecomrun: -target is required")
		os.Exit(2)
	}
	if *scriptPath == "" {
		fmt.Fprintln(os.Stderr, "mecomrun: -script is required")
		os.Exit(2)
	}

	script, err := loadScript(*scriptPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mecomrun: load script: %v\n", err)
		os.Exit(2)
	}

	if *dryRun {
		out, _ := json.MarshalIndent(script, "", "  ")
		fmt.Println(string(out))
		return
	}

	ep, ok := mecom.ParseEndpoint(*target)
	if !ok {
		fmt.Fprintf(os.Stderr, "mecomrun: invalid -target %q\n", *target)
		os.Exit(2)
	}
	address, err := validateRunAddress(ep, *addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mecomrun: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	client, err := mecom.NewForEndpoint(ctx, ep, mecom.ClientConfig{
		Address: address,
		Timeout: *timeout,
	}, socketCANDialer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mecomrun: open %s: %v\n", *target, err)
		os.Exit(2)
	}
	defer client.Close()

	writer, ok := client.(mecom.WriteClient)
	if !ok {
		fmt.Fprintf(os.Stderr, "mecomrun: transport %q does not support writes\n", ep.Network)
		os.Exit(2)
	}

	commander := mecom.NewCommander(writer, *timeout)

	fmt.Printf("mecomrun script=%q steps=%d target=%s\n", script.ID, len(script.Steps), *target)
	result, err := sequencer.Run(ctx, script, commander)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mecomrun: %v\n", err)
		os.Exit(1)
	}
	for _, sr := range result.Steps {
		mark := "ok"
		extra := ""
		if !sr.OK {
			mark = "FAIL"
			extra = "  " + sr.Error
		}
		fmt.Printf("  %-6s %s%s\n", mark, sr.StepID, extra)
	}
	if !result.OK {
		os.Exit(1)
	}
}

func validateRunAddress(ep mecom.Endpoint, address int) (byte, error) {
	if ep.Network == "can" {
		if address != 0 {
			return 0, fmt.Errorf("-address is not used with can: targets; encode the node in -target and leave -address at 0")
		}
		return 0, nil
	}
	if address < 0 || address > 255 {
		return 0, fmt.Errorf("-address must be in range 0..255")
	}
	return byte(address), nil
}

func loadScript(path string) (sequencer.Script, error) {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = readAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return sequencer.Script{}, err
	}
	var script sequencer.Script
	if err := json.Unmarshal(raw, &script); err != nil {
		return sequencer.Script{}, fmt.Errorf("parse JSON: %w", err)
	}
	if script.ID == "" {
		return sequencer.Script{}, fmt.Errorf("script.id is required")
	}
	return script, nil
}

func readAll(f *os.File) ([]byte, error) {
	const max = 1 << 20
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for len(buf) < max {
		n, err := f.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				return buf, nil
			}
			return buf, err
		}
	}
	return buf, fmt.Errorf("script larger than %d bytes", max)
}
