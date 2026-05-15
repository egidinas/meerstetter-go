/* ========================================================
   Meerstetter Gateway — root app
   ======================================================== */
/* global React, ReactDOM, MecomAPI,
   ToastProvider, FleetView, DeviceWorkspace, SequencerView,
   PIDAdvisor, ArchiveView, SettingsView, Pill, Chip,
   TweaksPanel, useTweaks, TweakSection, TweakRadio, TweakToggle, TweakColor,
   useGatewayTick */

const { useState, useEffect, useRef, useMemo, useCallback } = React;

const TWEAK_DEFAULTS = /*EDITMODE-BEGIN*/{
  "scenario": "mixed",
  "showSequencer": true,
  "showArchive": true,
  "showCanRing": false,
  "density": "comfortable",
  "accent": "#58a6ff"
}/*EDITMODE-END*/;

function useHashRoute() {
  const [hash, setHash] = useState(() => window.location.hash.slice(1) || "/fleet");
  useEffect(() => {
    const fn = () => setHash(window.location.hash.slice(1) || "/fleet");
    window.addEventListener("hashchange", fn);
    return () => window.removeEventListener("hashchange", fn);
  }, []);
  return [hash, (h) => { window.location.hash = h; }];
}

function App() {
  const [route, go] = useHashRoute();
  const [t, setT] = useTweaks(TWEAK_DEFAULTS);
  useGatewayTick();

  // Apply density to root
  useEffect(() => {
    document.documentElement.dataset.density = t.density === "compact" ? "compact" : "comfortable";
  }, [t.density]);
  // Apply accent
  useEffect(() => {
    document.documentElement.style.setProperty("--accent", t.accent);
    document.documentElement.style.setProperty("--accent-soft",
      `color-mix(in srgb, ${t.accent} 18%, transparent)`);
  }, [t.accent]);
  // Apply scenario
  useEffect(() => {
    MecomAPI.setScenario(t.scenario);
    MecomAPI.saveSettings({ scenario: t.scenario });
  }, [t.scenario]);

  // Parse route
  let view = "fleet", arg = null;
  if (route.startsWith("/device/")) { view = "device"; arg = route.slice("/device/".length); }
  else if (route === "/dictionary") view = "dictionary";
  else if (route === "/sequencer") view = "sequencer";
  else if (route === "/pid") view = "pid";
  else if (route === "/archive") view = "archive";
  else if (route === "/settings") view = "settings";

  const settings = MecomAPI.settings();
  const isLive = MecomAPI.isLive && MecomAPI.isLive();
  const liveError = MecomAPI.liveError && MecomAPI.liveError();
  const devices = MecomAPI.devices();
  const leaseCount = MecomAPI.leases().length;

  return (
    <ToastProvider>
      <div className="app">
        <header className="topbar">
          <div className="brand">
            <span className="mark">M</span>
            <span>Meerstetter</span>
            <span className="sub">mecomgw · v0.1</span>
          </div>
          <div className="crumbs">
            <span>workspace</span>
            <span className="sep">/</span>
            <span className="cur">{
              view === "fleet" ? "Fleet" :
              view === "device" ? `Device · ${arg}` :
              view === "dictionary" ? "Signal Dictionary" :
              view === "sequencer" ? "Sequencer" :
              view === "pid" ? "PID Advisor" :
              view === "archive" ? "Archive" :
              "Settings"
            }</span>
          </div>
          <div className="spacer"></div>
          <div className="right">
            <Pill kind={isLive ? "ok" : liveError ? "warn" : "accent"}>
              gateway · {isLive ? "live" : liveError ? "fallback" : "checking"}
            </Pill>
            <Chip kind="accent">holder · {settings.holder}</Chip>
            <Chip>{isLive ? "same-origin /api" : settings.gateway ? "explicit offline" : "mock fallback"}</Chip>
          </div>
        </header>

        <aside className="rail">
          <div className="nav-section">Operate</div>
          <div className="nav">
            <a className={view === "fleet" ? "active" : ""} href="#/fleet">
              <span className="icon">⌗</span><span className="label">Fleet</span>
              <span className="count">{devices.length}</span>
            </a>
            <a className={view === "dictionary" ? "active" : ""} href="#/dictionary">
              <span className="icon">☰</span><span className="label">Signal dictionary</span>
            </a>
            {devices.map((d) => (
              <a key={d.id}
                 className={view === "device" && arg === d.id ? "active" : ""}
                 href={`#/device/${d.id}`}
                 style={{ paddingLeft: 26, fontFamily: "var(--font-mono)", fontSize: 12 }}>
                <span style={{
                  width: 8, height: 8, borderRadius: "50%",
                  background: d.bound ? "var(--ok)" : "var(--bad)",
                  marginRight: 2,
                }}></span>
                <span className="label">{d.id}</span>
              </a>
            ))}
          </div>
          {(t.showSequencer || t.showArchive || t.showCanRing) && <div className="nav-section">Advanced</div>}
          <div className="nav">
            {t.showSequencer && (
              <a className={view === "sequencer" ? "active" : ""} href="#/sequencer">
                <span className="icon">⫶</span><span className="label">Sequencer</span>
              </a>
            )}
            <a className={view === "pid" ? "active" : ""} href="#/pid">
              <span className="icon">∿</span><span className="label">PID advisor</span>
            </a>
            {t.showArchive && (
              <a className={view === "archive" ? "active" : ""} href="#/archive">
                <span className="icon">⤓</span><span className="label">Archive</span>
              </a>
            )}
            {t.showCanRing && (
              <a className="" href="#/can-ring" onClick={(e) => { e.preventDefault(); alert("CAN ring viewer · next wave"); }}>
                <span className="icon">◎</span><span className="label">CAN ring</span>
                <span className="count">soon</span>
              </a>
            )}
          </div>
          <div className="nav-section">System</div>
          <div className="nav">
            <a className={view === "settings" ? "active" : ""} href="#/settings">
              <span className="icon">⚙</span><span className="label">Settings</span>
            </a>
          </div>
          <div className="footer">
            <span>leases · {leaseCount}</span>
            <span>scenario · {t.scenario}</span>
          </div>
        </aside>

        <main className="main">
          {view === "fleet"     && <FleetView onOpenDevice={(id) => go(`/device/${id}`)} />}
          {view === "device"    && <DeviceWorkspace deviceId={arg} onOpenSequencer={() => go("/sequencer")} />}
          {view === "dictionary" && <SignalDictionaryView />}
          {view === "sequencer" && <SequencerView />}
          {view === "pid"       && <PIDAdvisor deviceId={arg || devices[0]?.id || "tec-75"} onDeviceChange={(id) => go(`/pid${id ? "" : ""}`)} />}
          {view === "archive"   && <ArchiveView />}
          {view === "settings"  && <SettingsView />}
        </main>

        <TweaksPanel title="Tweaks">
          <TweakSelect
            label="Mock scenario"
            value={t.scenario}
            options={[
              { value: "healthy", label: "Healthy fleet" },
              { value: "mixed", label: "Mixed (default)" },
              { value: "lease-fight", label: "Lease conflict" },
              { value: "write-reject", label: "Write rejected" },
            ]}
            onChange={(v) => setT("scenario", v)}
          />
          <TweakSection label="Layout" />
          <TweakRadio
            label="Density"
            value={t.density}
            options={["comfortable", "compact"]}
            onChange={(v) => setT("density", v)}
          />
          <TweakColor
            label="Accent"
            value={t.accent}
            options={["#58a6ff", "#3fb950", "#a371f7", "#d29922", "#f47067", "#56d4dd"]}
            onChange={(v) => setT("accent", v)}
          />
          <TweakSection label="Advanced sections" />
          <TweakToggle label="Sequencer" value={t.showSequencer} onChange={(v) => setT("showSequencer", v)} />
          <TweakToggle label="Archive export" value={t.showArchive} onChange={(v) => setT("showArchive", v)} />
          <TweakToggle label="CAN ring (soon)" value={t.showCanRing} onChange={(v) => setT("showCanRing", v)} />
        </TweaksPanel>
      </div>
    </ToastProvider>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<App />);
