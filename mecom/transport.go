package mecom

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
)

// Endpoint describes one transparent MeCom transport target.
type Endpoint struct {
	Network string // "tcp", "serial", or "can"
	Address string
	Baud    int
}

func (e Endpoint) String() string {
	switch e.Network {
	case "serial":
		return fmt.Sprintf("serial:%s@%d", e.Address, e.Baud)
	case "can":
		return "can:" + e.Address
	default:
		return e.Address
	}
}

const legacyDefaultSerialBaud = 115200

// ParseTarget parses MeCom endpoints with Loom-compatible serial defaults.
func ParseTarget(target string) (Endpoint, bool) {
	return ParseEndpoint(target)
}

// ParseEndpoint parses "host:port", "tcp:host:port", "serial:/dev/ttyUSB0@57600",
// "can:bus/node", bare POSIX serial devices, and Windows COM ports.
func ParseEndpoint(target string) (Endpoint, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return Endpoint{}, false
	}
	lower := strings.ToLower(target)
	if strings.HasPrefix(lower, "tcp:") {
		return Endpoint{Network: "tcp", Address: strings.TrimSpace(target[4:])}, true
	}
	if strings.HasPrefix(lower, "can:") {
		return Endpoint{Network: "can", Address: strings.TrimSpace(target[4:])}, true
	}
	if strings.HasPrefix(lower, "serial:") || strings.HasPrefix(target, "/dev/") || strings.HasPrefix(strings.ToUpper(target), "COM") {
		raw := strings.TrimPrefix(target, "serial:")
		baud := legacyDefaultSerialBaud
		if at := strings.LastIndex(raw, "@"); at != -1 {
			if b, err := strconv.Atoi(strings.TrimSpace(raw[at+1:])); err == nil && b > 0 {
				baud = b
			}
			raw = raw[:at]
		}
		return Endpoint{Network: "serial", Address: strings.TrimSpace(raw), Baud: baud}, true
	}
	return Endpoint{Network: "tcp", Address: target}, true
}

// Open opens a transport connection to the endpoint.
func Open(ep Endpoint, timeout time.Duration) (net.Conn, error) {
	if timeout < 0 {
		return nil, fmt.Errorf("negative timeout")
	}
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return Dial(ctx, ep, timeout)
}

// Dial opens a transport connection.
func Dial(ctx context.Context, ep Endpoint, timeout time.Duration) (net.Conn, error) {
	switch ep.Network {
	case "serial":
		port, err := serial.Open(ep.Address, &serial.Mode{BaudRate: ep.Baud})
		if err != nil {
			return nil, fmt.Errorf("%w: open serial %s@%d: %v", ErrUnreachable, ep.Address, ep.Baud, err)
		}
		return &serialConn{Port: port, id: ep.String()}, nil
	case "can":
		return nil, fmt.Errorf("%w: CAN endpoints require an application adapter: %s", ErrTransportNotSupported, ep.Address)
	default:
		var d net.Dialer
		if timeout > 0 {
			d.Timeout = timeout
		}
		conn, err := d.DialContext(ctx, "tcp", ep.Address)
		if err != nil {
			return nil, fmt.Errorf("%w: dial tcp %s: %v", ErrUnreachable, ep.Address, err)
		}
		return conn, nil
	}
}

type serialConn struct {
	serial.Port
	id string
}

func (c *serialConn) LocalAddr() net.Addr                { return serialAddr(c.id) }
func (c *serialConn) RemoteAddr() net.Addr               { return serialAddr(c.id) }
func (c *serialConn) SetDeadline(t time.Time) error      { return c.SetReadDeadline(t) }
func (c *serialConn) SetWriteDeadline(_ time.Time) error { return nil }
func (c *serialConn) SetReadDeadline(t time.Time) error {
	d := time.Until(t)
	if d < 0 {
		d = 0
	}
	c.Port.SetReadTimeout(d)
	return nil
}

type serialAddr string

func (serialAddr) Network() string  { return "serial" }
func (a serialAddr) String() string { return string(a) }
