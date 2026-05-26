// @ts-nocheck
import React, { useState } from "react";
import { MecomAPI } from "../api/mecom";
import { Chip, useGatewayTick, DiscoveryTree } from "../components/atoms";
import { useAssignments, WALLS, channelColor } from "./assignments";

export function SignalDictionaryView() {
  useGatewayTick();
  const [query, setQuery] = useState("");
  const [filterCat, setFilterCat] = useState("");
  const [onlyWritable, setOnlyWritable] = useState(false);
  const [definitionFilter, setDefinitionFilter] = useState("");

  // We use the same DiscoveryTree component but for the entire fleet
  const definitions = MecomAPI.catalogueDefinitions();
  const catalogue = MecomAPI.catalogueForDefinition(definitionFilter);
  const totalCatalogueCount = definitions.reduce((sum, definition) => sum + Number(definition.parameter_count || 0), 0);
  const channels = MecomAPI.channels();
  const settings = MecomAPI.settings();
  const assigns = useAssignments();

  // For the fleet-wide dictionary, we don't have a single "active" device/instance in the tree context
  // but we pass all channels to DiscoveryTree so it can show the fan-out.
  
  const [cards, setCards] = useState([]);
  
  function togglePin(param, instance) {
    const wallId = param.role === "control" ? "fleet-cards" : WALLS.fleetTemp.wall_id;
    const deviceId = channels.find(c => c.instance === instance)?.device_id || "fleet";
    if (assigns.hasAssignment(wallId, param.id, deviceId, instance)) {
      assigns.remove(wallId, param.id, deviceId, instance);
    } else {
      assigns.add(wallId, param.id, deviceId, instance);
    }
  }

  function openCard(param, instance) {
    setCards((cur) => cur.find((c) => c.id === param.id && (c.instance || 1) === instance) ? cur : cur.concat({ id: param.id, instance }));
  }
  function closeCard(id, instance) { setCards((cur) => cur.filter((c) => !(c.id === id && (c.instance || 1) === instance))); }

  // Group definitions by sub_family for visual separation in the tab bar
  const definitionGroups = [];
  const seenGroups = new Map();
  definitions.forEach((def) => {
    const group = def.sub_family || def.family || "other";
    if (!seenGroups.has(group)) {
      const groupObj = { label: group.toUpperCase(), items: [] };
      seenGroups.set(group, groupObj);
      definitionGroups.push(groupObj);
    }
    seenGroups.get(group).items.push(def);
  });

  return (
    <div className="dict-v2" style={{ height: "100%", display: "flex", flexDirection: "column", overflow: "hidden" }}>
      <div className="dict-definition-bar" aria-label="Catalogue definitions">
        <button type="button" className={!definitionFilter ? "on" : ""} onClick={() => setDefinitionFilter("")}>
          All <span className="count">{totalCatalogueCount}</span>
        </button>
        {definitionGroups.map((group) => (
          <React.Fragment key={group.label}>
            <span className="dict-definition-group-label">{group.label}</span>
            {group.items.map((definition) => (
              <button
                key={definition.definition_ref}
                type="button"
                className={definitionFilter === definition.definition_ref ? "on" : ""}
                onClick={() => setDefinitionFilter(definition.definition_ref)}
                title={[definition.family, definition.sub_family, definition.variant, definition.version].filter(Boolean).join(" / ")}
              >
                <span>{definition.label || definition.definition_ref}</span>
                <span className="count">{definition.parameter_count || 0}</span>
              </button>
            ))}
          </React.Fragment>
        ))}
      </div>
      <div className="dict-tree-container" style={{ flex: 1, overflow: "hidden" }}>
        <DiscoveryTree
          deviceId="fleet"
          instance={null}
          channels={channels}
          catalogue={catalogue}
          pins={[]} 
          onTogglePin={togglePin}
          onPinCard={openCard}
          onWrite={openCard}
          onlyWritable={onlyWritable}
          query={query}
          setQuery={setQuery}
          filterCat={filterCat}
          setFilterCat={setFilterCat}
          writeCards={cards}
          leaseHolder={null}
          holderId={settings.holder}
          onCloseWrite={closeCard}
        />
      </div>
      
      {filterCat === "Metadata" && <ChannelsEditor />}
    </div>
  );
}

function ChannelsEditor() {
  useGatewayTick();
  const channels = MecomAPI.channels();
  const devices = MecomAPI.devices();
  function channelCountForDevice(device) {
    const raw = Number(device && (device.channel_count ?? device.channelCount));
    if (!Number.isFinite(raw) || raw <= 0) return 4;
    return Math.max(1, Math.min(255, Math.floor(raw)));
  }
  function setRole(deviceId, instance, role) {
    MecomAPI.setChannelRole(deviceId, instance, role);
  }
  const rows = [];
  devices.forEach((d) => {
    Array.from({ length: channelCountForDevice(d) }, (_, idx) => idx + 1).forEach((inst) => {
      const ch = channels.find((c) => c.device_id === d.id && c.instance === inst);
      rows.push({ device: d, instance: inst, channel: ch });
    });
  });
  return (
    <div className="channels-editor" style={{ padding: 24, background: "var(--bg-2)", borderTop: "1px solid var(--border)", maxHeight: "min(54vh, 560px)", overflow: "auto", flex: "0 0 auto" }}>
      <div className="head" style={{ marginBottom: 16, display: "flex", alignItems: "center", gap: 12 }}>
        <h3 style={{ margin: 0 }}>Channels configuration</h3>
        <Chip>{channels.length} active</Chip>
      </div>
      <table className="dict-table">
        <thead>
          <tr><th style={{ width: "26%" }}>Device</th><th>Instance</th><th>Active</th><th>Role</th><th>Source</th><th>Label</th><th>User note</th></tr>
        </thead>
        <tbody>
          {rows.map(({ device, instance, channel }) => {
            const allowLdd = String(`${device.id} ${device.label} ${channel?.role || ""}`).toLowerCase().includes("ldd");
            return (
              <tr key={device.id + "/" + instance}>
                <td>
                  <span style={{ display: "inline-block", width: 8, height: 8, borderRadius: 2, background: channel ? channelColor(device.id, instance) : "var(--muted-2)", marginRight: 6 }}></span>
                  {device.label} <span style={{ color: "var(--muted)" }}>{device.id}</span>
                </td>
                <td>{instance}</td>
                <td>{channel ? <Chip kind="ok">active</Chip> : <Chip>inactive</Chip>}</td>
                <td>
                  <div className="role-toggle">
                    <button className={"temp " + (channel && channel.role === "temp" ? "on" : "")}
                            onClick={() => setRole(device.id, instance, "temp")}>Temp ctrl</button>
                    <button className={"supply " + (channel && channel.role === "supply" ? "on" : "")}
                            onClick={() => setRole(device.id, instance, "supply")}>Supply</button>
                    {allowLdd && (
                      <button className={"ldd " + (channel && channel.role === "ldd" ? "on" : "")}
                              onClick={() => setRole(device.id, instance, "ldd")}>LDD</button>
                    )}
                  </div>
                </td>
                <td style={{ color: "var(--muted)" }}>{channel ? channel.role_source || "config/default" : "—"}</td>
                <td style={{ color: "var(--muted)" }}>{channel ? channel.label : "—"}</td>
                <td style={{ color: "var(--muted)" }}>{channel && channel.user_note ? channel.user_note : "—"}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
