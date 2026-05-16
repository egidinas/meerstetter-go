package export

// Manifest describes the durable archive shape exposed by meerstetter-go.
// NDJSON and Arrow IPC are available today. HDF5 writers should target these
// stream and field names without changing the UI, source catalogue, or
// import-review route.
type Manifest struct {
	Schema       string       `json:"schema"`
	Version      int          `json:"version"`
	Description  string       `json:"description"`
	Formats      []Format     `json:"formats"`
	Streams      []Stream     `json:"streams"`
	ReviewPolicy ReviewPolicy `json:"review_policy"`
}

type Format struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Status    string `json:"status"`
	Route     string `json:"route,omitempty"`
	Notes     string `json:"notes,omitempty"`
}

type Stream struct {
	Name         string   `json:"name"`
	Purpose      string   `json:"purpose"`
	Grain        string   `json:"grain"`
	PrimaryKey   []string `json:"primary_key"`
	TimeField    string   `json:"time_field"`
	SourceRoutes []string `json:"source_routes"`
	Fields       []Field  `json:"fields"`
}

type Field struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Unit     string `json:"unit,omitempty"`
	Required bool   `json:"required,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

type ReviewPolicy struct {
	ImportMode          string   `json:"import_mode"`
	DeduplicateBy       []string `json:"deduplicate_by"`
	RejectOnMissing     []string `json:"reject_on_missing"`
	PreferredLiveSource string   `json:"preferred_live_source"`
	FallbackLiveSources []string `json:"fallback_live_sources"`
}

func DefaultArchiveManifest() Manifest {
	return Manifest{
		Schema:      "meerstettergo.archive.manifest",
		Version:     1,
		Description: "Durable archive contract for Meerstetter TEC telemetry, CAN evidence, command events, and UI/source-catalogue state.",
		Formats: []Format{
			{
				Name:      "ndjson",
				MediaType: "application/x-ndjson",
				Status:    "implemented",
				Route:     "/api/log/export",
				Notes:     "Current portable export and import-review path.",
			},
			{
				Name:      "arrow_ipc",
				MediaType: "application/vnd.apache.arrow.stream",
				Status:    "implemented",
				Route:     "/api/log/export?format=arrow_ipc",
				Notes:     "Columnar telemetry_samples stream for SignalForge and downstream consumers.",
			},
			{
				Name:      "hdf5",
				MediaType: "application/x-hdf5",
				Status:    "planned_contract",
				Notes:     "Long-term archive writer should preserve these stream names and primary keys.",
			},
		},
		Streams: []Stream{
			{
				Name:         "telemetry_samples",
				Purpose:      "Decoded MeCom/CAN values after catalogue mapping, reduction, and freshness tagging.",
				Grain:        "one row per target value sample",
				PrimaryKey:   []string{"seq"},
				TimeField:    "time",
				SourceRoutes: []string{"/api/log/ring", "/api/tiles", "/api/graph-wall"},
				Fields: []Field{
					{Name: "seq", Type: "uint64", Required: true},
					{Name: "time", Type: "timestamp_ns", Required: true},
					{Name: "target_id", Type: "string", Required: true},
					{Name: "device_id", Type: "string", Required: true},
					{Name: "device_alias", Type: "string"},
					{Name: "instance", Type: "string"},
					{Name: "parameter", Type: "string", Required: true},
					{Name: "type", Type: "string"},
					{Name: "subtype", Type: "string"},
					{Name: "value", Type: "float64", Required: true},
					{Name: "unit", Type: "string"},
					{Name: "quality", Type: "string", Required: true},
					{Name: "source_path", Type: "string", Notes: "Preferred transport, such as pixtend-can or serial-ftdi."},
				},
			},
			{
				Name:         "can_frames",
				Purpose:      "Raw SocketCAN receive evidence from the PiXtend route, reconciled across RAM and flash rings.",
				Grain:        "one row per received CAN frame",
				PrimaryKey:   []string{"time", "interface", "id", "dlc", "data_hex"},
				TimeField:    "time",
				SourceRoutes: []string{"/api/can/ring?source=merged"},
				Fields: []Field{
					{Name: "seq", Type: "uint64"},
					{Name: "time", Type: "timestamp_ns", Required: true},
					{Name: "interface", Type: "string", Required: true},
					{Name: "id", Type: "uint32", Required: true},
					{Name: "dlc", Type: "uint8", Required: true},
					{Name: "data_hex", Type: "hex", Required: true},
					{Name: "source", Type: "string", Notes: "primary_ram or fallback_flash."},
				},
			},
			{
				Name:         "command_events",
				Purpose:      "Sequencer and write-path receipts, including lease-gated accepts and rejects.",
				Grain:        "one row per command state transition",
				PrimaryKey:   []string{"seq"},
				TimeField:    "time",
				SourceRoutes: []string{"/api/log/ring", "/api/events/swimlane"},
				Fields: []Field{
					{Name: "seq", Type: "uint64", Required: true},
					{Name: "time", Type: "timestamp_ns", Required: true},
					{Name: "target_id", Type: "string"},
					{Name: "write_path", Type: "string"},
					{Name: "status", Type: "string", Required: true},
					{Name: "owner", Type: "string"},
					{Name: "message", Type: "string"},
				},
			},
			{
				Name:         "object_dictionary_snapshots",
				Purpose:      "Catalogue rows used to interpret telemetry and expose writable paths.",
				Grain:        "one row per discovered target/parameter/instance",
				PrimaryKey:   []string{"target_id", "parameter", "instance"},
				TimeField:    "generated_at",
				SourceRoutes: []string{"/api/catalogue", "/api/discovery/tree"},
				Fields: []Field{
					{Name: "generated_at", Type: "timestamp_ns", Required: true},
					{Name: "target_id", Type: "string", Required: true},
					{Name: "device_id", Type: "string", Required: true},
					{Name: "type", Type: "string", Required: true},
					{Name: "subtype", Type: "string"},
					{Name: "parameter", Type: "string", Required: true},
					{Name: "instance", Type: "string"},
					{Name: "read_path", Type: "string", Required: true},
					{Name: "write_path", Type: "string"},
					{Name: "writable", Type: "bool", Required: true},
				},
			},
			{
				Name:         "graph_wall_assignments",
				Purpose:      "Operator graph-wall tile layout and semantic target binding.",
				Grain:        "one row per tile assignment",
				PrimaryKey:   []string{"wall_id", "tile_id"},
				TimeField:    "generated_at",
				SourceRoutes: []string{"/api/graph-wall"},
				Fields: []Field{
					{Name: "generated_at", Type: "timestamp_ns", Required: true},
					{Name: "wall_id", Type: "string", Required: true},
					{Name: "tile_id", Type: "string", Required: true},
					{Name: "target_id", Type: "string", Required: true},
					{Name: "kind", Type: "string", Required: true},
					{Name: "section", Type: "string"},
					{Name: "position", Type: "json"},
				},
			},
		},
		ReviewPolicy: ReviewPolicy{
			ImportMode:          "review_only_until_explicit_commit_route_exists",
			DeduplicateBy:       []string{"seq", "time", "target_id", "interface", "id", "dlc", "data_hex"},
			RejectOnMissing:     []string{"time", "target_id_or_can_frame_identity"},
			PreferredLiveSource: "pixtend_socketcan",
			FallbackLiveSources: []string{"serial_ftdi", "ram_ring", "flash_ring", "tec_internal_ring"},
		},
	}
}
