package mecom

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/egidinas/meerstetter-go/canopen"
)

// ErrCANopenObjectNotMapped is retained for backward compatibility. Prefer
// ErrUnknownParameter, which it wraps.
var ErrCANopenObjectNotMapped = fmt.Errorf("%w: CANopen object mapping not available", ErrUnknownParameter)

// CANopenClient reads the controller's CANopen SDO object dictionary while
// presenting the same read interface as the MeCom polling scheduler.
type CANopenClient struct {
	rw      CANTransceiver
	node    byte
	timeout time.Duration
	mu      sync.Mutex
	sdoMap  *CANopenSDOMap
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
		sdoMap:  cfg.SDOMap,
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

// WriteFloat32 writes a float32 value to the mapped CANopen object via an
// expedited SDO download. The parameter must be present in the MeCom↔CANopen
// mapping and marked writable.
func (c *CANopenClient) WriteFloat32(ctx context.Context, paramID, instance int, value float32) error {
	object, ok := c.canopenSDOObjectForMeCom(paramID, instance)
	if !ok {
		return fmt.Errorf("%w: parameter %d instance %d", ErrUnknownParameter, paramID, instance)
	}
	if !object.Writable {
		return fmt.Errorf("%w: parameter %d instance %d", ErrParameterReadOnly, paramID, instance)
	}
	if object.Kind != DataTypeFloat32 {
		return fmt.Errorf("%w: parameter %d instance %d is not float32", ErrInvalidArgument, paramID, instance)
	}
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], math.Float32bits(value))
	return c.writeSDO(ctx, object, buf[:])
}

// WriteInt32 writes a 32-bit signed integer value via an expedited SDO download.
func (c *CANopenClient) WriteInt32(ctx context.Context, paramID, instance int, value int32) error {
	object, ok := c.canopenSDOObjectForMeCom(paramID, instance)
	if !ok {
		return fmt.Errorf("%w: parameter %d instance %d", ErrUnknownParameter, paramID, instance)
	}
	if !object.Writable {
		return fmt.Errorf("%w: parameter %d instance %d", ErrParameterReadOnly, paramID, instance)
	}
	if object.Kind != DataTypeInt32 {
		return fmt.Errorf("%w: parameter %d instance %d is not int32", ErrInvalidArgument, paramID, instance)
	}
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(value))
	return c.writeSDO(ctx, object, buf[:])
}

func (c *CANopenClient) writeSDO(ctx context.Context, object CANopenSDOObject, value []byte) error {
	req, err := canopen.SDODownloadExpeditedRequest(c.node, object.Index, object.SubIndex, value)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.rw.Send(req); err != nil {
		return err
	}
	deadline := time.Now().Add(c.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	for {
		wait := time.Until(deadline)
		if wait <= 0 {
			return fmt.Errorf("%w: write 0x%04X:%02X", ErrTimeout, object.Index, object.SubIndex)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, err := c.rw.Recv(wait)
		if err != nil {
			return err
		}
		if frame.ID != 0x580+uint32(c.node) {
			continue
		}
		resp, err := canopen.ParseSDODownloadResponse(frame)
		if err != nil {
			var abort canopen.SDOAbortError
			if errors.As(err, &abort) {
				if abort.Index == object.Index && abort.SubIndex == object.SubIndex {
					return errors.Join(ErrWriteRejected, err)
				}
			}
			continue
		}
		if resp.Index != object.Index || resp.SubIndex != object.SubIndex {
			continue
		}
		return nil
	}
}

func (c *CANopenClient) ConfigureRingCapture(context.Context, uint16, []RingCaptureParameter) error {
	return ErrTransportNotSupported
}

func (c *CANopenClient) TriggerRingSync(context.Context) error {
	return ErrTransportNotSupported
}

func (c *CANopenClient) ReadRingPointer(context.Context) (uint32, error) {
	return 0, ErrTransportNotSupported
}

func (c *CANopenClient) ReadRingChunk(context.Context, uint32, uint16) (RingReadResponse, error) {
	return RingReadResponse{}, ErrTransportNotSupported
}

func (c *CANopenClient) readNumeric(ctx context.Context, paramID, instance int) (float64, error) {
	object, ok := c.canopenSDOObjectForMeCom(paramID, instance)
	if !ok {
		return math.NaN(), fmt.Errorf("%w: parameter %d instance %d", ErrUnknownParameter, paramID, instance)
	}
	req, err := canopen.SDOUploadRequest(c.node, object.Index, object.SubIndex)
	if err != nil {
		return math.NaN(), err
	}

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
			return math.NaN(), fmt.Errorf("%w: read 0x%04X:%02X", ErrTimeout, object.Index, object.SubIndex)
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
			var abort canopen.SDOAbortError
			if errors.As(err, &abort) {
				if abort.Index == object.Index && abort.SubIndex == object.SubIndex {
					return math.NaN(), err
				}
			}
			continue
		}
		if resp.Index != object.Index || resp.SubIndex != object.SubIndex {
			continue
		}
		switch object.Kind {
		case DataTypeInt32:
			value, err := resp.Int32()
			return float64(value), err
		default:
			value, err := resp.Float32()
			return float64(value), err
		}
	}
}

func (c *CANopenClient) canopenSDOObjectForMeCom(paramID, instance int) (CANopenSDOObject, bool) {
	if c.sdoMap != nil {
		return c.sdoMap.ObjectForMeCom(paramID, instance)
	}
	return defaultCANopenSDOMap.ObjectForMeCom(paramID, instance)
}

func (c *CANopenClient) ParameterType(id int) DataType {
	var m CANopenSDOMap
	if c.sdoMap != nil {
		m = *c.sdoMap
	} else {
		m = defaultCANopenSDOMap
	}
	if typ, ok := m.ParameterType(id); ok {
		return typ
	}
	return ""
}
