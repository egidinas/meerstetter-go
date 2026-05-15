// @ts-nocheck
export const TELE_BUF = new Map();
export const TELE_MAX = 720; // ~6 minutes at 500ms cadence — migration debt, will be replaced by tile pyramid

export function teleKey(deviceId, paramId, instance?) {
  return deviceId + "/" + (instance || 1) + ":" + paramId;
}

export function recordTelemetry(deviceId, paramId, value, quality, instance?) {
  const key = teleKey(deviceId, paramId, instance);
  let buf = TELE_BUF.get(key);
  if (!buf) { buf = { ts: [], v: [], q: [] }; TELE_BUF.set(key, buf); }
  buf.ts.push(Date.now());
  buf.v.push(value);
  buf.q.push(quality);
  if (buf.ts.length > TELE_MAX) { buf.ts.shift(); buf.v.shift(); buf.q.shift(); }
  return buf;
}

export function getTelemetry(deviceId, paramId, instance?) {
  return TELE_BUF.get(teleKey(deviceId, paramId, instance)) || { ts: [], v: [], q: [] };
}
