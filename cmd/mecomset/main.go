package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

type setSpec struct {
	ParamID  int
	Instance int
	Value    int32
}

type setFlags []string

func (s *setFlags) String() string {
	return strings.Join(*s, ",")
}

func (s *setFlags) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	var (
		targetsFlag = flag.String("targets", "", "comma-separated targets, for example serial:/dev/ttyUSB0@57600,tcp:127.0.0.1:50000")
		addressFlag = flag.Int("address", 0, "MeCom device address; 0 is broadcast and works for one device per link")
		timeoutFlag = flag.Duration("timeout", 800*time.Millisecond, "per-request timeout")
		verifyFlag  = flag.Bool("verify", false, "read each INT32 parameter back after writing")
		saveFlag    = flag.Bool("save", false, "send explicit SP save-to-flash command after writes")
		resetFlag   = flag.Bool("reset", false, "send explicit RS reset command after writes/save")
		sets        setFlags
	)
	flag.Var(&sets, "set", "parameter write as param[:instance]=int32; may be repeated or comma-separated")
	flag.Parse()

	targets := splitList(*targetsFlag)
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "mecomset: -targets is required")
		os.Exit(2)
	}
	if *addressFlag < 0 || *addressFlag > 255 {
		fmt.Fprintln(os.Stderr, "mecomset: -address must be in range 0..255")
		os.Exit(2)
	}
	specs, err := parseSetSpecs(sets)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := run(context.Background(), targets, *addressFlag, specs, *timeoutFlag, *verifyFlag, *saveFlag, *resetFlag); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, targets []string, address int, specs []setSpec, timeout time.Duration, verify, save, reset bool) error {
	var failures int
	for _, target := range targets {
		conn, err := dial(ctx, target, timeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s connect_error=%q\n", target, err)
			failures++
			continue
		}
		client := mecom.NewClient(conn, mecom.ClientConfig{Address: byte(address), Timeout: timeout})
		for _, spec := range specs {
			reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := client.WriteInt32(reqCtx, spec.ParamID, spec.Instance, spec.Value)
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s param=%d instance=%d write_error=%q\n", target, spec.ParamID, spec.Instance, err)
				failures++
				continue
			}
			if !verify {
				fmt.Printf("%s param=%d instance=%d wrote=%d\n", target, spec.ParamID, spec.Instance, spec.Value)
				continue
			}
			reqCtx, cancel = context.WithTimeout(ctx, 2*time.Second)
			got, err := client.ReadInt32(reqCtx, spec.ParamID, spec.Instance)
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s param=%d instance=%d verify_error=%q\n", target, spec.ParamID, spec.Instance, err)
				failures++
				continue
			}
			fmt.Printf("%s param=%d instance=%d wrote=%d verify=%d\n", target, spec.ParamID, spec.Instance, spec.Value, got)
		}
		if save {
			reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := client.SaveToFlash(reqCtx)
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s save_error=%q\n", target, err)
				failures++
			} else {
				fmt.Printf("%s save=ack\n", target)
			}
		}
		if reset {
			reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := client.Reset(reqCtx)
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s reset_error=%q\n", target, err)
				failures++
			} else {
				fmt.Printf("%s reset=ack\n", target)
			}
		}
		_ = conn.Close()
	}
	if failures > 0 {
		return fmt.Errorf("mecomset: %d operation(s) failed", failures)
	}
	return nil
}

func dial(ctx context.Context, target string, timeout time.Duration) (net.Conn, error) {
	ep, ok := mecom.ParseEndpoint(target)
	if !ok {
		return nil, fmt.Errorf("invalid target %q", target)
	}
	return mecom.Dial(ctx, ep, timeout)
}

func parseSetSpecs(values []string) ([]setSpec, error) {
	var specs []setSpec
	for _, value := range values {
		for _, part := range splitList(value) {
			left, right, ok := strings.Cut(part, "=")
			if !ok || strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
				return nil, fmt.Errorf("mecomset: invalid -set %q, expected param[:instance]=value", part)
			}
			paramPart, instancePart, hasInstance := strings.Cut(strings.TrimSpace(left), ":")
			paramID, err := strconv.Atoi(paramPart)
			if err != nil || paramID <= 0 || paramID > 65535 {
				return nil, fmt.Errorf("mecomset: invalid parameter id %q", paramPart)
			}
			instance := 1
			if hasInstance {
				instance, err = strconv.Atoi(instancePart)
				if err != nil || instance < 0 || instance > 255 {
					return nil, fmt.Errorf("mecomset: invalid instance %q", instancePart)
				}
			}
			value64, err := strconv.ParseInt(strings.TrimSpace(right), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("mecomset: invalid INT32 value %q", right)
			}
			specs = append(specs, setSpec{ParamID: paramID, Instance: instance, Value: int32(value64)})
		}
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("mecomset: at least one -set is required")
	}
	return specs, nil
}

func splitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
