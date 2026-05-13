package utility

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/egidinas/loom-gossamer-shared/go/discovery"
	"github.com/egidinas/loom-gossamer-shared/go/graphsem"
	graphwall "github.com/egidinas/loom-gossamer-shared/go/graphwall"
	"github.com/egidinas/loom-gossamer-shared/go/telemetrytiles"
	"github.com/egidinas/loom-gossamer-shared/go/tmtc"
	"github.com/egidinas/loom-gossamer-shared/go/tmtclog"
	"github.com/egidinas/meerstetter-go/canopen"
	"github.com/egidinas/meerstetter-go/canring"
	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/mecomserver"
)

func TestServerExposesDiscoveryAndBaselineGraphWall(t *testing.T) {
	server := newTestServer(t)

	for _, path := range []string{"/health", "/api/health", "/api/operator/meerstettergo/health"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s health status = %d body=%s", path, rec.Code, rec.Body.String())
		}
		if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
			t.Fatalf("%s health content-type = %q body=%s", path, contentType, rec.Body.String())
		}
		var health map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&health); err != nil {
			t.Fatalf("%s health JSON decode failed: %v", path, err)
		}
		if health["ok"] != true {
			t.Fatalf("%s health ok = %#v", path, health["ok"])
		}
	}

	for _, path := range []string{
		"/api/discovery/tree",
		"/api/loom/discovery-tree",
		"/api/loom/discovery/tree",
		"/api/operator/meerstettergo/discovery/tree",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s discovery status = %d body=%s", path, rec.Code, rec.Body.String())
		}
		if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
			t.Fatalf("%s discovery content-type = %q body=%s", path, contentType, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Object Temperature") || !strings.Contains(rec.Body.String(), "Output Power") {
			t.Fatalf("%s baseline discovery missing: %s", path, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/graph-wall", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	var assignments []graphWallAssignment
	if err := json.NewDecoder(rec.Body).Decode(&assignments); err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 4 {
		t.Fatalf("assignments = %d %#v", len(assignments), assignments)
	}
	if assignments[0].WallID != "baseline" {
		t.Fatalf("wall id = %q", assignments[0].WallID)
	}
	for _, assignment := range assignments {
		if assignment.Target.Metadata["baseline"] == "true" {
			t.Fatalf("default wall should use grouped semantic tiles, got %#v", assignment)
		}
	}
}

func TestDefaultGraphWallDoesNotOverlapDevices(t *testing.T) {
	cfg := testConfig()
	cfg.Devices = append(cfg.Devices, mecomserver.DeviceSpec{ID: "tec-02", Target: "tcp:192.168.1.51:50000"})
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[graphwall.Position]string{}
	for _, assignment := range server.GraphWall() {
		if previous, ok := seen[assignment.Position]; ok {
			t.Fatalf("position overlap %v between %s and %s", assignment.Position, previous, assignment.Target.ID)
		}
		seen[assignment.Position] = assignment.Target.ID
	}
}

func TestDefaultGraphWallFocusesFourControllerTECSignals(t *testing.T) {
	server := newTestServer(t)
	assignments := server.GraphWall()

	temperatures := graphWallAssignmentByTile(assignments, "all-temperatures")
	if temperatures == nil {
		t.Fatalf("missing all-temperatures assignment: %#v", assignments)
	}
	if !optionStringListContains(temperatures.Options, "focus_signals", "mecom.tec_04.cascade_temp_c") {
		t.Fatalf("temperature focus signals missing cascade signal for controller 4: %#v", temperatures.Options)
	}
	if temperatures.Options["reduction_policy"] != "mean_stddev_window_to_consumer_rate" {
		t.Fatalf("temperature reduction policy = %#v", temperatures.Options["reduction_policy"])
	}

	powers := graphWallAssignmentByTile(assignments, "all-powers")
	if powers == nil {
		t.Fatalf("missing all-powers assignment: %#v", assignments)
	}
	if !optionStringListContains(powers.Options, "focus_signals", "mecom.tec_04.hot_side_dissipated_w") {
		t.Fatalf("power focus signals missing hot-side dissipated heat for controller 4: %#v", powers.Options)
	}
}

func TestUtilityExposesFullTECCatalogueForSignalForge(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/tec/catalogue", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalogue status = %d body=%s", rec.Code, rec.Body.String())
	}
	var catalogue graphsem.SourceCatalogue
	if err := json.NewDecoder(rec.Body).Decode(&catalogue); err != nil {
		t.Fatalf("decode catalogue: %v", err)
	}
	rows := map[string]graphsem.SourceCatalogueRow{}
	for _, row := range catalogue.Entries {
		rows[row.TraceID] = row
	}
	for _, traceID := range []string{
		"mecom.tec_04.cascade_temp_c",
		"mecom.tec_04.hot_side_dissipated_w",
	} {
		if _, ok := rows[traceID]; !ok {
			t.Fatalf("catalogue missing trace %q rows=%#v", traceID, rows)
		}
	}
	if got := rows["mecom.tec_04.cascade_temp_c"].Metadata["background_readout"]; got != mecom.ReadoutVXRoundRobinQueue {
		t.Fatalf("cascade background readout = %q", got)
	}
	if got := rows["mecom.tec_04.hot_side_dissipated_w"].Metadata["preferred_readout"]; got != mecom.ReadoutDerivedChannelModel {
		t.Fatalf("hot-side preferred readout = %q", got)
	}
	if got := rows["mecom.tec_04.hot_side_dissipated_w"].Metadata["source_readout"]; got != mecom.ReadoutCRTVStreamRingBuffer {
		t.Fatalf("hot-side source readout = %q", got)
	}
}

func TestUtilityExposesLoomSourceCatalogueWithSequencerWritePath(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/loom/source-catalogue", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("loom catalogue status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload graphsem.GlobalSourceCatalogue
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode loom catalogue: %v", err)
	}
	if payload.SchemaVersion != graphsem.CurrentSourceCatalogueSchemaVersion {
		t.Fatalf("schema version = %d, want %d", payload.SchemaVersion, graphsem.CurrentSourceCatalogueSchemaVersion)
	}
	if len(payload.Catalogues) != 1 {
		t.Fatalf("catalogues = %d, want 1", len(payload.Catalogues))
	}
	catalogue := payload.Catalogues[0]
	if catalogue.SourceID != "mecom_tec_bank_a" {
		t.Fatalf("source id = %q", catalogue.SourceID)
	}
	if catalogue.Capabilities.SubscriptionEndpoint != "/api/operator/meerstettergo/target/read" {
		t.Fatalf("subscription endpoint = %q", catalogue.Capabilities.SubscriptionEndpoint)
	}
	routes := map[string]graphsem.RemoteRoute{}
	for _, route := range catalogue.Capabilities.RemoteRoutes {
		routes[route.RouteID] = route
	}
	for _, want := range []string{
		"meerstettergo.pixtend.catalogue",
		"meerstettergo.pixtend.health",
		"meerstettergo.pixtend.discovery_tree",
		"meerstettergo.pixtend.graph_wall",
		"meerstettergo.pixtend.tiles",
		"meerstettergo.pixtend.log_ring",
		"meerstettergo.pixtend.can_ring",
		"meerstettergo.pixtend.polling_status",
		"meerstettergo.pixtend.read",
		"meerstettergo.pixtend.write",
	} {
		if _, ok := routes[want]; !ok {
			t.Fatalf("missing advertised remote route %q in %#v", want, routes)
		}
	}
	if routes["meerstettergo.pixtend.tiles"].GatewayEndpoint != "/api/operator/meerstettergo/tiles" {
		t.Fatalf("tiles gateway endpoint = %#v", routes["meerstettergo.pixtend.tiles"])
	}
	if routes["meerstettergo.pixtend.polling_status"].GatewayEndpoint != "/api/operator/meerstettergo/polling/status" {
		t.Fatalf("polling status gateway endpoint = %#v", routes["meerstettergo.pixtend.polling_status"])
	}

	var targetRow *graphsem.SourceCatalogueRow
	for i := range catalogue.Entries {
		if strings.Contains(catalogue.Entries[i].TraceID, "target_object_temp_c") {
			targetRow = &catalogue.Entries[i]
			break
		}
	}
	if targetRow == nil {
		t.Fatalf("missing writable target temperature row in %#v", catalogue.Entries)
	}
	if targetRow.Access != "read_write" {
		t.Fatalf("target access = %q", targetRow.Access)
	}
	if targetRow.TargetID == "" || targetRow.Metadata["target_id"] != targetRow.TargetID {
		t.Fatalf("target id metadata mismatch row=%#v", targetRow)
	}
	if targetRow.Metadata["sequencer_write_path"] != "/api/operator/meerstettergo/target/write" {
		t.Fatalf("write path metadata = %#v", targetRow.Metadata)
	}
	if !strings.Contains(targetRow.Metadata["loom_read_path"], "/api/operator/meerstettergo/target/read?id=") {
		t.Fatalf("read path metadata = %#v", targetRow.Metadata)
	}
	if targetRow.RemoteRoute == nil || !targetRow.RemoteRoute.LeaseRequired || !targetRow.RemoteRoute.ReceiptRequired {
		t.Fatalf("remote route should require lease and receipt: %#v", targetRow.RemoteRoute)
	}

	if len(catalogue.CommandInputs) != 1 {
		t.Fatalf("command inputs = %#v", catalogue.CommandInputs)
	}
	command := catalogue.CommandInputs[0]
	if command.CommandID != "meerstettergo.tec.write" {
		t.Fatalf("command id = %q", command.CommandID)
	}
	if !stringSliceContains(command.RelatedTraceIDs, targetRow.TraceID) {
		t.Fatalf("command related trace ids = %#v, want %q", command.RelatedTraceIDs, targetRow.TraceID)
	}
	if len(payload.DiscoveredCatalogues) != 1 || payload.DiscoveredCatalogues[0].RouteHint != "/api/operator/meerstettergo/source-catalogue" {
		t.Fatalf("discovered catalogues = %#v", payload.DiscoveredCatalogues)
	}
}

func TestUtilityExposesAdvertisedOperatorRoutes(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/operator/meerstettergo/source-catalogue", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("operator catalogue status = %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("operator catalogue content-type = %q body=%s", ct, rec.Body.String())
	}
	var payload graphsem.GlobalSourceCatalogue
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode operator catalogue: %v", err)
	}
	if len(payload.Catalogues) != 1 || payload.Catalogues[0].SourceID != "mecom_tec_bank_a" {
		t.Fatalf("operator catalogue payload = %#v", payload.Catalogues)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/operator/meerstettergo/target/read", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("operator read without id status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "<!doctype html") {
		t.Fatalf("operator read fell through to index: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/operator/meerstettergo/target/write", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("operator write GET status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "<!doctype html") {
		t.Fatalf("operator write fell through to index: %s", rec.Body.String())
	}
}

func TestUtilityActivatesTECCatalogueWithInitializedTelemetry(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/log/ring?limit=200", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	var entries []struct {
		TM tmtc.Telemetry `json:"tm"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode ring: %v", err)
	}
	byTarget := map[string]tmtc.Telemetry{}
	for _, entry := range entries {
		byTarget[entry.TM.TargetID] = entry.TM
	}
	cascade := byTarget["device:tec-01:mecom.tec_04.cascade_temp_c"]
	if cascade.TargetID == "" {
		t.Fatalf("initialized telemetry missing controller 4 cascade target targets=%#v", byTarget)
	}
	if cascade.Metadata["catalogue_active"] != "true" || cascade.Metadata["value_state"] != "not_sampled" || cascade.Metadata["readout"] != mecom.ReadoutVXRoundRobinQueue {
		t.Fatalf("cascade metadata = %#v", cascade.Metadata)
	}
	hotSide := byTarget["device:tec-01:mecom.tec_04.hot_side_dissipated_w"]
	if hotSide.TargetID == "" {
		t.Fatalf("initialized telemetry missing controller 4 hot-side target targets=%#v", byTarget)
	}
	if hotSide.Metadata["catalogue_active"] != "true" || hotSide.Metadata["value_state"] != "not_sampled" || hotSide.Metadata["readout"] != mecom.ReadoutDerivedChannelModel {
		t.Fatalf("hot-side metadata = %#v", hotSide.Metadata)
	}
}

func TestUtilityBaselineReadoutUsesFullFourControllerCatalogue(t *testing.T) {
	params := utilityBaselineReadoutParameters(testConfig())

	if len(params) != len(mecom.DefaultTECReadoutParameters(4)) {
		t.Fatalf("readout parameter count = %d, want %d", len(params), len(mecom.DefaultTECReadoutParameters(4)))
	}
	if !readoutParameterSensorExists(params, "mecom.tec_04.cascade_temp_c") {
		t.Fatalf("missing controller 4 cascade temperature in %#v", params)
	}
}

func TestUtilityBaselinePollingUsesReadoutRoundRobinAndCurrentWritableValues(t *testing.T) {
	server := newTestServer(t)
	device := server.hub.Devices[0]
	client := &fakeUtilityReadClient{}

	server.pollBaselineClient(context.Background(), device, client, time.Unix(100, 0))

	if len(client.configured) == 0 {
		t.Fatal("ring capture was not configured for high-priority values")
	}
	if len(client.bulkParams) == 0 {
		t.Fatal("round-robin bulk read was not used")
	}
	byTarget := ringTelemetryByTarget(t, server, 300)
	if byTarget["device:tec-01:mecom.tec_03.cascade_temp_c"].TargetID == "" {
		t.Fatalf("poll telemetry missing controller 3 cascade target targets=%#v", byTarget)
	}
	target := byTarget["device:tec-01:mecom.tec_04.target_object_temp_c"]
	if target.TargetID == "" {
		t.Fatalf("poll telemetry missing controller 4 writable target targets=%#v", byTarget)
	}
	if got := numericTelemetryValue(t, target.Value); got != 30004 {
		t.Fatalf("target value = %v, want 30004", got)
	}
	if target.Metadata["readout"] != mecom.ReadoutVXRoundRobinQueue {
		t.Fatalf("target readout = %q metadata=%#v", target.Metadata["readout"], target.Metadata)
	}
	if target.Metadata["writable"] != "true" || target.Metadata["write_path"] != "/api/target/write" {
		t.Fatalf("target write metadata = %#v", target.Metadata)
	}
}

func TestUtilityBaselinePollingPublishesUnavailableReadoutValues(t *testing.T) {
	server := newTestServer(t)
	device := server.hub.Devices[0]
	client := &fakeUtilityReadClient{
		nanParameterIDs: map[int]bool{mecom.TECParamOutputCurrent: true},
	}

	server.pollBaselineClient(context.Background(), device, client, time.Unix(101, 0))

	byTarget := ringTelemetryByTarget(t, server, 300)
	target := byTarget["device:tec-01:mecom.tec_01.output_current_a"]
	if target.TargetID == "" {
		t.Fatalf("poll telemetry missing unavailable target targets=%#v", byTarget)
	}
	if target.Value != nil || target.Metadata["value_state"] != "not_applicable" || target.Quality != "ok" {
		t.Fatalf("unavailable telemetry = value=%#v quality=%q metadata=%#v", target.Value, target.Quality, target.Metadata)
	}
	derived := byTarget["device:tec-01:mecom.tec_01.electrical_input_w"]
	if derived.TargetID == "" || derived.Value != nil || derived.Metadata["value_state"] != "not_applicable" {
		t.Fatalf("derived unavailable telemetry = %#v", derived)
	}
}

func TestDiscoveryTreeExposesSortedInstancedTECCatalogueAndWritablePath(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/discovery/tree", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	body := compactJSON(t, rec.Body.Bytes())
	for _, want := range []string{
		`"Signals"`,
		`"Thermal"`,
		`"Target Object Temperature"`,
		`"tec-01 (TEC controller)"`,
		`"device:tec-01:mecom.tec_04.target_object_temp_c"`,
		`"write_path":"/api/target/write"`,
		`"read_path":"/api/target/read?id=device%3Atec-01%3Amecom.tec_04.target_object_temp_c"`,
		`"access":"read_write"`,
		`"instance":"4"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("tree missing %q body=%s", want, body)
		}
	}
	electricalIdx := strings.Index(body, `"Electrical"`)
	powerIdx := strings.Index(body, `"Power"`)
	thermalIdx := strings.Index(body, `"Thermal"`)
	if electricalIdx < 0 || powerIdx < 0 || thermalIdx < 0 || !(electricalIdx < powerIdx && powerIdx < thermalIdx) {
		t.Fatalf("signal tree should be sorted by type before subtype/parameter body=%s", body)
	}
}

func TestServerExposesEventSwimlaneFromRing(t *testing.T) {
	server := newTestServer(t)
	if err := server.recorder.PublishCommandEvent(tmtc.CommandEvent{Status: tmtc.CommandFailed, Error: "timeout"}); err != nil {
		t.Fatal(err)
	}
	if err := server.recorder.PublishTelemetry(tmtc.Telemetry{TargetID: "device:tec-01:mecom.tec_01.object_temp_c", Name: "Object Temperature", Quality: "warning"}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/events/swimlane", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `"lane": "commands"`) || !strings.Contains(body, `"severity": "error"`) || !strings.Contains(body, `"lane": "telemetry"`) {
		t.Fatalf("events body = %s", body)
	}
}

func TestServerSanitizesNonFiniteTelemetryForJSON(t *testing.T) {
	server := newTestServer(t)
	device := server.hub.Devices[0]
	server.publishReadoutValue(device, mecom.ReadoutValue{
		Parameter:  mecom.Parameter{ID: 1000, Instance: 1, Name: "Object Temperature", Unit: "degC", Type: mecom.DataTypeFloat32},
		Sensor:     "mecom.tec_01.object_temp_c",
		Value:      math.NaN(),
		ObservedAt: time.Now().UTC(),
		Readout:    mecom.ReadoutVXRoundRobinQueue,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/log/ring", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ring status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := compactJSON(t, rec.Body.Bytes())
	if !strings.Contains(body, `"quality":"ok"`) || !strings.Contains(body, `"raw_float":"NaN"`) || !strings.Contains(body, `"value_state":"not_applicable"`) {
		t.Fatalf("ring body = %s", body)
	}
}

func TestServerExposesPollingStatusForPixtendRoute(t *testing.T) {
	server := newTestServer(t)
	device := server.hub.Devices[0]
	server.publishReadoutValue(device, mecom.ReadoutValue{
		Parameter:  mecom.Parameter{ID: 1000, Instance: 1, Name: "Object Temperature", Unit: "degC", Type: mecom.DataTypeFloat32},
		Sensor:     "mecom.tec_01.object_temp_c",
		Value:      21.5,
		ObservedAt: time.Now().UTC(),
		Readout:    mecom.ReadoutVXRoundRobinQueue,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/operator/meerstettergo/polling/status", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("polling status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := compactJSON(t, rec.Body.Bytes())
	for _, want := range []string{
		`"live":true`,
		`"fresh_count":1`,
		`"tier":"high_priority"`,
		`"freshness_budget_seconds":10`,
		`"target_id":"device:tec-01:mecom.tec_01.object_temp_c"`,
		`"quality":"ok"`,
		`"value_state":"sampled"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("polling status missing %q body=%s", want, body)
		}
	}
}

func TestRingCanReplayInBatches(t *testing.T) {
	server, err := NewServer(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := server.recorder.PublishTelemetry(tmtc.Telemetry{
			ID:       string(rune('a' + i)),
			TargetID: "device:tec-01:mecom.tec_01.object_temp_c",
			Value:    float64(i),
			Quality:  "ok",
		}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/log/ring?after_seq=0&limit=2", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ring status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := strings.Count(rec.Body.String(), `"kind": "telemetry"`); got != 2 {
		t.Fatalf("telemetry count = %d body=%s", got, rec.Body.String())
	}
}

func TestRingCanBootstrapFromTailWithoutChangingReplay(t *testing.T) {
	server, err := NewServer(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := server.recorder.PublishTelemetry(tmtc.Telemetry{
			ID:       fmt.Sprintf("sample-%d", i),
			TargetID: "device:tec-01:mecom.tec_01.object_temp_c",
			Value:    float64(i),
			Quality:  "ok",
		}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/log/ring?tail=true&limit=2", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tail ring status = %d body=%s", rec.Code, rec.Body.String())
	}
	var tail []tmtclog.Entry
	if err := json.NewDecoder(rec.Body).Decode(&tail); err != nil {
		t.Fatalf("decode tail ring: %v body=%s", err, rec.Body.String())
	}
	if len(tail) != 2 || tail[0].TM == nil || tail[1].TM == nil || tail[0].TM.Value != float64(1) || tail[1].TM.Value != float64(2) {
		t.Fatalf("tail entries = %#v", tail)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/log/ring?after_seq=0&limit=2", nil)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("replay ring status = %d body=%s", rec.Code, rec.Body.String())
	}
	var replay []tmtclog.Entry
	if err := json.NewDecoder(rec.Body).Decode(&replay); err != nil {
		t.Fatalf("decode replay ring: %v body=%s", err, rec.Body.String())
	}
	if len(replay) != 2 || replay[0].Seq >= tail[0].Seq {
		t.Fatalf("replay changed to tail semantics replay=%#v tail=%#v", replay, tail)
	}
}

func TestLogExportAndImportReviewRoundTrip(t *testing.T) {
	server, err := NewServer(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	afterSeq := server.recorder.Ring().LatestSeq()
	if err := server.recorder.PublishTelemetry(tmtc.Telemetry{
		ID:       "sample-temp",
		TargetID: "device:tec-01:mecom.tec_01.object_temp_c",
		Name:     "Object Temperature",
		Value:    21.5,
		Quality:  "ok",
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.recorder.PublishCommandEvent(tmtc.CommandEvent{Status: tmtc.CommandCompleted}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/log/export?after_seq="+strconv.FormatUint(afterSeq, 10)+"&limit=2", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("export content-type = %q", got)
	}
	exported := rec.Body.String()
	if got := strings.Count(strings.TrimSpace(exported), "\n") + 1; got != 2 {
		t.Fatalf("export line count = %d body=%s", got, exported)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/log/import/review", bytes.NewBufferString(exported))
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import review status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := compactJSON(t, rec.Body.Bytes())
	for _, want := range []string{
		`"mode":"review_only"`,
		`"committed":false`,
		`"entry_count":2`,
		`"telemetry":1`,
		`"command_event":1`,
		`"tec-01"`,
		`"device:tec-01:mecom.tec_01.object_temp_c"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("review missing %q body=%s", want, body)
		}
	}
}

func TestLogExportArrowIPCTelemetryStream(t *testing.T) {
	server, err := NewServer(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	afterSeq := server.recorder.Ring().LatestSeq()
	if err := server.recorder.PublishTelemetry(tmtc.Telemetry{
		ID:       "sample-temp",
		TargetID: "device:tec-01:mecom.tec_01.object_temp_c",
		Name:     "Object Temperature",
		Time:     time.Unix(10, 123).UTC(),
		Value:    21.5,
		Unit:     "degC",
		Quality:  "ok",
		Metadata: map[string]string{
			"device_id":           "tec-01",
			"device_alias":        "TEC 01",
			"mecom_instance":      "1",
			"parameter_name":      "Object Temperature",
			"category":            "temperature",
			"subtype":             "object",
			"preferred_transport": "pixtend-can",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.recorder.PublishCommandEvent(tmtc.CommandEvent{Status: tmtc.CommandCompleted}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/log/export?format=arrow_ipc&after_seq="+strconv.FormatUint(afterSeq, 10)+"&limit=2", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("arrow export status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != arrowIPCContentType {
		t.Fatalf("arrow export content-type = %q", got)
	}
	reader, err := ipc.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("new arrow reader: %v", err)
	}
	defer reader.Release()
	if !reader.Next() {
		t.Fatal("arrow stream has no record")
	}
	record := reader.Record()
	if got := record.NumRows(); got != 1 {
		t.Fatalf("arrow rows = %d", got)
	}
	if got := record.Schema().Field(0).Name; got != "seq" {
		t.Fatalf("first arrow field = %q", got)
	}
	if got := record.Column(2).(*array.String).Value(0); got != "device:tec-01:mecom.tec_01.object_temp_c" {
		t.Fatalf("target_id = %q", got)
	}
	if got := record.Column(3).(*array.String).Value(0); got != "tec-01" {
		t.Fatalf("device_id = %q", got)
	}
	if got := record.Column(6).(*array.String).Value(0); got != "Object Temperature" {
		t.Fatalf("parameter = %q", got)
	}
	if got := record.Column(9).(*array.Float64).Value(0); got != 21.5 {
		t.Fatalf("value = %v", got)
	}
	if got := record.Column(12).(*array.String).Value(0); got != "pixtend-can" {
		t.Fatalf("source_path = %q", got)
	}
	if reader.Next() {
		t.Fatal("arrow stream has unexpected second record")
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("arrow reader err: %v", err)
	}
}

func TestLogArchiveManifestExposesDurableExportContract(t *testing.T) {
	server, err := NewServer(testConfig())
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/api/log/archive/manifest",
		"/api/operator/meerstettergo/log/archive/manifest",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
		body := compactJSON(t, rec.Body.Bytes())
		for _, want := range []string{
			`"schema":"meerstettergo.archive.manifest"`,
			`"name":"telemetry_samples"`,
			`"name":"can_frames"`,
			`"name":"command_events"`,
			`"name":"object_dictionary_snapshots"`,
			`"name":"graph_wall_assignments"`,
			`"name":"hdf5"`,
			`"name":"arrow_ipc"`,
			`"route":"/api/log/export?format=arrow_ipc"`,
			`"preferred_live_source":"pixtend_socketcan"`,
			`"fallback_live_sources":["serial_ftdi","ram_ring","flash_ring","tec_internal_ring"]`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q body=%s", path, want, body)
			}
		}
	}
}

func TestLogReviewSummarizesCurrentRing(t *testing.T) {
	server, err := NewServer(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := server.recorder.PublishTelemetry(tmtc.Telemetry{
		TargetID: "device:tec-02:mecom.tec_02.object_temp_c",
		Quality:  "warning",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/log/review?limit=1000", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("log review status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := compactJSON(t, rec.Body.Bytes())
	for _, want := range []string{`"entry_count"`, `"warning":1`, `"tec-02"`, `"seq_min"`, `"seq_max"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("log review missing %q body=%s", want, body)
		}
	}
}

func TestLogExportAndReviewCanUseTailSemantics(t *testing.T) {
	server, err := NewServer(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := server.recorder.PublishTelemetry(tmtc.Telemetry{
			TargetID: fmt.Sprintf("device:tec-%02d:mecom.tec_01.object_temp_c", i+1),
			Quality:  "ok",
			Value:    float64(i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/log/export?tail=true&limit=2", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", rec.Code, rec.Body.String())
	}
	exported := strings.TrimSpace(rec.Body.String())
	if got := strings.Count(exported, "\n") + 1; got != 2 {
		t.Fatalf("tail export line count = %d body=%s", got, exported)
	}
	if !strings.Contains(exported, "tec-03") || !strings.Contains(exported, "tec-04") {
		t.Fatalf("tail export did not include latest entries: %s", exported)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/log/review?tail=true&limit=2", nil)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("review status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := compactJSON(t, rec.Body.Bytes())
	for _, want := range []string{`"entry_count":2`, `"tec-03"`, `"tec-04"`, `"ok":2`} {
		if !strings.Contains(body, want) {
			t.Fatalf("tail review missing %q body=%s", want, body)
		}
	}
}

func TestHealthReportsCANRingStatsWithoutReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pixtend-can0.ring")
	writer, err := canring.OpenWriter(canring.Config{
		Path:       path,
		SizeBytes:  int64(2*4096) + 2*64*1024,
		ChunkBytes: 64 * 1024,
		Interface:  "can0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(canopen.Frame{ID: 0x701, DLC: 1, Data: [8]byte{0x05}}, time.Unix(1700000000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.CANRingPath = path
	cfg.CANRingFallbackPath = filepath.Join(t.TempDir(), "pixtend-can0.bootstrap.ring")
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := compactJSON(t, rec.Body.Bytes())
	for _, want := range []string{`"can_ring"`, `"configured":true`, `"stats"`, `"fallback_path"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("health missing %q body=%s", want, body)
		}
	}
	if strings.Contains(body, `"records"`) || strings.Contains(body, `"data_hex"`) {
		t.Fatalf("health should not replay CAN records body=%s", body)
	}
}

func TestCANRingStorageRoleMarksRAMAsPrimary(t *testing.T) {
	if got := canRingStorageRole("/run/meerstettergo/pixtend-can0.ring"); got != "primary_ram" {
		t.Fatalf("/run role = %q", got)
	}
	if got := canRingStorageRole("/var/lib/meerstettergo/pixtend-can0.ring"); got != "fallback_flash" {
		t.Fatalf("/var role = %q", got)
	}
}

func TestCANRingStatusFallsBackToFlashWhenRAMMissing(t *testing.T) {
	fallbackPath := filepath.Join(t.TempDir(), "pixtend-can0.ring")
	writer, err := canring.OpenWriter(canring.Config{
		Path:       fallbackPath,
		SizeBytes:  int64(2*4096) + 2*64*1024,
		ChunkBytes: 64 * 1024,
		Interface:  "can0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(canopen.Frame{ID: 0x723, DLC: 1, Data: [8]byte{0x05}}, time.Unix(1700000000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.CANRingPath = filepath.Join(t.TempDir(), "missing-primary.ring")
	cfg.CANRingFallbackPath = fallbackPath
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/can/ring?limit=10", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("can ring status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := compactJSON(t, rec.Body.Bytes())
	for _, want := range []string{`"source":"fallback_flash"`, `"degraded":true`, `"primary_error"`, `"id_hex":"723"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("fallback response missing %q body=%s", want, body)
		}
	}
}

func TestCANRingStatusCanReadFlashFallbackExplicitlyWhileRAMHealthy(t *testing.T) {
	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "primary.ring")
	fallbackPath := filepath.Join(dir, "fallback.ring")
	for _, spec := range []struct {
		path string
		id   uint32
	}{
		{path: primaryPath, id: 0x701},
		{path: fallbackPath, id: 0x723},
	} {
		writer, err := canring.OpenWriter(canring.Config{
			Path:       spec.path,
			SizeBytes:  int64(2*4096) + 2*64*1024,
			ChunkBytes: 64 * 1024,
			Interface:  "can0",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Append(canopen.Frame{ID: spec.id, DLC: 1, Data: [8]byte{0x05}}, time.Unix(1700000000, 0)); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	cfg := testConfig()
	cfg.CANRingPath = primaryPath
	cfg.CANRingFallbackPath = fallbackPath
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/can/ring?source=fallback_flash&limit=10", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("can ring status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := compactJSON(t, rec.Body.Bytes())
	for _, want := range []string{`"source":"fallback_flash"`, `"storage":"fallback_flash"`, `"primary_path":`, `"id_hex":"723"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("explicit fallback response missing %q body=%s", want, body)
		}
	}
	if strings.Contains(body, `"id_hex":"701"`) {
		t.Fatalf("explicit fallback response should not return primary RAM records body=%s", body)
	}
}

func TestCANRingMergedSourceReconcilesRAMAndFlash(t *testing.T) {
	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "primary.ring")
	fallbackPath := filepath.Join(dir, "fallback.ring")
	t0 := time.Unix(1700000000, 0)
	for _, spec := range []struct {
		path    string
		records []struct {
			id uint32
			ts time.Time
		}
	}{
		{
			path: primaryPath,
			records: []struct {
				id uint32
				ts time.Time
			}{
				{id: 0x701, ts: t0.Add(20 * time.Millisecond)},
				{id: 0x702, ts: t0.Add(30 * time.Millisecond)},
			},
		},
		{
			path: fallbackPath,
			records: []struct {
				id uint32
				ts time.Time
			}{
				{id: 0x700, ts: t0.Add(10 * time.Millisecond)},
				{id: 0x701, ts: t0.Add(20 * time.Millisecond)},
			},
		},
	} {
		writer, err := canring.OpenWriter(canring.Config{
			Path:       spec.path,
			SizeBytes:  int64(2*4096) + 2*64*1024,
			ChunkBytes: 64 * 1024,
			Interface:  "can0",
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range spec.records {
			if err := writer.Append(canopen.Frame{ID: record.id, DLC: 1, Data: [8]byte{0x05}}, record.ts); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	cfg := testConfig()
	cfg.CANRingPath = primaryPath
	cfg.CANRingFallbackPath = fallbackPath
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/can/ring?source=merged&limit=10", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("can ring status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := compactJSON(t, rec.Body.Bytes())
	for _, want := range []string{`"source":"merged"`, `"storage":"primary_ram+fallback_flash"`, `"id_hex":"700"`, `"id_hex":"701"`, `"id_hex":"702"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("merged response missing %q body=%s", want, body)
		}
	}
	if got := strings.Count(body, `"id_hex":"701"`); got != 1 {
		t.Fatalf("duplicate mirrored frame was not collapsed, count=%d body=%s", got, body)
	}
}

func TestCANRingRejectsUnknownSource(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/can/ring?source=bogus", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTilesEndpointServesServerReducedSeries(t *testing.T) {
	server := newTestServer(t)
	targetID := "device:tec-01:mecom.tec_01.object_temp_c"
	for i := 0; i < 100; i++ {
		if err := server.recorder.PublishTelemetry(tmtc.Telemetry{
			ID:       string(rune('a' + i%26)),
			TargetID: targetID,
			Name:     "Object Temperature",
			Value:    float64(i),
			Unit:     "degC",
			Quality:  "ok",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.recorder.PublishTelemetry(tmtc.Telemetry{
		ID:       "nan",
		TargetID: targetID,
		Name:     "Object Temperature",
		Value:    math.NaN(),
		Unit:     "degC",
		Quality:  "not_applicable",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tiles?target_id="+targetID+"&width=8", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tiles status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload telemetrytiles.Response
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Width != 16 || len(payload.Series) != 1 {
		t.Fatalf("tiles payload = %#v", payload)
	}
	series := payload.Series[0]
	if series.TargetID != targetID || series.Unit != "degC" || series.Quality != "not_applicable" || series.Latest != "NaN" {
		t.Fatalf("tiles series metadata = %#v", series)
	}
	if len(series.Points) == 0 || len(series.Points) > payload.Width*4 {
		t.Fatalf("tiles points should be server reduced for width: len=%d payload=%#v", len(series.Points), payload)
	}
	for _, point := range series.Points {
		if math.IsNaN(point.V) || math.IsInf(point.V, 0) {
			t.Fatalf("non-finite point leaked into tile: %#v", series.Points)
		}
	}
}

func TestTilesEndpointTreatsAggregatePseudoTargetAsAggregate(t *testing.T) {
	server := newTestServer(t)
	targetID := "device:tec-01:mecom.tec_01.object_temp_c"
	if err := server.recorder.PublishTelemetry(tmtc.Telemetry{
		ID:       "temp",
		TargetID: targetID,
		Name:     "Object Temperature",
		Value:    23.5,
		Unit:     "degC",
		Quality:  "ok",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tiles?target_id=aggregate:temperatures&aggregate=temperature&width=8", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tiles status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload telemetrytiles.Response
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Series) != 1 || payload.Series[0].TargetID != targetID {
		t.Fatalf("aggregate pseudo target did not return temperature series: %#v", payload)
	}
}

func TestCloudflareHTTPRedirectsToHTTPS(t *testing.T) {
	server, err := NewServer(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://mecomgo.jmeyer.space/api/health", nil)
	req.Header.Set("X-Forwarded-Proto", "http")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("redirect status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "https://mecomgo.jmeyer.space/api/health" {
		t.Fatalf("redirect location = %q", got)
	}
}

func TestIndexBindsGraphWallToTileTransport(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"/assets/shared/graphwall/renderer.css", "/assets/shared/graphwall/renderer.js", "LoomGraphWall.createGraphWall", "Project", "/api/health", "/api/loom/source-catalogue", "/api/discovery/tree", "/api/log/export", "Export Arrow IPC", "/api/log/import/review", "Review Ring Tail", "target-provenance", "activeTransport", "/api/can/ring", "updateProjectStatus", "/api/log/ring?tail=true", "/api/log/ring?after_seq=", "&limit=", "/api/target/read?id=", "/api/target/write", "/api/graph-wall/assign", "graph-focus", "tree-open", "targetInfoByID", `entry.kind === 'telemetry'`, "appendTelemetry"} {
		if !strings.Contains(body, want) {
			t.Fatalf("index missing %q in body=%s", want, body)
		}
	}
	for _, obsolete := range []string{"function lttb", "drawSparkline", "seriesByTarget", "decimationByTarget", "function drawGraphFrame", "function drawServerSeries", "function drawTileResponse", "targetAggregateGroup", "const palette"} {
		if strings.Contains(body, obsolete) {
			t.Fatalf("index still contains browser-side history reduction %q", obsolete)
		}
	}
}

func TestServerExposesSharedGraphWallAssets(t *testing.T) {
	server := newTestServer(t)
	for _, tc := range []struct {
		path        string
		contentType string
		want        string
	}{
		{path: "/assets/shared/graphwall/renderer.css", contentType: "text/css; charset=utf-8", want: "operator-graph-wall"},
		{path: "/assets/shared/graphwall/renderer.js", contentType: "text/javascript; charset=utf-8", want: "window.LoomGraphWall"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != tc.contentType {
			t.Fatalf("%s content type = %q", tc.path, got)
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s missing %q in body=%s", tc.path, tc.want, rec.Body.String())
		}
	}
}

func TestGraphWallConfigReferencesBusmasterTargetIDs(t *testing.T) {
	cfg := testConfig()
	cfg.GraphWall = []GraphTileConfig{{
		WallID:   "custom",
		TileID:   "object-temp",
		Kind:     graphwall.TileTrend,
		TargetID: "device:tec-01:mecom.tec_01.object_temp_c",
		Position: graphwall.Position{X: 1, Y: 2, W: 3, H: 4},
	}}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(server.graphWall) != 1 || server.graphWall[0].Target.ID != "device:tec-01:mecom.tec_01.object_temp_c" {
		t.Fatalf("graph wall = %#v", server.graphWall)
	}
}

func TestGraphWallAssignAddsTargetToExistingOrNewWall(t *testing.T) {
	server := newTestServer(t)
	body := strings.NewReader(`{"target_id":"device:tec-01:mecom.tec_01.object_temp_c","wall_id":"baseline"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/graph-wall/assign", body)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("assign status = %d body=%s", rec.Code, rec.Body.String())
	}
	assignBody := compactJSON(t, rec.Body.Bytes())
	if !strings.Contains(assignBody, `"ok":true`) || !strings.Contains(assignBody, `"id":"aggregate:temperatures"`) {
		t.Fatalf("assign body = %s", assignBody)
	}

	body = strings.NewReader(`{"target_id":"device:tec-01:mecom.tec_01.output_power_w","new_wall_id":"power-detail"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/graph-wall/assign", body)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("new graph status = %d body=%s", rec.Code, rec.Body.String())
	}
	newGraphBody := compactJSON(t, rec.Body.Bytes())
	if !strings.Contains(newGraphBody, `"wall_id":"power-detail"`) {
		t.Fatalf("new graph body = %s", newGraphBody)
	}
	if !strings.Contains(newGraphBody, `"id":"aggregate:powers"`) {
		t.Fatalf("new graph should use power type graph body=%s", newGraphBody)
	}
}

func TestDiscoveryExpandsRegistryAcrossInstances(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "params.go")
	if err := os.WriteFile(registry, []byte(`package code_reference
var TEC_PARAMETERS = []ParameterDef{
	{ID: 1000, Name: "Object Temperature", Format: "FLOAT32"},
	{ID: 104, Name: "Device Status", Format: "INT32"},
}
var _PARAMETERS = []ParameterDef{
	{ID: 1016, Name: "Laser Diode Current", Format: "FLOAT32"},
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.ParameterRegistryPath = registry
	cfg.Instances = []int{0, 1, 2}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/discovery/tree", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	body := compactJSON(t, rec.Body.Bytes())
	for _, want := range []string{
		"Signals",
		"Global",
		"Instance 1",
		"Instance 2",
		"Object Temperature",
		"Device Status",
		"device:tec-01:mecom:1000:i1",
		"device:tec-01:mecom:104:i2",
		`"read_path":"/api/target/read?id=device%3Atec-01%3Amecom%3A1000%3Ai1"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("discovery missing %q body=%s", want, body)
		}
	}
	if strings.Contains(body, "Laser Diode Current") {
		t.Fatalf("TEC discovery should not include LDD-only values body=%s", body)
	}
	if !strings.Contains(body, `"controller_family":"TEC"`) {
		t.Fatalf("discovery should expose controller family metadata body=%s", body)
	}
	if !strings.Contains(body, `"write_path":"/api/target/write"`) {
		t.Fatalf("discovery should expose writable registry path metadata body=%s", body)
	}
	var tree discovery.TreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &tree); err != nil {
		t.Fatal(err)
	}
	signals := findTreeChild(&tree, "Signals")
	if signals == nil {
		t.Fatalf("missing Signals root in %#v", tree.Children)
	}
	thermal := findTreeChild(signals, "Thermal")
	temperature := findTreeChild(thermal, "Temperature")
	objectTemperature := findTreeChild(temperature, "Object Temperature")
	tec01 := findTreeChild(objectTemperature, "tec-01 (TEC controller)")
	if tec01 == nil {
		t.Fatalf("discovery should pivot signal -> device -> instance tree=%#v", tree)
	}
	if len(tec01.Targets) != 4 {
		t.Fatalf("tec-01 object temperature targets = %d %#v", len(tec01.Targets), tec01.Targets)
	}
	for _, want := range []string{"1", "2", "3", "4"} {
		if !hasTreeTargetInstance(tec01, want) {
			t.Fatalf("tec-01 object temperature missing target %q targets=%#v", want, tec01.Targets)
		}
	}
}

func findTreeChild(node *discovery.TreeNode, name string) *discovery.TreeNode {
	if node == nil {
		return nil
	}
	for i := range node.Children {
		if node.Children[i].Name == name {
			return &node.Children[i]
		}
	}
	return nil
}

func hasTreeTarget(node *discovery.TreeNode, name string) bool {
	if node == nil {
		return false
	}
	for _, target := range node.Targets {
		if target.Name == name {
			return true
		}
	}
	return false
}

func hasTreeTargetInstance(node *discovery.TreeNode, instance string) bool {
	if node == nil {
		return false
	}
	for _, target := range node.Targets {
		if target.Metadata["instance"] == instance {
			return true
		}
	}
	return false
}

func graphWallAssignmentByTile(assignments []graphWallAssignment, tileID string) *graphWallAssignment {
	for i := range assignments {
		if assignments[i].TileID == tileID {
			return &assignments[i]
		}
	}
	return nil
}

func optionStringListContains(options map[string]any, key, want string) bool {
	values, ok := options[key].([]string)
	if !ok {
		return false
	}
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func readoutParameterSensorExists(params []mecom.ReadoutParameter, sensor string) bool {
	for _, param := range params {
		if param.Sensor == sensor {
			return true
		}
	}
	return false
}

func ringBody(t *testing.T, server *Server, limit int) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/log/ring?limit="+strconv.Itoa(limit), nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ring status = %d body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func compactJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(raw))
	}
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("compact JSON: %v", err)
	}
	return string(out)
}

func ringTelemetryByTarget(t *testing.T, server *Server, limit int) map[string]tmtc.Telemetry {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/log/ring?limit="+strconv.Itoa(limit), nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ring status = %d body=%s", rec.Code, rec.Body.String())
	}
	var entries []struct {
		TM tmtc.Telemetry `json:"tm"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode ring: %v", err)
	}
	byTarget := map[string]tmtc.Telemetry{}
	for _, entry := range entries {
		byTarget[entry.TM.TargetID] = entry.TM
	}
	return byTarget
}

func numericTelemetryValue(t *testing.T, value any) float64 {
	t.Helper()
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		got, err := v.Float64()
		if err != nil {
			t.Fatalf("telemetry value json number %q: %v", v, err)
		}
		return got
	default:
		t.Fatalf("telemetry value has type %T: %#v", value, value)
	}
	return 0
}

type fakeUtilityReadClient struct {
	configured      [][]mecom.RingCaptureParameter
	bulkParams      [][]mecom.Parameter
	nanParameterIDs map[int]bool
}

func (f *fakeUtilityReadClient) ReadBulk(_ context.Context, params []mecom.Parameter) ([]float64, error) {
	f.bulkParams = append(f.bulkParams, append([]mecom.Parameter(nil), params...))
	values := make([]float64, len(params))
	for i, param := range params {
		if f.nanParameterIDs[param.ID] {
			values[i] = math.NaN()
			continue
		}
		values[i] = float64(param.ID*10 + param.Instance)
	}
	return values, nil
}

func (f *fakeUtilityReadClient) ConfigureRingCapture(_ context.Context, _ uint16, params []mecom.RingCaptureParameter) error {
	f.configured = append(f.configured, append([]mecom.RingCaptureParameter(nil), params...))
	return nil
}

func (f *fakeUtilityReadClient) TriggerRingSync(context.Context) error {
	return nil
}

func (f *fakeUtilityReadClient) ReadRingPointer(context.Context) (uint32, error) {
	return 0, nil
}

func (f *fakeUtilityReadClient) ReadRingBuffer(context.Context, uint32, uint16) (mecom.RingReadResponse, error) {
	return mecom.RingReadResponse{Status: mecom.RingStatusAllDataRead}, nil
}

func TestServerAccessorsReturnModuleContracts(t *testing.T) {
	server := newTestServer(t)
	if server.Config().HTTPListen == "" {
		t.Fatal("missing config")
	}
	if len(server.HubConfig().Devices) != 1 {
		t.Fatalf("hub devices = %#v", server.HubConfig().Devices)
	}
	if server.Recorder() == nil {
		t.Fatal("missing recorder")
	}
	if len(server.Targets()) == 0 {
		t.Fatal("missing targets")
	}
	if len(server.GraphWall()) == 0 {
		t.Fatal("missing graph wall")
	}
}

func TestWritePrivilegeBoundary(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       bool
	}{
		{name: "loopback", remoteAddr: "127.0.0.1:1234", want: true},
		{name: "lan", remoteAddr: "192.168.6.10:1234", want: true},
		{name: "tailnet", remoteAddr: "100.64.0.1:1234", want: true},
		{name: "public", remoteAddr: "8.8.8.8:1234", want: false},
		{name: "cloudflare tunnel loopback", remoteAddr: "127.0.0.1:1234", headers: map[string]string{"CF-Connecting-IP": "203.0.113.20"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/target/write", nil)
			req.RemoteAddr = tc.remoteAddr
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}
			if got := writePrivilegedRequest(req); got != tc.want {
				t.Fatalf("writePrivilegedRequest() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTargetWriteRequiresExplicitLease(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/operator/meerstettergo/target/write", strings.NewReader(`{"target_id":"device:tec-75:mecom.tec_01.target_object_temp_c","value":23.5}`))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("write without lease status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "lease_id") {
		t.Fatalf("write without lease body = %s", rec.Body.String())
	}
}

func TestLocalPassthroughTargetUsesLoopbackForWildcardListeners(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:15000": "tcp:127.0.0.1:15000",
		":15001":        "tcp:127.0.0.1:15001",
		"[::]:15002":    "tcp:127.0.0.1:15002",
		"127.0.0.1:1":   "tcp:127.0.0.1:1",
	}
	for listen, want := range cases {
		if got := localPassthroughTarget(listen); got != want {
			t.Fatalf("localPassthroughTarget(%q) = %q, want %q", listen, got, want)
		}
	}
}

func TestDeviceTargetCandidatesPrefersPrimaryCANAndDeduplicatesFallbacks(t *testing.T) {
	device := mecomserver.DeviceConfig{
		ID:                "tec-31",
		Target:            "socketcan:can0?addr=0x1f",
		RedundantTargets:  []string{"serial:/dev/ttyUSB0@57600", "socketcan:can0?addr=0x1f", "serial:/dev/ttyUSB0@57600", ""},
		PassthroughListen: "0.0.0.0:15000",
	}
	got := deviceTargetCandidates(device)
	want := []string{"socketcan:can0?addr=0x1f", "tcp:127.0.0.1:15000", "serial:/dev/ttyUSB0@57600"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deviceTargetCandidates() = %#v, want %#v", got, want)
	}
}

func TestUtilityDiscoveryMetadataExposesPrimaryCANAndSerialFallback(t *testing.T) {
	cfg := testConfig()
	cfg.Devices = []mecomserver.DeviceSpec{{
		ID:               "tec-31",
		Target:           "socketcan:can0?addr=0x1f",
		RedundantTargets: []string{"serial:/dev/ttyUSB0@57600", "socketcan:can0?addr=0x1f"},
	}}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	target, ok := server.targetByID("device:tec-31:mecom.tec_01.object_temp_c")
	if !ok {
		t.Fatal("missing TEC catalogue target for tec-31")
	}
	if target.Transport != "socketcan:can0?addr=0x1f" {
		t.Fatalf("target transport = %q", target.Transport)
	}
	if target.Metadata["parameter_id"] == "" || target.Metadata["parameter_id"] != target.Metadata["mecom_parameter_id"] {
		t.Fatalf("TEC catalogue target should expose read-compatible parameter_id metadata: %#v", target.Metadata)
	}
	if target.Metadata["readout"] != mecom.ReadoutVXRoundRobinQueue ||
		target.Metadata["active_readout"] != mecom.ReadoutVXRoundRobinQueue ||
		target.Metadata["background_readout"] != mecom.ReadoutVXRoundRobinQueue ||
		target.Metadata["ring_readout"] != readoutUnsupportedOnActiveTransport ||
		target.Metadata["controller_ring_status"] != readoutUnsupportedOnActiveTransport {
		t.Fatalf("direct CAN target should not advertise active controller ring readout: %#v", target.Metadata)
	}
	if paramID, instance, err := targetParameter(target); err != nil || paramID <= 0 || instance != 1 {
		t.Fatalf("targetParameter(catalogue target) = param=%d instance=%d err=%v metadata=%#v", paramID, instance, err, target.Metadata)
	}
	if target.Metadata["primary_transport"] != "socketcan:can0?addr=0x1f" ||
		target.Metadata["preferred_transport"] != "socketcan:can0?addr=0x1f" ||
		target.Metadata["active_transport"] != "socketcan:can0?addr=0x1f" ||
		target.Metadata["redundant_targets"] != "serial:/dev/ttyUSB0@57600" ||
		target.Metadata["available_transports"] != "socketcan:can0?addr=0x1f,tcp:127.0.0.1:17000,serial:/dev/ttyUSB0@57600" ||
		target.Metadata["serial_device_server"] != "tcp:127.0.0.1:17000" ||
		target.Metadata["passthrough_downstream"] != "serial:/dev/ttyUSB0@57600" ||
		target.Metadata["active_transport_policy"] != "preferred_then_available_candidates" {
		t.Fatalf("target redundancy metadata = %#v", target.Metadata)
	}

	status, ok := server.targetByID("device:tec-31:status")
	if !ok {
		t.Fatal("missing status target for tec-31")
	}
	if status.Metadata["primary_transport"] != "socketcan:can0?addr=0x1f" ||
		status.Metadata["available_transports"] != "socketcan:can0?addr=0x1f,tcp:127.0.0.1:17000,serial:/dev/ttyUSB0@57600" ||
		status.Metadata["serial_device_server"] != "tcp:127.0.0.1:17000" ||
		status.Metadata["redundant_targets"] != "serial:/dev/ttyUSB0@57600" {
		t.Fatalf("status redundancy metadata = %#v", status.Metadata)
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	server, err := NewServer(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func testConfig() Config {
	return Config{
		HTTPListen:            "127.0.0.1:0",
		ListenHost:            "127.0.0.1",
		PassthroughBasePort:   17000,
		ParameterRegistryPath: "",
		Instances:             []int{0, 1, 2},
		Devices: []mecomserver.DeviceSpec{{
			ID:     "tec-01",
			Target: "tcp:192.168.1.50:50000",
		}},
	}
}
