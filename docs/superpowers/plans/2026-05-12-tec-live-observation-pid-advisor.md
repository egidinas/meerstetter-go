# TEC Live Observation and PID Advisor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `meerstetter-go` the reusable foundation for deterministic TEC controller polling, high-priority ring-buffer readout, Peltier heat-flow modelling, asynchronous signal reduction, graph-wall telemetry, and a safety-gated PID advisor that observes every relevant transition before recommending or applying tuning changes.

**Architecture:** Extend the existing MeCom readout stack instead of adding a parallel toolchain. `mecom.Readout` remains the polling engine. High-priority parameters use MeCom ring-buffer readout. Lower-priority parameters use round-robin multi-parameter reads. Observer, reducer, Peltier heat-flow estimator, transition detector, model fitter, and PID advisor run asynchronously from polling through bounded in-memory queues. PiXtend performs deterministic polling and local in-memory buffering only; heavier reduction or characterization can be offloaded to another node.

**Tech Stack:** Go, existing `mecom`, `mecomautomation`, `canadapter`, `utility`, `cmd/*` packages, SocketCAN on PiXtend, Kvaser CANlib paths where available, in-memory ring buffers, existing LUT/TMTC sequencer surfaces.

---

## Starting Assumptions

- The current repo already contains the right base primitives: `mecom.Readout`, `ReadRingBuffer`, `ReadBulk`, `PollQueue.EnqueueFront`, `PollQueue.NextChunk`, `mecom/reduction.go`, `mecomautomation/lut.go`, `sequencer/sequencer.go`, `utility/*`, `canadapter/*`, `socketcan/*`, and probe commands under `cmd/`.
- The first implementation must not write PID parameters to devices by default. It observes and recommends only.
- Ring-buffer values are the preferred path for high-priority controller state. Bulk round-robin reads are the default path for all other values.
- Manual operator reads push parameters to the front of the polling queue and return the newest value once available.
- Thermal/electrical modelling must respect the configured channel drive mode. A channel can be configured as resistor, power supply, Peltier driver, or unknown. Peltier heat-flow equations apply only to channels configured as Peltier drivers.
- Peltier heat-flow modelling must use controller-specific module data when available: temperature gradient, current, voltage, heat pumped from the test item, and total dissipated heat. Unknown module data produces lower-confidence estimates, not silent false precision.
- The target operating case is four TEC controllers per PiXtend. The design must remain sane up to sixteen controllers by degrading rate, increasing buffer read length, and reducing lower-priority polling.
- No persistent high-rate logging happens on PiXtend. PiXtend keeps bounded in-memory windows and exports live state.
- Do not introduce new transport stacks unless the existing packages cannot support the requirement.

## Main Risks

- Unsafe tuning writes: a bad PID write can destabilize thermal control. Mitigation: advisory-only default, explicit safety levels, hard bounds, rollback snapshots, and operator approval gates.
- CAN bus saturation: polling every parameter at high rate does not scale. Mitigation: ring-buffer priority lane, round-robin queue, congestion feedback, and rate budgets per controller.
- Polling jitter on PiXtend: graphing and analysis must not block CAN polling. Mitigation: non-blocking sample fanout, bounded queues, dropped-analysis counters, and asynchronous reducers.
- False confidence from passive observation: closed-loop data may not identify a plant model reliably. Mitigation: model confidence scoring and recommendations that say "insufficient evidence" when appropriate.
- Wrong channel mode: treating a resistor or power-supply channel as a Peltier driver would produce false heat-flow estimates. Mitigation: explicit channel mode discovery/configuration, unknown-mode fallback, and tests that reject Peltier estimates unless the channel mode is `peltier_driver`.
- Wrong Peltier module data: heat-flow estimates can become misleading if module coefficients do not match installed hardware. Mitigation: explicit data provenance, confidence grades, per-channel calibration metadata, and graph labels that distinguish measured electrical power from estimated heat pumped.
- Repo drift: the worktree is already dirty. Mitigation: touch only the files listed per task and never remove or rewrite unrelated files.

## Smallest Reversible Probe

- Add tests for a high-priority signal catalogue and advisor safety gate.
- Implement no device writes.
- Run:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom ./canadapter ./utility ./mecomautomation
```

Success means the repo has a typed place for the polling and advisor model without changing live device behavior.

---

## Phase 1: Lock Down the Core Contracts

### Task 1: Add High-Priority TEC Signal Catalogue Tests

Files:

- `mecom/telemetry_catalogue_test.go`
- `mecom/telemetry_catalogue.go`

Test first:

- [ ] Create `TestDefaultTECCatalogueHasFourControllerPrioritySignals`.
- [ ] Assert the default catalogue can expand for controller addresses `31,32,33,34`.
- [ ] Assert each controller has priority entries for:
  - object temperature
  - sink temperature
  - target temperature
  - cascade temperature
  - device status
  - error status
  - output current
  - output voltage
  - output power
  - output current limit
  - output voltage limit
  - output power limit
- [ ] Assert each entry has:
  - stable ID
  - MeCom parameter ID or discovery key
  - controller address
  - signal kind
  - unit
  - priority class
  - preferred read lane
  - graph-wall default flag
- [ ] Create `TestDefaultTECCatalogueDoesNotUseForbiddenDisplayNames`.
- [ ] Assert no default display name contains bundled electrical shorthand; use explicit labels like `Output voltage`, `Output current`, `Output power`, `Object temperature`, `Cascade temperature`.

Implementation:

- [ ] Add:

```go
type ReadLane string

const (
    ReadLaneRingBuffer ReadLane = "ring_buffer"
    ReadLaneBulkQueue  ReadLane = "bulk_queue"
)

type SignalPriority string

const (
    PriorityCritical SignalPriority = "critical"
    PriorityHigh     SignalPriority = "high"
    PriorityNormal   SignalPriority = "normal"
    PriorityLow      SignalPriority = "low"
)

type SignalKind string

const (
    SignalTemperature SignalKind = "temperature"
    SignalElectrical  SignalKind = "electrical"
    SignalState       SignalKind = "state"
    SignalLimit       SignalKind = "limit"
    SignalTuning      SignalKind = "tuning"
)

type TelemetrySignal struct {
    ID                string
    ControllerAddress int
    ParameterID       uint16
    Instance          int
    DisplayName       string
    Unit              string
    Kind              SignalKind
    Priority          SignalPriority
    PreferredLane     ReadLane
    GraphDefault      bool
    RingDefault       bool
}

type TECCatalogue struct {
    Controllers []int
    Signals     []TelemetrySignal
}
```

- [ ] Add `DefaultTECCatalogue(controllerAddresses []int) TECCatalogue`.
- [ ] Use existing `mecomdict` registry names where stable. If a parameter ID is not confirmed, keep the entry discoverable through `DiscoveryKey` and exclude it from live polling until discovery resolves it.
- [ ] Keep the catalogue Meerstetter-specific inside `mecom`.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/telemetry_catalogue.go mecom/telemetry_catalogue_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestDefaultTECCatalogue'
```

### Task 2: Add Polling Policy Tests

Files:

- `mecom/polling_policy_test.go`
- `mecom/polling_policy.go`
- `mecom/readout.go`
- `mecom/pollqueue.go`

Test first:

- [ ] Create `TestPollingPolicyPrioritizesRingBufferForCriticalSignals`.
- [ ] Given four controllers and the default catalogue, assert critical temperature and electrical output signals are assigned to `ReadLaneRingBuffer`.
- [ ] Create `TestPollingPolicyUsesBulkQueueForNormalSignals`.
- [ ] Assert normal and low-priority parameters are assigned to `ReadLaneBulkQueue`.
- [ ] Create `TestPollingPolicyManualRequestMovesToFront`.
- [ ] Given a low-priority parameter, call `EnqueueManualRead`.
- [ ] Assert the next bulk chunk includes that parameter before older queue entries.
- [ ] Create `TestPollingPolicyCongestionIncreasesRingReadLength`.
- [ ] Given dropped samples or slow bulk reads, assert the policy increases ring-buffer read length and reduces lower-priority bulk frequency.

Implementation:

- [ ] Add:

```go
type PollingPolicy struct {
    RingMaxBytesMin     int
    RingMaxBytesMax     int
    BulkChunkMin        int
    BulkChunkMax        int
    MaxControllerCount  int
    TargetLoopPeriodMS  int
    CongestionThreshold float64
}

type PollingDecision struct {
    RingMaxBytes int
    BulkChunk    int
    SkipNormal   bool
    SkipLow      bool
}
```

- [ ] Add `DefaultPollingPolicy() PollingPolicy`.
- [ ] Add `Decision(metrics PollingMetrics) PollingDecision`.
- [ ] Reuse existing adaptive `ringMaxBytes` behavior in `mecom.Readout`; do not duplicate transport code.
- [ ] Add `ManualReadRequest` path that delegates to `PollQueue.EnqueueFront`.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/polling_policy.go mecom/polling_policy_test.go mecom/readout.go mecom/pollqueue.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestPollingPolicy|TestPollQueue'
```

---

## Phase 2: Build the Asynchronous Observation Pipeline

### Task 3: Add Typed Samples, Windows, and Bounded In-Memory Storage

Files:

- `mecom/observer_test.go`
- `mecom/observer.go`
- `mecom/window_recorder_test.go`
- `mecom/window_recorder.go`

Test first:

- [ ] Create `TestObserverAcceptsSamplesWithoutBlockingPolling`.
- [ ] Push samples into a full observer queue.
- [ ] Assert `TryObserve` returns quickly and increments a dropped-analysis counter instead of blocking.
- [ ] Create `TestWindowRecorderKeepsBoundedTransitionWindows`.
- [ ] Configure `MaxWindows=16`, `MaxSamplesPerWindow=2048`.
- [ ] Insert more windows and samples than the cap.
- [ ] Assert old windows are evicted and memory bounds are respected.

Implementation:

- [ ] Add:

```go
type TelemetrySample struct {
    Time              time.Time
    ControllerAddress int
    SignalID          string
    ParameterID       uint16
    Instance          int
    Value             float64
    Unit              string
    Lane              ReadLane
    Quality           SampleQuality
}

type SampleQuality string

const (
    SampleGood        SampleQuality = "good"
    SampleStale       SampleQuality = "stale"
    SampleTimeout     SampleQuality = "timeout"
    SampleDecodeError SampleQuality = "decode_error"
)

type Observer struct {
    // private bounded queues and counters
}
```

- [ ] Add `NewObserver(ObserverConfig) *Observer`.
- [ ] Add `TryObserve(sample TelemetrySample) bool`.
- [ ] Add `Snapshot() ObservationSnapshot`.
- [ ] Add `WindowRecorder` with bounded in-memory transition windows.
- [ ] Store counters for accepted samples, dropped analysis samples, stale samples, and queue depth.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/observer.go mecom/observer_test.go mecom/window_recorder.go mecom/window_recorder_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestObserver|TestWindowRecorder'
```

### Task 4: Integrate Observer Fanout with Existing Readout

Files:

- `mecom/readout.go`
- `mecom/readout_test.go`
- `mecom/advisor_service.go`
- `mecom/advisor_service_test.go`

Test first:

- [ ] Create `TestReadoutFanoutDoesNotBlockWhenObserverQueueFull`.
- [ ] Use a fake read client and observer with queue size one.
- [ ] Assert polling continues and latest values are still updated.
- [ ] Create `TestAdvisorServiceConsumesRingAndBulkSamples`.
- [ ] Feed one ring-buffer read and one bulk read.
- [ ] Assert both become `TelemetrySample` values with correct lane metadata.

Implementation:

- [ ] Add optional observer hook to `ReadoutConfig`:

```go
type SampleSink interface {
    TryObserve(TelemetrySample) bool
}
```

- [ ] Add `SampleSink SampleSink` to `ReadoutConfig`.
- [ ] Emit samples after successful ring-buffer and bulk reads.
- [ ] Do not let observer errors affect polling. Only increment readout metrics.
- [ ] Add `AdvisorService` as the long-lived coordinator:
  - accepts samples
  - updates transition detector
  - records windows
  - updates reductions
  - updates PID advisor

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/readout.go mecom/readout_test.go mecom/advisor_service.go mecom/advisor_service_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestReadout|TestAdvisorService'
```

---

## Phase 3: Proven Transition Detection and Behavior Characterization

### Task 5: Implement EWMA and CUSUM Transition Detection

Files:

- `mecom/transition_detector_test.go`
- `mecom/transition_detector.go`

Test first:

- [ ] Create `TestEWMADetectorTracksNoiseFloor`.
- [ ] Feed stable noisy temperature samples.
- [ ] Assert no transition is emitted.
- [ ] Create `TestCUSUMDetectorFindsStepChange`.
- [ ] Feed stable samples, then a clear setpoint or temperature step.
- [ ] Assert a transition event is emitted with start time, signal ID, direction, baseline, and threshold.
- [ ] Create `TestTransitionDetectorSeparatesHeatingAndCooling`.
- [ ] Feed positive and negative transitions.
- [ ] Assert directions are `heating`, `cooling`, or `disturbance`.

Implementation:

- [ ] Add:

```go
type TransitionDirection string

const (
    TransitionHeating     TransitionDirection = "heating"
    TransitionCooling     TransitionDirection = "cooling"
    TransitionDisturbance TransitionDirection = "disturbance"
)

type TransitionEvent struct {
    ID                string
    ControllerAddress int
    SignalID          string
    Start             time.Time
    End               time.Time
    Direction         TransitionDirection
    Baseline          float64
    PeakDelta         float64
    NoiseSigma        float64
    Confidence        float64
}
```

- [ ] Implement EWMA baseline and variance tracking.
- [ ] Implement two-sided CUSUM for shift detection.
- [ ] Use conservative defaults:
  - minimum stable samples: 20
  - minimum transition samples: 10
  - threshold: `5 * noise sigma`
  - cool-down: one target loop period after event close

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/transition_detector.go mecom/transition_detector_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestEWMA|TestCUSUM|TestTransition'
```

### Task 6: Add Offline PELT-Style Change-Point Refinement for Recorded Windows

Files:

- `mecom/changepoint_test.go`
- `mecom/changepoint.go`

Test first:

- [ ] Create `TestPELTFindsKnownChangePoints`.
- [ ] Generate a deterministic signal with changes at sample indices 40 and 95.
- [ ] Assert returned change points are within two samples.
- [ ] Create `TestPELTReturnsNoChangeForStableNoise`.
- [ ] Feed stable noise.
- [ ] Assert no change points above confidence threshold.

Implementation:

- [ ] Add `DetectChangePointsPELT(values []float64, penalty float64) []ChangePoint`.
- [ ] Use squared-error segment cost.
- [ ] Keep this offline only; run it on closed transition windows, not inside the polling loop.
- [ ] Cap input length through `WindowRecorder.MaxSamplesPerWindow`.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/changepoint.go mecom/changepoint_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestPELT'
```

### Task 7: Score Transition Quality and Thermal Behavior

Files:

- `mecom/behavior_scorer_test.go`
- `mecom/behavior_scorer.go`

Test first:

- [ ] Create `TestBehaviorScorerComputesOvershootSettlingAndError`.
- [ ] Feed a synthetic step response.
- [ ] Assert overshoot, rise time, settling time, steady-state error, and noise floor.
- [ ] Create `TestBehaviorScorerFlagsOscillation`.
- [ ] Feed a damped oscillation.
- [ ] Assert oscillation score is greater than stable response score.
- [ ] Create `TestBehaviorScorerFlagsSaturation`.
- [ ] Feed output power/current pinned at limit.
- [ ] Assert saturation duration and saturation fraction.

Implementation:

- [ ] Add:

```go
type BehaviorScore struct {
    ControllerAddress int
    SituationID       string
    Direction         TransitionDirection
    RiseTime          time.Duration
    SettlingTime      time.Duration
    Overshoot         float64
    SteadyStateError  float64
    OscillationScore  float64
    SaturationSeconds float64
    NoiseSigma        float64
    SampleCount       int
    Confidence        float64
}
```

- [ ] Score every closed transition window.
- [ ] Persist only the newest bounded set in memory.
- [ ] Expose stable snapshots for graph wall and advisor.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/behavior_scorer.go mecom/behavior_scorer_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestBehaviorScorer'
```

### Task 7A: Model Channel Thermal/Electrical Output Per Channel and Globally

Files:

- `mecom/channel_mode_test.go`
- `mecom/channel_mode.go`
- `mecom/peltier_model_test.go`
- `mecom/peltier_model.go`
- `mecom/telemetry_catalogue.go`
- `mecom/telemetry_catalogue_test.go`
- `utility/tiles.go`
- `utility/tiles_test.go`
- `docs/tec_live_observation_pid_advisor.md`

Test first:

- [ ] Create `TestChannelModeControlsThermalEstimator`.
- [ ] Given channel mode `resistor`, assert the estimator reports measured electrical input and resistive heat only, with no Peltier heat-pumped estimate.
- [ ] Given channel mode `power_supply`, assert the estimator reports electrical output to external load and marks thermal effect as unknown.
- [ ] Given channel mode `peltier_driver`, assert the estimator uses Peltier module data.
- [ ] Given channel mode `unknown`, assert only measured electrical power is reported and derived thermal estimates have confidence `0.0`.
- [ ] Create `TestPeltierModelInterpolatesHeatPumpedFromModuleData`.
- [ ] Use a deterministic module table with rows for temperature gradient, current, voltage, and heat pumped.
- [ ] Assert interpolation returns expected heat pumped at an exact table point.
- [ ] Assert bilinear interpolation returns a value between neighbouring points for an intermediate temperature gradient and current.
- [ ] Create `TestPeltierModelComputesDissipatedHeat`.
- [ ] Given voltage `12.0 V`, current `2.0 A`, and heat pumped `15.0 W`, assert electrical input power is `24.0 W` and hot-side dissipated heat is `39.0 W`.
- [ ] Create `TestResistorModeComputesHeatWithoutPeltierTerms`.
- [ ] Given voltage `12.0 V` and current `2.0 A`, assert electrical input power is `24.0 W`, resistive heat is `24.0 W`, heat-pumped value is invalid, and hot-side dissipated heat is invalid.
- [ ] Create `TestPowerSupplyModeDoesNotClaimThermalCoupling`.
- [ ] Given voltage `12.0 V` and current `2.0 A`, assert electrical output power is `24.0 W`, thermal coupling is unknown, heat-pumped value is invalid, and confidence for thermal estimates is `0.0`.
- [ ] Create `TestPeltierModelHandlesCoolingAndHeatingDirection`.
- [ ] Assert sign convention is explicit:
  - positive heat pumped means heat removed from the test item
  - negative heat pumped means heat delivered into the test item
  - electrical input power is always non-negative after absolute current and voltage validation
- [ ] Create `TestGlobalHeatBalanceSumsFourControllers`.
- [ ] Given four per-channel estimates across mixed modes, assert global electrical power sums all valid modes, global heat pumped sums only valid Peltier estimates, global resistive heat sums only resistor channels, and global hot-side dissipated heat sums only valid Peltier estimates.
- [ ] Create `TestPeltierEstimateConfidenceDropsWithoutModuleData`.
- [ ] Given missing module table data, assert the estimator still reports measured electrical power but marks heat-pumped and hot-side heat estimates as low confidence.
- [ ] Extend `TestGraphWallDefaultsForFourControllers`.
- [ ] Assert the graph wall includes per-channel and global panels for:
  - electrical input power
  - estimated heat pumped from test item
  - estimated hot-side dissipated heat
  - resistive heat
  - channel mode
  - model confidence

Implementation:

- [ ] Add:

```go
type ChannelDriveMode string

const (
    ChannelModeUnknown       ChannelDriveMode = "unknown"
    ChannelModeResistor      ChannelDriveMode = "resistor"
    ChannelModePowerSupply   ChannelDriveMode = "power_supply"
    ChannelModePeltierDriver ChannelDriveMode = "peltier_driver"
)

type DerivedValue struct {
    Value      float64
    Valid      bool
    Confidence float64
    Source     string
}

type PeltierModulePoint struct {
    DeltaT          float64
    CurrentAmpere   float64
    VoltageVolt     float64
    HeatPumpedWatt  float64
}

type PeltierModuleData struct {
    ModuleID string
    Source   string
    Points   []PeltierModulePoint
}

type PeltierChannelInput struct {
    ControllerAddress int
    Channel           int
    DriveMode         ChannelDriveMode
    ColdTemperatureC  float64
    HotTemperatureC   float64
    CurrentAmpere     float64
    VoltageVolt       float64
}

type PeltierChannelEstimate struct {
    ControllerAddress     int
    Channel               int
    DriveMode             ChannelDriveMode
    DeltaTC               float64
    ElectricalInputWatt   float64
    HeatPumpedFromItemWatt DerivedValue
    ResistiveHeatWatt      DerivedValue
    HotSideDissipatedWatt  DerivedValue
    Confidence            float64
    Source                string
}

type PeltierGlobalEstimate struct {
    ElectricalInputWatt     float64
    HeatPumpedFromItemWatt  float64
    ResistiveHeatWatt       float64
    HotSideDissipatedWatt   float64
    ValidPeltierChannels    int
    ValidResistorChannels   int
    PowerSupplyChannels     int
    UnknownModeChannels     int
    MinConfidence           float64
    ChannelCount            int
}
```

- [ ] Add `ChannelModeResolver`:

```go
type ChannelModeResolver interface {
    ModeFor(controllerAddress int, channel int) ChannelDriveMode
}
```

- [ ] Add `NewPeltierEstimator(moduleData map[int]PeltierModuleData) *PeltierEstimator`.
- [ ] Add `EstimateChannel(input PeltierChannelInput) PeltierChannelEstimate`.
- [ ] Add `EstimateGlobal(estimates []PeltierChannelEstimate) PeltierGlobalEstimate`.
- [ ] Use sign convention consistently:
  - `DeltaTC = HotTemperatureC - ColdTemperatureC`
  - `ElectricalInputWatt = abs(CurrentAmpere * VoltageVolt)`
  - Peltier mode: `HotSideDissipatedWatt = ElectricalInputWatt + HeatPumpedFromItemWatt`
  - resistor mode: `ResistiveHeatWatt = ElectricalInputWatt`
  - power-supply mode: electrical output is measured, thermal effect is unknown unless a later explicit load model is configured
- [ ] Implement interpolation over module points only for `ChannelModePeltierDriver` by selecting the nearest enclosing `DeltaT` and `CurrentAmpere` grid values. If the exact voltage from the module table differs materially from measured voltage, reduce confidence but keep measured electrical input power.
- [ ] If no module table is configured for a Peltier-driver channel, return:
  - measured electrical input power
  - invalid heat pumped estimate
  - invalid hot-side estimate
  - confidence `0.0`
  - source `missing_module_data`
- [ ] For resistor channels, do not read or require temperature-gradient data.
- [ ] For power-supply channels, do not infer test-item heat. Report electrical output only.
- [ ] For unknown channels, do not infer thermal behavior. Report measured electrical power only.
- [ ] Extend `TelemetrySignal` with derived-signal support:

```go
type SignalDerivation string

const (
    SignalMeasured SignalDerivation = "measured"
    SignalDerived  SignalDerivation = "derived"
)
```

- [ ] Add derived catalogue entries for per-channel and global Peltier estimates.
- [ ] Add derived catalogue entries for resistor heat and power-supply electrical output.
- [ ] Add measured or discovered catalogue entry for channel mode.
- [ ] Keep derived estimates out of the polling queue; they are produced by the observer after source signals arrive.
- [ ] Add graph-wall tiles that clearly label estimates and confidence.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/peltier_model.go mecom/peltier_model_test.go mecom/telemetry_catalogue.go mecom/telemetry_catalogue_test.go utility/tiles.go utility/tiles_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestChannelMode|TestPeltier|TestResistor|TestPowerSupply|TestDefaultTECCatalogue'
/home/svc_pmg_testbed_b/.local/go/bin/go test ./utility -run 'TestGraphWall'
```

### Task 7B: Feed Channel Thermal/Electrical Estimates into Transition Scoring and PID Advice

Files:

- `mecom/channel_mode.go`
- `mecom/channel_mode_test.go`
- `mecom/advisor_service.go`
- `mecom/advisor_service_test.go`
- `mecom/behavior_scorer.go`
- `mecom/behavior_scorer_test.go`
- `mecom/pid_advisor.go`
- `mecom/pid_advisor_test.go`

Test first:

- [ ] Create `TestAdvisorServicePublishesDerivedPeltierSamples`.
- [ ] Feed cold-side temperature, hot-side temperature, current, and voltage samples for one channel.
- [ ] Assert the advisor service emits derived samples for electrical input power, heat pumped from the test item, hot-side dissipated heat, and confidence.
- [ ] Create `TestAdvisorServicePublishesResistorModeSamples`.
- [ ] Feed resistor channel mode, current, and voltage samples.
- [ ] Assert the advisor service emits electrical input power and resistive heat, and does not emit heat-pumped or hot-side Peltier estimates.
- [ ] Create `TestAdvisorServicePublishesPowerSupplyModeSamples`.
- [ ] Feed power-supply channel mode, current, and voltage samples.
- [ ] Assert the advisor service emits electrical output power and marks thermal effect unknown.
- [ ] Create `TestBehaviorScorerUsesHeatFlowForSaturationAndEfficiency`.
- [ ] Feed a transition with high electrical power and low estimated heat pumped.
- [ ] Assert the behavior score reports low thermal effectiveness and elevated saturation risk.
- [ ] Create `TestPIDAdvisorIncludesHeatFlowEvidenceInRecommendationReason`.
- [ ] Feed accepted behavior and Peltier estimates.
- [ ] Assert the PID recommendation reason includes whether the limiting factor appears to be thermal load, electrical saturation, or control oscillation.

Implementation:

- [ ] Extend `AdvisorService` to maintain a small per-controller source-sample cache:

```go
type PeltierSourceCache struct {
    DriveMode       ChannelDriveMode
    ColdTemperature *TelemetrySample
    HotTemperature  *TelemetrySample
    Current         *TelemetrySample
    Voltage         *TelemetrySample
}
```

- [ ] When all required source samples for a channel are fresh enough, call `PeltierEstimator.EstimateChannel`.
- [ ] For resistor and power-supply modes, do not wait for cold-side and hot-side temperature samples before producing electrical output estimates.
- [ ] Emit derived samples through the same observer/reducer path as measured samples, with `Quality=SampleGood` when confidence is greater than `0.5`, otherwise `Quality=SampleStale`.
- [ ] Extend `BehaviorScore` with:

```go
ThermalEffectiveness float64
ElectricalInputWatt  float64
HeatPumpedWatt       float64
ResistiveHeatWatt    float64
HotSideHeatWatt      float64
HeatModelConfidence  float64
ChannelMode          ChannelDriveMode
```

- [ ] Define `ThermalEffectiveness = abs(HeatPumpedWatt) / max(ElectricalInputWatt, 0.001)`.
- [ ] For resistor and power-supply modes, leave `ThermalEffectiveness` invalid unless an explicit thermal coupling model exists.
- [ ] Extend PID recommendation reasons with observed evidence:
  - control oscillation
  - actuator saturation
  - thermal load increase
  - poor heat pumping effectiveness
  - resistor heat increase
  - power-supply electrical load change
  - insufficient heat-model confidence
- [ ] Do not let heat-flow or electrical-output estimates directly trigger PID writes. They only affect recommendation text, confidence, and diagnostic classification.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/advisor_service.go mecom/advisor_service_test.go mecom/behavior_scorer.go mecom/behavior_scorer_test.go mecom/pid_advisor.go mecom/pid_advisor_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestAdvisorServicePublishesDerivedPeltierSamples|TestAdvisorServicePublishesResistorModeSamples|TestAdvisorServicePublishesPowerSupplyModeSamples|TestBehaviorScorerUsesHeatFlow|TestPIDAdvisorIncludesHeatFlow'
```

---

## Phase 4: Conservative PID Advisor

### Task 8: Implement Passive Model Identification with Confidence Gates

Files:

- `mecom/fopdt_test.go`
- `mecom/fopdt.go`

Test first:

- [ ] Create `TestFOPDTFitRecoversSyntheticResponse`.
- [ ] Generate a first-order-plus-dead-time response.
- [ ] Assert process gain, time constant, and dead time are close to expected values.
- [ ] Create `TestFOPDTRejectsLowExcitationWindow`.
- [ ] Feed a flat response.
- [ ] Assert confidence is below recommendation threshold.
- [ ] Create `TestFOPDTSeparatesHeatingAndCoolingModels`.
- [ ] Fit separate windows.
- [ ] Assert separate situation model keys.

Implementation:

- [ ] Add:

```go
type FOPDTModel struct {
    SituationID       string
    Direction         TransitionDirection
    ProcessGain       float64
    TimeConstant      time.Duration
    DeadTime          time.Duration
    FitR2             float64
    Excitation        float64
    SampleCount       int
    Confidence        float64
}
```

- [ ] Fit only from windows with enough excitation and known actuator or setpoint change.
- [ ] Mark closed-loop passive fits as advisory confidence only.
- [ ] Do not recommend parameter writes when confidence is below `0.75`.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/fopdt.go mecom/fopdt_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestFOPDT'
```

### Task 9: Implement Proven PID Recommendation Methods

Files:

- `mecom/pid_tuning_test.go`
- `mecom/pid_tuning.go`

Test first:

- [ ] Create `TestSIMCTuningProducesConservativePIRecommendation`.
- [ ] Given a stable FOPDT model, assert proportional gain, integral time, and derivative time are finite and bounded.
- [ ] Create `TestRelayFeedbackComputesUltimateGain`.
- [ ] Given relay amplitude `d`, output oscillation amplitude `a`, and period `Pu`, assert `Ku = 4*d/(pi*a)`.
- [ ] Create `TestPIDRecommendationRejectedWhenBoundsExceeded`.
- [ ] Given unsafe output, assert recommendation is rejected.

Implementation:

- [ ] Add:

```go
type PIDMethod string

const (
    PIDMethodSIMC          PIDMethod = "simc"
    PIDMethodRelayFeedback PIDMethod = "relay_feedback"
)

type PIDRecommendation struct {
    ControllerAddress int
    SituationID       string
    Method            PIDMethod
    Kp                float64
    TiSeconds         float64
    TdSeconds         float64
    Confidence        float64
    Reason            string
    ApplyAllowed      bool
}
```

- [ ] Implement SIMC/lambda-style conservative PI/PID recommendations from accepted FOPDT models.
- [ ] Implement relay feedback calculations for operator-triggered characterization sessions.
- [ ] Do not use aggressive ultimate-gain tuning as the automatic default. Report it as characterization evidence.
- [ ] Clamp all recommendations through `PIDBounds`.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/pid_tuning.go mecom/pid_tuning_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestSIMC|TestRelayFeedback|TestPIDRecommendation'
```

### Task 10: Add Safety-Gated PID Advisor State Machine

Files:

- `mecom/pid_advisor_test.go`
- `mecom/pid_advisor.go`

Test first:

- [ ] Create `TestPIDAdvisorDefaultIsObserveOnly`.
- [ ] Feed good models and scores.
- [ ] Assert advisor emits recommendations but no write intents.
- [ ] Create `TestPIDAdvisorRequiresOperatorApprovalForWriteIntent`.
- [ ] Enable bounded-write mode.
- [ ] Assert write intent requires explicit approval token and rollback snapshot.
- [ ] Create `TestPIDAdvisorRollsBackOnWorseBehavior`.
- [ ] Feed post-change behavior with worse overshoot or oscillation.
- [ ] Assert rollback intent is emitted.

Implementation:

- [ ] Add safety levels:

```go
type PIDAdvisorMode string

const (
    PIDObserveOnly        PIDAdvisorMode = "observe_only"
    PIDRecommendOnly      PIDAdvisorMode = "recommend_only"
    PIDSelfTuneAssist     PIDAdvisorMode = "self_tune_assist"
    PIDBoundedWriteAssist PIDAdvisorMode = "bounded_write_assist"
)
```

- [ ] Add `PIDAdvisor` with:
  - current mode
  - latest behavior scores
  - accepted models
  - pending recommendations
  - rollback snapshots
  - write-intent history
- [ ] Add `TuneGate` interface:

```go
type TuneGate interface {
    ApprovePIDWrite(controllerAddress int, situationID string, recommendationID string) bool
}
```

- [ ] No direct device writes in `PIDAdvisor`. It emits explicit write intents that a command layer can execute later.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/pid_advisor.go mecom/pid_advisor_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestPIDAdvisor'
```

---

## Phase 5: PiXtend and Transport Reliability

### Task 11: Make CAN Adapter Capabilities Explicit

Files:

- `canadapter/catalogue_test.go`
- `canadapter/catalogue.go`
- `docs/can_adapter_support.md`

Test first:

- [ ] Create `TestCANAdapterCatalogueMarksFirstClassAdapters`.
- [ ] Assert SocketCAN/PiXtend and Kvaser USB are first-class.
- [ ] Assert Kvaser DIN-rail Ethernet adapters are first-class.
- [ ] Create `TestCANAdapterCatalogueMarksUnprovenAdapters`.
- [ ] Assert PEAK, CANable/candleLight, Lawicel/SLCAN, and SocketCAN-backed USB adapters can be listed as easy-but-unproven options where driver support is known.

Implementation:

- [ ] Add:

```go
type AdapterSupportLevel string

const (
    AdapterFirstClass      AdapterSupportLevel = "first_class"
    AdapterEasyUnproven    AdapterSupportLevel = "easy_unproven"
    AdapterKnownUnsupported AdapterSupportLevel = "known_unsupported"
)
```

- [ ] Keep SocketCAN path as the PiXtend baseline.
- [ ] Add Kvaser USB and Kvaser DIN-rail Ethernet as first-class catalogue entries.
- [ ] Add non-Kvaser adapters only as catalogue entries with explicit support level; do not claim they are proven.
- [ ] Document required driver path, expected interface name, and time-stamping limitations.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w canadapter/catalogue.go canadapter/catalogue_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./canadapter
```

### Task 12: Add PiXtend Edge Runtime Profile

Files:

- `mecom/edge_profile_test.go`
- `mecom/edge_profile.go`
- `examples/pixtend-four-controller-profile.json`

Test first:

- [ ] Create `TestPiXtendProfileUsesInMemoryOnlyLogging`.
- [ ] Assert file logging is disabled.
- [ ] Create `TestPiXtendProfileDegradesLowerPriorityPollingUnderCongestion`.
- [ ] Assert congestion response keeps ring-buffer polling active and reduces normal/low-priority bulk reads.
- [ ] Create `TestPiXtendProfileScalesToSixteenControllers`.
- [ ] Assert the generated schedule has bounded loop work and no per-controller goroutine explosion.

Implementation:

- [ ] Add:

```go
type EdgeRuntimeProfile struct {
    Name                 string
    MaxControllers       int
    InMemoryWindows      int
    FileLoggingEnabled   bool
    PreferRingBuffer     bool
    BulkRoundRobin       bool
    OffloadReductionHint bool
}
```

- [ ] Add `PiXtendFourControllerProfile()` and `PiXtendSixteenControllerProfile()`.
- [ ] Ensure profiles feed `PollingPolicy`, `ObserverConfig`, and `AdvisorService`.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/edge_profile.go mecom/edge_profile_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestPiXtendProfile'
```

---

## Phase 6: Graph Wall and Operator Surfaces

### Task 13: Populate Graph-Wall Defaults for Four Controllers

Files:

- `utility/tiles_test.go`
- `utility/tiles.go`
- `utility/server.go`
- `utility/server_test.go`

Test first:

- [ ] Create `TestGraphWallDefaultsForFourControllers`.
- [ ] Assert tiles include object temperature, sink temperature, target temperature, cascade temperature, output voltage, output current, output power, device status, error status, and PID advisor status for controllers `31,32,33,34`.
- [ ] Create `TestGraphWallDefaultsAvoidFlashLogging`.
- [ ] Assert graph-wall data source is live/in-memory unless explicitly configured otherwise.

Implementation:

- [ ] Add utility function:

```go
func DefaultTECGraphWallTiles(catalogue mecom.TECCatalogue) []Tile
```

- [ ] Group tiles by controller and by signal kind.
- [ ] Mark high-priority ring-buffer signals as high-refresh charts.
- [ ] Add lower-priority values as slower status panels.
- [ ] Add advisor panels:
  - transition state
  - latest behavior score
  - current recommendation
  - confidence
  - safety mode

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w utility/tiles.go utility/tiles_test.go utility/server.go utility/server_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./utility
```

### Task 14: Add CLI for Advisory Runtime

Files:

- `cmd/tecadvisor/main.go`
- `cmd/tecadvisor/main_test.go`
- `README.md`

Test first:

- [ ] Create `TestTECADVISORConfigParse`.
- [ ] Parse:

```bash
--transport socketcan --if can0 --controllers 31,32,33,34 --profile pixtend-four-controller --mode observe-only
```

- [ ] Assert mode defaults to observe-only.
- [ ] Assert file logging defaults to disabled.
- [ ] Assert controller addresses are parsed exactly.

Implementation:

- [ ] Add `cmd/tecadvisor`.
- [ ] Supported flags:
  - `--transport socketcan|kvaser-usb|kvaser-dinrail`
  - `--if can0`
  - `--controllers 31,32,33,34`
  - `--profile pixtend-four-controller`
  - `--mode observe-only|recommend-only|self-tune-assist|bounded-write-assist`
  - `--listen :8098`
  - `--bulk-rate-hz 2`
  - `--ring-period-ms 100`
  - `--no-file-log`
- [ ] CLI starts polling, observer, reducer, advisor, and utility HTTP endpoints.
- [ ] CLI prints concise health every 30 seconds, not per-frame spam.
- [ ] CLI exits cleanly on SIGINT/SIGTERM.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w cmd/tecadvisor/main.go cmd/tecadvisor/main_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./cmd/tecadvisor
```

---

## Phase 7: LUT and TMTC Sequencer Compatibility

### Task 15: Add Four-Cycle Sample Program

Files:

- `mecomautomation/sample_program_test.go`
- `mecomautomation/sample_program.go`
- `docs/lut_tmtc_automation.md`

Test first:

- [ ] Create `TestFourCycleSampleProgramBuilds`.
- [ ] Assert the program contains four cycles:
  - idle observation
  - controlled setpoint step
  - hold and observe
  - return-to-baseline
- [ ] Assert each cycle emits TMTC-compatible metadata:
  - cycle ID
  - controller addresses
  - setpoint command intent
  - expected observation window
  - abort condition
- [ ] Create `TestFourCycleSampleProgramDoesNotEnablePIDWrites`.
- [ ] Assert PID write mode is disabled.

Implementation:

- [ ] Add:

```go
func FourCycleTECSampleProgram(controllerAddresses []int) Program
```

- [ ] Reuse existing `mecomautomation` and `sequencer` types.
- [ ] Include explicit abort conditions:
  - controller error state
  - temperature limit exceeded
  - communication loss
  - output saturation too long
- [ ] Keep this sample small and deterministic.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecomautomation/sample_program.go mecomautomation/sample_program_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecomautomation ./sequencer
```

---

## Phase 8: End-to-End Verification Without Unsafe Device Writes

### Task 16: Add Simulated Multi-Controller Integration Test

Files:

- `mecom/integration_advisor_test.go`

Test first:

- [ ] Create `TestAdvisorPipelineFourControllersSimulated`.
- [ ] Simulate four controllers with high-priority ring-buffer samples and bulk queue samples.
- [ ] Assert:
  - polling policy uses ring-buffer for high-priority signals
  - observer accepts samples
  - reducer produces consumer-rate values
  - transition detector finds setpoint steps
  - behavior scorer produces scores
  - PID advisor produces recommendations
  - no write intent is emitted in observe-only mode

Implementation:

- [ ] Use fake `ReadClient` and deterministic sample generator.
- [ ] Keep the test under two seconds.
- [ ] Avoid live CAN dependencies.

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom/integration_advisor_test.go
/home/svc_pmg_testbed_b/.local/go/bin/go test ./mecom -run 'TestAdvisorPipelineFourControllersSimulated'
```

### Task 17: Add Hardware Smoke Commands

Files:

- `docs/tec_live_observation_pid_advisor.md`
- `README.md`

Implementation:

- [ ] Document safe live smoke tests:

```bash
cd /home/pi/meerstetter-go
./tecadvisor --transport socketcan --if can0 --controllers 31,32,33,34 --profile pixtend-four-controller --mode observe-only --listen :8098
```

- [ ] Document status checks:

```bash
curl -fsS http://127.0.0.1:8098/health
curl -fsS http://127.0.0.1:8098/api/tec/catalogue
curl -fsS http://127.0.0.1:8098/api/tec/advisor
curl -fsS http://127.0.0.1:8098/api/tec/graph-wall
```

- [ ] Document failure interpretations:
  - no CAN frames: wiring, termination, bitrate, interface down
  - CAN frames but no MeCom replies: address scan or controller mode
  - ring-buffer empty but bulk works: ring-buffer not enabled or unsupported parameter set
  - recommendations absent: insufficient excitation or low model confidence
  - congestion: lower-priority polling degraded by design

Verify:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
rg -n "observe-only|pixtend-four-controller|graph-wall|ring-buffer" docs README.md
```

---

## Phase 9: Final Repo Verification

Run after all tasks:

```bash
cd /home/svc_pmg_testbed_b/meerstetter-go
/home/svc_pmg_testbed_b/.local/go/bin/gofmt -w mecom canadapter utility mecomautomation cmd/tecadvisor
/home/svc_pmg_testbed_b/.local/go/bin/go test ./...
git status --short
```

Acceptance criteria:

- [ ] Existing tests pass.
- [ ] New unit tests pass.
- [ ] No default path writes PID parameters to a TEC controller.
- [ ] No default path writes high-rate logs to SD card or USB storage.
- [ ] Four-controller PiXtend profile exists and is the documented default.
- [ ] Sixteen-controller schedule is bounded and degrades lower-priority reads under congestion.
- [ ] Graph wall exposes high-priority controller values and advisor state.
- [ ] Graph wall exposes per-channel and global Peltier heat-flow estimates with confidence.
- [ ] Graph wall and advisor respect resistor, power-supply, Peltier-driver, and unknown channel modes.
- [ ] PID advisor considers heat-flow evidence when classifying transition behavior, but does not use estimates as an automatic write trigger.
- [ ] Kvaser USB and Kvaser DIN-rail Ethernet are first-class adapter options.
- [ ] Other common adapters are clearly marked as easy-but-unproven, not proven.
- [ ] LUT/TMTC sample program exists and is safe by default.
- [ ] Documentation states that PID optimization begins as observation and recommendation, with writes gated by operator approval.

## Execution Notes for Workers

- Keep changes feature-preserving and conservative.
- Do not delete or normalize existing untracked work.
- Do not start long-running live probes during implementation.
- Do not run live device writes from tests.
- If a parameter ID is not known, represent it as discoverable and keep it out of active polling until discovery resolves it.
- Prefer one package-level change at a time with tests before implementation.
- If committing, stage only the files touched by the current task and leave unrelated dirty work untouched.
