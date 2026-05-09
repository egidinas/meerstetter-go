package objectdict

// Protocol identifies the source protocol used to define dictionary objects.
type Protocol string

const (
	ProtocolMeCom   Protocol = "mecom"
	ProtocolCANopen Protocol = "canopen"
)

// ValueKind describes how consumers should display and edit a value.
type ValueKind string

const (
	ValueKindUnknown    ValueKind = "unknown"
	ValueKindContinuous ValueKind = "continuous"
	ValueKindBoolean    ValueKind = "boolean"
	ValueKindEnum       ValueKind = "enum"
	ValueKindString     ValueKind = "string"
)

// Access describes object access rights from the controller point of view.
type Access string

const (
	AccessUnknown   Access = "unknown"
	AccessReadOnly  Access = "ro"
	AccessWriteOnly Access = "wo"
	AccessReadWrite Access = "rw"
)

// Dictionary is a stable, protocol-neutral view of Meerstetter controllable
// and observable objects.
type Dictionary struct {
	SchemaVersion int               `json:"schema_version"`
	Protocol      Protocol          `json:"protocol"`
	DefinitionID  string            `json:"definition_id,omitempty"`
	Device        Device            `json:"device,omitempty"`
	Objects       []Object          `json:"objects"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// Device identifies the device family described by a dictionary source.
type Device struct {
	Vendor        string `json:"vendor,omitempty"`
	VendorNumber  string `json:"vendor_number,omitempty"`
	Product       string `json:"product,omitempty"`
	ProductNumber string `json:"product_number,omitempty"`
	Revision      string `json:"revision,omitempty"`
}

// Object groups one protocol object and its subentries.
type Object struct {
	ID          string            `json:"id"`
	Index       uint16            `json:"index,omitempty"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Entries     []Entry           `json:"entries"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Entry is a typed readable and/or writable leaf in the object dictionary.
type Entry struct {
	ID       string            `json:"id"`
	Index    uint16            `json:"index,omitempty"`
	SubIndex uint8             `json:"sub_index,omitempty"`
	Name     string            `json:"name"`
	Unit     string            `json:"unit,omitempty"`
	DataType string            `json:"data_type,omitempty"`
	Kind     ValueKind         `json:"kind"`
	Access   Access            `json:"access"`
	Min      *float64          `json:"min,omitempty"`
	Max      *float64          `json:"max,omitempty"`
	Enum     map[int64]string  `json:"enum,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}
