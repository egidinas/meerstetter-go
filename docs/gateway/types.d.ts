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

// --- CAN signal registry (GET/POST /api/canmap*) ---

export interface CanmapMapEntry {
  index: string;    // "0x4200"
  subindex: number;
  bits: number;     // e.g. 32
  comment?: string;
}

export interface CanmapSDOWrite {
  index: string;    // "0x3300"
  subindex: number;
  value: number;
  comment?: string;
}

export interface CanmapNode {
  role: string;     // stable symbolic name, e.g. "tec-a"
  node_id?: number; // CANopen node ID 1..127; absent/0 in a pattern
  family?: string;  // "tec-v6.32", "rmm-1182"
  label?: string;
}

export interface CanmapProducer {
  role: string;
  tpdo: number;
  mapping: CanmapMapEntry[];
}

export interface CanmapConsumer {
  role: string;
  rpdo: number;
  mapping: CanmapMapEntry[];
  source_selects?: CanmapSDOWrite[];
}

export interface CanmapSignal {
  cob_id: string;   // "0x1A1"
  name: string;
  description?: string;
  producer: CanmapProducer;
  consumers: CanmapConsumer[];
  rate_ms?: number;
  saved_to_flash?: boolean;
  verified?: string; // YYYY-MM-DD of last live read-back
}

export interface CanmapRegistry {
  schema_version: number;
  name: string;
  description?: string;
  nodes: CanmapNode[];
  signals: CanmapSignal[];
}

export interface CanmapObservedPDO {
  number: number;
  cob_id: string;
  enabled: boolean;            // false when the COB-ID invalid bit is set
  transmission_type: number;
  mapping: CanmapMapEntry[];
}

export interface CanmapObservedNode {
  node_id: number;
  rpdos: CanmapObservedPDO[];
  tpdos: CanmapObservedPDO[];
  source_selects?: Record<string, number>;
  errors?: string[];
}

export type CanmapVerdict = "match" | "drift" | "unknown";

export interface CanmapFinding {
  signal: string;
  role: string;
  node_id?: number;
  aspect: string;   // e.g. "rpdo cob-id"
  verdict: CanmapVerdict;
  want?: string;
  got?: string;
}

export interface CanmapSignalStatus {
  signal: string;
  cob_id: string;
  verdict: CanmapVerdict;
  findings: CanmapFinding[];
}

export interface CanmapResponse {
  registry: CanmapRegistry | null;
  is_pattern?: boolean;
  observed?: Record<string, CanmapObservedNode>; // keyed by node ID (live=1)
  status?: CanmapSignalStatus[];                  // per-signal verdicts (live=1)
  observed_at?: string;                           // RFC3339 (live=1)
}

export interface CanmapBinding {
  role: string;
  node_id: number;
  label?: string;
}

export interface CanmapImportRequest {
  registry?: CanmapRegistry;  // import a concrete registry as-is
  pattern?: CanmapRegistry;   // or instantiate a pattern...
  name?: string;
  bindings?: CanmapBinding[]; // ...with these role→node bindings
}
