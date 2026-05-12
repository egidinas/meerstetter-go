package main

import (
	"reflect"
	"testing"
)

func TestParseSetSpecsAcceptsCommaSeparatedParamInstanceValues(t *testing.T) {
	got, err := parseSetSpecs([]string{"2072:1=1,2070=75"})
	if err != nil {
		t.Fatalf("parseSetSpecs returned error: %v", err)
	}
	want := []setSpec{
		{ParamID: 2072, Instance: 1, Value: 1},
		{ParamID: 2070, Instance: 1, Value: 75},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseSetSpecs() = %#v, want %#v", got, want)
	}
}

func TestParseSetSpecsDefaultsInstanceToOne(t *testing.T) {
	got, err := parseSetSpecs([]string{"2071=1000"})
	if err != nil {
		t.Fatalf("parseSetSpecs returned error: %v", err)
	}
	want := []setSpec{{ParamID: 2071, Instance: 1, Value: 1000}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseSetSpecs() = %#v, want %#v", got, want)
	}
}

func TestParseSetSpecsRejectsInvalidInput(t *testing.T) {
	tests := []string{
		"",
		"bad=1",
		"2072=bad",
		"2072:999=1",
		"2072",
	}
	for _, tc := range tests {
		if _, err := parseSetSpecs([]string{tc}); err == nil {
			t.Fatalf("parseSetSpecs(%q) returned nil error", tc)
		}
	}
}
