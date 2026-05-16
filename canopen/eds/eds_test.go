package eds

import (
	"strings"
	"testing"

	"github.com/egidinas/meerstetter-go/objectdict"
)

func TestParseBuildsObjectDictionary(t *testing.T) {
	const fixture = "\ufeff[FileInfo]\nFileName=TEC-CANopen.eds\nEDSVersion=4.0\nModificationDate=02-25-2026\n\n" +
		"[DeviceInfo]\nVendorName=Meerstetter Engineering GmbH\nVendorNumber=0x547\nProductName=TEC-Controllers\nProductNumber=0x441\nRevisionNumber=0x0\n\n" +
		"[2000]\nParameterName=Device Type\nObjectType=0x7\nDataType=0x0007\nAccessType=ro\nDefaultValue=0\n\n" +
		"[3000]\nParameterName=Measured temperatures\nObjectType=0x9\nSubNumber=2\n\n" +
		"[3000sub0]\nParameterName=Number of entries\nDataType=0x0005\nAccessType=ro\n\n" +
		"[3000sub1]\nParameterName=Object Temperature\nDataType=0x0008\nAccessType=rw\nPDOMapping=1\nLowLimit=-50\nHighLimit=150\nUnit=degC\n"

	dict, err := Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if dict.SchemaVersion != 1 {
		t.Fatalf("schema version = %d", dict.SchemaVersion)
	}
	if dict.Protocol != objectdict.ProtocolCANopen {
		t.Fatalf("protocol = %q", dict.Protocol)
	}
	if dict.Device.Vendor != "Meerstetter Engineering GmbH" {
		t.Fatalf("vendor = %q", dict.Device.Vendor)
	}
	if len(dict.Objects) != 2 {
		t.Fatalf("objects = %d", len(dict.Objects))
	}
	if dict.Objects[0].ID != "canopen:0x2000" || len(dict.Objects[0].Entries) != 1 {
		t.Fatalf("unexpected first object: %#v", dict.Objects[0])
	}
	entry := dict.Objects[1].Entries[0]
	if entry.ID != "canopen:0x3000:0x01" {
		t.Fatalf("entry id = %q", entry.ID)
	}
	if entry.Kind != objectdict.ValueKindContinuous {
		t.Fatalf("kind = %q", entry.Kind)
	}
	if entry.Access != objectdict.AccessReadWrite {
		t.Fatalf("access = %q", entry.Access)
	}
	if entry.Min == nil || *entry.Min != -50 {
		t.Fatalf("min = %#v", entry.Min)
	}
}

func TestParseAcceptsLongMetadataValues(t *testing.T) {
	longDescription := strings.Repeat("calibration", 7000)
	fixture := "[FileInfo]\nDescription=" + longDescription + "\n\n" +
		"[DeviceInfo]\nProductName=TEC-Controllers\n\n" +
		"[2000]\nParameterName=Device Type\nDataType=0x0007\nAccessType=ro\n"

	dict, err := Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if dict.Metadata["Description"] != longDescription {
		t.Fatalf("description length = %d", len(dict.Metadata["Description"]))
	}
}
