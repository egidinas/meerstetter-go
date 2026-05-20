// @ts-nocheck
/* ============================================================
   Meerstetter Gateway — mock API client (contract-aligned v3)
   Implements docs/gateway/UI_GRAPH_WALL_CONTRACT.md parameter IDs.
   ============================================================ */

import catalogueJson from "../data/mecom-catalogue.json?raw";
import protocolFamiliesJson from "../data/mecom-protocol-families.json?raw";
import operatorProjectionJson from "../data/mecom-operator-projection.json?raw";
import {
  loadSemanticOverlay,
  normalizeSemanticOverlayBundle,
  overlayEntryForTarget,
  removeSemanticOverlayEntry,
  saveSemanticOverlay,
  semanticOverlayTargetKey,
  upsertSemanticOverlayEntry,
} from "signalforge-web";

const LS_KEY = "mecomgw.settings";
const LS_CHANNELS = "mecomgw.channels";
const LS_CHANNELS_VERSION = "mecomgw.channels.version";
const CHANNEL_ALIAS_NAMESPACE = "mecomgw.channelAliases";
const CHANNEL_METADATA_VERSION = "2026-05-18-fixture-pattern-v1";
const DEFAULT_SETTINGS = {
  gateway: "",
  holder: "design-claude",
  scenario: "mixed",
  bridgeDefaultTransport: "serial",
  bridgeFallbackTransport: "can",
  bridgeRouteSelection: "fixed-preference",
  bridgeAddressZero: "default-device",
};

const MECOM_PARAMETER_FAMILIES = JSON.parse(protocolFamiliesJson)
  .map((family) => ({
    start: Number(family.start),
    end: Number(family.end),
    label: String(family.label || "").trim(),
  }))
  .filter((family) => Number.isFinite(family.start) && Number.isFinite(family.end) && family.label)
  .sort((a, b) => a.start - b.start || a.end - b.end);

const OPERATOR_PROJECTION = (() => {
  try {
    return JSON.parse(operatorProjectionJson);
  } catch (_) {
    return { primary_mappings: [], secondary_mappings: [] };
  }
})();
const OPERATOR_PRIMARY_PROJECTION_BY_ID = new Map();
const OPERATOR_SECONDARY_PROJECTION_BY_ID = new Map();

function registerOperatorProjectionMapping(mapping, secondary = false) {
  const ids = Array.isArray(mapping && mapping.ids) ? mapping.ids : [];
  ids.map((id) => Number(id)).filter((id) => Number.isFinite(id)).forEach((id) => {
    const normalized = {
      ...mapping,
      ids,
      path: normalizePathSegments(mapping && mapping.path),
      sort: Number.isFinite(Number(mapping && mapping.sort)) ? Number(mapping.sort) : 0,
      default_collapsed: Boolean(mapping && mapping.default_collapsed),
      secondary,
    };
    if (secondary) {
      const list = OPERATOR_SECONDARY_PROJECTION_BY_ID.get(id) || [];
      list.push(normalized);
      OPERATOR_SECONDARY_PROJECTION_BY_ID.set(id, list.sort((a, b) => a.sort - b.sort || String(a.bundle || "").localeCompare(String(b.bundle || ""))));
    } else if (!OPERATOR_PRIMARY_PROJECTION_BY_ID.has(id)) {
      OPERATOR_PRIMARY_PROJECTION_BY_ID.set(id, normalized);
    }
  });
}

(OPERATOR_PROJECTION.primary_mappings || []).forEach((mapping) => registerOperatorProjectionMapping(mapping, false));
(OPERATOR_PROJECTION.secondary_mappings || []).forEach((mapping) => registerOperatorProjectionMapping(mapping, true));

function operatorProjectionForID(id) {
  return OPERATOR_PRIMARY_PROJECTION_BY_ID.get(Number(id)) || null;
}

function secondaryOperatorProjectionsForID(id) {
  return OPERATOR_SECONDARY_PROJECTION_BY_ID.get(Number(id)) || [];
}

function loadSettings() {
  try {
    return Object.assign({}, DEFAULT_SETTINGS, JSON.parse(localStorage.getItem(LS_KEY) || "{}"));
  } catch (_) {
    return { ...DEFAULT_SETTINGS };
  }
}
function saveSettings(patch) {
  const next = Object.assign(loadSettings(), patch);
  localStorage.setItem(LS_KEY, JSON.stringify(next));
  return next;
}

const CATALOGUE = JSON.parse(catalogueJson).map((entry) => {
  const trees = treeSelectionFrom(entry, entry.id, entry.name || entry.sid);
  return { ...entry, tree_path: trees.tree_path, tree_paths: trees.tree_paths };
});

function paramById(id, instance?) {
  if (instance !== undefined && instance !== null) {
    const key = `${id}:${instance}`;
    const keyed = CATALOGUE.find((p) => `${p.id}:${p.instance || 1}` === key);
    if (keyed) return keyed;
  }
  return CATALOGUE.find((p) => p.id === id);
}

function parseTreeMeta(raw) {
  if (!raw) return null;
  if (typeof raw === "string") {
    try {
      return JSON.parse(raw);
    } catch (_) {
      return null;
    }
  }
  return raw;
}

function parseTreeList(raw) {
  if (Array.isArray(raw)) return raw;
  if (typeof raw === "string") {
    try {
      const parsed = JSON.parse(raw);
      return Array.isArray(parsed) ? parsed : [];
    } catch (_) {
      return [];
    }
  }
  return [];
}

function normalizePathSegments(raw) {
  if (Array.isArray(raw)) {
    return raw.map((part) => String(part || "").trim()).filter(Boolean);
  }
  if (typeof raw === "string") {
    return raw.split(/\s*(?:\/|>)\s*/).map((part) => part.trim()).filter(Boolean);
  }
  return [];
}

function normalizeTreePath(path, fallbackId, fallbackLabel) {
  if (!path) return null;
  if (typeof path === "string") {
    const segments = normalizePathSegments(path);
    if (!segments.length) return null;
    const text = segments.join(" / ");
    return {
      id: String(fallbackId ?? text).replace(/\s+/g, "_"),
      label: fallbackLabel || segments[segments.length - 1] || text,
      path: segments,
      default: true,
      bundle: "",
      default_collapsed: false,
      secondary: false,
      reason: "",
      duplicate_reason: "",
      instance_scope: "",
      sort: 0,
    };
  }
  if (Array.isArray(path)) {
    const segments = normalizePathSegments(path);
    if (!segments.length) return null;
    const text = segments.join(" / ");
    return {
      id: String(fallbackId ?? text).replace(/\s+/g, "_"),
      label: fallbackLabel || segments[segments.length - 1] || text,
      path: segments,
      default: true,
      bundle: "",
      default_collapsed: false,
      secondary: false,
      reason: "",
      duplicate_reason: "",
      instance_scope: "",
      sort: 0,
    };
  }
  if (typeof path !== "object") return null;
  let segments = normalizePathSegments(path.path);
  if (!segments.length) segments = normalizePathSegments(path.label);
  if (!segments.length) return null;
  const text = segments.join(" / ");
  const sort = Number(path.sort);
  return {
    id: String(path.id || fallbackId || text).trim(),
    label: String(path.label || fallbackLabel || segments[segments.length - 1] || text).trim(),
    path: segments,
    default: Boolean(path.default),
    bundle: String(path.bundle || "").trim(),
    default_collapsed: Boolean(path.default_collapsed),
    secondary: Boolean(path.secondary),
    reason: String(path.reason || path.duplicate_reason || "").trim(),
    duplicate_reason: String(path.duplicate_reason || path.reason || "").trim(),
    instance_scope: String(path.instance_scope || path.instanceScope || "").trim(),
    sort: Number.isFinite(sort) ? sort : 0,
    column_order: path.column_order || path.columnOrder || null,
  };
}

function normalizeTreePaths(raw, fallbackId, fallbackLabel, fallbackPath) {
  const list = parseTreeList(raw);
  const normalized = list.map((entry, idx) => normalizeTreePath(entry, `${fallbackId || fallbackPath || "tree"}:${idx}`, fallbackLabel)).filter(Boolean);
  if (normalized.length === 0) return [];
  const defaultIndex = normalized.findIndex((item) => item.default);
  if (defaultIndex >= 0) {
    return normalized.map((item, idx) => ({ ...item, default: idx === defaultIndex }));
  }
  return normalized.map((item, idx) => ({ ...item, default: idx === 0 }));
}

function treeSelectionFrom(entry, fallbackId, fallbackLabel) {
  const single = normalizeTreePath(entry && entry.tree_path, fallbackId, fallbackLabel);
  const many = normalizeTreePaths(entry && entry.tree_paths, fallbackId, fallbackLabel, single && single.path && single.path.join("/"));
  const generated = defaultTreePathsForEntry(entry, fallbackId, fallbackLabel, single);
  const paths = many.length ? many : (generated.length ? generated : (single ? [single] : []));
  const selected = paths.find((item) => item.default) || paths[0] || null;
  return { tree_path: selected, tree_paths: paths };
}

function parseTransportSupport(raw) {
  if (!raw) return raw;
  if (Array.isArray(raw)) return raw;
  if (typeof raw === "string") {
    try {
      const parsed = JSON.parse(raw);
      return parsed;
    } catch (_) {
      return raw;
    }
  }
  return raw;
}

function parseCounterparts(raw) {
  if (!raw) return null;
  if (typeof raw === "string") {
    try {
      return parseCounterparts(JSON.parse(raw));
    } catch (_) {
      return null;
    }
  }
  if (typeof raw !== "object" || Array.isArray(raw)) return null;
  const out = {};
  Object.keys(raw).forEach((key) => {
    const value = raw[key];
    if (!Array.isArray(value)) return;
    const ids = value.map((item) => Number(item)).filter((item) => Number.isFinite(item));
    if (ids.length) out[String(key)] = ids;
  });
  return Object.keys(out).length ? out : null;
}

function firstDefined(...values) {
  return values.find((value) => value !== undefined && value !== null);
}

function semanticName(entry, base, id) {
  return firstDefined(
    entry && entry.name,
    entry && entry.display_name,
    entry && entry.displayName,
    entry && entry.display,
    base && base.name,
    base && base.display_name,
    base && base.displayName,
    base && base.display,
    `Parameter ${id}`,
  );
}

function normalizeCatalogueEntry(entry, base, id, instance, metadata, trees) {
  entry = entry || {};
  base = base || {};
  const displayName = firstDefined(entry.displayName, entry.display_name, entry.display, base.displayName, base.display_name, base.display);
  const rawName = firstDefined(entry.rawName, entry.raw_name, base.rawName, base.raw_name, entry.sid, base.sid);
  const applicableModes = firstDefined(entry.applicableModes, entry.applicable_modes, base.applicableModes, base.applicable_modes, ["temp", "supply"]);
  const semanticRole = firstDefined(entry.semantic_role, entry.semanticRole, base.semantic_role, base.semanticRole, metadata && metadata.semantic_role);
  const sourceParameterName = firstDefined(entry.source_parameter_name, entry.sourceParameterName, base.source_parameter_name, base.sourceParameterName, metadata && metadata.source_parameter_name);
  const readoutPriority = firstDefined(entry.readout_priority, entry.readoutPriority, base.readout_priority, base.readoutPriority, metadata && metadata.readout_priority);
  const preferredReadout = firstDefined(entry.preferred_readout, entry.preferredReadout, base.preferred_readout, base.preferredReadout, metadata && metadata.preferred_readout);
  const transportSupport = parseTransportSupport(firstDefined(entry.transport_support, entry.transportSupport, base.transport_support, base.transportSupport));
  const counterparts = parseCounterparts(firstDefined(entry.counterparts, base.counterparts, metadata && metadata.counterparts));
  const hoverHelp = firstDefined(entry.hover_help, entry.hoverHelp, entry.help_text, entry.helpText, base.hover_help, base.hoverHelp, base.help_text, base.helpText, metadata && metadata.hover_help, metadata && metadata.help_text);
  const helpText = firstDefined(entry.help_text, entry.helpText, entry.hover_help, entry.hoverHelp, base.help_text, base.helpText, base.hover_help, base.hoverHelp, metadata && metadata.help_text, metadata && metadata.hover_help);
  const visibility = firstDefined(entry.visibility, base.visibility, metadata && metadata.visibility, "advanced");
  return {
    ...base,
    ...entry,
    id,
    instance,
    name: semanticName(entry, base, id),
    displayName,
    display_name: displayName,
    rawName,
    raw_name: rawName,
    sid: entry.sid || base.sid || String(entry.sensor || rawName || id),
    unit: entry.unit ?? base.unit ?? "",
    type: entry.type || base.type || "float32",
    kind: entry.kind || base.kind || "continuous",
    role: entry.role || base.role || (entry.writable ? "control" : "monitor"),
    group: entry.group || base.group || (entry.writable ? "Control" : "Telemetry"),
    subgroup: entry.subgroup || base.subgroup || "",
    category: entry.category || base.category || "",
    access: entry.access || base.access || (entry.writable ? "read_write" : "read_only"),
    semantic_role: semanticRole,
    semanticRole,
    source_parameter_name: sourceParameterName,
    sourceParameterName,
    readout_priority: readoutPriority,
    readoutPriority,
    preferred_readout: preferredReadout,
    preferredReadout,
    visibility,
    hover_help: hoverHelp || "",
    hoverHelp: hoverHelp || "",
    help_text: helpText || "",
    helpText: helpText || "",
    counterparts,
    min: entry.min ?? base.min,
    max: entry.max ?? base.max,
    enum: entry.enum !== undefined ? entry.enum : base.enum,
    optional: entry.optional ?? base.optional,
    dangerous: entry.dangerous ?? base.dangerous,
    high_priority: Boolean(firstDefined(entry.high_priority, entry.highPriority, base.high_priority, base.highPriority)),
    writable: entry.writable !== undefined ? Boolean(entry.writable) : isLocallyWritableParam(id, entry.writable),
    applicableModes,
    applicable_modes: applicableModes,
    tree_path: trees.tree_path,
    tree_paths: trees.tree_paths,
    projection_bundle: trees.tree_path && trees.tree_path.bundle || "",
    projectionBundle: trees.tree_path && trees.tree_path.bundle || "",
    metadata: entry.metadata || base.metadata || null,
    transport_support: transportSupport,
    transportSupport,
    command: entry.command || entry.cmd || base.command || base.cmd || metadata && metadata.command || null,
    cmd: entry.cmd || base.cmd || null,
  };
}

function mecomParameterFamily(id) {
  const value = Number(id);
  if (!Number.isFinite(value)) return "Unassigned parameters";
  const family = MECOM_PARAMETER_FAMILIES.find((item) => value >= item.start && value < item.end);
  if (family) return family.label;
  const block = Math.floor(value / 1000) * 1000;
  return `${String(block).padStart(4, "0")} MeCom parameter block`;
}

function defaultTreePathsForEntry(entry, fallbackId, fallbackLabel, single) {
  if (!entry) return [];
  const primaryProjection = operatorProjectionForID(fallbackId);
  const label = String(fallbackLabel || entry.name || entry.display || entry.sid || `Parameter ${fallbackId || ""}`).trim();
  const group = String(entry.group || "Other Signals").trim();
  const subgroup = String(entry.subgroup || "Signals").trim();
  const operatorPath = primaryProjection && primaryProjection.path && primaryProjection.path.length
    ? primaryProjection.path
    : single && single.path && single.path.length
    ? single.path
    : [group, subgroup, label].filter(Boolean);
  const protocolName = String(entry.raw_name || entry.rawName || entry.sid || label).trim();
  const paths = [
    normalizeTreePath({
      id: "operator",
      label: primaryProjection && primaryProjection.label || "Operator",
      path: operatorPath,
      default: true,
      bundle: primaryProjection && primaryProjection.bundle || "",
      default_collapsed: Boolean(primaryProjection && primaryProjection.default_collapsed),
      instance_scope: primaryProjection && primaryProjection.instance_scope || "",
      sort: primaryProjection && primaryProjection.sort || 0,
    }),
  ];
  secondaryOperatorProjectionsForID(fallbackId).forEach((mapping, idx) => {
    paths.push(normalizeTreePath({
      id: `operator-secondary-${mapping.bundle || idx}`,
      label: mapping.label || "Secondary",
      path: mapping.path,
      default: false,
      secondary: true,
      bundle: mapping.bundle || "",
      default_collapsed: Boolean(mapping.default_collapsed),
      instance_scope: mapping.instance_scope || "",
      reason: mapping.reason || "",
      duplicate_reason: mapping.reason || "",
      sort: mapping.sort || 0,
    }));
  });
  if (fallbackId !== undefined && fallbackId !== null && `${fallbackId}`.trim() !== "") {
    paths.push(normalizeTreePath({
      id: "protocol",
      label: "MeCom protocol",
      path: ["MeCom protocol", mecomParameterFamily(fallbackId), `Parameter ${fallbackId}`, protocolName],
    }));
  }
  return paths.filter(Boolean);
}

const READ_ONLY_OUTPUT_STAGE_PARAMS = new Set([1020, 1021, 1022, 40000]);
const WRITABLE_OUTPUT_STAGE_PARAMS = new Set([2020, 2021, 2030, 2031, 2032, 2033]);
const WRITABLE_CASCADE_PARAMS = new Set([53120, 53121, 53122, 53123]);

function isLocallyWritableParam(id, liveWritable) {
  if (READ_ONLY_OUTPUT_STAGE_PARAMS.has(id)) return false;
  if (WRITABLE_OUTPUT_STAGE_PARAMS.has(id) || WRITABLE_CASCADE_PARAMS.has(id)) return true;
  const base = paramById(id);
  return Boolean((base && base.writable) || liveWritable);
}

function unitLabel(unit) {
  const u = String(unit || "").trim();
  if (!u || u === "_") return "";
  if (u === "degC") return "°C";
  return u;
}

function formatWithUnit(value, unit, paramId?) {
  if (value === null || value === undefined) return "—";
  const label = unitLabel(unit);
  if (typeof value === "number" && String(unit || "") === "W" && Math.abs(value) > 0 && Math.abs(value) < 1) {
    return `${(value * 1000).toFixed(3)} mW`;
  }
  const formatted = mockAPIImpl.formatValue(value, unit, paramId);
  return label ? `${formatted} ${label}` : formatted;
}

function commandNameFor(signal) {
  if (!signal) return "write_float32";
  if (signal.command) return signal.command;
  if (signal.cmd) return signal.cmd;
  if (signal.type === "latin1" || signal.type === "string" || signal.kind === "text") return "write_big_data_string";
  if (signal.type === "int32" || signal.type === "uint32" || signal.type === "bool" || signal.type === "enum") return "write_int32";
  return "write_float32";
}

const COUNTERPART_PREFERENCE = {
  telemetry: ["measured", "source", "readout", "feedback"],
  control: ["setpoint", "command", "limit", "enable", "mode", "selection", "sync"],
};

function counterpartIdsFor(signal, kind?) {
  const raw = signal && signal.counterparts;
  if (!raw) return [];
  if (kind) return Array.isArray(raw[kind]) ? raw[kind].slice() : [];
  const seen = new Set();
  const ids = [];
  Object.values(raw).forEach((values) => {
    (Array.isArray(values) ? values : []).forEach((value) => {
      const id = Number(value);
      if (!Number.isFinite(id) || seen.has(id)) return;
      seen.add(id);
      ids.push(id);
    });
  });
  return ids;
}

function counterpartSignalsFor(signal, kind?) {
  return counterpartIdsFor(signal, kind).map((id) => MecomAPI.paramById(id, signal && signal.instance)).filter(Boolean);
}

function semanticCounterpartFor(signal, side?) {
  if (!signal) return null;
  const kinds = side === "telemetry" ? COUNTERPART_PREFERENCE.control : COUNTERPART_PREFERENCE.telemetry;
  for (const kind of kinds) {
    const hit = counterpartSignalsFor(signal, kind)[0];
    if (hit) {
      return {
        kind,
        parameter: hit,
        parameter_id: hit.id,
        instance: hit.instance,
      };
    }
  }
  const fallback = counterpartSignalsFor(signal)[0] || null;
  return fallback ? { kind: "", parameter: fallback, parameter_id: fallback.id, instance: fallback.instance } : null;
}

function semanticPairFor(signal) {
  if (!signal) return null;
  const telemetryHit = signal.role === "monitor" ? null : semanticCounterpartFor(signal, "telemetry");
  const controlHit = signal.role === "monitor" ? semanticCounterpartFor(signal, "control") : null;
  const telemetry = signal.role === "monitor" ? signal : telemetryHit && telemetryHit.parameter;
  const control = signal.role === "monitor" ? controlHit && controlHit.parameter : signal;
  if (!telemetry && !control) return null;
  return {
    telemetry: telemetry || null,
    control: control || null,
    telemetry_parameter_id: telemetry && telemetry.id || null,
    control_parameter_id: control && control.id || null,
    telemetry_semantic_role: telemetry && telemetry.semantic_role || null,
    control_semantic_role: control && control.semantic_role || null,
  };
}

function semanticPairSummary(param) {
  if (!param) return "";
  const pair = semanticPairFor(param);
  if (!pair) return "";
  const telemetry = pair.telemetry;
  const control = pair.control;
  const telemetryLabel = telemetry ? (telemetry.name || telemetry.label || `#${telemetry.id}`) : "n/a";
  const controlLabel = control ? (control.name || control.label || `#${control.id}`) : "n/a";
  const telemetryCmd = telemetry ? commandNameFor(telemetry) : "";
  const controlCmd = control ? commandNameFor(control) : "";
  const uLabel = (u) => {
    const l = unitLabel(u);
    if (!l) return "unitless";
    if (l === "degC") return "Degree Celsius";
    return l;
  };
  const telemetryUnit = telemetry && telemetry.unit ? ` ${uLabel(telemetry.unit)}` : "";
  const controlUnit = control && control.unit ? ` ${uLabel(control.unit)}` : "";
  return `telemetry ${telemetryLabel}${telemetryUnit}${telemetryCmd ? ` · ${telemetryCmd}` : ""} · telecommand ${controlLabel}${controlUnit}${controlCmd ? ` · ${controlCmd}` : ""}`;
}

const DEVICES_BASE = [
  {
    id: "tec-76",
    label: "Bus A · TEC SN76",
    endpoint: "serial+can:can0/0x4c",
    address: 76,
    transport: "serial MeCom over CANopen",
    routes: [
      { kind: "hot", label: "Kvaser USB CAN", detail: "primary" },
      { kind: "warm", label: "USB FTDI RS485", detail: "serial bridge" },
      { kind: "warm", label: "PiXtend CAN", detail: "fallback bus" },
    ],
  },
  {
    id: "tec-75",
    label: "Bus A · TEC SN75",
    endpoint: "serial+can:can0/0x4b",
    address: 75,
    transport: "serial MeCom over CANopen",
    routes: [
      { kind: "hot", label: "Kvaser USB CAN", detail: "primary" },
      { kind: "warm", label: "USB FTDI RS485", detail: "serial bridge" },
      { kind: "warm", label: "PiXtend CAN", detail: "fallback bus" },
    ],
  },
  {
    id: "tec-81",
    label: "Bus A · TEC SN81",
    endpoint: "serial+can:can0/0x51",
    address: 81,
    transport: "serial MeCom over CANopen",
    routes: [
      { kind: "hot", label: "Kvaser USB CAN", detail: "primary" },
      { kind: "warm", label: "USB FTDI RS485", detail: "serial bridge" },
      { kind: "warm", label: "PiXtend CAN", detail: "fallback bus" },
    ],
  },
  {
    id: "tec-84",
    label: "Bus A · TEC SN84",
    endpoint: "serial+can:can0/0x54",
    address: 84,
    transport: "serial MeCom over CANopen",
    routes: [
      { kind: "hot", label: "Kvaser USB CAN", detail: "primary" },
      { kind: "warm", label: "USB FTDI RS485", detail: "serial bridge" },
      { kind: "warm", label: "PiXtend CAN", detail: "fallback bus" },
    ],
  },
];

function inferTransportFromEndpoint(endpoint) {
  const value = String(endpoint || "");
  if (value.startsWith("serial+can:")) return "serial+can";
  if (value.startsWith("serial:")) return "serial";
  if (value.startsWith("canopen:")) return "canopen";
  if (value.startsWith("can:")) return "can";
  if (value.startsWith("tcp:")) return "tcp";
  return "";
}

function routeLabelFromRole(role) {
  if (role === "hot") return "Hot path";
  if (role === "warm") return "Warm standby";
  if (role === "fallback") return "Fallback";
  return "Route";
}

function normalizeRouteCandidate(route, activeRoute?, device?) {
  const rawRole = String(route && (route.kind || route.role) || "").toLowerCase();
  const activeEndpoint = activeRoute && activeRoute.endpoint;
  const active = Boolean(route && route.active) || Boolean(activeEndpoint && route && route.endpoint === activeEndpoint);
  const role = ["hot", "warm", "fallback"].includes(rawRole) ? rawRole : (active ? "hot" : "warm");
  const endpoint = route && route.endpoint || device && device.endpoint || "";
  const transport = route && route.transport || inferTransportFromEndpoint(endpoint);
  const state = route && route.state || (active ? "active" : (role === "hot" ? "ready" : "standby"));
  return {
    ...(route || {}),
    kind: role,
    role,
    label: route && (route.label || route.name) || routeLabelFromRole(role),
    detail: route && route.detail || [state, transport, endpoint].filter(Boolean).join(" · "),
    endpoint,
    transport,
    state,
    active,
  };
}

function normalizeDeviceView(device) {
  const base = DEVICES_BASE.find((d) => d.id === device.id) || {};
  const merged = { ...base, ...device, bound: device.bound !== false, last_error: device.last_error || "" };
  const activeRaw = device.active_route || device.activeRoute || null;
  const activeRoute = activeRaw ? normalizeRouteCandidate(activeRaw, null, merged) : null;
  const rawRoutes = Array.isArray(device.route_candidates)
    ? device.route_candidates
    : (Array.isArray(device.routeCandidates) ? device.routeCandidates : (Array.isArray(device.routes) ? device.routes : []));
  const routes = (rawRoutes.length ? rawRoutes : (activeRoute ? [activeRoute] : (base.routes || [])))
    .map((route) => normalizeRouteCandidate(route, activeRoute, merged));
  if (activeRoute && !routes.some((route) => route.endpoint === activeRoute.endpoint && route.role === activeRoute.role)) {
    routes.unshift(activeRoute);
  }
  return {
    ...merged,
    third_party_power_control_enabled: Boolean(merged.third_party_power_control_enabled || merged.thirdPartyPowerControlEnabled),
    routes,
    active_route: activeRoute || routes.find((route) => route.active) || null,
    route_candidates: routes,
  };
}

function mockGatewaySettings() {
  const settings = loadSettings();
  return {
    bridge: {
      default_transport: settings.bridgeDefaultTransport,
      fallback_transport: settings.bridgeFallbackTransport,
      route_selection: settings.bridgeRouteSelection,
      address_zero: settings.bridgeAddressZero,
    },
    routes: DEVICES_BASE.reduce((out, device) => {
      const view = normalizeDeviceView(device);
      (view.route_candidates || []).forEach((route) => {
        out.push({
          device_id: view.id,
          address: view.address,
          role: route.role,
          name: route.name || route.label || "",
          endpoint: route.endpoint || view.endpoint || "",
          transport: route.transport || "",
          state: route.state || "",
          active: Boolean(route.active),
        });
      });
      return out;
    }, []),
  };
}

function serialForDevice(deviceId, devices = DEVICES_BASE) {
  const device = (Array.isArray(devices) ? devices : []).find((d) => d.id === deviceId) || DEVICES_BASE.find((d) => d.id === deviceId);
  const direct = device && (device.serial || device.serial_number || device.sn);
  if (direct) return String(direct);
  const text = [device && device.label, deviceId].filter(Boolean).join(" ");
  const match = text.match(/\bSN\s*-?\s*([0-9A-Za-z]+)/i);
  return match ? `SN${match[1]}` : undefined;
}

function channelAliasTarget(deviceId, instance, channel?, devices = DEVICES_BASE) {
  const inst = Number(instance);
  return {
    kind: "channel",
    device_id: String(deviceId || channel && channel.device_id || "").trim(),
    serial: channel && channel.serial || serialForDevice(deviceId || channel && channel.device_id, devices),
    channel: Number.isFinite(inst) ? inst : instance,
    instance: Number.isFinite(inst) ? inst : instance,
  };
}

function loadChannelAliasBundle() {
  return loadSemanticOverlay({ namespace: CHANNEL_ALIAS_NAMESPACE });
}

function saveChannelAliasBundle(bundle) {
  const saved = saveSemanticOverlay(normalizeSemanticOverlayBundle(bundle, CHANNEL_ALIAS_NAMESPACE), { namespace: CHANNEL_ALIAS_NAMESPACE });
  mock.channels = normalizeChannels(mock.channels, live.active && live.devices ? live.devices : DEVICES_BASE);
  mock.listeners.forEach((fn) => fn());
  return saved;
}

function stripChannelOverlayFields(channel) {
  if (!channel || typeof channel !== "object") return channel;
  const {
    alias,
    nickname,
    custom_label,
    fixture_note,
    user_overlay_note,
    semantic_overlay,
    ...rest
  } = channel;
  return rest;
}

function applyChannelAliasOverlay(channels, devices = DEVICES_BASE) {
  const bundle = loadChannelAliasBundle();
  const entries = new Map((bundle.entries || []).map((entry) => [semanticOverlayTargetKey(entry.target), entry]));
  return (channels || []).map((channel) => {
    const target = channelAliasTarget(channel.device_id, channel.instance, channel, devices);
    const entry = entries.get(semanticOverlayTargetKey(target));
    if (!entry) {
      return {
        ...channel,
        serial: channel.serial || target.serial,
      };
    }
    const alias = entry.alias || entry.label || "";
    return {
      ...channel,
      serial: channel.serial || target.serial,
      alias: alias || undefined,
      nickname: alias || undefined,
      custom_label: entry.label || undefined,
      fixture_note: entry.fixture_note || undefined,
      user_overlay_note: entry.note || undefined,
      semantic_overlay: entry,
    };
  });
}

function channelDisplayLabel(channel, opts?) {
  const raw = [channel && channel.device_id || "device", `ch${channel && channel.instance || "?"}`].join(" ");
  if (opts && opts.includeAlias === false) return raw;
  const alias = channel && (channel.alias || channel.nickname);
  return alias ? `${raw} · ${alias}` : raw;
}

function setChannelAlias(deviceId, instance, patch = {}) {
  const devices = live.active && live.devices ? live.devices : DEVICES_BASE;
  const channels = normalizeChannels(mock.channels, devices);
  const channel = channels.find((ch) => ch.device_id === deviceId && Number(ch.instance) === Number(instance));
  const target = channelAliasTarget(deviceId, instance, channel, devices);
  const bundle = loadChannelAliasBundle();
  const current = overlayEntryForTarget(bundle, target) || {};
  const alias = String(patch.alias ?? current.alias ?? "").trim();
  const note = String(patch.note ?? current.note ?? "").trim();
  const fixtureNote = String(patch.fixture_note ?? patch.fixtureNote ?? current.fixture_note ?? "").trim();
  if (!alias && !note && !fixtureNote && patch.hidden === undefined && !(patch.tags && patch.tags.length)) {
    return saveChannelAliasBundle(removeSemanticOverlayEntry(bundle, target, CHANNEL_ALIAS_NAMESPACE));
  }
  return saveChannelAliasBundle(upsertSemanticOverlayEntry(bundle, {
    ...current,
    target,
    alias,
    note,
    fixture_note: fixtureNote,
    source: patch.source || current.source || "meerstetter-go-ui",
    author: patch.author || current.author || loadSettings().holder || "operator",
    hidden: patch.hidden !== undefined ? patch.hidden : current.hidden,
    tags: patch.tags || current.tags,
    meta: {
      ...(current.meta || {}),
      device_id: deviceId,
      instance: Number(instance),
      serial: target.serial,
    },
  }, CHANNEL_ALIAS_NAMESPACE));
}

function clearChannelAlias(deviceId, instance) {
  const bundle = loadChannelAliasBundle();
  const target = channelAliasTarget(deviceId, instance, null, live.active && live.devices ? live.devices : DEVICES_BASE);
  return saveChannelAliasBundle(removeSemanticOverlayEntry(bundle, target, CHANNEL_ALIAS_NAMESPACE));
}

function exportChannelAliases() {
  return loadChannelAliasBundle();
}

function importChannelAliases(input) {
  const parsed = typeof input === "string" ? JSON.parse(input) : input;
  return saveChannelAliasBundle(normalizeSemanticOverlayBundle(parsed, CHANNEL_ALIAS_NAMESPACE));
}

const CHANNELS_PER_DEVICE = 4;
const CHANNEL_ROLE_OVERRIDES = {
  "tec-75/1": {
    role: "temp",
    label: "Top-front-right temperature controller",
    user_note: "SN75 - top-right fixture - top-front-right heat-sink thermal zone. Target temperature 25 C.",
  },
  "tec-75/2": {
    role: "supply",
    label: "Top-front-right power supply",
    user_note: "SN75 - top-right fixture - power supply for the top-front-right test spot.",
  },
  "tec-75/3": {
    role: "temp",
    label: "Top-back-right temperature controller",
    user_note: "SN75 - top-right fixture - top-back-right heat-sink thermal zone. Target temperature 25 C.",
  },
  "tec-75/4": {
    role: "supply",
    label: "Top-back-right power supply",
    user_note: "SN75 - top-right fixture - power supply for the top-back-right test spot.",
  },
  "tec-76/1": {
    role: "temp",
    label: "Bottom-front-right cascade controller",
    user_note: "SN76 - bottom-right quadrant - front-right-bottom heat-sink thermal zone. HR1 is cascade control temperature; LR1/LR2 monitor the heat sink and feed the cascade input. Target temperature 25 C.",
  },
  "tec-76/2": {
    role: "supply",
    label: "Bottom-front-right power supply",
    user_note: "SN76 - bottom-right quadrant - power supply for the front-right-bottom test spot.",
  },
  "tec-76/3": {
    role: "temp",
    label: "Bottom-back-right temperature controller",
    user_note: "SN76 - bottom-right quadrant - back-right-bottom heat-sink thermal zone. Target temperature 25 C.",
  },
  "tec-76/4": {
    role: "supply",
    label: "Bottom-back-right power supply",
    user_note: "SN76 - bottom-right quadrant - power supply for the back-right-bottom test spot.",
  },
  "tec-81/1": {
    role: "temp",
    label: "Top-front-left temperature controller",
    user_note: "SN81 - inferred top-left fixture - top-front-left heat-sink thermal zone. Target temperature 25 C.",
  },
  "tec-81/2": {
    role: "supply",
    label: "Top-front-left power supply",
    user_note: "SN81 - inferred top-left fixture - power supply for the top-front-left test spot.",
  },
  "tec-81/3": {
    role: "temp",
    label: "Top-back-left temperature controller",
    user_note: "SN81 - inferred top-left fixture - top-back-left heat-sink thermal zone. Target temperature 25 C.",
  },
  "tec-81/4": {
    role: "supply",
    label: "Top-back-left power supply",
    user_note: "SN81 - inferred top-left fixture - power supply for the top-back-left test spot.",
  },
  "tec-84/1": {
    role: "temp",
    label: "Bottom-front-left temperature controller",
    user_note: "SN84 - inferred bottom-left quadrant - front-left-bottom heat-sink thermal zone. Target temperature 25 C.",
  },
  "tec-84/2": {
    role: "supply",
    label: "Bottom-front-left power supply",
    user_note: "SN84 - inferred bottom-left quadrant - power supply for the front-left-bottom test spot.",
  },
  "tec-84/3": {
    role: "temp",
    label: "Bottom-back-left temperature controller",
    user_note: "SN84 - inferred bottom-left quadrant - back-left-bottom heat-sink thermal zone. Target temperature 25 C.",
  },
  "tec-84/4": {
    role: "supply",
    label: "Bottom-back-left power supply",
    user_note: "SN84 - inferred bottom-left quadrant - power supply for the back-left-bottom test spot.",
  },
};

function defaultChannelFor(deviceId, instance) {
  const key = `${deviceId}/${instance}`;
  const hasOverride = Object.prototype.hasOwnProperty.call(CHANNEL_ROLE_OVERRIDES, key);
  const override = CHANNEL_ROLE_OVERRIDES[key] || {};
  const role = override.role || (instance % 2 === 0 ? "supply" : "temp");
  return {
    device_id: deviceId,
    instance,
    role,
    role_source: override.role_source || (hasOverride ? "config" : "local-assumption"),
    label: override.label || (role === "supply" ? `Supply ch${instance}` : `TEC ch${instance}`),
    user_note: override.user_note || "",
    hasCascade: override.hasCascade ?? false,
  };
}

const DEFAULT_CHANNELS = DEVICES_BASE.flatMap((d) =>
 Array.from({ length: CHANNELS_PER_DEVICE }, (_, idx) => defaultChannelFor(d.id, idx + 1))
);

function channelCountForDevice(device) {
  const raw = Number(device && (device.channel_count ?? device.channelCount));
  if (!Number.isFinite(raw) || raw <= 0) return CHANNELS_PER_DEVICE;
  return Math.max(1, Math.min(255, Math.floor(raw)));
}

function normalizeChannels(channels, devices = DEVICES_BASE, opts = {}) {
  const byKey = new Map();
  const deviceById = new Map((Array.isArray(devices) ? devices : []).map((d) => [d.id, d]));
  const deviceRank = new Map((Array.isArray(devices) && devices.length ? devices : DEVICES_BASE).map((d, idx) => [d.id, idx]));
  (Array.isArray(channels) ? channels : []).forEach((ch) => {
    if (!ch || !ch.device_id || !Number.isFinite(Number(ch.instance))) return;
    const inst = Number(ch.instance);
    const maxInst = channelCountForDevice(deviceById.get(ch.device_id));
    if (inst < 1 || inst > maxInst) return;
    const base = defaultChannelFor(ch.device_id, inst);
    const dev = deviceById.get(ch.device_id);
    const powerControlEnabled = Boolean(
      ch.third_party_power_control_enabled
      || ch.thirdPartyPowerControlEnabled
      || dev && (dev.third_party_power_control_enabled || dev.thirdPartyPowerControlEnabled)
    );
    byKey.set(`${ch.device_id}/${inst}`, {
      ...base,
      ...ch,
      instance: inst,
      endpoint: dev && dev.endpoint,
      third_party_power_control_enabled: powerControlEnabled,
    });
  });
  (Array.isArray(devices) && devices.length ? devices : DEVICES_BASE).forEach((d) => {
    for (let inst = 1; inst <= channelCountForDevice(d); inst++) {
      const key = `${d.id}/${inst}`;
      if (!byKey.has(key)) {
        byKey.set(key, {
          ...defaultChannelFor(d.id, inst),
          endpoint: d.endpoint,
          third_party_power_control_enabled: Boolean(d.third_party_power_control_enabled || d.thirdPartyPowerControlEnabled),
        });
      }
    }
  });
  const sorted = Array.from(byKey.values()).sort((a, b) => {
    const ar = deviceRank.has(a.device_id) ? deviceRank.get(a.device_id) : 9999;
    const br = deviceRank.has(b.device_id) ? deviceRank.get(b.device_id) : 9999;
    return ar - br || String(a.device_id).localeCompare(String(b.device_id)) || a.instance - b.instance;
  });
  return opts.withoutOverlay ? sorted.map(stripChannelOverlayFields) : applyChannelAliasOverlay(sorted, devices);
}

function loadChannels() {
  try {
    const raw = JSON.parse(localStorage.getItem(LS_CHANNELS) || "null");
    const version = localStorage.getItem(LS_CHANNELS_VERSION);
    if (raw && raw.length && version === CHANNEL_METADATA_VERSION) return normalizeChannels(raw);
  } catch (_) {}
  return normalizeChannels(DEFAULT_CHANNELS);
}
function saveChannels(channels) {
  const devices = live.active && live.devices ? live.devices : DEVICES_BASE;
  const normalized = normalizeChannels((Array.isArray(channels) ? channels : []).map(stripChannelOverlayFields), devices, { withoutOverlay: true });
  localStorage.setItem(LS_CHANNELS, JSON.stringify(normalized));
  localStorage.setItem(LS_CHANNELS_VERSION, CHANNEL_METADATA_VERSION);
  mock.channels = applyChannelAliasOverlay(normalized, devices);
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

  mock.channels = normalizeChannels(mock.channels, DEVICES_BASE);

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
        outputStageT: 24.0 + (Math.random() - 0.5) * 0.5,
        setI: 0.8,
        setV: 2.0,
        currentLimit: 6.5,
        voltageLimit: 12.0,
        currentErrorThreshold: 7.5,
        voltageErrorThreshold: 14.0,
        outputEnable: 1,
        opMode: s.opMode ?? 1,
        cascadeEnable: ch.hasCascade ? 1 : 0,
        cascadeSelection: 0,
        cascadeSyncChannel: 0,
        cascadeTargetT: s.target ?? 25.0,
        bound: s.bound !== false,
        rejectWrites: !!s.rejectWrites,
        jitter: s.jitter ?? 0.03,
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
        outputStageT: 24.5 + (Math.random() - 0.5) * 0.5,
        currentLimit: Math.max(1.0, (s.setI ?? 0.5) * 1.5),
        voltageLimit: Math.max(6.0, (s.setV ?? 5.0) * 1.25),
        currentErrorThreshold: Math.max(1.5, (s.setI ?? 0.5) * 1.8),
        voltageErrorThreshold: Math.max(8.0, (s.setV ?? 5.0) * 1.4),
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
      s.outputStageT += (s.sinkT + 2.0 - s.outputStageT) * 0.1 + (Math.random() - 0.5) * 0.04;
      const dT = s.targetT - s.objectT;
      s.outputI = Math.max(0, Math.min(s.outputEnable ? 6.5 : 0,
        0.7 + Math.abs(dT) * 1.6 + (Math.random() - 0.5) * 0.15));
      s.outputV = 1.4 + Math.abs(dT) * 0.6 + (Math.random() - 0.5) * 0.08;
      s.stable  = Math.abs(dT) < 0.05 ? 1 : 0;
    } else {
      s.actualV = s.outputEnable ? (s.setV - 0.02 + (Math.random() - 0.5) * 0.03) : 0;
      s.actualI = s.outputEnable ? (s.setI * 0.94 + (Math.random() - 0.5) * 0.01) : 0;
      s.sinkT   += (Math.random() - 0.5) * 0.05;
      s.outputStageT += (s.sinkT + 2.5 - s.outputStageT) * 0.08 + (Math.random() - 0.5) * 0.04;
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
  { dt: 41000, st: "completed", tgt: "tec-75", inst: 2, p: 2021, v: 5.1, prev: 5.0, holder: "design-claude" },
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
  channelAliases: exportChannelAliases,
  setChannelAlias,
  clearChannelAlias,
  exportChannelAliases,
  importChannelAliases,
  channelDisplayLabel,
  setChannelRole(deviceId, instance, role, opts?) {
    const idx = mock.channels.findIndex((c) => c.device_id === deviceId && c.instance === instance);
    if (idx >= 0) {
      mock.channels[idx] = { ...mock.channels[idx], role, role_source: (opts && opts.role_source) || "local-assumption" };
    } else {
      const base = defaultChannelFor(deviceId, instance);
      mock.channels.push({ ...base, role, role_source: "local-assumption" });
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
    const now = Date.now();
    const ch = mock.channelState[ckey(deviceId, inst)];
    if (!ch) return { value: null, quality: "missing", at: null, age_ms: null };
    if (!ch.bound) return { value: null, quality: "unreachable", at: now, age_ms: 0 };
    let v;
    if (ch.role === "temp") {
      if (paramId === 52200) {
        if (ch.cascadeT === null) return { value: null, quality: "missing", at: now, age_ms: 0 };
        return { value: ch.cascadeT, quality: "ok", at: now, age_ms: 0 };
      }
      const map = {
        1000: ch.objectT, 1001: ch.sinkT, 1200: ch.stable,
        1020: ch.outputI, 1021: ch.outputV, 1022: ch.outputI * ch.outputV, 40000: ch.outputStageT,
        1500: 12.0, 1501: 0.4,
        2010: ch.outputEnable,
        2020: ch.setI, 2021: ch.setV,
        2030: ch.currentLimit, 2031: ch.voltageLimit,
        2032: ch.currentErrorThreshold, 2033: ch.voltageErrorThreshold,
        2040: ch.opMode,
        3000: ch.targetT,
        53120: ch.cascadeEnable, 53121: ch.cascadeSelection,
        53122: ch.cascadeSyncChannel, 53123: ch.cascadeTargetT,
        104:  ch.deviceStatus, 105: 0, 109: ch.savePending,
        4000: 0, 4010: 60 * 60 * 24 * 12 + Math.random() * 1000,
        100:  150, 101: 2, 102: parseInt(deviceId.split("-")[1]) + 100, 103: 3.21,
      };
      v = map[paramId];
    } else {
      const map = {
        1001: ch.sinkT,
        1020: ch.actualI, 1021: ch.actualV, 1022: ch.actualV * ch.actualI, 40000: ch.outputStageT,
        1500: 12.0, 1501: 0.4,
        2020: ch.setI, 2021: ch.setV,
        2030: ch.currentLimit, 2031: ch.voltageLimit,
        2032: ch.currentErrorThreshold, 2033: ch.voltageErrorThreshold,
        2010: ch.outputEnable, 2040: ch.opMode,
        104:  ch.deviceStatus, 105: 0, 109: ch.savePending,
        4000: 0, 4010: 60 * 60 * 24 * 12 + Math.random() * 1000,
        100:  150, 101: 2, 102: parseInt(deviceId.split("-")[1]) + 100, 103: 3.21,
      };
      v = map[paramId];
    }
    if (v === undefined) return { value: null, quality: "missing", at: now, age_ms: 0 };
    if (typeof v === "number" && Number.isNaN(v)) return { value: null, quality: "nan", at: now, age_ms: 0 };
    return { value: v, quality: "ok", at: now, age_ms: 0 };
  },
  setpoint(deviceId, paramId, instance?) {
    const ch = mock.channelState[ckey(deviceId, instance || 1)];
    if (!ch) return null;
    if (ch.role === "supply") {
      if (paramId === 2020) return ch.setI;
      if (paramId === 2021) return ch.setV;
      if (paramId === 2030) return ch.currentLimit;
      if (paramId === 2031) return ch.voltageLimit;
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
      if (param === 2020) ch.setI = value;
      if (param === 2021) ch.setV = value;
      if (param === 2030) ch.currentLimit = value;
      if (param === 2031) ch.voltageLimit = value;
      if (param === 2032) ch.currentErrorThreshold = value;
      if (param === 2033) ch.voltageErrorThreshold = value;
      if (param === 53120) ch.cascadeEnable = value;
      if (param === 53121) ch.cascadeSelection = value;
      if (param === 53122) ch.cascadeSyncChannel = value;
      if (param === 53123) ch.cascadeTargetT = value;
    } else {
      if (param === 2021) ch.setV = value;
      if (param === 2020) ch.setI = value;
      if (param === 2030) ch.currentLimit = value;
      if (param === 2031) ch.voltageLimit = value;
      if (param === 2032) ch.currentErrorThreshold = value;
      if (param === 2033) ch.voltageErrorThreshold = value;
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
  gatewaySettings() {
    return Promise.resolve(mockGatewaySettings());
  },
  unitLabel,
  formatWithUnit,
  formatValue(v, unit, paramId?) {
    if (v === null || v === undefined) return "—";
    if (typeof v === "number") {
      if (paramId === 2010) return ({0: "OFF", 1: "ON", 2: "Live OFF", 3: "HW ctrl"}[v]) ?? String(v);
      if (paramId === 1200) return v ? "Stable" : "Drifting";
      if (paramId === 2040) return ({0: "Off", 1: "TEC", 2: "PSU CV", 3: "PSU CC"}[v]) ?? String(v);
      if (paramId === 53120) return v ? "On" : "Off";
      if (paramId === 104)  return ({0:"Init",1:"Ready",2:"Run",3:"Error",4:"Bootloader"}[v]) ?? String(v);
      if (paramId === 109)  return v ? "Busy" : "Ready";
      const abs = Math.abs(v);
      let digits = 2;
      if (unit === "degC" || unit === "V") digits = 3;
      else if (unit === "A") digits = 3;
      else if (unit === "W") digits = abs < 1 ? 5 : 3;
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
  primaryDeviceId() {
    const devices = DEVICES_BASE.slice();
    return (devices.find((d) => d.id === "tec-76") || devices[0] || {}).id || "";
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
  semanticPairFor,
  semanticPairSummary,
};

// Live gateway adapter
const live = {
  active: false,
  checked: false,
  refreshing: false,
  base: "",
  lastError: "",
  devices: null,
  catalogue: null,
  leases: null,
  values: Object.create(null),
  commands: [],
  commandsUnavailable: false,
  timer: null,
};
const LIVE_LAZY_READ_COOLDOWN_MS = 30_000;
const LIVE_STALE_REFRESH_MS = 60_000;
const liveLazyReadInflight = new Set();
const liveLazyReadLast = new Map();

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
function liveSampleAtMs(entry, now) {
  const rawAt = entry && entry.at;
  const atNumber = Number(rawAt);
  if (Number.isFinite(atNumber)) return atNumber > 1e12 ? atNumber : atNumber * 1000;
  if (typeof rawAt === "string") {
    const parsed = Date.parse(rawAt);
    if (Number.isFinite(parsed)) return parsed;
  }
  const age = Number(entry && entry.age_ms);
  if (Number.isFinite(age)) return now - Math.max(0, age);
  return now;
}
function storeLiveValue(deviceId, entry) {
  const instance = entry.instance || 1;
  const value = entry.value;
  const quality = entry.quality || (value === null || value === undefined ? "missing" : "ok");
  const now = Date.now();
  const at = liveSampleAtMs(entry, now);
  live.values[liveKey(deviceId, entry.id, instance)] = {
    value,
    quality,
    at,
    age_ms: Math.max(0, now - at),
  };
}
function paramsForChannel(role, instance) {
  const ids = role === "supply"
    ? [1020, 1021, 1022, 40000, 2020, 2021, 2030, 2031, 2032, 2033, 2010, 2040, 1001]
    : [1000, 1001, 3000, 52200, 1200, 1020, 1021, 1022, 40000, 2020, 2021, 2030, 2031, 2032, 2033, 53120, 53121, 53122, 53123];
  return ids.map((id) => `${id}:${instance || 1}`).join(",");
}

function queueLiveValueRead(deviceId, paramId, instance?) {
  if (!live.active || !configuredBase()) return;
  const inst = instance || 1;
  const key = liveKey(deviceId, paramId, inst);
  const now = Date.now();
  const last = liveLazyReadLast.get(key) || 0;
  if (liveLazyReadInflight.has(key) || now - last < LIVE_LAZY_READ_COOLDOWN_MS) return;
  liveLazyReadLast.set(key, now);
  liveLazyReadInflight.add(key);
  fetchJSON(`/api/devices/${encodeURIComponent(deviceId)}/read?params=${encodeURIComponent(`${paramId}:${inst}`)}`)
    .then((body) => {
      (body && body.values || []).forEach((entry) => storeLiveValue(deviceId, entry));
    })
    .catch((err: any) => {
      if ((err.status || 0) >= 400) {
        live.values[key] = { value: null, quality: "unreachable", at: Date.now(), age_ms: 0 };
      }
    })
    .finally(() => {
      liveLazyReadInflight.delete(key);
      notify();
    });
}

function liveCatalogueEntries() {
  const liveParams = Array.isArray(live.catalogue) ? live.catalogue : [];
  if (!live.active || liveParams.length === 0) return null;
  const byID = new Map();
  liveParams.forEach((entry) => {
    if (!entry || !Number.isFinite(Number(entry.id))) return;
    const id = Number(entry.id);
    const instance = Number.isFinite(Number(entry.instance)) ? Number(entry.instance) : 1;
    const key = `${id}:${instance}`;
    if (byID.has(key)) return;
    const base = paramById(id) || {};
    const metadata = parseTreeMeta(entry.metadata);
    const treeSource = {
      ...base,
      ...entry,
      tree_path: entry.tree_path || (metadata && metadata.ui_tree_path) || base.tree_path,
      tree_paths: entry.tree_paths || (metadata && metadata.ui_tree_paths) || base.tree_paths,
    };
    const trees = treeSelectionFrom(treeSource, id, base.name || entry.name);
    const merged = normalizeCatalogueEntry(entry, base, id, instance, metadata, trees);
    byID.set(key, merged);
  });
  return Array.from(byID.values());
}

function catalogueSignalKey(entry) {
  return String(Number(entry && entry.id));
}

const LIVE_CATALOGUE_OBSERVED_FIELDS = [
  "name",
  "unit",
  "type",
  "kind",
  "role",
  "group",
  "subgroup",
  "access",
  "metadata",
  "transport_support",
  "readout_priority",
  "preferred_readout",
  "tree_path",
  "tree_paths",
];

function liveCatalogueObserved(entry) {
  const out = {};
  LIVE_CATALOGUE_OBSERVED_FIELDS.forEach((field) => {
    if (entry && entry[field] !== undefined && entry[field] !== null && entry[field] !== "") {
      out[field] = entry[field];
    }
  });
  return out;
}

function mergeLiveCatalogueEntries(full, liveEntries) {
  const byID = new Map();
  (Array.isArray(full) ? full : []).forEach((entry, index) => {
    if (!entry || !Number.isFinite(Number(entry.id))) return;
    byID.set(catalogueSignalKey(entry), {
      ...entry,
      live_instances: [],
      __catalogue_order: index,
    });
  });
  (Array.isArray(liveEntries) ? liveEntries : []).forEach((entry) => {
    if (!entry || !Number.isFinite(Number(entry.id))) return;
    const key = catalogueSignalKey(entry);
    const existing = byID.get(key);
    if (!existing) {
      return;
    }
    const liveInstances = new Set(existing.live_instances || []);
    liveInstances.add(Number(entry.instance) || 1);
    const observed = liveCatalogueObserved(entry);
    const observedList = Array.isArray(existing.live_catalogue_observed)
      ? existing.live_catalogue_observed.slice()
      : [];
    if (Object.keys(observed).length) {
      observedList.push({ instance: Number(entry.instance) || 1, ...observed });
    }
    byID.set(key, {
      ...existing,
      live_instances: Array.from(liveInstances).sort((a, b) => a - b),
      live_catalogue_observed: observedList,
      __catalogue_order: existing.__catalogue_order,
    });
  });
  return Array.from(byID.values())
    .sort((a, b) => (a.__catalogue_order ?? Number.MAX_SAFE_INTEGER) - (b.__catalogue_order ?? Number.MAX_SAFE_INTEGER) || Number(a.id) - Number(b.id))
    .map(({ __catalogue_order, ...entry }) => entry);
}

function activeCatalogue() {
  return mergeLiveCatalogueEntries(CATALOGUE, liveCatalogueEntries());
}

async function refreshLiveReads(devices) {
  const channels = normalizeChannels(mock.channels, devices);
  mock.channels = channels;
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

function normalizeCommandEvent(e) {
  const raw = e || {};
  const targetId = raw.target_id || raw.device_id || "";
  const time = raw.time || new Date().toISOString();
  const unit = raw.unit ?? raw.signal_unit ?? "";
  const result = raw.result || {};
  return {
    ...raw,
    time,
    command_id: raw.command_id || raw.id || `${time}:${targetId}:${raw.param_id ?? ""}:${raw.instance ?? ""}:${raw.status ?? ""}`,
    target_id: targetId,
    unit,
    status: raw.status || "completed",
    // Field mapping for UI components
    paramId: raw.param_id,
    requestedValue: raw.requested_value,
    confirmedValue: result.confirmed_value !== undefined ? result.confirmed_value : raw.confirmed_value,
    prevValue: result.prev_value !== undefined ? result.prev_value : raw.prev_value,
    confirmedMatched: result.readback_matched !== undefined ? result.readback_matched : raw.readback_matched,
  };
}
function sameCommandEvent(a, b) {
  if (!a || !b) return false;
  if (a.command_id && b.command_id && a.command_id === b.command_id) return true;
  const aTime = new Date(a.time).getTime();
  const bTime = new Date(b.time).getTime();
  return a.target_id === b.target_id
    && Number(a.instance || 1) === Number(b.instance || 1)
    && Number(a.param_id) === Number(b.param_id)
    && String(a.requested_value ?? "") === String(b.requested_value ?? "")
    && Number.isFinite(aTime)
    && Number.isFinite(bTime)
    && Math.abs(aTime - bTime) < 5000;
}
function mergeLiveCommand(event) {
  const normalized = normalizeCommandEvent(event);
  live.commands = [normalized, ...(live.commands || []).filter((entry) => !sameCommandEvent(entry, normalized))]
    .sort((a, b) => new Date(b.time).getTime() - new Date(a.time).getTime())
    .slice(0, 80);
  notify();
  return normalized;
}

async function refreshLiveCommands() {
  const body = await fetchJSON("/api/commands?limit=80");
  const events = ((body && body.commands) || []).map(normalizeCommandEvent);
  live.commands = events.sort((a, b) => new Date(b.time).getTime() - new Date(a.time).getTime());
  live.commandsUnavailable = false;
}

async function refreshLiveOnce() {
  const base = configuredBase();
  if (!base || live.refreshing) return;
  live.refreshing = true;
  live.base = base;
  try {
    const devicesBody = await fetchJSON("/api/devices");
    const catalogueBody = await fetchJSON("/api/catalogue").catch(() => ({ parameters: [] }));
    const leasesBody = await fetchJSON("/api/leases").catch(() => ({ leases: [] }));
    const commandsPromise = refreshLiveCommands().catch(() => { live.commandsUnavailable = true; });
    const devices = (devicesBody && devicesBody.devices) || [];
    live.devices = devices.map(normalizeDeviceView);
    live.catalogue = (catalogueBody && catalogueBody.parameters) || [];
    mock.channels = normalizeChannels(mock.channels, live.devices);
    live.leases = (leasesBody && leasesBody.leases) || [];
    await refreshLiveReads(live.devices);
    await commandsPromise;
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
    live.catalogue = null;
    live.leases = null;
    live.values = Object.create(null);
    liveLazyReadInflight.clear();
    liveLazyReadLast.clear();
    live.commands = [];
    live.commandsUnavailable = false;
    live.base = base;
  }
  if (!live.timer) {
    refreshLiveOnce();
    live.timer = setInterval(refreshLiveOnce, 2500);
  }
}
function liveDeviceById(deviceId) {
  const liveDevice = (live.devices || []).find((d) => d.id === deviceId);
  const baseDevice = DEVICES_BASE.find((d) => d.id === deviceId);
  if (!liveDevice) return baseDevice;
  return normalizeDeviceView({ ...(baseDevice || {}), ...liveDevice });
}

export const MecomAPI = {
  ...mockAPIImpl,
  catalogue() {
    ensureLivePolling();
    return activeCatalogue();
  },
  paramById(id, instance?) {
    const catalogue = activeCatalogue();
    if (instance !== undefined && instance !== null) {
      const keyed = catalogue.find((p) => `${p.id}:${p.instance || 1}` === `${id}:${instance}`);
      if (keyed) return keyed;
    }
    return catalogue.find((p) => p.id === id);
  },
  catalogueFor(role) {
    return activeCatalogue().filter((p) => !p.applicableModes || p.applicableModes.includes(role) || p.applicableModes.includes("any"));
  },
  counterpartIdsFor,
  counterpartSignalsFor,
  semanticCounterpartFor,
  semanticPairFor,
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
  async gatewaySettings() {
    ensureLivePolling();
    if (!live.active && !explicitBase()) return mockAPIImpl.gatewaySettings();
    try {
      return await fetchJSON("/api/settings");
    } catch (err) {
      if (!explicitBase()) return mockAPIImpl.gatewaySettings();
      throw err;
    }
  },
  devices() {
    ensureLivePolling();
    if (live.active && live.devices) {
      return live.devices.map((d) => liveDeviceById(d.id));
    }
    return mockAPIImpl.devices();
  },
  channels() {
    ensureLivePolling();
    if (live.active && live.devices) {
      mock.channels = normalizeChannels(mock.channels, live.devices);
      return mock.channels.slice();
    }
    return mockAPIImpl.channels();
  },
  leases() {
    ensureLivePolling();
    return live.active && live.leases ? live.leases.slice() : mockAPIImpl.leases();
  },
  commandEvents() {
    ensureLivePolling();
    if (live.active && !live.commandsUnavailable) return (live.commands || []).slice(0, 80);
    return mockAPIImpl.commandEvents();
  },
  primaryDeviceId() {
    ensureLivePolling();
    const devices = live.active && live.devices && live.devices.length ? live.devices : DEVICES_BASE;
    return ((devices.find((d) => d.id === "tec-76") || devices[0] || {}).id) || "";
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
  async readFreshValue(deviceId, paramId, instance?) {
    ensureLivePolling();
    const inst = instance ?? 1;
    if (!live.active && !explicitBase()) return mockAPIImpl.readValue(deviceId, paramId, inst);
    try {
      const body = await fetchJSON(`/api/devices/${encodeURIComponent(deviceId)}/read?params=${encodeURIComponent(`${paramId}:${inst}`)}`);
      (body && body.values || []).forEach((entry) => storeLiveValue(deviceId, entry));
      notify();
      return MecomAPI.readValue(deviceId, paramId, inst);
    } catch (err: any) {
      if (!live.active && !explicitBase()) return mockAPIImpl.readValue(deviceId, paramId, inst);
      live.values[liveKey(deviceId, paramId, inst)] = { value: null, quality: "unreachable", at: Date.now(), age_ms: 0 };
      notify();
      throw err;
    }
  },
  async confirmWriteValue(deviceId, paramId, instance, expected, opts?) {
    const readback = await MecomAPI.readFreshValue(deviceId, paramId, instance);
    const actual = readback && readback.value;
    const numeric = typeof actual === "number" && typeof expected === "number";
    const tolerance = opts && Number.isFinite(Number(opts.tolerance)) ? Number(opts.tolerance) : (numeric ? Math.max(1e-6, Math.abs(expected) * 1e-6) : 0);
    const matched = numeric ? Number.isFinite(actual) && Math.abs(actual - expected) <= tolerance : String(actual) === String(expected);
    return {
      ...readback,
      expected,
      matched,
      status: matched ? "confirmedMatched" : "readback mismatch",
    };
  },
  readValue(deviceId, paramId, instance?) {
    ensureLivePolling();
    const inst = instance ?? 1;
    if (live.active) {
      const now = Date.now();
      const v = live.values[liveKey(deviceId, paramId, inst)];
      if (v) {
        const at = Number.isFinite(Number(v.at)) ? Number(v.at) : now;
        const age = Math.max(0, now - at);
        if (age > LIVE_STALE_REFRESH_MS) queueLiveValueRead(deviceId, paramId, inst);
        return { value: v.value, quality: v.quality || "ok", at, age_ms: age };
      }
      queueLiveValueRead(deviceId, paramId, inst);
      return { value: null, quality: "missing", at: null, age_ms: null };
    }
    return mockAPIImpl.readValue(deviceId, paramId, inst);
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
      const signal = MecomAPI.paramById(param, inst) || MecomAPI.paramById(param) || {};
      const lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      const submittedCommand = mergeLiveCommand({
        command_id: body && (body.command_id || body.id),
        time: (body && body.time) || new Date().toISOString(),
        target_id: deviceId,
        instance: inst,
        param_id: param,
        signal_name: signal.name || ("#" + param),
        unit: signal.unit || "",
        prev_value: (body && body.result && body.result.prev_value) !== undefined ? body.result.prev_value : prev,
        requested_value: req.arguments && req.arguments.value,
        lease_holder: lease && lease.holder,
        status: (body && body.status) || "completed",
      });
      await refreshLiveCommands().then(() => {
        if (!(live.commands || []).some((entry) => sameCommandEvent(entry, submittedCommand))) {
          mergeLiveCommand(submittedCommand);
        }
      }).catch(() => {
        live.commandsUnavailable = true;
        recordCommand({ deviceId, instance: inst, paramId: param, value: req.arguments && req.arguments.value, prev, status: (body && body.status) || "completed", leaseHolder: lease && lease.holder });
      });
      refreshLiveOnce();
      return body;
    } catch (err: any) {
      if (live.commandsUnavailable) {
        const lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
        recordCommand({ deviceId, instance: inst, paramId: param, value: req.arguments && req.arguments.value, prev, status: (err.status === 423 || err.status === 409) ? "rejected" : "failed", leaseHolder: lease && lease.holder, errMessage: err.message, httpStatus: err.status });
      } else {
        await refreshLiveCommands().catch(() => {
          live.commandsUnavailable = true;
          const lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
          recordCommand({ deviceId, instance: inst, paramId: param, value: req.arguments && req.arguments.value, prev, status: (err.status === 423 || err.status === 409) ? "rejected" : "failed", leaseHolder: lease && lease.holder, errMessage: err.message, httpStatus: err.status });
        });
      }
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
  async graphTile(tileId, level, series, opts?) {
    ensureLivePolling();
    const params = new URLSearchParams();
    (Array.isArray(series) ? series : []).forEach((item) => {
      const deviceId = item.device_id || item.deviceId || item.options?.device_id || item.options?.deviceId;
      const paramId = item.param_id || item.paramId || item.options?.param_id || item.options?.paramId;
      const instance = item.instance || item.options?.instance || 1;
      if (!deviceId || !Number.isFinite(Number(paramId))) return;
      params.append("series", `${deviceId}:${Number(paramId)}:${Number(instance) || 1}`);
    });
    if (opts && opts.t0 && opts.t1) {
      params.append("t0", opts.t0);
      params.append("t1", opts.t1);
    }
    const qs = params.toString();
    const path = `/api/graph/tiles/${encodeURIComponent(tileId || "graph-tile")}/${encodeURIComponent(level || "three_day")}${qs ? "?" + qs : ""}`;
    const headers = {};
    if (opts && opts.format === "arrow") {
      headers["X-Format"] = "arrow";
      return fetch(path, { headers }).then((res) => {
        if (!res.ok) throw new Error(`graph tile arrow failed: ${res.status}`);
        return res.arrayBuffer();
      });
    }
    return fetchJSON(path);
  },
  async fetchAvailability() {
    ensureLivePolling();
    return fetchJSON("/api/graph/availability");
  },
  subscribe(fn) {
    ensureLivePolling();
    return mockAPIImpl.subscribe(fn);
  },
};

export default MecomAPI;
