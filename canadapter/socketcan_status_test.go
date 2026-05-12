package canadapter

import "testing"

const sampleIPDetails = `4: can0: <NOARP,UP,LOWER_UP,ECHO> mtu 16 qdisc pfifo_fast state UNKNOWN mode DEFAULT group default qlen 10
    link/can  promiscuity 0 minmtu 0 maxmtu 0
    can state ERROR-ACTIVE (berr-counter tx 0 rx 0) restart-ms 0
          bitrate 500000 sample-point 0.875
          mcp251x: tseg1 3..16 tseg2 2..8 sjw 1..4 brp 2..64 brp-inc 2
`

func TestParseSocketCANStatus(t *testing.T) {
	status, err := ParseSocketCANStatus(sampleIPDetails)
	if err != nil {
		t.Fatal(err)
	}
	if status.Interface != "can0" {
		t.Fatalf("interface=%q", status.Interface)
	}
	if status.Type != "can" {
		t.Fatalf("type=%q", status.Type)
	}
	if status.OperState != "UNKNOWN" {
		t.Fatalf("oper=%q", status.OperState)
	}
	if status.CANState != "ERROR-ACTIVE" {
		t.Fatalf("can state=%q", status.CANState)
	}
	if status.Bitrate != 500000 {
		t.Fatalf("bitrate=%d", status.Bitrate)
	}
	if status.Driver != "mcp251x" {
		t.Fatalf("driver=%q", status.Driver)
	}
}

func TestValidateSocketCANStatusMatchesPiXtend(t *testing.T) {
	status, err := ParseSocketCANStatus(sampleIPDetails)
	if err != nil {
		t.Fatal(err)
	}
	findings := ValidateSocketCANStatus(status, MustProfile(ProfilePiXtendV2L))
	if len(findings) != 1 || findings[0].Severity != SeverityInfo {
		t.Fatalf("findings=%+v", findings)
	}
}

func TestValidateSocketCANStatusReportsDriverMismatchAndBusOff(t *testing.T) {
	status, err := ParseSocketCANStatus(sampleIPDetails)
	if err != nil {
		t.Fatal(err)
	}
	status.CANState = "BUS-OFF"
	status.Driver = "gs_usb"
	findings := ValidateSocketCANStatus(status, MustProfile(ProfilePiXtendV2L))
	if len(findings) < 2 {
		t.Fatalf("expected multiple findings, got %+v", findings)
	}
	if findings[0].Severity != SeverityError {
		t.Fatalf("first finding should be error: %+v", findings)
	}
}

