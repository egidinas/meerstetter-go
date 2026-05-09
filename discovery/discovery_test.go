package discovery

import (
	"testing"

	"github.com/egidinas/meerstetter-go/objectdict"
)

func TestBuildTreeGroupsTargets(t *testing.T) {
	tree := BuildTree([]Target{
		{ID: "b", Name: "B", Group: []string{"CAN", "TEC"}, Direction: DirectionTM},
		{ID: "a", Name: "A", Group: []string{"CAN", "TEC"}, Direction: DirectionTC},
		{ID: "c", Name: "C", Group: []string{"MeCom"}, Direction: DirectionIO},
	})
	if len(tree.Children) != 2 {
		t.Fatalf("children = %d", len(tree.Children))
	}
	if tree.Children[0].Name != "CAN" {
		t.Fatalf("first child = %q", tree.Children[0].Name)
	}
	tec := tree.Children[0].Children[0]
	if len(tec.Targets) != 2 || tec.Targets[0].Name != "A" {
		t.Fatalf("unexpected TEC targets: %#v", tec.Targets)
	}
}

func TestTargetsFromDictionaryMapsAccessToDirection(t *testing.T) {
	dict := objectdict.Dictionary{
		Protocol:     objectdict.ProtocolCANopen,
		DefinitionID: "tec",
		Objects: []objectdict.Object{{
			ID:   "canopen:0x3000",
			Name: "Temperatures",
			Entries: []objectdict.Entry{{
				ID:     "canopen:0x3000:0x01",
				Name:   "Object Temperature",
				Access: objectdict.AccessReadOnly,
				Kind:   objectdict.ValueKindContinuous,
			}},
		}},
	}
	targets := TargetsFromDictionary(dict, "node-a", string(OwnershipRemoteNode))
	if len(targets) != 1 {
		t.Fatalf("targets = %d", len(targets))
	}
	if targets[0].Direction != DirectionTM || targets[0].Ownership != OwnershipRemoteNode {
		t.Fatalf("target = %#v", targets[0])
	}
}
