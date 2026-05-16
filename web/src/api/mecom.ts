// @ts-nocheck
/* ============================================================
   Meerstetter Gateway — mock API client (contract-aligned v3)
   Implements docs/gateway/UI_GRAPH_WALL_CONTRACT.md parameter IDs.
   ============================================================ */

const LS_KEY = "mecomgw.settings";
const LS_CHANNELS = "mecomgw.channels";

function loadSettings() {
  try {
    return Object.assign(
      { gateway: "", holder: "design-claude", scenario: "mixed" },
      JSON.parse(localStorage.getItem(LS_KEY) || "{}")
    );
  } catch (_) {
    return { gateway: "", holder: "design-claude", scenario: "mixed" };
  }
}
function saveSettings(patch) {
  const next = Object.assign(loadSettings(), patch);
  localStorage.setItem(LS_KEY, JSON.stringify(next));
  return next;
}

const CATALOGUE = [
  { id: 1000,  name: "Object Temperature",            sid: "object_temp_c",        unit: "degC", type: "float32", kind: "continuous", role: "monitor", group: "Thermal", subgroup: "Object",    applicableModes: ["temp"], high_priority: true },
  { id: 1001,  name: "Sink Temperature",              sid: "sink_temp_c",          unit: "degC", type: "float32", kind: "continuous", role: "monitor", group: "Thermal", subgroup: "Sink",      applicableModes: ["temp", "supply"], high_priority: true },
  { id: 52200, name: "Cascade Temperature",           sid: "cascade_temp_c",       unit: "degC", type: "float32", kind: "continuous", role: "monitor", group: "Thermal", subgroup: "Cascade",   applicableModes: ["temp"], optional: true },
  { id: 1200,  name: "Temperature Is Stable",         sid: "temperature_stable",   unit: "",     type: "int32",   kind: "boolean",    role: "monitor", group: "Status",  subgroup: "Stability", applicableModes: ["temp"], enum: { 0: "Drifting", 1: "Stable" } },
  { id: 1020,  name: "Output Current",                sid: "output_current_a",     unit: "A",    type: "float32", kind: "continuous", role: "monitor", group: "Power",   subgroup: "Output",    applicableModes: ["temp", "supply"], high_priority: true },
  { id: 1021,  name: "Output Voltage",                sid: "output_voltage_v",     unit: "V",    type: "float32", kind: "continuous", role: "monitor", group: "Power",   subgroup: "Output",    applicableModes: ["temp", "supply"], high_priority: true },
  { id: 1022,  name: "Output Power",                  sid: "output_power_w",       unit: "W",    type: "float32", kind: "continuous", role: "monitor", group: "Power",   subgroup: "Output",    applicableModes: ["temp", "supply"] },
  { id: 1500,  name: "Driver Input Voltage",          sid: "driver_input_v",       unit: "V",    type: "float32", kind: "continuous", role: "monitor", group: "Power",   subgroup: "Input",     applicableModes: ["temp", "supply"] },
  { id: 1501,  name: "Driver Input Current",          sid: "driver_input_a",       unit: "A",    type: "float32", kind: "continuous", role: "monitor", group: "Power",   subgroup: "Input",     applicableModes: ["temp", "supply"] },
  { id: 3000,  name: "Target Object Temperature",     sid: "target_object_temp_c", unit: "degC", type: "float32", kind: "continuous", role: "control", group: "Control", subgroup: "Temperature setpoint", applicableModes: ["temp"], writable: true, cmd: "set_float32", min: -40, max: 150 },
  { id: 2020,  name: "PID · Kp",                      sid: "pid_kp",               unit: "",     type: "float32", kind: "continuous", role: "control", group: "Control", subgroup: "PID gains", applicableModes: ["temp"], writable: true, cmd: "write_float32", min: 0, max: 100 },
  { id: 2021,  name: "PID · Ti",                      sid: "pid_ti",               unit: "s",    type: "float32", kind: "continuous", role: "control", group: "Control", subgroup: "PID gains", applicableModes: ["temp"], writable: true, cmd: "write_float32", min: 0, max: 1000 },
  { id: 2022,  name: "PID · Td",                      sid: "pid_td",               unit: "s",    type: "float32", kind: "continuous", role: "control", group: "Control", subgroup: "PID gains", applicableModes: ["temp"], writable: true, cmd: "write_float32", min: 0, max: 100 },
  { id: 2010,  name: "Output Stage Enable",           sid: "output_stage_enable",  unit: "",     type: "int32",   kind: "enum",       role: "control", group: "Control", subgroup: "Stage",     applicableModes: ["temp", "supply"], writable: true, cmd: "write_int32", enum: { 0: "Static OFF", 1: "Static ON", 2: "Live OFF", 3: "HW ctrl" }, dangerous: true },
  { id: 2040,  name: "Operating Mode",                sid: "operating_mode",       unit: "",     type: "int32",   kind: "enum",       role: "control", group: "Control", subgroup: "Mode",      applicableModes: ["temp", "supply"], writable: true, cmd: "write_int32", enum: { 0: "Off", 1: "TEC closed-loop", 2: "PSU constant voltage", 3: "PSU constant current" }, dangerous: true },
  { id: 108,   name: "Save Data to Flash",            sid: "save_to_flash",        unit: "",     type: "int32",   kind: "enum",       role: "control", group: "Control", subgroup: "Persistence", applicableModes: ["temp", "supply"], writable: true, cmd: "save_to_flash", dangerous: true },
  { id: 104,   name: "Device Status",                 sid: "device_status",        unit: "",     type: "int32",   kind: "enum",       role: "monitor", group: "Status",  subgroup: "Device",    applicableModes: ["temp", "supply"], enum: { 0: "Init", 1: "Ready", 2: "Run", 3: "Error", 4: "Bootloader" } },
  { id: 105,   name: "Error Number",                  sid: "error_number",         unit: "",     type: "int32",   kind: "counter",    role: "monitor", group: "Status",  subgroup: "Errors",    applicableModes: ["temp", "supply"] },
  { id: 4000,  name: "Active Error Bits",             sid: "active_error_bits",    unit: "",     type: "int32",   kind: "state",      role: "monitor", group: "Status",  subgroup: "Errors",    applicableModes: ["temp", "supply"] },
  { id: 109,   name: "Flash Status",                  sid: "flash_status",         unit: "",     type: "int32",   kind: "boolean",    role: "monitor", group: "Status",  subgroup: "Persistence", applicableModes: ["temp", "supply"], enum: { 0: "Ready", 1: "Busy" } },
  { id: 4010,  name: "Total Operating Time",          sid: "total_operating_s",    unit: "s",    type: "float32", kind: "counter",    role: "monitor", group: "Status",  subgroup: "Counters",  applicableModes: ["temp", "supply"] },
  { id: 100,   name: "Device Type",                   sid: "device_type",          unit: "",     type: "int32",   kind: "state",      role: "monitor", group: "Metadata", subgroup: "Identity", applicableModes: ["temp", "supply"] },
  { id: 101,   name: "Hardware Version",              sid: "hw_version",           unit: "",     type: "int32",   kind: "state",      role: "monitor", group: "Metadata", subgroup: "Identity", applicableModes: ["temp", "supply"] },
  { id: 102,   name: "Serial Number",                 sid: "serial_number",        unit: "",     type: "int32",   kind: "state",      role: "monitor", group: "Metadata", subgroup: "Identity", applicableModes: ["temp", "supply"] },
  { id: 103,   name: "Firmware Version",              sid: "fw_version",           unit: "",     type: "int32",   kind: "state",      role: "monitor", group: "Metadata", subgroup: "Identity", applicableModes: ["temp", "supply"] },
];

function paramById(id) { return CATALOGUE.find((p) => p.id === id); }

function commandNameFor(signal) {
  if (!signal) return "write_float32";
  if (signal.cmd) return signal.cmd;
  if (signal.id === 3000) return "set_float32";
  if (signal.type === "int32") return "write_int32";
  return "write_float32";
}

const DEVICES_BASE = [
  { id: "tec-75", label: "Bus A · TEC SN75", endpoint: "can:can0/0x4b", address: 75 },
  { id: "tec-76", label: "Bus A · TEC SN76", endpoint: "can:can0/0x4c", address: 76 },
  { id: "tec-81", label: "Bus A · TEC SN81", endpoint: "can:can0/0x51", address: 81 },
  { id: "tec-84", label: "Bus A · TEC SN84", endpoint: "can:can0/0x54", address: 84 },
];

const DEFAULT_CHANNELS = [
  { device_id: "tec-75", instance: 1, role: "temp",   role_source: "local-assumption", label: "TEC ch1",       hasCascade: true  },
  { device_id: "tec-75", instance: 2, role: "supply", role_source: "local-assumption", label: "Laser drv ch2", hasCascade: false },
  { device_id: "tec-76", instance: 1, role: "temp",   role_source: "local-assumption", label: "TEC ch1",       hasCascade: false },
  { device_id: "tec-81", instance: 1, role: "temp",   role_source: "local-assumption", label: "TEC ch1",       hasCascade: false },
  { device_id: "tec-81", instance: 2, role: "supply", role_source: "local-assumption", label: "LD seed ch2",   hasCascade: false },
  { device_id: "tec-84", instance: 1, role: "temp",   role_source: "local-assumption", label: "TEC ch1",       hasCascade: false },
];

function loadChannels() {
  try {
    const raw = JSON.parse(localStorage.getItem(LS_CHANNELS) || "null");
    if (raw && raw.length) return raw;
  } catch (_) {}
  return DEFAULT_CHANNELS.slice();
}
function saveChannels(channels) {
  localStorage.setItem(LS_CHANNELS, JSON.stringify(channels));
  mock.channels = channels;
  mock.listeners.forEach((fn) => fn());
}

const SCENARIOS = {
  healthy: {
    "tec-75/1": { target: 25.0, bound: true, jitter: 0.03 },
    "tec-75/2": { setV: 5.1, setI: 0.45, bound: true, opMode: 2 },
    "tec-76/1": { target: 25.0, bound: true, jitter: 0.03 },
    "tec-81/1": { target: 25.0, bound: true, jitter: 0.03 },
    "tec-81/2": { setV: 3.3, setI: 0.18, bound: true, opMode: 2 },
    "tec-84/1": { target: 25.0, bound: true, jitter: 0.03 },
  },
  mixed: {
    "tec-75/1": { target: 25.0, bound: true, jitter: 0.03, opMode: 1 },
    "tec-75/2": { setV: 5.1, setI: 0.45, bound: true, opMode: 2 },
    "tec-76/1": { target: 18.0, bound: true, jitter: 0.18, drift: 0.45, leaseHolder: "design-claude", opMode: 1 },
    "tec-81/1": { target: 25.0, bound: true, jitter: 0.04, leaseHolder: "ops-bench-12", opMode: 1 },
    "tec-81/2": { setV: 3.3, setI: 0.18, bound: true, opMode: 3 },
    "tec-84/1": { target: 25.0, bound: false, lastError: "transport unreachable: dial can0/0x54: device not present" },
  },
  "lease-fight": {
    "tec-75/1": { target: 25.0, bound: true, leaseHolder: "ops-bench-12", opMode: 1 },
    "tec-75/2": { setV: 5.1, setI: 0.45, bound: true, leaseHolder: "ops-bench-12", opMode: 2 },
    "tec-76/1": { target: 25.0, bound: true, leaseHolder: "ops-bench-12", opMode: 1 },
    "tec-81/1": { target: 25.0, bound: true, leaseHolder: "ops-bench-12", opMode: 1 },
    "tec-81/2": { setV: 3.3, setI: 0.18, bound: true, leaseHolder: "ops-bench-12", opMode: 2 },
    "tec-84/1": { target: 25.0, bound: true, leaseHolder: "ops-bench-12", opMode: 1 },
  },
  "write-reject": {
    "tec-75/1": { target: 25.0, bound: true, leaseHolder: "design-claude", rejectWrites: true, opMode: 1 },
    "tec-75/2": { setV: 5.1, setI: 0.45, bound: true, opMode: 2 },
    "tec-76/1": { target: 25.0, bound: true, opMode: 1 },
    "tec-81/1": { target: 25.0, bound: true, opMode: 1 },
    "tec-81/2": { setV: 3.3, setI: 0.18, bound: true, opMode: 2 },
    "tec-84/1": { target: 25.0, bound: true, opMode: 1 },
  },
};

const mock = {
  t0: Date.now(),
  scenario: loadSettings().scenario || "mixed",
  channels: loadChannels(),
  channelState: {},
  deviceBound: {},
  deviceLastError: {},
  leases: [],
  commandEvents: [],
  listeners: new Set(),
  brokerStats: {},
};

function ckey(deviceId, instance) { return deviceId + "/" + instance; }

function resetScenario(name) {
  mock.scenario = name;
  const seed = SCENARIOS[name] || SCENARIOS.mixed;
  mock.leases = [];
  mock.channelState = {};

  DEVICES_BASE.forEach((d) => {
    mock.deviceBound[d.id] = true;
    mock.deviceLastError[d.id] = "";
    mock.brokerStats[d.id] = {
      frames_in: Math.floor(80000 + Math.random() * 40000),
      frames_out: Math.floor(60000 + Math.random() * 30000),
      error_count: Math.floor(Math.random() * 4),
      last_connect_at: new Date(Date.now() - 1000 * 60 * (4 + Math.random() * 12)).toISOString(),
      last_error_at: null,
    };
  });

  mock.channels.forEach((ch) => {
    const k = ckey(ch.device_id, ch.instance);
    const s = seed[k] || {};
    if (ch.role === "temp") {
      mock.channelState[k] = {
        role: "temp",
        targetT: s.target ?? 25.0,
        objectT: (s.target ?? 25.0) + (s.drift || 0),
        sinkT:   22.0 + (Math.random() - 0.5) * 0.6,
        cascadeT: ch.hasCascade ? ((s.target ?? 25.0) + 1.2 + (Math.random() - 0.5) * 0.3) : null,
        outputI: 0.6 + Math.random() * 0.2,
        outputV: 1.4 + Math.random() * 0.3,
        outputEnable: 1,
        opMode: s.opMode ?? 1,
        bound: s.bound !== false,
        rejectWrites: !!s.rejectWrites,
        jitter: s.jitter ?? 0.03,
        kp: 18, ti: 80, td: 1.5,
        stable: 1,
        savePending: 0,
        deviceStatus: 2,
        lastError: s.lastError || "",
      };
    } else {
      mock.channelState[k] = {
        role: "supply",
        setV: s.setV ?? 5.0,
        setI: s.setI ?? 0.5,
        actualV: (s.setV ?? 5.0) - 0.03,
        actualI: (s.setI ?? 0.5) * 0.94,
        opMode: s.opMode ?? 2,
        outputEnable: 1,
        sinkT: 22.0 + (Math.random() - 0.5) * 0.6,
        bound: s.bound !== false,
        rejectWrites: !!s.rejectWrites,
        savePending: 0,
        deviceStatus: 2,
        lastError: s.lastError || "",
      };
    }
    if (!mock.channelState[k].bound) {
      mock.deviceBound[ch.device_id] = false;
      mock.deviceLastError[ch.device_id] = mock.channelState[k].lastError;
      mock.brokerStats[ch.device_id].last_error_at = new Date(Date.now() - 1000 * 20).toISOString();
      mock.brokerStats[ch.device_id].error_count += 12;
    }
    if (s.leaseHolder) {
      if (!mock.leases.find((l) => l.device_id === ch.device_id)) {
        mock.leases.push({
          device_id: ch.device_id,
          holder: s.leaseHolder,
          token: "tok_" + ch.device_id + "_" + Math.random().toString(36).slice(2, 8),
          acquired_at: new Date(Date.now() - 60000).toISOString(),
          expires_at:  new Date(Date.now() + 1000 * 60 * 4.5).toISOString(),
        });
      }
    }
  });
}
resetScenario(mock.scenario);

setInterval(() => {
  Object.entries(mock.channelState).forEach(([k, s]) => {
    if (!s.bound) return;
    if (s.role === "temp") {
      const kPull = 0.18;
      s.objectT += (s.targetT - s.objectT) * kPull + (Math.random() - 0.5) * (s.jitter || 0.02);
      s.sinkT   += (Math.random() - 0.5) * 0.05;
      if (s.cascadeT !== null) {
        s.cascadeT += (s.objectT + 1.5 - s.cascadeT) * 0.12 + (Math.random() - 0.5) * 0.06;
      }
      const dT = s.targetT - s.objectT;
      s.outputI = Math.max(0, Math.min(s.outputEnable ? 6.5 : 0,
        0.7 + Math.abs(dT) * 1.6 + (Math.random() - 0.5) * 0.15));
      s.outputV = 1.4 + Math.abs(dT) * 0.6 + (Math.random() - 0.5) * 0.08;
      s.stable  = Math.abs(dT) < 0.05 ? 1 : 0;
    } else {
      s.actualV = s.outputEnable ? (s.setV - 0.02 + (Math.random() - 0.5) * 0.03) : 0;
      s.actualI = s.outputEnable ? (s.setI * 0.94 + (Math.random() - 0.5) * 0.01) : 0;
      s.sinkT   += (Math.random() - 0.5) * 0.05;
    }
  });
  DEVICES_BASE.forEach((d) => {
    if (!mock.deviceBound[d.id]) return;
    mock.brokerStats[d.id].frames_in  += Math.floor(20 + Math.random() * 6);
    mock.brokerStats[d.id].frames_out += Math.floor(15 + Math.random() * 5);
  });
  mock.listeners.forEach((fn) => fn());
}, 500);

function categorizeStatus(httpStatus) {
  if (httpStatus === 423) return "lease conflict";
  if (httpStatus === 503) return "device unreachable";
  if (httpStatus === 504) return "timeout";
  if (httpStatus === 403) return "read-only";
  if (httpStatus === 409) return "device rejected";
  if (httpStatus === 501) return "not supported";
  return "";
}
function pushEvent(ev) {
  mock.commandEvents.unshift(Object.assign({
    command_id: "cmd_" + Math.random().toString(36).slice(2, 10),
    time: new Date().toISOString(),
    status: "completed",
  }, ev));
  if (mock.commandEvents.length > 200) mock.commandEvents.pop();
  mock.listeners.forEach((fn) => fn());
}
function recordCommand({ deviceId, instance, paramId, value, prev, status, leaseHolder, errMessage, httpStatus }) {
  const def = paramById(paramId) || {};
  const dev = DEVICES_BASE.find((d) => d.id === deviceId);
  pushEvent({
    target_id: deviceId,
    instance,
    param_id: paramId,
    signal_name: def.name || ("#" + paramId),
    unit: def.unit || "",
    prev_value: prev,
    requested_value: value,
    lease_holder: leaseHolder,
    transport: dev ? dev.endpoint : "",
    status,
    error: errMessage || "",
    error_category: errMessage ? categorizeStatus(httpStatus) : "",
  });
}
[
  { dt: 3000,  st: "completed", tgt: "tec-75", inst: 1, p: 3000, v: 25,  prev: 24.8, holder: "ops-bench-12" },
  { dt: 15000, st: "completed", tgt: "tec-76", inst: 1, p: 2010, v: 1,   prev: 0,    holder: "design-claude" },
  { dt: 28000, st: "rejected",  tgt: "tec-84", inst: 1, p: 3000, v: 25,  prev: null, holder: "ops-bench-12", err: "transport unreachable: dial can0/0x54", hs: 503 },
  { dt: 41000, st: "completed", tgt: "tec-75", inst: 2, p: 1021, v: 5.1, prev: 4.95, holder: "design-claude" },
  { dt: 60000, st: "completed", tgt: "tec-81", inst: 1, p: 3000, v: 25,  prev: 24.5, holder: "ops-bench-12" },
].forEach((e) => {
  const def = paramById(e.p) || {};
  const dev = DEVICES_BASE.find((d) => d.id === e.tgt);
  pushEvent({
    time: new Date(Date.now() - e.dt).toISOString(),
    status: e.st,
    target_id: e.tgt,
    instance: e.inst,
    param_id: e.p,
    signal_name: def.name,
    unit: def.unit || "",
    prev_value: e.prev,
    requested_value: e.v,
    lease_holder: e.holder,
    transport: dev ? dev.endpoint : "",
    error: e.err || "",
    error_category: e.err ? categorizeStatus(e.hs) : "",
  });
});

const mockAPIImpl = {
  catalogue: () => CATALOGUE.slice(),
  paramById,
  commandNameFor,
  catalogueFor(role) {
    return CATALOGUE.filter((p) => !p.applicableModes || p.applicableModes.includes(role) || p.applicableModes.includes("any"));
  },
  devices: () => DEVICES_BASE.map((d) => ({
    ...d,
    bound: mock.deviceBound[d.id],
    last_error: mock.deviceLastError[d.id] || "",
  })),
  channels: () => mock.channels.slice(),
  setChannelRole(deviceId, instance, role, opts?) {
    const idx = mock.channels.findIndex((c) => c.device_id === deviceId && c.instance === instance);
    if (idx >= 0) {
      mock.channels[idx] = { ...mock.channels[idx], role, role_source: (opts && opts.role_source) || "local-assumption" };
    } else {
      mock.channels.push({ device_id: deviceId, instance, role, role_source: "local-assumption", label: `ch${instance}` });
    }
    saveChannels(mock.channels);
    resetScenario(mock.scenario);
  },
  leases: () => mock.leases.slice(),
  commandEvents: () => mock.commandEvents.slice(0, 50),
  brokerStats(deviceId) {
    const bs = mock.brokerStats[deviceId] || {};
    return {
      address: DEVICES_BASE.find((d) => d.id === deviceId)?.address,
      target:  DEVICES_BASE.find((d) => d.id === deviceId)?.endpoint,
      connected: mock.deviceBound[deviceId],
      last_connect_at: bs.last_connect_at,
      last_error_at: bs.last_error_at,
      last_error: mock.deviceLastError[deviceId] || "",
      frames_in:  bs.frames_in || 0,
      frames_out: bs.frames_out || 0,
      error_count: bs.error_count || 0,
    };
  },
  readValue(deviceId, paramId, instance?) {
    const inst = instance || 1;
    const ch = mock.channelState[ckey(deviceId, inst)];
    if (!ch) return { value: null, quality: "missing" };
    if (!ch.bound) return { value: null, quality: "unreachable" };
    let v;
    if (ch.role === "temp") {
      if (paramId === 52200) {
        if (ch.cascadeT === null) return { value: null, quality: "missing" };
        return { value: ch.cascadeT, quality: "ok" };
      }
      const map = {
        1000: ch.objectT, 1001: ch.sinkT, 1200: ch.stable,
        1020: ch.outputI, 1021: ch.outputV, 1022: ch.outputI * ch.outputV,
        1500: 12.0, 1501: 0.4,
        2010: ch.outputEnable, 2020: ch.kp, 2021: ch.ti, 2022: ch.td,
        2040: ch.opMode,
        3000: ch.targetT,
        104:  ch.deviceStatus, 105: 0, 109: ch.savePending,
        4000: 0, 4010: 60 * 60 * 24 * 12 + Math.random() * 1000,
        100:  150, 101: 2, 102: parseInt(deviceId.split("-")[1]) + 100, 103: 3.21,
      };
      v = map[paramId];
    } else {
      const map = {
        1001: ch.sinkT,
        1020: ch.actualI, 1021: ch.actualV, 1022: ch.actualV * ch.actualI,
        1500: 12.0, 1501: 0.4,
        2010: ch.outputEnable, 2040: ch.opMode,
        104:  ch.deviceStatus, 105: 0, 109: ch.savePending,
        4000: 0, 4010: 60 * 60 * 24 * 12 + Math.random() * 1000,
        100:  150, 101: 2, 102: parseInt(deviceId.split("-")[1]) + 100, 103: 3.21,
      };
      v = map[paramId];
    }
    if (v === undefined) return { value: null, quality: "missing" };
    if (typeof v === "number" && Number.isNaN(v)) return { value: null, quality: "nan" };
    return { value: v, quality: "ok" };
  },
  setpoint(deviceId, paramId, instance?) {
    const ch = mock.channelState[ckey(deviceId, instance || 1)];
    if (!ch) return null;
    if (ch.role === "supply") {
      if (paramId === 1020) return ch.setI;
      if (paramId === 1021) return ch.setV;
    }
    if (ch.role === "temp" && paramId === 3000) return ch.targetT;
    return null;
  },
  write(deviceId, req, leaseToken) {
    const inst = (req.arguments && req.arguments.instance) || 1;
    const ch = mock.channelState[ckey(deviceId, inst)];
    const lease = mock.leases.find((l) => l.device_id === deviceId);
    const holder = lease ? lease.holder : null;
    const { param, value } = req.arguments || {};
    const prev = (() => {
      if (!ch) return null;
      try { return mockAPIImpl.readValue(deviceId, param, inst).value; } catch (_) { return null; }
    })();
    if (!lease || lease.token !== leaseToken) {
      const err: any = new Error("lease invalid or expired");
      err.status = 423;
      recordCommand({ deviceId, instance: inst, paramId: param, value, prev, status: "rejected", leaseHolder: holder, errMessage: err.message, httpStatus: 423 });
      return Promise.reject(err);
    }
    if (!ch || !ch.bound) {
      const err: any = new Error("transport unreachable");
      err.status = 503;
      recordCommand({ deviceId, instance: inst, paramId: param, value, prev, status: "failed", leaseHolder: holder, errMessage: err.message, httpStatus: 503 });
      return Promise.reject(err);
    }
    if (ch.rejectWrites) {
      const err: any = new Error("device rejected: out of valid range");
      err.status = 409;
      recordCommand({ deviceId, instance: inst, paramId: param, value, prev, status: "rejected", leaseHolder: holder, errMessage: err.message, httpStatus: 409 });
      return Promise.reject(err);
    }
    if (ch.role === "temp") {
      if (param === 3000) ch.targetT = value;
      if (param === 2010) ch.outputEnable = value;
      if (param === 2040) ch.opMode = value;
      if (param === 2020) ch.kp = value;
      if (param === 2021) ch.ti = value;
      if (param === 2022) ch.td = value;
    } else {
      if (param === 1021) ch.setV = value;
      if (param === 1020) ch.setI = value;
      if (param === 2010) ch.outputEnable = value;
      if (param === 2040) ch.opMode = value;
    }
    if (req.name === "save_to_flash" || req.name === "save") {
      ch.savePending = 1;
      setTimeout(() => { ch.savePending = 0; mock.listeners.forEach((fn) => fn()); }, 1500);
    }
    recordCommand({ deviceId, instance: inst, paramId: param, value, prev, status: "completed", leaseHolder: holder });
    return Promise.resolve({
      command_id: "cmd_" + Math.random().toString(36).slice(2, 10),
      status: "completed",
      time: new Date().toISOString(),
      result: req,
    });
  },
  acquireLease(deviceId, holder, ttl?) {
    const existing = mock.leases.find((l) => l.device_id === deviceId);
    if (existing && existing.holder !== holder) {
      const err: any = new Error("device already leased by " + existing.holder);
      err.status = 423;
      return Promise.reject(err);
    }
    const lease = {
      device_id: deviceId,
      holder: holder || "design-claude",
      token: "tok_" + deviceId + "_" + Math.random().toString(36).slice(2, 8),
      acquired_at: new Date().toISOString(),
      expires_at:  new Date(Date.now() + 5 * 60 * 1000).toISOString(),
    };
    mock.leases = mock.leases.filter((l) => l.device_id !== deviceId).concat(lease);
    mock.listeners.forEach((fn) => fn());
    return Promise.resolve(lease);
  },
  releaseLease(deviceId, token) {
    mock.leases = mock.leases.filter((l) => l.device_id !== deviceId || l.token !== token);
    mock.listeners.forEach((fn) => fn());
    return Promise.resolve();
  },
  subscribe(fn) {
    mock.listeners.add(fn);
    return () => mock.listeners.delete(fn);
  },
  setScenario(name) {
    mock.commandEvents = [];
    resetScenario(name);
    mock.listeners.forEach((fn) => fn());
  },
  settings: loadSettings,
  saveSettings,
  formatValue(v, unit, paramId?) {
    if (v === null || v === undefined) return "—";
    if (typeof v === "number") {
      if (paramId === 2010) return ({0: "OFF", 1: "ON", 2: "Live OFF", 3: "HW ctrl"}[v]) ?? String(v);
      if (paramId === 1200) return v ? "Stable" : "Drifting";
      if (paramId === 2040) return ({0: "Off", 1: "TEC", 2: "PSU CV", 3: "PSU CC"}[v]) ?? String(v);
      if (paramId === 104)  return ({0:"Init",1:"Ready",2:"Run",3:"Error",4:"Bootloader"}[v]) ?? String(v);
      if (paramId === 109)  return v ? "Busy" : "Ready";
      const abs = Math.abs(v);
      let digits = 2;
      if (unit === "degC" || unit === "V") digits = 3;
      else if (unit === "A") digits = 3;
      else if (unit === "W") digits = 2;
      else if (unit === "s") digits = abs > 1000 ? 0 : 2;
      else if (abs > 1000) digits = 0;
      else if (abs < 1)    digits = 4;
      return v.toFixed(digits);
    }
    return String(v);
  },
  roleForParam(paramId) {
    if (paramId === 3000) return "cmd";
    if (paramId === 1000) return "actual";
    if (paramId === 1001) return "ghost";
    if (paramId === 52200) return "aux";
    if (paramId === 1021) return "actual";
    if (paramId === 1020) return "dut";
    if (paramId === 1022) return "aux";
    return "ghost";
  },
  colorForRole(role) {
    return {
      cmd:    "var(--series-cmd)",
      actual: "var(--series-actual)",
      dut:    "var(--series-dut)",
      ghost:  "var(--series-ghost)",
      aux:    "var(--series-aux)",
    }[role] || "var(--series-actual)";
  },
  provenance(deviceId, paramId, instance?) {
    const dev = DEVICES_BASE.find((d) => d.id === deviceId);
    return `device=${deviceId} param=${paramId} instance=${instance || 1}` +
           (dev ? ` endpoint=${dev.endpoint}` : "");
  },
  roleConfidence(channel) {
    if (!channel) {
      return { kind: "warn", label: "unconfirmed", detail: "Channel role is not available from the current gateway response." };
    }
    if (channel.role_source === "live") {
      return { kind: "ok", label: "live", detail: "Channel role was read from live device mode." };
    }
    if (channel.role_source === "config") {
      return { kind: "ok", label: "config", detail: "Channel role comes from configured channel metadata." };
    }
    return { kind: "warn", label: "unconfirmed", detail: "Channel role is a local assumption; confirm against the MeCom operating mode before relying on it." };
  },
  errorCategoryFromStatus: categorizeStatus,
};

// Live gateway adapter
const live = {
  active: false,
  checked: false,
  refreshing: false,
  base: "",
  lastError: "",
  devices: null,
  leases: null,
  values: Object.create(null),
  timer: null,
};

function configuredBase() {
  const raw = (loadSettings().gateway || "").trim();
  if (raw) return raw.replace(/\/+$/, "");
  if (window.location.protocol === "http:" || window.location.protocol === "https:") {
    return window.location.origin;
  }
  return "";
}
function explicitBase() {
  return !!(loadSettings().gateway || "").trim();
}
function hostedSameOrigin() {
  return !explicitBase() && (window.location.protocol === "http:" || window.location.protocol === "https:");
}
function notify() {
  mock.listeners.forEach((fn) => { try { fn(); } catch (_) {} });
}

async function fetchJSON(path, opts?) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 4500);
  const request = opts || {};
  try {
    const res = await fetch(configuredBase() + path, {
      credentials: "include",
      ...request,
      headers: { Accept: "application/json", ...(request.headers || {}) },
      signal: controller.signal,
    });
    const text = await res.text();
    let body = null;
    if (text) { try { body = JSON.parse(text); } catch (_) { body = null; } }
    if (!res.ok) {
      const bodyError = body && typeof body.error === "string" ? body.error.trim() : "";
      const err: any = new Error(bodyError || `HTTP ${res.status}`);
      err.status = res.status;
      err.body = body;
      throw err;
    }
    return body || {};
  } finally {
    clearTimeout(timeout);
  }
}

function liveKey(deviceId, paramId, instance?) {
  return `${deviceId}:${paramId}:${instance || 1}`;
}
function storeLiveValue(deviceId, entry) {
  const instance = entry.instance || 1;
  const value = entry.value;
  const quality = entry.quality || (value === null || value === undefined ? "missing" : "ok");
  live.values[liveKey(deviceId, entry.id, instance)] = { value, quality, at: Date.now() };
}
function paramsForChannel(role, instance) {
  const ids = role === "supply"
    ? [1020, 1021, 1022]
    : [1000, 1001, 3000, 52200, 1200, 1020, 1021, 1022];
  return ids.map((id) => `${id}:${instance || 1}`).join(",");
}
async function refreshLiveReads(devices) {
  const channels = mock.channels.slice();
  await Promise.all(devices.map(async (dev) => {
    const devChannels = channels.filter((c) => c.device_id === dev.id);
    for (const ch of devChannels) {
      try {
        const body = await fetchJSON(`/api/devices/${encodeURIComponent(dev.id)}/read?params=${encodeURIComponent(paramsForChannel(ch.role, ch.instance))}`);
        (body && body.values || []).forEach((entry) => storeLiveValue(dev.id, entry));
      } catch (err: any) {
        if ((err.status || 0) >= 400) {
          live.values[liveKey(dev.id, 104, ch.instance)] = { value: null, quality: "unreachable", at: Date.now() };
        }
      }
    }
  }));
}
async function refreshLiveOnce() {
  const base = configuredBase();
  if (!base || live.refreshing) return;
  live.refreshing = true;
  live.base = base;
  try {
    const devicesBody = await fetchJSON("/api/devices");
    const leasesBody = await fetchJSON("/api/leases").catch(() => ({ leases: [] }));
    const devices = (devicesBody && devicesBody.devices) || [];
    live.devices = devices.map((d) => ({ ...d, bound: d.bound !== false, last_error: d.last_error || "" }));
    live.leases = (leasesBody && leasesBody.leases) || [];
    await refreshLiveReads(live.devices);
    live.active = true;
    live.checked = true;
    live.lastError = "";
  } catch (err: any) {
    live.active = false;
    live.checked = true;
    live.lastError = err.message || String(err);
  } finally {
    live.refreshing = false;
    notify();
  }
}
function ensureLivePolling() {
  const base = configuredBase();
  if (!base) return;
  if (live.base !== base) {
    live.active = false;
    live.checked = false;
    live.lastError = "";
    live.devices = null;
    live.leases = null;
    live.values = Object.create(null);
    live.base = base;
  }
  if (!live.timer) {
    refreshLiveOnce();
    live.timer = setInterval(refreshLiveOnce, 2500);
  }
}
function liveDeviceById(deviceId) {
  return (live.devices || []).find((d) => d.id === deviceId) || DEVICES_BASE.find((d) => d.id === deviceId);
}

export const MecomAPI = {
  ...mockAPIImpl,
  isLive() {
    ensureLivePolling();
    return live.active;
  },
  liveBase() {
    return live.base || configuredBase();
  },
  liveError() {
    ensureLivePolling();
    return live.lastError;
  },
  devices() {
    ensureLivePolling();
    return live.active && live.devices ? live.devices : mockAPIImpl.devices();
  },
  leases() {
    ensureLivePolling();
    return live.active && live.leases ? live.leases.slice() : mockAPIImpl.leases();
  },
  brokerStats(deviceId) {
    ensureLivePolling();
    const dev = liveDeviceById(deviceId);
    const base = mockAPIImpl.brokerStats(deviceId);
    if (!live.active) return base;
    return {
      ...base,
      address: dev && dev.address,
      target: dev && dev.endpoint,
      connected: dev && dev.bound !== false,
      last_error: dev && dev.last_error || "",
    };
  },
  readValue(deviceId, paramId, instance?) {
    ensureLivePolling();
    if (live.active) {
      const v = live.values[liveKey(deviceId, paramId, instance)];
      if (v) return { value: v.value, quality: v.quality || "ok" };
      return { value: null, quality: "missing" };
    }
    return mockAPIImpl.readValue(deviceId, paramId, instance);
  },
  setpoint(deviceId, paramId, instance?) {
    ensureLivePolling();
    if (live.active) {
      const v = MecomAPI.readValue(deviceId, paramId, instance);
      return v.quality === "ok" ? v.value : null;
    }
    return mockAPIImpl.setpoint(deviceId, paramId, instance);
  },
  async write(deviceId, req, leaseToken) {
    ensureLivePolling();
    if (!live.active && hostedSameOrigin()) {
      const err: any = new Error("live gateway unavailable");
      err.status = 503;
      throw err;
    }
    if (!live.active && !explicitBase()) return mockAPIImpl.write(deviceId, req, leaseToken);
    const inst = (req.arguments && req.arguments.instance) || 1;
    const param = req.arguments && req.arguments.param;
    const prev = MecomAPI.readValue(deviceId, param, inst).value;
    try {
      const body = await fetchJSON(`/api/devices/${encodeURIComponent(deviceId)}/write`, {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Lease-Token": leaseToken || "" },
        body: JSON.stringify(req),
      });
      const lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      recordCommand({ deviceId, instance: inst, paramId: param, value: req.arguments && req.arguments.value, prev, status: (body && body.status) || "completed", leaseHolder: lease && lease.holder });
      refreshLiveOnce();
      return body;
    } catch (err: any) {
      const lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      recordCommand({ deviceId, instance: inst, paramId: param, value: req.arguments && req.arguments.value, prev, status: (err.status === 423 || err.status === 409) ? "rejected" : "failed", leaseHolder: lease && lease.holder, errMessage: err.message, httpStatus: err.status });
      throw err;
    }
  },
  async acquireLease(deviceId, holder, ttl?) {
    ensureLivePolling();
    if (!live.active && hostedSameOrigin()) {
      const err: any = new Error("live gateway unavailable");
      err.status = 503;
      throw err;
    }
    if (!live.active && !explicitBase()) return mockAPIImpl.acquireLease(deviceId, holder, ttl);
    const lease = await fetchJSON(`/api/devices/${encodeURIComponent(deviceId)}/lease`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ holder: holder || loadSettings().holder, ttl: ttl || "5m" }),
    });
    live.leases = (live.leases || []).filter((l) => l.device_id !== deviceId).concat(lease);
    notify();
    return lease;
  },
  async releaseLease(deviceId, token) {
    ensureLivePolling();
    if (!live.active && hostedSameOrigin()) {
      const err: any = new Error("live gateway unavailable");
      err.status = 503;
      throw err;
    }
    if (!live.active && !explicitBase()) return mockAPIImpl.releaseLease(deviceId, token);
    await fetchJSON(`/api/devices/${encodeURIComponent(deviceId)}/lease`, {
      method: "DELETE",
      headers: { "X-Lease-Token": token || "" },
    });
    live.leases = (live.leases || []).filter((l) => l.device_id !== deviceId || l.token !== token);
    notify();
  },
  subscribe(fn) {
    ensureLivePolling();
    return mockAPIImpl.subscribe(fn);
  },
};

export default MecomAPI;
