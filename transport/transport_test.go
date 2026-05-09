package transport

import "testing"

func TestParseEndpointSerial(t *testing.T) {
	ep, ok := ParseEndpoint("serial:/dev/ttyUSB0@57600")
	if !ok {
		t.Fatal("not parsed")
	}
	if ep.Network != "serial" || ep.Address != "/dev/ttyUSB0" || ep.Baud != 57600 {
		t.Fatalf("endpoint = %#v", ep)
	}
}

func TestParseEndpointSerialDefaultBaud(t *testing.T) {
	ep, ok := ParseEndpoint("serial:/dev/ttyUSB0")
	if !ok {
		t.Fatal("not parsed")
	}
	if ep.Network != "serial" || ep.Address != "/dev/ttyUSB0" || ep.Baud != DefaultSerialBaud {
		t.Fatalf("endpoint = %#v", ep)
	}
}

func TestParseEndpointTCP(t *testing.T) {
	ep, ok := ParseEndpoint("tcp:192.168.1.50:50000")
	if !ok {
		t.Fatal("not parsed")
	}
	if ep.Network != "tcp" || ep.Address != "192.168.1.50:50000" {
		t.Fatalf("endpoint = %#v", ep)
	}
}
