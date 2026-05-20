import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "..");
const catalogue = JSON.parse(await readFile(path.join(root, "src", "data", "mecom-catalogue.json"), "utf8"));
const projection = JSON.parse(await readFile(path.join(root, "src", "data", "mecom-operator-projection.json"), "utf8"));

assert.equal(
  projection.schema_version,
  "meerstetter.operator_projection.v1",
  "operator projection must declare the reviewed schema version",
);

const catalogueById = new Map(catalogue.map((entry) => [Number(entry.id), entry]));
const operatorIds = catalogue
  .filter((entry) => entry.visibility === "operator")
  .map((entry) => Number(entry.id))
  .sort((a, b) => a - b);

const primaryCounts = new Map();
for (const mapping of projection.primary_mappings || []) {
  assert.ok(Array.isArray(mapping.ids) && mapping.ids.length > 0, "primary mapping must list ids");
  assert.ok(Array.isArray(mapping.path) && mapping.path.length > 0, `primary mapping ${mapping.bundle || ""} must have a tree path`);
  assert.ok(mapping.bundle, `primary mapping for ${mapping.ids.join(",")} must declare a bundle`);
  for (const id of mapping.ids.map(Number)) {
    assert.ok(catalogueById.has(id), `projection maps unknown MeCom id ${id}`);
    primaryCounts.set(id, (primaryCounts.get(id) || 0) + 1);
  }
}

const missingPrimary = operatorIds.filter((id) => !primaryCounts.has(id));
assert.deepEqual(missingPrimary, [], "every operator-visible MeCom signal must have one primary projection mapping");

const duplicatedPrimary = [...primaryCounts.entries()].filter(([, count]) => count > 1).map(([id]) => id);
assert.deepEqual(duplicatedPrimary, [], "operator projection must not duplicate primary signal mappings");

for (const mapping of projection.secondary_mappings || []) {
  assert.ok(Array.isArray(mapping.ids) && mapping.ids.length > 0, "secondary mapping must list ids");
  assert.ok(mapping.reason && String(mapping.reason).trim().length >= 12, `secondary mapping ${mapping.bundle || ""} needs a review reason`);
  for (const id of mapping.ids.map(Number)) {
    assert.ok(catalogueById.has(id), `secondary projection maps unknown MeCom id ${id}`);
  }
}

const missingTooltip = operatorIds.filter((id) => {
  const entry = catalogueById.get(id);
  return !String(entry.hover_help || entry.help_text || "").trim();
});
assert.deepEqual(missingTooltip, [], "operator-visible MeCom signals must carry tooltip/help text");

const corePairChecks = [
  [1000, 3000],
  [52200, 53123],
  [1020, 2020],
  [1021, 2021],
  [1022, 2020],
  [1022, 2021],
];
for (const [source, counterpart] of corePairChecks) {
  const entry = catalogueById.get(source);
  const counterparts = entry && entry.counterparts && Object.values(entry.counterparts).flat().map(Number);
  assert.ok(counterparts && counterparts.includes(counterpart), `MeCom #${source} must keep semantic counterpart #${counterpart}`);
}
