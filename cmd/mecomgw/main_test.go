package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/mecom/writelease"
	"github.com/egidinas/meerstetter-go/tmtc"
)

func TestLoadConfigValidatesRequiredFields(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/gateway.json"
	if err := os.WriteFile(path, []byte(`{"devices":[{"id":"tec-75","endpoint":"tcp:127.0.0.1:50000","address":75,"label":"TEC 75"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}
	if got := len(cfg.Devices); got != 1 {
		t.Fatalf("devices len = %d, want 1", got)
	}
	if cfg.Devices[0].ID != "tec-75" || cfg.Devices[0].Address != 75 {
		t.Fatalf("unexpected device config: %+v", cfg.Devices[0])
	}
	if cfg.ChannelCount != 4 || cfg.Devices[0].ChannelCount != 4 {
		t.Fatalf("channel_count defaults = %d/%d, want 4/4", cfg.ChannelCount, cfg.Devices[0].ChannelCount)
	}
	if len(cfg.Devices[0].Routes) != 0 {
		t.Fatalf("unexpected default routes: %+v", cfg.Devices[0].Routes)
	}

	explicit := dir + "/explicit.json"
	if err := os.WriteFile(explicit, []byte(`{"channel_count":3,"devices":[{"id":"tec-75","endpoint":"tcp:127.0.0.1:50000","address":75,"routes":[{"role":"warm","endpoint":"tcp:127.0.0.1:50002","transport":"tcp","state":"standby"},{"role":"fallback","endpoint":"serial:/dev/ttyUSB0","transport":"serial","state":"offline"}],"channels":[{"instance":1,"role":"temp","label":"Zone A","user_note":"Fixture note","has_cascade":true}]},{"id":"tec-76","endpoint":"tcp:127.0.0.1:50001","address":76,"channel_count":6}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig(explicit)
	if err != nil {
		t.Fatalf("loadConfig explicit returned error: %v", err)
	}
	if cfg.Devices[0].ChannelCount != 3 || cfg.Devices[1].ChannelCount != 6 {
		t.Fatalf("explicit channel_count = %d/%d, want 3/6", cfg.Devices[0].ChannelCount, cfg.Devices[1].ChannelCount)
	}
	if len(cfg.Devices[0].Routes) != 2 || cfg.Devices[0].Routes[0].Role != "warm" || cfg.Devices[0].Routes[1].Role != "fallback" {
		t.Fatalf("explicit routes = %+v", cfg.Devices[0].Routes)
	}
	if got := cfg.Devices[0].Channels[0]; got.Instance != 1 || got.Role != "temp" || got.Label != "Zone A" || got.UserNote != "Fixture note" || !got.HasCascade {
		t.Fatalf("explicit channel metadata = %+v", got)
	}

	bad := dir + "/bad.json"
	if err := os.WriteFile(bad, []byte(`{"devices":[{"id":"missing-address","endpoint":"tcp:127.0.0.1:50000"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(bad); err == nil {
		t.Fatal("loadConfig accepted a device without address")
	}

	badChannel := dir + "/bad-channel.json"
	if err := os.WriteFile(badChannel, []byte(`{"devices":[{"id":"tec-75","endpoint":"tcp:127.0.0.1:50000","address":75,"channels":[{"instance":5,"role":"voltage"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(badChannel); err == nil {
		t.Fatal("loadConfig accepted invalid channel metadata")
	}

	dup := dir + "/dup.json"
	if err := os.WriteFile(dup, []byte(`{"devices":[{"id":"tec-75","endpoint":"tcp:127.0.0.1:50000","address":75},{"id":"tec-75","endpoint":"tcp:127.0.0.1:50001","address":76}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(dup); err == nil {
		t.Fatal("loadConfig accepted duplicate device ids")
	}

	missingDefault := dir + "/missing-default.json"
	if err := os.WriteFile(missingDefault, []byte(`{"default_device_id":"tec-999","devices":[{"id":"tec-75","endpoint":"tcp:127.0.0.1:50000","address":75}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(missingDefault); err == nil {
		t.Fatal("loadConfig accepted missing default_device_id")
	}
}

func TestGatewayRoutesExposeHealthDevicesCatalogueAndLeases(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	getJSON(t, ts.URL+"/api/healthz", http.StatusOK, nil)
	getJSON(t, ts.URL+"/api/health", http.StatusOK, nil)

	var devices struct {
		Devices []deviceView `json:"devices"`
	}
	getJSON(t, ts.URL+"/api/devices", http.StatusOK, &devices)
	if len(devices.Devices) != 1 || devices.Devices[0].ID != "tec-75" || devices.Devices[0].Bound || devices.Devices[0].ChannelCount != 4 {
		t.Fatalf("unexpected devices response: %+v", devices)
	}
	if got := devices.Devices[0].Channels[0]; got.Instance != 1 || got.Role != "temp" || got.RoleSource != "config" || got.Label != "TEC ch1" || got.UserNote != "test channel note" || !got.HasCascade {
		t.Fatalf("unexpected channel metadata: %+v", got)
	}
	if !devices.Devices[0].ActiveRoute.Active || devices.Devices[0].ActiveRoute.Endpoint != "tcp:127.0.0.1:50000" {
		t.Fatalf("unexpected active route: %+v", devices.Devices[0].ActiveRoute)
	}
	if got := len(devices.Devices[0].RouteCandidates); got != 1 {
		t.Fatalf("default route candidates = %d, want 1", got)
	}

	var catalogue struct {
		Parameters []gatewayCatalogueEntry `json:"parameters"`
	}
	getJSON(t, ts.URL+"/api/catalogue", http.StatusOK, &catalogue)
	if len(catalogue.Parameters) == 0 {
		t.Fatal("catalogue response had no parameters")
	}
	first := catalogue.Parameters[0]
	if first.DisplayName == "" || first.RawName == "" || first.ReadoutPriority == "" || first.PreferredReadout == "" || first.Metadata["semantic_role"] == "" {
		t.Fatalf("rich catalogue fields missing: %+v", first)
	}
	if len(first.RouteSupport) == 0 {
		t.Fatalf("public contract fields missing: %+v", first)
	}
	assertCatalogueWritable(t, catalogue.Parameters, 1000, 1, false)
	for _, id := range []int{1020, 1021, 1022, 40000} {
		assertCatalogueWritable(t, catalogue.Parameters, id, 1, false)
	}
	for _, id := range []int{120, 2010, 2020, 2021, 2030, 2031, 2032, 2033, 2040, 3000, 53120, 53121, 53122, 53123} {
		assertCatalogueWritable(t, catalogue.Parameters, id, 1, true)
	}
	for _, id := range []int{120, 1000, 1001, 1020, 1021, 1022, 2010, 2020, 2021, 2030, 2031, 2040, 3000, 40000, 53120, 53123} {
		assertCataloguePresent(t, catalogue.Parameters, id, 4)
	}

	objectTemp := findGatewayCatalogueEntry(t, catalogue.Parameters, 40000, 1)
	if len(objectTemp.RouteSupport) != 2 || objectTemp.RouteSupport[0] != "serial" || objectTemp.RouteSupport[1] != "tcp" {
		t.Fatalf("route support = %#v", objectTemp.RouteSupport)
	}
	if len(objectTemp.TreePaths) == 0 {
		t.Fatalf("tree projections missing: %#v", objectTemp.TreePaths)
	}
	objectCounterparts := findGatewayCatalogueEntry(t, catalogue.Parameters, 1000, 1)
	if objectCounterparts.TelemetryCounterparts["setpoint"][0] != 3000 || objectCounterparts.TelecommandCounterparts["stable"][0] != 1200 {
		t.Fatalf("counterpart projections = %#v / %#v", objectCounterparts.TelemetryCounterparts, objectCounterparts.TelecommandCounterparts)
	}
	if objectTemp.WriteSemantics != "read_only" {
		t.Fatalf("readout write semantics = %q", objectTemp.WriteSemantics)
	}

	targetTemp := findGatewayCatalogueEntry(t, catalogue.Parameters, 3000, 1)
	if targetTemp.WriteSemantics != "write_float32" || targetTemp.Access != "write" {
		t.Fatalf("writable write semantics = %q access=%q", targetTemp.WriteSemantics, targetTemp.Access)
	}

	lease := postLease(t, ts.URL+"/api/devices/tec-75/lease", `{"holder":"operator","ttl":"1m"}`)
	if lease.DeviceID != "tec-75" || lease.Holder != "operator" || lease.Token == "" {
		t.Fatalf("unexpected lease: %+v", lease)
	}

	var leases struct {
		Leases []writelease.Lease `json:"leases"`
	}
	getJSON(t, ts.URL+"/api/leases", http.StatusOK, &leases)
	if len(leases.Leases) != 1 || leases.Leases[0].Token != lease.Token {
		t.Fatalf("unexpected leases response: %+v", leases)
	}

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/devices/tec-75/lease", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Lease-Token", lease.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE lease status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestGatewayCatalogueUsesConfiguredChannelInventory(t *testing.T) {
	s := newServer(Config{
		ChannelCount: 3,
		Devices: []DeviceConfig{
			{ID: "tec-75", Endpoint: "tcp:127.0.0.1:50000", Address: 75, Label: "TEC 75"},
			{ID: "tec-76", Endpoint: "tcp:127.0.0.1:50001", Address: 76, Label: "TEC 76", ChannelCount: 6, Channels: []ChannelConfig{
				{Instance: 6, Role: "supply", Label: "Aux supply", UserNote: "auxiliary supply channel"},
			}},
		},
	}, time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var devices struct {
		Devices []deviceView `json:"devices"`
	}
	getJSON(t, ts.URL+"/api/devices", http.StatusOK, &devices)
	if len(devices.Devices) != 2 {
		t.Fatalf("devices len = %d, want 2", len(devices.Devices))
	}
	byID := map[string]deviceView{}
	for _, d := range devices.Devices {
		byID[d.ID] = d
	}
	if byID["tec-75"].ChannelCount != 3 || byID["tec-76"].ChannelCount != 6 {
		t.Fatalf("device channel counts = %+v, want tec-75=3 tec-76=6", byID)
	}
	if got := byID["tec-76"].Channels[0]; got.Instance != 6 || got.Role != "supply" || got.RoleSource != "config" || got.Label != "Aux supply" {
		t.Fatalf("device channel metadata = %+v", got)
	}

	var catalogue struct {
		Parameters []gatewayCatalogueEntry `json:"parameters"`
	}
	getJSON(t, ts.URL+"/api/catalogue", http.StatusOK, &catalogue)
	assertCataloguePresent(t, catalogue.Parameters, 1000, 6)
	assertCataloguePresent(t, catalogue.Parameters, 120, 6)
	assertCataloguePresent(t, catalogue.Parameters, 2010, 6)
	assertCatalogueMissing(t, catalogue.Parameters, 1000, 7)
	assertCatalogueMissing(t, catalogue.Parameters, 120, 7)
	assertCatalogueMissing(t, catalogue.Parameters, 2010, 7)
}

func TestGatewayOrdersDefaultDeviceFirst(t *testing.T) {
	s := newServer(Config{
		Devices: []DeviceConfig{
			{ID: "tec-75", Endpoint: "tcp:127.0.0.1:50000", Address: 75},
			{ID: "tec-76", Endpoint: "tcp:127.0.0.1:50001", Address: 76},
			{ID: "tec-77", Endpoint: "tcp:127.0.0.1:50002", Address: 77},
		},
	}, time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var devices struct {
		Devices []deviceView `json:"devices"`
	}
	getJSON(t, ts.URL+"/api/devices", http.StatusOK, &devices)
	if len(devices.Devices) != 3 {
		t.Fatalf("devices len = %d, want 3", len(devices.Devices))
	}
	if got := devices.Devices[0].ID; got != "tec-76" {
		t.Fatalf("first device = %q, want tec-76", got)
	}

	s = newServer(Config{
		DefaultDeviceID: "tec-77",
		Devices: []DeviceConfig{
			{ID: "tec-75", Endpoint: "tcp:127.0.0.1:50000", Address: 75},
			{ID: "tec-76", Endpoint: "tcp:127.0.0.1:50001", Address: 76},
			{ID: "tec-77", Endpoint: "tcp:127.0.0.1:50002", Address: 77},
		},
	}, time.Minute, log.New(io.Discard, "", 0))
	ts = httptest.NewServer(s.routes())
	defer ts.Close()
	getJSON(t, ts.URL+"/api/devices", http.StatusOK, &devices)
	if got := devices.Devices[0].ID; got != "tec-77" {
		t.Fatalf("default_device_id first device = %q, want tec-77", got)
	}
}

func TestGatewayServesGraphTileContract(t *testing.T) {
	s := newServer(Config{
		DefaultDeviceID: "tec-76",
		Devices: []DeviceConfig{
			{ID: "tec-75", Endpoint: "tcp:127.0.0.1:50000", Address: 75, Label: "TEC 75"},
			{ID: "tec-76", Endpoint: "tcp:127.0.0.1:50001", Address: 76, Label: "TEC 76"},
		},
	}, time.Minute, log.New(io.Discard, "", 0))
	now := time.Now().UTC()
	s.recordGraphSample("tec-75", 1000, 1, 21.5, "ok", now.Add(-2*time.Second))
	s.recordGraphSample("tec-76", 3000, 1, 24.0, "ok", now.Add(-1*time.Second))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var tile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/fleet-temp/live?series=tec-75:1000:1&series=tec-76:3000:1", http.StatusOK, &tile)

	if tile.SchemaVersion != "signalforge.graph_tile.v1" || tile.Renderer != canonicalTileRenderer || tile.Kind != "timeseries" {
		t.Fatalf("unexpected tile identity: %+v", tile)
	}
	if tile.LatestEndpoint != "/api/graph/tiles/fleet-temp/live" || tile.TileEndpoint != "/api/graph/tiles/fleet-temp/live" {
		t.Fatalf("unexpected endpoints: %+v", tile)
	}
	if tile.Diagnostics.OutlierPolicy != "drop_detached_degC_below_-50_and_initial_out_of_family" {
		t.Fatalf("unexpected outlier policy: %+v", tile.Diagnostics)
	}
	if tile.Diagnostics.SeriesCount != 2 || len(tile.Series) != 2 {
		t.Fatalf("unexpected series count: %+v", tile.Diagnostics)
	}
	if got := tile.Series[0].Source.DeviceID; got != "tec-76" {
		t.Fatalf("default device ordering = %q, want tec-76 first", got)
	}
	if tile.Series[0].Unit != "degC" || tile.Series[1].Unit != "degC" {
		t.Fatalf("unexpected units: %+v", []string{tile.Series[0].Unit, tile.Series[1].Unit})
	}
	if tile.Series[0].Label == "" || tile.Series[1].Label == "" || !strings.Contains(tile.Series[0].Label, "SN76") || !strings.Contains(tile.Series[1].Label, "SN75") {
		t.Fatalf("unexpected series labels: %+v", []string{tile.Series[0].Label, tile.Series[1].Label})
	}
	if len(tile.Axes) == 0 || tile.Axes[0].Unit != "degC" {
		t.Fatalf("unexpected axes: %+v", tile.Axes)
	}
	if len(tile.Series[0].Points) < 2 || len(tile.Series[1].Points) < 2 {
		t.Fatalf("expected history-backed points, got: %+v", tile.Series)
	}

	var defaultTile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/default-temp/day", http.StatusOK, &defaultTile)
	if defaultTile.Level != "day" || defaultTile.TimeWindowMs != 24*60*60_000 || len(defaultTile.Series) != 2 {
		t.Fatalf("unexpected default day tile: level=%s window=%d series=%d", defaultTile.Level, defaultTile.TimeWindowMs, len(defaultTile.Series))
	}
	if !tileFilesContain(defaultTile.TileFiles, "three_hour", 3*60*60_000) {
		t.Fatalf("three_hour tile file missing from manifest: %+v", defaultTile.TileFiles)
	}

	var threeHourTile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/default-temp/3h", http.StatusOK, &threeHourTile)
	if threeHourTile.Level != "three_hour" || threeHourTile.TimeWindowMs != 3*60*60_000 || threeHourTile.Diagnostics.TileSource != "bounded-gateway-history-cache" {
		t.Fatalf("unexpected 3h tile: level=%s window=%d source=%s", threeHourTile.Level, threeHourTile.TimeWindowMs, threeHourTile.Diagnostics.TileSource)
	}
}

func tileFilesContain(files []tileFile, level string, windowMs int) bool {
	for _, file := range files {
		if file.Level == level && file.TimeWindowMs == windowMs {
			return true
		}
	}
	return false
}

func TestGatewayRecordsCommandActivity(t *testing.T) {
	s := newServer(Config{Devices: []DeviceConfig{{
		ID:       "tec-75",
		Endpoint: "tcp:127.0.0.1:50000",
		Address:  75,
	}}}, time.Minute, log.New(io.Discard, "", 0))
	fw := &recordingWriteClient{}
	s.devices["tec-75"].client = fw
	s.devices["tec-75"].commander = mecom.NewCommander(fw, time.Second)
	s.devices["tec-75"].commander.TargetID = "tec-75"
	s.devices["tec-75"].commander.Authorizer = mecom.AuthorizerFunc(s.authorize)
	s.leases.Acquire("tec-75", "operator", time.Minute)
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	body := `{"name":"write_int32","arguments":{"param":2010,"instance":1,"value":1},"metadata":{"lease_token":"ignored"}}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/devices/tec-75/write", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Lease-Token", s.leases.List()[0].Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("write status = %d, want 200", resp.StatusCode)
	}

	var commands struct {
		Commands []gatewayCommandActivity `json:"commands"`
	}
	getJSON(t, ts.URL+"/api/commands", http.StatusOK, &commands)
	if len(commands.Commands) != 1 {
		t.Fatalf("commands len = %d, want 1", len(commands.Commands))
	}
	got := commands.Commands[0]
	if got.DeviceID != "tec-75" || got.ParamID != 2010 || got.Instance != 1 || got.SignalName != "output_stage_enable" || got.Status != "completed" || got.HTTPStatus != http.StatusOK || got.LeaseHolder != "operator" || got.IdempotencyKey == "" {
		t.Fatalf("unexpected command activity: %+v", got)
	}
	if got.RequestedValue == nil {
		t.Fatalf("command requested value missing: %+v", got)
	}
}

func TestGatewayRecordsFailedWriteAttempts(t *testing.T) {
	endpoint := "tcp:" + closedTCPAddress(t)
	s := newServer(Config{Devices: []DeviceConfig{{
		ID:       "tec-75",
		Endpoint: endpoint,
		Address:  75,
	}}}, time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/devices/tec-75/write", strings.NewReader(`{"name":"write_int32","arguments":{"param":2010,"instance":1,"value":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("failed write status = %d, want 503", resp.StatusCode)
	}

	var commands struct {
		Commands []gatewayCommandActivity `json:"commands"`
	}
	getJSON(t, ts.URL+"/api/commands", http.StatusOK, &commands)
	if len(commands.Commands) != 1 {
		t.Fatalf("commands len = %d, want 1", len(commands.Commands))
	}
	got := commands.Commands[0]
	if got.Status != "failed" || got.HTTPStatus != http.StatusServiceUnavailable || got.Error == "" || got.ErrorCategory == "" {
		t.Fatalf("unexpected failed command activity: %+v", got)
	}
}

func TestGatewayBindFailuresUseTransportStatus(t *testing.T) {
	endpoint := "tcp:" + closedTCPAddress(t)
	s := newServer(Config{Devices: []DeviceConfig{{
		ID:       "tec-75",
		Endpoint: endpoint,
		Address:  75,
		Label:    "TEC 75",
	}}}, time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "read", method: http.MethodGet, path: "/api/devices/tec-75/read?params=1000:1"},
		{name: "write", method: http.MethodPost, path: "/api/devices/tec-75/write", body: `{}`},
		{name: "poll", method: http.MethodGet, path: "/api/devices/tec-75/poll?params=1000:1&interval=1h"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, ts.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
			}
		})
	}

	resp, err := http.Get(ts.URL + "/api/devices/not-configured/read?params=1000:1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown device status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func closedTCPAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func assertCatalogueWritable(t *testing.T, params []gatewayCatalogueEntry, id, instance int, want bool) {
	t.Helper()
	for _, p := range params {
		if p.ID == id && p.Instance == instance {
			if p.Writable != want {
				t.Fatalf("catalogue %d:%d writable = %v, want %v", id, instance, p.Writable, want)
			}
			return
		}
	}
	t.Fatalf("catalogue missing parameter %d:%d", id, instance)
}

func assertCataloguePresent(t *testing.T, params []gatewayCatalogueEntry, id, instance int) {
	t.Helper()
	for _, p := range params {
		if p.ID == id && p.Instance == instance {
			return
		}
	}
	t.Fatalf("catalogue missing parameter %d:%d", id, instance)
}

func assertCatalogueMissing(t *testing.T, params []gatewayCatalogueEntry, id, instance int) {
	t.Helper()
	for _, p := range params {
		if p.ID == id && p.Instance == instance {
			t.Fatalf("catalogue unexpectedly included parameter %d:%d", id, instance)
		}
	}
}

func findGatewayCatalogueEntry(t *testing.T, params []gatewayCatalogueEntry, id, instance int) gatewayCatalogueEntry {
	t.Helper()
	for _, p := range params {
		if p.ID == id && p.Instance == instance {
			return p
		}
	}
	t.Fatalf("catalogue missing parameter %d:%d", id, instance)
	return gatewayCatalogueEntry{}
}

func TestParseParamsQuery(t *testing.T) {
	params, err := parseParamsQuery("1000:1, 1021:2")
	if err != nil {
		t.Fatalf("parseParamsQuery returned error: %v", err)
	}
	if len(params) != 2 || params[0].ID != 1000 || params[0].Instance != 1 || params[1].ID != 1021 || params[1].Instance != 2 {
		t.Fatalf("unexpected params: %+v", params)
	}
	intParams, err := parseParamsQuery("1200:4")
	if err != nil {
		t.Fatalf("parseParamsQuery int param returned error: %v", err)
	}
	if intParams[0].Type != mecom.DataTypeInt32 {
		t.Fatalf("param 1200 type = %s, want %s", intParams[0].Type, mecom.DataTypeInt32)
	}
	readParams, err := parseParamsQuery("1022:2,40000:1")
	if err != nil {
		t.Fatalf("parseParamsQuery read param returned error: %v", err)
	}
	if readParams[0].Type != mecom.DataTypeFloat32 || readParams[1].Type != mecom.DataTypeFloat32 {
		t.Fatalf("read param types = %s/%s, want float32/float32", readParams[0].Type, readParams[1].Type)
	}
	writeParams, err := parseParamsQuery("2010:4,2020:4,2021:4,2030:4,2031:4,2040:4,53120:1,53123:1")
	if err != nil {
		t.Fatalf("parseParamsQuery write param returned error: %v", err)
	}
	if writeParams[0].Type != mecom.DataTypeInt32 ||
		writeParams[1].Type != mecom.DataTypeFloat32 ||
		writeParams[2].Type != mecom.DataTypeFloat32 ||
		writeParams[3].Type != mecom.DataTypeFloat32 ||
		writeParams[4].Type != mecom.DataTypeFloat32 ||
		writeParams[5].Type != mecom.DataTypeInt32 ||
		writeParams[6].Type != mecom.DataTypeInt32 ||
		writeParams[7].Type != mecom.DataTypeFloat32 {
		t.Fatalf("write param types = %s/%s/%s/%s/%s/%s/%s/%s, want int32/float32/float32/float32/float32/int32/int32/float32",
			writeParams[0].Type, writeParams[1].Type, writeParams[2].Type, writeParams[3].Type, writeParams[4].Type, writeParams[5].Type, writeParams[6].Type, writeParams[7].Type)
	}

	for _, raw := range []string{"", "1000", "x:1", "1000:x", "1000:0", "1000:-1", "1000:256", "999999:1"} {
		if _, err := parseParamsQuery(raw); err == nil {
			t.Fatalf("parseParamsQuery(%q) returned nil error", raw)
		}
	}
}

func TestLeaseReleaseRequiresPathDeviceMatch(t *testing.T) {
	cfg := testConfig()
	cfg.Devices = append(cfg.Devices, DeviceConfig{
		ID:       "tec-76",
		Endpoint: "tcp:127.0.0.1:50001",
		Address:  76,
		Label:    "TEC 76",
	})
	s := newServer(cfg, time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	lease := postLease(t, ts.URL+"/api/devices/tec-75/lease", `{"holder":"operator","ttl":"1m"}`)
	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/devices/tec-76/lease", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Lease-Token", lease.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong-device DELETE lease status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	req, err = http.NewRequest(http.MethodDelete, ts.URL+"/api/devices/tec-75/lease", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Lease-Token", lease.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("matching DELETE lease status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

type recordingWriteClient struct {
	paramID  int
	instance int
	value    int32
}

func (c *recordingWriteClient) WriteFloat32(ctx context.Context, paramID, instance int, value float32) error {
	c.paramID = paramID
	c.instance = instance
	c.value = int32(value)
	return nil
}

func (c *recordingWriteClient) WriteInt32(ctx context.Context, paramID, instance int, value int32) error {
	c.paramID = paramID
	c.instance = instance
	c.value = value
	return nil
}

func (c *recordingWriteClient) ReadBulk(ctx context.Context, params []mecom.Parameter) ([]float64, error) {
	return nil, mecom.ErrTransportNotSupported
}

func (c *recordingWriteClient) ConfigureRingCapture(ctx context.Context, captureID uint16, params []mecom.RingCaptureParameter) error {
	return mecom.ErrTransportNotSupported
}

func (c *recordingWriteClient) TriggerRingSync(ctx context.Context) error {
	return mecom.ErrTransportNotSupported
}

func (c *recordingWriteClient) ReadRingPointer(ctx context.Context) (uint32, error) {
	return 0, mecom.ErrTransportNotSupported
}

func (c *recordingWriteClient) ReadRingChunk(ctx context.Context, offset uint32, maxBytes uint16) (mecom.RingReadResponse, error) {
	return mecom.RingReadResponse{}, mecom.ErrTransportNotSupported
}

func (c *recordingWriteClient) Close() error { return nil }

func TestAuthorizeRequiresMatchingLeaseToken(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	lease, err := s.leases.Acquire("tec-75", "operator", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.authorize("tec-75", tmtc.Telecommand{Metadata: map[string]string{"lease_token": lease.Token}}); err != nil {
		t.Fatalf("authorize valid token: %v", err)
	}
	if err := s.authorize("tec-75", tmtc.Telecommand{}); err == nil {
		t.Fatal("authorize accepted missing token")
	}
	if err := s.authorize("tec-76", tmtc.Telecommand{Metadata: map[string]string{"lease_token": lease.Token}}); !errors.Is(err, writelease.ErrUnknownDevice) {
		t.Fatalf("authorize wrong device err = %v, want ErrUnknownDevice", err)
	}
}

func TestGatewayServesStaticUI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/index.html", []byte("<!doctype html><title>MeCom Gateway</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	s.uiDir = dir
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ui/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui/ status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("MeCom Gateway")) {
		t.Fatalf("GET /ui/ body did not contain demo title: %q", string(body))
	}
}

func TestGatewayAccessTokenGateProtectsUIAndAPI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/index.html", []byte("<!doctype html><title>MeCom Gateway</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	s.uiDir = dir
	s.accessToken = "share-token"
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	getJSON(t, ts.URL+"/api/devices", http.StatusUnauthorized, nil)

	resp, err := http.Get(ts.URL + "/ui/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /ui/ without token status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	getJSON(t, ts.URL+"/api/devices?access_token=share-token", http.StatusOK, nil)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Gateway-Token", "share-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/devices with token header status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, err = http.NewRequest(http.MethodGet, ts.URL+"/ui/?t=share-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET /ui/?t=token status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if loc := resp.Header.Get("Location"); loc != "/ui/" {
		t.Fatalf("redirect Location = %q, want /ui/", loc)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != gatewayAccessCookie || cookies[0].Value != "share-token" || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("unexpected access cookie: %+v", cookies)
	}

	req, err = http.NewRequest(http.MethodGet, ts.URL+"/ui/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookies[0])
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui/ with cookie status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

type fakeGatewayDevice struct {
	values []float64
	byKey  map[string]float64
}

func (f fakeGatewayDevice) ReadBulk(_ context.Context, params []mecom.Parameter) ([]float64, error) {
	if f.byKey != nil {
		values := make([]float64, 0, len(params))
		for _, p := range params {
			v, ok := f.byKey[gatewayCatalogueKey(p.ID, p.Instance)]
			if !ok {
				return nil, fmt.Errorf("%w: parameter %d instance %d", mecom.ErrUnknownParameter, p.ID, p.Instance)
			}
			values = append(values, v)
		}
		return values, nil
	}
	return f.values, nil
}

func (f fakeGatewayDevice) ConfigureRingCapture(context.Context, uint16, []mecom.RingCaptureParameter) error {
	return mecom.ErrTransportNotSupported
}

func (f fakeGatewayDevice) TriggerRingSync(context.Context) error {
	return mecom.ErrTransportNotSupported
}

func (f fakeGatewayDevice) ReadRingPointer(context.Context) (uint32, error) {
	return 0, mecom.ErrTransportNotSupported
}

func (f fakeGatewayDevice) ReadRingChunk(context.Context, uint32, uint16) (mecom.RingReadResponse, error) {
	return mecom.RingReadResponse{}, mecom.ErrTransportNotSupported
}

func (f fakeGatewayDevice) Close() error {
	return nil
}

func TestGatewayReadReportsNaNAsNullQuality(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	s.devices["tec-75"].client = fakeGatewayDevice{values: []float64{25.5, math.NaN()}}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var got struct {
		Values []gatewayReadValue `json:"values"`
	}
	getJSON(t, ts.URL+"/api/devices/tec-75/read?params=1000:1,52200:1", http.StatusOK, &got)
	if len(got.Values) != 2 {
		t.Fatalf("values len = %d, want 2", len(got.Values))
	}
	if got.Values[0].Value == nil || *got.Values[0].Value != 25.5 || got.Values[0].Quality != "ok" {
		t.Fatalf("unexpected finite value entry: %+v", got.Values[0])
	}
	if got.Values[1].Value != nil || got.Values[1].Quality != "nan" {
		t.Fatalf("unexpected NaN value entry: %+v", got.Values[1])
	}
	s.devices["tec-75"].client = fakeGatewayDevice{byKey: map[string]float64{}}
	var tile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/readback/live?series=tec-75:1000:1", http.StatusOK, &tile)
	if len(tile.Series) != 1 || len(tile.Series[0].History.V) == 0 || tile.Series[0].History.V[len(tile.Series[0].History.V)-1] != 25.5 {
		t.Fatalf("read path did not feed graph history: %+v", tile.Series)
	}
}

func TestGatewayReadKeepsAvailableValuesWhenOptionalParamFails(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	s.devices["tec-75"].client = fakeGatewayDevice{byKey: map[string]float64{
		"1000:1": 25.5,
	}}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var got struct {
		Values []gatewayReadValue `json:"values"`
	}
	getJSON(t, ts.URL+"/api/devices/tec-75/read?params=1000:1,52200:1", http.StatusOK, &got)
	if len(got.Values) != 2 {
		t.Fatalf("values len = %d, want 2", len(got.Values))
	}
	if got.Values[0].Value == nil || *got.Values[0].Value != 25.5 || got.Values[0].Quality != "ok" {
		t.Fatalf("unexpected available value entry: %+v", got.Values[0])
	}
	if got.Values[1].Value != nil || got.Values[1].Quality != "missing" {
		t.Fatalf("unexpected missing optional value entry: %+v", got.Values[1])
	}
}

func TestWriteJSONFallsBackBeforeWritingHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, http.StatusOK, map[string]any{"bad": math.NaN()})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("fallback body is not JSON: %v", err)
	}
	if !strings.Contains(body.Error, "JSON encode failed") {
		t.Fatalf("fallback error = %q", body.Error)
	}
}

func TestGatewayCORSAllowsConfiguredOrigin(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	s.allowedOrigins = []string{"https://claude.ai"}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://claude.ai")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://claude.ai" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want claude origin", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-Lease-Token") {
		t.Fatalf("Access-Control-Allow-Headers = %q, want X-Lease-Token", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-Gateway-Token") {
		t.Fatalf("Access-Control-Allow-Headers = %q, want X-Gateway-Token", got)
	}

	req, err = http.NewRequest(http.MethodGet, ts.URL+"/api/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://not-allowed.example")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unconfigured origin received CORS header %q", got)
	}
}

func TestHTTPStatusForError(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{mecom.ErrUnreachable, http.StatusServiceUnavailable},
		{mecom.ErrTimeout, http.StatusGatewayTimeout},
		{mecom.ErrBadAddress, http.StatusBadRequest},
		{mecom.ErrUnknownParameter, http.StatusNotFound},
		{mecom.ErrParameterReadOnly, http.StatusForbidden},
		{mecom.ErrWriteRejected, http.StatusConflict},
		{mecom.ErrTransportNotSupported, http.StatusNotImplemented},
		{errors.New("other"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		if got := httpStatusForError(tc.err); got != tc.want {
			t.Fatalf("httpStatusForError(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

func testConfig() Config {
	return Config{Devices: []DeviceConfig{{
		ID:       "tec-75",
		Endpoint: "tcp:127.0.0.1:50000",
		Address:  75,
		Label:    "TEC 75",
		Channels: []ChannelConfig{{
			Instance:   1,
			Role:       "temp",
			Label:      "TEC ch1",
			UserNote:   "test channel note",
			HasCascade: true,
		}},
	}}}
}

func getJSON(t *testing.T, url string, wantStatus int, out any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s status = %d, want %d; body=%s", url, resp.StatusCode, wantStatus, string(body))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
}

func postLease(t *testing.T, url string, body string) writelease.Lease {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST lease status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, string(raw))
	}
	var lease writelease.Lease
	if err := json.NewDecoder(resp.Body).Decode(&lease); err != nil {
		t.Fatal(err)
	}
	return lease
}
