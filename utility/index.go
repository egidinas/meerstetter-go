package utility

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Meerstetter Utility</title>
  <link rel="stylesheet" href="/assets/shared/graphwall/renderer.css">
  <style>
    :root { color-scheme: dark; font-family: system-ui, sans-serif; background: #111; color: #eee; font-size: 14px; }
    html { height: 100%; }
    body { margin: 0; min-height: 100dvh; overflow: hidden; }
    header { padding: 10px 14px; border-bottom: 1px solid #333; display: flex; align-items: center; justify-content: space-between; gap: 10px; }
    .app-title { display: flex; align-items: center; gap: 8px; min-width: 0; }
    .app-actions { display: flex; align-items: center; gap: 6px; }
    .icon-button { min-width: 32px; height: 30px; display: inline-grid; place-items: center; border: 1px solid #405466; border-radius: 5px; background: #1c2a34; color: #dbe8f2; cursor: pointer; font: inherit; }
    .icon-button.text { padding: 0 9px; width: auto; }
    #treeToggle { display: none; }
    main { display: grid; grid-template-columns: minmax(260px, 340px) minmax(0, 1fr); height: calc(100dvh - 51px); min-height: 0; }
    aside { border-right: 1px solid #333; padding: 14px; overflow: auto; min-width: 0; }
    section { padding: 14px; overflow: auto; min-width: 0; min-height: 0; }
    .side-section { border-bottom: 1px solid #2b2b2b; padding: 6px 0 10px; }
    .side-section > summary { cursor: pointer; user-select: none; font-size: 15px; font-weight: 750; }
    .project-status { display: grid; gap: 5px; margin-top: 10px; color: #b9c8d4; font-size: 12px; }
    .project-status strong { color: #edf4fa; font-weight: 700; }
    .route-grid { display: grid; gap: 5px; margin-top: 10px; }
    .route-link { display: flex; justify-content: space-between; gap: 8px; padding: 5px 6px; border: 1px solid #344554; border-radius: 5px; background: #141c23; color: #dbe8f2; text-decoration: none; }
    .route-link:hover { border-color: #587086; background: #172531; }
    .route-link span { color: #8fa1b0; font-size: 11px; white-space: nowrap; }
    .side-controls { display: grid; gap: 8px; margin-top: 10px; font-size: 13px; }
    .side-controls label { display: flex; align-items: center; gap: 8px; color: #d7dee5; }
    .tree ul { list-style: none; padding-left: 12px; }
    .tree li { margin: 4px 0; }
    .tree details { margin: 4px 0; }
    .tree summary { cursor: pointer; user-select: none; font-weight: 650; }
    .target { color: #a7d7ff; font-size: 12px; }
    .target details { margin: 0; }
    .target summary { display: grid; grid-template-columns: minmax(80px, 1fr) auto auto auto; gap: 5px; align-items: center; list-style: none; }
    .target summary::-webkit-details-marker { display: none; }
    .target-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .target-value { color: #e3edf5; background: #18222b; border: 1px solid #344554; border-radius: 5px; padding: 2px 5px; min-width: 48px; text-align: right; }
    .target-label { color: #c7d4df; font-size: 11px; margin-left: 4px; }
    .target-value.absent { color: #84919d; border-style: dashed; }
    .target-editor { display: flex; gap: 4px; align-items: center; }
    .target-editor input, .target-editor select { width: 74px; min-width: 0; background: #101820; color: #edf4fa; border: 1px solid #3d4d5c; border-radius: 5px; padding: 2px 4px; font: inherit; }
    .target-editor button, .target-refresh { width: 24px; height: 22px; display: inline-grid; place-items: center; border: 1px solid #405466; border-radius: 5px; background: #1c2a34; color: #dbe8f2; cursor: pointer; }
    .target-refresh { color: #9fcfff; }
    .target-editor button.ok, .graph-assign button.ok { border-color: #3b7f57; background: #173823; color: #76e09d; }
    .target-editor button.error, .graph-assign button.error { border-color: #8d4b55; background: #3a1c22; color: #ffb2b2; }
    .target-detail { margin: 5px 0 0 14px; color: #98a8b6; font-size: 12px; display: grid; gap: 4px; }
    .target-detail strong { color: #d7e2eb; font-size: 12px; }
    .target-detail .enum-options { display: flex; flex-wrap: wrap; gap: 4px; }
    .target-detail .enum-option { border: 1px solid #334654; background: #141c23; border-radius: 5px; padding: 1px 5px; }
    .target-provenance { margin: 4px 0 0 14px; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 4px; color: #9fb1c0; }
    .target-provenance .chip { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; border: 1px solid #344554; background: #111a22; border-radius: 5px; padding: 2px 5px; font-size: 11px; line-height: 1.25; }
    .target-provenance .chip.transport, .target-provenance .chip.readout { grid-column: 1 / -1; }
    .target-provenance .chip::before { color: #8fa1b0; font-weight: 700; }
    .target-provenance .chip.device { color: #cde8ff; border-color: #416883; }
    .target-provenance .chip.device::before { content: 'dev '; }
    .target-provenance .chip.instance { color: #d7f2c7; border-color: #486b3d; }
    .target-provenance .chip.instance::before { content: 'inst '; }
    .target-provenance .chip.parameter::before { content: 'param '; }
    .target-provenance .chip.transport { color: #ffdba0; border-color: #775f35; }
    .target-provenance .chip.transport::before { content: 'src '; }
    .target-provenance .chip.readout::before { content: 'read '; }
    .target-provenance .chip.write { color: #ffb8c0; border-color: #81424f; }
    .target-provenance .chip.read::before, .target-provenance .chip.write::before { content: 'mode '; }
    .graph-assign { margin: 6px 0 0 14px; display: grid; grid-template-columns: minmax(80px, 1fr) minmax(72px, 1fr) auto; gap: 4px; }
    .graph-assign input, .graph-assign select { min-width: 0; background: #101820; color: #edf4fa; border: 1px solid #3d4d5c; border-radius: 5px; padding: 2px 4px; font: inherit; }
    .graph-assign button { width: 24px; height: 22px; display: inline-grid; place-items: center; border: 1px solid #405466; border-radius: 5px; background: #1c2a34; color: #dbe8f2; cursor: pointer; }
    .tool-card { display: grid; gap: 6px; margin-top: 10px; padding: 8px; border: 1px solid #344554; border-radius: 6px; background: #121b23; font-size: 12px; color: #c1ced8; }
    .tool-card button { justify-self: start; border: 1px solid #405466; border-radius: 5px; background: #1c2a34; color: #dbe8f2; padding: 4px 7px; cursor: pointer; font: inherit; }
    .tool-card pre { max-height: 120px; overflow: auto; margin: 0; padding: 6px; border: 1px solid #2d3d4a; border-radius: 5px; background: #0c1116; white-space: pre-wrap; }
    body.graph-focus header, body.graph-focus aside { display: none; }
    body.graph-focus main { display: block; min-height: 100vh; }
    body.graph-focus section { height: 100vh; box-sizing: border-box; }
    body.graph-focus h2 { display: none; }
    body.tree-open::after { content: ''; position: fixed; inset: 0; background: rgb(0 0 0 / 0.45); z-index: 5; }
    .events { display: grid; grid-template-rows: auto 1fr; gap: 10px; margin-top: 16px; border-top: 1px solid #333; padding-top: 12px; }
    .events.hide-events { display: none; }
    .event { display: grid; grid-template-columns: 100px 90px 1fr; gap: 8px; padding: 6px 0; border-bottom: 1px solid #262626; font-size: 13px; }
    .event.error { color: #ff9f9f; }
    .event.warning { color: #ffd28a; }
    code { color: #ffd28a; }
    @media (max-width: 760px) {
      :root { font-size: 11px; }
      header { padding: 5px 7px; min-height: 32px; }
      header strong { font-size: 12px; }
      header code { font-size: 10px; max-width: 42vw; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
      #treeToggle { display: inline-grid; }
      body { overflow: hidden; }
      main { display: block; height: calc(100dvh - 33px); min-height: 0; }
      aside { position: fixed; z-index: 10; top: 0; bottom: 0; left: 0; width: min(86vw, 360px); box-sizing: border-box; background: #111820; border-right: 1px solid #33404c; transform: translateX(-102%); transition: transform 140ms ease; padding: 10px; }
      body.tree-open aside { transform: translateX(0); }
      section { padding: 5px; }
      section h2 { font-size: 12px; margin: 0 0 5px; }
      .side-section > summary { font-size: 13px; }
      .side-controls { font-size: 12px; gap: 6px; }
      .tree ul { padding-left: 9px; }
      .tree li, .tree details { margin: 3px 0; }
      .target { font-size: 11px; }
      .target summary { grid-template-columns: minmax(70px, 1fr) auto auto; gap: 4px; }
      .target-refresh { display: none; }
      .target-value { min-width: 42px; padding: 1px 4px; }
      .target-editor input, .target-editor select { width: 58px; }
      .target-editor button { width: 22px; height: 20px; }
      .target-detail { font-size: 11px; margin-left: 8px; gap: 3px; }
      .target-provenance { margin-left: 8px; grid-template-columns: 1fr; }
      .target-provenance .chip { font-size: 10px; }
      .graph-assign { margin-left: 8px; grid-template-columns: minmax(64px, 1fr) minmax(58px, 1fr) auto; }
      body.graph-focus section { padding: 5px; }
    }
    @media (max-width: 430px) {
      :root { font-size: 10px; }
      .app-actions .icon-button.text { padding: 0 5px; }
    }
    @media (min-width: 1500px) {
      main { grid-template-columns: minmax(300px, 380px) minmax(0, 1fr); }
    }
  </style>
</head>
<body>
  <header>
    <div class="app-title"><button id="treeToggle" class="icon-button" type="button" title="Open value tree">☰</button><strong>Meerstetter Utility</strong></div>
    <div class="app-actions"><button id="focusToggle" class="icon-button text" type="button" title="Focus graph wall">Focus Graph</button><code>__LISTEN_ADDR__</code></div>
  </header>
  <main>
    <aside>
      <details class="side-section" open>
        <summary>Project</summary>
        <div id="projectStatus" class="project-status">
          <div><strong>Primary path</strong> PiXtend SocketCAN.</div>
          <div><strong>Fallbacks</strong> serial target route, RAM ring, flash ring.</div>
          <div><strong>Live status</strong> checking...</div>
        </div>
        <div class="route-grid">
          <a class="route-link" href="/api/health"><strong>Health</strong><span>route</span></a>
          <a class="route-link" href="/api/loom/source-catalogue"><strong>Source Catalogue</strong><span>Loom</span></a>
          <a class="route-link" href="/api/discovery/tree"><strong>Signal Tree</strong><span>targets</span></a>
          <a class="route-link" href="/api/log/ring?tail=true&limit=2500"><strong>Ring Log</strong><span>live</span></a>
          <a class="route-link" href="/api/log/export?tail=true&limit=2500"><strong>Export NDJSON</strong><span>tail</span></a>
          <a class="route-link" href="/api/log/export?format=arrow_ipc&tail=true&limit=2500"><strong>Export Arrow IPC</strong><span>binary</span></a>
          <a class="route-link" href="/api/log/archive/manifest"><strong>Archive</strong><span>schema</span></a>
          <a class="route-link" href="#import-review-tool"><strong>Import Review</strong><span>tool</span></a>
          <a class="route-link" href="/api/can/ring?tail=true&limit=128"><strong>CAN Ring</strong><span>raw</span></a>
        </div>
        <div id="import-review-tool" class="tool-card">
          <strong>Replay review</strong>
          <span>Exports the current ring tail and posts it to the non-mutating import review path.</span>
          <button id="reviewTailButton" type="button">Review Ring Tail</button>
          <pre id="reviewTailResult">idle</pre>
        </div>
      </details>
      <details class="side-section" open><summary>Discovery</summary><div id="tree" class="tree"></div></details>
      <details class="side-section" open>
        <summary>Graph Sections</summary>
        <div class="side-controls">
          <label><input type="checkbox" data-wall-section="temperature" checked> Temperatures</label>
          <label><input type="checkbox" data-wall-section="target" checked> Targets</label>
          <label><input type="checkbox" data-wall-section="power" checked> Power</label>
          <label><input type="checkbox" data-wall-section="events" checked> Event Swimlane</label>
        </div>
      </details>
    </aside>
    <section><h2>Graph Wall</h2><div id="wall" class="wall operator-graph-wall"></div><div class="events"><h2>Events</h2><div id="events"></div></div></section>
  </main>
  <script src="/assets/shared/graphwall/renderer.js"></script>
  <script>
    const treeLeavesByTarget = new Map();
    const targetInfoByID = new Map();
    const latestTelemetryByTarget = new Map();
    const ringBatchLimit = 2500;
    let latestSeq = 0;
    let ringBootstrapped = false;
    let graphWall = null;

    async function getJSON(path) { const r = await fetch(path); return await r.json(); }
    async function postJSON(path, body) {
      const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      return await r.json();
    }
    async function postText(path, body) {
      const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/x-ndjson' }, body });
      return await r.json();
    }
    function formatValue(value) {
      if (window.LoomGraphWall && typeof window.LoomGraphWall.formatValue === 'function') return window.LoomGraphWall.formatValue(value);
      if (value === null || value === undefined) return 'absent';
      if (typeof value === 'number') {
        if (!Number.isFinite(value)) return String(value);
        if (Math.abs(value) >= 1000 || (Math.abs(value) > 0 && Math.abs(value) < 0.01)) return value.toExponential(3);
        const places = Math.abs(value) >= 10 ? 2 : 3;
        return value.toFixed(places).replace(/\.?0+$/, '');
      }
      return String(value);
    }
    function graphIDs() { return graphWall ? graphWall.graphIDs() : ['baseline']; }
    function upsertGraphTile(assignment) { if (graphWall) graphWall.upsertGraphTile(assignment); }
    function appendTelemetry(tm) { if (graphWall) graphWall.appendTelemetry(tm); else if (tm && tm.target_id) updateTreeLeaf(tm.target_id, tm); }
    function renderEvents(events) { if (graphWall) graphWall.renderEvents(events); }
    function redrawAllTiles() { if (graphWall) graphWall.redrawAllTiles(); }
    function updateProjectStatus(health) {
      const status = document.getElementById('projectStatus');
      if (!status) return;
      const healthDevices = health && health.devices;
      const devices = Array.isArray(healthDevices) ? healthDevices.length : (Number.isFinite(healthDevices) ? healthDevices : (health && health.device_count) || 0);
      const latest = (health && (health.latest_seq || health.latestSeq)) || 0;
      const rings = health && health.rings ? health.rings : {};
      const canRing = health && health.can_ring ? health.can_ring : {};
      const recordCount = (stats) => {
        if (!stats) return 'n/a';
        if (stats.total_records !== undefined) return stats.total_records;
        if (stats.TotalRecords !== undefined) return stats.TotalRecords;
        return 'n/a';
      };
      const ram = recordCount(rings.ram || canRing.stats);
      const flash = recordCount(rings.flash || canRing.fallback);
      const ok = health && health.ok ? 'ok' : 'degraded';
      status.replaceChildren();
      for (const row of [
        ['Primary path', 'PiXtend SocketCAN to Meerstetter TEC controllers.'],
        ['Fallbacks', 'serial target route, RAM ring, flash ring.'],
        ['Live status', ok + ' · devices=' + devices + ' · latest_seq=' + latest],
        ['Ring layers', 'ram=' + ram + ' · flash=' + flash],
      ]) {
        const div = document.createElement('div');
        const strong = document.createElement('strong');
        strong.textContent = row[0] + ' ';
        div.append(strong, row[1]);
        status.appendChild(div);
      }
    }

    function renderTree(node) {
      const ul = document.createElement('ul');
      for (const child of node.children || []) {
        const li = document.createElement('li');
        const details = document.createElement('details');
        details.open = true;
        const summary = document.createElement('summary');
        summary.textContent = child.name;
        details.appendChild(summary);
        details.appendChild(renderTree(child));
        details.addEventListener('toggle', () => { if (details.open) refreshVisibleLeaves(details); }, { once: false });
        li.appendChild(details);
        ul.appendChild(li);
      }
      for (const target of node.targets || []) {
        targetInfoByID.set(target.id, target);
        const li = document.createElement('li');
        li.className = 'target';
        li.dataset.targetId = target.id;
        li.dataset.readable = isReadable(target) ? 'true' : 'false';
        li.dataset.writable = isWritable(target) ? 'true' : 'false';
        li.dataset.device = deviceName(target);
        li.dataset.instance = instanceName(target);
        li.dataset.transport = activeTransport(target);
        li.innerHTML = '<details><summary><span class="target-name"></span><button class="target-refresh" title="Read current value">↻</button><span class="target-value absent">absent</span><span class="target-editor"></span></summary><div class="target-provenance"></div><div class="target-detail"></div><div class="graph-assign"></div></details>';
        li.querySelector('.target-name').textContent = target.name;
        li.title = target.id;
        const refresh = li.querySelector('.target-refresh');
        const currentValue = li.querySelector('.target-value');
        refresh.hidden = !isReadable(target);
        currentValue.hidden = !isReadable(target);
        refresh.addEventListener('click', (event) => {
          event.preventDefault();
          event.stopPropagation();
          readTreeTarget(target.id);
        });
        renderEditor(li.querySelector('.target-editor'), target);
        renderTargetProvenance(li.querySelector('.target-provenance'), target);
        renderTargetDetail(li.querySelector('.target-detail'), target);
        if (isReadable(target)) renderGraphAssign(li.querySelector('.graph-assign'), target);
        const leaves = treeLeavesByTarget.get(target.id) || [];
        leaves.push(li);
        treeLeavesByTarget.set(target.id, leaves);
        const latest = latestTelemetryByTarget.get(target.id);
        if (latest) updateTreeLeaf(target.id, latest);
        ul.appendChild(li);
      }
      return ul;
    }
    function renderEditor(container, target) {
      if (!isWritable(target)) return;
      const enumValues = enumMap(target);
      let input;
      if (enumValues && typeof enumValues === 'object') {
        input = document.createElement('select');
        for (const [value, label] of Object.entries(enumValues)) {
          const option = document.createElement('option');
          option.value = value;
          option.textContent = value + ' · ' + label;
          input.appendChild(option);
        }
      } else {
        input = document.createElement('input');
        input.type = target.kind === 'enum' || valueType(target) === 'INT32' || valueType(target) === 'UINT32' ? 'number' : 'text';
        input.inputMode = input.type === 'number' ? 'numeric' : 'decimal';
      }
      input.dataset.targetInput = target.id;
      const button = document.createElement('button');
      button.type = 'button';
      button.textContent = '✓';
      button.title = 'Set value';
      button.addEventListener('click', (event) => {
        event.preventDefault();
        event.stopPropagation();
        writeTreeTarget(target.id, input.value, button);
      });
      container.append(input, button);
    }
    function enumMap(target) { return target.enum || null; }
    function isReadable(target) { return !target.metadata || target.metadata.readable !== 'false'; }
    function isWritable(target) { return !!(target && target.metadata && target.metadata.writable === 'true'); }
    function valueType(target) { return (target.metadata && (target.metadata.value_type || target.metadata.format)) || target.kind || 'unknown'; }
    function deviceName(target) {
      const match = target && target.id ? target.id.match(/^device:([^:]+):/) : null;
      return (target.metadata && (target.metadata.device_name || target.metadata.alias || target.metadata.serial_number)) || (match ? match[1] : 'device');
    }
    function instanceName(target) {
      return (target.metadata && (target.metadata.instance_name || target.metadata.mecom_instance || target.metadata.instance || target.metadata.channel_index)) || '';
    }
    function activeTransport(target) {
      return (target.metadata && (target.metadata.active_transport || target.metadata.primary_transport || target.metadata.preferred_transport)) || 'unknown';
    }
    function shortTransport(value) {
      if (!value) return 'unknown';
      const text = String(value);
      const can = text.match(/^canopen:([^?]+)(?:\?node=(0x[0-9a-fA-F]+|\d+))?/);
      if (can) return can[1] + (can[2] ? ' node ' + can[2] : '');
      const serial = text.match(/\/dev\/serial\/by-id\/([^@]+)(?:@(\d+))?/);
      if (serial) return 'serial ' + serial[1].replace(/^usb-/, '') + (serial[2] ? ' @' + serial[2] : '');
      return text.replace('/dev/serial/by-id/', 'serial:');
    }
    function readoutName(target) {
      return (target.metadata && (target.metadata.active_readout || target.metadata.readout || target.metadata.preferred_readout)) || 'single';
    }
    function appendChip(container, className, label, title) {
      const chip = document.createElement('span');
      chip.className = 'chip ' + className;
      chip.textContent = label;
      chip.title = title || label;
      container.appendChild(chip);
    }
    function renderTargetProvenance(container, target) {
      container.replaceChildren();
      const md = target.metadata || {};
      appendChip(container, 'device', deviceName(target), 'Device / alias / serial source: ' + deviceName(target));
      appendChip(container, 'instance', instanceName(target) || 'n/a', 'MeCom instance/channel: ' + (instanceName(target) || 'n/a'));
      appendChip(container, 'parameter', md.parameter_id || md.mecom_parameter_id || 'n/a', 'MeCom parameter ID');
      appendChip(container, 'transport', shortTransport(activeTransport(target)), 'Active transport: ' + activeTransport(target) + '\nAvailable transports: ' + (md.available_transports || 'n/a'));
      appendChip(container, 'readout', readoutName(target), 'Readout path: ' + readoutName(target));
      appendChip(container, isWritable(target) ? 'write' : 'read', isWritable(target) ? 'writable' : 'read-only', (md.write_path || md.read_path || target.id));
    }
    function renderTargetDetail(container, target) {
      const parameterName = target.metadata && target.metadata.parameter_name ? target.metadata.parameter_name : target.name;
      const instanceLabel = target.metadata && target.metadata.instance_name ? target.metadata.instance_name : '';
      const description = target.metadata && target.metadata.description ? target.metadata.description : (target.unit ? parameterName + ' in ' + target.unit : parameterName);
      const rows = [
        'Device: ' + deviceName(target),
        'Name: ' + parameterName,
        'Explanation: ' + description,
        'MeCom ID: ' + ((target.metadata && target.metadata.parameter_id) || 'n/a'),
        'Instance: ' + (instanceLabel || instanceName(target) || 'n/a'),
        'Unit: ' + (target.unit || (target.metadata && target.metadata.unit) || 'n/a'),
        'Value type: ' + valueType(target),
        'Active transport: ' + activeTransport(target),
        'Available transports: ' + ((target.metadata && target.metadata.available_transports) || 'n/a'),
        'Readout: ' + readoutName(target),
        'Priority: ' + ((target.metadata && target.metadata.readout_priority) || 'normal'),
        'Read path: ' + ((target.metadata && target.metadata.read_path) || 'n/a'),
        'Write path: ' + ((target.metadata && target.metadata.write_path) || 'n/a'),
        'Address: ' + (target.address || 'n/a'),
        'Kind: ' + (target.kind || 'unknown') + ' · Direction: ' + (target.direction || 'unknown'),
        'Target ID: ' + target.id
      ];
      const enumValues = enumMap(target);
      container.replaceChildren();
      for (const text of rows) {
        const row = document.createElement('div');
        row.textContent = text;
        container.appendChild(row);
      }
      if (enumValues && typeof enumValues === 'object') {
        const heading = document.createElement('strong');
        heading.textContent = 'Options';
        container.appendChild(heading);
        const options = document.createElement('div');
        options.className = 'enum-options';
        for (const [value, label] of Object.entries(enumValues)) {
          const option = document.createElement('span');
          option.className = 'enum-option';
          option.textContent = value + ' = ' + label;
          options.appendChild(option);
        }
        container.appendChild(options);
      }
    }
    async function reviewRingTail() {
      const result = document.getElementById('reviewTailResult');
      const button = document.getElementById('reviewTailButton');
      if (!result || !button) return;
      button.disabled = true;
      result.textContent = 'reviewing...';
      try {
        const entries = await getJSON('/api/log/ring?tail=true&limit=250');
        const payload = (entries || []).map((entry) => JSON.stringify(entry)).join('\n') + '\n';
        const review = await postText('/api/log/import/review', payload);
        result.textContent = JSON.stringify(review, null, 2).slice(0, 1800);
      } catch (err) {
        result.textContent = err.message || String(err);
      } finally {
        button.disabled = false;
      }
    }
    function renderGraphAssign(container, target) {
      container.replaceChildren();
      const select = document.createElement('select');
      select.title = 'Assign to graph';
      for (const id of graphIDs()) {
        const option = document.createElement('option');
        option.value = id;
        option.textContent = id;
        select.appendChild(option);
      }
      const newOption = document.createElement('option');
      newOption.value = '__new__';
      newOption.textContent = 'New graph...';
      select.appendChild(newOption);
      const name = document.createElement('input');
      name.placeholder = 'graph name';
      name.hidden = true;
      const button = document.createElement('button');
      button.type = 'button';
      button.textContent = '+';
      button.title = 'Assign value to graph';
      select.addEventListener('change', () => { name.hidden = select.value !== '__new__'; });
      button.addEventListener('click', async (event) => {
        event.preventDefault();
        event.stopPropagation();
        button.disabled = true;
        button.classList.remove('ok', 'error');
        try {
          const body = { target_id: target.id, wall_id: select.value === '__new__' ? '' : select.value, new_wall_id: select.value === '__new__' ? name.value : '' };
          const result = await postJSON('/api/graph-wall/assign', body);
          button.classList.add(result && result.ok ? 'ok' : 'error');
          if (result && result.assignment) upsertGraphTile(result.assignment);
          refreshGraphAssignControls();
        } catch (err) {
          button.classList.add('error');
        } finally {
          setTimeout(() => {
            button.disabled = false;
            button.classList.remove('ok', 'error');
          }, 1600);
        }
      });
      container.append(select, name, button);
    }
    function refreshGraphAssignControls() {
      const ids = graphIDs();
      for (const select of document.querySelectorAll('.graph-assign select')) {
        const current = select.value;
        select.replaceChildren();
        for (const id of ids) {
          const option = document.createElement('option');
          option.value = id;
          option.textContent = id;
          select.appendChild(option);
        }
        const newOption = document.createElement('option');
        newOption.value = '__new__';
        newOption.textContent = 'New graph...';
        select.appendChild(newOption);
        if (ids.includes(current) || current === '__new__') select.value = current;
      }
    }
    function refreshVisibleLeaves(root) {
      for (const li of root.querySelectorAll('.target')) {
        if (li.dataset.readable !== 'true') continue;
        const targetID = li.dataset.targetId;
        const current = li.querySelector('.target-value');
        if (current && current.classList.contains('absent')) readTreeTarget(targetID);
      }
    }
    async function readTreeTarget(targetID) {
      try {
        const result = await getJSON('/api/target/read?id=' + encodeURIComponent(targetID));
        if (result && result.telemetry) appendTelemetry(result.telemetry);
        else markAbsent(targetID, result && result.error ? result.error : 'absent');
      } catch (err) {
        markAbsent(targetID, err.message || 'read failed');
      }
    }
    async function writeTreeTarget(targetID, value, button) {
      button.classList.remove('ok', 'error');
      button.disabled = true;
      try {
        const result = await postJSON('/api/target/write', { target_id: targetID, value });
        button.classList.add(result && result.ok ? 'ok' : 'error');
        if (result && result.ok) await readTreeTarget(targetID);
      } catch (err) {
        button.classList.add('error');
      } finally {
        setTimeout(() => {
          button.disabled = false;
          button.classList.remove('ok', 'error');
        }, 1600);
      }
    }
    function markAbsent(targetID, title) {
      for (const li of treeLeavesByTarget.get(targetID) || []) {
        const value = li.querySelector('.target-value');
        value.textContent = 'absent';
        value.classList.add('absent');
        value.title = title || 'absent';
      }
    }
    function updateTreeLeaf(targetID, tm) {
      latestTelemetryByTarget.set(targetID, tm);
      for (const li of treeLeavesByTarget.get(targetID) || []) {
        const value = li.querySelector('.target-value');
        const label = tm.metadata && tm.metadata.value_label;
        value.textContent = formatValue(tm.value) + (label ? ' ' : '');
        if (label) {
          const span = document.createElement('span');
          span.className = 'target-label';
          span.textContent = label;
          value.appendChild(span);
        }
        value.classList.toggle('absent', tm.value === null || tm.value === undefined);
        value.title = label || tm.quality || '';
        const input = li.querySelector('[data-target-input]');
        if (input && tm.value !== null && tm.value !== undefined && document.activeElement !== input) input.value = tm.value;
      }
    }
    async function pollRing() {
      try {
        const path = ringBootstrapped
          ? '/api/log/ring?after_seq=' + latestSeq + '&limit=' + ringBatchLimit
          : '/api/log/ring?tail=true&limit=' + ringBatchLimit;
        const entries = await getJSON(path);
        ringBootstrapped = true;
        for (const entry of entries || []) {
          if (entry.seq && entry.seq > latestSeq) latestSeq = entry.seq;
          if ((entry.kind === 'telemetry' || entry.kind === 'tm') && entry.tm) appendTelemetry(entry.tm);
        }
        if (entries && entries.length >= ringBatchLimit) setTimeout(pollRing, 0);
      } catch (err) {
        console.error(err);
      }
    }
    Promise.all([getJSON('/api/health'), getJSON('/api/discovery/tree'), getJSON('/api/graph-wall'), getJSON('/api/events/swimlane')])
      .then(([health, tree, wall, events]) => {
        updateProjectStatus(health);
        const wallEl = document.getElementById('wall');
        const eventsSection = document.querySelector('.events');
        document.getElementById('tree').replaceChildren(renderTree(tree));
        graphWall = window.LoomGraphWall.createGraphWall({
          wallElement: wallEl,
          eventsElement: document.getElementById('events'),
          getJSON,
          targetInfoByID,
          onTelemetry: (tm) => updateTreeLeaf(tm.target_id, tm),
          tilesURL: (params) => '/api/tiles?' + params.toString(),
        });
        graphWall.renderWall(wall);
        graphWall.renderEvents(events);
        redrawAllTiles();
        document.getElementById('treeToggle').addEventListener('click', () => document.body.classList.toggle('tree-open'));
        const reviewButton = document.getElementById('reviewTailButton');
        if (reviewButton) reviewButton.addEventListener('click', reviewRingTail);
        document.getElementById('focusToggle').addEventListener('click', () => {
          document.body.classList.toggle('graph-focus');
          document.getElementById('focusToggle').textContent = document.body.classList.contains('graph-focus') ? 'Exit Focus' : 'Focus Graph';
          requestAnimationFrame(() => redrawAllTiles());
        });
        document.body.addEventListener('click', (event) => {
          if (!document.body.classList.contains('tree-open')) return;
          if (event.target.closest('aside') || event.target.closest('#treeToggle')) return;
          document.body.classList.remove('tree-open');
        });
        for (const input of document.querySelectorAll('[data-wall-section]')) {
          input.addEventListener('change', () => {
            const section = input.dataset.wallSection;
            wallEl.classList.toggle('hide-' + section, !input.checked);
            if (section === 'events' && eventsSection) {
              eventsSection.classList.toggle('hide-events', !input.checked);
            }
          });
        }
        pollRing();
        setInterval(pollRing, 1000);
        setInterval(() => getJSON('/api/health').then(updateProjectStatus).catch(console.error), 5000);
        setInterval(() => getJSON('/api/events/swimlane').then(renderEvents).catch(console.error), 2000);
        addEventListener('resize', redrawAllTiles);
      });
  </script>
</body>
</html>`
