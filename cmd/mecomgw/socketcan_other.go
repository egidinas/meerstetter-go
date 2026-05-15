//go:build !linux

package main

import (
	"context"
	"fmt"

	"github.com/egidinas/meerstetter-go/mecom"
)

func socketCANDialer(ctx context.Context, iface string) (mecom.CANTransceiver, func() error, error) {
	_ = ctx
	return nil, nil, fmt.Errorf("%w: SocketCAN interface %q requires Linux; use serial:, tcp:, or a platform CAN adapter", mecom.ErrTransportNotSupported, iface)
}
