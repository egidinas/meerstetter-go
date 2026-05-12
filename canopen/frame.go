package canopen

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Frame is the minimal CAN frame contract used by the CANopen helpers.
type Frame struct {
	ID   uint32
	Data [8]byte
	DLC  uint8
}

// Validate returns an error when the frame cannot be represented as classic CAN.
func (f Frame) Validate() error {
	if f.DLC > 8 {
		return fmt.Errorf("canopen: invalid classic CAN DLC %d", f.DLC)
	}
	return nil
}

// SDOUploadRequest builds an expedited SDO upload request for one object entry.
func SDOUploadRequest(nodeID byte, index uint16, subIndex byte) Frame {
	return Frame{
		ID: 0x600 + uint32(nodeID),
		Data: [8]byte{
			0x40,
			byte(index),
			byte(index >> 8),
			subIndex,
		},
		DLC: 8,
	}
}

// SDOAbortError reports an SDO abort response from the server.
type SDOAbortError struct {
	Index    uint16
	SubIndex byte
	Code     uint32
}

func (e SDOAbortError) Error() string {
	return fmt.Sprintf("canopen: SDO abort 0x%08X for 0x%04X:%02X", e.Code, e.Index, e.SubIndex)
}

// SDOUploadResponse is a decoded expedited SDO upload response.
type SDOUploadResponse struct {
	NodeID   byte
	Index    uint16
	SubIndex byte
	Data     []byte
	Frame    Frame
}

// ParseSDOUploadResponse decodes an expedited SDO upload response or abort.
func ParseSDOUploadResponse(f Frame) (SDOUploadResponse, error) {
	if err := f.Validate(); err != nil {
		return SDOUploadResponse{}, err
	}
	if f.DLC != 8 {
		return SDOUploadResponse{}, fmt.Errorf("canopen: SDO response DLC = %d, want 8", f.DLC)
	}
	if f.ID < 0x580 || f.ID > 0x5ff {
		return SDOUploadResponse{}, fmt.Errorf("canopen: SDO response id 0x%03X outside 0x580..0x5ff", f.ID)
	}
	index := uint16(f.Data[1]) | uint16(f.Data[2])<<8
	subIndex := f.Data[3]
	resp := SDOUploadResponse{
		NodeID:   byte(f.ID - 0x580),
		Index:    index,
		SubIndex: subIndex,
		Frame:    f,
	}
	if f.Data[0] == 0x80 {
		return resp, SDOAbortError{
			Index:    index,
			SubIndex: subIndex,
			Code:     binary.LittleEndian.Uint32(f.Data[4:8]),
		}
	}
	if f.Data[0]&0xe0 != 0x40 {
		return resp, fmt.Errorf("canopen: unexpected SDO upload command 0x%02X", f.Data[0])
	}
	if f.Data[0]&0x02 == 0 {
		return resp, fmt.Errorf("canopen: segmented SDO uploads are not supported")
	}
	size := 4
	if f.Data[0]&0x01 != 0 {
		unused := int((f.Data[0] >> 2) & 0x03)
		size = 4 - unused
	}
	resp.Data = make([]byte, size)
	copy(resp.Data, f.Data[4:4+size])
	return resp, nil
}

func (r SDOUploadResponse) Uint32() (uint32, error) {
	if len(r.Data) != 4 {
		return 0, fmt.Errorf("canopen: 0x%04X:%02X has %d bytes, want uint32", r.Index, r.SubIndex, len(r.Data))
	}
	return binary.LittleEndian.Uint32(r.Data), nil
}

func (r SDOUploadResponse) Int32() (int32, error) {
	v, err := r.Uint32()
	return int32(v), err
}

func (r SDOUploadResponse) Float32() (float32, error) {
	v, err := r.Uint32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(v), nil
}

func (r SDOUploadResponse) Byte() (byte, error) {
	if len(r.Data) != 1 {
		return 0, fmt.Errorf("canopen: 0x%04X:%02X has %d bytes, want byte", r.Index, r.SubIndex, len(r.Data))
	}
	return r.Data[0], nil
}

// SDODownloadExpeditedRequest builds an expedited SDO download request for up to
// four payload bytes. The value bytes are little-endian CANopen object bytes.
func SDODownloadExpeditedRequest(nodeID byte, index uint16, subIndex byte, value []byte) (Frame, error) {
	if len(value) == 0 || len(value) > 4 {
		return Frame{}, fmt.Errorf("canopen: expedited SDO payload must have 1..4 bytes, got %d", len(value))
	}
	unused := 4 - len(value)
	cmd := byte(0x23 | byte(unused<<2))
	frame := Frame{
		ID: 0x600 + uint32(nodeID),
		Data: [8]byte{
			cmd,
			byte(index),
			byte(index >> 8),
			subIndex,
		},
		DLC: 8,
	}
	copy(frame.Data[4:], value)
	return frame, nil
}
