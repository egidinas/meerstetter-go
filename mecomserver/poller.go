package mecomserver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

// RouterPollScheduler runs one readout plan per logical device through a
// shared RouterRuntime.
type RouterPollScheduler struct {
	rt       *RouterRuntime
	interval time.Duration
	mu       sync.Mutex
	devices  map[byte]*routerPollDevice
}

type RouterPollSchedulerConfig struct {
	Router   *RouterRuntime
	Interval time.Duration
	Devices  []RouterPollDeviceConfig
}

type RouterPollDeviceConfig struct {
	Address          byte
	Readout          mecom.ReadoutConfig
	SupportsRingRead *bool
	RoutePolicy      RouteSelectionPolicy
	RequestTimeout   time.Duration
}

type routerPollDevice struct {
	cfg         RouterPollDeviceConfig
	readout     *mecom.Readout
	subscribers map[chan mecom.ReadoutBatch]struct{}
}

func NewRouterPollScheduler(cfg RouterPollSchedulerConfig) *RouterPollScheduler {
	s := &RouterPollScheduler{
		rt:       cfg.Router,
		interval: cfg.Interval,
		devices:  make(map[byte]*routerPollDevice, len(cfg.Devices)),
	}
	for _, dev := range cfg.Devices {
		dev.Readout = routerReadoutConfig(dev)
		s.devices[dev.Address] = &routerPollDevice{
			cfg:         dev,
			readout:     mecom.NewReadout(dev.Readout),
			subscribers: map[chan mecom.ReadoutBatch]struct{}{},
		}
	}
	return s
}

func routerReadoutConfig(dev RouterPollDeviceConfig) mecom.ReadoutConfig {
	cfg := dev.Readout
	if supportsRingReadout(dev.SupportsRingRead) {
		return cfg
	}
	if len(cfg.Parameters) == 0 {
		return cfg
	}
	params := append([]mecom.ReadoutParameter(nil), cfg.Parameters...)
	for i := range params {
		params[i].HighPriority = false
	}
	cfg.Parameters = params
	return cfg
}

func (s *RouterPollScheduler) EnqueueFront(addr byte, spec mecom.ReadoutParameter) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dev := s.devices[addr]
	if dev == nil {
		dev = &routerPollDevice{
			cfg:         RouterPollDeviceConfig{Address: addr},
			readout:     mecom.NewReadout(mecom.ReadoutConfig{}),
			subscribers: map[chan mecom.ReadoutBatch]struct{}{},
		}
		s.devices[addr] = dev
	}
	if dev.readout != nil {
		dev.readout.EnqueueFront(spec)
	}
}

func (s *RouterPollScheduler) Subscribe(addr byte, buffer int) (<-chan mecom.ReadoutBatch, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan mecom.ReadoutBatch, buffer)
	if s == nil {
		return ch, func() { close(ch) }
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dev := s.devices[addr]
	if dev == nil {
		dev = &routerPollDevice{
			cfg:         RouterPollDeviceConfig{Address: addr},
			readout:     mecom.NewReadout(mecom.ReadoutConfig{}),
			subscribers: map[chan mecom.ReadoutBatch]struct{}{},
		}
		s.devices[addr] = dev
	}
	dev.subscribers[ch] = struct{}{}
	cancel := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if dev := s.devices[addr]; dev != nil {
			delete(dev.subscribers, ch)
		}
		close(ch)
	}
	return ch, cancel
}

func (s *RouterPollScheduler) PollOnce(ctx context.Context) map[byte]mecom.ReadoutBatch {
	out := map[byte]mecom.ReadoutBatch{}
	if s == nil {
		return out
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for addr, dev := range s.devices {
		if dev == nil || dev.readout == nil {
			out[addr] = mecom.ReadoutBatch{Errors: []error{fmt.Errorf("mecomserver: device 0x%02X missing readout", addr)}}
			continue
		}
		if s.rt == nil {
			out[addr] = mecom.ReadoutBatch{Errors: []error{fmt.Errorf("mecomserver: router runtime required for device 0x%02X", addr)}}
			continue
		}
		client := s.rt.NewReadClient(RouterDeviceClientConfig{
			Address:             addr,
			RequestTimeout:      dev.cfg.RequestTimeout,
			RouteSelection:      dev.cfg.RoutePolicy,
			SupportsRingReadout: supportsRingReadout(dev.cfg.SupportsRingRead),
		})
		batch := dev.readout.Poll(ctx, client, time.Now())
		out[addr] = batch
		for sub := range dev.subscribers {
			publishReadoutBatch(sub, batch)
		}
	}
	return out
}

func (s *RouterPollScheduler) Run(ctx context.Context) {
	if s == nil {
		if ctx != nil {
			<-ctx.Done()
		}
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	interval := s.interval
	if interval <= 0 {
		interval = time.Second
	}
	s.PollOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.PollOnce(ctx)
		}
	}
}

func supportsRingReadout(v *bool) bool {
	return v != nil && *v
}

func publishReadoutBatch(ch chan mecom.ReadoutBatch, batch mecom.ReadoutBatch) {
	select {
	case ch <- batch:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- batch:
	default:
	}
}
