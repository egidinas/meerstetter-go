package mecomserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

// DeviceClientOpener opens one typed MeCom device client for a downstream
// bridge. It is intentionally transport-opaque: serial/TCP can still use raw
// streams, while CAN routes can translate ASCII requests into typed calls.
type DeviceClientOpener func(context.Context) (mecom.DeviceClient, error)

// DialEndpointTarget returns a downstream dialer for any supported endpoint.
// Serial/TCP targets use the existing raw stream dialer. CAN targets are
// bridged through mecom.DeviceClient so the TCP-facing server can still speak
// normal ASCII MeCom frames to upstream tools.
func DialEndpointTarget(target string, cfg mecom.ClientConfig, dialCAN mecom.CANDialer) (DownstreamDial, error) {
	ep, ok := mecom.ParseTarget(target)
	if !ok {
		return nil, fmt.Errorf("mecomserver: invalid target %q", target)
	}
	if ep.Network != "can" {
		return DialTarget(target)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultRequestTimeout
	}
	return DialDeviceClient(ep.String(), func(ctx context.Context) (mecom.DeviceClient, error) {
		return mecom.NewForEndpoint(ctx, ep, cfg, dialCAN)
	}, cfg.Timeout), nil
}

// DialDeviceClient adapts a typed device client into the net.Conn-shaped
// DownstreamDial expected by the existing broker. The returned net.Conn is a
// private pipe; the goroutine on the other side translates one ASCII MeCom
// frame at a time and replies with an ASCII MeCom response.
func DialDeviceClient(description string, opener DeviceClientOpener, timeout time.Duration) DownstreamDial {
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return func(ctx context.Context) (net.Conn, string, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		client, err := opener(ctx)
		if err != nil {
			return nil, "", err
		}
		upstream, downstream := net.Pipe()
		go runDeviceClientBridge(downstream, client, timeout)
		return upstream, description, nil
	}
}

func runDeviceClientBridge(conn net.Conn, client mecom.DeviceClient, timeout time.Duration) {
	defer conn.Close()
	defer client.Close()

	reader := bufio.NewReader(conn)
	for {
		frame, err := readBoundedFramePartial(reader, maxClientFrameBytes)
		if err != nil {
			return
		}
		resp := handleDeviceClientFrame(client, frame, timeout)
		if len(resp) == 0 {
			resp = deviceServerError(frame, fmt.Errorf("mecomserver: empty device bridge response"))
		}
		if timeout > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(timeout))
		}
		if _, err := conn.Write(resp); err != nil {
			return
		}
	}
}

type deviceBridgeRequest struct {
	raw     []byte
	address byte
	seq     uint16
	payload string
}

func handleDeviceClientFrame(client mecom.DeviceClient, raw []byte, timeout time.Duration) []byte {
	req, err := parseDeviceBridgeRequest(raw)
	if err != nil {
		return deviceServerError(raw, err)
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	payload, err := deviceBridgePayload(ctx, client, req.payload)
	if err != nil {
		return deviceBridgeNACK(req.address, req.seq, nackCodeForError(err))
	}
	return deviceBridgeOK(req.address, req.seq, payload)
}

func parseDeviceBridgeRequest(raw []byte) (deviceBridgeRequest, error) {
	frame := bytes.TrimSuffix(bytes.TrimSpace(raw), []byte{mecom.FrameTerminator})
	if len(frame) < 11 || frame[0] != '#' {
		return deviceBridgeRequest{}, fmt.Errorf("mecomserver: invalid request frame %q", string(frame))
	}
	if err := verifyDeviceBridgeCRC(frame); err != nil {
		return deviceBridgeRequest{}, err
	}
	addr, err := strconv.ParseUint(string(frame[1:3]), 16, 8)
	if err != nil {
		return deviceBridgeRequest{}, fmt.Errorf("mecomserver: invalid request address %q: %w", string(frame[1:3]), err)
	}
	seq, err := strconv.ParseUint(string(frame[3:7]), 16, 16)
	if err != nil {
		return deviceBridgeRequest{}, fmt.Errorf("mecomserver: invalid request sequence %q: %w", string(frame[3:7]), err)
	}
	return deviceBridgeRequest{
		raw:     append([]byte(nil), raw...),
		address: byte(addr),
		seq:     uint16(seq),
		payload: strings.ToUpper(string(frame[7 : len(frame)-4])),
	}, nil
}

func verifyDeviceBridgeCRC(frame []byte) error {
	payloadEnd := len(frame) - 4
	if payloadEnd <= 0 {
		return fmt.Errorf("mecomserver: frame too short for CRC %q", string(frame))
	}
	got, err := strconv.ParseUint(string(frame[payloadEnd:]), 16, 16)
	if err != nil {
		return fmt.Errorf("mecomserver: invalid request CRC %q: %w", string(frame[payloadEnd:]), err)
	}
	want := mecom.CRC16(frame[:payloadEnd])
	if uint16(got) != want {
		return fmt.Errorf("mecomserver: CRC mismatch got %04X want %04X", got, want)
	}
	return nil
}

func deviceBridgePayload(ctx context.Context, client mecom.DeviceClient, payload string) (string, error) {
	switch {
	case strings.HasPrefix(payload, "?VR"):
		return deviceBridgeSingleRead(ctx, client, payload)
	case strings.HasPrefix(payload, "?VX"):
		return deviceBridgeBulkRead(ctx, client, payload)
	case strings.HasPrefix(payload, "VS"):
		return "", deviceBridgeWrite(ctx, client, payload)
	case payload == "SP":
		control, ok := client.(mecom.ControlClient)
		if !ok {
			return "", mecom.ErrTransportNotSupported
		}
		return "", control.SaveToFlash(ctx)
	case payload == "RS":
		control, ok := client.(mecom.ControlClient)
		if !ok {
			return "", mecom.ErrTransportNotSupported
		}
		return "", control.Reset(ctx)
	default:
		return "", fmt.Errorf("%w: ASCII command %q", mecom.ErrTransportNotSupported, payloadCommand(payload))
	}
}

func deviceBridgeSingleRead(ctx context.Context, client mecom.DeviceClient, payload string) (string, error) {
	if len(payload) != len("?VR000000") {
		return "", fmt.Errorf("%w: invalid ?VR payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	paramID, instance, err := parseDeviceBridgeParameter(payload[3:])
	if err != nil {
		return "", err
	}
	typ := deviceBridgeParameterType(paramID)
	switch typ {
	case mecom.DataTypeInt32:
		value, err := client.ReadInt32(ctx, paramID, instance)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%08X", uint32(value)), nil
	case mecom.DataTypeFloat32, "":
		value, err := client.ReadFloat32(ctx, paramID, instance)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%08X", math.Float32bits(float32(value))), nil
	default:
		return "", fmt.Errorf("%w: single read of %s parameter %d", mecom.ErrTransportNotSupported, typ, paramID)
	}
}

func deviceBridgeBulkRead(ctx context.Context, client mecom.DeviceClient, payload string) (string, error) {
	if len(payload) < len("?VX00") {
		return "", fmt.Errorf("%w: invalid ?VX payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	count64, err := strconv.ParseUint(payload[3:5], 16, 8)
	if err != nil {
		return "", fmt.Errorf("%w: invalid ?VX count %q: %v", mecom.ErrInvalidArgument, payload[3:5], err)
	}
	count := int(count64)
	if len(payload) != 5+count*6 {
		return "", fmt.Errorf("%w: ?VX count %d does not match payload length %d", mecom.ErrInvalidArgument, count, len(payload))
	}
	params := make([]mecom.Parameter, 0, count)
	for i := 0; i < count; i++ {
		paramID, instance, err := parseDeviceBridgeParameter(payload[5+i*6 : 11+i*6])
		if err != nil {
			return "", err
		}
		params = append(params, mecom.Parameter{ID: paramID, Instance: instance, Type: deviceBridgeParameterType(paramID)})
	}
	values, err := client.ReadBulk(ctx, params)
	if err != nil {
		return "", err
	}
	if len(values) != len(params) {
		return "", fmt.Errorf("%w: bulk read returned %d values for %d parameters", mecom.ErrInvalidArgument, len(values), len(params))
	}
	var out strings.Builder
	for i, value := range values {
		switch params[i].Type {
		case mecom.DataTypeInt32:
			if math.IsNaN(value) {
				return "", fmt.Errorf("%w: int32 parameter %d instance %d returned NaN", mecom.ErrUnknownParameter, params[i].ID, params[i].Instance)
			}
			fmt.Fprintf(&out, "%08X", uint32(int32(value)))
		case mecom.DataTypeFloat32, "":
			fmt.Fprintf(&out, "%08X", math.Float32bits(float32(value)))
		default:
			return "", fmt.Errorf("%w: bulk read of %s parameter %d", mecom.ErrTransportNotSupported, params[i].Type, params[i].ID)
		}
	}
	return out.String(), nil
}

func deviceBridgeWrite(ctx context.Context, client mecom.DeviceClient, payload string) error {
	if len(payload) < len("VS000000") {
		return fmt.Errorf("%w: invalid VS payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	writer, ok := client.(mecom.WriteClient)
	if !ok {
		return mecom.ErrTransportNotSupported
	}
	paramID, instance, err := parseDeviceBridgeParameter(payload[2:8])
	if err != nil {
		return err
	}
	valueHex := payload[8:]
	typ := deviceBridgeParameterType(paramID)
	switch typ {
	case mecom.DataTypeInt32:
		if len(valueHex) != 8 {
			return fmt.Errorf("%w: int32 value for parameter %d has %d hex chars", mecom.ErrInvalidArgument, paramID, len(valueHex))
		}
		bits, err := strconv.ParseUint(valueHex, 16, 32)
		if err != nil {
			return fmt.Errorf("%w: invalid int32 value %q: %v", mecom.ErrInvalidArgument, valueHex, err)
		}
		return writer.WriteInt32(ctx, paramID, instance, int32(uint32(bits)))
	case mecom.DataTypeFloat32, "":
		if len(valueHex) != 8 {
			return fmt.Errorf("%w: float32 value for parameter %d has %d hex chars", mecom.ErrInvalidArgument, paramID, len(valueHex))
		}
		bits, err := strconv.ParseUint(valueHex, 16, 32)
		if err != nil {
			return fmt.Errorf("%w: invalid float32 value %q: %v", mecom.ErrInvalidArgument, valueHex, err)
		}
		return writer.WriteFloat32(ctx, paramID, instance, math.Float32frombits(uint32(bits)))
	case mecom.DataTypeLatin1:
		stringClient, ok := client.(mecom.StringWriteClient)
		if !ok {
			return mecom.ErrTransportNotSupported
		}
		data, err := hex.DecodeString(valueHex)
		if err != nil {
			return fmt.Errorf("%w: invalid LATIN1 value for parameter %d: %v", mecom.ErrInvalidArgument, paramID, err)
		}
		return stringClient.WriteString(ctx, paramID, instance, string(bytes.TrimRight(data, "\x00")))
	default:
		return fmt.Errorf("%w: write of %s parameter %d", mecom.ErrTransportNotSupported, typ, paramID)
	}
}

func parseDeviceBridgeParameter(payload string) (int, int, error) {
	if len(payload) != 6 {
		return 0, 0, fmt.Errorf("%w: parameter payload %q must be 6 hex chars", mecom.ErrInvalidArgument, payload)
	}
	paramID, err := strconv.ParseUint(payload[:4], 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: invalid parameter id %q: %v", mecom.ErrInvalidArgument, payload[:4], err)
	}
	instance, err := strconv.ParseUint(payload[4:], 16, 8)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: invalid parameter instance %q: %v", mecom.ErrInvalidArgument, payload[4:], err)
	}
	return int(paramID), int(instance), nil
}

func deviceBridgeOK(addr byte, seq uint16, payload string) []byte {
	prefix := fmt.Sprintf("!%02X%04X+%s", addr, seq, strings.ToUpper(payload))
	return []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator))
}

func deviceBridgeNACK(addr byte, seq uint16, code byte) []byte {
	prefix := fmt.Sprintf("!%02X%04X-%02X", addr, seq, code)
	return []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator))
}

func nackCodeForError(err error) byte {
	switch {
	case errors.Is(err, mecom.ErrUnknownParameter):
		return 0x05
	case errors.Is(err, mecom.ErrParameterReadOnly):
		return 0x06
	case errors.Is(err, mecom.ErrInvalidArgument):
		return 0x04
	case errors.Is(err, mecom.ErrTransportNotSupported):
		return 0x01
	default:
		return 0x03
	}
}

func payloadCommand(payload string) string {
	if len(payload) < 2 {
		return payload
	}
	if payload[0] == '?' && len(payload) >= 3 {
		return payload[:3]
	}
	return payload[:2]
}

var defaultDeviceBridgeParameterTypes = buildDefaultDeviceBridgeParameterTypes()

func buildDefaultDeviceBridgeParameterTypes() map[int]mecom.DataType {
	out := map[int]mecom.DataType{}
	for _, spec := range mecom.DefaultTECReadoutParameters(16) {
		if spec.Parameter.Type != "" {
			out[spec.Parameter.ID] = spec.Parameter.Type
		}
	}
	for _, param := range mecom.DefaultTECWriteParameters(16) {
		if param.Type != "" {
			out[param.ID] = param.Type
		}
	}
	out[102] = mecom.DataTypeInt32
	out[103] = mecom.DataTypeInt32
	out[104] = mecom.DataTypeInt32
	out[105] = mecom.DataTypeInt32
	return out
}

func deviceBridgeParameterType(id int) mecom.DataType {
	if typ := defaultDeviceBridgeParameterTypes[id]; typ != "" {
		return typ
	}
	return mecom.DataTypeFloat32
}
