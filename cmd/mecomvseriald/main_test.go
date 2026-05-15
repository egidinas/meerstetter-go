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

func TestRouteFlagsSet(t *testing.T) {
	var routes routeFlags
	if err := routes.Set("0x4b=serial:COM3@57600"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}
	if routes[0].Address != 0x4b || routes[0].Target != "serial:COM3@57600" {
		t.Fatalf("route = %+v", routes[0])
	}
	if got := routes.String(); !strings.Contains(got, "0x4B=serial:COM3@57600") {
		t.Fatalf("String() = %q", got)
	}
}
