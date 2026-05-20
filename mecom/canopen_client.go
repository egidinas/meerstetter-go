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

// WriteFloat32 writes a float32 value to the mapped CANopen object via an
// expedited SDO download. The parameter must be present in the MeCom↔CANopen
// mapping and marked writable.
func (c *CANopenClient) WriteFloat32(ctx context.Context, paramID, instance int, value float32) error {
	object, ok := canopenSDOObjectForMeCom(paramID, instance)
	if !ok {
		return fmt.Errorf("%w: parameter %d instance %d", ErrUnknownParameter, paramID, instance)
	}
	if !object.writable {
		return fmt.Errorf("%w: parameter %d instance %d", ErrParameterReadOnly, paramID, instance)
	}
	if object.kind != DataTypeFloat32 {
		return fmt.Errorf("%w: parameter %d instance %d is not float32", ErrInvalidArgument, paramID, instance)
	}
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], math.Float32bits(value))
	return c.writeSDO(ctx, object, buf[:])
}

// WriteInt32 writes a 32-bit signed integer value via an expedited SDO download.
func (c *CANopenClient) WriteInt32(ctx context.Context, paramID, instance int, value int32) error {
	object, ok := canopenSDOObjectForMeCom(paramID, instance)
	if !ok {
		return fmt.Errorf("%w: parameter %d instance %d", ErrUnknownParameter, paramID, instance)
	}
	if !object.writable {
		return fmt.Errorf("%w: parameter %d instance %d", ErrParameterReadOnly, paramID, instance)
	}
	if object.kind != DataTypeInt32 {
		return fmt.Errorf("%w: parameter %d instance %d is not int32", ErrInvalidArgument, paramID, instance)
	}
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(value))
	return c.writeSDO(ctx, object, buf[:])
}

func (c *CANopenClient) writeSDO(ctx context.Context, object canopenSDOObject, value []byte) error {
	req, err := canopen.SDODownloadExpeditedRequest(c.node, object.index, object.subIndex, value)
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
			return fmt.Errorf("%w: write 0x%04X:%02X", ErrTimeout, object.index, object.subIndex)
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
				if abort.Index == object.index && abort.SubIndex == object.subIndex {
					return errors.Join(ErrWriteRejected, err)
				}
			}
			continue
		}
		if resp.Index != object.index || resp.SubIndex != object.subIndex {
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
	object, ok := canopenSDOObjectForMeCom(paramID, instance)
	if !ok {
		return math.NaN(), fmt.Errorf("%w: parameter %d instance %d", ErrUnknownParameter, paramID, instance)
	}
	req, err := canopen.SDOUploadRequest(c.node, object.index, object.subIndex)
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
			return math.NaN(), fmt.Errorf("%w: read 0x%04X:%02X", ErrTimeout, object.index, object.subIndex)
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
				if abort.Index == object.index && abort.SubIndex == object.subIndex {
					return math.NaN(), err
				}
			}
			continue
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
	writable bool
}

func canopenSDOObjectForMeCom(paramID, instance int) (canopenSDOObject, bool) {
	if instance <= 0 || instance > 0xff {
		return canopenSDOObject{}, false
	}
	sub := byte(instance)
	switch paramID {
	case 102: // system serial number (read-only int32)
		if instance != 1 {
			return canopenSDOObject{}, false
		}
		return canopenSDOObject{index: 0x2002, subIndex: 0x01, kind: DataTypeInt32}, true
	case 103: // firmware version integer (read-only int32)
		if instance != 1 {
			return canopenSDOObject{}, false
		}
		return canopenSDOObject{index: 0x2003, subIndex: 0x01, kind: DataTypeInt32}, true
	case 104: // device status (read-only int32)
		if instance != 1 {
			return canopenSDOObject{}, false
		}
		return canopenSDOObject{index: 0x2004, subIndex: 0x01, kind: DataTypeInt32}, true
	case 105: // error number (read-only int32)
		if instance != 1 {
			return canopenSDOObject{}, false
		}
		return canopenSDOObject{index: 0x2005, subIndex: 0x01, kind: DataTypeInt32}, true
	case 1000: // object temperature (sensor, read-only)
		return canopenSDOObject{index: 0x2100, subIndex: sub, kind: DataTypeFloat32}, true
	case 1001: // sink temperature (sensor, read-only)
		return canopenSDOObject{index: 0x2101, subIndex: sub, kind: DataTypeFloat32}, true
	case 1020: // output stage actual current (read-only)
		return canopenSDOObject{index: 0x2120, subIndex: sub, kind: DataTypeFloat32}, true
	case 1021: // output stage actual voltage (read-only)
		return canopenSDOObject{index: 0x2121, subIndex: sub, kind: DataTypeFloat32}, true
	case 1022: // output stage actual power (read-only)
		return canopenSDOObject{index: 0x2122, subIndex: sub, kind: DataTypeFloat32}, true
	case 2010: // output stage enable (writable int32)
		return canopenSDOObject{index: 0x2410, subIndex: sub, kind: DataTypeInt32, writable: true}, true
	case 2020: // output stage fixed set current (writable float32)
		return canopenSDOObject{index: 0x2420, subIndex: sub, kind: DataTypeFloat32, writable: true}, true
	case 2021: // output stage fixed set voltage (writable float32)
		return canopenSDOObject{index: 0x2421, subIndex: sub, kind: DataTypeFloat32, writable: true}, true
	case 2030: // output stage current limitation (writable float32)
		return canopenSDOObject{index: 0x2430, subIndex: sub, kind: DataTypeFloat32, writable: true}, true
	case 2031: // output stage voltage limitation (writable float32)
		return canopenSDOObject{index: 0x2431, subIndex: sub, kind: DataTypeFloat32, writable: true}, true
	case 2032: // output stage current error threshold (writable float32)
		return canopenSDOObject{index: 0x2432, subIndex: sub, kind: DataTypeFloat32, writable: true}, true
	case 2033: // output stage voltage error threshold (writable float32)
		return canopenSDOObject{index: 0x2433, subIndex: sub, kind: DataTypeFloat32, writable: true}, true
	case 2040: // operating mode (writable int32)
		return canopenSDOObject{index: 0x2440, subIndex: sub, kind: DataTypeInt32, writable: true}, true
	case 3000: // nominal target temperature (writable float32 setpoint)
		return canopenSDOObject{index: 0x2600, subIndex: sub, kind: DataTypeFloat32, writable: true}, true
	case 53120: // cascade temperature controller enable (writable int32)
		return canopenSDOObject{index: 0x4420, subIndex: sub, kind: DataTypeInt32, writable: true}, true
	case 53121: // cascade current temperature selection (writable int32)
		return canopenSDOObject{index: 0x4421, subIndex: sub, kind: DataTypeInt32, writable: true}, true
	case 53122: // cascade sync run-with channel (writable int32)
		return canopenSDOObject{index: 0x4422, subIndex: sub, kind: DataTypeInt32, writable: true}, true
	case 53123: // cascade target temperature (writable float32)
		return canopenSDOObject{index: 0x4423, subIndex: sub, kind: DataTypeFloat32, writable: true}, true
	default:
		return canopenSDOObject{}, false
	}
}
