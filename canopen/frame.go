package canopen

import "fmt"

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
