package mecom

import (
	"context"
	"errors"
	"math"
	"time"
)

const (
	RingCaptureLimit           = 16
	DefaultRingCaptureID       = 1
	DefaultRingInhibitTime10us = 1000
	DefaultRingReadMaxBytes    = 4096
	MaxRingReadMaxBytes        = 16384
)

const (
	ReadoutCRTVStreamRingBuffer = "mecom_crtvstream_ring_buffer"
	ReadoutVXRoundRobinQueue    = "mecom_vx_round_robin_queue"
	ReadoutDerivedChannelModel  = "mecom_derived_channel_model"
)

// ReadClient is the minimum MeCom primitive set needed by the reusable readout
// scheduler. Client satisfies this interface, but tests and CAN/serial
// adapters can provide narrower implementations.
type ReadClient interface {
	ReadFloat32(context.Context, int, int) (float64, error)
	ReadInt32(context.Context, int, int) (int32, error)
	// ReadBulk reads multiple parameters. If an individual parameter cannot be
	// retrieved (due to timeout, unsupported parameter on device, etc.), its slot
	// in the returned slice is set to math.NaN() and processing continues rather
	// than returning a top-level error. A non-nil error is only returned for general
	// transport or validation failures.
	ReadBulk(context.Context, []Parameter) ([]float64, error)
	ConfigureRingCapture(context.Context, uint16, []RingCaptureParameter) error
	TriggerRingSync(context.Context) error
	ReadRingPointer(context.Context) (uint32, error)
	ReadRingChunk(context.Context, uint32, uint16) (RingReadResponse, error)
}

func readBulkShouldReturnError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, ErrTimeout) || errors.Is(err, ErrUnknownParameter) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

// RingReadoutCapability lets transport adapters report whether the controller
// CRTVStream ring-buffer primitive is actually available on that transport.
// Older test clients and serial/TCP clients default to true for compatibility.
type RingReadoutCapability interface {
	SupportsRingReadout() bool
}

// ReadoutParameter describes one signal in a reusable polling plan.
type ReadoutParameter struct {
	Parameter    Parameter
	Sensor       string
	HighPriority bool
}

// ReadoutConfig controls queueing, CRTVStream capture, and congestion behavior.
type ReadoutConfig struct {
	Parameters              []ReadoutParameter
	BulkChunk               int
	RingCaptureLimit        int
	RingCaptureID           uint16
	RingInhibitTime10us     uint16
	DefaultRingReadMaxBytes uint16
	MaxRingReadMaxBytes     uint16
	RequestTimeout          time.Duration
	Derived                 *DerivedReadoutConfig
}

// ReadoutValue is one consumer-facing sample from either the ring buffer or the
// background round-robin queue.
type ReadoutValue struct {
	Parameter  Parameter
	Sensor     string
	Value      float64
	ObservedAt time.Time
	Readout    string
	Reduction  *ReducedRingSample
}

// ReadoutBatch groups all samples and soft errors from one poll iteration.
type ReadoutBatch struct {
	Values           []ReadoutValue
	RingValues       []ReadoutValue
	BackgroundValues []ReadoutValue
	DerivedValues    []ReadoutValue
	DerivedEstimates []PeltierChannelEstimate
	Errors           []error
}

type queuedReadoutParameter struct {
	spec        ReadoutParameter
	configIndex int
}

// Readout keeps long-lived cursor and queue state for one MeCom controller.
type Readout struct {
	bulkChunk           int
	ringCaptureID       uint16
	ringInhibitTime10us uint16
	ringMaxBytes        uint16
	ringMaxLimit        uint16
	ringMinBytes        uint16
	requestTimeout      time.Duration

	ringItems      []queuedReadoutParameter
	ringConfig     []RingCaptureParameter
	ringConfigured bool
	ringCursor     uint32
	ringTail       []byte

	background      *PollQueue
	backgroundItems map[string]ReadoutParameter
	derived         *derivedReadout
}

func NewReadout(cfg ReadoutConfig) *Readout {
	bulkChunk := cfg.BulkChunk
	if bulkChunk <= 0 {
		bulkChunk = 8
	}
	captureLimit := cfg.RingCaptureLimit
	if captureLimit <= 0 || captureLimit > RingCaptureLimit {
		captureLimit = RingCaptureLimit
	}
	captureID := cfg.RingCaptureID
	if captureID == 0 {
		captureID = DefaultRingCaptureID
	}
	inhibit := cfg.RingInhibitTime10us
	if inhibit == 0 {
		inhibit = DefaultRingInhibitTime10us
	}
	ringMinBytes := cfg.DefaultRingReadMaxBytes
	if ringMinBytes == 0 {
		ringMinBytes = DefaultRingReadMaxBytes
	}
	ringMaxLimit := cfg.MaxRingReadMaxBytes
	if ringMaxLimit == 0 {
		ringMaxLimit = MaxRingReadMaxBytes
	}
	if ringMaxLimit < ringMinBytes {
		ringMaxLimit = ringMinBytes
	}
	timeout := cfg.RequestTimeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	r := &Readout{
		bulkChunk:           bulkChunk,
		ringCaptureID:       captureID,
		ringInhibitTime10us: inhibit,
		ringMaxBytes:        ringMinBytes,
		ringMaxLimit:        ringMaxLimit,
		ringMinBytes:        ringMinBytes,
		requestTimeout:      timeout,
		backgroundItems:     map[string]ReadoutParameter{},
	}
	if cfg.Derived != nil {
		r.derived = newDerivedReadout(*cfg.Derived)
	}

	background := make([]Parameter, 0, len(cfg.Parameters))
	for _, spec := range cfg.Parameters {
		if spec.Sensor == "" {
			spec.Sensor = spec.Parameter.Name
		}
		if spec.HighPriority && len(r.ringConfig) < captureLimit {
			r.ringItems = append(r.ringItems, queuedReadoutParameter{
				spec:        spec,
				configIndex: len(r.ringConfig),
			})
			r.ringConfig = append(r.ringConfig, RingCaptureParameter{
				Parameter:       spec.Parameter,
				InhibitTime10us: r.ringInhibitTime10us,
			})
			continue
		}
		background = append(background, spec.Parameter)
		r.backgroundItems[ParameterKey(spec.Parameter)] = spec
	}
	r.background = NewPollQueue(background)
	return r
}

func (r *Readout) EnqueueFront(spec ReadoutParameter) {
	if spec.Sensor == "" {
		spec.Sensor = spec.Parameter.Name
	}
	if r.background == nil {
		r.background = NewPollQueue(nil)
	}
	r.backgroundItems[ParameterKey(spec.Parameter)] = spec
	r.background.EnqueueFront(spec.Parameter)
}

func (r *Readout) RingReadMaxBytes() uint16 {
	if r == nil {
		return 0
	}
	return r.ringMaxBytes
}

func (r *Readout) Poll(ctx context.Context, client ReadClient, observedAt time.Time) ReadoutBatch {
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	var batch ReadoutBatch
	r.pollRing(ctx, client, observedAt, &batch)
	r.pollBackground(ctx, client, observedAt, &batch)
	if r.derived != nil {
		r.derived.Append(observedAt, &batch)
	}
	return batch
}

func (r *Readout) pollRing(ctx context.Context, client ReadClient, observedAt time.Time, batch *ReadoutBatch) {
	if len(r.ringConfig) == 0 {
		return
	}
	if !supportsRingReadout(client) {
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, r.requestTimeout)
	defer cancel()
	if !r.ringConfigured {
		if err := client.ConfigureRingCapture(reqCtx, r.ringCaptureID, r.ringConfig); err != nil {
			batch.Errors = append(batch.Errors, err)
			return
		}
		if err := client.TriggerRingSync(reqCtx); err != nil {
			batch.Errors = append(batch.Errors, err)
		}
		pointer, err := client.ReadRingPointer(reqCtx)
		if err != nil {
			batch.Errors = append(batch.Errors, err)
			return
		}
		r.ringCursor = pointer
		r.ringConfigured = true
		return
	}

	resp, err := client.ReadRingChunk(reqCtx, r.ringCursor, r.ringMaxBytes)
	if err != nil {
		batch.Errors = append(batch.Errors, err)
		r.ringConfigured = false
		return
	}
	if resp.Status == RingStatusOverlap {
		if pointer, err := client.ReadRingPointer(reqCtx); err == nil {
			r.ringCursor = pointer
		} else {
			batch.Errors = append(batch.Errors, err)
		}
		r.ringTail = nil
		r.ringMaxBytes = r.ringMaxLimit
		return
	}
	r.ringCursor += uint32(resp.BytesAdded)
	r.tuneRingReadWindow(resp)

	data := append(append([]byte(nil), r.ringTail...), resp.Data...)
	frames, tail, err := ParseRingFrames(data, r.ringConfig)
	if err != nil {
		batch.Errors = append(batch.Errors, err)
		r.ringTail = nil
		return
	}
	r.ringTail = tail

	configIndices := make([]int, 0, len(r.ringItems))
	for _, item := range r.ringItems {
		configIndices = append(configIndices, item.configIndex)
	}
	reducedByIndex := ReduceRingSamplesForIndices(frames, configIndices)
	for _, item := range r.ringItems {
		reduced, ok := reducedByIndex[item.configIndex]
		if !ok {
			continue
		}
		value := ReadoutValue{
			Parameter:  item.spec.Parameter,
			Sensor:     item.spec.Sensor,
			Value:      reduced.Mean,
			ObservedAt: observedAt,
			Readout:    ReadoutCRTVStreamRingBuffer,
			Reduction:  &reduced,
		}
		batch.RingValues = append(batch.RingValues, value)
		batch.Values = append(batch.Values, value)
	}
}

func supportsRingReadout(client ReadClient) bool {
	capable, ok := client.(RingReadoutCapability)
	return !ok || capable.SupportsRingReadout()
}

func (r *Readout) tuneRingReadWindow(resp RingReadResponse) {
	if resp.Status == RingStatusHasMoreData || int(resp.BytesAdded) >= int(r.ringMaxBytes)*3/4 {
		if r.ringMaxBytes < r.ringMaxLimit {
			r.ringMaxBytes *= 2
			if r.ringMaxBytes > r.ringMaxLimit {
				r.ringMaxBytes = r.ringMaxLimit
			}
		}
		return
	}
	if resp.Status == RingStatusAllDataRead && resp.BytesAdded < r.ringMaxBytes/4 && r.ringMaxBytes > r.ringMinBytes {
		r.ringMaxBytes /= 2
		if r.ringMaxBytes < r.ringMinBytes {
			r.ringMaxBytes = r.ringMinBytes
		}
	}
}

func (r *Readout) pollBackground(ctx context.Context, client ReadClient, observedAt time.Time, batch *ReadoutBatch) {
	if r.background == nil {
		return
	}
	params := r.background.NextChunk(r.bulkChunk)
	if len(params) == 0 {
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, r.requestTimeout)
	defer cancel()
	values, err := client.ReadBulk(reqCtx, params)
	if err != nil {
		batch.Errors = append(batch.Errors, err)
		r.background.RecordBulk(params, nil, observedAt, err)
		return
	}
	r.background.RecordBulk(params, values, observedAt, nil)
	for i, param := range params {
		spec, ok := r.backgroundItems[ParameterKey(param)]
		if !ok {
			continue
		}
		sample := math.NaN()
		if i < len(values) {
			sample = values[i]
		}
		value := ReadoutValue{
			Parameter:  spec.Parameter,
			Sensor:     spec.Sensor,
			Value:      sample,
			ObservedAt: observedAt,
			Readout:    ReadoutVXRoundRobinQueue,
		}
		batch.BackgroundValues = append(batch.BackgroundValues, value)
		batch.Values = append(batch.Values, value)
	}
}
