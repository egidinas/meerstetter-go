package mecomserver

import "testing"

func TestNewHubConfigCreatesPerDevicePassthroughAndQueues(t *testing.T) {
	cfg, err := NewHubConfig("", 15000, []DeviceSpec{
		{ID: "tec-01", Target: "tcp:192.168.1.10:50000"},
		{ID: "tec-02", Target: "COM4@57600"},
		{ID: "tec-03", Target: "can:can0/0x23"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenHost != DefaultListenHost {
		t.Fatalf("listen host = %q", cfg.ListenHost)
	}
	if len(cfg.Devices) != 3 {
		t.Fatalf("devices = %d", len(cfg.Devices))
	}
	if cfg.Devices[0].PassthroughListen != "127.0.0.1:15000" ||
		cfg.Devices[1].PassthroughListen != "127.0.0.1:15001" ||
		cfg.Devices[2].PassthroughListen != "127.0.0.1:15002" {
		t.Fatalf("passthrough listeners = %#v", cfg.Devices)
	}
	for _, device := range cfg.Devices {
		if device.Queue.TelemetryDepth != DefaultQueueDepth || device.Queue.TelecommandDepth != DefaultQueueDepth {
			t.Fatalf("queue policy for %s = %#v", device.ID, device.Queue)
		}
		if device.RingRetention != DefaultRingRetention {
			t.Fatalf("ring retention for %s = %d", device.ID, device.RingRetention)
		}
		if device.Metadata["tc_demux"] != "serialized_downstream" {
			t.Fatalf("metadata for %s = %#v", device.ID, device.Metadata)
		}
		if device.Metadata["manual_poll"] != "front_of_round_robin_queue" ||
			device.Metadata["single_read"] != "compatibility_only" ||
			device.Metadata["ring_reduction"] != "mean_stddev_window_to_consumer_rate" ||
			device.Metadata["consumer_rate_policy"] != "publish_reduced_windows_at_requested_rate" {
			t.Fatalf("readout metadata for %s = %#v", device.ID, device.Metadata)
		}
	}
}

func TestNewHubConfigRejectsAmbiguousDeviceOwnership(t *testing.T) {
	_, err := NewHubConfig("127.0.0.1", 15000, []DeviceSpec{
		{ID: "tec-01", Target: "tcp:192.168.1.10:50000"},
		{ID: "tec-01", Target: "tcp:192.168.1.11:50000"},
	})
	if err == nil {
		t.Fatal("expected duplicate device id error")
	}
}

func TestDeviceConfigServerConfigUsesQueueTiming(t *testing.T) {
	device := DeviceConfig{Target: "tcp:127.0.0.1:50000"}
	cfg := device.ServerConfig()
	if cfg.Target != device.Target {
		t.Fatalf("target = %q", cfg.Target)
	}
	if cfg.RequestTimeout != defaultRequestTimeout || cfg.ReconnectDelay != defaultReconnectDelay {
		t.Fatalf("defaults = %#v", cfg)
	}
}
