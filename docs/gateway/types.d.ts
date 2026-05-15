// TypeScript types for the meerstetter-go gateway (mecomgw) HTTP/SSE surface.
// Regenerate from docs/gateway/openapi.yaml when handlers change.
//
// Source of truth: cmd/mecomgw/handlers.go.

export interface Lease {
  device_id: string;
  holder: string;
  token: string;
  acquired_at: string; // RFC3339
  expires_at: string;  // RFC3339
}

export interface DeviceView {
  id: string;
  label?: string;
  endpoint: string;
  address: number;       // 1..254
  bound: boolean;
  last_error?: string;
}

export interface CatalogueEntry {
  id: number;
  instance: number;
  name: string;
  unit?: string;
  type: "float32" | "int32";
  sensor?: string;
  high_priority: boolean;
  writable: boolean;
}

export type CommandStatus =
  | "accepted"
  | "sent"
  | "acked"
  | "completed"
  | "rejected"
  | "failed";

export interface CommandEvent {
  command_id: string;
  session_id?: string;
  time: string;
  status: CommandStatus;
  transport?: string;
  idempotency_key?: string;
  result?: unknown;
  error?: string;
  metadata?: Record<string, string>;
}

export type TelemetryQuality = "ok" | "nan" | "missing" | "unreachable";

export interface Telemetry {
  id: string;
  target_id: string;
  session_id?: string;
  time: string;
  name: string;
  value: number | string | boolean | null;
  unit?: string;
  quality: TelemetryQuality;
  metadata?: Record<string, string>;
}

export type WriteCommandName =
  | "set_float32"
  | "write_float32"
  | "set_int32"
  | "write_int32"
  | "reset"
  | "save_to_flash"
  | "save";

export interface WriteRequest {
  name: WriteCommandName;
  arguments: {
    param?: number;
    instance?: number;
    value?: number;
  };
  metadata?: Record<string, string>;
}

export interface LeaseAcquireRequest {
  holder: string;
  ttl?: string; // Go duration string e.g. "5m"
}

// Convenience: response envelopes.

export interface DevicesResponse { devices: DeviceView[] }
export interface CatalogueResponse { parameters: CatalogueEntry[] }
export interface LeasesResponse { leases: Lease[] }

export interface ReadResponse {
  values: Array<{ id: number; instance: number; value: number | null }>;
}
