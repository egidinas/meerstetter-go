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

const (
	deviceBridgeFirmwareIdentification = "8065-TEC SW G01     "
	deviceBridgeBoardIdentification    = "00000000"

	deviceBridgeMetadataReadFlag    = 0x01
	deviceBridgeMetadataWriteFlag   = 0x02
	deviceBridgeBigDataMaxElements  = 0x100
	deviceBridgeMaxChannelInstances = 4
	deviceBridgeMaxCascadeInstances = 0xFF

	deviceBridgeTransformSynthesizeFloat32FromInt32 = "synthesize_float32_from_int32"
	deviceBridgeTransformMaskInt32                  = "mask_int32"
	deviceBridgeTransformConstantInt32              = "constant_int32"
)

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
	if isDeviceBridgeInfoProbe(req.payload) {
		return deviceBridgeInfo(req.address, req.seq, payload)
	}
	if isDeviceBridgeRead(req.payload) {
		return deviceBridgeData(req.address, req.seq, payload)
	}
	return deviceBridgeOK(req.address, req.seq, payload)
}

func parseDeviceBridgeRequest(raw []byte) (deviceBridgeRequest, error) {
	frame := bytes.TrimSuffix(bytes.TrimSpace(raw), []byte{mecom.FrameTerminator})
	if len(frame) >= 2 && frame[0] == '?' {
		return deviceBridgeRequest{
			raw:     append([]byte(nil), raw...),
			address: 0,
			seq:     0,
			payload: strings.ToUpper(string(frame)),
		}, nil
	}
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
	case payload == "?IF" || payload == "?VI":
		return deviceBridgeFirmwareIdentification, nil
	case payload == "?BI" || payload == "?BID" || payload == "?BIF":
		return deviceBridgeBoardIdentification, nil
	case strings.HasPrefix(payload, "?VM"):
		return deviceBridgeMetadata(payload)
	case strings.HasPrefix(payload, "?VB"):
		return deviceBridgeBigDataRead(payload)
	case strings.HasPrefix(payload, "?VL"):
		return deviceBridgeLimits(payload)
	case strings.HasPrefix(payload, "?RS"):
		return deviceBridgeRingPayload(ctx, client, payload)
	case strings.HasPrefix(payload, "?VR"):
		return deviceBridgeSingleRead(ctx, client, payload)
	case strings.HasPrefix(payload, "?VX"):
		return deviceBridgeBulkRead(ctx, client, payload)
	case strings.HasPrefix(payload, "VB"):
		return "", deviceBridgeBigDataWrite(ctx, client, payload)
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

func isDeviceBridgeInfoProbe(payload string) bool {
	return payload == "?IF" || payload == "?VI" || payload == "?BI" || payload == "?BID" || payload == "?BIF"
}

func isDeviceBridgeRead(payload string) bool {
	return strings.HasPrefix(payload, "?VR") ||
		strings.HasPrefix(payload, "?VX") ||
		strings.HasPrefix(payload, "?VM") ||
		strings.HasPrefix(payload, "?VB") ||
		strings.HasPrefix(payload, "?VL") ||
		strings.HasPrefix(payload, "?RS")
}

func deviceBridgeMetadata(payload string) (string, error) {
	if len(payload) != len("?VM000000") {
		return "", fmt.Errorf("%w: invalid ?VM payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	paramID, _, err := parseDeviceBridgeParameter(payload[3:])
	if err != nil {
		return "", err
	}
	typ := deviceBridgeParameterType(paramID)
	meParType, err := deviceBridgeMeParType(typ)
	if err != nil {
		return "", err
	}
	flags := byte(deviceBridgeMetadataReadFlag)
	if deviceBridgeParameterWritable(paramID) {
		flags |= deviceBridgeMetadataWriteFlag
	}
	minimum, maximum, actual, err := deviceBridgeParameterBounds(typ)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%02X%02X%02X%08X%s%s%s",
		meParType,
		flags,
		deviceBridgeParameterMaxInstances(paramID),
		deviceBridgeParameterMaxElements(typ),
		minimum,
		maximum,
		actual,
	), nil
}

func deviceBridgeLimits(payload string) (string, error) {
	if len(payload) != len("?VL000000") {
		return "", fmt.Errorf("%w: invalid ?VL payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	paramID, _, err := parseDeviceBridgeParameter(payload[3:])
	if err != nil {
		return "", err
	}
	typ := deviceBridgeParameterType(paramID)
	meParType, err := deviceBridgeMeParType(typ)
	if err != nil {
		return "", err
	}
	minimum, maximum, _, err := deviceBridgeParameterBounds(typ)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%02X%s%s", meParType, minimum, maximum), nil
}

func deviceBridgeBigDataRead(payload string) (string, error) {
	if len(payload) != len("?VB000000000000000000") {
		return "", fmt.Errorf("%w: invalid ?VB payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	paramID, instance, err := parseDeviceBridgeParameter(payload[3:9])
	if err != nil {
		return "", err
	}
	if _, err := parseDeviceBridgeHexUint(payload[9:17], "big-data read start", 32); err != nil {
		return "", err
	}
	if _, err := parseDeviceBridgeHexUint(payload[17:21], "big-data max elements", 16); err != nil {
		return "", err
	}
	if deviceBridgeParameterType(paramID) != mecom.DataTypeLatin1 {
		return "", fmt.Errorf("%w: big-data read of non-LATIN1 parameter %d instance %d", mecom.ErrTransportNotSupported, paramID, instance)
	}
	return "000000", nil
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
		if transform, ok := deviceBridgeTransform(paramID, deviceBridgeTransformConstantInt32); ok {
			return fmt.Sprintf("%08X", uint32(transform.Int32Value)), nil
		}
		value, err := client.ReadInt32(ctx, paramID, instance)
		if err != nil {
			return "", err
		}
		value = deviceBridgeCompatibleInt32Value(paramID, value)
		return fmt.Sprintf("%08X", uint32(value)), nil
	case mecom.DataTypeFloat32, "":
		if transform, ok := deviceBridgeTransform(paramID, deviceBridgeTransformSynthesizeFloat32FromInt32); ok {
			value, err := deviceBridgeSynthesizedFloat32(ctx, client, transform, instance)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%08X", math.Float32bits(float32(value))), nil
		}
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
	values := make([]float64, len(params))
	downstreamParams := make([]mecom.Parameter, 0, len(params))
	downstreamIndexes := make([]int, 0, len(params))
	for i, param := range params {
		if transform, ok := deviceBridgeTransform(param.ID, deviceBridgeTransformConstantInt32); ok {
			values[i] = float64(transform.Int32Value)
			continue
		}
		if transform, ok := deviceBridgeTransform(param.ID, deviceBridgeTransformSynthesizeFloat32FromInt32); ok {
			value, err := deviceBridgeSynthesizedFloat32(ctx, client, transform, param.Instance)
			if err != nil {
				return "", err
			}
			values[i] = value
			continue
		}
		downstreamIndexes = append(downstreamIndexes, i)
		downstreamParams = append(downstreamParams, param)
	}
	if len(downstreamParams) > 0 {
		downstreamValues, err := client.ReadBulk(ctx, downstreamParams)
		if err != nil {
			return "", err
		}
		if len(downstreamValues) != len(downstreamParams) {
			return "", fmt.Errorf("%w: bulk read returned %d values for %d parameters", mecom.ErrInvalidArgument, len(downstreamValues), len(downstreamParams))
		}
		for i, value := range downstreamValues {
			values[downstreamIndexes[i]] = value
		}
	}
	var out strings.Builder
	for i, value := range values {
		switch params[i].Type {
		case mecom.DataTypeInt32:
			if math.IsNaN(value) {
				return "", fmt.Errorf("%w: int32 parameter %d instance %d returned NaN", mecom.ErrUnknownParameter, params[i].ID, params[i].Instance)
			}
			intValue := deviceBridgeCompatibleInt32Value(params[i].ID, int32(value))
			fmt.Fprintf(&out, "%08X", uint32(intValue))
		case mecom.DataTypeFloat32, "":
			fmt.Fprintf(&out, "%08X", math.Float32bits(float32(value)))
		default:
			return "", fmt.Errorf("%w: bulk read of %s parameter %d", mecom.ErrTransportNotSupported, params[i].Type, params[i].ID)
		}
	}
	return out.String(), nil
}

func deviceBridgeCompatibleInt32Value(paramID int, value int32) int32 {
	if transform, ok := deviceBridgeTransform(paramID, deviceBridgeTransformMaskInt32); ok {
		return int32(uint32(value) & transform.Int32Mask)
	}
	return value
}

func deviceBridgeSynthesizedFloat32(ctx context.Context, client mecom.DeviceClient, transform mecom.BridgeTransform, instance int) (float64, error) {
	version, err := client.ReadInt32(ctx, transform.SourceMeComID, instance)
	if err != nil {
		return 0, err
	}
	return float64(version) * transform.Scale, nil
}

func deviceBridgeTransform(paramID int, kind string) (mecom.BridgeTransform, bool) {
	transform, ok := mecom.CANopenBridgeTransform(paramID)
	return transform, ok && transform.Kind == kind
}

func deviceBridgeBigDataWrite(ctx context.Context, client mecom.DeviceClient, payload string) error {
	if len(payload) < len("VB00000000000000000000") {
		return fmt.Errorf("%w: invalid VB payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	paramID, instance, err := parseDeviceBridgeParameter(payload[2:8])
	if err != nil {
		return err
	}
	start64, err := parseDeviceBridgeHexUint(payload[8:16], "big-data write start", 32)
	if err != nil {
		return err
	}
	count64, err := parseDeviceBridgeHexUint(payload[16:20], "big-data element count", 16)
	if err != nil {
		return err
	}
	isLast64, err := parseDeviceBridgeHexUint(payload[20:22], "big-data final flag", 8)
	if err != nil {
		return err
	}
	valueHex := payload[22:]
	if len(valueHex) != int(count64)*2 {
		return fmt.Errorf("%w: VB element count %d does not match payload hex length %d", mecom.ErrInvalidArgument, count64, len(valueHex))
	}
	if deviceBridgeParameterType(paramID) != mecom.DataTypeLatin1 {
		return fmt.Errorf("%w: big-data write of non-LATIN1 parameter %d", mecom.ErrTransportNotSupported, paramID)
	}
	if start64 != 0 || isLast64 != 1 {
		return fmt.Errorf("%w: multi-package big-data writes are not supported", mecom.ErrTransportNotSupported)
	}
	stringClient, ok := client.(mecom.StringWriteClient)
	if !ok {
		return mecom.ErrTransportNotSupported
	}
	data, err := hex.DecodeString(valueHex)
	if err != nil {
		return fmt.Errorf("%w: invalid LATIN1 big-data value for parameter %d: %v", mecom.ErrInvalidArgument, paramID, err)
	}
	return stringClient.WriteBigDataString(ctx, paramID, instance, string(bytes.TrimRight(data, "\x00")))
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

func deviceBridgeRingPayload(ctx context.Context, client mecom.DeviceClient, payload string) (string, error) {
	if len(payload) < len("?RS0000") {
		return "", fmt.Errorf("%w: invalid ?RS payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	command := payload[3:7]
	switch command {
	case "0000":
		if len(payload) != len("?RS0000") {
			return "", fmt.Errorf("%w: invalid ring pointer payload length %d", mecom.ErrInvalidArgument, len(payload))
		}
		pointer, err := client.ReadRingPointer(ctx)
		if errors.Is(err, mecom.ErrTransportNotSupported) {
			return "00000000", nil
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%08X", pointer), nil
	case "0001":
		if len(payload) != len("?RS0001000000000000") {
			return "", fmt.Errorf("%w: invalid ring read payload length %d", mecom.ErrInvalidArgument, len(payload))
		}
		start, err := parseDeviceBridgeHexUint(payload[7:15], "ring read start", 32)
		if err != nil {
			return "", err
		}
		maxBytes, err := parseDeviceBridgeHexUint(payload[15:19], "ring read max bytes", 16)
		if err != nil {
			return "", err
		}
		resp, err := client.ReadRingChunk(ctx, uint32(start), uint16(maxBytes))
		if errors.Is(err, mecom.ErrTransportNotSupported) {
			return "000000", nil
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%04X%02X%s", resp.BytesAdded, resp.Status, strings.ToUpper(hex.EncodeToString(resp.Data))), nil
	case "0002":
		if len(payload) < len("?RS0002000000") {
			return "", fmt.Errorf("%w: invalid ring capture-config payload length %d", mecom.ErrInvalidArgument, len(payload))
		}
		captureID, err := parseDeviceBridgeHexUint(payload[7:11], "ring capture id", 16)
		if err != nil {
			return "", err
		}
		count64, err := parseDeviceBridgeHexUint(payload[11:13], "ring capture count", 8)
		if err != nil {
			return "", err
		}
		count := int(count64)
		if len(payload) != 13+count*10 {
			return "", fmt.Errorf("%w: ring capture count %d does not match payload length %d", mecom.ErrInvalidArgument, count, len(payload))
		}
		params := make([]mecom.RingCaptureParameter, 0, count)
		for i := 0; i < count; i++ {
			off := 13 + i*10
			paramID, instance, err := parseDeviceBridgeParameter(payload[off : off+6])
			if err != nil {
				return "", err
			}
			inhibit, err := parseDeviceBridgeHexUint(payload[off+6:off+10], "ring inhibit time", 16)
			if err != nil {
				return "", err
			}
			params = append(params, mecom.RingCaptureParameter{
				Parameter: mecom.Parameter{
					ID:       paramID,
					Instance: instance,
					Type:     deviceBridgeParameterType(paramID),
				},
				InhibitTime10us: uint16(inhibit),
			})
		}
		if err := client.ConfigureRingCapture(ctx, uint16(captureID), params); errors.Is(err, mecom.ErrTransportNotSupported) {
			return "00", nil
		} else if err != nil {
			return "", err
		}
		return "00", nil
	case "0003":
		if len(payload) != len("?RS0003") {
			return "", fmt.Errorf("%w: invalid ring trigger-sync payload length %d", mecom.ErrInvalidArgument, len(payload))
		}
		if err := client.TriggerRingSync(ctx); errors.Is(err, mecom.ErrTransportNotSupported) {
			return "00", nil
		} else if err != nil {
			return "", err
		}
		return "00", nil
	default:
		return "", fmt.Errorf("%w: ring command %s", mecom.ErrTransportNotSupported, command)
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

func parseDeviceBridgeHexUint(value, name string, bitSize int) (uint64, error) {
	out, err := strconv.ParseUint(value, 16, bitSize)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid %s %q: %v", mecom.ErrInvalidArgument, name, value, err)
	}
	return out, nil
}

func deviceBridgeMeParType(typ mecom.DataType) (byte, error) {
	switch typ {
	case mecom.DataTypeFloat32, "":
		return 0, nil
	case mecom.DataTypeInt32:
		return 1, nil
	case mecom.DataTypeLatin1:
		return 3, nil
	default:
		return 0, fmt.Errorf("%w: unsupported metadata type %s", mecom.ErrTransportNotSupported, typ)
	}
}

func deviceBridgeParameterBounds(typ mecom.DataType) (string, string, string, error) {
	switch typ {
	case mecom.DataTypeFloat32, "":
		return "FF800000", "7F800000", "00000000", nil
	case mecom.DataTypeInt32:
		return "80000000", "7FFFFFFF", "00000000", nil
	case mecom.DataTypeLatin1:
		return "00", "00", "00", nil
	default:
		return "", "", "", fmt.Errorf("%w: unsupported bounds type %s", mecom.ErrTransportNotSupported, typ)
	}
}

func deviceBridgeParameterMaxElements(typ mecom.DataType) uint32 {
	if typ == mecom.DataTypeLatin1 {
		return deviceBridgeBigDataMaxElements
	}
	return 1
}

func deviceBridgeParameterMaxInstances(id int) byte {
	switch {
	case id >= 53120 && id <= 53123:
		return deviceBridgeMaxCascadeInstances
	case id >= 1000:
		return deviceBridgeMaxChannelInstances
	default:
		return 1
	}
}

func deviceBridgeParameterWritable(id int) bool {
	return mecom.TECParameterWritable(id)
}

func deviceBridgeOK(addr byte, seq uint16, payload string) []byte {
	prefix := fmt.Sprintf("!%02X%04X+%s", addr, seq, strings.ToUpper(payload))
	return []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator))
}

func deviceBridgeInfo(addr byte, seq uint16, payload string) []byte {
	return deviceBridgeData(addr, seq, payload)
}

func deviceBridgeData(addr byte, seq uint16, payload string) []byte {
	prefix := fmt.Sprintf("!%02X%04X%s", addr, seq, payload)
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
	for id, typ := range mecom.CANopenMappedParameterTypes() {
		out[id] = typ
	}
	for id, transform := range mecom.CANopenBridgeTransforms() {
		if transform.Type != "" {
			out[id] = transform.Type
		}
	}
	return out
}

func deviceBridgeParameterType(id int) mecom.DataType {
	if typ := defaultDeviceBridgeParameterTypes[id]; typ != "" {
		return typ
	}
	return mecom.DataTypeFloat32
}
