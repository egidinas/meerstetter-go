//go:build linux

package main

import (
	"context"
	"time"

	"github.com/egidinas/meerstetter-go/canopen"
	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/socketcan"
)

// socketCANDialer is the default Linux CANDialer wired into mecomrun. The core
// mecom package stays platform-free; this binary chooses SocketCAN as the
// transport. Adapter packages (Kvaser, PCAN, Ethernet-CAN) can supply their own
// dialer through library integration.
func socketCANDialer(ctx context.Context, iface string) (mecom.CANTransceiver, func() error, error) {
	_ = ctx
	conn, err := socketcan.Open(iface)
	if err != nil {
		return nil, nil, err
	}
	return socketCANTransceiver{conn: conn}, conn.Close, nil
}

type socketCANTransceiver struct {
	conn *socketcan.Conn
}

func (t socketCANTransceiver) Send(f canopen.Frame) error {
	return t.conn.Send(f)
}

func (t socketCANTransceiver) Recv(timeout time.Duration) (canopen.Frame, error) {
	return t.conn.Recv(timeout)
}
