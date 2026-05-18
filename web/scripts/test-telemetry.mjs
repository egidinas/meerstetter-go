import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import ts from "typescript";

const root = path.resolve(import.meta.dirname, "..");
const source = await readFile(path.join(root, "src", "lib", "telemetry.ts"), "utf8");
const output = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ES2022,
    target: ts.ScriptTarget.ES2022,
    sourceMap: false,
  },
});
const dir = await mkdtemp(path.join(tmpdir(), "meerstetter-telemetry-"));
const modulePath = path.join(dir, "telemetry.mjs");
await writeFile(modulePath, output.outputText);

const telemetry = await import(modulePath);
const { TELE_BUF, TELE_MAX, TELE_SERIES_MAX, teleKey, recordTelemetry, getTelemetry } = telemetry;

function reset() {
  TELE_BUF.clear();
}

reset();
recordTelemetry("dev", "temp", 10, "ok", 0);
recordTelemetry("dev", "temp", 11, "ok", 1);
assert.deepEqual(getTelemetry("dev", "temp", 0).v, [10]);
assert.deepEqual(getTelemetry("dev", "temp", 1).v, [11]);
assert.notEqual(teleKey("a/1", "b", 1), teleKey("a", "b", "1:"));
assert.notEqual(teleKey(75, 3000, 1), teleKey("75", 3000, 1));
assert.notEqual(teleKey("tec", 3000, 1), teleKey("tec", "3000", 1));
recordTelemetry(75, 3000, 12, "ok", 1);
recordTelemetry("75", 3000, 13, "ok", 1);
recordTelemetry("tec", "3000", 14, "ok", 1);
assert.deepEqual(getTelemetry(75, 3000, 1).v, [12]);
assert.deepEqual(getTelemetry("75", 3000, 1).v, [13]);
assert.deepEqual(getTelemetry("tec", "3000", 1).v, [14]);

reset();
for (let i = 0; i < TELE_MAX + 3; i += 1) {
  recordTelemetry("dev", "trim", i, "ok");
}
const trimmed = getTelemetry("dev", "trim");
assert.equal(trimmed.v.length, TELE_MAX);
assert.equal(trimmed.seq, TELE_MAX + 3);
assert.equal(trimmed.v[0], 3);
trimmed.v[0] = -1;
trimmed.ts.length = 0;
assert.equal(getTelemetry("dev", "trim").v[0], 3);
assert.equal(getTelemetry("dev", "trim").ts.length, TELE_MAX);

reset();
for (let i = 0; i < TELE_SERIES_MAX; i += 1) {
  recordTelemetry("dev", `p${i}`, i, "ok");
}
assert.equal(TELE_BUF.size, TELE_SERIES_MAX);
assert.deepEqual(getTelemetry("dev", "p0").v, [0]);
recordTelemetry("dev", "overflow", 999, "ok");
assert.equal(TELE_BUF.size, TELE_SERIES_MAX);
assert.deepEqual(getTelemetry("dev", "p0").v, [0]);
assert.deepEqual(getTelemetry("dev", "p1").v, []);
assert.deepEqual(getTelemetry("dev", "overflow").v, [999]);

console.log("telemetry tests ok");
