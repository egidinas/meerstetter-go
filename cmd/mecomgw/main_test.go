package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
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

	bad := dir + "/bad.json"
	if err := os.WriteFile(bad, []byte(`{"devices":[{"id":"missing-address","endpoint":"tcp:127.0.0.1:50000"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(bad); err == nil {
		t.Fatal("loadConfig accepted a device without address")
	}
}

func TestGatewayRoutesExposeHealthDevicesCatalogueAndLeases(t *testing.T) {
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	getJSON(t, ts.URL+"/api/healthz", http.StatusOK, nil)

	var devices struct {
		Devices []deviceView `json:"devices"`
	}
	getJSON(t, ts.URL+"/api/devices", http.StatusOK, &devices)
	if len(devices.Devices) != 1 || devices.Devices[0].ID != "tec-75" || devices.Devices[0].Bound {
		t.Fatalf("unexpected devices response: %+v", devices)
	}

	var catalogue struct {
		Parameters []struct {
			ID       int    `json:"id"`
			Instance int    `json:"instance"`
			Name     string `json:"name"`
			Type     string `json:"type"`
		} `json:"parameters"`
	}
	getJSON(t, ts.URL+"/api/catalogue", http.StatusOK, &catalogue)
	if len(catalogue.Parameters) == 0 {
		t.Fatal("catalogue response had no parameters")
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

func TestParseParamsQuery(t *testing.T) {
	params, err := parseParamsQuery("1000:1, 1021:2")
	if err != nil {
		t.Fatalf("parseParamsQuery returned error: %v", err)
	}
	if len(params) != 2 || params[0].ID != 1000 || params[0].Instance != 1 || params[1].ID != 1021 || params[1].Instance != 2 {
		t.Fatalf("unexpected params: %+v", params)
	}

	for _, raw := range []string{"", "1000", "x:1", "1000:x"} {
		if _, err := parseParamsQuery(raw); err == nil {
			t.Fatalf("parseParamsQuery(%q) returned nil error", raw)
		}
	}
}

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
