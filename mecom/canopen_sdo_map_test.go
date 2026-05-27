package mecom

import (
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestCANopenBridgeTransformsExposeCompatibilityRuntime(t *testing.T) {
	transforms := CANopenBridgeTransforms()

	firmware, ok := transforms[112]
	if !ok {
		t.Fatal("missing firmware float bridge transform for MeCom ID 112")
	}
	if firmware.Type != DataTypeFloat32 {
		t.Fatalf("firmware transform type = %q, want %q", firmware.Type, DataTypeFloat32)
	}
	if firmware.Kind != "synthesize_float32_from_int32" {
		t.Fatalf("firmware transform kind = %q, want synthesize_float32_from_int32", firmware.Kind)
	}
	if firmware.SourceMeComID != 103 || firmware.Scale != 0.01 {
		t.Fatalf("firmware transform source/scale = %d/%f, want 103/0.01", firmware.SourceMeComID, firmware.Scale)
	}

	startup, ok := transforms[115]
	if !ok {
		t.Fatal("missing random startup bridge transform for MeCom ID 115")
	}
	if startup.Type != DataTypeInt32 {
		t.Fatalf("startup transform type = %q, want %q", startup.Type, DataTypeInt32)
	}
	if startup.Kind != "mask_int32" || startup.Int32Mask != 0x00FFFFFF {
		t.Fatalf("startup transform kind/mask = %q/0x%08X, want mask_int32/0x00FFFFFF", startup.Kind, startup.Int32Mask)
	}

	flashStatus, ok := transforms[108]
	if !ok {
		t.Fatal("missing CoSo flash-status bridge transform for MeCom ID 108")
	}
	if flashStatus.Type != DataTypeInt32 {
		t.Fatalf("flash-status transform type = %q, want %q", flashStatus.Type, DataTypeInt32)
	}
	if flashStatus.Kind != "constant_int32" || flashStatus.Int32Value != 0 {
		t.Fatalf("flash-status transform kind/value = %q/%d, want constant_int32/0", flashStatus.Kind, flashStatus.Int32Value)
	}
	if flashStatus.MinInstance != 1 || flashStatus.MaxInstance != 1 {
		t.Fatalf("flash-status transform instance range = %d..%d, want 1..1", flashStatus.MinInstance, flashStatus.MaxInstance)
	}

	notes, ok := transforms[120]
	if !ok {
		t.Fatal("missing User Notes bridge transform for MeCom ID 120")
	}
	if notes.Type != DataTypeLatin1 {
		t.Fatalf("notes transform type = %q, want %q", notes.Type, DataTypeLatin1)
	}
	if notes.Kind != "latin1_big_data" {
		t.Fatalf("notes transform kind = %q, want latin1_big_data", notes.Kind)
	}
	if !notes.Writable {
		t.Fatal("notes transform writable = false, want catalogue read/write access")
	}

	freertos, ok := transforms[217]
	if !ok {
		t.Fatal("missing FreeRTOS statistics bridge transform for MeCom ID 217")
	}
	if freertos.Type != DataTypeLatin1 {
		t.Fatalf("FreeRTOS transform type = %q, want %q", freertos.Type, DataTypeLatin1)
	}
	if freertos.Kind != "latin1_big_data" {
		t.Fatalf("FreeRTOS transform kind = %q, want latin1_big_data", freertos.Kind)
	}

	stable, ok := transforms[1200]
	if !ok {
		t.Fatal("missing temperature stable bridge transform for MeCom ID 1200")
	}
	if stable.Type != DataTypeInt32 {
		t.Fatalf("stable transform type = %q, want %q", stable.Type, DataTypeInt32)
	}
	if stable.Kind != "constant_int32" || stable.Int32Value != 0 {
		t.Fatalf("stable transform kind/value = %q/%d, want constant_int32/0", stable.Kind, stable.Int32Value)
	}
	if stable.MaxInstance != 4 {
		t.Fatalf("stable transform max instance = %d, want 4", stable.MaxInstance)
	}

	objectExternalTemperature, ok := transforms[52200]
	if !ok {
		t.Fatal("missing Object External Temperature bridge transform for MeCom ID 52200")
	}
	if objectExternalTemperature.Type != DataTypeFloat32 {
		t.Fatalf("Object External Temperature transform type = %q, want %q", objectExternalTemperature.Type, DataTypeFloat32)
	}
	if objectExternalTemperature.Kind != "virtual_parameter" {
		t.Fatalf("Object External Temperature transform kind = %q, want virtual_parameter", objectExternalTemperature.Kind)
	}
	if !objectExternalTemperature.Writable {
		t.Fatal("Object External Temperature transform writable = false, want catalogue read/write access")
	}
	if objectExternalTemperature.MinInstance != 1 || objectExternalTemperature.MaxInstance != 1 {
		t.Fatalf("Object External Temperature transform instance range = %d..%d, want 1..1", objectExternalTemperature.MinInstance, objectExternalTemperature.MaxInstance)
	}

	sinkFixedTemperature, ok := transforms[52201]
	if !ok {
		t.Fatal("missing Sink Fixed Temperature bridge transform for MeCom ID 52201")
	}
	if sinkFixedTemperature.Type != DataTypeFloat32 {
		t.Fatalf("Sink Fixed Temperature transform type = %q, want %q", sinkFixedTemperature.Type, DataTypeFloat32)
	}
	if sinkFixedTemperature.Kind != "virtual_parameter" {
		t.Fatalf("Sink Fixed Temperature transform kind = %q, want virtual_parameter", sinkFixedTemperature.Kind)
	}
	if !sinkFixedTemperature.Writable {
		t.Fatal("Sink Fixed Temperature transform writable = false, want catalogue read/write access")
	}
	if sinkFixedTemperature.MinInstance != 1 || sinkFixedTemperature.MaxInstance != 4 {
		t.Fatalf("Sink Fixed Temperature transform instance range = %d..%d, want 1..4", sinkFixedTemperature.MinInstance, sinkFixedTemperature.MaxInstance)
	}
}

func TestCANopenCoSoCompatibilityParametersLoadFromCatalogue(t *testing.T) {
	params := CANopenCoSoCompatibilityParameters()
	if len(params) == 0 {
		t.Fatal("CoSo compatibility parameter surface is empty")
	}
	for _, id := range []int{100, 1044, 1045, 1200, 53184, 65100} {
		param, ok := params[id]
		if !ok {
			t.Fatalf("CoSo compatibility surface missing MeCom ID %d", id)
		}
		if param.MeComID != id {
			t.Fatalf("CoSo compatibility parameter key %d has MeComID %d", id, param.MeComID)
		}
		if param.MaxInstance <= 0 {
			t.Fatalf("CoSo compatibility parameter %d has invalid max instance %d", id, param.MaxInstance)
		}
	}
	if got := params[1200].MaxInstance; got != 4 {
		t.Fatalf("CoSo compatibility max instance for 1200 = %d, want observed batch coverage through instance 4", got)
	}
}

func TestCANopenCoSoCompatibilityProfileCoversNormalTECReadWriteCatalogue(t *testing.T) {
	params := CANopenCoSoCompatibilityParameters()
	directTypes := CANopenMappedParameterTypes()
	transforms := CANopenBridgeTransforms()
	unsupported := CANopenUnsupportedParameterIDs()

	expected := map[int]mecomTECParameter{}
	for _, param := range mecomTECParameters() {
		expected[param.ID] = param
	}
	for _, param := range mecomTECWriteParameters() {
		expected[param.ID] = param
	}

	missing := []int{}
	for id, catalogueParam := range expected {
		profileParam, ok := params[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		if profileParam.MaxInstance <= 0 {
			t.Fatalf("CoSo profile parameter %d has invalid max instance %d", id, profileParam.MaxInstance)
		}
		if transform, ok := transforms[id]; ok {
			if !profileParam.Supported || profileParam.Translation != canopenCompatibilityTranslationBridgeTransform {
				t.Fatalf("CoSo profile parameter %d transform support = %v/%q, want bridge_transform", id, profileParam.Supported, profileParam.Translation)
			}
			if profileParam.Type != transform.Type {
				t.Fatalf("CoSo profile parameter %d type = %q, want transform type %q", id, profileParam.Type, transform.Type)
			}
			continue
		}
		if _, ok := unsupported[id]; ok {
			if profileParam.Supported {
				t.Fatalf("CoSo profile parameter %d is explicitly unsupported but marked supported", id)
			}
			if profileParam.Translation != canopenCompatibilityTranslationUnsupported {
				t.Fatalf("CoSo profile parameter %d translation = %q, want unsupported", id, profileParam.Translation)
			}
			continue
		}
		if directType, ok := directTypes[id]; ok {
			if !profileParam.Supported || profileParam.Translation != canopenCompatibilityTranslationDirectMapping {
				t.Fatalf("CoSo profile parameter %d direct support = %v/%q, want direct_mapping", id, profileParam.Supported, profileParam.Translation)
			}
			if profileParam.Type != directType {
				t.Fatalf("CoSo profile parameter %d type = %q, want direct type %q", id, profileParam.Type, directType)
			}
			if catalogueParam.Writable && !profileParam.Writable {
				t.Fatalf("CoSo profile parameter %d is writable in TEC catalogue but not router profile", id)
			}
			continue
		}
		t.Fatalf("CoSo profile parameter %d has no direct mapping, bridge transform, or unsupported declaration", id)
	}
	if len(missing) > 0 {
		sort.Ints(missing)
		t.Fatalf("CoSo profile missing normal TEC read/write parameters: %v", missing)
	}
}

func TestCANopenCoSoCompatibilityProfileResolvesTranslationMetadata(t *testing.T) {
	params := CANopenCoSoCompatibilityParameters()

	tests := []struct {
		id          int
		typ         DataType
		writable    bool
		supported   bool
		translation string
		maxInstance int
	}{
		{id: 104, typ: DataTypeInt32, writable: false, supported: true, translation: "direct_mapping", maxInstance: 1},
		{id: 108, typ: DataTypeInt32, writable: false, supported: true, translation: "bridge_transform", maxInstance: 1},
		{id: 1045, typ: DataTypeFloat32, writable: false, supported: true, translation: "direct_mapping", maxInstance: 1},
		{id: 1063, typ: DataTypeFloat32, writable: false, supported: true, translation: "direct_mapping", maxInstance: 1},
		{id: 1102, typ: DataTypeFloat32, writable: false, supported: true, translation: "direct_mapping", maxInstance: 2},
		{id: 120, typ: DataTypeLatin1, writable: true, supported: true, translation: "bridge_transform", maxInstance: 1},
		{id: 1200, typ: DataTypeInt32, writable: false, supported: true, translation: "bridge_transform", maxInstance: 4},
		{id: 2010, typ: DataTypeInt32, writable: true, supported: true, translation: "direct_mapping", maxInstance: 1},
		{id: 52200, typ: DataTypeFloat32, writable: true, supported: true, translation: "bridge_transform", maxInstance: 1},
		{id: 52201, typ: DataTypeFloat32, writable: true, supported: true, translation: "bridge_transform", maxInstance: 4},
		{id: 53020, typ: DataTypeInt32, writable: false, supported: true, translation: "direct_mapping", maxInstance: 1},
		{id: 6200, typ: DataTypeInt32, writable: true, supported: true, translation: "direct_mapping", maxInstance: 2},
		{id: 65100, typ: "", writable: false, supported: false, translation: "unsupported", maxInstance: 5},
	}

	for _, tc := range tests {
		t.Run(strconv.Itoa(tc.id), func(t *testing.T) {
			param, ok := params[tc.id]
			if !ok {
				t.Fatalf("CoSo compatibility surface missing MeCom ID %d", tc.id)
			}
			if param.Type != tc.typ {
				t.Fatalf("parameter %d type = %q, want %q", tc.id, param.Type, tc.typ)
			}
			if param.Writable != tc.writable {
				t.Fatalf("parameter %d writable = %v, want %v", tc.id, param.Writable, tc.writable)
			}
			if param.Supported != tc.supported {
				t.Fatalf("parameter %d supported = %v, want %v", tc.id, param.Supported, tc.supported)
			}
			if param.Translation != tc.translation {
				t.Fatalf("parameter %d translation = %q, want %q", tc.id, param.Translation, tc.translation)
			}
			if param.MaxInstance != tc.maxInstance {
				t.Fatalf("parameter %d max instance = %d, want %d", tc.id, param.MaxInstance, tc.maxInstance)
			}
		})
	}
}

func TestCANopenCoSoCompatibilityMetadataUsesProfileSurface(t *testing.T) {
	types := CANopenCoSoCompatibilityParameterTypes()
	if got := types[102]; got != DataTypeInt32 {
		t.Fatalf("CoSo profile type for serial number 102 = %q, want int32", got)
	}
	if got := types[104]; got != DataTypeInt32 {
		t.Fatalf("CoSo profile type for device status 104 = %q, want int32", got)
	}
	if got := types[108]; got != DataTypeInt32 {
		t.Fatalf("CoSo profile type for transform 108 = %q, want int32", got)
	}
	if got := types[1045]; got != DataTypeFloat32 {
		t.Fatalf("CoSo profile type for 1045 = %q, want float32", got)
	}
	if got := types[1063]; got != DataTypeFloat32 {
		t.Fatalf("CoSo profile type for device temperature 1063 = %q, want float32", got)
	}
	if got := types[1102]; got != DataTypeFloat32 {
		t.Fatalf("CoSo profile type for fan actual speed 1102 = %q, want float32", got)
	}
	if got := types[53020]; got != DataTypeInt32 {
		t.Fatalf("CoSo profile type for unipolar license status 53020 = %q, want int32", got)
	}
	if got := types[6200]; got != DataTypeInt32 {
		t.Fatalf("CoSo profile type for fan enable 6200 = %q, want int32", got)
	}
	if got := types[120]; got != DataTypeLatin1 {
		t.Fatalf("CoSo profile type for transform 120 = %q, want latin1", got)
	}
	if got := types[1200]; got != DataTypeInt32 {
		t.Fatalf("CoSo profile type for transform 1200 = %q, want int32", got)
	}
	if got := types[52200]; got != DataTypeFloat32 {
		t.Fatalf("CoSo profile type for transform 52200 = %q, want float32", got)
	}
	if got := types[52201]; got != DataTypeFloat32 {
		t.Fatalf("CoSo profile type for transform 52201 = %q, want float32", got)
	}
	if got := types[1000]; got != DataTypeFloat32 {
		t.Fatalf("CoSo profile type for normal TEC readout 1000 = %q, want float32", got)
	}
	if got := types[2033]; got != DataTypeFloat32 {
		t.Fatalf("CoSo profile type for normal TEC write parameter 2033 = %q, want float32", got)
	}
	if _, ok := types[65100]; ok {
		t.Fatal("CoSo profile types unexpectedly include unsupported parameter 65100")
	}

	maxInstances := CANopenCoSoCompatibilityParameterMaxInstances()
	if got := maxInstances[102]; got != 1 {
		t.Fatalf("CoSo profile max instances for serial number 102 = %d, want 1", got)
	}
	if got := maxInstances[104]; got != 1 {
		t.Fatalf("CoSo profile max instances for device status 104 = %d, want 1", got)
	}
	if got := maxInstances[108]; got != 1 {
		t.Fatalf("CoSo profile max instances for transform 108 = %d, want 1", got)
	}
	if got := maxInstances[1045]; got != 1 {
		t.Fatalf("CoSo profile max instances for 1045 = %d, want profile cap 1", got)
	}
	if got := maxInstances[1063]; got != 1 {
		t.Fatalf("CoSo profile max instances for device temperature 1063 = %d, want 1", got)
	}
	if got := maxInstances[1102]; got != 2 {
		t.Fatalf("CoSo profile max instances for fan actual speed 1102 = %d, want 2", got)
	}
	if got := maxInstances[53020]; got != 1 {
		t.Fatalf("CoSo profile max instances for unipolar license status 53020 = %d, want 1", got)
	}
	if got := maxInstances[6200]; got != 2 {
		t.Fatalf("CoSo profile max instances for fan enable 6200 = %d, want 2", got)
	}
	if got := maxInstances[1044]; got != 4 {
		t.Fatalf("CoSo profile max instances for 1044 = %d, want PRD332 observed metadata coverage through instance 4", got)
	}
	if got := maxInstances[1200]; got != 4 {
		t.Fatalf("CoSo profile max instances for 1200 = %d, want 4", got)
	}
	if got := maxInstances[52200]; got != 1 {
		t.Fatalf("CoSo profile max instances for 52200 = %d, want 1", got)
	}
	if got := maxInstances[52201]; got != 4 {
		t.Fatalf("CoSo profile max instances for 52201 = %d, want 4", got)
	}
	if got := maxInstances[1000]; got != 4 {
		t.Fatalf("CoSo profile max instances for normal TEC readout 1000 = %d, want 4", got)
	}

	writable := CANopenCoSoCompatibilityParameterWritability()
	if !writable[2010] {
		t.Fatal("CoSo profile writability for mapped write parameter 2010 = false, want true")
	}
	if !writable[120] {
		t.Fatal("CoSo profile writability for User Notes transform 120 = false, want true")
	}
	if writable[1200] {
		t.Fatal("CoSo profile writability for read-only transform 1200 = true, want false")
	}
	if writable[108] {
		t.Fatal("CoSo profile writability for read-only transform 108 = true, want false")
	}
	if !writable[52200] {
		t.Fatal("CoSo profile writability for Object External Temperature transform 52200 = false, want true")
	}
	if !writable[52201] {
		t.Fatal("CoSo profile writability for Sink Fixed Temperature transform 52201 = false, want true")
	}
	if !writable[2033] {
		t.Fatal("CoSo profile writability for normal TEC write parameter 2033 = false, want true")
	}
	if !writable[6200] {
		t.Fatal("CoSo profile writability for fan enable 6200 = false, want true")
	}
}

func TestCANopenCoSoCompatibilityProfileCoversPRD332TraceFailures(t *testing.T) {
	params := CANopenCoSoCompatibilityParameters()
	observed := map[int]struct {
		maxInstance int
		translation string
	}{
		104:   {maxInstance: 1, translation: canopenCompatibilityTranslationDirectMapping},
		108:   {maxInstance: 1, translation: canopenCompatibilityTranslationBridgeTransform},
		1044:  {maxInstance: 4, translation: canopenCompatibilityTranslationDirectMapping},
		1045:  {maxInstance: 1, translation: canopenCompatibilityTranslationDirectMapping},
		1063:  {maxInstance: 1, translation: canopenCompatibilityTranslationDirectMapping},
		1102:  {maxInstance: 2, translation: canopenCompatibilityTranslationDirectMapping},
		115:   {maxInstance: 1, translation: canopenCompatibilityTranslationBridgeTransform},
		120:   {maxInstance: 1, translation: canopenCompatibilityTranslationBridgeTransform},
		52200: {maxInstance: 1, translation: canopenCompatibilityTranslationBridgeTransform},
		52201: {maxInstance: 4, translation: canopenCompatibilityTranslationBridgeTransform},
		6200:  {maxInstance: 2, translation: canopenCompatibilityTranslationDirectMapping},
		52002: {maxInstance: 4, translation: canopenCompatibilityTranslationDirectMapping},
		53020: {maxInstance: 1, translation: canopenCompatibilityTranslationDirectMapping},
	}

	for id, want := range observed {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			param, ok := params[id]
			if !ok {
				t.Fatalf("CoSo PRD332 trace parameter %d missing from JSON compatibility profile", id)
			}
			if !param.Supported {
				t.Fatalf("CoSo PRD332 trace parameter %d marked unsupported", id)
			}
			if param.MaxInstance != want.maxInstance {
				t.Fatalf("CoSo PRD332 trace parameter %d max instance = %d, want %d", id, param.MaxInstance, want.maxInstance)
			}
			if param.Translation != want.translation {
				t.Fatalf("CoSo PRD332 trace parameter %d translation = %q, want %q", id, param.Translation, want.translation)
			}
			if param.Type == "" {
				t.Fatalf("CoSo PRD332 trace parameter %d has empty value type", id)
			}
		})
	}
}

func TestCANopenCompatibilityMetadataMergesMappingsAndTransforms(t *testing.T) {
	types := CANopenCompatibilityParameterTypes()
	if got := types[1000]; got != DataTypeFloat32 {
		t.Fatalf("compatibility type for direct mapping 1000 = %q, want float32", got)
	}
	if got := types[1200]; got != DataTypeInt32 {
		t.Fatalf("compatibility type for transform 1200 = %q, want int32", got)
	}
	if got := types[120]; got != DataTypeLatin1 {
		t.Fatalf("compatibility type for big-data transform 120 = %q, want latin1", got)
	}
	if _, ok := types[65100]; ok {
		t.Fatal("compatibility types unexpectedly include unsupported diagnostics parameter 65100")
	}

	mappedMaxInstances := CANopenMappedParameterMaxInstances()
	if got := mappedMaxInstances[2040]; got != 255 {
		t.Fatalf("mapped max instances for direct SDO 2040 = %d, want raw CANopen cap 255", got)
	}

	maxInstances := CANopenCompatibilityParameterMaxInstances()
	if got := maxInstances[1045]; got != 1 {
		t.Fatalf("compatibility max instances for profile-capped direct mapping 1045 = %d, want 1", got)
	}
	if got := maxInstances[2040]; got != 1 {
		t.Fatalf("compatibility max instances for profile-capped operating mode 2040 = %d, want 1", got)
	}
	if got := maxInstances[1200]; got != 4 {
		t.Fatalf("compatibility max instances for transform 1200 = %d, want 4", got)
	}

	writable := CANopenCompatibilityParameterWritability()
	if !writable[2010] {
		t.Fatal("compatibility writability for direct mapped write parameter 2010 = false, want true")
	}
	if writable[1000] {
		t.Fatal("compatibility writability for read-only direct mapping 1000 = true, want false")
	}
	if !writable[120] {
		t.Fatal("compatibility writability for read/write transform 120 = false, want true")
	}
	if writable[1200] {
		t.Fatal("compatibility writability for read-only transform 1200 = true, want false")
	}

	cacheBehaviors := CANopenCompatibilityParameterCacheBehaviors()
	if got := cacheBehaviors[2040]; got != CANopenCompatibilityCacheBehaviorMetadataLiveActual {
		t.Fatalf("compatibility cache behavior for operating mode 2040 = %q, want %q", got, CANopenCompatibilityCacheBehaviorMetadataLiveActual)
	}
	if got := cacheBehaviors[50000]; got != "" {
		t.Fatalf("compatibility cache behavior for unsupported ID 50000 = %q, want empty", got)
	}

	unsupported := CANopenUnsupportedParameterIDs()
	if _, ok := unsupported[50000]; !ok {
		t.Fatal("unsupported parameter catalogue missing debug/flash MeParID 50000")
	}
	unsupportedBehaviors := CANopenUnsupportedParameterBridgeBehavior()
	for _, tc := range []struct {
		id       int
		base     uint16
		elements int
	}{
		{id: 2150, base: 0x1400, elements: 80},
		{id: 2151, base: 0x1600, elements: 144},
		{id: 2152, base: 0x1800, elements: 144},
		{id: 2153, base: 0x1A00, elements: 144},
	} {
		if _, ok := unsupported[tc.id]; ok {
			t.Fatalf("CANopen NVC byte/PDO config MeParID %d is still marked unsupported", tc.id)
		}
		if got := types[tc.id]; got != DataTypeByte {
			t.Fatalf("compatibility type for CANopen NVC byte/PDO config MeParID %d = %q, want byte", tc.id, got)
		}
		if got := maxInstances[tc.id]; got != 1 {
			t.Fatalf("compatibility max instances for CANopen NVC byte/PDO config MeParID %d = %d, want 1", tc.id, got)
		}
		if writable[tc.id] {
			t.Fatalf("compatibility writability for CANopen NVC byte/PDO config MeParID %d = true, want false", tc.id)
		}
		transform, ok := CANopenBridgeTransform(tc.id)
		if !ok {
			t.Fatalf("missing bridge transform for CANopen NVC byte/PDO config MeParID %d", tc.id)
		}
		if transform.Kind != "canopen_pdo_config_bytes" || transform.Type != DataTypeByte || transform.MaxElements != tc.elements || transform.CANopenIndexBase != tc.base || transform.Writable || !transform.HasMetadataFlags || transform.MetadataFlags != 0x03 {
			t.Fatalf("bridge transform for CANopen NVC byte/PDO config MeParID %d = %+v, want BYTE base 0x%04X max %d read-only with metadata flags 0x03", tc.id, transform, tc.base, tc.elements)
		}
	}
	if got := unsupportedBehaviors[50000]; got != CANopenUnsupportedBridgeBehaviorNACKBulkRead {
		t.Fatalf("unsupported bridge behavior for debug/flash MeParID 50000 = %q, want %q", got, CANopenUnsupportedBridgeBehaviorNACKBulkRead)
	}
}

func TestCANopenSDOMapMapsMeasuredTemperatureSignals(t *testing.T) {
	for _, tc := range []struct {
		id      int
		index   uint16
		maxInst int
		signal  string
	}{
		{id: 1044, index: 0x2144, maxInst: 4, signal: "LR measured temperature"},
		{id: 1045, index: 0x2145, maxInst: 2, signal: "HR measured temperature"},
	} {
		t.Run(tc.signal, func(t *testing.T) {
			object, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, 1)
			if !ok {
				t.Fatalf("missing CANopen mapping for MeCom ID %d", tc.id)
			}
			if object.Index != tc.index || object.SubIndex != 1 {
				t.Fatalf("object for %d = 0x%04X.%d, want 0x%04X.1", tc.id, object.Index, object.SubIndex, tc.index)
			}
			if object.Kind != DataTypeFloat32 || object.Writable {
				t.Fatalf("object for %d kind/writable = %q/%v, want float32/read-only", tc.id, object.Kind, object.Writable)
			}
			if _, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, tc.maxInst+1); ok {
				t.Fatalf("mapping for %d accepted instance %d beyond configured max %d", tc.id, tc.maxInst+1, tc.maxInst)
			}
			typ, ok := defaultCANopenSDOMap.ParameterType(tc.id)
			if !ok || typ != DataTypeFloat32 {
				t.Fatalf("parameter type for %d = %q/%v, want float32", tc.id, typ, ok)
			}
		})
	}
}

func TestCANopenSDOMapMapsCoSoSystemTraceSignals(t *testing.T) {
	for _, tc := range []struct {
		id     int
		index  uint16
		typ    DataType
		signal string
	}{
		{id: 104, index: 0x2004, typ: DataTypeInt32, signal: "device status"},
		{id: 1063, index: 0x2163, typ: DataTypeFloat32, signal: "device temperature"},
	} {
		t.Run(tc.signal, func(t *testing.T) {
			object, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, 1)
			if !ok {
				t.Fatalf("missing CANopen mapping for CoSo trace MeCom ID %d", tc.id)
			}
			if object.Index != tc.index || object.SubIndex != 1 {
				t.Fatalf("object for %d = 0x%04X.%d, want 0x%04X.1", tc.id, object.Index, object.SubIndex, tc.index)
			}
			if object.Kind != tc.typ || object.Writable {
				t.Fatalf("object for %d kind/writable = %q/%v, want %q/read-only", tc.id, object.Kind, object.Writable, tc.typ)
			}
			if _, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, 2); ok {
				t.Fatalf("mapping for CoSo trace MeCom ID %d accepted instance 2 beyond fixed instance 1", tc.id)
			}
		})
	}
}

func TestCANopenSDOMapMapsCoSoFanMonitorFamily(t *testing.T) {
	for _, tc := range []struct {
		id     int
		index  uint16
		signal string
	}{
		{id: 1100, index: 0x2200, signal: "relative cooling power"},
		{id: 1101, index: 0x2201, signal: "nominal fan speed"},
		{id: 1102, index: 0x2202, signal: "actual fan speed"},
		{id: 1103, index: 0x2203, signal: "fan pwm level"},
	} {
		t.Run(tc.signal, func(t *testing.T) {
			object, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, 1)
			if !ok {
				t.Fatalf("missing CANopen mapping for CoSo fan monitor MeCom ID %d", tc.id)
			}
			if object.Index != tc.index || object.SubIndex != 1 {
				t.Fatalf("object for %d = 0x%04X.%d, want 0x%04X.1", tc.id, object.Index, object.SubIndex, tc.index)
			}
			if object.Kind != DataTypeFloat32 || object.Writable {
				t.Fatalf("object for %d kind/writable = %q/%v, want float32/read-only", tc.id, object.Kind, object.Writable)
			}
			if _, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, 2); !ok {
				t.Fatalf("mapping for CoSo fan monitor MeCom ID %d missing instance 2", tc.id)
			}
			if _, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, 3); ok {
				t.Fatalf("mapping for CoSo fan monitor MeCom ID %d accepted instance 3 beyond configured max 2", tc.id)
			}
		})
	}
}

func TestCANopenSDOMapMapsCoSoLicenseStatusFamily(t *testing.T) {
	for _, tc := range []struct {
		id     int
		index  uint16
		signal string
	}{
		{id: 53001, index: 0x4300, signal: "feature key status"},
		{id: 53010, index: 0x4310, signal: "estimator status"},
		{id: 53011, index: 0x4311, signal: "estimator trial from"},
		{id: 53012, index: 0x4312, signal: "estimator trial to"},
		{id: 53015, index: 0x4315, signal: "cascade status"},
		{id: 53016, index: 0x4316, signal: "cascade trial from"},
		{id: 53017, index: 0x4317, signal: "cascade trial to"},
		{id: 53020, index: 0x4320, signal: "unipolar status"},
		{id: 53021, index: 0x4321, signal: "unipolar trial from"},
		{id: 53022, index: 0x4322, signal: "unipolar trial to"},
	} {
		t.Run(tc.signal, func(t *testing.T) {
			object, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, 1)
			if !ok {
				t.Fatalf("missing CANopen mapping for CoSo license MeCom ID %d", tc.id)
			}
			if object.Index != tc.index || object.SubIndex != 1 {
				t.Fatalf("object for %d = 0x%04X.%d, want 0x%04X.1", tc.id, object.Index, object.SubIndex, tc.index)
			}
			if object.Kind != DataTypeInt32 || object.Writable {
				t.Fatalf("object for %d kind/writable = %q/%v, want int32/read-only", tc.id, object.Kind, object.Writable)
			}
			if _, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, 2); ok {
				t.Fatalf("mapping for CoSo license MeCom ID %d accepted instance 2 beyond fixed instance 1", tc.id)
			}
		})
	}
}

func TestCANopenSDOMapMapsCoSoFanControllerFamily(t *testing.T) {
	for _, tc := range []struct {
		id      int
		index   uint16
		typ     DataType
		maxInst int
		signal  string
	}{
		{id: 6200, index: 0x3200, typ: DataTypeInt32, maxInst: 2, signal: "fan enable"},
		{id: 6201, index: 0x3201, typ: DataTypeInt32, maxInst: 2, signal: "fan mode"},
		{id: 6210, index: 0x3210, typ: DataTypeInt32, maxInst: 2, signal: "heatsink temperature source"},
		{id: 6211, index: 0x3211, typ: DataTypeFloat32, maxInst: 2, signal: "target temperature"},
		{id: 6212, index: 0x3212, typ: DataTypeFloat32, maxInst: 2, signal: "temperature controller kp"},
		{id: 6213, index: 0x3213, typ: DataTypeFloat32, maxInst: 2, signal: "temperature controller ti"},
		{id: 6214, index: 0x3214, typ: DataTypeFloat32, maxInst: 2, signal: "temperature controller td"},
		{id: 6220, index: 0x3220, typ: DataTypeFloat32, maxInst: 2, signal: "zero percent speed"},
		{id: 6221, index: 0x3221, typ: DataTypeFloat32, maxInst: 2, signal: "full speed"},
		{id: 6222, index: 0x3222, typ: DataTypeFloat32, maxInst: 2, signal: "speed controller kp"},
		{id: 6223, index: 0x3223, typ: DataTypeFloat32, maxInst: 2, signal: "speed controller ti"},
		{id: 6224, index: 0x3224, typ: DataTypeFloat32, maxInst: 2, signal: "speed controller td"},
		{id: 6225, index: 0x3225, typ: DataTypeInt32, maxInst: 2, signal: "bypass speed controller"},
		{id: 6226, index: 0x3226, typ: DataTypeInt32, maxInst: 2, signal: "fan surveillance"},
		{id: 6227, index: 0x3227, typ: DataTypeFloat32, maxInst: 2, signal: "minimum speed start"},
		{id: 6228, index: 0x3228, typ: DataTypeFloat32, maxInst: 2, signal: "minimum speed stop"},
		{id: 6229, index: 0x3229, typ: DataTypeFloat32, maxInst: 2, signal: "fixed pwm level"},
		{id: 6230, index: 0x3230, typ: DataTypeInt32, maxInst: 1, signal: "pwm frequency"},
		{id: 6240, index: 0x3240, typ: DataTypeInt32, maxInst: 2, signal: "ambient temperature source"},
		{id: 6241, index: 0x3241, typ: DataTypeFloat32, maxInst: 2, signal: "ambient temperature fixed value"},
		{id: 6242, index: 0x3242, typ: DataTypeFloat32, maxInst: 2, signal: "conditioner kp"},
		{id: 6243, index: 0x3243, typ: DataTypeInt32, maxInst: 2, signal: "linked temperature controller"},
	} {
		t.Run(tc.signal, func(t *testing.T) {
			object, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, 1)
			if !ok {
				t.Fatalf("missing CANopen mapping for CoSo fan MeCom ID %d", tc.id)
			}
			if object.Index != tc.index || object.SubIndex != 1 {
				t.Fatalf("object for %d = 0x%04X.%d, want 0x%04X.1", tc.id, object.Index, object.SubIndex, tc.index)
			}
			if object.Kind != tc.typ || !object.Writable {
				t.Fatalf("object for %d kind/writable = %q/%v, want %q/writable", tc.id, object.Kind, object.Writable, tc.typ)
			}
			if _, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, tc.maxInst); !ok {
				t.Fatalf("mapping for CoSo fan MeCom ID %d missing max instance %d", tc.id, tc.maxInst)
			}
			if _, ok := defaultCANopenSDOMap.ObjectForMeCom(tc.id, tc.maxInst+1); ok {
				t.Fatalf("mapping for CoSo fan MeCom ID %d accepted instance %d beyond configured max %d", tc.id, tc.maxInst+1, tc.maxInst)
			}
		})
	}
}

func TestCANopenMappedParameterMaxInstancesUsesCatalogue(t *testing.T) {
	maxInstances := CANopenMappedParameterMaxInstances()
	if got := maxInstances[1044]; got != 4 {
		t.Fatalf("max instances for 1044 = %d, want 4", got)
	}
	if got := maxInstances[1045]; got != 2 {
		t.Fatalf("max instances for 1045 = %d, want 2", got)
	}
	if got := maxInstances[1012]; got != 4 {
		t.Fatalf("max instances for 1012 = %d, want 4", got)
	}
	if got := maxInstances[6132]; got != 4 {
		t.Fatalf("max instances for 6132 = %d, want 4", got)
	}
	if got := maxInstances[53184]; got != 2 {
		t.Fatalf("max instances for 53184 = %d, want 2", got)
	}
	if got := maxInstances[6000]; got != 2 {
		t.Fatalf("max instances for 6000 = %d, want 2", got)
	}
	if _, ok := maxInstances[65100]; ok {
		t.Fatal("max instances unexpectedly includes unsupported diagnostics parameter 65100")
	}
}

func TestCANopenSDOMapCoversObservedCoSoTraceIDs(t *testing.T) {
	unsupported := CANopenUnsupportedParameterIDs()
	transforms := CANopenBridgeTransforms()
	params := CANopenCoSoCompatibilityParameters()

	ids := make([]int, 0, len(params))
	for id := range params {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	for _, id := range ids {
		maxInstance := params[id].MaxInstance
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			if transform, ok := transforms[id]; ok {
				if transform.Type == "" {
					t.Fatalf("bridge transform %d has empty type", id)
				}
				if transform.MaxInstance < maxInstance {
					t.Fatalf("bridge transform %d max instance = %d, want at least observed %d", id, transform.MaxInstance, maxInstance)
				}
				return
			}
			if _, ok := unsupported[id]; ok {
				if object, ok := defaultCANopenSDOMap.ObjectForMeCom(id, 1); ok {
					t.Fatalf("unsupported trace ID %d has direct SDO object %+v", id, object)
				}
				return
			}
			if object, ok := defaultCANopenSDOMap.ObjectForMeCom(id, 1); !ok {
				t.Fatalf("missing direct SDO mapping for observed trace ID %d", id)
			} else if object.Kind == "" {
				t.Fatalf("direct SDO mapping for observed trace ID %d has empty type", id)
			}
			if maxInstance > 1 {
				if _, ok := defaultCANopenSDOMap.ObjectForMeCom(id, maxInstance); !ok {
					t.Fatalf("missing direct SDO mapping for observed trace ID %d instance %d", id, maxInstance)
				}
			}
		})
	}
}

func TestLoadCANopenSDOMapRejectsInvalidSubindexMin(t *testing.T) {
	raw := []byte(`{
		"schema_version": "mecom_ldd130x_canopen_sdo_map.v1",
		"source_policy": "test",
		"mappings": [{
			"mecom_id": 1000,
			"name": "Object Temperature",
			"value_type": "float32",
			"access": "ro",
			"instances": { "mode": "subindex", "min": -1, "max": 4 },
			"canopen": { "index": "0x2100", "subindex": "instance", "subindex_mode": "instance", "data_type": "0x0008" },
			"source_evidence": ["test"]
		}]
	}`)
	_, err := LoadCANopenSDOMap(raw)
	if err == nil || !strings.Contains(err.Error(), "invalid instance range") {
		t.Fatalf("LoadCANopenSDOMap error = %v, want invalid instance range", err)
	}
}

func TestLoadCANopenSDOMapLoadsNumericUnsupportedIDs(t *testing.T) {
	raw := []byte(`{
		"schema_version": "mecom_tec_canopen_sdo_map.v1",
		"source_policy": "test",
		"unsupported": [
			{"id": 203, "reason": "no SDO path", "source_evidence": ["test"]},
			{"id": "?RS0000", "reason": "stream command", "source_evidence": ["test"]}
		]
	}`)
	mapping, err := LoadCANopenSDOMap(raw)
	if err != nil {
		t.Fatalf("LoadCANopenSDOMap returned error: %v", err)
	}
	unsupported := mapping.UnsupportedParameterIDs()
	if got := unsupported[203]; got != "no SDO path" {
		t.Fatalf("unsupported[203] = %q, want no SDO path", got)
	}
	if got := len(unsupported); got != 1 {
		t.Fatalf("numeric unsupported count = %d, want 1", got)
	}
}

func TestLoadCANopenSDOMapRejectsUnsupportedIDsWithoutEvidence(t *testing.T) {
	raw := []byte(`{
		"schema_version": "mecom_tec_canopen_sdo_map.v1",
		"source_policy": "test",
		"unsupported": [
			{"id": 203, "reason": "no SDO path"}
		]
	}`)
	_, err := LoadCANopenSDOMap(raw)
	if err == nil || !strings.Contains(err.Error(), "unsupported MeCom ID 203 has no source evidence") {
		t.Fatalf("LoadCANopenSDOMap error = %v, want unsupported evidence error", err)
	}
}
