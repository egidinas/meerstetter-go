// mod-ingestor-mecom polls one or more MeComm TEC devices and publishes
// channel readings as Arrow BatchEnvelopes.
//
// Configuration (env vars):
//
//	LOOM_MECOM_TARGETS   — comma-separated list of "addr@host:port" or "addr@serial:/dev/ttyUSBx@baud"
//	                       e.g. "80@192.168.1.100:50000,81@192.168.1.101:50000"
//	LOOM_MECOM_CHANNELS  — number of TEC channels per device (default: 8)
//	LOOM_MECOM_POLL_MS   — poll interval in ms (default: 1000)
//	LOOM_DUT_SERIAL / LOOM_TEST_ID — tagging
//
// Key parameters polled per channel:
//
//	1000  Object Temperature          (°C)
//	1001  Sink Temperature            (°C)
//	52200 External/Cascade Temperature (°C)
//	3000  Target Object Temperature   (°C)
//	1011  Ramp Object Temperature     (°C)
//	1020  Output Current              (A)
//	1021  VAWC Voltage                (V)
//	1022  Output Power                (W)
//	1200  Temperature Stable          (int)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/egidinas/loom-gossamer-shared/go/edgepub"
	"github.com/egidinas/loom-gossamer-shared/go/envelope"
	"github.com/egidinas/loom-gossamer-shared/go/graphsem"
	"github.com/egidinas/loom-gossamer-shared/go/ipc"
	"github.com/egidinas/loom-gossamer-shared/go/lifecycle"
	"github.com/egidinas/loom-gossamer-shared/go/livebus"
	"github.com/egidinas/loom-gossamer-shared/go/modulekit"
	"github.com/egidinas/loom-gossamer-shared/go/otelutil"
	"github.com/egidinas/loom-gossamer-shared/go/schema"
	"github.com/egidinas/loom-gossamer-shared/go/telemetrytiles"
	"github.com/egidinas/meerstetter-go/mecom"
)

const moduleID = "mod-ingestor-mecom"

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type target struct {
	addr     int
	endpoint mecom.Endpoint
}

type targetReadout struct {
	target target
	dut    string
	testID string
	engine *mecom.Readout
}

func parseTargets(raw string) []target {
	var out []target
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		addrStr, endpointStr, found := strings.Cut(part, "@")
		if !found {
			continue
		}
		addr, err := strconv.Atoi(addrStr)
		if err != nil {
			slog.Info(fmt.Sprintf("[%s] bad address %q: %v", moduleID, addrStr, err))
			continue
		}
		ep, ok := mecom.ParseTarget(endpointStr)
		if !ok {
			slog.Info(fmt.Sprintf("[%s] bad endpoint %q", moduleID, endpointStr))
			continue
		}
		out = append(out, target{addr: addr, endpoint: ep})
	}
	return out
}

func main() {
	shutdownOTel, _ := otelutil.SetupOTel("mod-ingestor-mecom", os.Stdout)
	defer shutdownOTel(context.Background())
	slog.Info(fmt.Sprintf("[%s] Starting...", moduleID))
	instanceID := envOr("LOOM_INSTANCE_ID", "local")
	targetsRaw := envOr("LOOM_MECOM_TARGETS", "")
	channels, _ := strconv.Atoi(envOr("LOOM_MECOM_CHANNELS", "8"))
	if channels <= 0 {
		channels = 8
	}
	pollMs, _ := strconv.Atoi(envOr("LOOM_MECOM_POLL_MS", "1000"))
	if pollMs < 100 {
		pollMs = 100
	}
	bulkChunk, _ := strconv.Atoi(envOr("LOOM_MECOM_BULK_CHUNK", "8"))
	if bulkChunk <= 0 {
		bulkChunk = 8
	}
	channelModes := channelModesFromEnv(os.Getenv, channels)
	dut := envOr("LOOM_DUT_SERIAL", "")
	testID := envOr("LOOM_TEST_ID", "")

	targets := parseTargets(targetsRaw)
	if len(targets) == 0 {
		slog.Info(fmt.Sprintf("[%s] No LOOM_MECOM_TARGETS set; running in dry-run (no hardware)", moduleID))
	}
	readouts := make([]*targetReadout, 0, len(targets))
	for _, t := range targets {
		readouts = append(readouts, newDeviceReadoutWithModes(t, channels, dut, testID, bulkChunk, channelModes))
	}

	bus, err := ipc.NewBus("", "ipc-"+moduleID+"-"+instanceID, "")
	if err != nil {
		slog.Error(fmt.Sprintf("[%s] IPC: %v", moduleID, err))
		os.Exit(1)
	}
	defer bus.Close()
	_ = bus.PublishLifecycle(moduleID, instanceID, lifecycle.StateStarting,
		fmt.Sprintf("%d devices, %d channels each", len(targets), channels))

	dataBus, err := ipc.NewBus("", "data-"+moduleID+"-"+instanceID, "")
	if err != nil {
		slog.Error(fmt.Sprintf("[%s] DataBus: %v", moduleID, err))
		os.Exit(1)
	}
	defer dataBus.Close()
	nc := dataBus.Connection()

	subject := envOr("LOOM_MECOM_SUBJECT", "telemetry.v4.local.lab.bench1.mecom.live")
	sourceOffer := mecomSourceOffer(os.Getenv, channels, subject, pollMs, bulkChunk)
	stopSourceOfferHeartbeat := edgepub.StartSourceOfferHeartbeat(nc, sourceOffer, 5*time.Second)
	defer stopSourceOfferHeartbeat()

	_ = bus.PublishLifecycle(moduleID, instanceID, lifecycle.StateReady, "polling")
	lifecycle.WatchSupervisor(nc, 5*time.Second)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	_ = bus.SubscribeDrain(moduleID, instanceID, func() {
		_ = bus.PublishDrained(moduleID, instanceID, lifecycle.HandoverToken{})
		sigChan <- syscall.SIGTERM
	})

	ticker := time.NewTicker(time.Duration(pollMs) * time.Millisecond)
	defer ticker.Stop()
	seqNum := uint64(0)

	for {
		select {
		case <-sigChan:
			goto shutdown
		case <-ticker.C:
			var rows []schema.TelemetryRow
			for _, readout := range readouts {
				rows = append(rows, readout.Poll(context.Background())...)
			}
			if len(rows) == 0 {
				continue
			}
			payload, err := schema.EncodeTelemetryRows(rows)
			if err != nil {
				slog.Info(fmt.Sprintf("[%s] encode: %v", moduleID, err))
				continue
			}
			env := envelope.BatchEnvelope{
				SchemaID: "telemetry-mecom-v1", SchemaVer: 1,
				IngestorID:   moduleID,
				SequenceNum:  seqNum,
				TimestampMin: rows[0].TimestampNS,
				TimestampMax: rows[len(rows)-1].TimestampNS,
				LiveMetadata: edgepub.LiveBatchMetadataFromOffer(sourceOffer, subject, rows[len(rows)-1].TimestampNS, time.Now().UTC()),
				Payload:      payload,
			}
			env.UpdateCRC()
			seqNum++
			data, _ := env.EncodeBinary()
			_ = nc.Publish(subject, data)
		}
	}

shutdown:
	_ = bus.PublishLifecycle(moduleID, instanceID, lifecycle.StateDraining, "")
	time.Sleep(200 * time.Millisecond)
	_ = bus.PublishLifecycle(moduleID, instanceID, lifecycle.StateStopped, "stopped")
}

func pollDevice(t target, channels int, dut, testID string, bulkChunk int) []schema.TelemetryRow {
	return newDeviceReadout(t, channels, dut, testID, bulkChunk).Poll(context.Background())
}

func newDeviceReadout(t target, channels int, dut, testID string, bulkChunk int) *targetReadout {
	return newDeviceReadoutWithModes(t, channels, dut, testID, bulkChunk, nil)
}

func newDeviceReadoutWithModes(t target, channels int, dut, testID string, bulkChunk int, channelModes map[int]mecom.ChannelDriveMode) *targetReadout {
	if channels <= 0 {
		channels = 1
	}
	if bulkChunk <= 0 {
		bulkChunk = 8
	}
	modes := make(map[int]mecom.ChannelDriveMode, len(channelModes))
	for channel, mode := range channelModes {
		if channel >= 1 && channel <= channels {
			modes[channel] = mecom.NormalizeChannelDriveMode(mode)
		}
	}
	return &targetReadout{
		target: t,
		dut:    dut,
		testID: testID,
		engine: mecom.NewReadout(mecom.ReadoutConfig{
			Parameters: mecom.DefaultTECReadoutParameters(channels),
			BulkChunk:  bulkChunk,
			Derived: &mecom.DerivedReadoutConfig{
				ControllerAddress: t.addr,
				ChannelModes:      modes,
			},
		}),
	}
}

func (d *targetReadout) Poll(ctx context.Context) []schema.TelemetryRow {
	conn, err := mecom.Open(d.target.endpoint, 2*time.Second)
	if err != nil {
		slog.Info(fmt.Sprintf("[%s] connect %s: %v", moduleID, d.target.endpoint, err))
		return nil
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{
		Address: byte(d.target.addr),
		Timeout: 2 * time.Second,
	})
	return d.pollClient(ctx, client)
}

func (d *targetReadout) pollClient(ctx context.Context, client mecom.ReadClient) []schema.TelemetryRow {
	batch := d.engine.Poll(ctx, client, time.Now())
	for _, err := range batch.Errors {
		slog.Info(fmt.Sprintf("[%s] readout %s: %v", moduleID, d.target.endpoint, err))
	}
	rows := make([]schema.TelemetryRow, 0, len(batch.Values))
	for _, value := range batch.Values {
		rows = append(rows, d.row(value))
	}
	return rows
}

func (d *targetReadout) row(value mecom.ReadoutValue) schema.TelemetryRow {
	return schema.TelemetryRow{
		TimestampNS:  value.ObservedAt.UnixNano(),
		Sensor:       value.Sensor,
		Value:        value.Value,
		RawValue:     value.Value,
		DUTSerial:    d.dut,
		TestID:       d.testID,
		Source:       d.target.endpoint.String(),
		SourceFamily: string(graphsem.SourceFamilyMeComTec),
		Unit:         value.Parameter.Unit,
		SignalKind:   "continuous",
	}
}

func mecomSourceOffer(getenv func(string) string, channels int, subject string, pollMs int, bulkChunk int) telemetrytiles.SourceOffer {
	if channels <= 0 {
		channels = 8
	}
	if bulkChunk <= 0 {
		bulkChunk = 8
	}
	signals := mecomSignalNames(channels)
	catalogue := mecom.BuildMeComTECCatalogue(mecom.MeComTECCatalogueConfig{
		SourceID:          "mecom_tec",
		ChannelCount:      channels,
		SourceSubject:     subject,
		ControllerAddress: parsePositiveInt(getenv("LOOM_MECOM_CONTROLLER_ADDRESS")),
		FixtureProvenance: getenv("LOOM_MECOM_FIXTURE_PROVENANCE"),
	})
	metadata := modulekit.Metadata(modulekit.CapabilityManifest{
		ModuleID:                 moduleID,
		SemanticOwner:            moduleID,
		OfferRole:                modulekit.OfferRoleOrigin,
		SemanticModelID:          "loom.mecom_tec.v1",
		SemanticModelClass:       "mecom_tec_controller_bank",
		SemanticModelTitle:       "MeCom TEC controller bank",
		SemanticModelDescription: "Origin-authored read-only discovery for MeCom TEC channel telemetry. Command/write affordances remain disabled until leases, fencing, and receipts are implemented.",
		Properties:               signals,
		Actions:                  []string{"request_setpoint", "request_enable", "request_disable"},
		Events:                   []string{"tec_sample_batch", "mecom_read_error", "channel_state_changed"},
		Units:                    mecomUnits(),
		Freshness:                "poll_interval_ms_origin_declared",
		AllowedUses:              []string{"display", "preview", "archive", "derive", "rebid"},
		RebidAllowed:             modulekit.Bool(true),
		RebidRequires:            []string{"preserve_semantic_owner", "preserve_units", "declare_transform", "carry_input_offer_ids", "do_not_claim_control_authority"},
		Attribution:              moduleID,
		UIKind:                   "tec_controller_bank",
		Panels:                   []string{"main", "details", "settings", "docs", "preview"},
		Subjects:                 []string{subject},
	}, map[string]string{
		"action_policy":       "disabled_until_edge_owned_lease_and_originator_receipts",
		"channel_count":       strconv.Itoa(channels),
		"command_policy":      "read_only_discovery_writes_disabled_until_saga_lease",
		"control_audit":       "saga_receipt_required_for_apply_release_and_rollback",
		"control_lease":       "originator_lease_required_before_any_mecom_write",
		"control_surface":     "planned_channel_setpoint_write_requires_lease_and_receipt",
		"semantic_mapping":    "graphsem_mecom_tec_catalogue",
		"semantic_roles":      "test_spot_temperature,tec_sink_temperature,cascade_temperature,target_object_temperature,ramp_object_temperature,vawc_current,vawc_voltage,vawc_power,temperature_stable,electrical_input_power,heat_pumped_from_item,resistive_heat,hot_side_dissipated_heat",
		"catalogue_entries":   strconv.Itoa(len(catalogue.Entries)),
		"history_binding":     "arrow_tiles_when_archived",
		"poll_interval_ms":    strconv.Itoa(pollMs),
		"bulk_chunk":          strconv.Itoa(bulkChunk),
		"publish_subject":     subject,
		"derived_readout":     "mecom_derived_channel_model",
		"channel_mode_policy": "explicit_channel_modes_no_thermal_inference_for_power_supply",
		"readout_policy":      "ring_buffer_high_priority_reduced_to_consumer_rate_background_vx_round_robin",
		"priority_groups":     "vawc,cascade,key_temperatures",
		"high_priority_signals": strings.Join([]string{
			"object_temp_c",
			"sink_temp_c",
			"cascade_temp_c",
			"target_object_temp_c",
			"ramp_object_temp_c",
			"output_current_a",
			"vawc_voltage_v",
			"output_power_w",
		}, ","),
		"high_priority":      "mecom_crtvstream_ring_buffer",
		"ring_reduction":     "mean_stddev_window_to_consumer_rate",
		"background_readout": "?VX_round_robin_queue",
		"manual_poll_policy": "front_of_round_robin_queue_return_latest_when_polled",
		"single_read_policy": "compatibility_only",
	})
	cfg := edgepub.ConfigFromEnv(getenv, edgepub.SourceConfig{
		SourceID:            "mecom_tec",
		SourceFamily:        string(graphsem.SourceFamilyMeComTec),
		Streams:             []string{livebus.StreamTelemetrySamplesLive},
		Signals:             signals,
		SupportsHistory:     false,
		MaxBytesPerSecond:   64 * 1024,
		TileDurationSeconds: 30,
		Metadata:            metadata,
	})
	offer := cfg.Offer()
	if trimmed := strings.TrimSpace(subject); trimmed != "" {
		offer.Subjects = []string{trimmed}
	}
	return offer
}

func parsePositiveInt(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func channelModesFromEnv(getenv func(string) string, channels int) map[int]mecom.ChannelDriveMode {
	if channels <= 0 {
		channels = 8
	}
	modes := map[int]mecom.ChannelDriveMode{}
	defaultMode := parseChannelDriveMode(getenv("LOOM_MECOM_DEFAULT_CHANNEL_MODE"))
	if defaultMode != mecom.ChannelModeUnknown {
		for channel := 1; channel <= channels; channel++ {
			modes[channel] = defaultMode
		}
	}
	for channel, mode := range parseChannelModes(getenv("LOOM_MECOM_CHANNEL_MODES")) {
		if channel >= 1 && channel <= channels {
			modes[channel] = mode
		}
	}
	return modes
}

func parseChannelModes(raw string) map[int]mecom.ChannelDriveMode {
	modes := map[int]mecom.ChannelDriveMode{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		channelRaw, modeRaw, ok := strings.Cut(part, "=")
		if !ok {
			channelRaw, modeRaw, ok = strings.Cut(part, ":")
		}
		if !ok {
			continue
		}
		channel, err := strconv.Atoi(strings.TrimSpace(channelRaw))
		if err != nil || channel <= 0 {
			continue
		}
		mode := parseChannelDriveMode(modeRaw)
		if mode == mecom.ChannelModeUnknown {
			continue
		}
		modes[channel] = mode
	}
	return modes
}

func parseChannelDriveMode(raw string) mecom.ChannelDriveMode {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "resistor", "resistive", "heater":
		return mecom.ChannelModeResistor
	case "power_supply", "powersupply", "supply":
		return mecom.ChannelModePowerSupply
	case "peltier", "peltier_driver", "tec", "tec_driver":
		return mecom.ChannelModePeltierDriver
	default:
		return mecom.ChannelModeUnknown
	}
}

func mecomSignalNames(channels int) []string {
	return mecom.DefaultTECSignalNames(channels)
}

func mecomUnits() []string {
	return mecom.DefaultTECUnits()
}

func readBulk(ctx context.Context, client *mecom.Client, params []mecom.Parameter) ([]float64, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return client.ReadBulk(reqCtx, params)
}
