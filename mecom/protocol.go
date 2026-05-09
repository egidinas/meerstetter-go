package mecom

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

const FrameTerminator byte = '\r'

// DataType controls how a 32-bit MeCom payload is converted.
type DataType string

const (
	DataTypeFloat32 DataType = "float32"
	DataTypeInt32   DataType = "int32"
)

// Parameter describes the minimum metadata needed to encode and decode values.
type Parameter struct {
	ID       int
	Instance int
	Name     string
	Unit     string
	Type     DataType
	Writable bool
}

var errorsByCode = map[int]string{
	1: "CMD_NOT_AVAILABLE",
	2: "DEVICE_BUSY",
	3: "GENERAL_COM",
	4: "FORMAT",
	5: "PAR_NOT_AVAILABLE",
	6: "PAR_NOT_WRITABLE",
	7: "PAR_OUT_OF_RANGE",
	8: "PAR_INST_NOT_AVAILABLE",
}

// CRC16 implements the CRC-16-CCITT algorithm used by MeCom frames.
func CRC16(data []byte) uint16 {
	var crc uint16
	const poly uint16 = 0x1021
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ poly
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// BuildSingleGetFrame constructs a ?VR frame for reading one parameter.
func BuildSingleGetFrame(addr int, seq uint16, paramID, instance int) []byte {
	body := fmt.Sprintf("?VR%04X%02X", paramID, instance)
	return appendCRC(addr, seq, body)
}

// BuildBulkGetFrame constructs a ?VX frame for reading multiple parameters.
func BuildBulkGetFrame(addr int, seq uint16, params []Parameter) []byte {
	var body strings.Builder
	fmt.Fprintf(&body, "?VX%02X", len(params))
	for _, p := range params {
		fmt.Fprintf(&body, "%04X%02X", p.ID, p.Instance)
	}
	return appendCRC(addr, seq, body.String())
}

// BuildWriteFloat32Frame constructs a VS frame for writing a float32 parameter.
func BuildWriteFloat32Frame(addr int, seq uint16, paramID, instance int, value float32) []byte {
	return buildWriteFrame(addr, seq, paramID, instance, encodeFloat32(value))
}

// BuildWriteInt32Frame constructs a VS frame for writing an int32 parameter.
func BuildWriteInt32Frame(addr int, seq uint16, paramID, instance int, value int32) []byte {
	return buildWriteFrame(addr, seq, paramID, instance, encodeInt32(value))
}

// BuildWriteStringFrame constructs a VS frame for writing a string parameter.
func BuildWriteStringFrame(addr int, seq uint16, paramID, instance int, value string) []byte {
	return buildWriteFrame(addr, seq, paramID, instance, strings.ToUpper(hex.EncodeToString([]byte(value))))
}

func buildWriteFrame(addr int, seq uint16, paramID, instance int, valueHex string) []byte {
	body := fmt.Sprintf("VS%04X%02X%s", paramID, instance, valueHex)
	return appendCRC(addr, seq, body)
}

func appendCRC(addr int, seq uint16, body string) []byte {
	prefix := fmt.Sprintf("#%02X%04X%s", addr, seq, body)
	return []byte(fmt.Sprintf("%s%04X\r", prefix, CRC16([]byte(prefix))))
}

func encodeInt32(v int32) string {
	return fmt.Sprintf("%08X", uint32(v))
}

func encodeFloat32(v float32) string {
	return fmt.Sprintf("%08X", math.Float32bits(v))
}

// SplitFrames divides a raw buffer into CR-terminated MeCom frames.
func SplitFrames(data []byte) [][]byte {
	var frames [][]byte
	start := 0
	for i, b := range data {
		if b == FrameTerminator {
			frames = append(frames, data[start:i+1])
			start = i + 1
		}
	}
	return frames
}

// ParseSingleResponse decodes a single numeric read response.
func ParseSingleResponse(raw []byte, dataType DataType) (float64, error) {
	payload, err := parsePayload(raw)
	if err != nil {
		return 0, err
	}
	if len(payload) < 8 {
		return 0, fmt.Errorf("mecom: invalid single response payload %q", payload)
	}
	return DecodeNumeric(payload[:8], dataType)
}

// ParseBulkResponse decodes numeric values from a bulk response.
func ParseBulkResponse(raw []byte, params []Parameter) ([]float64, error) {
	payload, err := parsePayload(raw)
	if err != nil {
		return nil, err
	}
	payload = hexOnly(payload)
	values := make([]float64, len(params))
	for i := range values {
		values[i] = math.NaN()
	}
	for i, p := range params {
		if len(payload) < (i+1)*8 {
			continue
		}
		v, err := DecodeNumeric(payload[i*8:(i+1)*8], p.Type)
		if err == nil {
			values[i] = v
		}
	}
	return values, nil
}

// ParseWriteResponse validates a write acknowledgement and returns NACKs as errors.
func ParseWriteResponse(raw []byte) error {
	_, err := parsePayload(raw)
	return err
}

// DecodeNumeric decodes a MeCom 8-hex-character numeric payload.
func DecodeNumeric(chunk string, dataType DataType) (float64, error) {
	bits, err := strconv.ParseUint(chunk, 16, 32)
	if err != nil {
		return 0, err
	}
	if dataType == DataTypeInt32 {
		return float64(int32(uint32(bits))), nil
	}
	return float64(math.Float32frombits(uint32(bits))), nil
}

func parsePayload(raw []byte) (string, error) {
	frame := bytes.TrimSuffix(raw, []byte{FrameTerminator})
	if len(frame) < 7 || frame[0] != '!' {
		return "", fmt.Errorf("mecom: invalid response %q", string(frame))
	}
	payloadStart := 7
	if len(frame) > 7 {
		switch frame[7] {
		case '+':
			payloadStart = 8
		case '-':
			payloadEnd := len(frame) - 4
			if payloadEnd <= 8 {
				return "", fmt.Errorf("mecom: invalid nack %q", string(frame))
			}
			code, err := strconv.ParseUint(string(frame[8:payloadEnd]), 16, 8)
			if err != nil {
				return "", fmt.Errorf("mecom: invalid nack code: %w", err)
			}
			if name, ok := errorsByCode[int(code)]; ok {
				return "", fmt.Errorf("mecom: nack %02X (%s)", code, name)
			}
			return "", fmt.Errorf("mecom: nack %02X", code)
		}
	}
	payloadEnd := len(frame) - 4
	if payloadEnd <= payloadStart {
		return "", nil
	}
	return string(frame[payloadStart:payloadEnd]), nil
}

func hexOnly(v string) string {
	return strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') || (r >= 'a' && r <= 'f') {
			return r
		}
		return -1
	}, v)
}

// ClientConfig configures a synchronous MeCom client.
type ClientConfig struct {
	Address byte
	Timeout time.Duration
}

// Client sends one request at a time over a shared io.ReadWriter.
type Client struct {
	rw      io.ReadWriter
	address byte
	timeout time.Duration
	mu      sync.Mutex
	seq     uint16
}

// NewClient creates a MeCom client over TCP, serial, or any test transport.
func NewClient(rw io.ReadWriter, cfg ClientConfig) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	return &Client{rw: rw, address: cfg.Address, timeout: timeout}
}

// ReadFloat32 reads one float32 parameter.
func (c *Client) ReadFloat32(ctx context.Context, paramID, instance int) (float64, error) {
	return c.readNumeric(ctx, paramID, instance, DataTypeFloat32)
}

// ReadInt32 reads one int32 parameter.
func (c *Client) ReadInt32(ctx context.Context, paramID, instance int) (int32, error) {
	v, err := c.readNumeric(ctx, paramID, instance, DataTypeInt32)
	return int32(v), err
}

func (c *Client) readNumeric(ctx context.Context, paramID, instance int, dataType DataType) (float64, error) {
	raw, err := c.roundTrip(ctx, BuildSingleGetFrame(int(c.address), c.nextSeq(), paramID, instance))
	if err != nil {
		return 0, err
	}
	return ParseSingleResponse(raw, dataType)
}

func (c *Client) roundTrip(ctx context.Context, frame []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := c.rw.Write(frame); err != nil {
		return nil, err
	}
	done := make(chan struct {
		raw []byte
		err error
	}, 1)
	go func() {
		raw, err := readFrame(c.rw)
		done <- struct {
			raw []byte
			err error
		}{raw: raw, err: err}
	}()
	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("mecom: response timeout after %s", c.timeout)
	case result := <-done:
		return result.raw, result.err
	}
}

func (c *Client) nextSeq() uint16 {
	c.seq++
	if c.seq == 0 {
		c.seq = 1
	}
	return c.seq
}

func readFrame(r io.Reader) ([]byte, error) {
	var out []byte
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = append(out, buf[0])
			if buf[0] == FrameTerminator {
				return out, nil
			}
		}
		if err != nil {
			return out, err
		}
	}
}
