package main

import (
	"strings"
	"testing"
)

func TestParseAddressAcceptsDecimalAndHex(t *testing.T) {
	tests := map[string]byte{
		"75":   75,
		"0x4c": 0x4c,
	}
	for input, want := range tests {
		got, err := parseAddress(input)
		if err != nil {
			t.Fatalf("parseAddress(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("parseAddress(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestParseAddressRejectsReservedAndOutOfRange(t *testing.T) {
	for _, input := range []string{"0", "255", "not-an-address"} {
		if _, err := parseAddress(input); err == nil {
			t.Fatalf("parseAddress(%q) returned nil error", input)
		}
	}
}

func TestParseAddressZeroModeAllowsDisabledRouteOrderAndDefaultDevice(t *testing.T) {
	tests := map[string]byte{
		"0":    0,
		"76":   76,
		"0x4c": 0x4c,
	}
	for input, want := range tests {
		got, err := parseAddressZeroMode(input)
		if err != nil {
			t.Fatalf("parseAddressZeroMode(%q) returned error: %v", input, err)
		}
		if got.fixed != want {
			t.Fatalf("parseAddressZeroMode(%q).fixed = %d, want %d", input, got.fixed, want)
		}
	}
	got, err := parseAddressZeroMode("route-order")
	if err != nil {
		t.Fatalf("parseAddressZeroMode(route-order) returned error: %v", err)
	}
	if !got.routeOrder {
		t.Fatalf("parseAddressZeroMode(route-order) = %+v, want route order", got)
	}
	for _, input := range []string{"255", "not-an-address"} {
		if _, err := parseAddressZeroMode(input); err == nil {
			t.Fatalf("parseAddressZeroMode(%q) returned nil error", input)
		}
	}
}

func TestRouteFlagsSet(t *testing.T) {
	var routes routeFlags
	if err := routes.Set("0x4b=serial:COM3@57600"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := routes.Set("0x4c=tcp:127.0.0.1:50001"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(routes))
	}
	if routes[0].Address != 0x4b || routes[0].Target != "serial:COM3@57600" {
		t.Fatalf("route = %+v", routes[0])
	}
	if routes[1].Address != 0x4c || routes[1].Target != "tcp:127.0.0.1:50001" {
		t.Fatalf("route = %+v", routes[1])
	}
	if got := routes.String(); !strings.Contains(got, "0x4B=serial:COM3@57600") {
		t.Fatalf("String() = %q", got)
	}
	if got := routes.String(); !strings.Contains(got, "0x4C=tcp:127.0.0.1:50001") {
		t.Fatalf("String() = %q", got)
	}
	if err := routes.Set("0x4d=can:can0/0x4b"); err == nil {
		t.Fatal("Set accepted CAN target")
	}
}

func TestSelectServerModeAcceptsSingleTarget(t *testing.T) {
	mode, err := selectServerMode("serial:COM3@57600", nil)
	if err != nil {
		t.Fatalf("selectServerMode returned error: %v", err)
	}
	if mode.target != "serial:COM3@57600" {
		t.Fatalf("target = %q, want serial:COM3@57600", mode.target)
	}
	if len(mode.routes) != 0 {
		t.Fatalf("routes = %d, want 0", len(mode.routes))
	}
}

func TestSelectServerModeAcceptsRoutes(t *testing.T) {
	routes := routeFlags{{Address: 0x4b, Target: "serial:COM3@57600"}}
	mode, err := selectServerMode("", routes)
	if err != nil {
		t.Fatalf("selectServerMode returned error: %v", err)
	}
	if mode.target != "" {
		t.Fatalf("target = %q, want empty", mode.target)
	}
	if len(mode.routes) != 1 || mode.routes[0].Address != 0x4b {
		t.Fatalf("routes = %+v", mode.routes)
	}
}

func TestSelectServerModeRejectsInvalidCombinations(t *testing.T) {
	routes := routeFlags{{Address: 0x4b, Target: "serial:COM3@57600"}}
	tests := map[string]struct {
		target string
		routes routeFlags
	}{
		"empty":        {"", nil},
		"target-route": {"serial:COM3@57600", routes},
		"bad-target":   {"can:", nil},
		"can-target":   {"can:can0/0x4b", nil},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := selectServerMode(tc.target, tc.routes); err == nil {
				t.Fatal("selectServerMode returned nil error")
			}
		})
	}
}
