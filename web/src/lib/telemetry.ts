export type TelemetryKeyPart = string | number;
export type TelemetryValue = number | null;
export type TelemetryQuality = string;

export type TelemetryBuffer = {
  ts: number[];
  v: TelemetryValue[];
  q: TelemetryQuality[];
  seq: number;
  latestTs?: number;
};

export const TELE_BUF = new Map<string, TelemetryBuffer>();
export const TELE_MAX = 720; // ~6 minutes at 500ms cadence — migration debt, will be replaced by tile pyramid
export const TELE_SERIES_MAX = 256;

function typedKeyPart(value: TelemetryKeyPart): [string, string | number] {
  if (typeof value === "number") return ["number", Number.isFinite(value) ? value : String(value)];
  return ["string", value];
}

export function teleKey(deviceId: TelemetryKeyPart, paramId: TelemetryKeyPart, instance?: TelemetryKeyPart) {
  return JSON.stringify([typedKeyPart(deviceId), typedKeyPart(instance ?? 1), typedKeyPart(paramId)]);
}

function snapshotTelemetry(buf: TelemetryBuffer): TelemetryBuffer {
  return {
    ts: buf.ts.slice(),
    v: buf.v.slice(),
    q: buf.q.slice(),
    seq: buf.seq || 0,
    latestTs: buf.latestTs,
  };
}

function touchTelemetryKey(key: string, buf: TelemetryBuffer) {
  TELE_BUF.delete(key);
  TELE_BUF.set(key, buf);
}

function evictTelemetrySeries() {
  while (TELE_BUF.size > TELE_SERIES_MAX) {
    const oldest = TELE_BUF.keys().next().value;
    if (oldest === undefined) break;
    TELE_BUF.delete(oldest);
  }
}

export function recordTelemetry(
  deviceId: TelemetryKeyPart,
  paramId: TelemetryKeyPart,
  value: TelemetryValue,
  quality: TelemetryQuality,
  instance?: TelemetryKeyPart,
) {
  const key = teleKey(deviceId, paramId, instance);
  let buf = TELE_BUF.get(key);
  if (!buf) {
    buf = { ts: [], v: [], q: [], seq: 0 };
    TELE_BUF.set(key, buf);
    evictTelemetrySeries();
  } else {
    touchTelemetryKey(key, buf);
  }
  const ts = Date.now();
  buf.ts.push(ts);
  buf.v.push(value);
  buf.q.push(quality);
  if (buf.ts.length > TELE_MAX) { buf.ts.shift(); buf.v.shift(); buf.q.shift(); }
  buf.seq = (buf.seq || 0) + 1;
  buf.latestTs = ts;
  return snapshotTelemetry(buf);
}

export function getTelemetry(deviceId: TelemetryKeyPart, paramId: TelemetryKeyPart, instance?: TelemetryKeyPart): TelemetryBuffer {
  const key = teleKey(deviceId, paramId, instance);
  const buf = TELE_BUF.get(key);
  if (buf) touchTelemetryKey(key, buf);
  return buf ? snapshotTelemetry(buf) : { ts: [], v: [], q: [], seq: 0, latestTs: undefined };
}
