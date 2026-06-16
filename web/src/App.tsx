// @ts-nocheck
import React, { useState, useEffect } from "react";
import { MecomAPI } from "./api/mecom";
import { Chip, ToastProvider, useGatewayTick } from "./components/atoms";
import { TweaksPanel, useTweaks, TweakSection, TweakRadio, TweakToggle, TweakColor, TweakSelect } from "./components/tweaks";
import { seedAssignments } from "./views/assignments";
import { FleetView, DeviceWorkspace, DeviceMini, CommandFeed, LeaseSummary } from "./views/main";
import { SignalDictionaryView } from "./views/dict";
import { CanMapView } from "./views/canmap";
import { SequencerView, PIDAdvisor, ArchiveView, SettingsView } from "./views/extra";
import { HelpView } from "./views/help";

const TWEAK_DEFAULTS = {
  scenario: "mixed",
  showSequencer: true,
  showArchive: true,
  showCanRing: false,
  density: "comfortable",
  accent: "#58a6ff",
  fixtureLabels: true,
};

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
  const [drawerOpen, setDrawerOpen] = useState(() => localStorage.getItem("mecomgw.cmdDrawer") !== "closed");
  useGatewayTick();

  useEffect(() => {
    document.documentElement.dataset.density = t.density === "compact" ? "compact" : "comfortable";
  }, [t.density]);

  useEffect(() => {
    document.documentElement.style.setProperty("--accent", t.accent);
    document.documentElement.style.setProperty("--accent-soft", `color-mix(in srgb, ${t.accent} 18%, transparent)`);
  }, [t.accent]);

  useEffect(() => {
    document.documentElement.dataset.fixtureLabels = t.fixtureLabels ? "on" : "off";
  }, [t.fixtureLabels]);

  useEffect(() => {
    localStorage.setItem("mecomgw.cmdDrawer", drawerOpen ? "open" : "closed");
  }, [drawerOpen]);

  useEffect(() => {
    if (MecomAPI.isLive && MecomAPI.isLive()) return;
    MecomAPI.setScenario(t.scenario);
    MecomAPI.saveSettings({ scenario: t.scenario });
  }, [t.scenario]);

  let view = "fleet", arg = null;
  if (route.startsWith("/device/")) { view = "device"; arg = route.slice("/device/".length); }
  else if (route === "/dictionary") view = "dictionary";
  else if (route === "/canmap") view = "canmap";
  else if (route === "/sequencer") view = "sequencer";
  else if (route === "/pid") view = "pid";
  else if (route === "/archive") view = "archive";
  else if (route === "/settings") view = "settings";
  else if (route === "/help") view = "help";

  const settings = MecomAPI.settings();
  const isLive = MecomAPI.isLive && MecomAPI.isLive();
  const liveError = MecomAPI.liveError && MecomAPI.liveError();
  const devices = MecomAPI.devices();
  const channels = MecomAPI.channels();
  const leases = MecomAPI.leases();
  const events = MecomAPI.commandEvents();
  const tempChannels = channels.filter((c) => c.role === "temp");
  const supplyChannels = channels.filter((c) => c.role === "supply");
  const monitorChannels = channels.filter((c) => c.role === "monitor");
  const otherChannels = channels.filter((c) => c.role !== "temp" && c.role !== "supply");
  const familyCounts = devices.reduce((acc, device) => {
    const family = String(device.device_family || device.family || device.device_type || "generic").toLowerCase();
    const key = family.includes("rmm") ? "rmm" : family.includes("ldd") ? "ldd" : family.includes("tec") ? "tec" : "generic";
    acc[key] = (acc[key] || 0) + 1;
    return acc;
  }, {});
  const bound = devices.filter((d) => d.bound).length;
  const errors = devices.filter((d) => d.last_error).length;
  const leasedByMe = leases.filter((l) => l.holder === settings.holder).length;
  const writesLastMinute = events.filter((e) => {
    const t = new Date(e.time).getTime();
    return (Date.now() - t) < 60_000 && (e.status === "completed" || e.status === "accepted");
  }).length;
  const fallbackDeviceId = devices.find((d) => d.id === "tec-4c" || d.id === "tec-76")?.id
    || devices.find((d) => d.device_family === "tec")?.id
    || devices.find((d) => String(d.id || "").startsWith("tec-"))?.id
    || devices[0]?.id
    || "tec-76";

  return (
    <ToastProvider>
      <div className={"app " + (drawerOpen ? "with-drawer-open" : "with-drawer-closed")}>
        <aside className="rail">
          <div className="rail-brand">
            <span className="mark">M</span>
            <span className="brand-text">
              <b>Meerstetter</b>
              <span>Meerstetter Gateway · v0.1</span>
            </span>
          </div>
          <div className="nav-section">Operate</div>
          <div className="nav">
            <a className={view === "fleet" ? "active" : ""} href="#/fleet">
              <span className="icon">⌗</span><span className="label">Fleet</span>
              <span className="count">{devices.length}</span>
            </a>
            <a className={view === "dictionary" ? "active" : ""} href="#/dictionary">
              <span className="icon">☰</span><span className="label">Signal dictionary</span>
            </a>
            <a className={view === "canmap" ? "active" : ""} href="#/canmap">
              <span className="icon">⇄</span><span className="label">CAN signal map</span>
            </a>
            {devices.map((d) => (
              <DeviceMini key={d.id} device={d} onOpen={() => go(`/device/${d.id}`)} />
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
              <span className="icon">∿</span><span className="label">PID tuning advisor</span>
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
            <a className={view === "help" ? "active" : ""} href="#/help">
              <span className="icon">❓</span><span className="label">Help & Data Flow</span>
            </a>
          </div>
          <div className="footer">
            <div className="rail-status">
              <div className="rail-status-row">
                <span>Gateway</span>
                <b className={isLive ? "ok" : liveError ? "warn" : ""}>{isLive ? "live" : liveError ? "fallback" : "checking"}</b>
              </div>
              <small>{isLive ? (settings.gateway || "same-origin API /api") : settings.gateway ? "explicit offline mode" : "mock fallback"}</small>
              <div className="rail-status-row"><span>Devices</span><b>{bound}/{devices.length}</b></div>
              <small>{bound} bound · {errors} errors</small>
              <small>{familyCounts.tec || 0} TEC · {familyCounts.rmm || 0} RMM{familyCounts.ldd ? ` · ${familyCounts.ldd} LDD` : ""}</small>
              <div className="rail-status-row"><span>Channels</span><b>{channels.length}</b></div>
              <small>{tempChannels.length} temperature · {supplyChannels.length} supply · {monitorChannels.length} monitor{otherChannels.length - monitorChannels.length ? ` · ${otherChannels.length - monitorChannels.length} other` : ""}</small>
              <div className="rail-status-row"><span>Leases</span><b>{leases.length}</b></div>
              <small>{leasedByMe} you · {leases.length - leasedByMe} others</small>
              <div className="rail-status-row"><span>Writes in last minute</span><b>{writesLastMinute}</b></div>
              <small>completed or accepted writes</small>
              <div className="rail-status-row"><span>{isLive ? "Config" : "Scenario"}</span><b>{isLive ? "live" : t.scenario.replace("-", " ")}</b></div>
              <small>{isLive ? "gateway config + active devices" : "change via Tweaks"}</small>
            </div>
            <span>holder · {settings.holder}</span>
          </div>
        </aside>

        <main className="main">
          {view === "fleet"      && <FleetView onOpenDevice={(id) => go(`/device/${id}`)} />}
          {view === "device"     && <DeviceWorkspace deviceId={arg} onOpenSequencer={() => go("/sequencer")} />}
          {view === "dictionary" && <SignalDictionaryView />}
          {view === "canmap"     && <CanMapView />}
          {view === "sequencer"  && <SequencerView />}
          {view === "pid"        && <PIDAdvisor deviceId={arg || fallbackDeviceId || "tec-76"} onDeviceChange={(id) => go(`/pid`)} />}
          {view === "archive"    && <ArchiveView />}
          {view === "settings"   && <SettingsView />}
          {view === "help"       && <HelpView />}
        </main>

        <aside className="cmd-drawer" aria-label="Command and lease activity">
          <button className="cmd-drawer-tab" onClick={() => setDrawerOpen((open) => !open)}>
            {drawerOpen ? "Hide" : "Activity"}
          </button>
          {drawerOpen && (
            <>
              <div className="cmd-drawer-head">
                <h3>Command activity</h3>
                <Chip>{events.length} recent</Chip>
              </div>
              <div className="cmd-drawer-body">
                <div className="cmd-drawer-feed">
                  <CommandFeed events={events} />
                </div>
                <div className="cmd-drawer-lease">
                  <div className="cmd-drawer-subhead">
                    <h3>Lease ownership</h3>
                    <Chip>{leases.length} active</Chip>
                  </div>
                  <LeaseSummary leases={leases} holder={settings.holder} />
                </div>
              </div>
            </>
          )}
        </aside>

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
          <TweakRadio label="Density" value={t.density} options={["comfortable", "compact"]} onChange={(v) => setT("density", v)} />
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
          <TweakToggle label="Fixture labels" value={t.fixtureLabels} onChange={(v) => setT("fixtureLabels", v)} />
        </TweaksPanel>
      </div>
    </ToastProvider>
  );
}

export default function AppWithSeed() {
  useEffect(() => { seedAssignments(); }, []);
  return <App />;
}
