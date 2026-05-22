package mecom

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

//go:embed catalogues/sources/tec_canopen_sdo_map.v631.json
var tecCANopenSDOMapJSON []byte

//go:embed catalogues/sources/ldd_130x_canopen_sdo_map.v221.json
var lddCANopenSDOMapJSON []byte

type canopenSDOMapSource struct {
	SchemaVersion         string                              `json:"schema_version"`
	SourcePolicy          string                              `json:"source_policy"`
	Mappings              []canopenSDOMappingSource           `json:"mappings"`
	BridgeTransforms      []canopenBridgeTransformSource      `json:"bridge_transforms"`
	Unsupported           []canopenUnsupportedSource          `json:"unsupported"`
	CompatibilityProfiles []canopenCompatibilityProfileSource `json:"compatibility_profiles"`
}

type canopenInstanceSource struct {
	Mode  string `json:"mode"`
	Min   int    `json:"min"`
	Max   int    `json:"max"`
	Fixed int    `json:"fixed"`
}

type canopenSDOMappingSource struct {
	MeComID   int                   `json:"mecom_id"`
	Name      string                `json:"name"`
	ValueType DataType              `json:"value_type"`
	Access    string                `json:"access"`
	Instances canopenInstanceSource `json:"instances"`
	CANopen   struct {
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
	byMeComID            map[int]CANopenSDOMapping
	bridgeTransform      map[int]BridgeTransform
	unsupported          map[int]string
	compatibilityProfile map[string]map[int]CANopenCompatibilityParameter
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
	Writable      bool
	MinInstance   int
	MaxInstance   int
}

type canopenBridgeTransformSource struct {
	MeComID   int                   `json:"mecom_id"`
	Name      string                `json:"name"`
	ValueType DataType              `json:"value_type"`
	Access    string                `json:"access"`
	Instances canopenInstanceSource `json:"instances"`
	Trigger   string                `json:"trigger"`
	Runtime   struct {
		Kind          string  `json:"kind"`
		SourceMeComID int     `json:"source_mecom_id"`
		Scale         float64 `json:"scale"`
		Int32Mask     string  `json:"int32_mask"`
		Int32Value    int32   `json:"int32_value"`
	} `json:"runtime"`
	SourceEvidence []string `json:"source_evidence"`
}

type canopenUnsupportedSource struct {
	ID             any      `json:"id"`
	Reason         string   `json:"reason"`
	SourceEvidence []string `json:"source_evidence"`
}

type canopenCompatibilityProfileSource struct {
	Name           string                                       `json:"name"`
	SourceEvidence []string                                     `json:"source_evidence"`
	Parameters     []canopenCompatibilityProfileParameterSource `json:"parameters"`
}

type canopenCompatibilityProfileParameterSource struct {
	MeComID     int      `json:"mecom_id"`
	MaxInstance int      `json:"max_instance"`
	Translation string   `json:"translation"`
	ValueType   DataType `json:"value_type"`
	Access      string   `json:"access"`
}

// CANopenCompatibilityParameter records one external client parameter surface
// entry observed in a compatibility trace/profile.
type CANopenCompatibilityParameter struct {
	MeComID     int
	MaxInstance int
	Type        DataType
	Writable    bool
	Supported   bool
	Translation string
}

const (
	canopenCompatibilityTranslationDirectMapping   = "direct_mapping"
	canopenCompatibilityTranslationBridgeTransform = "bridge_transform"
	canopenCompatibilityTranslationUnsupported     = "unsupported"
)

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
	if !strings.HasSuffix(source.SchemaVersion, "_canopen_sdo_map.v1") {
		return CANopenSDOMap{}, fmt.Errorf("unexpected CANopen SDO map schema %q", source.SchemaVersion)
	}
	out := CANopenSDOMap{
		byMeComID:            make(map[int]CANopenSDOMapping, len(source.Mappings)),
		bridgeTransform:      make(map[int]BridgeTransform, len(source.BridgeTransforms)),
		unsupported:          make(map[int]string, len(source.Unsupported)),
		compatibilityProfile: make(map[string]map[int]CANopenCompatibilityParameter, len(source.CompatibilityProfiles)),
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
	for _, entry := range source.Unsupported {
		id, numeric, err := parseCANopenUnsupportedID(entry.ID)
		if err != nil {
			return CANopenSDOMap{}, err
		}
		if !numeric {
			continue
		}
		if strings.TrimSpace(entry.Reason) == "" {
			return CANopenSDOMap{}, fmt.Errorf("unsupported MeCom ID %d has no reason", id)
		}
		if len(entry.SourceEvidence) == 0 {
			return CANopenSDOMap{}, fmt.Errorf("unsupported MeCom ID %d has no source evidence", id)
		}
		out.unsupported[id] = entry.Reason
	}
	for _, profile := range source.CompatibilityProfiles {
		name := strings.ToLower(strings.TrimSpace(profile.Name))
		if name == "" {
			return CANopenSDOMap{}, fmt.Errorf("compatibility profile has no name")
		}
		if _, exists := out.compatibilityProfile[name]; exists {
			return CANopenSDOMap{}, fmt.Errorf("duplicate compatibility profile %q", profile.Name)
		}
		if len(profile.SourceEvidence) == 0 {
			return CANopenSDOMap{}, fmt.Errorf("compatibility profile %q has no source evidence", profile.Name)
		}
		params := make(map[int]CANopenCompatibilityParameter, len(profile.Parameters))
		for _, param := range profile.Parameters {
			if param.MeComID <= 0 {
				return CANopenSDOMap{}, fmt.Errorf("compatibility profile %q has invalid MeCom ID %d", profile.Name, param.MeComID)
			}
			if param.MaxInstance <= 0 || param.MaxInstance > 0xff {
				return CANopenSDOMap{}, fmt.Errorf("compatibility profile %q MeCom ID %d has invalid max instance %d", profile.Name, param.MeComID, param.MaxInstance)
			}
			if _, exists := params[param.MeComID]; exists {
				return CANopenSDOMap{}, fmt.Errorf("compatibility profile %q has duplicate MeCom ID %d", profile.Name, param.MeComID)
			}
			compatParam, err := out.resolveCANopenCompatibilityProfileParameter(name, param)
			if err != nil {
				return CANopenSDOMap{}, err
			}
			params[param.MeComID] = compatParam
		}
		out.compatibilityProfile[name] = params
	}
	return out, nil
}

func (m CANopenSDOMap) resolveCANopenCompatibilityProfileParameter(profileName string, param canopenCompatibilityProfileParameterSource) (CANopenCompatibilityParameter, error) {
	resolved := CANopenCompatibilityParameter{
		MeComID:     param.MeComID,
		MaxInstance: param.MaxInstance,
	}
	if transform, ok := m.bridgeTransform[param.MeComID]; ok {
		resolved.Type = transform.Type
		resolved.Writable = transform.Writable
		resolved.Supported = true
		resolved.Translation = canopenCompatibilityTranslationBridgeTransform
	} else if mapping, ok := m.byMeComID[param.MeComID]; ok {
		resolved.Type = mapping.kind
		resolved.Writable = mapping.writable
		resolved.Supported = true
		resolved.Translation = canopenCompatibilityTranslationDirectMapping
	} else if _, ok := m.unsupported[param.MeComID]; ok {
		resolved.Supported = false
		resolved.Translation = canopenCompatibilityTranslationUnsupported
	} else {
		return CANopenCompatibilityParameter{}, fmt.Errorf("compatibility profile %q MeCom ID %d has no direct mapping, bridge transform, or unsupported declaration", profileName, param.MeComID)
	}

	if translation := strings.ToLower(strings.TrimSpace(param.Translation)); translation != "" && translation != resolved.Translation {
		return CANopenCompatibilityParameter{}, fmt.Errorf("compatibility profile %q MeCom ID %d translation %q does not match resolved %q", profileName, param.MeComID, translation, resolved.Translation)
	}
	if param.ValueType != "" {
		typ, err := canopenSDOMapDataType(param.ValueType, "")
		if err != nil {
			return CANopenCompatibilityParameter{}, fmt.Errorf("compatibility profile %q MeCom ID %d value_type: %w", profileName, param.MeComID, err)
		}
		if resolved.Supported && typ != resolved.Type {
			return CANopenCompatibilityParameter{}, fmt.Errorf("compatibility profile %q MeCom ID %d value_type %q does not match resolved %q", profileName, param.MeComID, typ, resolved.Type)
		}
		resolved.Type = typ
	}
	if access := strings.ToLower(strings.TrimSpace(param.Access)); access != "" {
		resolved.Writable = strings.Contains(access, "w")
	}
	return resolved, nil
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
	minInstance, maxInstance, err := canopenInstanceBounds(entry.Instances)
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
		Writable:      strings.Contains(strings.ToLower(entry.Access), "w"),
		MinInstance:   minInstance,
		MaxInstance:   maxInstance,
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
	case "virtual_parameter":
		switch transform.Type {
		case DataTypeInt32, DataTypeFloat32, DataTypeLatin1:
		default:
			return BridgeTransform{}, fmt.Errorf("virtual parameter transform has type %q", transform.Type)
		}
	default:
		return BridgeTransform{}, fmt.Errorf("unknown runtime kind %q", transform.Kind)
	}
	return transform, nil
}

func canopenInstanceBounds(instances canopenInstanceSource) (int, int, error) {
	switch strings.ToLower(strings.TrimSpace(instances.Mode)) {
	case "fixed":
		if instances.Fixed <= 0 || instances.Fixed > 0xff {
			return 0, 0, fmt.Errorf("invalid fixed instance %d", instances.Fixed)
		}
		return instances.Fixed, instances.Fixed, nil
	case "subindex":
		minInstance := instances.Min
		maxInstance := instances.Max
		if minInstance == 0 {
			minInstance = 1
		}
		if maxInstance == 0 {
			maxInstance = 0xff
		}
		if minInstance <= 0 || maxInstance > 0xff || minInstance > maxInstance {
			return 0, 0, fmt.Errorf("invalid instance range %d..%d", minInstance, maxInstance)
		}
		return minInstance, maxInstance, nil
	default:
		return 0, 0, fmt.Errorf("unknown instance mode %q", instances.Mode)
	}
}

func parseCANopenMapNumber(value string, bitSize int) (uint64, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "0x") {
		return strconv.ParseUint(value[2:], 16, bitSize)
	}
	return strconv.ParseUint(value, 10, bitSize)
}

func parseCANopenUnsupportedID(value any) (int, bool, error) {
	switch v := value.(type) {
	case float64:
		id := int(v)
		if id <= 0 || float64(id) != v {
			return 0, true, fmt.Errorf("invalid unsupported MeCom ID %v", value)
		}
		return id, true, nil
	case int:
		if v <= 0 {
			return 0, true, fmt.Errorf("invalid unsupported MeCom ID %v", value)
		}
		return v, true, nil
	case string:
		raw := strings.TrimSpace(v)
		id, err := strconv.Atoi(raw)
		if err != nil {
			return 0, false, nil
		}
		if id <= 0 {
			return 0, true, fmt.Errorf("invalid unsupported MeCom ID %v", value)
		}
		return id, true, nil
	default:
		return 0, false, nil
	}
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

// ParameterMaxInstances returns configured maximum instance counts for mapped parameters.
func (m CANopenSDOMap) ParameterMaxInstances() map[int]int {
	out := make(map[int]int, len(m.byMeComID))
	for id, mapping := range m.byMeComID {
		if mapping.subIndexFromInstance {
			out[id] = mapping.maxInstance
			continue
		}
		out[id] = 1
	}
	return out
}

// BridgeTransforms returns compatibility transforms defined in the SDO catalog.
func (m CANopenSDOMap) BridgeTransforms() map[int]BridgeTransform {
	out := make(map[int]BridgeTransform, len(m.bridgeTransform))
	for id, transform := range m.bridgeTransform {
		out[id] = transform
	}
	return out
}

// UnsupportedParameterIDs returns numeric MeCom IDs explicitly documented as
// unsupported direct SDO paths in the SDO catalog.
func (m CANopenSDOMap) UnsupportedParameterIDs() map[int]string {
	out := make(map[int]string, len(m.unsupported))
	for id, reason := range m.unsupported {
		out[id] = reason
	}
	return out
}

// CompatibilityParameterTypes returns router-visible parameter data types from
// direct SDO mappings plus documented compatibility transforms.
func (m CANopenSDOMap) CompatibilityParameterTypes() map[int]DataType {
	out := m.ParameterTypes()
	transformIDs := make(map[int]struct{}, len(m.bridgeTransform))
	for id, transform := range m.bridgeTransform {
		if transform.Type == "" {
			continue
		}
		out[id] = transform.Type
		transformIDs[id] = struct{}{}
	}
	for id := range m.unsupported {
		if _, ok := transformIDs[id]; ok {
			continue
		}
		delete(out, id)
	}
	return out
}

// CompatibilityParameterMaxInstances returns router-visible parameter instance
// caps from direct SDO mappings plus documented compatibility transforms.
func (m CANopenSDOMap) CompatibilityParameterMaxInstances() map[int]int {
	out := m.ParameterMaxInstances()
	transformIDs := make(map[int]struct{}, len(m.bridgeTransform))
	for id, transform := range m.bridgeTransform {
		if transform.MaxInstance <= 0 {
			continue
		}
		out[id] = transform.MaxInstance
		transformIDs[id] = struct{}{}
	}
	for id := range m.unsupported {
		if _, ok := transformIDs[id]; ok {
			continue
		}
		delete(out, id)
	}
	return out
}

// CompatibilityParameterWritability returns router-visible writability flags
// from direct SDO mappings plus documented compatibility transforms.
func (m CANopenSDOMap) CompatibilityParameterWritability() map[int]bool {
	out := m.ParameterWritability()
	transformIDs := make(map[int]struct{}, len(m.bridgeTransform))
	for id, transform := range m.bridgeTransform {
		out[id] = transform.Writable
		transformIDs[id] = struct{}{}
	}
	for id := range m.unsupported {
		if _, ok := transformIDs[id]; ok {
			continue
		}
		delete(out, id)
	}
	return out
}

// CompatibilityProfile returns the external-client parameter surface captured
// in the SDO catalogue for the named compatibility profile.
func (m CANopenSDOMap) CompatibilityProfile(name string) map[int]CANopenCompatibilityParameter {
	params := m.compatibilityProfile[strings.ToLower(strings.TrimSpace(name))]
	out := make(map[int]CANopenCompatibilityParameter, len(params))
	for id, param := range params {
		out[id] = param
	}
	return out
}

// CompatibilityProfileParameterTypes returns supported parameter types from a
// named external-client compatibility profile.
func (m CANopenSDOMap) CompatibilityProfileParameterTypes(name string) map[int]DataType {
	params := m.CompatibilityProfile(name)
	out := make(map[int]DataType, len(params))
	for id, param := range params {
		if !param.Supported || param.Type == "" {
			continue
		}
		out[id] = param.Type
	}
	return out
}

// CompatibilityProfileParameterMaxInstances returns supported instance caps from
// a named external-client compatibility profile.
func (m CANopenSDOMap) CompatibilityProfileParameterMaxInstances(name string) map[int]int {
	params := m.CompatibilityProfile(name)
	out := make(map[int]int, len(params))
	for id, param := range params {
		if !param.Supported || param.MaxInstance <= 0 {
			continue
		}
		out[id] = param.MaxInstance
	}
	return out
}

// CompatibilityProfileParameterWritability returns supported writability flags
// from a named external-client compatibility profile.
func (m CANopenSDOMap) CompatibilityProfileParameterWritability(name string) map[int]bool {
	params := m.CompatibilityProfile(name)
	out := make(map[int]bool, len(params))
	for id, param := range params {
		if !param.Supported {
			continue
		}
		out[id] = param.Writable
	}
	return out
}

// CoSoCompatibilityParameters returns the CoSo parameter surface captured in
// the embedded TEC CANopen SDO source map.
func (m CANopenSDOMap) CoSoCompatibilityParameters() map[int]CANopenCompatibilityParameter {
	return m.CompatibilityProfile("coso")
}

// CoSoCompatibilityParameterTypes returns supported CoSo profile parameter data
// types captured in the embedded TEC CANopen SDO source map.
func (m CANopenSDOMap) CoSoCompatibilityParameterTypes() map[int]DataType {
	return m.CompatibilityProfileParameterTypes("coso")
}

// CoSoCompatibilityParameterMaxInstances returns supported CoSo profile
// instance caps captured in the embedded TEC CANopen SDO source map.
func (m CANopenSDOMap) CoSoCompatibilityParameterMaxInstances() map[int]int {
	return m.CompatibilityProfileParameterMaxInstances("coso")
}

// CoSoCompatibilityParameterWritability returns supported CoSo profile
// writability flags captured in the embedded TEC CANopen SDO source map.
func (m CANopenSDOMap) CoSoCompatibilityParameterWritability() map[int]bool {
	return m.CompatibilityProfileParameterWritability("coso")
}

// CANopenMappedParameterTypes returns the MeCom parameter types backed by the
// embedded TEC CANopen SDO source map.
func CANopenMappedParameterTypes() map[int]DataType {
	return defaultCANopenSDOMap.ParameterTypes()
}

// CANopenMappedParameterMaxInstances returns the MeCom parameter instance caps
// backed by the embedded TEC CANopen SDO source map.
func CANopenMappedParameterMaxInstances() map[int]int {
	return defaultCANopenSDOMap.ParameterMaxInstances()
}

// CANopenBridgeTransforms returns compatibility behaviors documented in the
// embedded TEC CANopen SDO source map.
func CANopenBridgeTransforms() map[int]BridgeTransform {
	return defaultCANopenSDOMap.BridgeTransforms()
}

// CANopenUnsupportedParameterIDs returns numeric MeCom IDs documented as
// unsupported direct SDO paths in the embedded TEC CANopen SDO source map.
func CANopenUnsupportedParameterIDs() map[int]string {
	return defaultCANopenSDOMap.UnsupportedParameterIDs()
}

// CANopenCompatibilityParameterTypes returns router-visible parameter types
// from direct mappings plus documented bridge transforms.
func CANopenCompatibilityParameterTypes() map[int]DataType {
	return defaultCANopenSDOMap.CompatibilityParameterTypes()
}

// CANopenCompatibilityParameterMaxInstances returns router-visible instance
// caps from direct mappings plus documented bridge transforms.
func CANopenCompatibilityParameterMaxInstances() map[int]int {
	return defaultCANopenSDOMap.CompatibilityParameterMaxInstances()
}

// CANopenCompatibilityParameterWritability returns router-visible writability
// from direct mappings plus documented bridge transforms.
func CANopenCompatibilityParameterWritability() map[int]bool {
	return defaultCANopenSDOMap.CompatibilityParameterWritability()
}

// CANopenCoSoCompatibilityParameters returns the CoSo compatibility profile
// captured in the embedded TEC CANopen SDO source map.
func CANopenCoSoCompatibilityParameters() map[int]CANopenCompatibilityParameter {
	return defaultCANopenSDOMap.CoSoCompatibilityParameters()
}

// CANopenCoSoCompatibilityParameterTypes returns supported CoSo profile
// parameter types captured in the embedded TEC CANopen SDO source map.
func CANopenCoSoCompatibilityParameterTypes() map[int]DataType {
	return defaultCANopenSDOMap.CoSoCompatibilityParameterTypes()
}

// CANopenCoSoCompatibilityParameterMaxInstances returns supported CoSo profile
// instance caps captured in the embedded TEC CANopen SDO source map.
func CANopenCoSoCompatibilityParameterMaxInstances() map[int]int {
	return defaultCANopenSDOMap.CoSoCompatibilityParameterMaxInstances()
}

// CANopenCoSoCompatibilityParameterWritability returns supported CoSo profile
// writability flags captured in the embedded TEC CANopen SDO source map.
func CANopenCoSoCompatibilityParameterWritability() map[int]bool {
	return defaultCANopenSDOMap.CoSoCompatibilityParameterWritability()
}

// CANopenBridgeTransform returns one compatibility behavior documented in the
// embedded TEC CANopen SDO source map.
func CANopenBridgeTransform(id int) (BridgeTransform, bool) {
	transform, ok := defaultCANopenSDOMap.bridgeTransform[id]
	return transform, ok
}

// ParameterWritability returns the parameter writability flags mapped in the SDO catalog.
func (m CANopenSDOMap) ParameterWritability() map[int]bool {
	out := make(map[int]bool, len(m.byMeComID))
	for id, mapping := range m.byMeComID {
		out[id] = mapping.writable
	}
	return out
}

// CANopenMappedParameterWritability returns the MeCom parameter writability flags backed by the
// embedded TEC CANopen SDO source map.
func CANopenMappedParameterWritability() map[int]bool {
	return defaultCANopenSDOMap.ParameterWritability()
}

var lddCANopenSDOMap = mustLoadLDDCANopenSDOMap()

func mustLoadLDDCANopenSDOMap() CANopenSDOMap {
	m, err := LoadCANopenSDOMap(lddCANopenSDOMapJSON)
	if err != nil {
		panic(err)
	}
	return m
}

// ResolveCANopenSDOMap returns the appropriate SDO map for a given device subFamily and variant.
func ResolveCANopenSDOMap(subFamily, variant string) (CANopenSDOMap, bool) {
	switch strings.ToLower(subFamily) {
	case MeerstetterSubFamilyTEC:
		return defaultCANopenSDOMap, true
	case MeerstetterSubFamilyLDD:
		if strings.Contains(strings.ToLower(variant), "130x") || variant == "" {
			return lddCANopenSDOMap, true
		}
	}
	return CANopenSDOMap{}, false
}

type sdoMapContextKey struct{}

// WithSDOMap attaches a CANopenSDOMap to the context.
func WithSDOMap(ctx context.Context, sdoMap CANopenSDOMap) context.Context {
	return context.WithValue(ctx, sdoMapContextKey{}, sdoMap)
}

// SDOMapFromContext retrieves the CANopenSDOMap from the context, if present.
func SDOMapFromContext(ctx context.Context) (CANopenSDOMap, bool) {
	m, ok := ctx.Value(sdoMapContextKey{}).(CANopenSDOMap)
	return m, ok
}
