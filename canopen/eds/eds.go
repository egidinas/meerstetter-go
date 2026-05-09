package eds

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/egidinas/meerstetter-go/objectdict"
)

// Parse reads a CANopen EDS file and returns a semantic object dictionary.
func Parse(r io.Reader) (*objectdict.Dictionary, error) {
	sections, order, err := parseINI(r)
	if err != nil {
		return nil, err
	}

	deviceInfo := sections["DeviceInfo"]
	dict := &objectdict.Dictionary{
		SchemaVersion: 1,
		Protocol:      objectdict.ProtocolCANopen,
		DefinitionID:  deviceInfo["ProductName"],
		Device: objectdict.Device{
			Vendor:        deviceInfo["VendorName"],
			VendorNumber:  deviceInfo["VendorNumber"],
			Product:       deviceInfo["ProductName"],
			ProductNumber: deviceInfo["ProductNumber"],
			Revision:      deviceInfo["RevisionNumber"],
		},
		Metadata: map[string]string{},
	}
	if fileInfo := sections["FileInfo"]; fileInfo != nil {
		for _, key := range []string{"FileName", "FileVersion", "FileRevision", "EDSVersion", "Description", "ModificationDate"} {
			if fileInfo[key] != "" {
				dict.Metadata[key] = fileInfo[key]
			}
		}
	}
	if len(dict.Metadata) == 0 {
		dict.Metadata = nil
	}

	objectMap := map[uint16]*objectdict.Object{}
	for _, sectionName := range order {
		index, subIndex, isObject, isSub := parseObjectSection(sectionName)
		if !isObject {
			continue
		}
		section := sections[sectionName]
		obj := objectMap[index]
		if obj == nil {
			obj = &objectdict.Object{
				ID:    fmt.Sprintf("canopen:0x%04X", index),
				Index: index,
				Name:  firstNonEmpty(section["ParameterName"], section["ObjectName"], fmt.Sprintf("0x%04X", index)),
			}
			objectMap[index] = obj
		}
		if !isSub {
			obj.Name = firstNonEmpty(section["ParameterName"], section["ObjectName"], obj.Name)
			obj.Metadata = copySelected(section, "ObjectType", "ObjFlags", "CompactSubObj")
			if section["DataType"] != "" || section["AccessType"] != "" {
				obj.Entries = append(obj.Entries, entryFromSection(index, 0, section))
			}
			continue
		}
		if subIndex == 0 {
			continue
		}
		obj.Entries = append(obj.Entries, entryFromSection(index, subIndex, section))
	}

	indexes := make([]int, 0, len(objectMap))
	for index := range objectMap {
		indexes = append(indexes, int(index))
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		obj := objectMap[uint16(index)]
		sort.Slice(obj.Entries, func(i, j int) bool {
			return obj.Entries[i].SubIndex < obj.Entries[j].SubIndex
		})
		dict.Objects = append(dict.Objects, *obj)
	}
	return dict, nil
}

func parseINI(r io.Reader) (map[string]map[string]string, []string, error) {
	sections := map[string]map[string]string{}
	var order []string
	current := ""
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			current = strings.TrimSpace(line[1:strings.Index(line, "]")])
			if _, ok := sections[current]; !ok {
				sections[current] = map[string]string{}
				order = append(order, current)
			}
			continue
		}
		if current == "" {
			return nil, nil, fmt.Errorf("eds: key outside section at line %d", lineNo)
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		sections[current][strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return sections, order, nil
}

func parseObjectSection(name string) (uint16, uint8, bool, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	if len(lower) < 4 {
		return 0, 0, false, false
	}
	if subAt := strings.Index(lower, "sub"); subAt > 0 {
		index64, err := strconv.ParseUint(lower[:subAt], 16, 16)
		if err != nil {
			return 0, 0, false, false
		}
		sub64, err := strconv.ParseUint(lower[subAt+3:], 16, 8)
		if err != nil {
			return 0, 0, false, false
		}
		return uint16(index64), uint8(sub64), true, true
	}
	index64, err := strconv.ParseUint(lower, 16, 16)
	if err != nil {
		return 0, 0, false, false
	}
	return uint16(index64), 0, true, false
}

func entryFromSection(index uint16, subIndex uint8, section map[string]string) objectdict.Entry {
	dataType := strings.ToLower(section["DataType"])
	entry := objectdict.Entry{
		ID:       fmt.Sprintf("canopen:0x%04X:0x%02X", index, subIndex),
		Index:    index,
		SubIndex: subIndex,
		Name:     firstNonEmpty(section["ParameterName"], section["ObjectName"], fmt.Sprintf("0x%04X:%02X", index, subIndex)),
		Unit:     section["Unit"],
		DataType: dataType,
		Kind:     kindFromDataType(dataType),
		Access:   accessFromEDS(section["AccessType"]),
		Metadata: copySelected(section, "PDOMapping", "DefaultValue", "LowLimit", "HighLimit", "ObjFlags"),
	}
	entry.Min = parseFloatPtr(section["LowLimit"])
	entry.Max = parseFloatPtr(section["HighLimit"])
	if len(entry.Metadata) == 0 {
		entry.Metadata = nil
	}
	return entry
}

func accessFromEDS(v string) objectdict.Access {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "ro", "const":
		return objectdict.AccessReadOnly
	case "wo":
		return objectdict.AccessWriteOnly
	case "rw", "rwr", "rww":
		return objectdict.AccessReadWrite
	default:
		return objectdict.AccessUnknown
	}
}

func kindFromDataType(dataType string) objectdict.ValueKind {
	switch dataType {
	case "0x0001":
		return objectdict.ValueKindBoolean
	case "0x0009", "0x000a", "0x000b":
		return objectdict.ValueKindString
	case "0x0002", "0x0003", "0x0004", "0x0005", "0x0006", "0x0007", "0x0008", "0x0010", "0x0011", "0x0012", "0x0015", "0x001b":
		return objectdict.ValueKindContinuous
	default:
		return objectdict.ValueKindUnknown
	}
}

func parseFloatPtr(v string) *float64 {
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return nil
	}
	return &f
}

func copySelected(src map[string]string, keys ...string) map[string]string {
	dst := map[string]string{}
	for _, key := range keys {
		if src[key] != "" {
			dst[key] = src[key]
		}
	}
	return dst
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
