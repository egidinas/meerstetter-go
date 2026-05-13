package mecomserver

import (
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	DefaultListenHost    = "127.0.0.1"
	DefaultQueueDepth    = 256
	DefaultRingRetention = 100000
)

// QueuePolicy describes how smart TM/TC arbitration should be sized for one
// owned downstream connection.
type QueuePolicy struct {
	TelemetryDepth   int           `json:"telemetry_depth"`
	TelecommandDepth int           `json:"telecommand_depth"`
	RequestTimeout   time.Duration `json:"request_timeout"`
	ReconnectDelay   time.Duration `json:"reconnect_delay"`
}

// DeviceConfig describes one attached Meerstetter device or transparent
// Ethernet, serial, or CAN proxy.
// Each device gets one serialized owner and, optionally, one TCP passthrough
// listener for original Meerstetter software.
type DeviceConfig struct {
	ID                string            `json:"id"`
	Target            string            `json:"target"`
	RedundantTargets  []string          `json:"redundant_targets,omitempty"`
	PassthroughListen string            `json:"passthrough_listen,omitempty"`
	Queue             QueuePolicy       `json:"queue"`
	RingRetention     int               `json:"ring_retention"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// HubConfig is the neutral application shape for many controllers and
// transparent Ethernet, serial, and CAN targets.
type HubConfig struct {
	ListenHost string         `json:"listen_host"`
	Devices    []DeviceConfig `json:"devices"`
}

// DeviceSpec is the minimal user-facing input for an attached device.
type DeviceSpec struct {
	ID               string   `json:"id"`
	Target           string   `json:"target"`
	RedundantTargets []string `json:"redundant_targets,omitempty"`
}

// NewHubConfig creates a scalable default: every device has a bounded TM/TC
// queue, ring retention for lossless readout within the retention window, and a
// deterministic passthrough TCP port for original Meerstetter tools when the
// application has an adapter for the target transport.
func NewHubConfig(listenHost string, basePassthroughPort int, devices []DeviceSpec) (HubConfig, error) {
	if strings.TrimSpace(listenHost) == "" {
		listenHost = DefaultListenHost
	}
	if basePassthroughPort <= 0 {
		return HubConfig{}, fmt.Errorf("mecomserver: base passthrough port required")
	}
	cfg := HubConfig{ListenHost: listenHost}
	seen := map[string]struct{}{}
	for i, spec := range devices {
		id := strings.TrimSpace(spec.ID)
		target := strings.TrimSpace(spec.Target)
		if id == "" {
			return HubConfig{}, fmt.Errorf("mecomserver: device %d has no id", i)
		}
		if target == "" {
			return HubConfig{}, fmt.Errorf("mecomserver: device %q has no target", id)
		}
		if _, ok := seen[id]; ok {
			return HubConfig{}, fmt.Errorf("mecomserver: duplicate device id %q", id)
		}
		seen[id] = struct{}{}
		redundantTargets := cleanRedundantTargets(target, spec.RedundantTargets)
		passthroughListen := fmt.Sprintf("%s:%d", listenHost, basePassthroughPort+i)
		passthroughDownstream := passthroughDownstreamTarget(target, redundantTargets)
		cfg.Devices = append(cfg.Devices, DeviceConfig{
			ID:                id,
			Target:            target,
			RedundantTargets:  redundantTargets,
			PassthroughListen: passthroughListen,
			Queue: QueuePolicy{
				TelemetryDepth:   DefaultQueueDepth,
				TelecommandDepth: DefaultQueueDepth,
				RequestTimeout:   defaultRequestTimeout,
				ReconnectDelay:   defaultReconnectDelay,
			},
			RingRetention: DefaultRingRetention,
			Metadata: map[string]string{
				"owner":                   "local_node",
				"passthrough":             "meerstetter_original_software",
				"tm_mux":                  "mecom_crtvstream_ring_for_high_priority_vx_round_robin_for_background",
				"tc_demux":                "serialized_downstream",
				"ring_reduction":          "mean_stddev_window_to_consumer_rate",
				"consumer_rate_policy":    "publish_reduced_windows_at_requested_rate",
				"manual_poll":             "front_of_round_robin_queue",
				"single_read":             "compatibility_only",
				"bulk_readout":            "?VX",
				"primary_transport":       target,
				"preferred_transport":     target,
				"available_transports":    strings.Join(metadataTransportCandidates(target, redundantTargets, passthroughListen, passthroughDownstream), ","),
				"redundant_targets":       strings.Join(redundantTargets, ","),
				"passthrough_downstream":  passthroughDownstream,
				"active_transport_policy": "preferred_then_available_candidates",
			},
		})
	}
	return cfg, nil
}

func cleanRedundantTargets(primary string, targets []string) []string {
	seen := map[string]struct{}{}
	if primary = strings.TrimSpace(primary); primary != "" {
		seen[primary] = struct{}{}
	}
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}

// PassthroughTarget returns the MeCom-compatible downstream transport owned by
// the per-device TCP passthrough. CAN remains a selectable data path, but the
// transparent original-software path needs a byte-stream transport.
func (d DeviceConfig) PassthroughTarget() string {
	return passthroughDownstreamTarget(d.Target, d.RedundantTargets)
}

func passthroughDownstreamTarget(primary string, targets []string) string {
	for _, target := range append([]string{primary}, targets...) {
		target = strings.TrimSpace(target)
		if target == "" || isSocketCANTargetName(target) {
			continue
		}
		return target
	}
	return strings.TrimSpace(primary)
}

func metadataTransportCandidates(primary string, targets []string, passthroughListen string, passthroughDownstream string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(targets)+2)
	add := func(target string) {
		target = strings.TrimSpace(target)
		if target == "" {
			return
		}
		if _, ok := seen[target]; ok {
			return
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	add(primary)
	if strings.TrimSpace(passthroughDownstream) != "" && !isSocketCANTargetName(passthroughDownstream) {
		add(localPassthroughMetadataTarget(passthroughListen))
	}
	for _, target := range targets {
		add(target)
	}
	return out
}

func localPassthroughMetadataTarget(listen string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil || port == "" {
		return ""
	}
	switch strings.Trim(host, "[]") {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "tcp:" + net.JoinHostPort(host, port)
}

func isSocketCANTargetName(target string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.ToLower(target)), "socketcan:")
}

// ServerConfig converts a device into the existing single-device TCP proxy
// configuration.
func (d DeviceConfig) ServerConfig() Config {
	cfg := Config{
		Target:         d.PassthroughTarget(),
		RequestTimeout: d.Queue.RequestTimeout,
		ReconnectDelay: d.Queue.ReconnectDelay,
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = defaultReconnectDelay
	}
	return cfg
}
