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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/mecom/writelease"
	"github.com/egidinas/signalforge/arrowtelemetry"
	tmtc "github.com/egidinas/signalforge/contracts"
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
	if err := os.WriteFile(explicit, []byte(`{"channel_count":3,"devices":[{"id":"tec-75","endpoint":"tcp:127.0.0.1:50000","address":75,"third_party_power_control_enabled":true,"routes":[{"role":"warm","endpoint":"tcp:127.0.0.1:50002","transport":"tcp","state":"standby"},{"role":"fallback","endpoint":"serial:/dev/ttyUSB0","transport":"serial","state":"offline"}],"channels":[{"instance":1,"role":"temp","label":"Zone A","user_note":"Fixture note","has_cascade":true}]},{"id":"tec-76","endpoint":"tcp:127.0.0.1:50001","address":76,"channel_count":6}]}`), 0o600); err != nil {
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
	if !cfg.Devices[0].PowerControlEnabled {
		t.Fatal("third-party power-control opt-in was not loaded")
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

func TestGatewayDevicesExposePowerControlOptIn(t *testing.T) {
	s := newServer(Config{Devices: []DeviceConfig{{
		ID:                  "tec-75",
		Endpoint:            "tcp:127.0.0.1:50000",
		Address:             75,
		ChannelCount:        1,
		PowerControlEnabled: true,
		Channels: []ChannelConfig{{
			Instance: 1,
			Role:     "supply",
			Label:    "Power channel",
		}},
	}}}, time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var devices struct {
		Devices []deviceView `json:"devices"`
	}
	getJSON(t, ts.URL+"/api/devices", http.StatusOK, &devices)
	if len(devices.Devices) != 1 {
		t.Fatalf("devices response = %+v, want one device", devices)
	}
	if !devices.Devices[0].PowerControlEnabled {
		t.Fatalf("device opt-in flag missing: %+v", devices.Devices[0])
	}
	if len(devices.Devices[0].Channels) != 1 || !devices.Devices[0].Channels[0].PowerControlEnabled {
		t.Fatalf("channel opt-in flag missing: %+v", devices.Devices[0].Channels)
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
	if len(byID["tec-75"].Channels) != 3 || len(byID["tec-76"].Channels) != 6 {
		t.Fatalf("device channel inventory lengths = tec-75:%d tec-76:%d, want 3 and 6", len(byID["tec-75"].Channels), len(byID["tec-76"].Channels))
	}
	configured := channelView{}
	for _, ch := range byID["tec-76"].Channels {
		if ch.Instance == 6 {
			configured = ch
			break
		}
	}
	if configured.Instance != 6 || configured.Role != "supply" || configured.RoleSource != "config" || configured.Label != "Aux supply" {
		t.Fatalf("configured device channel metadata = %+v", configured)
	}
	if got := byID["tec-76"].Channels[0]; got.Instance != 1 || got.Role != "temp" || got.RoleSource != "gateway-default" {
		t.Fatalf("default device channel metadata = %+v", got)
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

func TestGatewayProxyBasePortUsesStableConfigOrder(t *testing.T) {
	s := newServer(Config{
		Devices: []DeviceConfig{
			{ID: "tec-75", Endpoint: "tcp:127.0.0.1:50000", Address: 75},
			{ID: "tec-76", Endpoint: "tcp:127.0.0.1:50001", Address: 76},
			{ID: "tec-77", Endpoint: "tcp:127.0.0.1:50002", Address: 77},
		},
	}, time.Minute, log.New(io.Discard, "", 0))

	if got := s.deviceIndex("tec-75"); got != 0 {
		t.Fatalf("deviceIndex(tec-75) = %d, want 0", got)
	}
	if got := s.deviceIndex("tec-76"); got != 1 {
		t.Fatalf("deviceIndex(tec-76) = %d, want 1", got)
	}
	if got := s.deviceIndex("tec-77"); got != 2 {
		t.Fatalf("deviceIndex(tec-77) = %d, want 2", got)
	}
}

func TestGatewayResetDeviceBindingStopsProxyAndClearsBinding(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	b := s.devices["tec-75"]
	client := &fakeReadClient{}
	b.client = client
	b.commander = &mecom.Commander{}
	b.proxy = mecom.NewProxyServer("127.0.0.1:0", &mecom.Client{})
	if err := b.proxy.Start(); err != nil {
		t.Fatalf("proxy start: %v", err)
	}

	s.resetDeviceBinding("tec-75", client, errors.New("boom"))

	if !client.closed {
		t.Fatal("expected client to be closed")
	}
	if b.client != nil {
		t.Fatalf("client not cleared: %+v", b.client)
	}
	if b.commander != nil {
		t.Fatalf("commander not cleared: %+v", b.commander)
	}
	if b.proxy != nil {
		t.Fatalf("proxy not cleared: %+v", b.proxy)
	}
	if b.lastErr == nil || b.lastErr.Error() != "boom" {
		t.Fatalf("lastErr = %v, want boom", b.lastErr)
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
	if tile.Series[0].Quality != "ok" || tile.Series[1].Quality != "ok" {
		t.Fatalf("unexpected series quality: %+v", []string{tile.Series[0].Quality, tile.Series[1].Quality})
	}
	if !tile.Series[0].DefaultVisible || !tile.Series[1].DefaultVisible || tile.Series[0].VisibilityReason != "" || tile.Series[1].VisibilityReason != "" {
		t.Fatalf("unexpected default visibility for ok series: %+v", tile.Series)
	}
	if tile.Series[0].Diagnostics.Status != "ok" || tile.Series[1].Diagnostics.Status != "ok" {
		t.Fatalf("unexpected series diagnostics: %+v", []graphTileItemDiag{tile.Series[0].Diagnostics, tile.Series[1].Diagnostics})
	}
	if len(tile.Axes) == 0 || tile.Axes[0].Unit != "degC" {
		t.Fatalf("unexpected axes: %+v", tile.Axes)
	}
	if len(tile.Series[0].Points) < 2 || len(tile.Series[1].Points) < 2 {
		t.Fatalf("expected history-backed points, got: %+v", tile.Series)
	}

	var defaultTile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/default-temp/day", http.StatusOK, &defaultTile)
	if defaultTile.Level != "day" || defaultTile.TimeWindowMs != 24*60*60_000 || len(defaultTile.Series) != 4 {
		t.Fatalf("unexpected default day tile: level=%s window=%d series=%d", defaultTile.Level, defaultTile.TimeWindowMs, len(defaultTile.Series))
	}
	for _, series := range defaultTile.Series {
		wantKey := fmt.Sprintf("%s:%d:%d", series.Source.DeviceID, series.Source.ParamID, series.Source.Instance)
		if series.ID != wantKey || series.SeriesID != wantKey {
			t.Fatalf("default temperature series identity = id %q series_id %q, want %q", series.ID, series.SeriesID, wantKey)
		}
		if series.Source.ParamID != 1000 {
			t.Fatalf("default temperature tile included param %d, want 1000", series.Source.ParamID)
		}
		if series.Source.Instance != 1 && series.Source.Instance != 3 {
			t.Fatalf("default temperature tile included instance %d, want temperature-control channels 1/3", series.Source.Instance)
		}
	}
	if !tileFilesContain(defaultTile.TileFiles, "three_hour", 3*60*60_000) {
		t.Fatalf("three_hour tile file missing from manifest: %+v", defaultTile.TileFiles)
	}

	var supplyTile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/fleet-supply/live", http.StatusOK, &supplyTile)
	if len(supplyTile.Series) != 4 {
		t.Fatalf("supply default series = %d, want supply channels 2/4 on both devices", len(supplyTile.Series))
	}
	if len(supplyTile.Axes) == 0 || supplyTile.Axes[0].Unit != "W" || supplyTile.Axes[0].Label != "Power [W]" {
		t.Fatalf("unexpected supply axes: %+v", supplyTile.Axes)
	}
	for _, series := range supplyTile.Series {
		wantKey := fmt.Sprintf("%s:%d:%d", series.Source.DeviceID, series.Source.ParamID, series.Source.Instance)
		if series.ID != wantKey || series.SeriesID != wantKey {
			t.Fatalf("supply series identity = id %q series_id %q, want %q", series.ID, series.SeriesID, wantKey)
		}
		if series.Source.ParamID != 1022 || series.Unit != "W" || !strings.Contains(series.Label, "OP") {
			t.Fatalf("supply tile series = %+v, want output power", series)
		}
		if series.Source.Instance != 2 && series.Source.Instance != 4 {
			t.Fatalf("supply tile included instance %d, want power-supply channels 2/4", series.Source.Instance)
		}
	}

	var threeHourTile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/default-temp/3h", http.StatusOK, &threeHourTile)
	if threeHourTile.Level != "three_hour" || threeHourTile.TimeWindowMs != 3*60*60_000 || threeHourTile.Diagnostics.TileSource != "bounded-gateway-history-cache" {
		t.Fatalf("unexpected 3h tile: level=%s window=%d source=%s", threeHourTile.Level, threeHourTile.TimeWindowMs, threeHourTile.Diagnostics.TileSource)
	}
}

func TestGatewayGraphHistoryKeepsHotRawSamplesAndReducesLongRangeHistory(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	now := time.Date(2026, 5, 19, 12, 0, 0, 250_000_000, time.UTC)
	s.recordGraphSample("tec-75", 1000, 1, 20.0, "ok", now)
	s.recordGraphSample("tec-75", 1000, 1, 1.0, "ok", now.Add(400*time.Millisecond))

	hot := s.lookupGraphHistory("tec-75", 1000, 1, 15*60*1000, now.Add(time.Second))
	if len(hot.TS) != 2 || len(hot.V) != 2 {
		t.Fatalf("15-minute history = %+v, want 2 raw samples", hot)
	}
	if hot.V[0] != 20.0 || hot.V[1] != 1.0 {
		t.Fatalf("15-minute history values = %+v, want raw samples", hot.V)
	}

	long := s.lookupGraphHistory("tec-75", 1000, 1, 3*24*60*60*1000, now.Add(time.Second))
	if len(long.TS) != 1 || len(long.V) != 1 {
		t.Fatalf("3-day history = %+v, want 1 mean bucket", long)
	}
	if long.V[0] != 10.5 {
		t.Fatalf("3-day history mean = %v, want 10.5", long.V[0])
	}
}

func TestGraphDerivationsIgnoreSamplesAlreadyRecordedInPyramid(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	base := time.Date(2026, 5, 19, 12, 0, 0, 100_000_000, time.UTC)
	s.recordGraphSample("tec-75", 1000, 1, 20.0, "ok", base)
	s.recordGraphSample("tec-75", 1000, 1, 1.0, "ok", base.Add(1600*time.Millisecond))

	s.processDerivationsAt(base.Add(3500 * time.Millisecond))

	assertLongGraphMean(t, s, 10.5, base.Add(4*time.Second))
}

func TestGraphDerivationsProcessRawSamplesOnce(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	base := time.Date(2026, 5, 19, 12, 0, 0, 100_000_000, time.UTC)
	s.recordGraphRawSample("tec-75", 1000, 1, 20.0, base)
	s.recordGraphRawSample("tec-75", 1000, 1, 1.0, base.Add(1600*time.Millisecond))

	s.processDerivationsAt(base.Add(1800 * time.Millisecond))
	s.processDerivationsAt(base.Add(3500 * time.Millisecond))

	assertLongGraphMean(t, s, 10.5, base.Add(4*time.Second))
}

func assertLongGraphMean(t *testing.T, s *server, want float64, now time.Time) {
	t.Helper()
	long := s.lookupGraphHistory("tec-75", 1000, 1, 3*24*60*60*1000, now)
	if len(long.TS) != 1 || len(long.V) != 1 {
		t.Fatalf("3-day history = %+v, want one mean bucket", long)
	}
	if math.Abs(long.V[0]-want) > 1e-9 {
		t.Fatalf("3-day history mean = %v, want %v", long.V[0], want)
	}
}

func TestGraphHotHistoryKeepsRawSamplesOrderedAndTrimmed(t *testing.T) {
	h := newGraphTileHistory()
	base := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	h.mu.Lock()
	for _, sample := range []struct {
		offset time.Duration
		value  float64
	}{
		{20 * time.Minute, 20},
		{0, 0},
		{10 * time.Minute, 10},
		{5 * time.Minute, 5},
	} {
		h.addSampleLocked(base.Add(sample.offset), sample.value, true)
	}
	hot := h.hotHistoryLocked(base.Add(4 * time.Minute))
	h.mu.Unlock()

	if got, want := hot.V, []float64{5, 10, 20}; !equalFloatSlices(got, want) {
		t.Fatalf("hot values = %+v, want %+v", got, want)
	}
	if len(hot.TS) != 3 {
		t.Fatalf("hot timestamp count = %d, want 3", len(hot.TS))
	}
	prev := time.Time{}
	for _, raw := range hot.TS {
		ts, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			t.Fatalf("parse timestamp %q: %v", raw, err)
		}
		if !prev.IsZero() && ts.Before(prev) {
			t.Fatalf("timestamps not ordered: %+v", hot.TS)
		}
		prev = ts
	}
}

func equalFloatSlices(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestGatewayGraphTileDuplicatesSingleHistoryPointChronologically(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	now := time.Now().UTC().Add(-30 * time.Second)
	s.recordGraphSample("tec-75", 1022, 2, 0.0041, "ok", now)
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var tile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/single-point/live?series=tec-75:1022:2", http.StatusOK, &tile)
	if len(tile.Series) != 1 {
		t.Fatalf("series count = %d, want 1", len(tile.Series))
	}
	history := tile.Series[0].History
	if len(history.TS) != 2 || len(history.V) != 2 {
		t.Fatalf("history = %+v, want duplicated two-point series", history)
	}
	t0, err := time.Parse(time.RFC3339Nano, history.TS[0])
	if err != nil {
		t.Fatalf("parse t0: %v", err)
	}
	t1, err := time.Parse(time.RFC3339Nano, history.TS[1])
	if err != nil {
		t.Fatalf("parse t1: %v", err)
	}
	if !t0.Before(t1) {
		t.Fatalf("history timestamps are not chronological: %+v", history.TS)
	}
	if history.V[0] != 0.0041 || history.V[1] != 0.0041 {
		t.Fatalf("history values = %+v, want duplicated original value", history.V)
	}
}

func TestGatewayGraphHistoryImportSeedsTileCache(t *testing.T) {
	s := newServer(Config{Devices: []DeviceConfig{{
		ID:       "tec-75",
		Endpoint: "tcp:127.0.0.1:50000",
		Address:  75,
		Label:    "TEC 75",
	}}}, time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	now := time.Now().UTC().Truncate(time.Second)
	body := fmt.Sprintf(`{"schema_version":"signalforge.graph_tile.v1","id":"imported","renderer":"signalforge.tile.uplot","series":[{"id":"tec-75:1000:1","label":"SN75-ch1 OT","source":{"device_id":"tec-75","param_id":1000,"instance":1},"history":{"ts":[%q,%q],"v":[22.25,22.5]}}]}`,
		now.Add(-2*time.Minute).Format(time.RFC3339Nano),
		now.Add(-time.Minute).Format(time.RFC3339Nano),
	)
	raw := postJSON(t, ts.URL+"/api/graph/history/import", body, http.StatusOK)
	var imported graphHistoryImportResponse
	if err := json.Unmarshal(raw, &imported); err != nil {
		t.Fatal(err)
	}
	if imported.Status != "ok" || imported.SeriesCount != 1 || imported.ImportedSamples != 2 {
		t.Fatalf("import response = %+v, want 1 series and 2 samples", imported)
	}

	var tile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/imported/day?series=tec-75:1000:1", http.StatusOK, &tile)
	if len(tile.Series) != 1 {
		t.Fatalf("series count = %d, want 1", len(tile.Series))
	}
	series := tile.Series[0]
	if series.ID != "tec-75:1000:1" || series.Source.DeviceID != "tec-75" || series.Source.ParamID != 1000 || series.Source.Instance != 1 {
		t.Fatalf("series identity = %+v, want exact imported identity", series.Source)
	}
	if series.Quality != gatewayQualityOK || series.Diagnostics.HistoryPoints != 2 || series.Diagnostics.LiveRead != "" {
		t.Fatalf("series diagnostics = quality %q %+v, want history-backed ok without live read", series.Quality, series.Diagnostics)
	}
	if tile.Diagnostics.TileSource != "bounded-gateway-history-cache" {
		t.Fatalf("tile source = %q, want bounded-gateway-history-cache", tile.Diagnostics.TileSource)
	}
	if len(series.History.V) != 2 || series.History.V[0] != 22.25 || series.History.V[1] != 22.5 {
		t.Fatalf("history values = %+v, want imported values", series.History.V)
	}
}

func TestGatewayGraphHistoryImportHidesDetachedTemperatureSeries(t *testing.T) {
	s := newServer(Config{Devices: []DeviceConfig{{
		ID:       "tec-75",
		Endpoint: "tcp:127.0.0.1:50000",
		Address:  75,
		Label:    "TEC 75",
	}}}, time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	now := time.Now().UTC().Truncate(time.Second)
	body := fmt.Sprintf(`{"schema_version":"signalforge.graph_tile.v1","series":[{"source":{"device_id":"tec-75","param_id":1000,"instance":1},"history":{"ts":[%q,%q],"v":[23.25,23.5]}},{"source":{"device_id":"tec-75","param_id":1000,"instance":3},"history":{"ts":[%q,%q],"v":[-55.5,-55.25]}}]}`,
		now.Add(-20*time.Second).Format(time.RFC3339Nano),
		now.Add(-10*time.Second).Format(time.RFC3339Nano),
		now.Add(-20*time.Second).Format(time.RFC3339Nano),
		now.Add(-10*time.Second).Format(time.RFC3339Nano),
	)
	postJSON(t, ts.URL+"/api/graph/history/import", body, http.StatusOK)

	var tile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/imported/live?series=tec-75:1000:1&series=tec-75:1000:3", http.StatusOK, &tile)
	if len(tile.Series) != 2 {
		t.Fatalf("series count = %d, want 2", len(tile.Series))
	}

	visible := tile.Series[0]
	detached := tile.Series[1]
	if visible.ID != "tec-75:1000:1" || visible.Quality != gatewayQualityOK || !visible.DefaultVisible {
		t.Fatalf("visible imported series = id %q quality %q default_visible %v, want ok visible", visible.ID, visible.Quality, visible.DefaultVisible)
	}
	if detached.ID != "tec-75:1000:3" {
		t.Fatalf("detached series id = %q, want tec-75:1000:3", detached.ID)
	}
	if detached.Quality != gatewayQualityDetached || detached.Diagnostics.Status != gatewayQualityDetached {
		t.Fatalf("detached imported series = quality %q diagnostics %+v, want detached", detached.Quality, detached.Diagnostics)
	}
	if detached.DefaultVisible || detached.VisibilityReason == "" {
		t.Fatalf("detached imported default visibility = %v reason %q, want hidden with reason", detached.DefaultVisible, detached.VisibilityReason)
	}
	if detached.Diagnostics.SuppressedOpenSensorPoints != 2 || tile.Diagnostics.SuppressedOpenSensorPoints != 2 {
		t.Fatalf("suppressed detached points = item %d tile %d, want 2", detached.Diagnostics.SuppressedOpenSensorPoints, tile.Diagnostics.SuppressedOpenSensorPoints)
	}
	if len(detached.History.V) != 2 || detached.History.V[0] != -55.5 || detached.History.V[1] != -55.25 {
		t.Fatalf("detached imported history = %+v, want diagnostic values preserved", detached.History.V)
	}
}

func TestGatewayGraphHistoryExportRoundTripShape(t *testing.T) {
	s := newServer(Config{Devices: []DeviceConfig{{
		ID:       "tec-75",
		Endpoint: "tcp:127.0.0.1:50000",
		Address:  75,
		Label:    "TEC 75",
	}}}, time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	now := time.Now().UTC().Truncate(time.Second)
	body := fmt.Sprintf(`{"schema_version":"signalforge.graph_tile.v1","series":[{"source":{"device_id":"tec-75","param_id":1000,"instance":1},"history":{"ts":[%q,%q],"v":[22.25,22.5]}}]}`,
		now.Add(-2*time.Minute).Format(time.RFC3339Nano),
		now.Add(-time.Minute).Format(time.RFC3339Nano),
	)
	postJSON(t, ts.URL+"/api/graph/history/import", body, http.StatusOK)

	var exported graphHistoryExportResponse
	getJSON(t, ts.URL+"/api/graph/history/export?level=three_day&series=tec-75:1000:1", http.StatusOK, &exported)

	if exported.SchemaVersion != "signalforge.graph_tile.v1" || exported.Source != "meerstetter-go.graph-history" {
		t.Fatalf("unexpected export identity: %+v", exported)
	}
	if exported.Level != "three_day" || exported.TimeWindowMs != 3*24*60*60_000 {
		t.Fatalf("unexpected export window: level=%s window=%d", exported.Level, exported.TimeWindowMs)
	}
	if exported.SeriesCount != 1 || exported.SampleCount != 2 || len(exported.Series) != 1 {
		t.Fatalf("unexpected export counts: %+v", exported)
	}
	series := exported.Series[0]
	if series.DeviceID != "tec-75" || series.ParamID != 1000 || series.Instance != 1 {
		t.Fatalf("export identity = %+v, want exact imported identity", series)
	}
	if series.Source.DeviceID != "tec-75" || series.Source.ParamID != 1000 || series.Source.Instance != 1 || series.Source.SignalID == "" {
		t.Fatalf("export source = %+v, want signal source metadata", series.Source)
	}
	if len(series.History.TS) != 2 || len(series.History.V) != 2 || series.History.V[0] != 22.25 || series.History.V[1] != 22.5 {
		t.Fatalf("exported history = %+v, want imported bucket values", series.History)
	}

	fresh := newServer(Config{Devices: []DeviceConfig{{
		ID:       "tec-75",
		Endpoint: "tcp:127.0.0.1:50000",
		Address:  75,
		Label:    "TEC 75",
	}}}, time.Minute, log.New(io.Discard, "", 0))
	freshTS := httptest.NewServer(fresh.routes())
	defer freshTS.Close()
	raw, err := json.Marshal(graphHistoryImportRequest{
		SchemaVersion: exported.SchemaVersion,
		Source:        exported.Source,
		Series:        exported.Series,
	})
	if err != nil {
		t.Fatal(err)
	}
	postJSON(t, freshTS.URL+"/api/graph/history/import", string(raw), http.StatusOK)

	var tile graphTileResponse
	getJSON(t, freshTS.URL+"/api/graph/tiles/roundtrip/day?series=tec-75:1000:1", http.StatusOK, &tile)
	if len(tile.Series) != 1 || len(tile.Series[0].History.V) != 2 || tile.Series[0].History.V[0] != 22.25 || tile.Series[0].History.V[1] != 22.5 {
		t.Fatalf("round-tripped tile history = %+v", tile.Series)
	}
}

func TestGatewayGraphHistoryExportEmptyIsOk(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var exported graphHistoryExportResponse
	getJSON(t, ts.URL+"/api/graph/history/export?series=tec-75:1000:1", http.StatusOK, &exported)
	if exported.SchemaVersion != "signalforge.graph_tile.v1" || exported.SeriesCount != 0 || exported.SampleCount != 0 || len(exported.Series) != 0 {
		t.Fatalf("empty export = %+v, want empty successful archive", exported)
	}
}

func TestGatewayArrowImportSeedsTileCache(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	now := time.Now().UTC().Truncate(time.Second)
	body := buildArrowTelemetryStream(t, []arrowImportTestRow{
		{At: now, SensorID: "tec-75:1000:1", Value: 21.25, Quality: gatewayQualityOK},
	})
	raw := postArrow(t, ts.URL+"/api/log/import", body, http.StatusOK)
	var imported struct {
		ImportedSamples int `json:"imported_samples"`
	}
	if err := json.Unmarshal(raw, &imported); err != nil {
		t.Fatal(err)
	}
	if imported.ImportedSamples != 1 {
		t.Fatalf("imported samples = %d, want 1", imported.ImportedSamples)
	}

	hot := s.lookupGraphHistory("tec-75", 1000, 1, 15*60*1000, now.Add(time.Second))
	if len(hot.TS) != 1 || len(hot.V) != 1 || hot.V[0] != 21.25 {
		t.Fatalf("imported Arrow history = %+v, want one valid sample", hot)
	}
}

func TestGatewayArrowImportRejectsWrongSchema(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	postArrow(t, ts.URL+"/api/log/import", buildWrongArrowStream(t), http.StatusBadRequest)
}

func TestGatewayArrowImportRejectsTooManySamplesAtomically(t *testing.T) {
	oldLimit := arrowImportSampleLimit
	arrowImportSampleLimit = 1
	t.Cleanup(func() { arrowImportSampleLimit = oldLimit })

	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	now := time.Now().UTC().Truncate(time.Second)
	body := buildArrowTelemetryStream(t, []arrowImportTestRow{
		{At: now.Add(-time.Second), SensorID: "tec-75:1000:1", Value: 21.25, Quality: gatewayQualityOK},
		{At: now, SensorID: "tec-75:1000:1", Value: 21.5, Quality: gatewayQualityOK},
	})
	postArrow(t, ts.URL+"/api/log/import", body, http.StatusRequestEntityTooLarge)

	hot := s.lookupGraphHistory("tec-75", 1000, 1, 15*60*1000, now.Add(time.Second))
	if len(hot.TS) != 0 || len(hot.V) != 0 {
		t.Fatalf("rejected Arrow import persisted history: %+v", hot)
	}
}

func TestGatewayArrowImportRejectsOversizedBody(t *testing.T) {
	oldLimit := arrowImportByteLimit
	arrowImportByteLimit = 8
	t.Cleanup(func() { arrowImportByteLimit = oldLimit })

	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	now := time.Now().UTC().Truncate(time.Second)
	body := buildArrowTelemetryStream(t, []arrowImportTestRow{
		{At: now, SensorID: "tec-75:1000:1", Value: 21.25, Quality: gatewayQualityOK},
	})
	postArrow(t, ts.URL+"/api/log/import", body, http.StatusRequestEntityTooLarge)

	hot := s.lookupGraphHistory("tec-75", 1000, 1, 15*60*1000, now.Add(time.Second))
	if len(hot.TS) != 0 || len(hot.V) != 0 {
		t.Fatalf("oversized Arrow import persisted history: %+v", hot)
	}
}

func TestHDF5ExportTempFilesAreUniqueAndCleanable(t *testing.T) {
	first, err := newHDF5ExportTempFile()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(first)
	second, err := newHDF5ExportTempFile()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(second)

	if first == second {
		t.Fatalf("temp files collided: %q", first)
	}
	if strings.Contains(first, "/meerstetter-go/scratch/") || strings.Contains(second, "/meerstetter-go/scratch/") {
		t.Fatalf("temp files use repository scratch path: %q %q", first, second)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("first temp file is not cleanable: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("second temp file is not cleanable: %v", err)
	}
}

func TestGatewayGraphHistoryImportRejectsUnknownDevice(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := fmt.Sprintf(`{"series":[{"source":{"device_id":"tec-999","param_id":1000,"instance":1},"history":{"ts":[%q],"v":[22.25]}}]}`, now)
	postJSON(t, ts.URL+"/api/graph/history/import", body, http.StatusBadRequest)
}

func TestGatewayGraphHistoryImportRejectsAtomically(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := fmt.Sprintf(`{"series":[{"source":{"device_id":"tec-75","param_id":1000,"instance":1},"history":{"ts":[%q],"v":[22.25]}},{"source":{"device_id":"tec-999","param_id":1000,"instance":1},"history":{"ts":[%q],"v":[23.25]}}]}`, now, now)
	postJSON(t, ts.URL+"/api/graph/history/import", body, http.StatusBadRequest)

	var tile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/imported/day?series=tec-75:1000:1", http.StatusOK, &tile)
	if len(tile.Series) != 1 {
		t.Fatalf("series count = %d, want 1", len(tile.Series))
	}
	if tile.Series[0].Diagnostics.HistoryPoints != 0 || len(tile.Series[0].History.TS) != 0 || len(tile.Series[0].History.V) != 0 {
		t.Fatalf("rejected import persisted history: diagnostics=%+v history=%+v", tile.Series[0].Diagnostics, tile.Series[0].History)
	}
}

func TestGatewayGraphHistoryImportRejectsMalformedHistoryShape(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := fmt.Sprintf(`{"series":[{"source":{"device_id":"tec-75","param_id":1000,"instance":1},"history":{"ts":[%q],"v":[22.25,22.5]}}]}`, now)
	postJSON(t, ts.URL+"/api/graph/history/import", body, http.StatusBadRequest)
}

func TestGatewayGraphHistoryImportRejectsIdentityMismatch(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := fmt.Sprintf(`{"series":[{"device_id":"tec-75","source":{"device_id":"tec-76","param_id":1000,"instance":1},"history":{"ts":[%q],"v":[22.25]}}]}`, now)
	postJSON(t, ts.URL+"/api/graph/history/import", body, http.StatusBadRequest)
}

func TestGatewayGraphTileMissingSeriesStaysEmpty(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	s.devices["tec-75"].client = fakeGatewayDevice{byKey: map[string]float64{}}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var tile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/missing-series/live?series=tec-75:1000:1", http.StatusOK, &tile)

	if len(tile.Series) != 1 {
		t.Fatalf("series count = %d, want 1", len(tile.Series))
	}
	got := tile.Series[0]
	if got.Quality != "missing" {
		t.Fatalf("quality = %q, want missing", got.Quality)
	}
	if got.DefaultVisible || got.VisibilityReason == "" {
		t.Fatalf("missing series default visibility = %v reason %q, want hidden with reason", got.DefaultVisible, got.VisibilityReason)
	}
	if got.Diagnostics.Status != "missing" || got.Diagnostics.LiveRead != "unavailable" || got.Diagnostics.Message == "" {
		t.Fatalf("missing-series diagnostics = %+v", got.Diagnostics)
	}
	if len(got.Points) != 0 {
		t.Fatalf("missing-series points = %+v, want empty", got.Points)
	}
	if len(got.History.TS) != 0 || len(got.History.V) != 0 {
		t.Fatalf("missing-series history = %+v, want empty", got.History)
	}
	if got.History.TS == nil || got.History.V == nil {
		t.Fatalf("missing-series history slices are nil: %+v", got.History)
	}
	if tile.Diagnostics.SeriesCount != 1 || tile.Diagnostics.PointCount != 0 || tile.Diagnostics.Status != "ok" {
		t.Fatalf("unexpected tile diagnostics for missing series: %+v", tile.Diagnostics)
	}
}

func TestGatewayArchiveGraphTileDoesNotUseLiveReadFallback(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	s.devices["tec-75"].client = fakeGatewayDevice{byKey: map[string]float64{
		"1000:1": 25.5,
	}}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var archive graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/no-history/three_day?series=tec-75:1000:1", http.StatusOK, &archive)
	if len(archive.Series) != 1 {
		t.Fatalf("archive series count = %d, want 1", len(archive.Series))
	}
	gotArchive := archive.Series[0]
	if gotArchive.Quality != gatewayQualityMissing || gotArchive.Diagnostics.Status != gatewayQualityMissing {
		t.Fatalf("archive series quality/diagnostics = %q %+v, want missing without live synthesis", gotArchive.Quality, gotArchive.Diagnostics)
	}
	if gotArchive.Diagnostics.LiveRead != "not_attempted" {
		t.Fatalf("archive live_read = %q, want not_attempted", gotArchive.Diagnostics.LiveRead)
	}
	if len(gotArchive.Points) != 0 || len(gotArchive.History.TS) != 0 || len(gotArchive.History.V) != 0 {
		t.Fatalf("archive series was synthesized from live data: points=%+v history=%+v", gotArchive.Points, gotArchive.History)
	}

	var live graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/no-history/live?series=tec-75:1000:1", http.StatusOK, &live)
	if len(live.Series) != 1 {
		t.Fatalf("live series count = %d, want 1", len(live.Series))
	}
	gotLive := live.Series[0]
	if gotLive.Quality != gatewayQualityOK || gotLive.Diagnostics.LiveRead != "ok" || len(gotLive.Points) != 2 {
		t.Fatalf("live tile fallback = quality %q diagnostics %+v points=%+v, want explicit live fallback only", gotLive.Quality, gotLive.Diagnostics, gotLive.Points)
	}
}

func TestGatewayReadAndGraphTileClassifyDetachedTemperatureSensor(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	s.devices["tec-75"].client = fakeGatewayDevice{byKey: map[string]float64{
		"1000:3":  -55.5,
		"1001:3":  -55.5,
		"3000:3":  -55.5,
		"40000:3": -55.5,
	}}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var got struct {
		Values []gatewayReadValue `json:"values"`
	}
	getJSON(t, ts.URL+"/api/devices/tec-75/read?params=1000:3,1001:3,3000:3,40000:3", http.StatusOK, &got)
	if len(got.Values) != 4 {
		t.Fatalf("values len = %d, want 4", len(got.Values))
	}
	for _, idx := range []int{0, 1, 3} {
		if got.Values[idx].Value == nil || got.Values[idx].Quality != gatewayQualityDetached {
			t.Fatalf("value %d = %+v, want detached sensor value", idx, got.Values[idx])
		}
	}
	if got.Values[2].Value == nil || got.Values[2].Quality != gatewayQualityOK {
		t.Fatalf("target temperature quality = %+v, want ok", got.Values[2])
	}

	var tile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/detached-temp/live?series=tec-75:1000:3&series=tec-75:3000:3", http.StatusOK, &tile)
	if len(tile.Series) != 2 {
		t.Fatalf("series count = %d, want 2", len(tile.Series))
	}
	objectTemp := tile.Series[0]
	targetTemp := tile.Series[1]
	if objectTemp.Quality != gatewayQualityDetached || objectTemp.Diagnostics.Status != gatewayQualityDetached {
		t.Fatalf("object temperature series = quality %q diagnostics %+v, want detached", objectTemp.Quality, objectTemp.Diagnostics)
	}
	if objectTemp.DefaultVisible || objectTemp.VisibilityReason == "" {
		t.Fatalf("detached object temperature default visibility = %v reason %q, want hidden with reason", objectTemp.DefaultVisible, objectTemp.VisibilityReason)
	}
	if objectTemp.Diagnostics.SuppressedOpenSensorPoints == 0 || tile.Diagnostics.SuppressedOpenSensorPoints == 0 {
		t.Fatalf("detached points were not counted: item=%+v tile=%+v", objectTemp.Diagnostics, tile.Diagnostics)
	}
	if len(objectTemp.Points) == 0 || len(objectTemp.History.V) == 0 {
		t.Fatalf("detached value should stay available for diagnostics: %+v", objectTemp)
	}
	if targetTemp.Quality != gatewayQualityOK || targetTemp.Diagnostics.Status != gatewayQualityOK {
		t.Fatalf("target temperature series = quality %q diagnostics %+v, want ok", targetTemp.Quality, targetTemp.Diagnostics)
	}
	if !targetTemp.DefaultVisible || targetTemp.VisibilityReason != "" {
		t.Fatalf("target temperature default visibility = %v reason %q, want visible without reason", targetTemp.DefaultVisible, targetTemp.VisibilityReason)
	}
}

func TestGatewayReadPreservesExactChannelParameterInstances(t *testing.T) {
	s := newServer(Config{Devices: []DeviceConfig{{
		ID:       "tec-76",
		Endpoint: "tcp:127.0.0.1:50001",
		Address:  76,
		Label:    "TEC 76",
	}}}, time.Minute, log.New(io.Discard, "", 0))
	s.devices["tec-76"].client = fakeGatewayDevice{byKey: map[string]float64{
		"1000:1": 31.25,
		"1022:2": 0.0041,
		"1000:3": 32.50,
		"1022:4": 0.0035,
	}}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var got struct {
		Values []gatewayReadValue `json:"values"`
	}
	getJSON(t, ts.URL+"/api/devices/tec-76/read?params=1000:1,1022:2,1000:3,1022:4", http.StatusOK, &got)
	if len(got.Values) != 4 {
		t.Fatalf("values len = %d, want 4", len(got.Values))
	}
	want := []struct {
		id       int
		instance int
		value    float64
	}{
		{1000, 1, 31.25},
		{1022, 2, 0.0041},
		{1000, 3, 32.50},
		{1022, 4, 0.0035},
	}
	for i, w := range want {
		if got.Values[i].ID != w.id || got.Values[i].Instance != w.instance {
			t.Fatalf("value %d identity = %d:%d, want %d:%d", i, got.Values[i].ID, got.Values[i].Instance, w.id, w.instance)
		}
		if got.Values[i].Value == nil || *got.Values[i].Value != w.value || got.Values[i].Quality != gatewayQualityOK {
			t.Fatalf("value %d = %+v, want %v ok", i, got.Values[i], w.value)
		}
	}
}

func TestGatewayReadValuesExposeFreshnessMetadata(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	s.devices["tec-75"].client = fakeGatewayDevice{byKey: map[string]float64{
		"1000:1": 25.5,
		"1022:2": 0.0041,
	}}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var got struct {
		Values []struct {
			ID       int      `json:"id"`
			Instance int      `json:"instance"`
			Value    *float64 `json:"value"`
			Quality  string   `json:"quality"`
			At       string   `json:"at"`
			AgeMS    *int64   `json:"age_ms"`
		} `json:"values"`
	}
	getJSON(t, ts.URL+"/api/devices/tec-75/read?params=1000:1,1022:2", http.StatusOK, &got)
	if len(got.Values) != 2 {
		t.Fatalf("values len = %d, want 2", len(got.Values))
	}
	for i, value := range got.Values {
		if value.Value == nil || value.Quality != gatewayQualityOK {
			t.Fatalf("value %d = %+v, want readable ok value", i, value)
		}
		if value.At == "" {
			t.Fatalf("value %d at = empty, want sample timestamp", i)
		}
		if _, err := time.Parse(time.RFC3339Nano, value.At); err != nil {
			t.Fatalf("value %d at = %q, want RFC3339Nano timestamp: %v", i, value.At, err)
		}
		if value.AgeMS == nil || *value.AgeMS < 0 {
			t.Fatalf("value %d age_ms = %v, want non-negative freshness", i, value.AgeMS)
		}
	}
}

func TestGatewayGraphTilePreservesExactDeviceChannelInstances(t *testing.T) {
	s := newServer(Config{Devices: []DeviceConfig{
		{
			ID:       "tec-76",
			Endpoint: "tcp:127.0.0.1:50001",
			Address:  76,
			Label:    "TEC 76",
		},
		{
			ID:       "tec-75",
			Endpoint: "tcp:127.0.0.1:50000",
			Address:  75,
			Label:    "TEC 75",
		},
	}}, time.Minute, log.New(io.Discard, "", 0))
	s.devices["tec-76"].client = fakeGatewayDevice{byKey: map[string]float64{
		"1000:1": 21.25,
		"1000:3": -55.5,
	}}
	s.devices["tec-75"].client = fakeGatewayDevice{byKey: map[string]float64{
		"1000:1": 22.75,
	}}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var tile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/channel-isolation/live?series=tec-76:1000:1&series=tec-76:1000:3&series=tec-75:1000:1&series=tec-75:1000:3", http.StatusOK, &tile)
	if len(tile.Series) != 4 {
		t.Fatalf("series count = %d, want 4", len(tile.Series))
	}

	got := map[string]graphTileItem{}
	for _, series := range tile.Series {
		got[series.ID] = series
		if series.SeriesID != series.ID {
			t.Fatalf("series %q has series_id %q, want exact identity", series.ID, series.SeriesID)
		}
		if series.Source.DeviceID == "" || series.Source.ParamID != 1000 || series.Source.Instance == 0 {
			t.Fatalf("series %q source = %+v, want device/param/instance", series.ID, series.Source)
		}
	}

	wantVisible := map[string]float64{
		"tec-76:1000:1": 21.25,
		"tec-75:1000:1": 22.75,
	}
	for key, value := range wantVisible {
		series, ok := got[key]
		if !ok {
			t.Fatalf("series %q missing from graph tile", key)
		}
		if series.Source.DeviceID+":"+strconv.Itoa(series.Source.ParamID)+":"+strconv.Itoa(series.Source.Instance) != key {
			t.Fatalf("series %q source = %+v, want exact key", key, series.Source)
		}
		if len(series.History.V) != 2 || series.History.V[0] != value || series.History.V[1] != value {
			t.Fatalf("series %q history = %+v, want duplicated live value %v", key, series.History.V, value)
		}
		if series.Quality != gatewayQualityOK || !series.DefaultVisible || series.VisibilityReason != "" {
			t.Fatalf("series %q quality/default visibility = %q/%v reason %q, want ok visible", key, series.Quality, series.DefaultVisible, series.VisibilityReason)
		}
	}

	detached := got["tec-76:1000:3"]
	if detached.Source.DeviceID != "tec-76" || detached.Source.Instance != 3 {
		t.Fatalf("detached series source = %+v, want tec-76 instance 3", detached.Source)
	}
	if detached.Quality != gatewayQualityDetached || detached.DefaultVisible || detached.VisibilityReason == "" {
		t.Fatalf("detached series = quality %q visible %v reason %q, want hidden detached", detached.Quality, detached.DefaultVisible, detached.VisibilityReason)
	}
	if len(detached.History.V) != 2 || detached.History.V[0] != -55.5 || detached.History.V[1] != -55.5 {
		t.Fatalf("detached series history = %+v, want diagnostic value preserved", detached.History.V)
	}

	missing := got["tec-75:1000:3"]
	if missing.Source.DeviceID != "tec-75" || missing.Source.Instance != 3 {
		t.Fatalf("missing series source = %+v, want tec-75 instance 3", missing.Source)
	}
	if missing.Quality != gatewayQualityMissing || missing.DefaultVisible || missing.VisibilityReason == "" {
		t.Fatalf("missing series = quality %q visible %v reason %q, want hidden missing", missing.Quality, missing.DefaultVisible, missing.VisibilityReason)
	}
	if len(missing.History.V) != 0 || len(missing.Points) != 0 {
		t.Fatalf("missing series data = history %+v points %+v, want empty", missing.History, missing.Points)
	}
}

func TestGatewayDefaultGraphTilesUseAllConfiguredChannelRoles(t *testing.T) {
	channelRoles := []ChannelConfig{
		{Instance: 1, Role: "temp"},
		{Instance: 2, Role: "supply"},
		{Instance: 3, Role: "temp"},
		{Instance: 4, Role: "supply"},
		{Instance: 5, Role: "temp"},
		{Instance: 6, Role: "supply"},
	}
	s := newServer(Config{
		ChannelCount:    6,
		DefaultDeviceID: "tec-76",
		Devices: []DeviceConfig{
			{ID: "tec-81", Endpoint: "tcp:127.0.0.1:50002", Address: 81, Label: "TEC 81", ChannelCount: 6, Channels: channelRoles},
			{ID: "tec-76", Endpoint: "tcp:127.0.0.1:50001", Address: 76, Label: "TEC 76", ChannelCount: 6, Channels: channelRoles},
		},
	}, time.Minute, log.New(io.Discard, "", 0))
	s.devices["tec-76"].client = fakeGatewayDevice{byKey: map[string]float64{
		"1000:1": 21.25,
		"1000:3": -55.5,
		"1000:5": 22.50,
		"1022:2": 0.002,
		"1022:4": 0.004,
		"1022:6": 0.006,
	}}
	s.devices["tec-81"].client = fakeGatewayDevice{byKey: map[string]float64{
		"1000:1": 31.25,
		"1000:3": 32.50,
		"1022:2": 0.012,
		"1022:4": 0.014,
		"1022:6": 0.016,
		// 1000:5 is intentionally missing to verify that absent sensors remain
		// represented in the legend but hidden from the default plotted tile.
	}}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var tempTile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/fleet-temperature/live", http.StatusOK, &tempTile)
	wantTemp := map[string]struct {
		device   string
		instance int
		quality  string
		visible  bool
	}{
		"tec-76:1000:1": {device: "tec-76", instance: 1, quality: gatewayQualityOK, visible: true},
		"tec-76:1000:3": {device: "tec-76", instance: 3, quality: gatewayQualityDetached, visible: false},
		"tec-76:1000:5": {device: "tec-76", instance: 5, quality: gatewayQualityOK, visible: true},
		"tec-81:1000:1": {device: "tec-81", instance: 1, quality: gatewayQualityOK, visible: true},
		"tec-81:1000:3": {device: "tec-81", instance: 3, quality: gatewayQualityOK, visible: true},
		"tec-81:1000:5": {device: "tec-81", instance: 5, quality: gatewayQualityMissing, visible: false},
	}
	assertGraphTileSeriesSet(t, tempTile.Series, wantTemp, 1000)
	if len(tempTile.Series) > 0 && tempTile.Series[0].Source.DeviceID != "tec-76" {
		t.Fatalf("default device ordering starts with %q, want tec-76", tempTile.Series[0].Source.DeviceID)
	}
	for _, series := range tempTile.Series {
		if series.Source.Instance%2 == 0 {
			t.Fatalf("temperature tile contains supply channel series: %+v", series.Source)
		}
	}

	var powerTile graphTileResponse
	getJSON(t, ts.URL+"/api/graph/tiles/fleet-supply-power/live", http.StatusOK, &powerTile)
	wantPower := map[string]struct {
		device   string
		instance int
		quality  string
		visible  bool
	}{
		"tec-76:1022:2": {device: "tec-76", instance: 2, quality: gatewayQualityOK, visible: true},
		"tec-76:1022:4": {device: "tec-76", instance: 4, quality: gatewayQualityOK, visible: true},
		"tec-76:1022:6": {device: "tec-76", instance: 6, quality: gatewayQualityOK, visible: true},
		"tec-81:1022:2": {device: "tec-81", instance: 2, quality: gatewayQualityOK, visible: true},
		"tec-81:1022:4": {device: "tec-81", instance: 4, quality: gatewayQualityOK, visible: true},
		"tec-81:1022:6": {device: "tec-81", instance: 6, quality: gatewayQualityOK, visible: true},
	}
	assertGraphTileSeriesSet(t, powerTile.Series, wantPower, 1022)
	for _, series := range powerTile.Series {
		if series.Source.Instance%2 != 0 {
			t.Fatalf("power tile contains temperature channel series: %+v", series.Source)
		}
		if !series.DefaultVisible || series.VisibilityReason != "" {
			t.Fatalf("power series %q default visibility = %v reason %q, want visible", series.ID, series.DefaultVisible, series.VisibilityReason)
		}
	}
}

func assertGraphTileSeriesSet(t *testing.T, got []graphTileItem, want map[string]struct {
	device   string
	instance int
	quality  string
	visible  bool
}, paramID int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("series count = %d, want %d: %+v", len(got), len(want), got)
	}
	seen := map[string]bool{}
	for _, series := range got {
		expected, ok := want[series.ID]
		if !ok {
			t.Fatalf("unexpected series %q in tile: %+v", series.ID, series)
		}
		if seen[series.ID] {
			t.Fatalf("duplicate series identity %q", series.ID)
		}
		seen[series.ID] = true
		if series.SeriesID != series.ID {
			t.Fatalf("series %q has series_id %q, want exact identity", series.ID, series.SeriesID)
		}
		if series.Source.DeviceID != expected.device || series.Source.ParamID != paramID || series.Source.Instance != expected.instance {
			t.Fatalf("series %q source = %+v, want %s:%d:%d", series.ID, series.Source, expected.device, paramID, expected.instance)
		}
		if series.Quality != expected.quality || series.DefaultVisible != expected.visible {
			t.Fatalf("series %q quality/default visibility = %q/%v, want %q/%v", series.ID, series.Quality, series.DefaultVisible, expected.quality, expected.visible)
		}
		if !expected.visible && series.VisibilityReason == "" {
			t.Fatalf("series %q hidden without visibility reason", series.ID)
		}
	}
	for key := range want {
		if !seen[key] {
			t.Fatalf("expected series %q missing from tile", key)
		}
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

func TestGatewayRecordsTemperatureTargetWriteMetadata(t *testing.T) {
	s := newServer(Config{Devices: []DeviceConfig{{
		ID:       "tec-76",
		Endpoint: "can:can0/0x4c",
		Address:  76,
	}}}, time.Minute, log.New(io.Discard, "", 0))
	fw := &recordingWriteClient{}
	s.devices["tec-76"].client = fw
	s.devices["tec-76"].commander = mecom.NewCommander(fw, time.Second)
	s.devices["tec-76"].commander.TargetID = "tec-76"
	s.devices["tec-76"].commander.Authorizer = mecom.AuthorizerFunc(s.authorize)
	s.leases.Acquire("tec-76", "operator", time.Minute)
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	body := `{"name":"write_float32","arguments":{"param":3000,"instance":1,"value":25},"metadata":{"lease_token":"ignored"}}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/devices/tec-76/write", strings.NewReader(body))
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
	if fw.lastParamID != 3000 || fw.lastInstance != 1 || fw.value != 25 {
		t.Fatalf("writer got param=%d instance=%d value=%d, want 3000:1=25", fw.lastParamID, fw.lastInstance, fw.value)
	}

	var commands struct {
		Commands []gatewayCommandActivity `json:"commands"`
	}
	getJSON(t, ts.URL+"/api/commands", http.StatusOK, &commands)
	if len(commands.Commands) != 1 {
		t.Fatalf("commands len = %d, want 1", len(commands.Commands))
	}
	got := commands.Commands[0]
	if got.DeviceID != "tec-76" || got.ParamID != 3000 || got.Instance != 1 || got.SignalName != "target_object_temp_c" || got.SignalUnit != "degC" || got.Status != "completed" || got.HTTPStatus != http.StatusOK {
		t.Fatalf("unexpected temperature command metadata: %+v", got)
	}
	requested, ok := got.RequestedValue.(float64)
	if !ok || math.Abs(requested-25) > 1e-9 {
		t.Fatalf("temperature command requested value = %#v, want 25", got.RequestedValue)
	}
}

func TestGatewayWriteReturnsConflictWhenReadbackDoesNotMatch(t *testing.T) {
	s := newServer(Config{Devices: []DeviceConfig{{
		ID:       "tec-76",
		Endpoint: "can:can0/0x4c",
		Address:  76,
	}}}, time.Minute, log.New(io.Discard, "", 0))
	fw := &recordingWriteClient{
		readback: map[string]float64{gatewayCatalogueKey(3000, 1): 45},
	}
	s.devices["tec-76"].client = fw
	s.devices["tec-76"].commander = mecom.NewCommander(fw, time.Second)
	s.devices["tec-76"].commander.TargetID = "tec-76"
	s.devices["tec-76"].commander.Authorizer = mecom.AuthorizerFunc(s.authorize)
	s.leases.Acquire("tec-76", "operator", time.Minute)
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	body := `{"name":"write_float32","arguments":{"param":3000,"instance":1,"value":25},"metadata":{"lease_token":"ignored"}}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/devices/tec-76/write", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Lease-Token", s.leases.List()[0].Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("write status = %d, want 409 readback mismatch", resp.StatusCode)
	}

	var commands struct {
		Commands []gatewayCommandActivity `json:"commands"`
	}
	getJSON(t, ts.URL+"/api/commands", http.StatusOK, &commands)
	if len(commands.Commands) != 1 {
		t.Fatalf("commands len = %d, want 1", len(commands.Commands))
	}
	got := commands.Commands[0]
	if got.Status != "readback_mismatch" || got.HTTPStatus != http.StatusConflict || got.Error == "" {
		t.Fatalf("unexpected mismatch command activity: %+v", got)
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
	lastParamID  int
	lastInstance int
	value        int32
	floatValue   float32
	readback     map[string]float64
	readCount    int
	writeErr     error
	writes       []recordedWrite
}

type recordedWrite struct {
	paramID  int
	instance int
	value    float64
}

func (c *recordingWriteClient) WriteFloat32(ctx context.Context, paramID, instance int, value float32) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	c.lastParamID = paramID
	c.lastInstance = instance
	c.value = int32(value)
	c.floatValue = value
	c.writes = append(c.writes, recordedWrite{paramID: paramID, instance: instance, value: float64(value)})
	return nil
}

func (c *recordingWriteClient) WriteInt32(ctx context.Context, paramID, instance int, value int32) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	c.lastParamID = paramID
	c.lastInstance = instance
	c.value = value
	c.floatValue = float32(value)
	c.writes = append(c.writes, recordedWrite{paramID: paramID, instance: instance, value: float64(value)})
	return nil
}

func (c *recordingWriteClient) ReadBulk(ctx context.Context, params []mecom.Parameter) ([]float64, error) {
	c.readCount++
	values := make([]float64, 0, len(params))
	for _, p := range params {
		if c.readback != nil {
			if v, ok := c.readback[gatewayCatalogueKey(p.ID, p.Instance)]; ok {
				values = append(values, v)
				continue
			}
		}
		if p.ID != c.lastParamID || p.Instance != c.lastInstance {
			return nil, fmt.Errorf("%w: parameter %d instance %d", mecom.ErrUnknownParameter, p.ID, p.Instance)
		}
		if p.Type == mecom.DataTypeInt32 {
			values = append(values, float64(c.value))
		} else {
			values = append(values, float64(c.floatValue))
		}
	}
	return values, nil
}

func (c *recordingWriteClient) ReadFloat32(_ context.Context, paramID, instance int) (float64, error) {
	if c.readback != nil {
		if v, ok := c.readback[gatewayCatalogueKey(paramID, instance)]; ok {
			return v, nil
		}
	}
	if paramID == c.lastParamID && instance == c.lastInstance {
		return float64(c.floatValue), nil
	}
	return 0, nil
}

func (c *recordingWriteClient) ReadInt32(_ context.Context, paramID, instance int) (int32, error) {
	if paramID == c.lastParamID && instance == c.lastInstance {
		return c.value, nil
	}
	return 0, nil
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

func newPowerControlTestServer(enabled bool, readback map[string]float64) (*server, *recordingWriteClient) {
	cfg := Config{Devices: []DeviceConfig{{
		ID:                  "tec-75",
		Endpoint:            "tcp:127.0.0.1:50000",
		Address:             75,
		ChannelCount:        1,
		PowerControlEnabled: enabled,
		Channels: []ChannelConfig{{
			Instance: 1,
			Role:     "supply",
		}},
	}}}
	s := newServer(cfg, time.Minute, log.New(io.Discard, "", 0))
	fw := &recordingWriteClient{readback: readback}
	s.devices["tec-75"].client = fw
	s.devices["tec-75"].commander = mecom.NewCommander(fw, time.Second)
	s.devices["tec-75"].commander.TargetID = "tec-75"
	s.devices["tec-75"].commander.Authorizer = mecom.AuthorizerFunc(s.authorize)
	return s, fw
}

func stablePowerControlReadback() map[string]float64 {
	return map[string]float64{
		gatewayCatalogueKey(1021, 1): 10,
		gatewayCatalogueKey(1020, 1): 2,
		gatewayCatalogueKey(2010, 1): 1,
		gatewayCatalogueKey(2021, 1): 0,
		gatewayCatalogueKey(2030, 1): 0,
	}
}

func TestPowerControlRequiresExplicitDeviceOptIn(t *testing.T) {
	s, fw := newPowerControlTestServer(false, stablePowerControlReadback())
	s.setVirtualParam("tec-75", 2035, 1, 100)

	s.runDevicePowerControl(context.Background(), "tec-75", s.devices["tec-75"])

	if len(fw.writes) != 0 {
		t.Fatalf("power control wrote without opt-in: %+v", fw.writes)
	}
}

func TestPowerControlSamplingRequiresExplicitDeviceOptIn(t *testing.T) {
	s, _ := newPowerControlTestServer(false, stablePowerControlReadback())

	snap, err := s.bind("tec-75")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if snap.binding.ring != nil {
		t.Fatal("ring reader started when power control was disabled")
	}

	s.devices["tec-75"].cfg.PowerControlEnabled = true

	snap2, err2 := s.bind("tec-75")
	if err2 != nil {
		t.Fatalf("bind enabled: %v", err2)
	}
	if snap2.binding.ring != nil {
		t.Fatal("ring reader unexpectedly non-nil")
	}
}

func TestPowerControlRingReceiverStopsOnBindingReset(t *testing.T) {
	s, _ := newPowerControlTestServer(true, stablePowerControlReadback())
	bound := s.devices["tec-75"]
	samples := make(chan mecom.RingSample)
	bound.samplesChan = samples
	bound.ringStop = make(chan struct{})
	bound.ring = mecom.NewRingReader(nil, []mecom.RingCaptureParameter{
		{Parameter: mecom.Parameter{ID: 1021, Instance: 1}},
	}, samples)

	done := make(chan struct{})
	stop := bound.ringStop
	go func() {
		s.runDeviceRingReceiver("tec-75", bound, samples, stop)
		close(done)
	}()

	s.resetDeviceBinding("tec-75", nil, errors.New("reset"))

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ring receiver did not stop after binding reset")
	}
}

func TestPowerControlEnabledWritesSlewedSetpoints(t *testing.T) {
	s, fw := newPowerControlTestServer(true, stablePowerControlReadback())
	s.setVirtualParam("tec-75", 2035, 1, 100)

	s.runDevicePowerControl(context.Background(), "tec-75", s.devices["tec-75"])

	if len(fw.writes) != 2 {
		t.Fatalf("writes len = %d, want 2: %+v", len(fw.writes), fw.writes)
	}
	if fw.writes[0].paramID != 2021 || math.Abs(fw.writes[0].value-5.0) > 1e-6 {
		t.Fatalf("voltage write = %+v, want 2021:1=5.0", fw.writes[0])
	}
	if fw.writes[1].paramID != 2030 || math.Abs(fw.writes[1].value-2.0) > 1e-6 {
		t.Fatalf("current write = %+v, want 2030:1=2.0", fw.writes[1])
	}
	if got := s.getVirtualParam("tec-75", 2038, 1, -1); got != 1 {
		t.Fatalf("loop status = %v, want active 1", got)
	}
}

func TestPowerControlUsesBufferedHistoryForResistance(t *testing.T) {
	s, fw := newPowerControlTestServer(true, map[string]float64{
		gatewayCatalogueKey(2010, 1): 1,
		gatewayCatalogueKey(2021, 1): 0,
		gatewayCatalogueKey(2030, 1): 0,
	})
	s.devices["tec-75"].mu.Lock()
	s.devices["tec-75"].powerHistory[1] = []powerControlSample{
		{v: 7.8, i: 2},
		{v: 8.0, i: 2},
		{v: 8.2, i: 2},
	}
	s.devices["tec-75"].mu.Unlock()
	s.setVirtualParam("tec-75", 2035, 1, 64)

	s.runDevicePowerControl(context.Background(), "tec-75", s.devices["tec-75"])

	if len(fw.writes) != 2 {
		t.Fatalf("writes len = %d, want buffered control writes: %+v", len(fw.writes), fw.writes)
	}
	if fw.readCount != 1 {
		t.Fatalf("read count = %d, want only output/control readback without voltage/current fallback", fw.readCount)
	}
	if fw.writes[0].paramID != 2021 || math.Abs(fw.writes[0].value-5.0) > 1e-6 {
		t.Fatalf("voltage write = %+v, want 2021:1=5.0 from buffered R=4", fw.writes[0])
	}
	if fw.writes[1].paramID != 2030 || math.Abs(fw.writes[1].value-1.92) > 1e-6 {
		t.Fatalf("current write = %+v, want 2030:1=1.92 from buffered R=4", fw.writes[1])
	}
}

func TestPowerControlRejectsNonOhmicSamplesWithGraceWindow(t *testing.T) {
	s, fw := newPowerControlTestServer(true, map[string]float64{
		gatewayCatalogueKey(1021, 1): 10,
		gatewayCatalogueKey(1020, 1): 20,
		gatewayCatalogueKey(2010, 1): 1,
		gatewayCatalogueKey(2021, 1): 7,
		gatewayCatalogueKey(2030, 1): 3,
	})
	s.setVirtualParam("tec-75", 2035, 1, 100)
	s.setVirtualParam("tec-75", 2037, 1, 5)

	s.runDevicePowerControl(context.Background(), "tec-75", s.devices["tec-75"])
	if len(fw.writes) != 0 {
		t.Fatalf("non-ohmic grace sample wrote setpoints: %+v", fw.writes)
	}
	if got := s.getVirtualParam("tec-75", 2038, 1, -1); got != 1 {
		t.Fatalf("first non-ohmic status = %v, want active grace 1", got)
	}
	if got := s.getVirtualParam("tec-75", 2043, 1, 0); got != 1 {
		t.Fatalf("non-ohmic count = %v, want 1", got)
	}
	if got := s.getVirtualParam("tec-75", 2037, 1, 0); got != 5 {
		t.Fatalf("accepted resistance changed on rejected sample = %v, want frozen 5", got)
	}

	s.runDevicePowerControl(context.Background(), "tec-75", s.devices["tec-75"])
	s.runDevicePowerControl(context.Background(), "tec-75", s.devices["tec-75"])
	if got := s.getVirtualParam("tec-75", 2038, 1, -1); got != 2 {
		t.Fatalf("third non-ohmic status = %v, want fallback 2", got)
	}
	if got := s.getVirtualParam("tec-75", 2037, 1, 0); got != 5 {
		t.Fatalf("accepted resistance changed during fallback = %v, want frozen 5", got)
	}

	fw.readback = stablePowerControlReadback()
	s.runDevicePowerControl(context.Background(), "tec-75", s.devices["tec-75"])
	if got := s.getVirtualParam("tec-75", 2043, 1, -1); got != 0 {
		t.Fatalf("recovered non-ohmic count = %v, want reset 0", got)
	}
}

func TestPowerControlWriteErrorFallsBackAndResetsBinding(t *testing.T) {
	s, fw := newPowerControlTestServer(true, stablePowerControlReadback())
	fw.writeErr = mecom.ErrUnreachable
	s.setVirtualParam("tec-75", 2035, 1, 100)

	s.runDevicePowerControl(context.Background(), "tec-75", s.devices["tec-75"])

	if got := s.getVirtualParam("tec-75", 2038, 1, -1); got != 2 {
		t.Fatalf("write-error status = %v, want fallback 2", got)
	}
	if s.devices["tec-75"].client != nil {
		t.Fatalf("write-error binding still has client, want reset")
	}
}

func TestPowerControlTargetWriteRequiresOptInAndLease(t *testing.T) {
	s, _ := newPowerControlTestServer(false, stablePowerControlReadback())
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	body := `{"name":"write_float32","arguments":{"param":2035,"instance":1,"value":100}}`
	resp, err := http.Post(ts.URL+"/api/devices/tec-75/write", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("disabled power target write status = %d, want 403", resp.StatusCode)
	}

	s.devices["tec-75"].cfg.PowerControlEnabled = true
	resp, err = http.Post(ts.URL+"/api/devices/tec-75/write", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusLocked {
		t.Fatalf("unleased power target write status = %d, want 423", resp.StatusCode)
	}

	lease, err := s.leases.Acquire("tec-75", "operator", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/devices/tec-75/write", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Lease-Token", lease.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("leased power target write status = %d, want 200", resp.StatusCode)
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

func (f fakeGatewayDevice) ReadFloat32(_ context.Context, _, _ int) (float64, error) {
	return 0, nil
}

func (f fakeGatewayDevice) ReadInt32(_ context.Context, _, _ int) (int32, error) {
	return 0, nil
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

type fakeReadClient struct {
	closed bool
}

func (f *fakeReadClient) ReadFloat32(context.Context, int, int) (float64, error) { return 0, nil }
func (f *fakeReadClient) ReadInt32(context.Context, int, int) (int32, error)     { return 0, nil }
func (f *fakeReadClient) ReadBulk(context.Context, []mecom.Parameter) ([]float64, error) {
	return nil, nil
}
func (f *fakeReadClient) ConfigureRingCapture(context.Context, uint16, []mecom.RingCaptureParameter) error {
	return nil
}
func (f *fakeReadClient) TriggerRingSync(context.Context) error           { return nil }
func (f *fakeReadClient) ReadRingPointer(context.Context) (uint32, error) { return 0, nil }
func (f *fakeReadClient) ReadRingChunk(context.Context, uint32, uint16) (mecom.RingReadResponse, error) {
	return mecom.RingReadResponse{}, nil
}
func (f *fakeReadClient) Close() error {
	f.closed = true
	return nil
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

func postJSON(t *testing.T, url string, body string, wantStatus int) []byte {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST %s status = %d, want %d; body=%s", url, resp.StatusCode, wantStatus, string(raw))
	}
	return raw
}

func postArrow(t *testing.T, url string, body []byte, wantStatus int) []byte {
	t.Helper()
	resp, err := http.Post(url, arrowtelemetry.TransportMIME, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST %s status = %d, want %d; body=%s", url, resp.StatusCode, wantStatus, string(raw))
	}
	return raw
}

type arrowImportTestRow struct {
	At       time.Time
	SensorID string
	Value    float64
	Quality  string
}

func buildArrowTelemetryStream(t *testing.T, rows []arrowImportTestRow) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf, ipc.WithSchema(arrowtelemetry.TelemetrySchema))
	defer writer.Close()

	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrowtelemetry.TelemetrySchema)
	defer builder.Release()

	tsBuilder := builder.Field(0).(*array.Int64Builder)
	sensorBuilder := builder.Field(1).(*array.BinaryDictionaryBuilder)
	valueBuilder := builder.Field(2).(*array.Float64Builder)
	unitBuilder := builder.Field(3).(*array.BinaryDictionaryBuilder)
	campaignBuilder := builder.Field(4).(*array.BinaryDictionaryBuilder)
	sourceBuilder := builder.Field(5).(*array.BinaryDictionaryBuilder)
	roleBuilder := builder.Field(6).(*array.BinaryDictionaryBuilder)
	kindBuilder := builder.Field(7).(*array.BinaryDictionaryBuilder)
	familyBuilder := builder.Field(8).(*array.BinaryDictionaryBuilder)
	qualityBuilder := builder.Field(9).(*array.BinaryDictionaryBuilder)
	stateBuilder := builder.Field(10).(*array.BinaryDictionaryBuilder)

	for _, row := range rows {
		tsBuilder.Append(row.At.UnixNano())
		_ = sensorBuilder.AppendString(row.SensorID)
		valueBuilder.Append(row.Value)
		_ = unitBuilder.AppendString("degC")
		_ = campaignBuilder.AppendString("test")
		_ = sourceBuilder.AppendString("test")
		_ = roleBuilder.AppendString("telemetry")
		_ = kindBuilder.AppendString("temperature")
		_ = familyBuilder.AppendString("mecom")
		_ = qualityBuilder.AppendString(row.Quality)
		stateBuilder.AppendNull()
	}
	record := builder.NewRecord()
	if err := writer.Write(record); err != nil {
		record.Release()
		t.Fatal(err)
	}
	record.Release()
	return buf.Bytes()
}

func buildWrongArrowStream(t *testing.T) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "timestamp_ns", Type: arrow.PrimitiveTypes.Int64}}, nil)
	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	defer writer.Close()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).Append(time.Now().UnixNano())
	record := builder.NewRecord()
	if err := writer.Write(record); err != nil {
		record.Release()
		t.Fatal(err)
	}
	record.Release()
	return buf.Bytes()
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
