//go:build linux

package utility

import "testing"

func TestParseSocketCANTarget(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		metadata map[string]string
		wantIF   string
		wantAddr byte
		wantOK   bool
		wantErr  bool
	}{
		{
			name:     "query address",
			target:   "socketcan:can0?addr=0x1f",
			wantIF:   "can0",
			wantAddr: 0x1f,
			wantOK:   true,
		},
		{
			name:     "at address",
			target:   "socketcan:can1@32",
			wantIF:   "can1",
			wantAddr: 32,
			wantOK:   true,
		},
		{
			name:     "metadata address",
			target:   "socketcan:can0",
			metadata: map[string]string{"mecom_address": "0x21"},
			wantIF:   "can0",
			wantAddr: 0x21,
			wantOK:   true,
		},
		{
			name:   "non socketcan",
			target: "tcp:127.0.0.1:15000",
		},
		{
			name:    "missing address",
			target:  "socketcan:can0",
			wantOK:  true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIF, gotAddr, gotOK, err := parseSocketCANTarget(tt.target, tt.metadata)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if gotIF != tt.wantIF || gotAddr != tt.wantAddr {
				t.Fatalf("target = (%q, 0x%02x), want (%q, 0x%02x)", gotIF, gotAddr, tt.wantIF, tt.wantAddr)
			}
		})
	}
}

func TestParseCANopenTarget(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		metadata map[string]string
		wantIF   string
		wantNode byte
		wantOK   bool
		wantErr  bool
	}{
		{
			name:     "query node",
			target:   "canopen:can0?node=0x4b",
			wantIF:   "can0",
			wantNode: 0x4b,
			wantOK:   true,
		},
		{
			name:     "metadata node",
			target:   "canopen:can0",
			metadata: map[string]string{"canopen_node": "0x54"},
			wantIF:   "can0",
			wantNode: 0x54,
			wantOK:   true,
		},
		{
			name:   "non canopen",
			target: "socketcan:can0?addr=0x1f",
		},
		{
			name:    "missing node",
			target:  "canopen:can0",
			wantOK:  true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIF, gotNode, gotOK, err := parseCANopenTarget(tt.target, tt.metadata)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if gotIF != tt.wantIF || gotNode != tt.wantNode {
				t.Fatalf("target = (%q, 0x%02x), want (%q, 0x%02x)", gotIF, gotNode, tt.wantIF, tt.wantNode)
			}
		})
	}
}
