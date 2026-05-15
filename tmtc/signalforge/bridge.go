// Package signalforge adapts Meerstetter TM/TC values to and from
// SignalForge's generic contract types.
//
// The bridge is intentionally explicit: the core tmtc package owns the
// Meerstetter public API, while this package is the boundary where generic
// SignalForge contracts are imported.
package signalforge

import (
	"github.com/egidinas/meerstetter-go/tmtc"
	"github.com/egidinas/signalforge/contracts"
)

func ToContractTelemetry(tm tmtc.Telemetry) contracts.Telemetry {
	return contracts.Telemetry{
		ID:        tm.ID,
		TargetID:  tm.TargetID,
		SessionID: tm.SessionID,
		Time:      tm.Time,
		Name:      tm.Name,
		Value:     tm.Value,
		Unit:      tm.Unit,
		Quality:   tm.Quality,
		Raw:       cloneBytes(tm.Raw),
		Metadata:  cloneStringMap(tm.Metadata),
	}
}

func FromContractTelemetry(tm contracts.Telemetry) tmtc.Telemetry {
	return tmtc.Telemetry{
		ID:        tm.ID,
		TargetID:  tm.TargetID,
		SessionID: tm.SessionID,
		Time:      tm.Time,
		Name:      tm.Name,
		Value:     tm.Value,
		Unit:      tm.Unit,
		Quality:   tm.Quality,
		Raw:       cloneBytes(tm.Raw),
		Metadata:  cloneStringMap(tm.Metadata),
	}
}

func ToContractTelecommand(tc tmtc.Telecommand) contracts.Telecommand {
	return contracts.Telecommand{
		ID:             tc.ID,
		TargetID:       tc.TargetID,
		SessionID:      tc.SessionID,
		Time:           tc.Time,
		Name:           tc.Name,
		Payload:        cloneBytes(tc.Payload),
		Arguments:      cloneAnyMap(tc.Arguments),
		IdempotencyKey: tc.IdempotencyKey,
		RequiresAck:    tc.RequiresAck,
		Metadata:       cloneStringMap(tc.Metadata),
	}
}

func FromContractTelecommand(tc contracts.Telecommand) tmtc.Telecommand {
	return tmtc.Telecommand{
		ID:             tc.ID,
		TargetID:       tc.TargetID,
		SessionID:      tc.SessionID,
		Time:           tc.Time,
		Name:           tc.Name,
		Payload:        cloneBytes(tc.Payload),
		Arguments:      cloneAnyMap(tc.Arguments),
		IdempotencyKey: tc.IdempotencyKey,
		RequiresAck:    tc.RequiresAck,
		Metadata:       cloneStringMap(tc.Metadata),
	}
}

func ToContractCommandEvent(ev tmtc.CommandEvent) contracts.CommandEvent {
	return contracts.CommandEvent{
		ID:             ev.ID,
		CommandID:      ev.CommandID,
		SessionID:      ev.SessionID,
		Time:           ev.Time,
		Status:         contracts.CommandStatus(ev.Status),
		Transport:      ev.Transport,
		IdempotencyKey: ev.IdempotencyKey,
		Result:         ev.Result,
		Error:          ev.Error,
		Metadata:       cloneStringMap(ev.Metadata),
	}
}

func FromContractCommandEvent(ev contracts.CommandEvent) tmtc.CommandEvent {
	return tmtc.CommandEvent{
		ID:             ev.ID,
		CommandID:      ev.CommandID,
		SessionID:      ev.SessionID,
		Time:           ev.Time,
		Status:         tmtc.CommandStatus(ev.Status),
		Transport:      ev.Transport,
		IdempotencyKey: ev.IdempotencyKey,
		Result:         ev.Result,
		Error:          ev.Error,
		Metadata:       cloneStringMap(ev.Metadata),
	}
}

func cloneBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	return append([]byte(nil), in...)
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
