package mecom

import "context"

// WriteClient is the symmetric counterpart to ReadClient. Concrete clients
// (ASCII Client, binary CANClient, CANopenClient) may implement it; CANClient
// does not yet wire writes through the binary MeCom-over-CAN frame format and
// returns an error.
type WriteClient interface {
	WriteFloat32(ctx context.Context, paramID, instance int, value float32) error
	WriteInt32(ctx context.Context, paramID, instance int, value int32) error
}

// ControlClient is the optional lifecycle surface for ASCII MeCom devices.
// Only the ASCII Client implements it today; CAN clients use vendor-specific
// SDO objects for these actions.
type ControlClient interface {
	Reset(ctx context.Context) error
	SaveToFlash(ctx context.Context) error
}

// Compile-time assertions that the concrete clients satisfy the right surfaces.
var (
	_ ReadClient    = (*Client)(nil)
	_ ReadClient    = (*CANClient)(nil)
	_ ReadClient    = (*CANopenClient)(nil)
	_ WriteClient   = (*Client)(nil)
	_ WriteClient   = (*CANopenClient)(nil)
	_ ControlClient = (*Client)(nil)
)
