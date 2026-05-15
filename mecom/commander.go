package mecom

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/egidinas/meerstetter-go/tmtc"
)

// Commander adapts any WriteClient into a tmtc.Commander. It is the bridge
// between high-level sequencer.Script steps and the concrete read/write
// surface of a Meerstetter device, regardless of transport.
//
// Recognised telecommand names:
//   - "write_float32" / "set_float32"  args: param (int), instance (int, default 1), value (float)
//   - "write_int32"   / "set_int32"    args: param (int), instance (int, default 1), value (int)
//   - "reset"                          (requires ControlClient)
//   - "save_to_flash" / "save"         (requires ControlClient)
//
// Unrecognised names return CommandRejected with the offending name.
type Commander struct {
	Writer  WriteClient
	Control ControlClient
	Timeout time.Duration
}

// NewCommander wraps a WriteClient. If the writer is also a ControlClient
// (the ASCII Client is), Reset/SaveToFlash are accepted; otherwise those
// commands are rejected.
func NewCommander(writer WriteClient, timeout time.Duration) *Commander {
	c := &Commander{Writer: writer, Timeout: timeout}
	if ctrl, ok := writer.(ControlClient); ok {
		c.Control = ctrl
	}
	return c
}

// Send dispatches a Telecommand to the underlying client.
func (c *Commander) Send(tc tmtc.Telecommand) (tmtc.CommandEvent, error) {
	if c == nil || c.Writer == nil {
		return tmtc.CommandEvent{
			CommandID: tc.ID,
			Time:      time.Now(),
			Status:    tmtc.CommandFailed,
			Error:     "mecom.Commander: no writer configured",
		}, fmt.Errorf("mecom.Commander: no writer configured")
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ev := tmtc.CommandEvent{
		CommandID:      tc.ID,
		SessionID:      tc.SessionID,
		Time:           time.Now(),
		IdempotencyKey: tc.IdempotencyKey,
	}
	if err := c.dispatch(ctx, tc); err != nil {
		ev.Status = tmtc.CommandFailed
		ev.Error = err.Error()
		return ev, err
	}
	ev.Status = tmtc.CommandCompleted
	return ev, nil
}

func (c *Commander) dispatch(ctx context.Context, tc tmtc.Telecommand) error {
	switch tc.Name {
	case "write_float32", "set_float32":
		paramID, instance, err := paramAndInstance(tc.Arguments)
		if err != nil {
			return err
		}
		value, err := floatArg(tc.Arguments, "value")
		if err != nil {
			return err
		}
		return c.Writer.WriteFloat32(ctx, paramID, instance, float32(value))

	case "write_int32", "set_int32":
		paramID, instance, err := paramAndInstance(tc.Arguments)
		if err != nil {
			return err
		}
		value, err := intArg(tc.Arguments, "value")
		if err != nil {
			return err
		}
		return c.Writer.WriteInt32(ctx, paramID, instance, int32(value))

	case "reset":
		if c.Control == nil {
			return fmt.Errorf("reset: underlying client does not support control actions")
		}
		return c.Control.Reset(ctx)

	case "save_to_flash", "save":
		if c.Control == nil {
			return fmt.Errorf("save: underlying client does not support control actions")
		}
		return c.Control.SaveToFlash(ctx)

	default:
		return fmt.Errorf("unknown command %q", tc.Name)
	}
}

func paramAndInstance(args map[string]any) (int, int, error) {
	paramID, err := intArg(args, "param")
	if err != nil {
		return 0, 0, err
	}
	instance := 1
	if _, ok := args["instance"]; ok {
		v, err := intArg(args, "instance")
		if err != nil {
			return 0, 0, err
		}
		instance = v
	}
	return paramID, instance, nil
}

func intArg(args map[string]any, key string) (int, error) {
	raw, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("missing %q argument", key)
	}
	switch v := raw.(type) {
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case float32:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("invalid %q: %v", key, err)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("invalid %q type %T", key, raw)
	}
}

func floatArg(args map[string]any, key string) (float64, error) {
	raw, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("missing %q argument", key)
	}
	switch v := raw.(type) {
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid %q: %v", key, err)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("invalid %q type %T", key, raw)
	}
}
