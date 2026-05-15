/* ========================================================
   Meerstetter Gateway — fleet & device views (v2)
   ======================================================== */
/* global React, MecomAPI, Pill, Chip, Panel, Sparkline, MultiChart,
   MetricTile, DiscoveryTree, InputCard, useLiveValue, useGatewayTick,
   useToast, categorizeError, getTelemetry,
   useAssignments, WALLS, wallForDevice, signalAddress,
   normalizeAssignment, buildGraphTileFromAssignments, renderSeriesFromGraphTile,
   channelColor,
   HeroGraph, TempSettingsTable, SupplySettingsTable */

const { useState, useEffect, useRef, useMemo, useCallback } = React;

function compactGatewayError(message) {
  const text = String(message || "").replace(/\s+/g, " ").trim();
  if (!text) return "waiting for gateway";
  if (text.startsWith("<")) return "gateway unavailable";
  return text.length > 96 ? `${text.slice(0, 93)}...` : text;
}

/* ============================================================
   FLEET VIEW — hero-graph centric
   ============================================================ */
function FleetView({ onOpenDevice }) {
  useGatewayTick();
  const assigns = useAssignments();
  const devices = MecomAPI.devices();
  const channels = MecomAPI.channels();
  const leases = MecomAPI.leases();
  const events = MecomAPI.commandEvents();
  const settings = MecomAPI.settings();
  const isLive = MecomAPI.isLive();
  const liveError = MecomAPI.liveError && MecomAPI.liveError();

  const tempChannels = channels.filter((c) => c.role === "temp");
  const supplyChannels = channels.filter((c) => c.role === "supply");

  // Build SignalForge graph tiles directly from graph-wall assignments.
  const tempTile = useMemo(() =>
    buildGraphTileFromAssignments(assigns.forWall(WALLS.fleetTemp.wall_id), {
      tile_id: WALLS.fleetTemp.wall_id,
      title: WALLS.fleetTemp.label,
      colorByChannel: false,
      timeWindowMs: 90_000,
    }),
    [assigns.list]);
  const supplyTile = useMemo(() =>
    buildGraphTileFromAssignments(assigns.forWall(WALLS.fleetSupply.wall_id), {
      tile_id: WALLS.fleetSupply.wall_id,
      title: WALLS.fleetSupply.label,
      colorByChannel: false,
      timeWindowMs: 90_000,
    }),
    [assigns.list]);

  // Stats
  const total = devices.length;
  const bound = devices.filter((d) => d.bound).length;
  const errors = devices.filter((d) => d.last_error).length;
  const leasedByMe = leases.filter((l) => l.holder === settings.holder).length;
  const leasedByOthers = leases.length - leasedByMe;
  const writesLastMinute = events.filter((e) => {
    const t = new Date(e.time).getTime();
    return (Date.now() - t) < 60_000 && (e.status === "completed" || e.status === "accepted");
  }).length;

  return (
    <div className="fleet">
      <div className="stat-strip">
        <div className="stat">
          <div className="label">Gateway</div>
          <div className={`value ${isLive ? "ok" : liveError ? "warn" : ""}`}>
            {isLive ? "live" : liveError ? "fallback" : "checking"}
          </div>
          <div className="sub">{isLive ? (settings.gateway || "same-origin /api") : compactGatewayError(liveError)}</div>
        </div>
        <div className="stat">
          <div className="label">Devices</div>
          <div className="value">{bound}<span className="unit">/ {total}</span></div>
          <div className="sub">{bound} bound · {errors} {errors === 1 ? "error" : "errors"}</div>
        </div>
        <div className="stat">
          <div className="label">Channels</div>
          <div className="value">{channels.length}</div>
          <div className="sub">{tempChannels.length} temp · {supplyChannels.length} supply</div>
        </div>
        <div className="stat">
          <div className="label">Leases</div>
          <div className="value">{leases.length}</div>
          <div className="sub">{leasedByMe} you · {leasedByOthers} others</div>
        </div>
        <div className="stat">
          <div className="label">Writes · 1m</div>
          <div className="value">{writesLastMinute}</div>
          <div className="sub">completed + accepted</div>
        </div>
        <div className="stat">
          <div className="label">Scenario</div>
          <div className="value" style={{ fontSize: 14, textTransform: "capitalize" }}>{settings.scenario.replace("-", " ")}</div>
          <div className="sub">change via Tweaks</div>
        </div>
      </div>

      {/* Two hero graphs */}
      <div className="fleet-heroes">
        <HeroGraph wall={WALLS.fleetTemp} role="temp" tile={tempTile} height={260}>
          <TempSettingsTable channels={tempChannels} holderId={settings.holder} />
        </HeroGraph>
        <HeroGraph wall={WALLS.fleetSupply} role="supply" tile={supplyTile} height={260}>
          {supplyChannels.length === 0 ? (
            <div style={{ padding: 24, textAlign: "center", color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
              No supply-mode channels configured. Set a channel role under Signal Dictionary → Metadata.
            </div>
          ) : (
            <SupplySettingsTable channels={supplyChannels} holderId={settings.holder} />
          )}
        </HeroGraph>
      </div>

      {/* Device strip + activity */}
      <div style={{ display: "grid", gridTemplateColumns: "minmax(0,2fr) minmax(280px, 1fr)", gap: 8 }}>
        <Panel title="Devices" meta={`${devices.length} configured`} flush>
          <div className="fleet-strip" style={{ padding: 8 }}>
            {devices.map((d) => (
              <DeviceMini key={d.id} device={d} onOpen={() => onOpenDevice(d.id)} />
            ))}
          </div>
        </Panel>
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          <Panel title="Command activity" meta={`${events.length} recent`} flush>
            <CommandFeed events={events} />
          </Panel>
          <Panel title="Lease ownership" right={<Chip>{leases.length} active</Chip>} flush>
            <LeaseSummary leases={leases} holder={settings.holder} />
          </Panel>
        </div>
      </div>

      {/* Telemetry / Telecommand accordions — in context with hero graphs */}
      <SignalsAccordion role="monitor"
                        channels={channels}
                        title="Telemetry"
                        defaultOpen={false}
                        storageKey="mecomgw.acc.fleet.monitor" />
      <SignalsAccordion role="control"
                        channels={channels}
                        title="Telecommands"
                        defaultOpen={true}
                        storageKey="mecomgw.acc.fleet.control" />
    </div>
  );
}

function DeviceMini({ device, onOpen }) {
  const channels = MecomAPI.channels().filter((c) => c.device_id === device.id);
  const leases = MecomAPI.leases();
  const lease = leases.find((l) => l.device_id === device.id);
  const bs = MecomAPI.brokerStats(device.id);
  return (
    <div className={"dev-mini" + (device.last_error ? " bad" : "")} onClick={onOpen}>
      <div className="top">
        <span className="name">{device.label}</span>
        <span className="id">{device.id}</span>
        <span className="right">
          <Pill kind={device.bound ? "ok" : "bad"}>{device.bound ? "bound" : "unbound"}</Pill>
        </span>
      </div>
      <div className="chans">
        {channels.map((c) => (
          <Chip key={c.instance} kind={c.role === "supply" ? "warn" : "accent"}>
            ch{c.instance} · {c.role === "supply" ? "supply" : "temp"}
          </Chip>
        ))}
        {channels.length === 0 && <Chip>no channels</Chip>}
        {lease && <Chip kind={lease.holder === MecomAPI.settings().holder ? "accent" : "warn"} title={lease.holder}>{lease.holder === MecomAPI.settings().holder ? "you hold" : lease.holder}</Chip>}
      </div>
      <div className="stats">
        <span>{bs.frames_in.toLocaleString()} ↓</span>
        <span>{bs.frames_out.toLocaleString()} ↑</span>
        {bs.error_count > 0 && <span style={{ color: "var(--warn)" }}>{bs.error_count} err</span>}
        <span style={{ marginLeft: "auto" }}>{device.endpoint}</span>
      </div>
    </div>
  );
}

function CommandFeed({ events }) {
  return (
    <div className="feed">
      {events.length === 0 && <div style={{ padding: 14, color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>No recent commands.</div>}
      {events.slice(0, 16).map((e) => {
        const t = new Date(e.time);
        const hh = String(t.getHours()).padStart(2, "0") + ":" + String(t.getMinutes()).padStart(2, "0") + ":" + String(t.getSeconds()).padStart(2, "0");
        let stChip = "ok";
        if (e.status === "rejected" || e.status === "failed") stChip = "bad";
        if (e.status === "accepted" || e.status === "sent") stChip = "accent";
        // Rich row: signal name + prev → req + holder + endpoint
        let msg;
        if (e.error) {
          msg = e.error_category ? `[${e.error_category}] ${e.error}` : e.error;
        } else if (e.signal_name !== undefined) {
          const prev = e.prev_value === null || e.prev_value === undefined ? "—" :
                       (typeof e.prev_value === "number" ? e.prev_value.toFixed(3) : String(e.prev_value));
          const req  = typeof e.requested_value === "number" ? e.requested_value.toFixed(3) : String(e.requested_value);
          msg = `${e.signal_name} ${prev} → ${req}${e.unit ? " " + e.unit : ""}`;
        } else if (e.result && e.result.arguments) {
          msg = `${e.result.name || "write"} param=${e.result.arguments.param}` +
                (e.result.arguments.instance !== undefined ? `/${e.result.arguments.instance}` : "") +
                (e.result.arguments.value !== undefined ? ` value=${e.result.arguments.value}` : "");
        } else {
          msg = e.command_id || "—";
        }
        const tooltip = [
          e.transport && `endpoint=${e.transport}`,
          e.lease_holder && `holder=${e.lease_holder}`,
          e.instance !== undefined && e.param_id !== undefined && `param=${e.param_id} instance=${e.instance}`,
        ].filter(Boolean).join(" ");
        return (
          <div key={e.command_id} className="ev">
            <span className="t">{hh}</span>
            <span className="dev">{e.target_id || "—"}{e.instance !== undefined ? "/" + e.instance : ""}</span>
            <span className="msg" title={tooltip}>{msg}</span>
            <Chip kind={stChip}>{e.status}</Chip>
          </div>
        );
      })}
    </div>
  );
}

function LeaseSummary({ leases, holder }) {
  if (!leases.length) {
    return <div style={{ padding: 14, color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>No active leases.</div>;
  }
  return (
    <div className="feed">
      {leases.map((l) => {
        const remain = Math.max(0, new Date(l.expires_at).getTime() - Date.now());
        const mins = Math.floor(remain / 60000);
        const secs = Math.floor((remain % 60000) / 1000);
        const mine = l.holder === holder;
        return (
          <div key={l.token} className="ev" style={{ gridTemplateColumns: "76px minmax(0,1fr) auto" }}>
            <span className="t">{l.device_id}</span>
            <span className="msg">
              held by <b style={{ color: mine ? "var(--accent)" : "var(--text)" }}>{l.holder}</b>
            </span>
            <Chip kind={mine ? "accent" : "warn"}>{mins}m {String(secs).padStart(2,"0")}s</Chip>
          </div>
        );
      })}
    </div>
  );
}

/* ============================================================
   DEVICE WORKSPACE — channel-aware
   ============================================================ */
function DeviceWorkspace({ deviceId, onOpenSequencer }) {
  useGatewayTick();
  const catalogue = useMemo(() => MecomAPI.catalogue(), []);
  const device = MecomAPI.devices().find((d) => d.id === deviceId);
  const allChannels = MecomAPI.channels();
  const channels = allChannels.filter((c) => c.device_id === deviceId);
  const [activeChannelInst, setActiveChannelInst] = useState(() => channels[0]?.instance || 1);
  const activeChannel = channels.find((c) => c.instance === activeChannelInst) || channels[0];

  const settings = MecomAPI.settings();
  const leases = MecomAPI.leases();
  const lease = leases.find((l) => l.device_id === deviceId);
  const youHold = lease && lease.holder === settings.holder;
  const assigns = useAssignments();

  // Default pins per channel role
  const deviceWallId = wallForDevice(deviceId).wall_id + "-" + (activeChannel?.instance || 1);
  const pins = assigns.forWall(deviceWallId);
  const graphTile = useMemo(() => buildGraphTileFromAssignments(device && activeChannel ? pins : [], {
    tile_id: deviceWallId,
    title: `${device ? device.label : deviceId} · channel ${activeChannel ? activeChannel.instance : activeChannelInst}`,
    colorByChannel: false,
    timeWindowMs: 90_000,
  }), [assigns.list, deviceWallId, device?.label, deviceId, activeChannel?.instance, activeChannelInst]);
  const series = useMemo(() => renderSeriesFromGraphTile(graphTile), [graphTile]);

  // Adapter pin shape for DiscoveryTree. Graph assignments stay the source of truth.
  const pinShape = useMemo(() => pins.map((p) => {
    const a = normalizeAssignment(p);
    const paramId = a.options.param_id;
    return { id: paramId, color: MecomAPI.colorForRole(MecomAPI.roleForParam(paramId)) };
  }), [assigns.list, deviceWallId]);

  // Seed default pins if empty for this channel
  useEffect(() => {
    if (!activeChannel) return;
    const cur = assigns.forWall(deviceWallId);
    if (cur.length > 0) return;
    const defaults = activeChannel.role === "temp"
      ? [3000, 1000, 1001]   // target, object, sink
      : [1021, 1020, 1022];  // output voltage, output current, output power
    defaults.forEach((pid) => assigns.add(deviceWallId, pid, deviceId, activeChannel.instance));
  }, [deviceWallId, activeChannel?.instance, activeChannel?.role]);

  // Cards persistence (per channel)
  const cardsKey = "mecomgw.cards." + deviceId + "." + activeChannelInst;
  const [cards, setCards] = useState(() => {
    try { return JSON.parse(localStorage.getItem(cardsKey)) || []; }
    catch (_) { return []; }
  });
  useEffect(() => { localStorage.setItem(cardsKey, JSON.stringify(cards)); }, [cardsKey, cards]);
  useEffect(() => {
    // load cards for the new channel
    try {
      const k = "mecomgw.cards." + deviceId + "." + activeChannelInst;
      setCards(JSON.parse(localStorage.getItem(k)) || []);
    } catch (_) { setCards([]); }
  }, [deviceId, activeChannelInst]);

  const [query, setQuery] = useState("");
  const [filterCat, setFilterCat] = useState("");
  const [onlyWritable, setOnlyWritable] = useState(false);
  const toast = useToast();

  if (!device) return <div style={{ padding: 32, color: "var(--muted)" }}>Device {deviceId} not found.</div>;
  if (!activeChannel) return <div style={{ padding: 32, color: "var(--muted)" }}>No channels active for {deviceId}.</div>;

  // Catalogue applicable to current channel's role
  const applicableCatalogue = catalogue.filter((p) => !p.applicableModes || p.applicableModes.includes(activeChannel.role));

  function togglePin(param) {
    if (assigns.hasAssignment(deviceWallId, param.id, deviceId, activeChannel.instance)) {
      assigns.remove(deviceWallId, param.id, deviceId, activeChannel.instance);
    } else {
      assigns.add(deviceWallId, param.id, deviceId, activeChannel.instance);
    }
  }
  function openCard(param) {
    setCards((cur) => cur.find((c) => c.id === param.id) ? cur : cur.concat({ id: param.id }));
  }
  function closeCard(id) { setCards((cur) => cur.filter((c) => c.id !== id)); }

  async function quickAcquire() {
    try {
      await MecomAPI.acquireLease(deviceId, settings.holder, "5m");
      toast.push({ kind: "ok", title: "Lease acquired", body: deviceId });
    } catch (err) {
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    }
  }
  async function quickRelease() {
    if (!lease) return;
    await MecomAPI.releaseLease(deviceId, lease.token);
    toast.push({ kind: "ok", title: "Lease released", body: deviceId });
  }

  const bs = MecomAPI.brokerStats(deviceId);

  return (
    <div className="ws">
      <DiscoveryTree
        deviceId={deviceId}
        instance={activeChannel.instance}
        catalogue={applicableCatalogue}
        pins={pinShape}
        onTogglePin={(p) => togglePin(p)}
        onWrite={(p) => openCard(p)}
        onlyWritable={onlyWritable}
        query={query}
        setQuery={setQuery}
        filterCat={filterCat}
        setFilterCat={setFilterCat}
      />
      <div className="canvas">
        <div className="ws-head">
          <h2>{device.label} <span className="id">{device.id} · {device.endpoint} · addr {device.address}</span></h2>
          <div className="right">
            {/* Channel selector */}
            {channels.length > 1 && (
              <div className="role-toggle" style={{ height: 28 }}>
                {channels.map((c) => (
                  <button key={c.instance}
                          className={(c.role === "temp" ? "temp " : "supply ") + (c.instance === activeChannelInst ? "on" : "")}
                          style={{ padding: "0 12px" }}
                          onClick={() => setActiveChannelInst(c.instance)}>
                    ch{c.instance} · {c.role}
                  </button>
                ))}
              </div>
            )}
            {channels.length === 1 && (
              <Chip kind={activeChannel.role === "supply" ? "warn" : "accent"}>ch{activeChannel.instance} · {activeChannel.role === "supply" ? "supply" : "temp ctrl"}</Chip>
            )}
            <button className="btn sm" onClick={() => setOnlyWritable((v) => !v)}>{onlyWritable ? "All params" : "Writable only"}</button>
            <button className="btn sm" onClick={onOpenSequencer}>Run sequence ▸</button>
            {lease ? (
              <>
                <Pill kind={youHold ? "info" : "warn"}>{youHold ? "lease · you" : "lease · " + lease.holder}</Pill>
                {youHold && <button className="btn sm" onClick={quickRelease}>Release</button>}
              </>
            ) : (
              <>
                <Pill kind="ok">lease free</Pill>
                <button className="btn sm primary" onClick={quickAcquire}>Acquire lease</button>
              </>
            )}
          </div>
        </div>

        <div style={{ display: "flex", gap: 12, flexWrap: "wrap", fontFamily: "var(--font-mono)", fontSize: 11.5, color: "var(--muted)" }}>
          <span>connected <b style={{ color: bs.connected ? "var(--ok)" : "var(--bad)" }}>{String(bs.connected)}</b></span>
          <span>frames in <b style={{ color: "var(--text)" }}>{bs.frames_in.toLocaleString()}</b></span>
          <span>frames out <b style={{ color: "var(--text)" }}>{bs.frames_out.toLocaleString()}</b></span>
          <span>errors <b style={{ color: bs.error_count ? "var(--warn)" : "var(--text)" }}>{bs.error_count}</b></span>
          <span>last connect <b style={{ color: "var(--text)" }}>{bs.last_connect_at ? new Date(bs.last_connect_at).toLocaleTimeString() : "—"}</b></span>
        </div>

        <div className="chart-card">
          <div className="head">
            <div className="title">
              {activeChannel.role === "temp" ? "Object T · Target T · Sink T" : "Output voltage · Set voltage · Output current"}
            </div>
            <div className="legend">
              {series.map((s) => {
                const last = s.history.v[s.history.v.length - 1];
                return (
                  <span key={s.key} className="item" onClick={() => assigns.remove(deviceWallId, s.paramId, s.deviceId, s.instance)} title="Click to unpin">
                    <span className="sw" style={{ background: s.color }}></span>
                    {(MecomAPI.catalogue().find((c) => c.id === s.paramId) || {}).name || ("#" + s.paramId)}
                    <span className="cur">{MecomAPI.formatValue(last, s.unit, s.paramId)}{s.unit ? " " + s.unit : ""}</span>
                  </span>
                );
              })}
              {series.length === 0 && <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--muted)" }}>Pin parameters from the tree →</span>}
            </div>
            <div className="right">
              <Chip>90s</Chip>
              <Chip>500ms</Chip>
            </div>
          </div>
          <div className="body">
            {series.length === 0
              ? <div className="empty">Pin parameters from the discovery tree on the left.<br />Click a parameter to add it; star highlights pinned signals.</div>
              : <MultiChart tile={graphTile} height={360} />}
          </div>
        </div>

        {cards.length > 0 && (
          <div className="card-grid">
            {cards.map((c) => {
              const param = catalogue.find((p) => p.id === c.id);
              if (!param) return null;
              return (
                <InputCard key={c.id}
                           deviceId={deviceId}
                           param={{ ...param, instance: activeChannel.instance }}
                           leaseHolder={lease ? lease.holder : null}
                           holderId={settings.holder}
                           onClose={() => closeCard(c.id)} />
              );
            })}
          </div>
        )}

        {/* Quick-action ribbon: writable params not in cards */}
        <Panel title="Writable parameters · quick add" right={<Chip>{applicableCatalogue.filter((p) => p.writable).length}</Chip>}>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
            {applicableCatalogue.filter((p) => p.writable).map((p) => (
              <button key={p.id} className="btn sm" disabled={cards.find((c) => c.id === p.id)} onClick={() => openCard(p)}>
                + {p.name} <span style={{ color: "var(--muted)" }}>·{p.id}</span>
              </button>
            ))}
          </div>
        </Panel>

        {/* Telemetry / Telecommand accordions — scoped to this channel */}
        <SignalsAccordion role="monitor"
                          channels={[activeChannel]}
                          title="Telemetry"
                          scopeLabel={`${deviceId}/${activeChannel.instance} only`}
                          defaultOpen={false}
                          storageKey={"mecomgw.acc.dev." + deviceId + "." + activeChannel.instance + ".monitor"} />
        <SignalsAccordion role="control"
                          channels={[activeChannel]}
                          title="Telecommands"
                          scopeLabel={`${deviceId}/${activeChannel.instance} only`}
                          defaultOpen={true}
                          storageKey={"mecomgw.acc.dev." + deviceId + "." + activeChannel.instance + ".control"} />
      </div>
    </div>
  );
}

Object.assign(window, { FleetView, DeviceWorkspace, CommandFeed });
