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
    .graph-assign { margin: 6px 0 0 14px; display: grid; grid-template-columns: minmax(80px, 1fr) minmax(72px, 1fr) auto; gap: 4px; }
    .graph-assign input, .graph-assign select { min-width: 0; background: #101820; color: #edf4fa; border: 1px solid #3d4d5c; border-radius: 5px; padding: 2px 4px; font: inherit; }
    .graph-assign button { width: 24px; height: 22px; display: inline-grid; place-items: center; border: 1px solid #405466; border-radius: 5px; background: #1c2a34; color: #dbe8f2; cursor: pointer; }
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
    let graphWall = null;

    async function getJSON(path) { const r = await fetch(path); return await r.json(); }
    async function postJSON(path, body) {
      const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      return await r.json();
    }
    function formatValue(value) { return window.LoomGraphWall.formatValue(value); }
    function graphIDs() { return graphWall ? graphWall.graphIDs() : ['baseline']; }
    function upsertGraphTile(assignment) { if (graphWall) graphWall.upsertGraphTile(assignment); }
    function appendTelemetry(tm) { if (graphWall) graphWall.appendTelemetry(tm); else if (tm && tm.target_id) updateTreeLeaf(tm.target_id, tm); }
    function renderEvents(events) { if (graphWall) graphWall.renderEvents(events); }
    function redrawAllTiles() { if (graphWall) graphWall.redrawAllTiles(); }

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
        li.innerHTML = '<details><summary><span class="target-name"></span><button class="target-refresh" title="Read current value">↻</button><span class="target-value absent">absent</span><span class="target-editor"></span></summary><div class="target-detail"></div><div class="graph-assign"></div></details>';
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
    function renderTargetDetail(container, target) {
      const parameterName = target.metadata && target.metadata.parameter_name ? target.metadata.parameter_name : target.name;
      const instanceName = target.metadata && target.metadata.instance_name ? target.metadata.instance_name : '';
      const description = target.metadata && target.metadata.description ? target.metadata.description : (target.unit ? parameterName + ' in ' + target.unit : parameterName);
      const rows = [
        'Name: ' + parameterName,
        'Explanation: ' + description,
        'MeCom ID: ' + ((target.metadata && target.metadata.parameter_id) || 'n/a'),
        'Instance: ' + (instanceName || 'n/a'),
        'Unit: ' + (target.unit || (target.metadata && target.metadata.unit) || 'n/a'),
        'Value type: ' + valueType(target),
        'Readout: ' + ((target.metadata && target.metadata.readout) || 'single'),
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
        const entries = await getJSON('/api/log/ring?after_seq=' + latestSeq + '&limit=' + ringBatchLimit);
        for (const entry of entries || []) {
          if (entry.seq && entry.seq > latestSeq) latestSeq = entry.seq;
          if ((entry.kind === 'telemetry' || entry.kind === 'tm') && entry.tm) appendTelemetry(entry.tm);
        }
        if (entries && entries.length >= ringBatchLimit) setTimeout(pollRing, 0);
      } catch (err) {
        console.error(err);
      }
    }
    Promise.all([getJSON('/api/discovery/tree'), getJSON('/api/graph-wall'), getJSON('/api/events/swimlane')])
      .then(([tree, wall, events]) => {
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
        setInterval(() => getJSON('/api/events/swimlane').then(renderEvents).catch(console.error), 2000);
        addEventListener('resize', redrawAllTiles);
      });
  </script>
</body>
</html>`
