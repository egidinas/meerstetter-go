package mecom

import "testing"

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
}
