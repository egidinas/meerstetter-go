package mecomdict

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/egidinas/meerstetter-go/objectdict"
)

const DefaultParameterRegistryEnv = "MECOM_PARAMETER_REGISTRY"

func DefaultParameterRegistryPath() string {
	return strings.TrimSpace(os.Getenv(DefaultParameterRegistryEnv))
}

// ParameterDef is the compact MeCom parameter shape extracted from the
// official pyMeCom parameter registry.
type ParameterDef struct {
	ID         int
	Name       string
	Format     string
	Enum       map[int64]string
	Families   []string
	SourceList string
}

var parameterRE = regexp.MustCompile(`\{ID:\s*([0-9]+),\s*Name:\s*"([^"]+)",\s*Format:\s*"([^"]+)"\}`)
var parameterBlockRE = regexp.MustCompile(`(?s)var\s+([A-Za-z0-9_]+)\s*=\s*\[\]ParameterDef\s*\{(.*?)\n\}`)
var errorRE = regexp.MustCompile(`\{Code:\s*([0-9]+),\s*Symbol:\s*"([^"]+)",\s*Description:\s*"([^"]+)"\}`)

const (
	FamilyTEC     = "TEC"
	FamilyLDD112x = "LDD_112x"
	FamilyLDD130x = "LDD_130x"
	FamilyLDD1321 = "LDD_1321"
)

// LoadParameterRegistry loads generated Go parameter definitions without
// depending on the generated file as a package.
func LoadParameterRegistry(path string) ([]ParameterDef, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	enums := enumsFromRegistry(string(raw))
	blocks, err := parameterBlocks(string(raw), enums)
	if err != nil {
		return nil, err
	}
	if len(blocks) > 0 {
		return mergeParameterBlocks(blocks), nil
	}
	seen := map[int]ParameterDef{}
	for _, match := range parameterRE.FindAllStringSubmatch(string(raw), -1) {
		id, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("mecomdict: parse parameter id %q: %w", match[1], err)
		}
		seen[id] = ParameterDef{ID: id, Name: match[2], Format: strings.ToUpper(match[3]), Enum: enumFor(id, match[2], enums)}
	}
	return sortedParameters(seen), nil
}

// LoadParameterRegistryForFamily loads only parameters that belong to a
// controller family such as TEC or LDD_130x. It falls back to the full registry
// when the source file has no list-level family information.
func LoadParameterRegistryForFamily(path, family string) ([]ParameterDef, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw := string(rawBytes)
	enums := enumsFromRegistry(raw)
	blocks, err := parameterBlocks(raw, enums)
	if err != nil {
		return nil, err
	}
	family = strings.ToUpper(strings.TrimSpace(family))
	if len(blocks) == 0 || family == "" {
		return LoadParameterRegistry(path)
	}
	filtered := make([]parameterBlock, 0, len(blocks))
	for _, block := range blocks {
		if strings.ToUpper(block.Family) == family {
			filtered = append(filtered, block)
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	return mergeParameterBlocks(filtered), nil
}

type parameterBlock struct {
	ListName string
	Family   string
	Params   []ParameterDef
}

func parameterBlocks(raw string, enums map[int]map[int64]string) ([]parameterBlock, error) {
	matches := parameterBlockRE.FindAllStringSubmatch(raw, -1)
	blocks := make([]parameterBlock, 0, len(matches))
	for index, match := range matches {
		listName := match[1]
		params, err := parametersFromBlock(match[2], listName, enums)
		if err != nil {
			return nil, err
		}
		if len(params) == 0 {
			continue
		}
		family := inferFamily(listName, params)
		if family == "" {
			family = fmt.Sprintf("REGISTRY_%d", index+1)
		}
		for i := range params {
			params[i].SourceList = listName
			params[i].Families = []string{family}
		}
		blocks = append(blocks, parameterBlock{ListName: listName, Family: family, Params: params})
	}
	return blocks, nil
}

func parametersFromBlock(raw, listName string, enums map[int]map[int64]string) ([]ParameterDef, error) {
	matches := parameterRE.FindAllStringSubmatch(raw, -1)
	params := make([]ParameterDef, 0, len(matches))
	for _, match := range matches {
		id, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("mecomdict: parse %s parameter id %q: %w", listName, match[1], err)
		}
		name := match[2]
		params = append(params, ParameterDef{ID: id, Name: name, Format: strings.ToUpper(match[3]), Enum: enumFor(id, name, enums)})
	}
	return params, nil
}

func inferFamily(listName string, params []ParameterDef) string {
	upper := strings.ToUpper(listName)
	switch {
	case strings.HasPrefix(upper, "TEC_"):
		return FamilyTEC
	case strings.Contains(upper, "LDD_112"):
		return FamilyLDD112x
	case strings.Contains(upper, "LDD_130"):
		return FamilyLDD130x
	case strings.Contains(upper, "LDD_1321"):
		return FamilyLDD1321
	}
	names := strings.ToLower(listName)
	for _, param := range params {
		names += "\n" + strings.ToLower(param.Name)
	}
	switch {
	case strings.Contains(names, "actual anode voltage") || strings.Contains(names, "cathode"):
		return FamilyLDD1321
	case strings.Contains(names, "photodiode input") || strings.Contains(names, "phase current x") || strings.Contains(names, "driver input voltage"):
		return FamilyLDD130x
	case strings.Contains(names, "laser diode current") || strings.Contains(names, "ldd"):
		return FamilyLDD112x
	case strings.Contains(names, "object temperature") || strings.Contains(names, "target object temperature") || strings.Contains(names, "sink temperature"):
		return FamilyTEC
	default:
		return ""
	}
}

func mergeParameterBlocks(blocks []parameterBlock) []ParameterDef {
	seen := map[int]ParameterDef{}
	for _, block := range blocks {
		for _, param := range block.Params {
			if existing, ok := seen[param.ID]; ok {
				param.Families = mergeStrings(existing.Families, param.Families)
				if existing.SourceList != "" && !strings.Contains(existing.SourceList, param.SourceList) {
					param.SourceList = existing.SourceList + "," + param.SourceList
				} else if param.SourceList == "" {
					param.SourceList = existing.SourceList
				}
			}
			seen[param.ID] = param
		}
	}
	return sortedParameters(seen)
}

func mergeStrings(left, right []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(left)+len(right))
	for _, value := range append(left, right...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func sortedParameters(seen map[int]ParameterDef) []ParameterDef {
	params := make([]ParameterDef, 0, len(seen))
	for _, param := range seen {
		params = append(params, param)
	}
	sort.Slice(params, func(i, j int) bool {
		if params[i].ID == params[j].ID {
			return params[i].Name < params[j].Name
		}
		return params[i].ID < params[j].ID
	})
	return params
}

func enumsFromRegistry(raw string) map[int]map[int64]string {
	out := map[int]map[int64]string{}
	errorEnum := map[int64]string{0: "No error"}
	for _, match := range errorRE.FindAllStringSubmatch(raw, -1) {
		code, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			continue
		}
		errorEnum[code] = match[2] + " - " + match[3]
	}
	if len(errorEnum) > 1 {
		out[105] = errorEnum
	}
	return out
}

func enumFor(id int, name string, registryEnums map[int]map[int64]string) map[int64]string {
	if enum := registryEnums[id]; len(enum) > 0 {
		return enum
	}
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "enable") || strings.Contains(lower, "enabled"):
		return map[int64]string{0: "Disabled", 1: "Enabled"}
	case strings.Contains(lower, "temperature is stable"):
		return map[int64]string{0: "Not stable", 1: "Stable"}
	case strings.Contains(lower, "save data to flash"):
		return map[int64]string{0: "Idle", 1: "Save to flash"}
	case strings.Contains(lower, "flash status"):
		return map[int64]string{0: "Ready", 1: "Busy"}
	default:
		return nil
	}
}

func UnitForName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "temperature"):
		return "degC"
	case strings.Contains(lower, "voltage"):
		return "V"
	case strings.Contains(lower, "current"):
		return "A"
	case strings.Contains(lower, "power"):
		return "W"
	case strings.Contains(lower, "frequency"):
		return "Hz"
	case strings.Contains(lower, "time"):
		return "s"
	default:
		return ""
	}
}

func KindFor(param ParameterDef) objectdict.ValueKind {
	name := strings.ToLower(param.Name)
	format := strings.ToUpper(param.Format)
	switch {
	case format == "FLOAT32" || format == "FLOAT64":
		return objectdict.ValueKindContinuous
	case strings.Contains(name, "enable") || strings.Contains(name, "enabled"):
		return objectdict.ValueKindBoolean
	case strings.Contains(name, "status") || strings.Contains(name, "state") || strings.Contains(name, "mode") || strings.Contains(name, "error") || strings.Contains(name, "warning") || strings.Contains(name, "event"):
		return objectdict.ValueKindEnum
	case strings.Contains(format, "STRING"):
		return objectdict.ValueKindString
	default:
		return objectdict.ValueKindContinuous
	}
}

func DescriptionFor(param ParameterDef) string {
	name := strings.TrimSpace(param.Name)
	if name == "" {
		name = fmt.Sprintf("Parameter %d", param.ID)
	}
	format := strings.ToUpper(strings.TrimSpace(param.Format))
	parts := []string{name}
	if unit := UnitForName(param.Name); unit != "" {
		parts = append(parts, "unit "+unit)
	}
	if format != "" {
		parts = append(parts, fmt.Sprintf("MeCom %d %s", param.ID, format))
	} else {
		parts = append(parts, fmt.Sprintf("MeCom %d", param.ID))
	}
	parts = append(parts, "kind "+string(KindFor(param)))
	if len(param.Enum) > 0 {
		parts = append(parts, "enumerated values available")
	}
	return strings.Join(parts, "; ")
}

func CategoryForName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "temperature") || strings.Contains(lower, "temp"):
		return "Temperature"
	case strings.Contains(lower, "power") || strings.Contains(lower, "current") || strings.Contains(lower, "voltage"):
		return "Power and Output"
	case strings.Contains(lower, "target") || strings.Contains(lower, "limit") || strings.Contains(lower, "pid") || strings.Contains(lower, "control") || strings.Contains(lower, "mode") || strings.Contains(lower, "enable"):
		return "Control"
	case strings.Contains(lower, "status") || strings.Contains(lower, "state") || strings.Contains(lower, "error") || strings.Contains(lower, "warning") || strings.Contains(lower, "event") || strings.Contains(lower, "alarm"):
		return "Status and Events"
	case strings.Contains(lower, "firmware") || strings.Contains(lower, "hardware") || strings.Contains(lower, "serial") || strings.Contains(lower, "device") || strings.Contains(lower, "version"):
		return "Device Metadata"
	default:
		return "Other Signals"
	}
}

func DictionaryFromParameters(params []ParameterDef) objectdict.Dictionary {
	objects := make([]objectdict.Object, 0, len(params))
	for _, param := range params {
		id := fmt.Sprintf("mecom:%d", param.ID)
		objects = append(objects, objectdict.Object{
			ID:    id,
			Index: uint16(param.ID),
			Name:  param.Name,
			Entries: []objectdict.Entry{{
				ID:       id,
				Index:    uint16(param.ID),
				Name:     param.Name,
				Unit:     UnitForName(param.Name),
				DataType: strings.ToUpper(param.Format),
				Kind:     KindFor(param),
				Access:   objectdict.AccessReadWrite,
				Enum:     param.Enum,
				Metadata: map[string]string{
					"parameter_id": strconv.Itoa(param.ID),
					"category":     CategoryForName(param.Name),
				},
			}},
		})
	}
	return objectdict.Dictionary{
		SchemaVersion: 1,
		Protocol:      objectdict.ProtocolMeCom,
		DefinitionID:  "mecom-pymecom-parameters",
		Device: objectdict.Device{
			Vendor:  "Meerstetter",
			Product: "TEC/LDD controller family",
		},
		Objects: objects,
		Metadata: map[string]string{
			"source": "pyMeCom parameter registry",
		},
	}
}
