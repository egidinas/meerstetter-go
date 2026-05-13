package utility

import (
	"encoding/json"
	"fmt"
	"os"

	graphwall "github.com/egidinas/loom-gossamer-shared/go/graphwall"
	"github.com/egidinas/loom-gossamer-shared/go/tmtclog"
	"github.com/egidinas/meerstetter-go/mecomdict"
	"github.com/egidinas/meerstetter-go/mecomserver"
)

const (
	DefaultHTTPListen          = "127.0.0.1:18080"
	DefaultPassthroughBasePort = 15000
)

// Config is the standalone utility configuration. It is JSON by default so the
// neutral module does not need to carry a YAML dependency.
type Config struct {
	HTTPListen            string                   `json:"http_listen"`
	ListenHost            string                   `json:"listen_host"`
	PassthroughBasePort   int                      `json:"passthrough_base_port"`
	Devices               []mecomserver.DeviceSpec `json:"devices"`
	ReadPolicy            tmtclog.ReadPolicy       `json:"read_policy"`
	ParameterRegistryPath string                   `json:"parameter_registry_path,omitempty"`
	Instances             []int                    `json:"instances,omitempty"`
	DiscoverInstances     bool                     `json:"discover_instances,omitempty"`
	InstanceScanMax       int                      `json:"instance_scan_max,omitempty"`
	GraphWall             []GraphTileConfig        `json:"graph_wall,omitempty"`
	CANRingPath           string                   `json:"can_ring_path,omitempty"`
	CANRingFallbackPath   string                   `json:"can_ring_fallback_path,omitempty"`
	CANRingReplayLimit    int                      `json:"can_ring_replay_limit,omitempty"`
}

type GraphTileConfig = graphwall.TileConfig

func DefaultConfig() Config {
	return Config{
		HTTPListen:            DefaultHTTPListen,
		ListenHost:            mecomserver.DefaultListenHost,
		PassthroughBasePort:   DefaultPassthroughBasePort,
		ParameterRegistryPath: mecomdict.DefaultParameterRegistryPath(),
		DiscoverInstances:     true,
		InstanceScanMax:       16,
		ReadPolicy: tmtclog.ReadPolicy{
			DefaultMode: tmtclog.ReadModeSingle,
		},
		Devices: []mecomserver.DeviceSpec{{
			ID:     "tec-01",
			Target: "tcp:192.168.1.50:50000",
		}},
	}
}

func LoadConfig(path string) (Config, error) {
	if path == "" {
		return DefaultConfig(), nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.HTTPListen == "" {
		return fmt.Errorf("utility: http_listen required")
	}
	if c.PassthroughBasePort <= 0 {
		return fmt.Errorf("utility: passthrough_base_port required")
	}
	if len(c.Devices) == 0 {
		return fmt.Errorf("utility: at least one device required")
	}
	for _, instance := range c.Instances {
		if instance < 0 || instance > 255 {
			return fmt.Errorf("utility: instance %d outside MeCom range 0..255", instance)
		}
	}
	if c.InstanceScanMax < 0 || c.InstanceScanMax > 255 {
		return fmt.Errorf("utility: instance_scan_max %d outside MeCom range 0..255", c.InstanceScanMax)
	}
	if c.CANRingReplayLimit < 0 {
		return fmt.Errorf("utility: can_ring_replay_limit must be >= 0")
	}
	_, err := mecomserver.NewHubConfig(c.ListenHost, c.PassthroughBasePort, c.Devices)
	return err
}

func (c Config) HubConfig() (mecomserver.HubConfig, error) {
	return mecomserver.NewHubConfig(c.ListenHost, c.PassthroughBasePort, c.Devices)
}
