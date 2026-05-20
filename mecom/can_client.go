package mecom

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/egidinas/meerstetter-go/canopen"
)

// CANTransceiver is the small SocketCAN/Kvaser/bridge boundary needed by the
// binary MeCom-over-CAN client.
type CANTransceiver interface {
	Send(canopen.Frame) error
	Recv(time.Duration) (canopen.Frame, error)
}

// CANClient sends binary MeCom read requests over an 11-bit CAN transport.
// The default request and response identifiers follow the MeCom convention
// used by TEC controllers: 0x300+address for requests and 0x400+address for
// replies.
type CANClient struct {
	rw      CANTransceiver
	address byte
	timeout time.Duration
	mu      sync.Mutex
	seq     uint16
}

// NewCANClient creates a binary MeCom client over CAN.
func NewCANClient(rw CANTransceiver, cfg ClientConfig) *CANClient {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	return &CANClient{rw: rw, address: cfg.Address, timeout: timeout}
}

func (c *CANClient) SupportsRingReadout() bool { return false }

func (c *CANClient) ReadFloat32(ctx context.Context, paramID, instance int) (float64, error) {
	return c.readNumeric(ctx, paramID, instance, DataTypeFloat32)
}

func (c *CANClient) ReadInt32(ctx context.Context, paramID, instance int) (int32, error) {
	v, err := c.readNumeric(ctx, paramID, instance, DataTypeInt32)
	return int32(v), err
}

// ReadBulk uses binary single-value reads as a conservative compatibility
// fallback until the exact binary multi-parameter CAN framing is proven live.
func (c *CANClient) ReadBulk(ctx context.Context, params []Parameter) ([]float64, error) {
	if err := validateParameterList(params); err != nil {
		return nil, err
	}
	values := make([]float64, 0, len(params))
	for _, param := range params {
		value, err := c.readNumeric(ctx, param.ID, param.Instance, param.Type)
		if err != nil {
			return values, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (c *CANClient) ConfigureRingCapture(context.Context, uint16, []RingCaptureParameter) error {
	return ErrTransportNotSupported
}

func (c *CANClient) TriggerRingSync(context.Context) error {
	return ErrTransportNotSupported
}

func (c *CANClient) ReadRingPointer(context.Context) (uint32, error) {
	return 0, ErrTransportNotSupported
}

func (c *CANClient) ReadRingChunk(context.Context, uint32, uint16) (RingReadResponse, error) {
	return RingReadResponse{}, ErrTransportNotSupported
}

func (c *CANClient) readNumeric(ctx context.Context, paramID, instance int, dataType DataType) (float64, error) {
	if err := validateParameterAddress(paramID, instance); err != nil {
		return 0, err
	}
	if dataType == "" {
		dataType = DataTypeFloat32
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	seq := c.nextSeq()
	payload := BuildBinarySingleGetFrame(int(c.address), seq, paramID, instance)
	var data [8]byte
	copy(data[:], payload)
	reqID := uint32(0x300) + uint32(c.address)
	respID := uint32(0x400) + uint32(c.address)
	if err := c.rw.Send(canopen.Frame{ID: reqID, DLC: uint8(len(payload)), Data: data}); err != nil {
		return 0, err
	}
	deadline := time.Now().Add(c.timeout)
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		left := time.Until(deadline)
		if left <= 0 {
			return 0, fmt.Errorf("%w: CAN response after %s", ErrTimeout, c.timeout)
		}
		f, err := c.rw.Recv(left)
		if err != nil {
			return 0, err
		}
		if f.ID == reqID {
			continue
		}
		if f.ID != respID {
			continue
		}
		if !BinaryResponseMatchesRequest(f, c.address, seq, BinaryCmdQueryValue) {
			continue
		}
		return DecodeBinaryCANFrame(f, dataType)
	}
}

func (c *CANClient) nextSeq() uint16 {
	c.seq++
	if c.seq == 0 {
		c.seq = 1
	}
	return c.seq
}
