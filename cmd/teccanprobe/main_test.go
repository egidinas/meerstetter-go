package main

import (
	"testing"

	"github.com/egidinas/meerstetter-go/canopen"
)

func TestSDOResponseMatchesProbeRequiresObjectIdentity(t *testing.T) {
	probe := sdoProbe{Index: 0x2030, SubIndex: 1}
	if !sdoResponseMatchesProbe(canopen.SDOUploadResponse{Index: 0x2030, SubIndex: 1}, probe) {
		t.Fatal("matching SDO response was rejected")
	}
	if sdoResponseMatchesProbe(canopen.SDOUploadResponse{Index: 0x2031, SubIndex: 1}, probe) {
		t.Fatal("mismatched SDO index was accepted")
	}
	if sdoResponseMatchesProbe(canopen.SDOUploadResponse{Index: 0x2030, SubIndex: 2}, probe) {
		t.Fatal("mismatched SDO subindex was accepted")
	}
}

func TestMeComResponseMatchesRequestRequiresAddressAndSequence(t *testing.T) {
	frame := canopen.Frame{
		ID:  0x44b,
		DLC: 8,
		Data: [8]byte{
			0x81,
			0x4b,
		},
	}
	if !mecomResponseMatchesRequest(frame, 0x4b, 1) {
		t.Fatal("matching MeCom response was rejected")
	}
	frame.Data[1] = 0x4c
	if mecomResponseMatchesRequest(frame, 0x4b, 1) {
		t.Fatal("mismatched MeCom address was accepted")
	}
	frame.Data[1] = 0x4b
	frame.Data[0] = 0x82
	if mecomResponseMatchesRequest(frame, 0x4b, 1) {
		t.Fatal("mismatched MeCom sequence was accepted")
	}
	frame.Data[0] = 0x01
	if mecomResponseMatchesRequest(frame, 0x4b, 1) {
		t.Fatal("request frame was accepted as response")
	}
}
