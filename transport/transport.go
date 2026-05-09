package transport

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
)

// Endpoint describes one MeCom transport target.
type Endpoint struct {
	Network string // "tcp" or "serial"
	Address string
	Baud    int
}

func (e Endpoint) String() string {
	if e.Network == "serial" {
		return fmt.Sprintf("serial:%s@%d", e.Address, e.Baud)
	}
	return e.Address
}

// ParseEndpoint parses "host:port", "tcp:host:port", "serial:/dev/ttyUSB0@115200",
// or a bare POSIX serial device path.
func ParseEndpoint(target string) (Endpoint, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return Endpoint{}, false
	}
	lower := strings.ToLower(target)
	if strings.HasPrefix(lower, "tcp:") {
		return Endpoint{Network: "tcp", Address: strings.TrimSpace(target[4:])}, true
	}
	if strings.HasPrefix(lower, "serial:") || strings.HasPrefix(target, "/dev/") || strings.HasPrefix(strings.ToUpper(target), "COM") {
		raw := strings.TrimPrefix(target, "serial:")
		baud := 115200
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

// Dial opens a TCP or serial transport. The returned connection implements
// net.Conn so callers can set deadlines when supported by the transport.
func Dial(ctx context.Context, ep Endpoint, timeout time.Duration) (net.Conn, error) {
	switch ep.Network {
	case "serial":
		port, err := serial.Open(ep.Address, &serial.Mode{BaudRate: ep.Baud})
		if err != nil {
			return nil, fmt.Errorf("transport: open serial %s@%d: %w", ep.Address, ep.Baud, err)
		}
		return &serialConn{Port: port, id: ep.String()}, nil
	default:
		var d net.Dialer
		if timeout > 0 {
			d.Timeout = timeout
		}
		return d.DialContext(ctx, "tcp", ep.Address)
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
	return c.Port.SetReadTimeout(d)
}

type serialAddr string

func (serialAddr) Network() string  { return "serial" }
func (a serialAddr) String() string { return string(a) }
