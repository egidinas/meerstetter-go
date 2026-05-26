import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "..");
const hero = await readFile(path.join(root, "src", "views", "hero.tsx"), "utf8");
const mainView = await readFile(path.join(root, "src", "views", "main.tsx"), "utf8");
const dict = await readFile(path.join(root, "src", "views", "dict.tsx"), "utf8");
const api = await readFile(path.join(root, "src", "api", "mecom.ts"), "utf8");
const atoms = await readFile(path.join(root, "src", "components", "atoms.tsx"), "utf8");
const assignments = await readFile(path.join(root, "src", "views", "assignments.ts"), "utf8");
const seriesLib = await readFile(path.join(root, "src", "lib", "series.ts"), "utf8");
const styles = await readFile(path.join(root, "src", "styles.css"), "utf8");
const graphTilesBackend = await readFile(path.join(root, "..", "cmd", "mecomgw", "graph_tiles.go"), "utf8");
const assignmentTileBuilder = assignments.slice(
  assignments.indexOf("export function buildGraphTileFromAssignments"),
  assignments.indexOf("function graphTileSeriesFromAssignments"),
);

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
  api,
  /async\s+readFreshValue/,
  "write confirmation must be able to force a fresh value read instead of trusting stale cache",
);
assert.match(
  api,
  /async\s+confirmWriteValue/,
  "writes must close the loop with an explicit readback confirmation helper",
);
assert.match(
  hero,
  /confirmWriteValue\(deviceId,\s*PARAM_TARGET_TEMPERATURE/,
  "temperature target writes must confirm against a fresh readback of parameter 3000",
);
assert.doesNotMatch(
  hero,
  /const\s+confirmed\s*=\s*liveSnapshot\(deviceId,\s*PARAM_TARGET_TEMPERATURE/,
  "temperature target writes must not label a stale cached snapshot as confirmed",
);
assert.match(
  atoms,
  /confirmedMatched|readback mismatch/,
  "write lifecycle UI must distinguish accepted writes from matching readback confirmation",
);
assert.match(
  hero,
  /SemanticValuePopup/,
  "quick-write values must expose semantic details on hover",
);
assert.match(
  atoms,
  /hover_help|help_text/,
  "semantic value popups must surface harvested catalogue tooltip text",
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
  /age_ms/,
  "readValue results must carry freshness metadata for catalogue rows",
);
assert.match(
  api,
  /function\s+queueLiveValueRead/,
  "live signal dictionary cache misses must schedule an exact lazy read instead of staying empty",
);
assert.match(
  api,
  /\/api\/devices\/\$\{encodeURIComponent\(deviceId\)\}\/read\?params=\$\{encodeURIComponent\(`\$\{paramId\}:\$\{inst\}`\)\}/,
  "lazy live reads must target the exact device:param:instance requested by the row",
);
assert.match(
  api,
  /queueLiveValueRead\(deviceId,\s*paramId,\s*inst\)/,
  "readValue must queue a lazy refresh when an opened catalogue row is missing or stale",
);
assert.match(
  api,
  /role === "supply"[\s\S]*\[\s*1020,\s*1021,\s*1022,\s*40000,\s*2020,\s*2021,\s*2030,\s*2031,\s*2032,\s*2033,\s*2010,\s*2040,\s*1001\s*\]/,
  "supply channels must poll measured current/voltage/power plus writable voltage/current/output controls for their own instance",
);
assert.match(
  api,
  /\[\s*1000,\s*1001,\s*3000,\s*52200,\s*1200,\s*1020,\s*1021,\s*1022,\s*40000,\s*2020,\s*2021,\s*2030,\s*2031,\s*2032,\s*2033,\s*53120,\s*53121,\s*53122,\s*53123\s*\]/,
  "temperature channels must poll object/sink/target/cascade and channel electrical values for their own instance",
);
assert.doesNotMatch(
  api,
  /paramsForChannel[\s\S]*1010/,
  "channel polling must not include unsupported telemetry IDs that break whole-channel reads",
);
assert.match(
  hero,
  /const\s+cascadeActive\s*=\s*cascadeEnable\.value\s*===\s*1/,
  "cascade target visibility must be driven by the live cascade-enable parameter 53120",
);
assert.match(
  hero,
  /\{cascadeActive\s*&&\s*\([\s\S]*PARAM_CASCADE_TARGET_TEMPERATURE/,
  "stored cascade target 53123 must only render in the temperature card when cascade control is active",
);
assert.match(
  atoms,
  /export function formatValueAge/,
  "catalogue rows must render value freshness instead of hiding staleness in hover text",
);
assert.match(
  atoms,
  /export function valueAgeKind/,
  "value freshness severity must be reusable by dictionary and tree rows",
);
const signalDictionaryViewFn = dict.slice(
  dict.indexOf("export function SignalDictionaryView"),
  dict.indexOf("function ChannelsEditor"),
);
assert.match(
  signalDictionaryViewFn,
  /useGatewayTick\(\)/,
  "signal dictionary must subscribe to gateway polling ticks so live catalogue/channel updates re-render",
);
assert.doesNotMatch(
  signalDictionaryViewFn,
  /useMemo\(\(\)\s*=>\s*MecomAPI\.catalogue\(\),\s*\[\]\)/,
  "signal dictionary must not freeze the catalogue at first render before live discovery finishes",
);
assert.match(
  api,
  /const\s+SUPPLY_POLL_PARAM_IDS\s*=/,
  "live dictionary reads must use a named supply poll set instead of row-scale ad hoc reads",
);
assert.match(
  api,
  /const\s+TEMP_POLL_PARAM_IDS\s*=/,
  "live dictionary reads must use a named temperature poll set instead of row-scale ad hoc reads",
);
assert.match(
  api,
  /isPolledSignal\(role,\s*paramId\)/,
  "frontend components must be able to tell which dictionary signals are backed by live polling",
);
assert.match(
  api,
  /hasLiveValue\(deviceId,\s*paramId,\s*instance\?\)/,
  "dictionary rows must be able to render already-cached live values without forcing fresh exact reads",
);
assert.match(
  atoms,
  /opts\?\.enabled\s*!==\s*false/,
  "live value hooks must support disabled catalogue rows without queuing device reads",
);
assert.match(
  atoms,
  /MecomAPI\.isPolledSignal\?\.\(role,\s*param\.id\)[\s\S]*MecomAPI\.hasLiveValue\?\.\(resolvedDeviceId,\s*param\.id,\s*instance\)/,
  "signal dictionary rows must gate live reads to polled or already-cached values",
);
assert.match(
  atoms,
  /useLiveValue\(resolvedDeviceId,\s*param\.id,\s*instance,\s*\{\s*enabled:\s*liveEnabled\s*\}\)/,
  "signal dictionary rows must pass live-read gating into useLiveValue",
);
assert.match(
  atoms,
  /<SparklineWrapper deviceId=\{resolvedDeviceId\} paramId=\{param\.id\} instance=\{instance\} enabled=\{liveEnabled\} \/>/,
  "signal dictionary sparklines must be gated with the same live-read policy as values",
);
assert.doesNotMatch(
  atoms,
  /\/api\/graph\/sparklines/,
  "dictionary sparklines must not fetch the gateway once per rendered catalogue row",
);
assert.match(
  atoms,
  /className=\{"qtag "/,
  "catalogue rows must show quality as visible metadata",
);
assert.match(
  atoms,
  /semanticValueRows\(param, value, quality, ageMs\)/,
  "semantic value hover metadata must include value age",
);
assert.match(
  atoms,
  /default_collapsed|instance_scope|duplicate_reason/,
  "tree projection metadata must be available to the rendered signal dictionary",
);
assert.match(
  atoms,
  /hover_help|help_text/,
  "tree search and value cards must include harvested tooltip/help metadata",
);
assert.match(
  styles,
  /\.tree-inst\s*\{[\s\S]*grid-template-columns/,
  "signal dictionary instance rows must use a stable grid instead of content-width columns",
);
assert.match(
  styles,
  /overflow-wrap:\s*anywhere/,
  "signal dictionary text must wrap inside cards instead of disappearing beyond card bounds",
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
  /import\s+lddCatalogueJson\s+from\s+"..\/data\/mecom-ldd-130x-catalogue\.json\?raw"/,
  "LDD catalogue must be loaded as a first-class JSON definition beside the TEC catalogue",
);
assert.match(
  api,
  /import\s+catalogueDefinitionsJson\s+from\s+"..\/data\/mecom-catalogue-definitions\.json\?raw"/,
  "catalogue definition metadata must be loaded from JSON instead of hardcoded UI branches",
);
assert.match(
  api,
  /import\s+operatorProjectionJson\s+from\s+"..\/data\/mecom-operator-projection\.json\?raw"/,
  "operator-facing signal projection must be loaded from JSON settings, not hardcoded in React",
);
assert.match(
  api,
  /function\s+mergeLiveCatalogueEntries/,
  "live catalogue discovery must merge onto the full static catalogue instead of replacing non-priority/lazy signals",
);
assert.match(
  api,
  /function\s+entryDefinitionRef/,
  "catalogue entries must expose a normalized definition identity before merging or filtering",
);
assert.match(
  api,
  /function\s+catalogueDefinitionSummaries/,
  "signal dictionary must expose available catalogue definitions with counts",
);
assert.match(
  api,
  /function\s+catalogueEntriesForDefinition/,
  "signal dictionary must be able to filter catalogue entries by definition",
);
assert.match(
  api,
  /function\s+catalogueSignalKey\(entry\)[\s\S]*entryDefinitionRef\(entry\)[\s\S]*Number\(entry && entry\.id\)/,
  "catalogue merge identity must include definition_ref so TEC and LDD numeric IDs cannot collide",
);
assert.match(
  api,
  /const\s+key\s*=\s*`\$\{definitionRef\}:\$\{id\}:\$\{instance\}`/,
  "live catalogue entries must also de-duplicate by definition_ref plus parameter and instance",
);
assert.match(
  api,
  /return\s+mergeLiveCatalogueEntries\(CATALOGUE,\s*liveCatalogueEntries\(\)\)/,
  "active catalogue must preserve the complete static MeCom dictionary while overlaying live metadata",
);
const mergeLiveCatalogueFn = api.slice(api.indexOf("function mergeLiveCatalogueEntries"), api.indexOf("function activeCatalogue"));
assert.match(
  mergeLiveCatalogueFn,
  /live_catalogue_observed/,
  "known live catalogue entries must be stored as observed runtime metadata, not used as replacement truth",
);
assert.doesNotMatch(
  mergeLiveCatalogueFn,
  /if\s*\(!existing\)\s*{\s*byID\.set/s,
  "live-only catalogue discoveries must not be promoted into the active curated catalogue",
);
assert.doesNotMatch(
  mergeLiveCatalogueFn,
  /normalizeCatalogueEntry\(entry,\s*existing/,
  "known live catalogue entries must not re-normalize over established catalogue truth fields",
);
assert.doesNotMatch(
  api,
  /return\s+liveCatalogueEntries\(\)\s*\|\|\s*CATALOGUE\.slice\(\)/,
  "live discovery must not drop lazy/non-priority catalogue entries by replacing the full catalogue",
);
assert.match(
  api,
  /default_collapsed|instance_scope|duplicate_reason/,
  "projection metadata must survive API normalization for deterministic UI grouping",
);
assert.match(
  assignments,
  /DEFAULT_GRAPH_TILE_LEVEL\s*=\s*"session"/,
  "graph assignment tiles must default to the bounded current-session history level, not live-only buffers",
);
assert.match(
  graphTilesBackend,
  /defaultGraphTileLevel\s*=\s*"session"/,
  "backend graph tile route must default to the bounded current-session history level when no level is supplied",
);
assert.match(
  api,
  /level \|\| "session"/,
  "API graph tile client must also default missing levels to bounded current-session history",
);
assert.doesNotMatch(
  graphTilesBackend,
  /level := "live"/,
  "backend graph tile route must not default missing levels to the live buffer",
);
assert.match(
  assignments,
  /const\s+explicitLevel\s*=\s*opts\.level\s*\|\|\s*opts\.tileLevel;[\s\S]*const\s+requestedLevel\s*=\s*explicitLevel\s*\|\|\s*DEFAULT_GRAPH_TILE_LEVEL/,
  "tile loading must choose an explicit session default level before deriving the window",
);
assert.doesNotMatch(
  assignments,
  /const\s+timeWindowMs\s*=\s*opts\.time_window_ms\s*\|\|\s*opts\.timeWindowMs\s*\|\|\s*90_000/,
  "tile loading must not silently default to a 90 second live buffer window",
);
assert.match(
  mainView,
  /useState\(DEFAULT_GRAPH_TILE_LEVEL\)/,
  "device graph walls must default to session tile history rather than live-only tiles",
);
const fleetViewFn = mainView.slice(mainView.indexOf("export function FleetView"), mainView.indexOf("export function DeviceMini"));
assert.doesNotMatch(
  fleetViewFn,
  /level:\s*"live"/,
  "fleet hero graphs must not hardcode the live tile level",
);
assert.match(
  fleetViewFn,
  /fleetTileLevel/,
  "fleet hero graphs must expose a shared history tile level for aligned temperature and power timelines",
);
assert.match(
  assignments,
  /export function configDrivenGraphTileAssignments[\s\S]*return\s+\[\];/,
  "config-driven fleet tiles must let the gateway choose series from the live device/channel config",
);
assert.doesNotMatch(
  fleetViewFn,
  /fleetTempStored|fleetSupplyStored/,
  "fleet hero graphs must not merge stale persisted SignalForge assignments into config-driven live tiles",
);
assert.match(
  fleetViewFn,
  /configDrivenGraphTileAssignments\(WALLS\.fleetTemp\.wall_id,\s*tempChannels\)/,
  "fleet temperature hero must request the gateway-owned default tile series from current config",
);
assert.match(
  fleetViewFn,
  /configDrivenGraphTileAssignments\(WALLS\.fleetSupply\.wall_id,\s*supplyChannels\)/,
  "fleet supply hero must request the gateway-owned default tile series from current config",
);
const seedAssignmentsFn = assignments.slice(
  assignments.indexOf("export function seedAssignments"),
  assignments.indexOf("export const CHANNEL_COLORS"),
);
assert.match(
  seedAssignmentsFn,
  /let\s+next\s*=\s*current\.filter[\s\S]*fleetWall[\s\S]*return\s+!fleetWall;/,
  "assignment seeding must purge stale fleet-wall entries unconditionally",
);
assert.doesNotMatch(
  seedAssignmentsFn,
  /WALLS\.fleetTemp\.wall_id,\s*tempChannels/,
  "assignment seeding must not persist fleet temperature defaults when fleet tiles are config-owned",
);
assert.doesNotMatch(
  seedAssignmentsFn,
  /WALLS\.fleetSupply\.wall_id,\s*supplyChannels/,
  "assignment seeding must not persist fleet supply defaults when fleet tiles are config-owned",
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
  signalDictionaryViewFn,
  /const\s+\[definitionFilter,\s*setDefinitionFilter\]\s*=\s*useState\(""\)/,
  "signal dictionary must keep catalogue definition selection as UI state",
);
assert.match(
  signalDictionaryViewFn,
  /MecomAPI\.catalogueDefinitions\(\)/,
  "signal dictionary must list definitions from the API",
);
assert.match(
  signalDictionaryViewFn,
  /MecomAPI\.catalogueForDefinition\(definitionFilter\)/,
  "signal dictionary must feed the tree with definition-filtered catalogue rows",
);
assert.match(
  styles,
  /\.dict-definition-bar/,
  "signal dictionary must render a visible definition selector for TEC/LDD/DAQ catalogues",
);
assert.match(
  mainView,
  /function\s+formatRouteLabel/,
  "device route labels must be normalized in the UI",
);
assert.match(
  mainView,
  /function\s+routeTransport/,
  "device route labels must derive transport from backend transport or endpoint metadata",
);
assert.match(
  mainView,
  /function\s+canonicalRouteName/,
  "device route labels must replace uninformative backend names like configured with a transport-aware name",
);
assert.match(
  mainView,
  /uninformativeRouteLabel/,
  "device route labels must explicitly reject placeholder names before rendering",
);
assert.match(
  mainView,
  /Kvaser USB CAN|PiXtend CAN|SocketCAN|USB FTDI RS485/,
  "device route labels must support short hardware/transport names without hardcoding one lab topology",
);
assert.doesNotMatch(
  mainView,
  /warm serial\/FTDI/,
  "CAN standby routes must not be mislabeled as serial/FTDI from role alone",
);
assert.match(
  assignments,
  /compactSeriesLabel/,
  "compact graph labels must remain SN-channel plus optional nickname",
);
assert.match(
  seriesLib,
  /graphSeriesIdentityKey,[\s\S]*} from "signalforge-web"/,
  "graph series identity must come from the shared SignalForge helper",
);
assert.doesNotMatch(
  seriesLib,
  /function\s+(readSource|parseTargetId|parseColonKey|graphSeriesIdentityKey)\b/,
  "Meerstetter-Go must not keep a local graph-series identity parser",
);
assert.doesNotMatch(
  seriesLib,
  /series\?\.label|\blabel\b[\s\S]*graphSeriesIdentityKey/,
  "graph series identity must not fall back to display labels that can collide across devices or channels",
);
assert.match(
  assignments,
  /return `\$\{n\.device_id\}:\$\{n\.param_id\}:\$\{n\.instance \|\| 1\}`/,
  "Meerstetter-generated tile series IDs must use exact device:param:instance identity",
);
assert.match(
  assignments,
  /fallbackSeriesVisibility/,
  "fallback graph tiles must carry missing/detached quality and default visibility metadata",
);
assert.doesNotMatch(
  assignmentTileBuilder,
  /MecomAPI\.readValue|getTelemetry\(|recordTelemetry\(/,
  "frontend graph tile fallback must not read live values or local telemetry; only the gateway may author tile history",
);
assert.doesNotMatch(
  assignmentTileBuilder,
  /history:\s*seriesHistory|points,\s*$/m,
  "frontend graph tile fallback must not fabricate history or points when the gateway tile is unavailable",
);
const historyFromServerSeriesFn = assignments.slice(
  assignments.indexOf("function historyFromServerSeries"),
  assignments.indexOf("function seriesSourceKey"),
);
assert.doesNotMatch(
  historyFromServerSeriesFn,
  /series\s*&&\s*series\.points|series\.points|\.map\(\(p\)/,
  "frontend must not rebuild authoritative graph history from server point fallbacks when history is missing",
);
const enrichServerGraphTileFn = assignments.slice(
  assignments.indexOf("function enrichServerGraphTile"),
  assignments.indexOf("function useGraphTileData"),
);
assert.doesNotMatch(
  enrichServerGraphTileFn,
  /raw\.points|series\.points/,
  "enriched graph tiles must render backend history and must not treat raw points as fallback history",
);
assert.match(
  assignmentTileBuilder,
  /tile_source:\s*"gateway-tile-unavailable"/,
  "frontend graph tile fallback must be an explicit unavailable placeholder, not a live/history substitute",
);
assert.match(
  assignments,
  /loadSignalForgeAssignments/,
  "assignment persistence must consume the shared SignalForge assignment store primitive",
);
assert.match(
  assignments,
  /useSignalForgeAssignments/,
  "graph assignment hooks must be backed by SignalForge instead of a local hook fork",
);
assert.doesNotMatch(
  assignments,
  /const\s+ASSIGNMENT_KEY/,
  "Meerstetter-Go must not keep a local assignment storage key when SignalForge owns assignment persistence",
);
assert.doesNotMatch(
  assignments,
  /localStorage\.(?:getItem|setItem)\(ASSIGNMENT_KEY/,
  "Meerstetter-Go must not read or write the assignment list directly from localStorage",
);
assert.doesNotMatch(
  assignments,
  /window\.dispatchEvent\(new CustomEvent\("mecomgw-assignments-changed"\)/,
  "Meerstetter-Go must not emit a local assignment-store change event when SignalForge owns assignment persistence",
);
const defaultHiddenFn = mainView.slice(
  mainView.indexOf("function defaultHiddenSeriesForTile"),
  mainView.indexOf("export function FleetView"),
);
assert.match(
  defaultHiddenFn,
  /return\s+Array\.from\(hiddenKeys\)/,
  "default-hidden detection must keep raw missing/detached tile series keys",
);
assert.match(
  defaultHiddenFn,
  /\(tile\?\.series\s*\|\|\s*\[\]\)\.forEach/,
  "default-hidden detection must inspect raw tile series, including non-rendered missing/detached channels",
);
assert.match(
  mainView,
  /DEFAULT_HIDDEN_QUALITIES\s*=\s*new Set\(\[[^\]]*"missing"[^\]]*"detached"/s,
  "missing and detached channel qualities must be deselected by default",
);
assert.match(
  defaultHiddenFn,
  /DEFAULT_HIDDEN_QUALITIES\.has\(quality\)/,
  "hidden-series defaults must be driven by backend quality metadata",
);
assert.doesNotMatch(
  defaultHiddenFn,
  /\.label\b|\.full_label\b|\.name\b/,
  "default-hidden detection must use exact backend identity, not display labels that can collide across channels",
);
assert.match(
  defaultHiddenFn,
  /seriesDefaultVisible\(series\) === false/,
  "backend tile default_visible=false must deselect disconnected or invalid channels",
);
assert.match(
  mainView,
  /function channelSortRank\(channel\)/,
  "device workspace channel selection must have a deterministic channel sort helper",
);
assert.match(
  mainView,
  /\.sort\(\(a, b\) => channelSortRank\(a\) - channelSortRank\(b\)\)/,
  "device workspace must sort channels before choosing defaults",
);
assert.match(
  mainView,
  /defaultChannelInst[\s\S]*channels\.find\(\(c\) => c\.instance === 1\)/,
  "device workspace must prefer channel 1 as the stable default when present",
);
assert.match(
  defaultHiddenFn,
  /isDegreeCSeries\(series\)[\s\S]*!hasFiniteInFamilyValue\(series\)/,
  "temperature channels with only detached/open-sensor lows must be hidden from autoscale by default",
);
assert.doesNotMatch(
  defaultHiddenFn,
  /renderSeriesFromGraphTile/,
  "default-hidden detection must not require a drawable uPlot trace",
);
assert.match(
  hero,
  /rawSeriesKeys/,
  "hero graph hidden-series state must include raw tile series keys",
);
assert.match(
  hero,
  /validHiddenSeriesKeys/,
  "hero graph hidden-series state must keep non-rendered missing/detached series deselected",
);
assert.match(
  hero,
  /renderedSeries\.map\(\(series\) => tileSeriesKey\(series\)\)\.concat\(rawSeriesKeys\)/,
  "hero graph hidden-series allowlist must combine drawable traces with raw missing/detached tile keys",
);
assert.match(
  hero,
  /rawOnlyLegendSeries/,
  "hero legend must expose raw missing/detached tile series as off-by-default selectable entries",
);
assert.match(
  hero,
  /data-series-quality=\{s\.quality \|\| "ok"\}/,
  "browser checks need per-series quality metadata in the legend",
);
assert.match(
  hero,
  /data-series-visible=\{off \? "false" : "true"\}/,
  "hidden disconnected channels must be visibly deselected and reversible from the legend",
);
assert.match(
  hero,
  /quality-mini/,
  "legend must visibly explain why disconnected or missing traces are hidden by default",
);
assert.match(
  hero,
  /visibility_reason \|\| series\.visibilityReason/,
  "hero legend must preserve backend visibility reasons from the tile contract",
);
assert.match(
  mainView,
  /visibility_reason \|\| raw\.visibilityReason/,
  "device graph legends must preserve backend visibility reasons from the tile contract",
);
assert.match(
  mainView,
  /rawOnlyLegendSeries/,
  "device graph legend must expose raw missing/detached tile series as off-by-default selectable entries",
);
assert.match(
  mainView,
  /data-series-quality=\{s\.quality \|\| "ok"\}/,
  "device graph legend must preserve per-channel quality metadata for browser verification",
);
assert.match(
  mainView,
  /tileSeriesCount === 0/,
  "device graph cards must not look empty just because every channel series is hidden or non-drawable",
);
assert.match(
  atoms,
  /data-hidden-series-count=\{hidden\.size\}/,
  "browser smoke checks need a rendered hidden-series count for missing/detached channel verification",
);
assert.match(
  atoms,
  /graphSeriesIdentityKey\(series\)/,
  "hidden-series filtering must use exact graph identity instead of display labels",
);
assert.match(
  mainView,
  /function seriesKey\(series\)[\s\S]*graphSeriesIdentityKey\(series\)/,
  "main graph legends must use exact graph identity instead of display labels",
);
assert.match(
  hero,
  /function tileSeriesKey\(series\)[\s\S]*graphSeriesIdentityKey\(series\)/,
  "hero graph legends must use exact graph identity instead of display labels",
);
assert.match(
  atoms,
  /const\s+visibleTile\s*=\s*useMemo\(/,
  "chart rendering must build an explicit visible tile after hidden detached/open-sensor series are removed",
);
assert.match(
  atoms,
  /graphTileTimeRange\(visibleTile,\s*timeWindowMs\)/,
  "chart time range must be computed from visible series, not hidden detached/open-sensor series",
);
assert.match(
  atoms,
  /renderSeriesFromGraphTile\(rangedTile\)/,
  "uPlot series must be rendered from the visible ranged tile",
);
assert.match(
  atoms,
  /tickCount=\{16\}/,
  "shared graph time axes must request 16 markers by default",
);
assert.match(
  atoms,
  /currentTimeMs=\{currentTimeMs\}/,
  "shared graph time axes must receive the current-time anchor for present-anchored zoom",
);
assert.match(
  atoms,
  /const full = isFullTimeRange\(range,\s*fullTimeRange\);[\s\S]*setManualX\(!full\);[\s\S]*setTimeRange\(full \? fullTimeRange : range\);/,
  "manual x-axis changes must mark subranges as manual while full-range selections return to full tracking",
);
assert.match(
  atoms,
  /const\s+isCollapsed\s*=\s*collapsed\[group\]\s*!==\s*undefined\s*\?\s*collapsed\[group\]\s*:\s*!hasActiveFilter/,
  "signal dictionary groups must be collapsed by default until a filter/search is active",
);
assert.match(
  atoms,
  /formatValueAge\(ageMs,\s*quality\)/,
  "signal dictionary instance rows must show live value age",
);
assert.match(
  atoms,
  /valueAgeKind\(ageMs,\s*quality\)/,
  "signal dictionary instance rows must style stale or missing values",
);
assert.match(
  atoms,
  /<span className=\{"age " \+ valueAgeKind\(ageMs,\s*quality\)\}>\{formatValueAge\(ageMs,\s*quality\)\}<\/span>/,
  "signal dictionary instance rows must render age as visible row metadata",
);
assert.match(
  atoms,
  /formatWithUnit\(value,\s*param\.unit,\s*param\.id\)/,
  "signal dictionary live values must render proper units such as degree Celsius",
);

assert.match(
  hero,
  /export function HeroGraph\(\{[\s\S]*minPlotHeight\s*=\s*150/,
  "fleet hero graph layout must support a compact plot floor independent of the nominal chart height",
);
assert.match(
  hero,
  /<MultiChart[\s\S]*titleOverride=\{wall\.label\}[\s\S]*headerExtras=\{/,
  "hero graph titles and history controls must live in the chart setup bar instead of a separate header row",
);
assert.match(
  hero,
  /<MultiChart[\s\S]*height=\{height\}[\s\S]*minHeight=\{minPlotHeight\}/,
  "hero graphs must pass the compact plot floor to MultiChart instead of reusing the nominal height as a hard minimum",
);
assert.match(
  mainView,
  /<HeroGraph wall=\{WALLS\.fleetTemp\}[\s\S]*height=\{220\}[\s\S]*minPlotHeight=\{150\}/,
  "fleet temperature hero must use compact graph sizing so settings cannot squeeze the plot below fold",
);
assert.match(
  mainView,
  /<HeroGraph wall=\{WALLS\.fleetSupply\}[\s\S]*height=\{220\}[\s\S]*minPlotHeight=\{150\}/,
  "fleet supply hero must use compact graph sizing so both graph blocks fit above fold",
);
assert.doesNotMatch(
  styles,
  /\.fleet-heroes\s*\{[\s\S]*min-height:\s*620px/,
  "fleet graph stack must not force a 620px minimum that wastes vertical space on short viewports",
);
assert.match(
  styles,
  /\.fleet-heroes\s*\{[\s\S]*height:\s*calc\(100dvh - 58px\)/,
  "fleet graph stack must reserve only the compact viewport budget below the toolbar",
);
assert.match(
  styles,
  /\.fleet-heroes \.hero\s*\{[\s\S]*grid-template-rows:\s*minmax\(170px,\s*1fr\) clamp\(54px,\s*11dvh,\s*82px\)/,
  "fleet heroes must bound the settings table row and prioritize plot height",
);
assert.match(
  styles,
  /\.chart-setup-bar\s*\{[\s\S]*grid-template-rows:\s*auto auto/,
  "graph title, y controls, and shared time controls must share the graph setup area above the plot",
);
assert.match(
  styles,
  /\.chart-title\s*\{[\s\S]*font-size:\s*11px/,
  "graph titles must use compact dashboard-scale type inside the setup bar",
);
assert.match(
  styles,
  /\.chart-setup-bar \.operator-shared-time-axis\s*\{[\s\S]*grid-template-columns:\s*34px minmax\(0,\s*1fr\)/,
  "SignalForge shared time zoom and scroll controls must be mounted above the graph in compact form",
);
assert.match(
  styles,
  /\.hero-settings \.supply-inputs-inline\s*\{[\s\S]*flex-direction:\s*row/,
  "voltage command and limit controls must fit on one line inside the settings row",
);
const lastChartControls = styles.slice(styles.lastIndexOf(".chart-controls {"));
assert.match(
  lastChartControls,
  /gap:\s*4px/,
  "chart controls must use compact horizontal gaps inside fleet heroes",
);
assert.match(
  lastChartControls,
  /padding:\s*0/,
  "chart controls must not consume a full row of vertical padding inside fleet heroes",
);
assert.match(
  atoms,
  /autoY=\{autoY\}[\s\S]*yRange=\{parsedYRange\}/,
  "automatic y scaling must stay on the SignalForge/uPlot renderer default while manual bounds use the existing yRange contract",
);
assert.doesNotMatch(
  atoms,
  /tightAutoYRange/,
  "fleet charts must avoid custom autoscale helpers when the SignalForge renderer can use native autoY",
);
assert.match(
  atoms,
  /<SharedTimeAxis[\s\S]*tickCount=\{16\}[\s\S]*\/>/,
  "graphs must keep the existing SignalForge shared time axis and its tick density",
);
assert.doesNotMatch(
  atoms,
  /<div className="chart-plot"[\s\S]*<SharedTimeAxis/,
  "shared time zoom and scroll controls must not remain as a separate bottom row below the plot",
);
assert.match(
  graphTilesBackend,
  /graphTileTargetPointCount\s*=\s*2400/,
  "backend graph tiles must target roughly one sample per 1080p plot pixel before frontend rendering",
);

console.log("ui semantics tests ok");
