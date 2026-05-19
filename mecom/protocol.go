package mecom

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egidinas/meerstetter-go/canopen"
)

const FrameTerminator byte = '\r'

// DataType controls how a 32-bit MeCom payload is converted.
type DataType string

const (
	DataTypeFloat32 DataType = "float32"
	DataTypeInt32   DataType = "int32"
	DataTypeLatin1  DataType = "latin1"
)

// Parameter describes the minimum metadata needed to encode and decode values.
type Parameter struct {
	ID       int
	Instance int
	Name     string
	Unit     string
	Type     DataType
	Writable bool
	Role     string
	Kind     string
}

const (
	RingStatusAllDataRead byte = 0
	RingStatusHasMoreData byte = 1
	RingStatusOverlap     byte = 2

	maxRingCaptureParameters = 16
)

// RingCaptureParameter configures one CRTVStream capture slot.
type RingCaptureParameter struct {
	Parameter
	// InhibitTime10us is encoded in 10 microsecond steps by the MeCom protocol.
	InhibitTime10us uint16
}

// RingReadResponse is the decoded response to a CRTVStream ring-buffer read.
type RingReadResponse struct {
	BytesAdded uint16
	Status     byte
	Data       []byte
}

// RingSample is one decoded sample from a CRTVStream data frame.
type RingSample struct {
	ConfigIndex int
	Type        DataType
	Value       float64
}

// RingFrame is one decoded CRTVStream normal or sync frame.
type RingFrame struct {
	Sync               bool
	HasCaptureConfigID bool
	CaptureConfigID    uint16
	Timestamp10us      uint16
	Samples            []RingSample
	Raw                []byte
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

// ── ASCII MeCom (Serial/TCP) ──────────────────────────────────────────────────

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

// BuildRingPointerFrame constructs a CRTVStream current-ring-pointer request.
func BuildRingPointerFrame(addr int, seq uint16) []byte {
	return appendCRC(addr, seq, "?RS0000")
}

// BuildRingReadFrame constructs a CRTVStream ring-buffer read request.
func BuildRingReadFrame(addr int, seq uint16, start uint32, maxBytes uint16) []byte {
	body := fmt.Sprintf("?RS0001%08X%04X", start, maxBytes)
	return appendCRC(addr, seq, body)
}

// BuildRingCaptureConfigFrame constructs a volatile CRTVStream capture config request.
func BuildRingCaptureConfigFrame(addr int, seq uint16, captureID uint16, params []RingCaptureParameter) ([]byte, error) {
	if len(params) > maxRingCaptureParameters {
		return nil, fmt.Errorf("mecom: ring capture supports at most %d parameters, got %d", maxRingCaptureParameters, len(params))
	}
	var body strings.Builder
	fmt.Fprintf(&body, "?RS0002%04X%02X", captureID, len(params))
	for _, p := range params {
		if p.ID < 0 || p.ID > 0xFFFF {
			return nil, fmt.Errorf("mecom: invalid parameter id %d", p.ID)
		}
		if p.Instance < 0 || p.Instance > 0xFF {
			return nil, fmt.Errorf("mecom: invalid parameter instance %d", p.Instance)
		}
		fmt.Fprintf(&body, "%04X%02X%04X", p.ID, p.Instance, p.InhibitTime10us)
	}
	return appendCRC(addr, seq, body.String()), nil
}

// BuildRingTriggerSyncFrame constructs a CRTVStream trigger-sync request.
func BuildRingTriggerSyncFrame(addr int, seq uint16) []byte {
	return appendCRC(addr, seq, "?RS0003")
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

// BuildSetBigDataStringFrame constructs a VB frame for writing one LATIN1 big-data package.
func BuildSetBigDataStringFrame(addr int, seq uint16, paramID, instance int, writeStart uint32, value string, isLast bool) ([]byte, error) {
	data, err := encodeLatin1String(value, true)
	if err != nil {
		return nil, err
	}
	if len(data) > 0xFFFF {
		return nil, fmt.Errorf("mecom: big-data string package too long: %d elements", len(data))
	}
	last := 0
	if isLast {
		last = 1
	}
	body := fmt.Sprintf(
		"VB%04X%02X%08X%04X%02X%s",
		paramID,
		instance,
		writeStart,
		len(data),
		last,
		strings.ToUpper(hex.EncodeToString(data)),
	)
	return appendCRC(addr, seq, body), nil
}

// BuildSaveToFlashFrame constructs an SP frame for explicitly saving all parameter values to flash.
func BuildSaveToFlashFrame(addr int, seq uint16) []byte {
	return appendCRC(addr, seq, "SP")
}

// BuildResetFrame constructs an RS frame for resetting the device.
func BuildResetFrame(addr int, seq uint16) []byte {
	return appendCRC(addr, seq, "RS")
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

func encodeLatin1String(value string, zeroTerminated bool) ([]byte, error) {
	out := make([]byte, 0, len(value)+1)
	for _, r := range value {
		if r == 0 {
			return nil, fmt.Errorf("mecom: LATIN1 string contains embedded NUL")
		}
		if r > 0xFF {
			return nil, fmt.Errorf("mecom: rune %q is outside LATIN1", r)
		}
		out = append(out, byte(r))
	}
	if zeroTerminated {
		out = append(out, 0)
	}
	return out, nil
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

// ParseRingPointerResponse decodes the current CRTVStream ring-buffer pointer.
func ParseRingPointerResponse(raw []byte) (uint32, error) {
	payload, err := parsePayload(raw)
	if err != nil {
		return 0, err
	}
	payload = hexOnly(payload)
	if len(payload) < 8 {
		return 0, fmt.Errorf("mecom: invalid ring pointer payload %q", payload)
	}
	v, err := strconv.ParseUint(payload[:8], 16, 32)
	if err != nil {
		return 0, err
	}
	return uint32(v), nil
}

// ParseRingReadResponse decodes a CRTVStream ring-buffer read response.
func ParseRingReadResponse(raw []byte) (RingReadResponse, error) {
	payload, err := parsePayload(raw)
	if err != nil {
		return RingReadResponse{}, err
	}
	payload = hexOnly(payload)
	if len(payload) < 6 {
		return RingReadResponse{}, fmt.Errorf("mecom: invalid ring read payload %q", payload)
	}
	bytesAdded, err := strconv.ParseUint(payload[:4], 16, 16)
	if err != nil {
		return RingReadResponse{}, err
	}
	status, err := strconv.ParseUint(payload[4:6], 16, 8)
	if err != nil {
		return RingReadResponse{}, err
	}
	data, err := hex.DecodeString(payload[6:])
	if err != nil {
		return RingReadResponse{}, err
	}
	return RingReadResponse{BytesAdded: uint16(bytesAdded), Status: byte(status), Data: data}, nil
}

// ParseRingCaptureConfigResponse validates a CRTVStream capture config acknowledgement.
func ParseRingCaptureConfigResponse(raw []byte) error {
	payload, err := parsePayload(raw)
	if err != nil {
		return err
	}
	payload = hexOnly(payload)
	if payload == "" || payload == "00" {
		return nil
	}
	code, err := strconv.ParseUint(payload, 16, 8)
	if err != nil {
		return fmt.Errorf("mecom: invalid ring capture config response %q", payload)
	}
	if code == 0 {
		return nil
	}
	if name, ok := errorsByCode[int(code)]; ok {
		return fmt.Errorf("mecom: ring capture config error %02X (%s)", code, name)
	}
	return fmt.Errorf("mecom: ring capture config error %02X", code)
}

// ParseRingFrames decodes complete CRTVStream frames and returns any trailing partial bytes.
func ParseRingFrames(data []byte, config []RingCaptureParameter) ([]RingFrame, []byte, error) {
	var frames []RingFrame
	offset := 0
	for {
		start := findRingStart(data[offset:])
		if start < 0 {
			return frames, nil, nil
		}
		start += offset
		end := findRingEnd(data[start+2:])
		if end < 0 {
			return frames, append([]byte(nil), data[start:]...), nil
		}
		end += start + 2
		unescaped, err := unescapeRingPayload(data[start+2 : end])
		if err != nil {
			return frames, nil, err
		}
		frame, err := decodeRingFrame(data[start+1], unescaped, data[start:end+2], config)
		if err != nil {
			return frames, nil, err
		}
		frames = append(frames, frame)
		offset = end + 2
		if offset >= len(data) {
			return frames, nil, nil
		}
	}
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

func findRingStart(data []byte) int {
	for i := 0; i+1 < len(data); i++ {
		if data[i] == 0x88 && (data[i+1] == 0x00 || data[i+1] == 0x01) {
			return i
		}
	}
	return -1
}

func findRingEnd(data []byte) int {
	for i := 0; i+1 < len(data); i++ {
		if data[i] != 0x88 {
			continue
		}
		switch data[i+1] {
		case 0x88:
			i++
		case 0x10:
			return i
		}
	}
	return -1
}

func unescapeRingPayload(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] != 0x88 {
			out = append(out, data[i])
			continue
		}
		if i+1 >= len(data) {
			return nil, fmt.Errorf("mecom: truncated ring escape")
		}
		if data[i+1] != 0x88 {
			return nil, fmt.Errorf("mecom: unexpected ring marker 0x88 0x%02X in payload", data[i+1])
		}
		out = append(out, 0x88)
		i++
	}
	return out, nil
}

func decodeRingFrame(kind byte, payload, raw []byte, config []RingCaptureParameter) (RingFrame, error) {
	frame := RingFrame{Sync: kind == 0x01, Raw: append([]byte(nil), raw...)}
	offset := 0
	if frame.Sync {
		if len(payload) < 4 {
			return RingFrame{}, fmt.Errorf("mecom: sync ring frame too short")
		}
		frame.HasCaptureConfigID = true
		frame.CaptureConfigID = binary.LittleEndian.Uint16(payload[:2])
		frame.Timestamp10us = binary.LittleEndian.Uint16(payload[2:4])
		offset = 4
	} else {
		if len(payload) < 2 {
			return RingFrame{}, fmt.Errorf("mecom: ring frame too short")
		}
		frame.Timestamp10us = binary.LittleEndian.Uint16(payload[:2])
		offset = 2
	}
	for offset < len(payload) {
		if offset+5 > len(payload) {
			return RingFrame{}, fmt.Errorf("mecom: truncated ring sample")
		}
		tag := payload[offset]
		offset++
		index := int(tag & 0x7F)
		dataType := DataTypeFloat32
		if index < len(config) && config[index].Type != "" {
			dataType = config[index].Type
		}
		if tag&0x80 != 0 {
			if offset >= len(payload) {
				return RingFrame{}, fmt.Errorf("mecom: truncated ring sample type")
			}
			switch payload[offset] {
			case 0x01:
				dataType = DataTypeFloat32
			case 0x02:
				dataType = DataTypeInt32
			default:
				return RingFrame{}, fmt.Errorf("mecom: unsupported ring sample type 0x%02X", payload[offset])
			}
			offset++
		}
		if offset+4 > len(payload) {
			return RingFrame{}, fmt.Errorf("mecom: truncated ring sample value")
		}
		bits := binary.LittleEndian.Uint32(payload[offset : offset+4])
		offset += 4
		value := float64(math.Float32frombits(bits))
		if dataType == DataTypeInt32 {
			value = float64(int32(bits))
		}
		frame.Samples = append(frame.Samples, RingSample{ConfigIndex: index, Type: dataType, Value: value})
	}
	return frame, nil
}

func parsePayload(raw []byte) (string, error) {
	frame := bytes.TrimSuffix(raw, []byte{FrameTerminator})
	if len(frame) < 7 || frame[0] != '!' {
		return "", fmt.Errorf("mecom: invalid response %q", string(frame))
	}
	if err := verifyCRC(frame); err != nil {
		return "", err
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

func verifyCRC(frame []byte) error {
	if len(frame) < 5 {
		return fmt.Errorf("mecom: response too short for CRC %q", string(frame))
	}
	payloadEnd := len(frame) - 4
	got, err := strconv.ParseUint(string(frame[payloadEnd:]), 16, 16)
	if err != nil {
		return fmt.Errorf("mecom: invalid CRC %q: %w", string(frame[payloadEnd:]), err)
	}
	want := CRC16(frame[:payloadEnd])
	if uint16(got) != want {
		return fmt.Errorf("mecom: CRC mismatch got %04X want %04X", got, want)
	}
	return nil
}

func hexOnly(v string) string {
	return strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') || (r >= 'a' && r <= 'f') {
			return r
		}
		return -1
	}, v)
}

// ── Binary MeCom (over CAN) ───────────────────────────────────────────────────

// BinaryCommand constants for MeCom-over-CAN.
const (
	BinaryCmdQueryValue    uint16 = 0x01
	BinaryCmdSetValue      uint16 = 0x02
	BinaryCmdQueryBulk     uint16 = 0x05
	BinaryCmdResponseError uint16 = 0x06
)

// BuildBinarySingleGetFrame constructs a binary MeCom ?VR request.
// This is typically encapsulated in a CAN frame with ID 0x300 + address.
func BuildBinarySingleGetFrame(addr int, seq uint16, paramID, instance int) []byte {
	buf := make([]byte, 7)
	buf[0] = byte(seq & 0x7F) // Control: Bit 7=0 (Request)
	buf[1] = byte(addr)       // Device Address
	binary.BigEndian.PutUint16(buf[2:4], BinaryCmdQueryValue)
	binary.BigEndian.PutUint16(buf[4:6], uint16(paramID))
	buf[6] = byte(instance)
	return buf
}

// BinaryResponseMatchesRequest checks the binary CAN response fields that are
// echoed from the request envelope. Single-value responses do not carry the
// requested parameter identity, so callers must use a fresh sequence for each
// in-flight request to avoid accepting stale responses for another parameter.
func BinaryResponseMatchesRequest(f canopen.Frame, addr byte, seq, command uint16) bool {
	if f.DLC < 8 {
		return false
	}
	if f.Data[1] != addr {
		return false
	}
	if f.Data[0]&0x80 == 0 {
		return false
	}
	if uint16(f.Data[0]&0x7f) != seq&0x7f {
		return false
	}
	return binary.BigEndian.Uint16(f.Data[2:4]) == command
}

// DecodeBinaryCANFrame parses a binary MeCom response frame from CAN.
// It handles !VR responses (CAN ID 0x400 + address).
func DecodeBinaryCANFrame(f canopen.Frame, dataType DataType) (float64, error) {
	if f.DLC < 8 {
		return 0, fmt.Errorf("mecom: binary response DLC %d too short", f.DLC)
	}
	// Byte 0: Control Byte
	if f.Data[0]&0x80 == 0 {
		return 0, fmt.Errorf("mecom: CAN frame is not a response (bit 7 clear)")
	}
	// Byte 2-3: Command (should match original request, or be 0x01 for !VR)
	cmd := binary.BigEndian.Uint16(f.Data[2:4])
	if cmd == BinaryCmdResponseError {
		code := f.Data[4]
		if name, ok := errorsByCode[int(code)]; ok {
			return 0, fmt.Errorf("mecom: binary nack %02X (%s)", code, name)
		}
		return 0, fmt.Errorf("mecom: binary nack %02X", code)
	}
	if cmd != BinaryCmdQueryValue {
		return 0, fmt.Errorf("mecom: unexpected binary response command 0x%04X", cmd)
	}

	// Byte 4-7: Value
	bits := binary.BigEndian.Uint32(f.Data[4:8])
	if dataType == DataTypeInt32 {
		return float64(int32(bits)), nil
	}
	return float64(math.Float32frombits(bits)), nil
}

// ── Client ────────────────────────────────────────────────────────────────────

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

func (c *Client) SupportsRingReadout() bool { return true }

// ReadFloat32 reads one float32 parameter.
func (c *Client) ReadFloat32(ctx context.Context, paramID, instance int) (float64, error) {
	return c.readNumeric(ctx, paramID, instance, DataTypeFloat32)
}

// ReadInt32 reads one int32 parameter.
func (c *Client) ReadInt32(ctx context.Context, paramID, instance int) (int32, error) {
	v, err := c.readNumeric(ctx, paramID, instance, DataTypeInt32)
	return int32(v), err
}

// ReadBulk reads a chunk of parameters via ?VX. It is the preferred primitive
// for background round-robin polling.
func (c *Client) ReadBulk(ctx context.Context, params []Parameter) ([]float64, error) {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildBulkGetFrame(int(c.address), seq, params), nil
	})
	if err != nil {
		return nil, err
	}
	return ParseBulkResponse(raw, params)
}

func (c *Client) ReadRingPointer(ctx context.Context) (uint32, error) {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildRingPointerFrame(int(c.address), seq), nil
	})
	if err != nil {
		return 0, err
	}
	return ParseRingPointerResponse(raw)
}

func (c *Client) ReadRingChunk(ctx context.Context, start uint32, maxBytes uint16) (RingReadResponse, error) {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildRingReadFrame(int(c.address), seq, start, maxBytes), nil
	})
	if err != nil {
		return RingReadResponse{}, err
	}
	return ParseRingReadResponse(raw)
}

func (c *Client) ConfigureRingCapture(ctx context.Context, captureID uint16, params []RingCaptureParameter) error {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildRingCaptureConfigFrame(int(c.address), seq, captureID, params)
	})
	if err != nil {
		return err
	}
	return ParseRingCaptureConfigResponse(raw)
}

func (c *Client) TriggerRingSync(ctx context.Context) error {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildRingTriggerSyncFrame(int(c.address), seq), nil
	})
	if err != nil {
		return err
	}
	return ParseWriteResponse(raw)
}

func (c *Client) WriteFloat32(ctx context.Context, paramID, instance int, value float32) error {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildWriteFloat32Frame(int(c.address), seq, paramID, instance, value), nil
	})
	if err != nil {
		return err
	}
	return ParseWriteResponse(raw)
}

func (c *Client) WriteInt32(ctx context.Context, paramID, instance int, value int32) error {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildWriteInt32Frame(int(c.address), seq, paramID, instance, value), nil
	})
	if err != nil {
		return err
	}
	return ParseWriteResponse(raw)
}

func (c *Client) WriteString(ctx context.Context, paramID, instance int, value string) error {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildWriteStringFrame(int(c.address), seq, paramID, instance, value), nil
	})
	if err != nil {
		return err
	}
	return ParseWriteResponse(raw)
}

func (c *Client) WriteBigDataString(ctx context.Context, paramID, instance int, value string) error {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildSetBigDataStringFrame(int(c.address), seq, paramID, instance, 0, value, true)
	})
	if err != nil {
		return err
	}
	return ParseWriteResponse(raw)
}

func (c *Client) SaveToFlash(ctx context.Context) error {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildSaveToFlashFrame(int(c.address), seq), nil
	})
	if err != nil {
		return err
	}
	return ParseWriteResponse(raw)
}

func (c *Client) Reset(ctx context.Context) error {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildResetFrame(int(c.address), seq), nil
	})
	if err != nil {
		return err
	}
	return ParseWriteResponse(raw)
}

func (c *Client) readNumeric(ctx context.Context, paramID, instance int, dataType DataType) (float64, error) {
	raw, err := c.roundTrip(ctx, func(seq uint16) ([]byte, error) {
		return BuildSingleGetFrame(int(c.address), seq, paramID, instance), nil
	})
	if err != nil {
		return 0, err
	}
	return ParseSingleResponse(raw, dataType)
}

func (c *Client) roundTrip(ctx context.Context, buildFrame func(seq uint16) ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	seq := c.nextSeqLocked()
	c.mu.Unlock()

	frame, err := buildFrame(seq)
	if err != nil {
		return nil, err
	}
	return c.roundTripRaw(ctx, frame)
}

func (c *Client) roundTripRaw(ctx context.Context, frame []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if _, err := c.rw.Write(frame); err != nil {
		return nil, err
	}

	if deadlineRW, ok := c.rw.(interface{ SetReadDeadline(time.Time) error }); ok {
		deadline := time.Now().Add(c.timeout)
		if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
		if err := deadlineRW.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		defer deadlineRW.SetReadDeadline(time.Time{})
	}

	raw, err := readFrame(c.rw)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, fmt.Errorf("%w after %s", ErrTimeout, c.timeout)
		}
	}
	return raw, err
}

func (c *Client) nextSeqLocked() uint16 {
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
