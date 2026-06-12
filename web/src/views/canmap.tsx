// @ts-nocheck
import React, { useEffect, useState, useCallback } from "react";
import { MecomAPI } from "../api/mecom";
import { Chip, useToast } from "../components/atoms";

// CanMapView shows the CAN signal registry — which node produces which value
// under which COB-ID, and which nodes consume it — live-informed by the
// gateway's read-back of the actual device PDO configuration. It also lets an
// operator export the wiring as a reusable pattern and import a pattern (with
// fresh node bindings) when standing up a copy of a testbed.

function verdictChip(verdict) {
  const tone = verdict === "drift" ? "warn" : verdict === "match" ? "ok" : "";
  const label = verdict === "drift" ? "drift" : verdict === "match" ? "in sync" : "unknown";
  return <b className={"canmap-verdict " + tone}>{label}</b>;
}

function hex(v) {
  if (typeof v === "string") return v;
  if (typeof v === "number") return "0x" + v.toString(16).toUpperCase();
  return String(v ?? "");
}

function mappingText(mapping) {
  if (!mapping || !mapping.length) return "—";
  return mapping.map((m) => `${hex(m.index)}:${String(m.subindex).padStart(2, "0")}/${m.bits}b`).join("  ");
}

export function CanMapView() {
  const toast = useToast();
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [showImport, setShowImport] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setErr("");
    try {
      setData(await MecomAPI.canmap(true));
    } catch (e) {
      setErr(e?.message || String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const registry = data?.registry || null;
  const statusBySignal = {};
  (data?.status || []).forEach((s) => { statusBySignal[s.signal] = s; });
  const observed = data?.observed || {};

  const roleNode = {};
  (registry?.nodes || []).forEach((n) => { roleNode[n.role] = n; });
  const nodeOf = (role) => roleNode[role] || { role };

  const drift = (data?.status || []).filter((s) => s.verdict === "drift").length;
  const unknown = (data?.status || []).filter((s) => s.verdict === "unknown").length;

  return (
    <div className="canmap-view" style={{ height: "100%", display: "flex", flexDirection: "column", overflow: "hidden" }}>
      <header className="canmap-head">
        <div>
          <h2>CAN signal map {registry?.name ? <Chip>{registry.name}</Chip> : null}</h2>
          <p className="muted">
            Documented PDO contracts between RMM producers and TEC consumers, checked live against the connected controllers.
          </p>
        </div>
        <div className="canmap-actions">
          <button onClick={load} disabled={loading}>{loading ? "Reading…" : "Refresh live"}</button>
          <a className="btn" href={MecomAPI.canmapExportURL("registry")} download>Export registry</a>
          <a className="btn" href={MecomAPI.canmapExportURL("pattern")} download>Export pattern</a>
          <button onClick={() => setShowImport((v) => !v)}>{showImport ? "Close import" : "Import…"}</button>
        </div>
      </header>

      {registry && (
        <div className="canmap-summary">
          <span>{registry.signals?.length || 0} signals</span>
          <span className={drift ? "warn" : ""}>{drift} drift</span>
          <span>{unknown} unknown</span>
          {data?.observed_at && <span className="muted">read {new Date(data.observed_at).toLocaleTimeString()}</span>}
          {registry.is_pattern && <Chip>pattern (no node bindings)</Chip>}
        </div>
      )}

      {err && <div className="canmap-error">Could not load registry: {err}</div>}
      {!loading && !registry && !err && (
        <div className="canmap-empty">
          <p>No CAN signal registry is loaded.</p>
          <p className="muted">
            Start the gateway with <code>-canmap path/to/registry.json</code>, or import a pattern below to create one.
          </p>
          <button onClick={() => setShowImport(true)}>Import a registry or pattern</button>
        </div>
      )}

      {showImport && <ImportPanel onDone={() => { setShowImport(false); load(); }} toast={toast} />}

      <div className="canmap-signals" style={{ overflow: "auto" }}>
        {(registry?.signals || []).map((sig) => {
          const st = statusBySignal[sig.name];
          return (
            <section key={sig.name} className="canmap-signal">
              <div className="canmap-signal-head">
                <div>
                  <h3>{sig.name}</h3>
                  <span className="canmap-cob">COB-ID {hex(sig.cob_id)}{sig.rate_ms ? ` · ${sig.rate_ms} ms` : ""}</span>
                </div>
                {st ? verdictChip(st.verdict) : null}
              </div>
              {sig.description && <p className="muted">{sig.description}</p>}

              <table className="canmap-table">
                <thead>
                  <tr><th>Role</th><th>Node</th><th>Dir</th><th>PDO</th><th>Mapping</th><th>State</th></tr>
                </thead>
                <tbody>
                  <Row
                    role={sig.producer.role} node={nodeOf(sig.producer.role)} dir="TPDO"
                    pdo={sig.producer.tpdo} mapping={sig.producer.mapping}
                    findings={findingsFor(st, sig.producer.role)} observed={observed} />
                  {(sig.consumers || []).map((c) => (
                    <Row
                      key={c.role} role={c.role} node={nodeOf(c.role)} dir="RPDO"
                      pdo={c.rpdo} mapping={c.mapping} sourceSelects={c.source_selects}
                      findings={findingsFor(st, c.role)} observed={observed} />
                  ))}
                </tbody>
              </table>

              {sig.verified && <div className="canmap-verified muted">last verified {sig.verified}{sig.saved_to_flash ? " · saved to flash" : ""}</div>}
            </section>
          );
        })}
      </div>
    </div>
  );
}

function findingsFor(status, role) {
  if (!status) return [];
  return (status.findings || []).filter((f) => f.role === role);
}

function Row({ role, node, dir, pdo, mapping, sourceSelects, findings, observed }) {
  const drifts = findings.filter((f) => f.verdict === "drift");
  const unknown = findings.length > 0 && findings.every((f) => f.verdict === "unknown");
  const rowClass = drifts.length ? "drift" : unknown ? "unknown" : findings.length ? "match" : "";
  return (
    <>
      <tr className={"canmap-row " + rowClass}>
        <td>{role}</td>
        <td>{node.node_id ? hex(node.node_id) : <span className="muted">unbound</span>}{node.label ? <small> {node.label}</small> : null}</td>
        <td>{dir}</td>
        <td>{pdo}</td>
        <td className="canmap-mapping">
          {mappingText(mapping)}
          {sourceSelects && sourceSelects.length
            ? <div className="muted">select {sourceSelects.map((w) => `${hex(w.index)}:${String(w.subindex).padStart(2, "0")}=${w.value}`).join(", ")}</div>
            : null}
        </td>
        <td>{drifts.length ? <b className="warn">{drifts.length} drift</b> : unknown ? <span className="muted">unknown</span> : findings.length ? <b className="ok">ok</b> : <span className="muted">—</span>}</td>
      </tr>
      {drifts.map((f, i) => (
        <tr key={i} className="canmap-finding">
          <td colSpan={6}><span className="warn">{f.aspect}</span>: want <code>{f.want}</code>, got <code>{f.got}</code></td>
        </tr>
      ))}
    </>
  );
}

// ImportPanel accepts a registry or pattern JSON file. For a pattern it asks
// for role→node bindings before handing it to the gateway.
function ImportPanel({ onDone, toast }) {
  const [text, setText] = useState("");
  const [parsed, setParsed] = useState(null);
  const [parseErr, setParseErr] = useState("");
  const [name, setName] = useState("");
  const [bindings, setBindings] = useState({});
  const [busy, setBusy] = useState(false);

  function ingest(raw) {
    setText(raw);
    setParseErr("");
    try {
      const obj = JSON.parse(raw);
      setParsed(obj);
      setName(obj.name || "");
      const isPattern = (obj.nodes || []).every((n) => !n.node_id);
      if (isPattern) {
        const b = {};
        (obj.nodes || []).forEach((n) => { b[n.role] = ""; });
        setBindings(b);
      } else {
        setBindings({});
      }
    } catch (e) {
      setParsed(null);
      setParseErr(e?.message || "invalid JSON");
    }
  }

  async function onFile(ev) {
    const file = ev.target.files?.[0];
    if (file) ingest(await file.text());
  }

  const isPattern = parsed && (parsed.nodes || []).every((n) => !n.node_id);

  async function submit() {
    setBusy(true);
    try {
      let body;
      if (isPattern) {
        const list = Object.entries(bindings)
          .filter(([, v]) => String(v).trim() !== "")
          .map(([role, v]) => ({ role, node_id: parseNode(v) }));
        body = { pattern: parsed, name: name || parsed.name, bindings: list };
      } else {
        body = { registry: parsed };
      }
      await MecomAPI.canmapImport(body);
      toast?.push?.({ kind: "ok", title: "CAN signal map imported", body: name || parsed.name });
      onDone();
    } catch (e) {
      toast?.push?.({ kind: "error", title: "Import failed", body: e?.message || String(e) });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="canmap-import">
      <h3>Import registry or pattern</h3>
      <p className="muted">
        A pattern carries the wiring without node IDs — bind each role to a controller on this bench to instantiate it.
        Importing never writes to devices; refresh afterward to see what still needs to be configured.
      </p>
      <input type="file" accept="application/json,.json" onChange={onFile} />
      <textarea
        placeholder="…or paste registry/pattern JSON here"
        value={text}
        onChange={(e) => ingest(e.target.value)}
        rows={5}
      />
      {parseErr && <div className="canmap-error">{parseErr}</div>}
      {parsed && (
        <div className="canmap-bindings">
          <label>Name <input value={name} onChange={(e) => setName(e.target.value)} placeholder="bench-b" /></label>
          {isPattern ? (
            <>
              <div className="muted">Bind each role to a CANopen node ID (1–127, decimal or 0x):</div>
              {Object.keys(bindings).map((role) => (
                <label key={role}>
                  {role}
                  <input
                    value={bindings[role]}
                    onChange={(e) => setBindings((b) => ({ ...b, [role]: e.target.value }))}
                    placeholder="0x4b" />
                </label>
              ))}
            </>
          ) : (
            <div className="muted">Concrete registry — node bindings already present.</div>
          )}
          <button onClick={submit} disabled={busy}>{busy ? "Importing…" : "Import"}</button>
        </div>
      )}
    </div>
  );
}

function parseNode(v) {
  const s = String(v).trim().toLowerCase();
  return s.startsWith("0x") ? parseInt(s, 16) : parseInt(s, 10);
}
