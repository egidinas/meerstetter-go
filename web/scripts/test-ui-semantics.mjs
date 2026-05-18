import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "..");
const hero = await readFile(path.join(root, "src", "views", "hero.tsx"), "utf8");
const mainView = await readFile(path.join(root, "src", "views", "main.tsx"), "utf8");
const api = await readFile(path.join(root, "src", "api", "mecom.ts"), "utf8");
const assignments = await readFile(path.join(root, "src", "views", "assignments.ts"), "utf8");

assert.match(
  hero,
  /PARAM_FIXED_VOLTAGE\s*=\s*2021/,
  "supply voltage command must be represented as writable parameter 2021",
);
assert.match(
  hero,
  /PARAM_CURRENT_LIMIT\s*=\s*2030/,
  "supply current limit must be represented as writable parameter 2030",
);
assert.doesNotMatch(
  hero,
  /onCommitField\([^)]*,\s*1021\s*,\s*stagedV\)/,
  "supply voltage quick-set must not write measured voltage telemetry parameter 1021",
);
assert.doesNotMatch(
  hero,
  /onCommitField\([^)]*,\s*1020\s*,\s*stagedI\)/,
  "supply current quick-set must not write measured current telemetry parameter 1020",
);
assert.match(
  hero,
  /WriteLifecycleTrace/,
  "quick-write surfaces must show the command lifecycle",
);
assert.match(
  hero,
  /SemanticValuePopup/,
  "quick-write values must expose semantic details on hover",
);
assert.match(
  api,
  /function\s+mergeLiveCommand/,
  "successful live writes must be merged into the command activity stream",
);
assert.match(
  api,
  /sameCommandEvent/,
  "gateway command refresh must deduplicate optimistic write events",
);
assert.match(
  api,
  /const\s+submittedCommand\s*=\s*mergeLiveCommand/,
  "live writes must record the submitted command before waiting for command-history refresh",
);
assert.match(
  api,
  /function\s+mecomParameterFamily/,
  "MeCom parameter IDs must provide a generated semantic family for the protocol tree",
);
assert.match(
  api,
  /import\s+protocolFamiliesJson\s+from\s+"..\/data\/mecom-protocol-families\.json\?raw"/,
  "MeCom parameter family ranges must be loaded from JSON, not embedded as the catalogue",
);
assert.match(
  api,
  /MECOM_PARAMETER_FAMILIES\.find/,
  "MeCom parameter family lookup must be data-driven from the protocol family JSON",
);
assert.match(
  api,
  /path:\s*\["MeCom protocol",\s*mecomParameterFamily\(fallbackId\),\s*`Parameter \$\{fallbackId\}`,\s*protocolName\]/,
  "MeCom protocol tree must group entries by generated parameter family before individual IDs",
);
assert.match(
  mainView,
  /function\s+formatRouteLabel/,
  "device route labels must be normalized in the UI",
);
assert.match(
  mainView,
  /hot CAN/i,
  "hot CAN routes must be labeled explicitly",
);
assert.match(
  mainView,
  /warm serial\/FTDI/i,
  "warm serial/FTDI routes must be labeled explicitly",
);
assert.match(
  mainView,
  /fallback route/i,
  "fallback routes must remain distinct in the UI",
);
assert.match(
  assignments,
  /compactSeriesLabel/,
  "compact graph labels must remain SN-channel plus optional nickname",
);

console.log("ui semantics tests ok");
