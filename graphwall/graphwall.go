package graphwall

import "github.com/egidinas/meerstetter-go/discovery"

type TileKind string

const (
	TileTrend   TileKind = "trend"
	TileState   TileKind = "state"
	TileCommand TileKind = "command"
	TileLog     TileKind = "log"
)

type Assignment struct {
	WallID   string           `json:"wall_id"`
	TileID   string           `json:"tile_id"`
	Kind     TileKind         `json:"kind"`
	Target   discovery.Target `json:"target"`
	Position Position         `json:"position"`
	Options  map[string]any   `json:"options,omitempty"`
}

type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}
