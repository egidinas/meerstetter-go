package mecom

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/egidinas/meerstetter-go/canopen"
)

var ErrCANopenObjectNotMapped = errors.New("mecom: CANopen object mapping not available")

// CANopenClient reads the TEC controller's CANopen SDO object dictionary while
// presenting the same read interface as the MeCom polling scheduler.
type CANopenClient struct {
	rw      CANTransceiver
	node    byte
	timeout time.Duration
	mu      sync.Mutex
}

func NewCANopenClient(rw CANTransceiver, cfg ClientConfig) *CANopenClient {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	return &CANopenClient{
		rw:      rw,
		node:    cfg.Address,
		timeout: timeout,
	}
}

func (c *CANopenClient) SupportsRingReadout() bool { return false }

func (c *CANopenClient) ReadFloat32(ctx context.Context, paramID, instance int) (float64, error) {
	return c.readNumeric(ctx, paramID, instance)
}

func (c *CANopenClient) ReadInt32(ctx context.Context, paramID, instance int) (int32, error) {
	value, err := c.readNumeric(ctx, paramID, instance)
	return int32(value), err
}

func (c *CANopenClient) ReadBulk(ctx context.Context, params []Parameter) ([]float64, error) {
	values := make([]float64, 0, len(params))
	for _, param := range params {
		value, err := c.readNumeric(ctx, param.ID, param.Instance)
		if err != nil {
			values = append(values, math.NaN())
			continue
		}
		values = append(values, value)
	}
	return values, nil
}

func (c *CANopenClient) ConfigureRingCapture(context.Context, uint16, []RingCaptureParameter) error {
	return ErrCANopenObjectNotMapped
}

func (c *CANopenClient) TriggerRingSync(context.Context) error {
	return ErrCANopenObjectNotMapped
}

func (c *CANopenClient) ReadRingPointer(context.Context) (uint32, error) {
	return 0, ErrCANopenObjectNotMapped
}

func (c *CANopenClient) ReadRingBuffer(context.Context, uint32, uint16) (RingReadResponse, error) {
	return RingReadResponse{}, ErrCANopenObjectNotMapped
}

func (c *CANopenClient) readNumeric(ctx context.Context, paramID, instance int) (float64, error) {
	object, ok := canopenSDOObjectForMeCom(paramID, instance)
	if !ok {
		return math.NaN(), fmt.Errorf("%w: parameter %d instance %d", ErrCANopenObjectNotMapped, paramID, instance)
	}
	req := canopen.SDOUploadRequest(c.node, object.index, object.subIndex)

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.rw.Send(req); err != nil {
		return math.NaN(), err
	}
	deadline := time.Now().Add(c.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	for {
		wait := time.Until(deadline)
		if wait <= 0 {
			return math.NaN(), context.DeadlineExceeded
		}
		if err := ctx.Err(); err != nil {
			return math.NaN(), err
		}
		frame, err := c.rw.Recv(wait)
		if err != nil {
			return math.NaN(), err
		}
		if frame.ID != 0x580+uint32(c.node) {
			continue
		}
		resp, err := canopen.ParseSDOUploadResponse(frame)
		if err != nil {
			return math.NaN(), err
		}
		if resp.Index != object.index || resp.SubIndex != object.subIndex {
			continue
		}
		switch object.kind {
		case DataTypeInt32:
			value, err := resp.Int32()
			return float64(value), err
		default:
			value, err := resp.Float32()
			return float64(value), err
		}
	}
}

type canopenSDOObject struct {
	index    uint16
	subIndex byte
	kind     DataType
}

func canopenSDOObjectForMeCom(paramID, instance int) (canopenSDOObject, bool) {
	if instance <= 0 {
		instance = 1
	}
	if instance > 0xff {
		return canopenSDOObject{}, false
	}
	sub := byte(instance)
	switch paramID {
	case 1000:
		return canopenSDOObject{index: 0x2100, subIndex: sub, kind: DataTypeFloat32}, true
	case 1001:
		return canopenSDOObject{index: 0x2101, subIndex: sub, kind: DataTypeFloat32}, true
	case 3000:
		return canopenSDOObject{index: 0x2600, subIndex: sub, kind: DataTypeFloat32}, true
	case 1020:
		return canopenSDOObject{index: 0x2420, subIndex: sub, kind: DataTypeFloat32}, true
	case 1021:
		return canopenSDOObject{index: 0x2421, subIndex: sub, kind: DataTypeFloat32}, true
	default:
		return canopenSDOObject{}, false
	}
}
