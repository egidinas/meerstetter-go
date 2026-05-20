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
	for _, input := range []string{"auto-first", "auto", "first"} {
		got, err := parseAddressZeroMode(input)
		if err != nil {
			t.Fatalf("parseAddressZeroMode(%q) returned error: %v", input, err)
		}
		if !got.autoFirst {
			t.Fatalf("parseAddressZeroMode(%q) = %+v, want auto-first", input, got)
		}
	}
	for _, input := range []string{"255", "not-an-address"} {
		if _, err := parseAddressZeroMode(input); err == nil {
			t.Fatalf("parseAddressZeroMode(%q) returned nil error", input)
		}
	}
}

func TestResolveAddressZeroModeUsesConfiguredRouteOrder(t *testing.T) {
	routes := routeFlags{
		{Address: 0x5a, Target: "serial:COM9@57600"},
		{Address: 0x5b, Target: "tcp:127.0.0.1:15010"},
		{Address: 0x5a, Target: "can:can0/0x5a"},
	}

	autoFirst, err := parseAddressZeroMode("auto-first")
	if err != nil {
		t.Fatalf("parseAddressZeroMode(auto-first) returned error: %v", err)
	}
	fixed, order, err := resolveAddressZeroMode(autoFirst, routes)
	if err != nil {
		t.Fatalf("resolveAddressZeroMode(auto-first) returned error: %v", err)
	}
	if fixed != 0x5a {
		t.Fatalf("fixed = 0x%02X, want first configured address 0x5A", fixed)
	}
	if len(order) != 0 {
		t.Fatalf("order = %v, want nil/empty for auto-first", order)
	}

	routeOrder, err := parseAddressZeroMode("route-order")
	if err != nil {
		t.Fatalf("parseAddressZeroMode(route-order) returned error: %v", err)
	}
	fixed, order, err = resolveAddressZeroMode(routeOrder, routes)
	if err != nil {
		t.Fatalf("resolveAddressZeroMode(route-order) returned error: %v", err)
	}
	if fixed != 0 {
		t.Fatalf("fixed = 0x%02X, want disabled fixed address", fixed)
	}
	wantOrder := []byte{0x5a, 0x5b}
	if len(order) != len(wantOrder) {
		t.Fatalf("order = %v, want %v", order, wantOrder)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("order = %v, want %v", order, wantOrder)
		}
	}

	fixedMode, err := parseAddressZeroMode("0x5b")
	if err != nil {
		t.Fatalf("parseAddressZeroMode(0x5b) returned error: %v", err)
	}
	fixed, order, err = resolveAddressZeroMode(fixedMode, routes)
	if err != nil {
		t.Fatalf("resolveAddressZeroMode(fixed) returned error: %v", err)
	}
	if fixed != 0x5b {
		t.Fatalf("fixed = 0x%02X, want 0x5B", fixed)
	}
	if len(order) != 0 {
		t.Fatalf("order = %v, want nil/empty for fixed mode", order)
	}
}

func TestResolveAddressZeroModeRejectsAutoFirstWithoutRoutes(t *testing.T) {
	autoFirst, err := parseAddressZeroMode("auto-first")
	if err != nil {
		t.Fatalf("parseAddressZeroMode(auto-first) returned error: %v", err)
	}
	if _, _, err := resolveAddressZeroMode(autoFirst, nil); err == nil {
		t.Fatal("resolveAddressZeroMode(auto-first, nil) returned nil error")
	}
}

func TestParseRouteSelectionPolicy(t *testing.T) {
	for _, input := range []string{"", "fixed", "fixed-preference"} {
		got, err := parseRouteSelectionPolicy(input)
		if err != nil {
			t.Fatalf("parseRouteSelectionPolicy(%q) returned error: %v", input, err)
		}
		if got != "" && got != "fixed-preference" {
			t.Fatalf("parseRouteSelectionPolicy(%q) = %q, want fixed-preference or empty default", input, got)
		}
	}
	got, err := parseRouteSelectionPolicy("dynamic")
	if err != nil {
		t.Fatalf("parseRouteSelectionPolicy(dynamic) returned error: %v", err)
	}
	if got != "dynamic" {
		t.Fatalf("parseRouteSelectionPolicy(dynamic) = %q, want dynamic", got)
	}
	if _, err := parseRouteSelectionPolicy("round-robin"); err == nil {
		t.Fatal("parseRouteSelectionPolicy accepted unknown policy")
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
	if err := routes.Set("0x4d=can:can0/0x4d"); err != nil {
		t.Fatalf("Set returned error for CAN target: %v", err)
	}
	if len(routes) != 3 {
		t.Fatalf("routes = %d, want 3", len(routes))
	}
	if routes[2].Address != 0x4d || routes[2].Target != "can:can0/0x4d" {
		t.Fatalf("route = %+v", routes[2])
	}
	if got := routes.String(); !strings.Contains(got, "0x4D=can:can0/0x4d") {
		t.Fatalf("String() = %q", got)
	}
}

func TestRouteFlagsAddressesDeduplicatesDuplicateTransports(t *testing.T) {
	routes := routeFlags{
		{Address: 0x4b, Target: "serial:COM3@57600"},
		{Address: 0x4b, Target: "can:can0/0x4b"},
		{Address: 0x4c, Target: "serial:COM4@57600"},
		{Address: 0x4c, Target: "can:can0/0x4c"},
	}
	got := routes.addresses()
	want := []byte{0x4b, 0x4c}
	if len(got) != len(want) {
		t.Fatalf("addresses() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("addresses() = %v, want %v", got, want)
		}
	}
}

func TestSelectServerModeAcceptsSingleTarget(t *testing.T) {
	for _, target := range []string{"serial:COM3@57600", "can:can0/0x4b"} {
		t.Run(target, func(t *testing.T) {
			mode, err := selectServerMode(target, nil)
			if err != nil {
				t.Fatalf("selectServerMode returned error: %v", err)
			}
			if mode.target != target {
				t.Fatalf("target = %q, want %q", mode.target, target)
			}
			if len(mode.routes) != 0 {
				t.Fatalf("routes = %d, want 0", len(mode.routes))
			}
		})
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
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := selectServerMode(tc.target, tc.routes); err == nil {
				t.Fatal("selectServerMode returned nil error")
			}
		})
	}
}
