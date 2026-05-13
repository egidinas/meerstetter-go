#!/usr/bin/env node
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";

const ROOT = path.resolve(path.dirname(new URL(import.meta.url).pathname), "..");
const BASE_URL = (process.env.BASE_URL || "http://192.168.6.229:18080").replace(/\/$/, "");
const KEEP = process.env.KEEP_UI_INTERACT === "1";
const MIN_TARGETS = Number(process.env.MIN_TARGETS || 200);
const MIN_PROVENANCE = Number(process.env.MIN_PROVENANCE || 200);
const BROWSER = process.env.BROWSER || process.env.CHROMIUM_BIN || "/snap/bin/chromium";
const tmpdir = fs.mkdtempSync(path.join(ROOT, ".ui-interact."));
const userDataDir = path.join(tmpdir, "profile");
fs.mkdirSync(userDataDir, { recursive: true });

function logOK(message) {
  console.log(`OK   ${message}`);
}

function fail(message) {
  console.error(`FAIL ${message}`);
  process.exitCode = 1;
  throw new Error(message);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.listen(0, "127.0.0.1", () => {
      const port = server.address().port;
      server.close(() => resolve(port));
    });
    server.on("error", reject);
  });
}

async function fetchJSON(url, retries = 60) {
  let last;
  for (let i = 0; i < retries; i += 1) {
    try {
      const response = await fetch(url);
      if (response.ok) return await response.json();
      last = `${response.status} ${response.statusText}`;
    } catch (error) {
      last = error.message;
    }
    await sleep(250);
  }
  fail(`cannot fetch ${url}: ${last}`);
}

class CDP {
  constructor(url) {
    this.url = url;
    this.seq = 0;
    this.pending = new Map();
    this.events = [];
  }

  async connect() {
    this.ws = new WebSocket(this.url);
    await new Promise((resolve, reject) => {
      this.ws.addEventListener("open", resolve, { once: true });
      this.ws.addEventListener("error", reject, { once: true });
    });
    this.ws.addEventListener("message", (event) => {
      const message = JSON.parse(event.data);
      if (message.id && this.pending.has(message.id)) {
        const { resolve, reject } = this.pending.get(message.id);
        this.pending.delete(message.id);
        if (message.error) reject(new Error(JSON.stringify(message.error)));
        else resolve(message.result || {});
      } else {
        this.events.push(message);
      }
    });
  }

  send(method, params = {}) {
    const id = ++this.seq;
    this.ws.send(JSON.stringify({ id, method, params }));
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      setTimeout(() => {
        if (this.pending.has(id)) {
          this.pending.delete(id);
          reject(new Error(`CDP timeout: ${method}`));
        }
      }, 10000);
    });
  }

  async eval(expression) {
    const result = await this.send("Runtime.evaluate", {
      expression,
      awaitPromise: true,
      returnByValue: true,
    });
    if (result.exceptionDetails) fail(`browser evaluation failed: ${JSON.stringify(result.exceptionDetails)}`);
    return result.result.value;
  }

  close() {
    try {
      this.ws?.close();
    } catch {
      // best effort
    }
  }
}

let browser;
try {
  const port = await freePort();
  const browserLog = fs.openSync(path.join(tmpdir, "chromium.log"), "w");
  browser = spawn(BROWSER, [
    "--headless=new",
    "--no-sandbox",
    "--disable-gpu",
    `--remote-debugging-port=${port}`,
    `--user-data-dir=${userDataDir}`,
    "about:blank",
  ], { stdio: ["ignore", browserLog, browserLog] });

  browser.on("exit", (code, signal) => {
    if (process.exitCode === undefined && code !== null && code !== 0) {
      process.exitCode = 1;
      console.error(`FAIL browser exited unexpectedly: code=${code} signal=${signal}`);
    }
  });

  const version = await fetchJSON(`http://127.0.0.1:${port}/json/version`);
  const browserCdp = new CDP(version.webSocketDebuggerUrl);
  await browserCdp.connect();
  const target = await browserCdp.send("Target.createTarget", { url: `${BASE_URL}/` });
  const targets = await fetchJSON(`http://127.0.0.1:${port}/json/list`);
  const pageInfo = targets.find((entry) => entry.id === target.targetId) || targets.find((entry) => entry.type === "page");
  if (!pageInfo?.webSocketDebuggerUrl) fail("cannot find page debugging endpoint");

  const page = new CDP(pageInfo.webSocketDebuggerUrl);
  await page.connect();
  await page.send("Page.enable");
  await page.send("Runtime.enable");
  await page.send("Emulation.setDeviceMetricsOverride", {
    width: 1440,
    height: 1000,
    deviceScaleFactor: 1,
    mobile: false,
  });
  await page.send("Page.navigate", { url: `${BASE_URL}/` });
  await sleep(Number(process.env.UI_WAIT_MS || 6500));

  const summary = await page.eval(`(() => {
    const links = Array.from(document.querySelectorAll('.route-link strong')).map((n) => n.textContent.trim());
    const importHref = document.querySelector('a[href="#import-review-tool"]')?.getAttribute('href') || '';
    return {
      title: document.querySelector('h1')?.textContent.trim(),
      targetCount: document.querySelectorAll('.target').length,
      provenanceCount: document.querySelectorAll('.target-provenance').length,
      deviceChips: document.querySelectorAll('.chip.device').length,
      instanceChips: document.querySelectorAll('.chip.instance').length,
      transportChips: document.querySelectorAll('.chip.transport').length,
      writeChips: document.querySelectorAll('.chip.write').length,
      editors: document.querySelectorAll('.target-editor input,.target-editor select').length,
      graphTiles: document.querySelectorAll('#wall .loom-graph-card, #wall [data-graph-id], #wall canvas, #wall svg').length,
      events: document.querySelectorAll('.event').length,
      links,
      importHref,
      reviewButton: !!document.querySelector('#reviewTailButton'),
      loading: document.body.textContent.includes('loading...'),
      sample: Array.from(document.querySelectorAll('.target')).slice(0, 4).map((target) => ({
        id: target.dataset.targetId,
        device: target.dataset.device,
        instance: target.dataset.instance,
        transport: target.dataset.transport,
        chips: Array.from(target.querySelectorAll('.chip')).map((chip) => ({
          className: chip.className,
          label: chip.textContent.trim(),
          prefix: getComputedStyle(chip, '::before').content.replace(/^["']|["']$/g, '').trim(),
          width: Math.round(chip.getBoundingClientRect().width),
        })),
      })),
    };
  })()`);

  if (summary.loading) fail("UI still contains loading placeholders");
  if (summary.targetCount < MIN_TARGETS) fail(`target count too low: ${summary.targetCount} < ${MIN_TARGETS}`);
  if (summary.provenanceCount < MIN_PROVENANCE) fail(`provenance count too low: ${summary.provenanceCount} < ${MIN_PROVENANCE}`);
  if (summary.deviceChips < MIN_PROVENANCE || summary.instanceChips < MIN_PROVENANCE || summary.transportChips < MIN_PROVENANCE) {
    fail(`provenance chip coverage incomplete: device=${summary.deviceChips} instance=${summary.instanceChips} transport=${summary.transportChips}`);
  }
  if (summary.editors < 16 || summary.writeChips < 16) fail(`writable path coverage too low: editors=${summary.editors} writeChips=${summary.writeChips}`);
  for (const label of ["Health", "Source Catalogue", "Signal Tree", "Ring Log", "Export NDJSON", "Export Arrow IPC", "Archive", "Import Review", "CAN Ring"]) {
    if (!summary.links.includes(label)) fail(`route link missing: ${label}`);
  }
  if (summary.importHref !== "#import-review-tool" || !summary.reviewButton) fail("import review is not exposed as an in-page tool");
  for (const sample of summary.sample) {
    if (!sample.id || !sample.device || !sample.instance || !sample.transport || sample.chips.length < 4) {
      fail(`sample target has unclear provenance: ${JSON.stringify(sample)}`);
    }
    const prefixes = new Set(sample.chips.map((chip) => chip.prefix));
    for (const prefix of ["dev", "inst", "param", "src"]) {
      if (!prefixes.has(prefix)) fail(`sample target is missing visible provenance prefix ${prefix}: ${JSON.stringify(sample)}`);
    }
    if (sample.chips.some((chip) => chip.width < 28)) fail(`sample target provenance chip is visually too small: ${JSON.stringify(sample)}`);
  }
  logOK(`provenance visible: ${summary.targetCount} targets, ${summary.provenanceCount} provenance rows, ${summary.editors} writable controls`);

  await page.eval(`document.querySelector('#focusToggle').click()`);
  await sleep(750);
  const focus = await page.eval(`(() => {
    const aside = getComputedStyle(document.querySelector('aside'));
    const wall = document.querySelector('#wall');
    const section = wall.closest('section');
    const wallRect = wall.getBoundingClientRect();
    return {
      active: document.body.classList.contains('graph-focus'),
      asideDisplay: aside.display,
      button: document.querySelector('#focusToggle').textContent.trim(),
      wallWidth: Math.round(wallRect.width),
      wallHeight: Math.round(wallRect.height),
      sectionHeight: Math.round(section.getBoundingClientRect().height),
    };
  })()`);
  if (!focus.active || focus.asideDisplay !== "none" || focus.button !== "Exit Focus") {
    fail(`graph focus mode did not engage cleanly: ${JSON.stringify(focus)}`);
  }
  if (focus.wallWidth < 1000 || focus.wallHeight < 250 || focus.sectionHeight < 900) {
    fail(`graph focus layout too small: ${JSON.stringify(focus)}`);
  }
  logOK(`graph focus mode usable: wall=${focus.wallWidth}x${focus.wallHeight}, section=${focus.sectionHeight}px`);

  await page.eval(`document.querySelector('#focusToggle').click()`);
  await sleep(500);
  const filter = await page.eval(`(() => {
    const input = document.querySelector('[data-wall-section="power"]');
    input.click();
    const off = document.querySelector('#wall').classList.contains('hide-power');
    input.click();
    const on = !document.querySelector('#wall').classList.contains('hide-power');
    return { off, on };
  })()`);
  if (!filter.off || !filter.on) fail(`graph filter controls did not update the wall: ${JSON.stringify(filter)}`);
  logOK("graph wall filters remain interactive");

  const review = await page.eval(`(async () => {
    const button = document.querySelector('#reviewTailButton');
    button.click();
    const start = Date.now();
    while (Date.now() - start < 7000) {
      const text = document.querySelector('#reviewTailResult')?.textContent || '';
      if (text && !text.includes('idle')) return text.slice(0, 500);
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
    return document.querySelector('#reviewTailResult')?.textContent || '';
  })()`);
  if (!review || /idle|405|method not allowed/i.test(review)) fail(`import review tool did not return a useful result: ${review}`);
  logOK("import review tool submits ring tail without exposing a dead GET link");

  const screenshot = await page.send("Page.captureScreenshot", { format: "png", fromSurface: true });
  const screenshotPath = path.join(tmpdir, "interaction.png");
  fs.writeFileSync(screenshotPath, Buffer.from(screenshot.data, "base64"));
  const bytes = fs.statSync(screenshotPath).size;
  if (bytes < 50000) fail(`interaction screenshot too small: ${bytes}`);
  logOK(`browser interaction screenshot captured: ${screenshotPath} (${bytes} bytes)`);
  logOK(`browser interaction smoke passed at ${BASE_URL}/`);

  page.close();
  browserCdp.close();
} finally {
  if (browser && !browser.killed) browser.kill("SIGTERM");
  if (!KEEP) {
    fs.rmSync(tmpdir, { recursive: true, force: true });
  } else {
    console.log(`OK   retained browser interaction artifacts in ${tmpdir}`);
  }
}
