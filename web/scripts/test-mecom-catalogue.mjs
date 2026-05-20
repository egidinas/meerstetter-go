import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "..");
const raw = await readFile(path.join(root, "src", "data", "mecom-catalogue.json"), "utf8");
const catalogue = JSON.parse(raw);

const MIN_EXPECTED_ENTRIES = 200;
assert.ok(
  catalogue.length >= MIN_EXPECTED_ENTRIES,
  `catalogue has ${catalogue.length} entries; target is at least ${MIN_EXPECTED_ENTRIES}. Expand the public-safe JSON seed before shipping.`,
);

for (const entry of catalogue) {
  assert.ok(entry.unit !== undefined, `parameter ${entry.id} is missing unit`);
  assert.ok(entry.group && entry.group.trim().length > 0, `parameter ${entry.id} (${entry.raw_name || entry.sid}) is missing 'group' needed for default projection`);
  assert.ok(entry.subgroup !== undefined && entry.subgroup !== null, `parameter ${entry.id} (${entry.raw_name || entry.sid}) is missing 'subgroup' needed for default projection`);
  assert.ok(entry.role && entry.role.trim().length > 0, `parameter ${entry.id} is missing role`);
  assert.ok(entry.source_status && entry.source_status.trim().length > 0, `parameter ${entry.id} is missing source_status`);
}

const readWritePairs = [
  [1000, 3000],
  [1020, 2020],
  [1021, 2021],
  [1020, 2030],
  [1021, 2031],
];

for (const [readId, writeId] of readWritePairs) {
  const readEntry = catalogue.find((item) => item.id === readId);
  const writeEntry = catalogue.find((item) => item.id === writeId);
  if (readEntry && writeEntry) {
    assert.ok(
      readEntry.counterparts?.write?.includes(writeId) || writeEntry.counterparts?.read?.includes(readId) ||
      readEntry.counterparts?.setpoint?.includes(writeId) || writeEntry.counterparts?.measured?.includes(readId),
      `expected counterpart relationship between ${readId} and ${writeId}`,
    );
  }
}

const protocolAliasExpectations = [
  { catalogueId: 12034, mecomId: 52002, canopenIndex: "0x2F02", type: "int32" },
  { catalogueId: 12035, mecomId: 52003, canopenIndex: "0x2F03", type: "int32" },
];

for (const expected of protocolAliasExpectations) {
  const entry = catalogue.find((item) => item.id === expected.catalogueId);
  assert.ok(entry, `expected CANopen catalogue entry ${expected.catalogueId}`);
  assert.equal(entry.type, expected.type, `catalogue entry ${expected.catalogueId} type should match CANopen SDO source map`);
  assert.equal(entry.protocol_aliases?.mecom_parameter_id, expected.mecomId, `catalogue entry ${expected.catalogueId} must document CoSo/MeCom parameter alias`);
  assert.equal(entry.protocol_aliases?.canopen_index, expected.canopenIndex, `catalogue entry ${expected.catalogueId} must document CANopen index`);
  assert.equal(entry.protocol_aliases?.canopen_object_decimal, expected.catalogueId, `catalogue entry ${expected.catalogueId} must document decimal CANopen object alias`);
  assert.ok(
    entry.protocol_aliases?.source_map?.includes("tec_canopen_sdo_map"),
    `catalogue entry ${expected.catalogueId} must point at the source SDO map`,
  );
}

// Global counterparts validation to ensure all targets exist and mutual back-references exist for primary telemetry/control pairs
for (const entry of catalogue) {
  if (entry.counterparts) {
    assert.ok(typeof entry.counterparts === "object" && entry.counterparts !== null, `parameter ${entry.id} counterparts must be an object`);
    for (const [relation, targetIds] of Object.entries(entry.counterparts)) {
      assert.ok(Array.isArray(targetIds), `parameter ${entry.id} counterparts.${relation} must be an array`);
      for (const targetId of targetIds.map(Number)) {
        const targetEntry = catalogue.find((item) => item.id === targetId);
        assert.ok(targetEntry, `parameter ${entry.id} counterparts.${relation} lists unknown parameter ${targetId}`);

        // Ensure that for primary read-write (telemetry-control) relationships, the link is mutual.
        const isTelemetryToControl = (entry.role === "monitor" && targetEntry.role === "control") || (entry.role === "control" && targetEntry.role === "monitor");
        if (isTelemetryToControl && (relation === "measured" || relation === "read" || relation === "telemetry" || relation === "write" || relation === "setpoint")) {
          const targetCounterparts = targetEntry.counterparts;
          assert.ok(targetCounterparts, `parameter ${targetId} is counterparts target for ${entry.id}, but has no counterparts map itself`);
          const allTargetCounterpartIds = Object.values(targetCounterparts).flat().map(Number);
          assert.ok(
            allTargetCounterpartIds.includes(Number(entry.id)),
            `mutual link missing: parameter ${entry.id} points to ${targetId}, but parameter ${targetId} counterparts [${allTargetCounterpartIds.join(", ")}] does not point back to ${entry.id}`
          );
        }
      }
    }
  }
}

const placeholderEntries = catalogue.filter((item) => item.source_status === "needs_datasheet_review");
assert.ok(
  placeholderEntries.length > 0,
  "expected at least one structured placeholder entry for unpublished metadata",
);

console.log(
  `mecom catalogue audit: ${catalogue.length} entries, ${placeholderEntries.length} placeholders, target ${MIN_EXPECTED_ENTRIES}`,
);
