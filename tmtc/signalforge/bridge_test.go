package signalforge

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/tmtc"
	"github.com/egidinas/signalforge/contracts"
)

const (
	localTMTCPackage      = "github.com/egidinas/meerstetter-go/tmtc"
	adapterPackage        = "github.com/egidinas/meerstetter-go/tmtc/signalforge"
	contractPackage       = "github.com/egidinas/signalforge/contracts"
	signalForgeImportRoot = "github.com/egidinas/signalforge"
)

func TestTelecommandBridgePreservesFieldsAndBoundary(t *testing.T) {
	local := tmtc.Telecommand{
		ID:             "cmd-1",
		TargetID:       "tec-31",
		SessionID:      "session-1",
		Time:           time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
		Name:           "reset",
		Payload:        []byte{1, 2, 3},
		Arguments:      map[string]any{"temperature": 25.0},
		IdempotencyKey: "stable-key",
		RequiresAck:    true,
		Metadata:       map[string]string{"source": "operator"},
	}

	contract := ToContractTelecommand(local)
	if got, want := reflect.TypeOf(local).PkgPath(), "github.com/egidinas/meerstetter-go/tmtc"; got != want {
		t.Fatalf("local telecommand package = %q, want %q", got, want)
	}
	if got, want := reflect.TypeOf(contract).PkgPath(), "github.com/egidinas/signalforge/contracts"; got != want {
		t.Fatalf("contract telecommand package = %q, want %q", got, want)
	}

	roundTrip := FromContractTelecommand(contract)
	if !reflect.DeepEqual(roundTrip, local) {
		t.Fatalf("round trip mismatch:\n got: %#v\nwant: %#v", roundTrip, local)
	}

	contract.Payload[0] = 9
	contract.Metadata["source"] = "mutated"
	contract.Arguments["temperature"] = 30.0
	if local.Payload[0] != 1 || local.Metadata["source"] != "operator" || local.Arguments["temperature"] != 25.0 {
		t.Fatal("contract mutation leaked through bridge boundary")
	}
}

func TestTelemetryAndCommandEventBridge(t *testing.T) {
	at := time.Date(2026, 5, 15, 12, 1, 0, 0, time.UTC)
	tm := tmtc.Telemetry{
		ID:        "tm-1",
		TargetID:  "tec-31",
		SessionID: "session-1",
		Time:      at,
		Name:      "temperature.object",
		Value:     21.5,
		Unit:      "degC",
		Quality:   "good",
		Raw:       []byte{4, 5, 6},
		Metadata:  map[string]string{"source": "poll"},
	}
	if got := FromContractTelemetry(ToContractTelemetry(tm)); !reflect.DeepEqual(got, tm) {
		t.Fatalf("telemetry round trip mismatch: got %#v want %#v", got, tm)
	}

	ev := contracts.CommandEvent{
		ID:             "ev-1",
		CommandID:      "cmd-1",
		SessionID:      "session-1",
		Time:           at,
		Status:         contracts.CommandCompleted,
		Transport:      "tcp",
		IdempotencyKey: "stable-key",
		Result:         "ok",
		Metadata:       map[string]string{"source": "operator"},
	}
	if got := ToContractCommandEvent(FromContractCommandEvent(ev)); !reflect.DeepEqual(got, ev) {
		t.Fatalf("command event round trip mismatch: got %#v want %#v", got, ev)
	}
}

func TestSignalForgeTMTCImportBoundary(t *testing.T) {
	packages := goListPackages(t, "./...")
	contractImporters := make([]string, 0, 1)
	for _, pkg := range packages {
		for _, imported := range pkg.Imports {
			if imported == contractPackage {
				contractImporters = append(contractImporters, pkg.ImportPath)
			}
			if pkg.ImportPath == localTMTCPackage && strings.HasPrefix(imported, signalForgeImportRoot) {
				t.Fatalf("%s imports %s; keep SignalForge behind %s", localTMTCPackage, imported, adapterPackage)
			}
		}
	}
	if !reflect.DeepEqual(contractImporters, []string{adapterPackage}) {
		t.Fatalf("%s importers = %v, want [%s]", contractPackage, contractImporters, adapterPackage)
	}
}

type listedPackage struct {
	ImportPath string
	Imports    []string
}

func goListPackages(t *testing.T, pattern string) []listedPackage {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", pattern)
	cmd.Dir = "../.."
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list %s failed: %v\n%s", pattern, err, exitErr.Stderr)
		}
		t.Fatalf("go list %s failed: %v", pattern, err)
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	var packages []listedPackage
	for {
		var pkg listedPackage
		err := dec.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		packages = append(packages, pkg)
	}
	return packages
}
