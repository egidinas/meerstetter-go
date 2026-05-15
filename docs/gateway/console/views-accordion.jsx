/* ============================================================
   Meerstetter Gateway — bottom accordion (Telemetry + Telecommand)
   The dictionary's two tabs reified as in-context bottom panels:
     - placed beneath hero graphs (Fleet) and per-device chart
       (Device workspace)
     - telecommand inputs pre-populated with the live current value
     - Sends stream into Command Activity, visible above
   ============================================================ */
/* global React, MecomAPI, Pill, Chip, useToast, useLiveValue, useGatewayTick,
   categorizeError, channelColor */

const { useState, useEffect, useRef, useMemo, useCallback } = React;

/* ============================================================
   <BottomAccordion> chrome — collapsible panel
   ============================================================ */
function BottomAccordion({ title, sub, right, defaultOpen = true, storageKey, children }) {
  const [open, setOpen] = useState(() => {
    if (!storageKey) return defaultOpen;
    try {
      const raw = localStorage.getItem(storageKey);
      if (raw === null) return defaultOpen;
      return raw === "1";
    } catch (_) { return defaultOpen; }
  });
  useEffect(() => {
    if (storageKey) try { localStorage.setItem(storageKey, open ? "1" : "0"); } catch (_) {}
  }, [storageKey, open]);
  return (
    <div className={"bot-acc " + (open ? "expanded" : "collapsed")}>
      <div className="bot-acc-head" onClick={() => setOpen((o) => !o)}>
        <span className="chev">▸</span>
        <h3>{title} {sub && <span className="sub">· {sub}</span>}</h3>
        <div className="right" onClick={(e) => e.stopPropagation()}>{right}</div>
      </div>
      {open && <div className="bot-acc-body"><div>{children}</div></div>}
    </div>
  );
}

/* ============================================================
   Helpers
   ============================================================ */
function groupSignals(signals) {
  const g = {};
  signals.forEach((s) => {
    if (!g[s.group]) g[s.group] = {};
    if (!g[s.group][s.subgroup]) g[s.group][s.subgroup] = [];
    g[s.group][s.subgroup].push(s);
  });
  return g;
}

function applicableChannels(signal, channels) {
  if (!signal.applicableModes) return channels;
  return channels.filter((c) => signal.applicableModes.includes(c.role));
}

function RoleSourceBadge({ channel }) {
  const confidence = MecomAPI.roleConfidence
    ? MecomAPI.roleConfidence(channel)
    : { kind: "warn", label: "unconfirmed", detail: "Channel role source is not available." };
  return (
    <span className={"role-source " + confidence.kind} title={confidence.detail}>
      {confidence.label}
    </span>
  );
}

/* ============================================================
   Monitor signal block — compact row of channel readouts
   ============================================================ */
function MonitorSignalBlock({ signal, channels }) {
  const cells = applicableChannels(signal, channels);
  return (
    <div className="sig-strip">
      <div className="sig-name">
        <span className="nm">{signal.name}</span>
        <span className="pid">·{signal.id}{signal.unit ? " · " + signal.unit : ""}</span>
        <span className="prov mono">sid={signal.sid} kind={signal.kind}</span>
      </div>
      <div className="channels-row">
        {cells.map((c) => <MonitorCell key={c.device_id + "/" + c.instance} signal={signal} channel={c} />)}
        {cells.length === 0 && (
          <span className="dim mono small" style={{ padding: "4px 8px" }}>No channels apply for this signal.</span>
        )}
      </div>
    </div>
  );
}

function MonitorCell({ signal, channel }) {
  const { value, quality } = useLiveValue(channel.device_id, signal.id, channel.instance);
  const role = MecomAPI.roleForParam(signal.id);
  const colorClass = role === "cmd" ? "cmd" : role === "actual" ? "actual" : role === "dut" ? "dut" : role === "aux" ? "aux" : "";
  const unreach = quality === "unreachable";
  const missing = quality === "missing";
  return (
    <div className={"ch-cell " + (unreach ? "unreach" : "") + " " + (missing ? "dim" : "")}
         title={MecomAPI.provenance(channel.device_id, signal.id, channel.instance)}>
      <span className="swatch" style={{ background: channelColor(channel.device_id, channel.instance) }}></span>
      <span className="id">{channel.device_id}<span style={{opacity:0.6}}>/{channel.instance}</span></span>
      <RoleSourceBadge channel={channel} />
      <span className={"v " + colorClass}>
        {MecomAPI.formatValue(value, signal.unit, signal.id)}
        {signal.unit && <span className="u">{signal.unit === "degC" ? "°C" : " " + signal.unit}</span>}
      </span>
    </div>
  );
}

/* ============================================================
   Control signal block — per-channel pre-populated input cards
   ============================================================ */
function ControlSignalBlock({ signal, channels, holderId }) {
  const cells = applicableChannels(signal, channels);
  return (
    <div className="cmd-block">
      <div className="cmd-head">
        <span className="nm">{signal.name}</span>
        <span className="pid">·{signal.id} · {MecomAPI.commandNameFor(signal)}{signal.unit ? " · " + signal.unit : ""}</span>
        {signal.dangerous && <Chip kind="warn">requires confirm</Chip>}
        <span className="prov">sid={signal.sid}</span>
      </div>
      <div className="cmd-grid">
        {cells.map((c) => <ControlCell key={c.device_id + "/" + c.instance}
                                        signal={signal} channel={c} holderId={holderId} />)}
        {cells.length === 0 && (
          <span className="dim mono small" style={{ padding: "4px 8px" }}>No channels apply.</span>
        )}
      </div>
    </div>
  );
}

function ControlCell({ signal, channel, holderId }) {
  const { value: cur, quality } = useLiveValue(channel.device_id, signal.id, channel.instance);
  const [staged, setStaged] = useState("");
  const [confirmText, setConfirmText] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  const leases = MecomAPI.leases();
  const lease = leases.find((l) => l.device_id === channel.device_id);
  const youHold = lease && lease.holder === holderId;
  const someoneElse = lease && !youHold;
  const unreach = quality === "unreachable";
  const isEnum = signal.kind === "enum";
  const isTrigger = signal.cmd === "save_to_flash";

  // Pre-populate: input shows current value formatted unless user has typed
  const curStr = MecomAPI.formatValue(cur, signal.unit, signal.id);
  const displayed = staged === "" ? (cur !== null && cur !== undefined && !isEnum ? formatForInput(cur, signal) : "") : staged;
  const changed = staged !== "" && parseFloat(staged) !== cur;
  const stagedNum = parseFloat(staged);
  const isValid = isEnum || isTrigger
    ? true
    : (!Number.isNaN(stagedNum)
       && (signal.min === undefined || stagedNum >= signal.min)
       && (signal.max === undefined || stagedNum <= signal.max));
  const needsConfirm = signal.dangerous && (changed || isEnum || isTrigger);
  const confirmOK = !needsConfirm || confirmText.trim().toUpperCase() === "WRITE";

  async function commit(overrideValue) {
    setBusy(true);
    try {
      let token;
      if (!youHold) {
        const l = await MecomAPI.acquireLease(channel.device_id, holderId, "5m");
        token = l.token;
        toast.push({ kind: "ok", title: "Lease acquired", body: channel.device_id });
      } else {
        token = lease.token;
      }
      const v = overrideValue !== undefined ? overrideValue
                : (isEnum ? parseInt(staged, 10) : stagedNum);
      const cmdName = MecomAPI.commandNameFor(signal);
      await MecomAPI.write(channel.device_id, {
        name: cmdName,
        arguments: { param: signal.id, instance: channel.instance, value: v },
      }, token);
      toast.push({
        kind: "ok",
        title: `${signal.name} → ${signal.unit ? v + " " + signal.unit : v}`,
        body: `${channel.device_id}/${channel.instance}`,
      });
      setStaged("");
      setConfirmText("");
    } catch (err) {
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={[
      "cmd-cell",
      changed || (isEnum && staged !== "") ? "staged" : "",
      signal.dangerous ? "danger" : "",
      unreach ? "unreach" : "",
    ].join(" ")} title={MecomAPI.provenance(channel.device_id, signal.id, channel.instance)}>
      <div className="head">
        <span className="swatch" style={{ display: "inline-block", width: 8, height: 8, borderRadius: 2, background: channelColor(channel.device_id, channel.instance) }}></span>
        <span className="ch-id">{channel.device_id}/{channel.instance}</span>
        <span className="ch-role">{channel.role}</span>
        <RoleSourceBadge channel={channel} />
        {lease ? (
          <span className={"lease-mini " + (youHold ? "you" : "other")}>{youHold ? "lease·you" : "lease·" + lease.holder}</span>
        ) : <span className="lease-mini">lease·free</span>}
      </div>
      <div className="cur">
        <span>current</span>
        <b>{curStr}{signal.unit && (signal.unit === "degC" ? " °C" : " " + signal.unit)}</b>
      </div>

      {isTrigger ? (
        <button className="send" disabled={unreach || busy} onClick={() => commit(1)}>
          {busy ? "…" : `Trigger ${signal.cmd}`}
        </button>
      ) : isEnum ? (
        <>
          <div className="enum-row">
            {Object.entries(signal.enum || {}).map(([k, label]) => {
              const kNum = parseInt(k, 10);
              const isCur = kNum === cur;
              const isStaged = staged !== "" && parseInt(staged, 10) === kNum;
              return (
                <button key={k}
                        className={isStaged ? "staged" : (isCur ? "cur" : "")}
                        disabled={unreach}
                        onClick={() => isCur ? setStaged("") : setStaged(String(kNum))}
                        title={label}>
                  {label}
                </button>
              );
            })}
          </div>
          {needsConfirm && (
            <div className="confirm-strip">
              <span>type WRITE to commit</span>
              <input value={confirmText} onChange={(e) => setConfirmText(e.target.value)} placeholder="WRITE" />
            </div>
          )}
          <div className="stage-row">
            <span className="range-hint">enum · {Object.keys(signal.enum || {}).length} states</span>
            <button className="send" disabled={!isValid || !confirmOK || unreach || busy || staged === ""}
                    onClick={() => commit()}>
              {busy ? "…" : "Send"}
            </button>
          </div>
        </>
      ) : (
        <>
          <div className="stage-row">
            <input className="prefill" value={displayed}
                   disabled={unreach}
                   onChange={(e) => setStaged(e.target.value)}
                   placeholder={curStr} />
            {changed && (
              <button className="reset-btn" title="Discard change" onClick={() => setStaged("")}>↺</button>
            )}
          </div>
          <div className="stage-row">
            <span className="range-hint">
              {signal.min !== undefined || signal.max !== undefined
                ? `${signal.min ?? "—"} … ${signal.max ?? "—"}`
                : "no range"}
            </span>
            <button className="send" disabled={!isValid || !changed || !confirmOK || unreach || busy}
                    onClick={() => commit()}>
              {busy ? "…" : "Send"}
            </button>
          </div>
          {needsConfirm && (
            <div className="confirm-strip">
              <span>type WRITE to commit</span>
              <input value={confirmText} onChange={(e) => setConfirmText(e.target.value)} placeholder="WRITE" />
            </div>
          )}
        </>
      )}
    </div>
  );
}

function formatForInput(v, signal) {
  if (typeof v !== "number") return "";
  // Match number formatting but without unit characters for raw editing.
  const unit = signal.unit;
  let digits = 2;
  if (unit === "degC" || unit === "V") digits = 3;
  else if (unit === "A") digits = 3;
  else if (unit === "W") digits = 2;
  else if (unit === "s") digits = Math.abs(v) > 1000 ? 0 : 2;
  return v.toFixed(digits);
}

/* ============================================================
   SignalsAccordion — entry point used by Fleet & Device views.
   Renders a single accordion that lists either Telemetry (monitor)
   or Telecommands (control) signals applicable to the channel set,
   grouped by signalforge group/subgroup.
   ============================================================ */
function SignalsAccordion({ role, channels, title, sub, defaultOpen, storageKey, scopeLabel }) {
  useGatewayTick();
  const catalogue = useMemo(() => MecomAPI.catalogue(), []);
  const settings = MecomAPI.settings();
  // Filter signals: by role, and to signals at least one channel applies to.
  const signals = useMemo(() => {
    const channelRoles = new Set(channels.map((c) => c.role));
    return catalogue.filter((p) => {
      if (p.role !== role) return false;
      if (!p.applicableModes) return true;
      return p.applicableModes.some((m) => channelRoles.has(m));
    });
  }, [catalogue, role, channels.map((c) => c.role).join("")]);

  const grouped = useMemo(() => groupSignals(signals), [signals]);

  // For the header summary
  const writable = signals.filter((s) => s.writable).length;
  const leaseCount = MecomAPI.leases().length;
  const summary = role === "monitor"
    ? `${signals.length} signals · ${channels.length} channel${channels.length === 1 ? "" : "s"}`
    : `${writable} writable · ${leaseCount} lease${leaseCount === 1 ? "" : "s"} active · ${channels.length} channel${channels.length === 1 ? "" : "s"}`;

  return (
    <BottomAccordion
      title={title}
      sub={sub || summary}
      defaultOpen={defaultOpen}
      storageKey={storageKey}
      right={
        <>
          <Chip>{scopeLabel || "all channels"}</Chip>
          {role === "control" && <Chip kind="accent">holder · {settings.holder}</Chip>}
        </>
      }
    >
      {Object.entries(grouped).map(([group, sgrps]) => (
        <div key={group} className="bot-acc-section">
          <div className="grp-h">
            <span>{group}</span>
            <span className="line"></span>
            <span>{Object.values(sgrps).reduce((a, b) => a + b.length, 0)} signals</span>
          </div>
          {Object.entries(sgrps).map(([sgrp, items]) => (
            <div key={sgrp} style={{ marginBottom: 8 }}>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--muted)", textTransform: "uppercase", letterSpacing: "0.06em", marginBottom: 4 }}>
                {sgrp}
              </div>
              {items.map((s) => role === "monitor"
                ? <MonitorSignalBlock key={s.id} signal={s} channels={applicableChannels(s, channels)} />
                : <ControlSignalBlock key={s.id} signal={s} channels={applicableChannels(s, channels)} holderId={settings.holder} />
              )}
            </div>
          ))}
        </div>
      ))}
      {signals.length === 0 && (
        <div style={{ padding: 24, textAlign: "center", color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
          No {role === "monitor" ? "telemetry signals" : "telecommands"} apply for the current channel set.
        </div>
      )}
    </BottomAccordion>
  );
}

Object.assign(window, {
  BottomAccordion, SignalsAccordion,
  MonitorSignalBlock, ControlSignalBlock,
});
