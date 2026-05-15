package mecom

import (
	"context"
	"fmt"
	"net"
)

// DeviceClient is the unified surface every concrete MeCom client implements.
// Use this as the consumer-facing type when transport choice should be opaque.
type DeviceClient interface {
	ReadClient
	// Close releases any owned resources (sockets, serial ports). For shared
	// transports (a CAN bus the caller still owns), Close is a no-op.
	Close() error
}

// NewForEndpoint opens the appropriate concrete client for an endpoint:
//   - "tcp:..." and "serial:..." return a *Client (ASCII MeCom protocol)
//   - "can:can0/0x4b" returns a *CANopenClient on the named SocketCAN interface
//
// The caller owns the returned client; calling Close releases the transport.
// To layer on a custom CAN transceiver (Kvaser, USB-CAN, remote bridge),
// instantiate the concrete client directly with NewCANopenClient or NewClient.
//
// can: endpoints have the form "can:IFACE/NODE", where NODE is a CANopen node
// ID (1..127). Example: "can:can0/0x4b".
func NewForEndpoint(ctx context.Context, ep Endpoint, cfg ClientConfig, dialCAN CANDialer) (DeviceClient, error) {
	switch ep.Network {
	case "serial", "tcp":
		conn, err := Dial(ctx, ep, cfg.Timeout)
		if err != nil {
			return nil, err
		}
		return &closingASCII{Client: NewClient(conn, cfg), conn: conn}, nil
	case "can":
		if dialCAN == nil {
			return nil, fmt.Errorf("%w: can endpoint requires a CANDialer", ErrTransportNotSupported)
		}
		iface, node, err := parseCANEndpoint(ep.Address)
		if err != nil {
			return nil, err
		}
		rw, closer, err := dialCAN(ctx, iface)
		if err != nil {
			return nil, err
		}
		canCfg := cfg
		canCfg.Address = node
		return &closingCAN{CANopenClient: NewCANopenClient(rw, canCfg), closer: closer}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported endpoint network %q", ErrTransportNotSupported, ep.Network)
	}
}

// CANDialer opens a CAN transceiver for the given interface. The closer is
// invoked when the DeviceClient is Closed. Implementations live in adapter
// packages (e.g. socketcan, kvaser) so this core package stays platform-free.
type CANDialer func(ctx context.Context, iface string) (CANTransceiver, func() error, error)

func parseCANEndpoint(addr string) (string, byte, error) {
	for i := 0; i < len(addr); i++ {
		if addr[i] == '/' {
			iface := addr[:i]
			nodeStr := addr[i+1:]
			node, err := parseNodeID(nodeStr)
			if err != nil {
				return "", 0, err
			}
			return iface, node, nil
		}
	}
	return "", 0, fmt.Errorf("%w: can endpoint %q must be IFACE/NODE", ErrBadAddress, addr)
}

func parseNodeID(s string) (byte, error) {
	var n uint64
	switch {
	case len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X'):
		_, err := fmt.Sscanf(s, "0x%x", &n)
		if err != nil {
			return 0, fmt.Errorf("%w: invalid node %q: %v", ErrBadAddress, s, err)
		}
	default:
		_, err := fmt.Sscanf(s, "%d", &n)
		if err != nil {
			return 0, fmt.Errorf("%w: invalid node %q: %v", ErrBadAddress, s, err)
		}
	}
	if n == 0 || n > 127 {
		return 0, fmt.Errorf("%w: node %d outside CANopen 1..127", ErrBadAddress, n)
	}
	return byte(n), nil
}

type closingASCII struct {
	*Client
	conn net.Conn
}

func (c *closingASCII) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

type closingCAN struct {
	*CANopenClient
	closer func() error
}

func (c *closingCAN) Close() error {
	if c.closer != nil {
		return c.closer()
	}
	return nil
}

// Compile-time assertions: both concrete wrappers satisfy DeviceClient.
var (
	_ DeviceClient = (*closingASCII)(nil)
	_ DeviceClient = (*closingCAN)(nil)
)
