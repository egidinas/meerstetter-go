package mecomserver

import (
	"fmt"
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
	ID     string `json:"id"`
	Target string `json:"target"`
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
		cfg.Devices = append(cfg.Devices, DeviceConfig{
			ID:                id,
			Target:            target,
			PassthroughListen: fmt.Sprintf("%s:%d", listenHost, basePassthroughPort+i),
			Queue: QueuePolicy{
				TelemetryDepth:   DefaultQueueDepth,
				TelecommandDepth: DefaultQueueDepth,
				RequestTimeout:   defaultRequestTimeout,
				ReconnectDelay:   defaultReconnectDelay,
			},
			RingRetention: DefaultRingRetention,
			Metadata: map[string]string{
				"owner":                "local_node",
				"passthrough":          "meerstetter_original_software",
				"tm_mux":               "mecom_crtvstream_ring_for_high_priority_vx_round_robin_for_background",
				"tc_demux":             "serialized_downstream",
				"ring_reduction":       "mean_stddev_window_to_consumer_rate",
				"consumer_rate_policy": "publish_reduced_windows_at_requested_rate",
				"manual_poll":          "front_of_round_robin_queue",
				"single_read":          "compatibility_only",
				"bulk_readout":         "?VX",
			},
		})
	}
	return cfg, nil
}

// ServerConfig converts a device into the existing single-device TCP proxy
// configuration.
func (d DeviceConfig) ServerConfig() Config {
	cfg := Config{
		Target:         d.Target,
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
