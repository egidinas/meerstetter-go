package mecom

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

//go:embed catalogues/sources/tec_canopen_sdo_map.v631.json
var tecCANopenSDOMapJSON []byte

type canopenSDOMapSource struct {
	SchemaVersion    string                         `json:"schema_version"`
	SourcePolicy     string                         `json:"source_policy"`
	Mappings         []canopenSDOMappingSource      `json:"mappings"`
	BridgeTransforms []canopenBridgeTransformSource `json:"bridge_transforms"`
}

type canopenSDOMappingSource struct {
	MeComID   int      `json:"mecom_id"`
	Name      string   `json:"name"`
	ValueType DataType `json:"value_type"`
	Access    string   `json:"access"`
	Instances struct {
		Mode  string `json:"mode"`
		Min   int    `json:"min"`
		Max   int    `json:"max"`
		Fixed int    `json:"fixed"`
	} `json:"instances"`
	CANopen struct {
		Index        string `json:"index"`
		Subindex     string `json:"subindex"`
		SubindexMode string `json:"subindex_mode"`
		DataType     string `json:"data_type"`
	} `json:"canopen"`
	SourceEvidence []string `json:"source_evidence"`
}

// CANopenSDOObject defines the index, subindex, and metadata for a mapped CANopen SDO.
type CANopenSDOObject struct {
	Index    uint16
	SubIndex byte
	Kind     DataType
	Writable bool
}

// CANopenSDOMap represents a parsed MeCom to CANopen SDO mappings catalog.
type CANopenSDOMap struct {
	byMeComID       map[int]CANopenSDOMapping
	bridgeTransform map[int]BridgeTransform
}

// BridgeTransform describes a CAN-backed MeCom compatibility behavior that is
// intentionally not a direct SDO lookup.
type BridgeTransform struct {
	MeComID       int
	Name          string
	Type          DataType
	Trigger       string
	Kind          string
	SourceMeComID int
	Scale         float64
	Int32Mask     uint32
	Int32Value    int32
}

type canopenBridgeTransformSource struct {
	MeComID   int      `json:"mecom_id"`
	Name      string   `json:"name"`
	ValueType DataType `json:"value_type"`
	Trigger   string   `json:"trigger"`
	Runtime   struct {
		Kind          string  `json:"kind"`
		SourceMeComID int     `json:"source_mecom_id"`
		Scale         float64 `json:"scale"`
		Int32Mask     string  `json:"int32_mask"`
		Int32Value    int32   `json:"int32_value"`
	} `json:"runtime"`
	SourceEvidence []string `json:"source_evidence"`
}

// CANopenSDOMapping stores parsed details for an individual MeCom ID mapping.
type CANopenSDOMapping struct {
	index                uint16
	kind                 DataType
	writable             bool
	minInstance          int
	maxInstance          int
	fixedInstance        int
	fixedSubIndex        byte
	subIndexFromInstance bool
}

var defaultCANopenSDOMap = mustLoadCANopenSDOMap()

func mustLoadCANopenSDOMap() CANopenSDOMap {
	m, err := LoadCANopenSDOMap(tecCANopenSDOMapJSON)
	if err != nil {
		panic(err)
	}
	return m
}

// LoadCANopenSDOMap parses a JSON SDO mapping document.
func LoadCANopenSDOMap(raw []byte) (CANopenSDOMap, error) {
	var source canopenSDOMapSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return CANopenSDOMap{}, err
	}
	if !strings.HasSuffix(source.SchemaVersion, "_canopen_sdo_map.v1") && source.SchemaVersion != "mecom_tec_canopen_sdo_map.v1" {
		return CANopenSDOMap{}, fmt.Errorf("unexpected CANopen SDO map schema %q", source.SchemaVersion)
	}
	out := CANopenSDOMap{
		byMeComID:       make(map[int]CANopenSDOMapping, len(source.Mappings)),
		bridgeTransform: make(map[int]BridgeTransform, len(source.BridgeTransforms)),
	}
	for _, entry := range source.Mappings {
		if entry.MeComID <= 0 {
			return CANopenSDOMap{}, fmt.Errorf("invalid MeCom ID %d", entry.MeComID)
		}
		if _, exists := out.byMeComID[entry.MeComID]; exists {
			return CANopenSDOMap{}, fmt.Errorf("duplicate MeCom ID %d", entry.MeComID)
		}
		if len(entry.SourceEvidence) == 0 {
			return CANopenSDOMap{}, fmt.Errorf("MeCom ID %d has no source evidence", entry.MeComID)
		}
		mapping, err := buildCANopenSDOMapping(entry)
		if err != nil {
			return CANopenSDOMap{}, fmt.Errorf("MeCom ID %d: %w", entry.MeComID, err)
		}
		out.byMeComID[entry.MeComID] = mapping
	}
	for _, entry := range source.BridgeTransforms {
		if entry.MeComID <= 0 {
			return CANopenSDOMap{}, fmt.Errorf("invalid bridge transform MeCom ID %d", entry.MeComID)
		}
		if _, exists := out.bridgeTransform[entry.MeComID]; exists {
			return CANopenSDOMap{}, fmt.Errorf("duplicate bridge transform MeCom ID %d", entry.MeComID)
		}
		if len(entry.SourceEvidence) == 0 {
			return CANopenSDOMap{}, fmt.Errorf("bridge transform MeCom ID %d has no source evidence", entry.MeComID)
		}
		transform, err := buildCANopenBridgeTransform(entry)
		if err != nil {
			return CANopenSDOMap{}, fmt.Errorf("bridge transform MeCom ID %d: %w", entry.MeComID, err)
		}
		out.bridgeTransform[entry.MeComID] = transform
	}
	return out, nil
}

func buildCANopenSDOMapping(entry canopenSDOMappingSource) (CANopenSDOMapping, error) {
	index, err := parseCANopenMapNumber(entry.CANopen.Index, 16)
	if err != nil {
		return CANopenSDOMapping{}, fmt.Errorf("parse CANopen index %q: %w", entry.CANopen.Index, err)
	}
	kind, err := canopenSDOMapDataType(entry.ValueType, entry.CANopen.DataType)
	if err != nil {
		return CANopenSDOMapping{}, err
	}
	mapping := CANopenSDOMapping{
		index:       uint16(index),
		kind:        kind,
		writable:    strings.Contains(strings.ToLower(entry.Access), "w"),
		minInstance: entry.Instances.Min,
		maxInstance: entry.Instances.Max,
	}
	switch strings.ToLower(entry.Instances.Mode) {
	case "fixed":
		if entry.Instances.Fixed <= 0 || entry.Instances.Fixed > 0xff {
			return CANopenSDOMapping{}, fmt.Errorf("invalid fixed instance %d", entry.Instances.Fixed)
		}
		subIndex, err := parseCANopenMapNumber(entry.CANopen.Subindex, 8)
		if err != nil {
			return CANopenSDOMapping{}, fmt.Errorf("parse CANopen subindex %q: %w", entry.CANopen.Subindex, err)
		}
		mapping.fixedInstance = entry.Instances.Fixed
		mapping.fixedSubIndex = byte(subIndex)
	case "subindex":
		if entry.CANopen.SubindexMode != "instance" || entry.CANopen.Subindex != "instance" {
			return CANopenSDOMapping{}, fmt.Errorf("subindex mapping must use instance subindex")
		}
		if mapping.minInstance == 0 {
			mapping.minInstance = 1
		}
		if mapping.maxInstance == 0 {
			mapping.maxInstance = 0xff
		}
		if mapping.minInstance <= 0 || mapping.maxInstance > 0xff || mapping.minInstance > mapping.maxInstance {
			return CANopenSDOMapping{}, fmt.Errorf("invalid instance range %d..%d", mapping.minInstance, mapping.maxInstance)
		}
		mapping.subIndexFromInstance = true
	default:
		return CANopenSDOMapping{}, fmt.Errorf("unknown instance mode %q", entry.Instances.Mode)
	}
	return mapping, nil
}

func canopenSDOMapDataType(valueType DataType, dataTypeCode string) (DataType, error) {
	switch DataType(strings.ToLower(string(valueType))) {
	case DataTypeInt32:
		return DataTypeInt32, nil
	case DataTypeFloat32:
		return DataTypeFloat32, nil
	case DataTypeLatin1:
		return DataTypeLatin1, nil
	}
	switch strings.ToLower(strings.TrimSpace(dataTypeCode)) {
	case "0x0004":
		return DataTypeInt32, nil
	case "0x0008":
		return DataTypeFloat32, nil
	default:
		return "", fmt.Errorf("unsupported CANopen data type %q", dataTypeCode)
	}
}

func buildCANopenBridgeTransform(entry canopenBridgeTransformSource) (BridgeTransform, error) {
	if strings.TrimSpace(entry.Name) == "" {
		return BridgeTransform{}, fmt.Errorf("missing name")
	}
	if strings.TrimSpace(entry.Trigger) == "" {
		return BridgeTransform{}, fmt.Errorf("missing trigger")
	}
	typ, err := canopenSDOMapDataType(entry.ValueType, "")
	if err != nil {
		return BridgeTransform{}, err
	}
	transform := BridgeTransform{
		MeComID:       entry.MeComID,
		Name:          entry.Name,
		Type:          typ,
		Trigger:       entry.Trigger,
		Kind:          strings.TrimSpace(entry.Runtime.Kind),
		SourceMeComID: entry.Runtime.SourceMeComID,
		Scale:         entry.Runtime.Scale,
	}
	switch transform.Kind {
	case "synthesize_float32_from_int32":
		if transform.Type != DataTypeFloat32 {
			return BridgeTransform{}, fmt.Errorf("synthesized float transform has type %q", transform.Type)
		}
		if transform.SourceMeComID <= 0 {
			return BridgeTransform{}, fmt.Errorf("missing source_mecom_id")
		}
		if transform.Scale == 0 {
			return BridgeTransform{}, fmt.Errorf("missing scale")
		}
	case "mask_int32":
		if transform.Type != DataTypeInt32 {
			return BridgeTransform{}, fmt.Errorf("masked int transform has type %q", transform.Type)
		}
		mask, err := parseCANopenMapNumber(entry.Runtime.Int32Mask, 32)
		if err != nil {
			return BridgeTransform{}, fmt.Errorf("parse int32_mask %q: %w", entry.Runtime.Int32Mask, err)
		}
		transform.Int32Mask = uint32(mask)
	case "latin1_big_data":
		if transform.Type != DataTypeLatin1 {
			return BridgeTransform{}, fmt.Errorf("latin1 transform has type %q", transform.Type)
		}
	case "constant_int32":
		if transform.Type != DataTypeInt32 {
			return BridgeTransform{}, fmt.Errorf("constant int transform has type %q", transform.Type)
		}
		transform.Int32Value = entry.Runtime.Int32Value
	default:
		return BridgeTransform{}, fmt.Errorf("unknown runtime kind %q", transform.Kind)
	}
	return transform, nil
}

func parseCANopenMapNumber(value string, bitSize int) (uint64, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "0x") {
		return strconv.ParseUint(value[2:], 16, bitSize)
	}
	return strconv.ParseUint(value, 10, bitSize)
}

// ObjectForMeCom returns the mapped SDO index and subindex for the requested MeCom ID.
func (m CANopenSDOMap) ObjectForMeCom(paramID, instance int) (CANopenSDOObject, bool) {
	if instance <= 0 || instance > 0xff {
		return CANopenSDOObject{}, false
	}
	mapping, ok := m.byMeComID[paramID]
	if !ok {
		return CANopenSDOObject{}, false
	}
	object := CANopenSDOObject{
		Index:    mapping.index,
		Kind:     mapping.kind,
		Writable: mapping.writable,
	}
	if mapping.subIndexFromInstance {
		if instance < mapping.minInstance || instance > mapping.maxInstance {
			return CANopenSDOObject{}, false
		}
		object.SubIndex = byte(instance)
		return object, true
	}
	if instance != mapping.fixedInstance {
		return CANopenSDOObject{}, false
	}
	object.SubIndex = mapping.fixedSubIndex
	return object, true
}

// ParameterTypes returns the parameter data types mapped in the SDO catalog.
func (m CANopenSDOMap) ParameterTypes() map[int]DataType {
	out := make(map[int]DataType, len(m.byMeComID))
	for id, mapping := range m.byMeComID {
		out[id] = mapping.kind
	}
	return out
}

// ParameterType returns the data type for the given MeCom ID if present in the catalog.
func (m CANopenSDOMap) ParameterType(id int) (DataType, bool) {
	if mapping, ok := m.byMeComID[id]; ok {
		return mapping.kind, true
	}
	return "", false
}

// BridgeTransforms returns compatibility transforms defined in the SDO catalog.
func (m CANopenSDOMap) BridgeTransforms() map[int]BridgeTransform {
	out := make(map[int]BridgeTransform, len(m.bridgeTransform))
	for id, transform := range m.bridgeTransform {
		out[id] = transform
	}
	return out
}

// CANopenMappedParameterTypes returns the MeCom parameter types backed by the
// embedded TEC CANopen SDO source map.
func CANopenMappedParameterTypes() map[int]DataType {
	return defaultCANopenSDOMap.ParameterTypes()
}

// CANopenBridgeTransforms returns compatibility behaviors documented in the
// embedded TEC CANopen SDO source map.
func CANopenBridgeTransforms() map[int]BridgeTransform {
	return defaultCANopenSDOMap.BridgeTransforms()
}

// CANopenBridgeTransform returns one compatibility behavior documented in the
// embedded TEC CANopen SDO source map.
func CANopenBridgeTransform(id int) (BridgeTransform, bool) {
	transform, ok := defaultCANopenSDOMap.bridgeTransform[id]
	return transform, ok
}
