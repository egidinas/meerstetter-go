package export

import "testing"

func TestDefaultArchiveManifestCoversMVPStreams(t *testing.T) {
	manifest := DefaultArchiveManifest()
	if manifest.Schema == "" || manifest.Version == 0 {
		t.Fatalf("manifest identity missing: %#v", manifest)
	}

	formats := map[string]string{}
	for _, format := range manifest.Formats {
		formats[format.Name] = format.Status
	}
	if formats["ndjson"] != "implemented" {
		t.Fatalf("ndjson format status = %q", formats["ndjson"])
	}
	if formats["arrow_ipc"] != "implemented" {
		t.Fatalf("arrow_ipc format status = %q", formats["arrow_ipc"])
	}
	if formats["hdf5"] != "planned_contract" {
		t.Fatalf("hdf5 format status = %q", formats["hdf5"])
	}

	streams := map[string]Stream{}
	for _, stream := range manifest.Streams {
		streams[stream.Name] = stream
	}
	for _, name := range []string{
		"telemetry_samples",
		"can_frames",
		"command_events",
		"object_dictionary_snapshots",
		"graph_wall_assignments",
	} {
		stream, ok := streams[name]
		if !ok {
			t.Fatalf("missing stream %q", name)
		}
		if len(stream.PrimaryKey) == 0 || stream.TimeField == "" || len(stream.Fields) == 0 {
			t.Fatalf("stream %q is underspecified: %#v", name, stream)
		}
	}

	requireField(t, streams["telemetry_samples"], "target_id")
	requireField(t, streams["telemetry_samples"], "value")
	requireField(t, streams["can_frames"], "data_hex")
	requireField(t, streams["object_dictionary_snapshots"], "write_path")
	requireField(t, streams["graph_wall_assignments"], "tile_id")
}

func requireField(t *testing.T, stream Stream, name string) {
	t.Helper()
	for _, field := range stream.Fields {
		if field.Name == name {
			return
		}
	}
	t.Fatalf("stream %q missing field %q", stream.Name, name)
}
