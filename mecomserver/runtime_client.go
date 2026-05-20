package mecomserver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

// RouterDeviceClientConfig describes one logical device accessed through the
// shared router runtime.
type RouterDeviceClientConfig struct {
	Address             byte
	RequestTimeout      time.Duration
	RouteSelection      RouteSelectionPolicy
	SupportsRingReadout bool
}

// ReadClient returns a routed MeCom client for one configured address.
func (rt *RouterRuntime) ReadClient(addr byte) mecom.ReadClient {
	return rt.NewReadClient(RouterDeviceClientConfig{Address: addr})
}

// NewReadClient returns a routed MeCom client for one logical device.
func (rt *RouterRuntime) NewReadClient(cfg RouterDeviceClientConfig) mecom.ReadClient {
	return &runtimeReadClient{
		rt:               rt,
		addr:             cfg.Address,
		timeout:          cfg.RequestTimeout,
		routeSelection:   cfg.RouteSelection,
		supportsRingRead: cfg.SupportsRingReadout,
	}
}

type runtimeReadClient struct {
	rt               *RouterRuntime
	addr             byte
	timeout          time.Duration
	routeSelection   RouteSelectionPolicy
	supportsRingRead bool

	mu  sync.Mutex
	seq uint16
}

func (c *runtimeReadClient) SupportsRingReadout() bool {
	return c != nil && c.supportsRingRead
}

func (c *runtimeReadClient) ReadFloat32(ctx context.Context, paramID, instance int) (float64, error) {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return mecom.BuildSingleGetFrame(int(c.addr), seq, paramID, instance), nil
	})
	if err != nil {
		return 0, err
	}
	return mecom.ParseSingleResponse(raw, mecom.DataTypeFloat32)
}

func (c *runtimeReadClient) ReadInt32(ctx context.Context, paramID, instance int) (int32, error) {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return mecom.BuildSingleGetFrame(int(c.addr), seq, paramID, instance), nil
	})
	if err != nil {
		return 0, err
	}
	v, err := mecom.ParseSingleResponse(raw, mecom.DataTypeInt32)
	return int32(v), err
}

func (c *runtimeReadClient) ReadBulk(ctx context.Context, params []mecom.Parameter) ([]float64, error) {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return mecom.BuildBulkGetFrame(int(c.addr), seq, params), nil
	})
	if err != nil {
		return nil, err
	}
	return mecom.ParseBulkResponse(raw, params)
}

func (c *runtimeReadClient) ConfigureRingCapture(ctx context.Context, captureID uint16, params []mecom.RingCaptureParameter) error {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return mecom.BuildRingCaptureConfigFrame(int(c.addr), seq, captureID, params)
	})
	if err != nil {
		return err
	}
	return mecom.ParseRingCaptureConfigResponse(raw)
}

func (c *runtimeReadClient) TriggerRingSync(ctx context.Context) error {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return mecom.BuildRingTriggerSyncFrame(int(c.addr), seq), nil
	})
	if err != nil {
		return err
	}
	return mecom.ParseWriteResponse(raw)
}

func (c *runtimeReadClient) ReadRingPointer(ctx context.Context) (uint32, error) {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return mecom.BuildRingPointerFrame(int(c.addr), seq), nil
	})
	if err != nil {
		return 0, err
	}
	return mecom.ParseRingPointerResponse(raw)
}

func (c *runtimeReadClient) ReadRingChunk(ctx context.Context, start uint32, maxBytes uint16) (mecom.RingReadResponse, error) {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return mecom.BuildRingReadFrame(int(c.addr), seq, start, maxBytes), nil
	})
	if err != nil {
		return mecom.RingReadResponse{}, err
	}
	return mecom.ParseRingReadResponse(raw)
}

func (c *runtimeReadClient) roundTrip(ctx context.Context, build func(seq uint16) ([]byte, error)) ([]byte, error) {
	if c == nil || c.rt == nil {
		return nil, fmt.Errorf("mecomserver: RouterRuntime required")
	}
	reqCtx := ctx
	if reqCtx == nil {
		reqCtx = context.Background()
	}
	seq := c.nextSequence()
	frame, err := build(seq)
	if err != nil {
		return nil, err
	}
	return c.rt.routeAddressedFrame(reqCtx, frame, c.addr, c.timeout, c.routeSelection, false, "runtime-read-client")
}

func (c *runtimeReadClient) nextSequence() uint16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	if c.seq == 0 {
		c.seq = 1
	}
	return c.seq
}
