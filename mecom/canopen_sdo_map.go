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
	SchemaVersion string                    `json:"schema_version"`
	SourcePolicy  string                    `json:"source_policy"`
	Mappings      []canopenSDOMappingSource `json:"mappings"`
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

type canopenSDOMap struct {
	byMeComID map[int]canopenSDOMapping
}

type canopenSDOMapping struct {
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

func mustLoadCANopenSDOMap() canopenSDOMap {
	m, err := loadCANopenSDOMap(tecCANopenSDOMapJSON)
	if err != nil {
		panic(err)
	}
	return m
}

func loadCANopenSDOMap(raw []byte) (canopenSDOMap, error) {
	var source canopenSDOMapSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return canopenSDOMap{}, err
	}
	if source.SchemaVersion != "mecom_tec_canopen_sdo_map.v1" {
		return canopenSDOMap{}, fmt.Errorf("unexpected CANopen SDO map schema %q", source.SchemaVersion)
	}
	out := canopenSDOMap{byMeComID: make(map[int]canopenSDOMapping, len(source.Mappings))}
	for _, entry := range source.Mappings {
		if entry.MeComID <= 0 {
			return canopenSDOMap{}, fmt.Errorf("invalid MeCom ID %d", entry.MeComID)
		}
		if _, exists := out.byMeComID[entry.MeComID]; exists {
			return canopenSDOMap{}, fmt.Errorf("duplicate MeCom ID %d", entry.MeComID)
		}
		if len(entry.SourceEvidence) == 0 {
			return canopenSDOMap{}, fmt.Errorf("MeCom ID %d has no source evidence", entry.MeComID)
		}
		mapping, err := buildCANopenSDOMapping(entry)
		if err != nil {
			return canopenSDOMap{}, fmt.Errorf("MeCom ID %d: %w", entry.MeComID, err)
		}
		out.byMeComID[entry.MeComID] = mapping
	}
	return out, nil
}

func buildCANopenSDOMapping(entry canopenSDOMappingSource) (canopenSDOMapping, error) {
	index, err := parseCANopenMapNumber(entry.CANopen.Index, 16)
	if err != nil {
		return canopenSDOMapping{}, fmt.Errorf("parse CANopen index %q: %w", entry.CANopen.Index, err)
	}
	kind, err := canopenSDOMapDataType(entry.ValueType, entry.CANopen.DataType)
	if err != nil {
		return canopenSDOMapping{}, err
	}
	mapping := canopenSDOMapping{
		index:       uint16(index),
		kind:        kind,
		writable:    strings.Contains(strings.ToLower(entry.Access), "w"),
		minInstance: entry.Instances.Min,
		maxInstance: entry.Instances.Max,
	}
	switch strings.ToLower(entry.Instances.Mode) {
	case "fixed":
		if entry.Instances.Fixed <= 0 || entry.Instances.Fixed > 0xff {
			return canopenSDOMapping{}, fmt.Errorf("invalid fixed instance %d", entry.Instances.Fixed)
		}
		subIndex, err := parseCANopenMapNumber(entry.CANopen.Subindex, 8)
		if err != nil {
			return canopenSDOMapping{}, fmt.Errorf("parse CANopen subindex %q: %w", entry.CANopen.Subindex, err)
		}
		mapping.fixedInstance = entry.Instances.Fixed
		mapping.fixedSubIndex = byte(subIndex)
	case "subindex":
		if entry.CANopen.SubindexMode != "instance" || entry.CANopen.Subindex != "instance" {
			return canopenSDOMapping{}, fmt.Errorf("subindex mapping must use instance subindex")
		}
		if mapping.minInstance == 0 {
			mapping.minInstance = 1
		}
		if mapping.maxInstance == 0 {
			mapping.maxInstance = 0xff
		}
		if mapping.minInstance <= 0 || mapping.maxInstance > 0xff || mapping.minInstance > mapping.maxInstance {
			return canopenSDOMapping{}, fmt.Errorf("invalid instance range %d..%d", mapping.minInstance, mapping.maxInstance)
		}
		mapping.subIndexFromInstance = true
	default:
		return canopenSDOMapping{}, fmt.Errorf("unknown instance mode %q", entry.Instances.Mode)
	}
	return mapping, nil
}

func canopenSDOMapDataType(valueType DataType, dataTypeCode string) (DataType, error) {
	switch DataType(strings.ToLower(string(valueType))) {
	case DataTypeInt32:
		return DataTypeInt32, nil
	case DataTypeFloat32:
		return DataTypeFloat32, nil
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

func parseCANopenMapNumber(value string, bitSize int) (uint64, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "0x") {
		return strconv.ParseUint(value[2:], 16, bitSize)
	}
	return strconv.ParseUint(value, 10, bitSize)
}

func (m canopenSDOMap) objectForMeCom(paramID, instance int) (canopenSDOObject, bool) {
	if instance <= 0 || instance > 0xff {
		return canopenSDOObject{}, false
	}
	mapping, ok := m.byMeComID[paramID]
	if !ok {
		return canopenSDOObject{}, false
	}
	object := canopenSDOObject{
		index:    mapping.index,
		kind:     mapping.kind,
		writable: mapping.writable,
	}
	if mapping.subIndexFromInstance {
		if instance < mapping.minInstance || instance > mapping.maxInstance {
			return canopenSDOObject{}, false
		}
		object.subIndex = byte(instance)
		return object, true
	}
	if instance != mapping.fixedInstance {
		return canopenSDOObject{}, false
	}
	object.subIndex = mapping.fixedSubIndex
	return object, true
}

func (m canopenSDOMap) parameterTypes() map[int]DataType {
	out := make(map[int]DataType, len(m.byMeComID))
	for id, mapping := range m.byMeComID {
		out[id] = mapping.kind
	}
	return out
}

// CANopenMappedParameterTypes returns the MeCom parameter types backed by the
// embedded TEC CANopen SDO source map.
func CANopenMappedParameterTypes() map[int]DataType {
	return defaultCANopenSDOMap.parameterTypes()
}
