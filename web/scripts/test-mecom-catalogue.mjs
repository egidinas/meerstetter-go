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

const requiredIds = [1000, 1001, 1020, 1021, 2020, 2021, 2030, 40000];
for (const id of requiredIds) {
  const entry = catalogue.find((item) => item.id === id);
  assert.ok(entry, `catalogue is missing required parameter ${id}`);
  assert.ok(entry.unit !== undefined, `parameter ${id} is missing unit`);
  assert.ok(entry.group, `parameter ${id} is missing group`);
  assert.ok(entry.subgroup !== undefined, `parameter ${id} is missing subgroup`);
  assert.ok(entry.role, `parameter ${id} is missing role`);
  assert.ok(entry.source_status, `parameter ${id} is missing source_status`);
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
      readEntry.counterparts?.write?.includes(writeId) || writeEntry.counterparts?.read?.includes(readId),
      `expected counterpart relationship between ${readId} and ${writeId}`,
    );
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
