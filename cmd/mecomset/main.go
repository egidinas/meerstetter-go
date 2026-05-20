package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

type setSpec struct {
	ParamID    int
	Instance   int
	Type       mecom.DataType
	IntValue   int32
	FloatValue float32
}

type setFlags []string

type setValueKind string

const (
	setKindAuto    setValueKind = ""
	setKindInt32   setValueKind = "int32"
	setKindFloat32 setValueKind = "float32"
)

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
		verifyFlag  = flag.Bool("verify", false, "read each parameter back after writing")
		saveFlag    = flag.Bool("save", false, "send explicit SP save-to-flash command after writes")
		resetFlag   = flag.Bool("reset", false, "send explicit RS reset command after writes/save")
		sets        setFlags
		intSets     setFlags
		floatSets   setFlags
	)
	flag.Var(&sets, "set", "parameter write as param[:instance]=value using the TEC catalogue type; may be repeated or comma-separated")
	flag.Var(&intSets, "set-int", "force INT32 parameter write as param[:instance]=value; may be repeated or comma-separated")
	flag.Var(&floatSets, "set-float", "force FLOAT32 parameter write as param[:instance]=value; may be repeated or comma-separated")
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
	if err := validateSetOptions(*timeoutFlag); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	specs, err := parseAllSetSpecs(sets, intSets, floatSets)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := run(context.Background(), targets, *addressFlag, specs, *timeoutFlag, *verifyFlag, *saveFlag, *resetFlag); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func validateSetOptions(timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("mecomset: -timeout must be positive")
	}
	return nil
}

func run(ctx context.Context, targets []string, address int, specs []setSpec, timeout time.Duration, verify, save, reset bool) error {
	var failures int
	for _, target := range targets {
		failures += runTarget(ctx, target, address, specs, timeout, verify, save, reset)
	}
	if failures > 0 {
		return fmt.Errorf("mecomset: %d operation(s) failed", failures)
	}
	return nil
}

func runTarget(ctx context.Context, target string, address int, specs []setSpec, timeout time.Duration, verify, save, reset bool) (failures int) {
	targetFailed := false
	conn, err := dial(ctx, target, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s connect_error=%q\n", target, err)
		return 1
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "%s close_error=%q\n", target, err)
			failures++
		}
	}()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: byte(address), Timeout: timeout})
	for _, spec := range specs {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		err := writeSetSpec(reqCtx, client, spec)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s param=%d instance=%d write_error=%q\n", target, spec.ParamID, spec.Instance, err)
			failures++
			targetFailed = true
			continue
		}
		if !verify {
			fmt.Printf("%s param=%d instance=%d type=%s wrote=%s\n", target, spec.ParamID, spec.Instance, spec.Type, formatSetValue(spec))
			continue
		}
		reqCtx, cancel = context.WithTimeout(ctx, timeout)
		got, err := readSetSpec(reqCtx, client, spec)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s param=%d instance=%d verify_error=%q\n", target, spec.ParamID, spec.Instance, err)
			failures++
			targetFailed = true
			continue
		}
		if !setSpecMatches(spec, got) {
			fmt.Fprintf(os.Stderr, "%s param=%d instance=%d verify_mismatch=%s want=%s\n", target, spec.ParamID, spec.Instance, formatReadbackValue(spec, got), formatSetValue(spec))
			failures++
			targetFailed = true
			continue
		}
		fmt.Printf("%s param=%d instance=%d type=%s wrote=%s verify=%s\n", target, spec.ParamID, spec.Instance, spec.Type, formatSetValue(spec), formatReadbackValue(spec, got))
	}
	if !targetFailed && save {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		err := client.SaveToFlash(reqCtx)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s save_error=%q\n", target, err)
			failures++
			targetFailed = true
		} else {
			fmt.Printf("%s save=ack\n", target)
		}
	}
	if !targetFailed && reset {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		err := client.Reset(reqCtx)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s reset_error=%q\n", target, err)
			failures++
		} else {
			fmt.Printf("%s reset=ack\n", target)
		}
	}
	return failures
}

func writeSetSpec(ctx context.Context, client *mecom.Client, spec setSpec) error {
	switch spec.Type {
	case mecom.DataTypeFloat32:
		return client.WriteFloat32(ctx, spec.ParamID, spec.Instance, spec.FloatValue)
	case mecom.DataTypeInt32, "":
		return client.WriteInt32(ctx, spec.ParamID, spec.Instance, spec.IntValue)
	default:
		return fmt.Errorf("unsupported write type %q for parameter %d", spec.Type, spec.ParamID)
	}
}

func readSetSpec(ctx context.Context, client *mecom.Client, spec setSpec) (float64, error) {
	switch spec.Type {
	case mecom.DataTypeFloat32:
		return client.ReadFloat32(ctx, spec.ParamID, spec.Instance)
	case mecom.DataTypeInt32, "":
		v, err := client.ReadInt32(ctx, spec.ParamID, spec.Instance)
		return float64(v), err
	default:
		return 0, fmt.Errorf("unsupported verify type %q for parameter %d", spec.Type, spec.ParamID)
	}
}

func setSpecMatches(spec setSpec, got float64) bool {
	switch spec.Type {
	case mecom.DataTypeFloat32:
		want := float64(spec.FloatValue)
		tolerance := math.Max(1e-4, math.Abs(want)*1e-5)
		return !math.IsNaN(got) && math.Abs(got-want) <= tolerance
	case mecom.DataTypeInt32, "":
		return int32(math.Round(got)) == spec.IntValue
	default:
		return false
	}
}

func dial(ctx context.Context, target string, timeout time.Duration) (net.Conn, error) {
	ep, ok := mecom.ParseEndpoint(target)
	if !ok {
		return nil, fmt.Errorf("invalid target %q", target)
	}
	return mecom.Dial(ctx, ep, timeout)
}

func parseSetSpecs(values []string) ([]setSpec, error) {
	specs, err := parseSetSpecsWithKind(values, setKindAuto)
	if err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("mecomset: at least one -set is required")
	}
	return specs, nil
}

func parseAllSetSpecs(sets, intSets, floatSets []string) ([]setSpec, error) {
	var all []setSpec
	for _, group := range []struct {
		values []string
		kind   setValueKind
	}{
		{values: sets, kind: setKindAuto},
		{values: intSets, kind: setKindInt32},
		{values: floatSets, kind: setKindFloat32},
	} {
		specs, err := parseSetSpecsWithKind(group.values, group.kind)
		if err != nil {
			return nil, err
		}
		all = append(all, specs...)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("mecomset: at least one -set is required")
	}
	return all, nil
}

func parseSetSpecsWithKind(values []string, kind setValueKind) ([]setSpec, error) {
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
			spec, err := parseSetValue(paramID, instance, strings.TrimSpace(right), kind)
			if err != nil {
				return nil, err
			}
			specs = append(specs, spec)
		}
	}
	return specs, nil
}

func parseSetValue(paramID, instance int, raw string, kind setValueKind) (setSpec, error) {
	dataType := mecom.DataType(kind)
	if kind == setKindAuto {
		dataType = inferSetDataType(paramID, raw)
	}
	switch dataType {
	case mecom.DataTypeFloat32:
		value64, err := strconv.ParseFloat(raw, 32)
		if err != nil {
			return setSpec{}, fmt.Errorf("mecomset: invalid FLOAT32 value %q", raw)
		}
		return setSpec{ParamID: paramID, Instance: instance, Type: mecom.DataTypeFloat32, FloatValue: float32(value64)}, nil
	case mecom.DataTypeInt32, "":
		value64, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return setSpec{}, fmt.Errorf("mecomset: invalid INT32 value %q", raw)
		}
		return setSpec{ParamID: paramID, Instance: instance, Type: mecom.DataTypeInt32, IntValue: int32(value64)}, nil
	default:
		return setSpec{}, fmt.Errorf("mecomset: unsupported write type %q for parameter %d", dataType, paramID)
	}
}

func inferSetDataType(paramID int, raw string) mecom.DataType {
	if param, ok := tecParameterByID(paramID); ok && param.Type != "" {
		return param.Type
	}
	if strings.ContainsAny(raw, ".eE") {
		return mecom.DataTypeFloat32
	}
	return mecom.DataTypeInt32
}

func tecParameterByID(paramID int) (mecom.Parameter, bool) {
	for _, param := range mecom.DefaultTECWriteParameters(1) {
		if param.ID == paramID {
			return param, true
		}
	}
	for _, readout := range mecom.DefaultTECReadoutParameters(1) {
		if readout.Parameter.ID == paramID {
			return readout.Parameter, true
		}
	}
	return mecom.Parameter{}, false
}

func formatSetValue(spec setSpec) string {
	switch spec.Type {
	case mecom.DataTypeFloat32:
		return strconv.FormatFloat(float64(spec.FloatValue), 'f', -1, 32)
	default:
		return strconv.FormatInt(int64(spec.IntValue), 10)
	}
}

func formatReadbackValue(spec setSpec, got float64) string {
	switch spec.Type {
	case mecom.DataTypeFloat32:
		return strconv.FormatFloat(got, 'f', -1, 32)
	default:
		return strconv.FormatInt(int64(math.Round(got)), 10)
	}
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
