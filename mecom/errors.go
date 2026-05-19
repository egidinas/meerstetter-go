package mecom

import "errors"

// Sentinel errors let consumers (including UI gateways) switch on error
// categories with errors.Is rather than parsing wrapped strings. Every error
// returned from public mecom APIs SHOULD wrap one of these where the
// category fits, in addition to its specific context.
var (
	// ErrUnreachable indicates the underlying transport could not be opened
	// or has dropped (serial port missing, TCP refused, CAN interface down).
	ErrUnreachable = errors.New("mecom: transport unreachable")

	// ErrTimeout indicates a request was issued but no reply arrived within
	// the configured timeout. Distinct from ErrUnreachable: the transport
	// itself is alive, the device just did not answer.
	ErrTimeout = errors.New("mecom: request timed out")

	// ErrBadAddress indicates a device address or CANopen node ID is outside
	// the allowed range, missing, or rejected by parsing.
	ErrBadAddress = errors.New("mecom: invalid device address")

	// ErrUnknownParameter indicates a parameter ID/instance combination has
	// no mapping for the requested transport. For example, asking
	// CANopenClient to read a parameter without an SDO object.
	ErrUnknownParameter = errors.New("mecom: parameter not mapped for transport")

	// ErrParameterReadOnly indicates a write was attempted against a
	// parameter the catalogue marks as read-only (sensor inputs, etc.).
	ErrParameterReadOnly = errors.New("mecom: parameter is read-only")

	// ErrWriteRejected indicates the device returned a NACK or SDO abort
	// for an otherwise well-formed write request.
	ErrWriteRejected = errors.New("mecom: device rejected write")

	// ErrTransportNotSupported indicates a feature (ring capture, control
	// actions, etc.) is not implemented for the active transport.
	ErrTransportNotSupported = errors.New("mecom: feature not supported on this transport")

	// ErrInvalidArgument indicates a malformed call (missing field, bad
	// type, value outside spec). Distinct from ErrBadAddress for narrower
	// matching.
	ErrInvalidArgument = errors.New("mecom: invalid argument")

	// ErrReadbackMismatch indicates a write succeeded at the protocol level
	// but an immediate readback showed the value did not stick or changed
	// unexpectedly.
	ErrReadbackMismatch = errors.New("mecom: readback mismatch")
)
