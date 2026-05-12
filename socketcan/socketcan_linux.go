//go:build linux

package socketcan

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/egidinas/meerstetter-go/canopen"
	"golang.org/x/sys/unix"
)

const (
	canFrameSize = 16
	canSFFMask   = 0x000007ff
	canEFFFlag   = 0x80000000
	canRTRFlag   = 0x40000000
	canERRFlag   = 0x20000000
)

var ErrTimeout = errors.New("socketcan: receive timeout")

type Conn struct {
	fd    int
	iface string
}

func Open(iface string) (*Conn, error) {
	link, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("socketcan: interface %s: %w", iface, err)
	}
	fd, err := unix.Socket(unix.AF_CAN, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.CAN_RAW)
	if err != nil {
		return nil, fmt.Errorf("socketcan: open raw socket: %w", err)
	}
	conn := &Conn{fd: fd, iface: iface}
	if err := unix.Bind(fd, &unix.SockaddrCAN{Ifindex: link.Index}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("socketcan: bind %s: %w", iface, err)
	}
	return conn, nil
}

func (c *Conn) Close() error {
	if c.fd < 0 {
		return nil
	}
	err := unix.Close(c.fd)
	c.fd = -1
	return err
}

func (c *Conn) Send(f canopen.Frame) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if f.ID > canSFFMask {
		return fmt.Errorf("socketcan: only 11-bit CAN IDs are supported, got 0x%X", f.ID)
	}
	var raw [canFrameSize]byte
	binary.LittleEndian.PutUint32(raw[0:4], f.ID)
	raw[4] = f.DLC
	copy(raw[8:16], f.Data[:])
	n, err := unix.Write(c.fd, raw[:])
	if err != nil {
		return fmt.Errorf("socketcan: send on %s: %w", c.iface, err)
	}
	if n != canFrameSize {
		return fmt.Errorf("socketcan: short write on %s: %d/%d", c.iface, n, canFrameSize)
	}
	return nil
}

func (c *Conn) Recv(timeout time.Duration) (canopen.Frame, error) {
	ms := int(timeout / time.Millisecond)
	if timeout < 0 {
		ms = -1
	} else if timeout > 0 && ms == 0 {
		ms = 1
	}
	poll := []unix.PollFd{{Fd: int32(c.fd), Events: unix.POLLIN}}
	n, err := unix.Poll(poll, ms)
	if err != nil {
		return canopen.Frame{}, fmt.Errorf("socketcan: poll %s: %w", c.iface, err)
	}
	if n == 0 {
		return canopen.Frame{}, ErrTimeout
	}
	var raw [canFrameSize]byte
	read, err := unix.Read(c.fd, raw[:])
	if err != nil {
		return canopen.Frame{}, fmt.Errorf("socketcan: read %s: %w", c.iface, err)
	}
	if read < canFrameSize {
		return canopen.Frame{}, fmt.Errorf("socketcan: short read on %s: %d/%d", c.iface, read, canFrameSize)
	}
	id := binary.LittleEndian.Uint32(raw[0:4])
	if id&(canEFFFlag|canRTRFlag|canERRFlag) != 0 {
		return canopen.Frame{}, fmt.Errorf("socketcan: unsupported frame flags in 0x%X", id)
	}
	dlc := raw[4]
	if dlc > 8 {
		return canopen.Frame{}, fmt.Errorf("socketcan: invalid DLC %d", dlc)
	}
	var data [8]byte
	copy(data[:], raw[8:16])
	return canopen.Frame{ID: id & canSFFMask, DLC: dlc, Data: data}, nil
}
