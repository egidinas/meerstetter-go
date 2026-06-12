package canmap

import (
	"fmt"
	"sort"
)

// ExportPattern returns a deep copy of the registry with every concrete node
// ID removed. The result describes the wiring of a *kind* of testbed — roles,
// COB-IDs, mappings, source selects — without naming the hardware, and is
// what you commit as the template for testbed copies.
//
// Verification state is stripped too: a pattern has never been verified
// against hardware by definition.
func ExportPattern(r *Registry) *Registry {
	p := cloneRegistry(r)
	for i := range p.Nodes {
		p.Nodes[i].NodeID = 0
		p.Nodes[i].Label = ""
	}
	for i := range p.Signals {
		p.Signals[i].Verified = ""
		p.Signals[i].SavedToFlash = false
	}
	return p
}

// Binding assigns a concrete node to a pattern role on one physical testbed.
type Binding struct {
	Role   string `json:"role"`
	NodeID byte   `json:"node_id"`
	Label  string `json:"label,omitempty"`
}

// Instantiate applies role bindings to a pattern and returns a concrete
// registry for one physical testbed. Every pattern role must be bound, every
// binding must name a pattern role, and node IDs must be unique and valid.
// The pattern itself is not modified.
func Instantiate(pattern *Registry, name string, bindings []Binding) (*Registry, error) {
	byRole := map[string]Binding{}
	for _, b := range bindings {
		if b.NodeID == 0 || b.NodeID > 127 {
			return nil, fmt.Errorf("canmap: binding %q: node_id 0x%02X out of range 1..127", b.Role, b.NodeID)
		}
		if _, dup := byRole[b.Role]; dup {
			return nil, fmt.Errorf("canmap: duplicate binding for role %q", b.Role)
		}
		byRole[b.Role] = b
	}
	seen := map[byte]string{}
	for role, b := range byRole {
		if prev, dup := seen[b.NodeID]; dup {
			return nil, fmt.Errorf("canmap: node_id 0x%02X bound to both %q and %q", b.NodeID, prev, role)
		}
		seen[b.NodeID] = role
	}

	out := cloneRegistry(pattern)
	if name != "" {
		out.Name = name
	}
	var missing []string
	for i := range out.Nodes {
		b, ok := byRole[out.Nodes[i].Role]
		if !ok {
			missing = append(missing, out.Nodes[i].Role)
			continue
		}
		out.Nodes[i].NodeID = b.NodeID
		out.Nodes[i].Label = b.Label
		delete(byRole, out.Nodes[i].Role)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("canmap: unbound pattern roles: %v", missing)
	}
	if len(byRole) > 0 {
		extra := make([]string, 0, len(byRole))
		for role := range byRole {
			extra = append(extra, role)
		}
		sort.Strings(extra)
		return nil, fmt.Errorf("canmap: bindings name unknown roles: %v", extra)
	}
	if errs := out.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("canmap: instantiated registry invalid: %s", joinErrors(errs))
	}
	return out, nil
}

func cloneRegistry(r *Registry) *Registry {
	out := *r
	out.Nodes = append([]Node(nil), r.Nodes...)
	out.Signals = make([]Signal, len(r.Signals))
	for i, s := range r.Signals {
		cp := s
		cp.Producer.Mapping = append([]MapEntry(nil), s.Producer.Mapping...)
		cp.Consumers = make([]Consumer, len(s.Consumers))
		for j, c := range s.Consumers {
			cc := c
			cc.Mapping = append([]MapEntry(nil), c.Mapping...)
			cc.SourceSelects = append([]SDOWrite(nil), c.SourceSelects...)
			cp.Consumers[j] = cc
		}
		out.Signals[i] = cp
	}
	return &out
}
