package discovery

import (
	"sort"
	"strings"

	"github.com/egidinas/meerstetter-go/objectdict"
)

// Direction describes whether a target is telemetry, telecommand, or both.
type Direction string

const (
	DirectionTM Direction = "tm"
	DirectionTC Direction = "tc"
	DirectionIO Direction = "tm_tc"
)

// Ownership describes who is master of a connection or commandable resource.
type Ownership string

const (
	OwnershipLocalNode  Ownership = "local_node"
	OwnershipRemoteNode Ownership = "remote_node"
	OwnershipLegacyApp  Ownership = "legacy_app"
	OwnershipShared     Ownership = "shared"
	OwnershipDerived    Ownership = "derived"
)

// Target is the shared discovery shape used by BusMaster-style trees, graph
// assignment, sequencer wiring, and command-center subscription.
type Target struct {
	ID              string               `json:"id"`
	ParentID        string               `json:"parent_id,omitempty"`
	Name            string               `json:"name"`
	Group           []string             `json:"group,omitempty"`
	Direction       Direction            `json:"direction"`
	Ownership       Ownership            `json:"ownership"`
	Protocol        objectdict.Protocol  `json:"protocol,omitempty"`
	NodeID          string               `json:"node_id,omitempty"`
	Transport       string               `json:"transport,omitempty"`
	Address         string               `json:"address,omitempty"`
	Unit            string               `json:"unit,omitempty"`
	Kind            objectdict.ValueKind `json:"kind,omitempty"`
	Dictionary      string               `json:"dictionary,omitempty"`
	DictionaryEntry string               `json:"dictionary_entry,omitempty"`
	Metadata        map[string]string    `json:"metadata,omitempty"`
}

// TreeNode is a collapsible BusMaster-style grouping node.
type TreeNode struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Group    []string   `json:"group,omitempty"`
	Targets  []Target   `json:"targets,omitempty"`
	Children []TreeNode `json:"children,omitempty"`
}

// BuildTree groups targets by their Group path and sorts nodes and leaves.
func BuildTree(targets []Target) TreeNode {
	root := TreeNode{ID: "root", Name: "Sources"}
	for _, target := range targets {
		path := target.Group
		if len(path) == 0 {
			path = []string{"Ungrouped"}
		}
		insertTarget(&root, path, target)
	}
	sortTree(&root)
	return root
}

func insertTarget(node *TreeNode, path []string, target Target) {
	if len(path) == 0 {
		node.Targets = append(node.Targets, target)
		return
	}
	name := strings.TrimSpace(path[0])
	if name == "" {
		name = "Ungrouped"
	}
	for i := range node.Children {
		if node.Children[i].Name == name {
			insertTarget(&node.Children[i], path[1:], target)
			return
		}
	}
	child := TreeNode{
		ID:    strings.Join(append(node.Group, name), "/"),
		Name:  name,
		Group: append(append([]string(nil), node.Group...), name),
	}
	node.Children = append(node.Children, child)
	insertTarget(&node.Children[len(node.Children)-1], path[1:], target)
}

func sortTree(node *TreeNode) {
	sort.Slice(node.Targets, func(i, j int) bool { return node.Targets[i].Name < node.Targets[j].Name })
	sort.Slice(node.Children, func(i, j int) bool { return node.Children[i].Name < node.Children[j].Name })
	for i := range node.Children {
		sortTree(&node.Children[i])
	}
}

// TargetsFromDictionary creates TM/TC discovery targets from object dictionary entries.
func TargetsFromDictionary(dict objectdict.Dictionary, nodeID, owner string) []Target {
	var targets []Target
	ownership := OwnershipLocalNode
	if owner != "" {
		ownership = Ownership(owner)
	}
	for _, obj := range dict.Objects {
		for _, entry := range obj.Entries {
			direction := DirectionIO
			if entry.Access == objectdict.AccessReadOnly {
				direction = DirectionTM
			}
			if entry.Access == objectdict.AccessWriteOnly {
				direction = DirectionTC
			}
			targets = append(targets, Target{
				ID:              entry.ID,
				Name:            entry.Name,
				Group:           []string{string(dict.Protocol), obj.Name},
				Direction:       direction,
				Ownership:       ownership,
				Protocol:        dict.Protocol,
				NodeID:          nodeID,
				Unit:            entry.Unit,
				Kind:            entry.Kind,
				Dictionary:      dict.DefinitionID,
				DictionaryEntry: entry.ID,
			})
		}
	}
	return targets
}
