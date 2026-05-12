package utility

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egidinas/loom-gossamer-shared/go/discovery"
	graphwall "github.com/egidinas/loom-gossamer-shared/go/graphwall"
	"github.com/egidinas/loom-gossamer-shared/go/telemetrytiles"
	"github.com/egidinas/loom-gossamer-shared/go/tmtc"
	"github.com/egidinas/meerstetter-go/mecomserver"
)

func TestServerExposesDiscoveryAndBaselineGraphWall(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/discovery/tree", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("discovery status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "HR Temperature") || !strings.Contains(rec.Body.String(), "Output Power") {
		t.Fatalf("baseline discovery missing: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/graph-wall", nil)
	rec = httptest.NewRecorder()
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

func TestServerExposesEventSwimlaneFromRing(t *testing.T) {
	server := newTestServer(t)
	if err := server.recorder.PublishCommandEvent(tmtc.CommandEvent{Status: tmtc.CommandFailed, Error: "timeout"}); err != nil {
		t.Fatal(err)
	}
	if err := server.recorder.PublishTelemetry(tmtc.Telemetry{TargetID: "device:tec-01:temperature:hr", Name: "HR Temperature", Quality: "warning"}); err != nil {
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
	server.publishFloat(device, "temperature:hr", "HR Temperature", "degC", math.NaN(), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/log/ring", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ring status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"quality": "ok"`) || !strings.Contains(body, `"raw_float": "NaN"`) || !strings.Contains(body, `"value_state": "not_applicable"`) {
		t.Fatalf("ring body = %s", body)
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
			TargetID: "device:tec-01:temperature:hr",
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

func TestTilesEndpointServesServerReducedSeries(t *testing.T) {
	server := newTestServer(t)
	targetID := "device:tec-01:temperature:hr"
	for i := 0; i < 100; i++ {
		if err := server.recorder.PublishTelemetry(tmtc.Telemetry{
			ID:       string(rune('a' + i%26)),
			TargetID: targetID,
			Name:     "HR Temperature",
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
		Name:     "HR Temperature",
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
	for _, want := range []string{"/assets/shared/graphwall/renderer.css", "/assets/shared/graphwall/renderer.js", "LoomGraphWall.createGraphWall", "/api/log/ring?after_seq=", "&limit=", "/api/target/read?id=", "/api/target/write", "/api/graph-wall/assign", "graph-focus", "tree-open", "targetInfoByID", `entry.kind === 'telemetry'`, "appendTelemetry"} {
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
		TileID:   "hr",
		Kind:     graphwall.TileTrend,
		TargetID: "device:tec-01:temperature:hr",
		Position: graphwall.Position{X: 1, Y: 2, W: 3, H: 4},
	}}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(server.graphWall) != 1 || server.graphWall[0].Target.ID != "device:tec-01:temperature:hr" {
		t.Fatalf("graph wall = %#v", server.graphWall)
	}
}

func TestGraphWallAssignAddsTargetToExistingOrNewWall(t *testing.T) {
	server := newTestServer(t)
	body := strings.NewReader(`{"target_id":"device:tec-01:temperature:hr","wall_id":"baseline"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/graph-wall/assign", body)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("assign status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok": true`) || !strings.Contains(rec.Body.String(), `"id": "aggregate:temperatures"`) {
		t.Fatalf("assign body = %s", rec.Body.String())
	}

	body = strings.NewReader(`{"target_id":"device:tec-01:power:hr","new_wall_id":"power-detail"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/graph-wall/assign", body)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("new graph status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"wall_id": "power-detail"`) {
		t.Fatalf("new graph body = %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id": "aggregate:powers"`) {
		t.Fatalf("new graph should use power type graph body=%s", rec.Body.String())
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
	body := rec.Body.String()
	for _, want := range []string{
		"Signals",
		"Global",
		"Instance 1 HR",
		"Instance 2 LR",
		"Object Temperature",
		"Device Status",
		"device:tec-01:mecom:1000:i1",
		"device:tec-01:mecom:104:i2",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("discovery missing %q body=%s", want, body)
		}
	}
	if strings.Contains(body, "Laser Diode Current") {
		t.Fatalf("TEC discovery should not include LDD-only values body=%s", body)
	}
	if !strings.Contains(body, `"controller_family": "TEC"`) {
		t.Fatalf("discovery should expose controller family metadata body=%s", body)
	}
	var tree discovery.TreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &tree); err != nil {
		t.Fatal(err)
	}
	signals := findTreeChild(&tree, "Signals")
	if signals == nil {
		t.Fatalf("missing Signals root in %#v", tree.Children)
	}
	temperature := findTreeChild(signals, "Temperature")
	objectTemperature := findTreeChild(temperature, "Object Temperature")
	tec01 := findTreeChild(objectTemperature, "tec-01")
	if tec01 == nil {
		t.Fatalf("discovery should pivot signal -> device -> instance tree=%#v", tree)
	}
	if len(tec01.Targets) != 3 {
		t.Fatalf("tec-01 object temperature targets = %d %#v", len(tec01.Targets), tec01.Targets)
	}
	for _, want := range []string{"Global", "Instance 1 HR", "Instance 2 LR"} {
		if !hasTreeTarget(tec01, want) {
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
