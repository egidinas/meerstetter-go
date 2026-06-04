package mecom

import "testing"

func TestDefaultCommandInventoryIncludesRMMControllerCommands(t *testing.T) {
	inventory := DefaultCommandInventory()
	if got, want := len(inventory), 12; got != want {
		t.Fatalf("inventory length = %d, want %d", got, want)
	}

	want := []struct {
		token     string
		operation CommandOperation
		direction CommandDirection
	}{
		{"RS", CommandOperationResetDevice, CommandDirectionHostToDevice},
		{"SP", CommandOperationSaveToFlash, CommandDirectionHostToDevice},
		{"SA", CommandOperationSetDeviceAddress, CommandDirectionHostToDevice},
		{"?BI", CommandOperationReadBranchID, CommandDirectionRequestResponse},
		{"?VI", CommandOperationReadFirmwareVersion, CommandDirectionRequestResponse},
		{"?VR", CommandOperationReadParameterValue, CommandDirectionRequestResponse},
		{"VS", CommandOperationWriteParameterValue, CommandDirectionHostToDevice},
		{"?VL", CommandOperationReadParameterLimits, CommandDirectionRequestResponse},
		{"?VM", CommandOperationReadParameterMetadata, CommandDirectionRequestResponse},
		{"?VX", CommandOperationBulkReadParameters, CommandDirectionRequestResponse},
		{"?MB", CommandOperationDownloadMeBlob, CommandDirectionRequestResponse},
		{"?BC", CommandOperationSendBootloaderCommand, CommandDirectionRequestResponse},
	}

	for i, expected := range want {
		got := inventory[i]
		if got.Token != expected.token {
			t.Fatalf("inventory[%d].Token = %q, want %q", i, got.Token, expected.token)
		}
		if got.Operation != expected.operation {
			t.Fatalf("inventory[%d].Operation = %q, want %q", i, got.Operation, expected.operation)
		}
		if got.Direction != expected.direction {
			t.Fatalf("inventory[%d].Direction = %q, want %q", i, got.Direction, expected.direction)
		}
		if got.Source != CommandSourceRMM1182ControllerSoftware {
			t.Fatalf("inventory[%d].Source = %q, want RMM controller evidence", i, got.Source)
		}
		if got.PayloadShape == "" {
			t.Fatalf("inventory[%d] missing payload shape", i)
		}
	}
}

func TestCommandDefinitionByToken(t *testing.T) {
	cmd, ok := CommandDefinitionByToken("?VX")
	if !ok {
		t.Fatal("missing ?VX command definition")
	}
	if cmd.Operation != CommandOperationBulkReadParameters {
		t.Fatalf("?VX operation = %q", cmd.Operation)
	}
	if cmd.PayloadShape != "?VX<count><param-id><instance>..." {
		t.Fatalf("?VX payload shape = %q", cmd.PayloadShape)
	}
	if cmd.Confidence != CommandConfidenceHigh {
		t.Fatalf("?VX confidence = %q", cmd.Confidence)
	}

	if _, ok := CommandDefinitionByToken("?NOPE"); ok {
		t.Fatal("unknown command token was accepted")
	}
}

func TestDefaultCommandInventoryReturnsCopy(t *testing.T) {
	first := DefaultCommandInventory()
	first[0].Token = "mutated"

	second := DefaultCommandInventory()
	if second[0].Token != "RS" {
		t.Fatalf("inventory mutation leaked: first token = %q", second[0].Token)
	}
}
