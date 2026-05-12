package mecom

import (
	"fmt"
	"net"
	"time"

	"github.com/egidinas/loom-gossamer-shared/go/transport"
)

// Endpoint is a shared transport endpoint alias for compatibility.
type Endpoint = transport.Endpoint

const legacyDefaultSerialBaud = 115200

// ParseTarget parses MeCom endpoints with Loom-compatible serial defaults.
func ParseTarget(target string) (Endpoint, bool) {
	return transport.ParseEndpointWithDefault(target, legacyDefaultSerialBaud)
}

// ParseEndpoint parses MeCom endpoints using shared parser defaults.
func ParseEndpoint(target string) (Endpoint, bool) {
	return transport.ParseEndpointWithDefault(target, legacyDefaultSerialBaud)
}

// Open opens a transport connection to the endpoint.
func Open(ep Endpoint, timeout time.Duration) (net.Conn, error) {
	if timeout < 0 {
		return nil, fmt.Errorf("negative timeout")
	}
	return transport.Open(ep, timeout)
}
