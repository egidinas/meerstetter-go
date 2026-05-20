// @ts-nocheck
import React, { useState, useEffect } from "react";
import { MecomAPI } from "../api/mecom";
import { Chip, useGatewayTick } from "../components/atoms";

export function HelpView() {
  useGatewayTick();
  const [activeNode, setActiveNode] = useState("mecomgw");
  const [devices, setDevices] = useState([]);
  const [leases, setLeases] = useState([]);

  useEffect(() => {
    setDevices(MecomAPI.devices() || []);
    setLeases(MecomAPI.leases() || []);
  }, []);

  const nodes = {
    browser: {
      title: "Operator Web UI",
      subtitle: "React/TS Dashboard",
      port: "Port 18082 / Port 3000",
      file: "web/src/",
      desc: "Single-page console application providing real-time instrumentation, custom signal mappings, PID advisor tuning, parameter dictionary exploration, and multi-device coordination.",
      dataOut: "HTTP requests, SSE subscriptions, command transmissions, lease acquisitions.",
      dataIn: "SSE telemetry streams, command execution events, lease tables, active fleet metadata."
    },
    serviceTool: {
      title: "MeCom Service Software",
      subtitle: "Desktop Vendor App",
      port: "TCP Port 50000",
      file: "N/A (Proprietary)",
      desc: "Official desktop control and diagnostics software provided by Meerstetter. Configured to point to the gateway's multiplexed virtual serial socket to monitor or tune the hardware directly.",
      dataOut: "Direct binary MeCom frames (Query/Set commands) addressed to specific Node IDs.",
      dataIn: "Acknowledge payloads and responses forwarded back from target physical controllers."
    },
    mecomgw: {
      title: "MeCom Gateway (mecomgw)",
      subtitle: "REST & Telemetry Broker",
      port: "Port 18082",
      file: "cmd/mecomgw/main.go",
      desc: "Serves web console assets and exposes a unified JSON REST API. Manages device connections, buffers telemetry history in memory, publishes telemetry via SSE, and enforces write lock leases to prevent concurrent write collisions.",
      dataOut: "REST API responses, SSE telemetry streams, binary MeCom writes to devices/proxies.",
      dataIn: "Telecommands from Web UI, raw parameters from devices, configuration JSON parameters."
    },
    mecomvseriald: {
      title: "Virtual Serial Router (mecomvseriald)",
      subtitle: "MeCom Frame Router",
      port: "Port 50000",
      file: "cmd/mecomvseriald/main.go",
      desc: "Virtual serial multiplexer daemon that binds to a TCP port and exposes access to the serial bus. Inspects incoming frames, extracts the target MeCom Address Byte, and routes the packet to the correct USB-to-UART port.",
      dataOut: "Binary MeCom frames to specific FTDI USB serial nodes.",
      dataIn: "Binary MeCom replies from serial lines, routed back to connected TCP socket clients."
    },
    physical: {
      title: "Physical Transport Layer",
      subtitle: "UART FTDI / CAN Bus",
      port: "TTY / can0",
      file: "mecom/transport.go",
      desc: "FTDI USB-to-UART serial bridges (`/dev/serial/by-id/usb-FTDI_*`) running at 57600 baud, and/or SocketCAN interfaces routing binary packages directly onto the hardware bus.",
      dataOut: "Differential serial/CAN signals sent across cables.",
      dataIn: "Differential serial/CAN signals received from microcontroller boards."
    },
    fleet: {
      title: "TEC Fleet Controllers",
      subtitle: "Microcontroller Hardware",
      port: "Nodes 75, 76, 81, 84",
      file: "mecom/protocol.go",
      desc: "Physical Meerstetter 8065-TEC controllers executing closed-loop thermal control. Manages safety limits and throws protection faults (e.g. Overvoltage Error 104) to prevent Peltier damage.",
      dataOut: "Measured telemetry (temp, voltage, current, status flags), command ACK responses.",
      dataIn: "Configuration parameters (error thresholds, setpoints, output enable command)."
    }
  };

  const selected = nodes[activeNode] || nodes.mecomgw;

  return (
    <div className="help-view-container" style={styles.container}>
      <style>{cssStyles}</style>
      
      {/* Header */}
      <div className="help-header" style={styles.header}>
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <h2 style={{ margin: 0, fontSize: 20, fontWeight: 600 }}>System Architecture & Data Flow</h2>
          <Chip>Help Center</Chip>
        </div>
        <p style={{ margin: "4px 0 0", color: "var(--muted)", fontSize: 13 }}>
          Interactive system topology, protocol documentation, and error recovery guide.
        </p>
      </div>

      <div className="help-layout" style={styles.layout}>
        {/* Left Side: Data Flow Visualization */}
        <div className="help-visual-card" style={styles.visualCard}>
          <div style={{ padding: "16px 20px 0", borderBottom: "1px solid var(--hairline)" }}>
            <h3 style={{ margin: 0, fontSize: 14, fontWeight: 600, color: "var(--text-soft)" }}>Interactive Data Flow Topology</h3>
            <p style={{ margin: "4px 0 12px", color: "var(--muted)", fontSize: 11 }}>
              Hover or click elements in the diagram to inspect component details and data paths.
            </p>
          </div>

          <div style={styles.canvasContainer}>
            {/* SVG Connecting Paths with animated dash offsets to show active data flow */}
            <svg style={styles.svgOverlay} width="100%" height="100%" viewBox="0 0 800 480" preserveAspectRatio="none">
              <defs>
                <linearGradient id="blueGlow" x1="0%" y1="0%" x2="100%" y2="0%">
                  <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.2" />
                  <stop offset="100%" stopColor="var(--accent)" stopOpacity="0.8" />
                </linearGradient>
                <linearGradient id="purpleGlow" x1="0%" y1="0%" x2="0%" y2="100%">
                  <stop offset="0%" stopColor="var(--accent-2)" stopOpacity="0.8" />
                  <stop offset="100%" stopColor="var(--accent-2)" stopOpacity="0.2" />
                </linearGradient>
              </defs>

              {/* Path: Browser -> Gateway */}
              <path d="M 210,95 L 210,180" stroke="var(--line-strong)" strokeWidth="2" fill="none" />
              <path d="M 210,95 L 210,180" stroke="var(--accent)" strokeWidth="2" strokeDasharray="6,6" fill="none" className="flow-dash-down" />

              {/* Path: Service Tool -> vseriald */}
              <path d="M 590,95 L 590,180" stroke="var(--line-strong)" strokeWidth="2" fill="none" />
              <path d="M 590,95 L 590,180" stroke="var(--accent-2)" strokeWidth="2" strokeDasharray="6,6" fill="none" className="flow-dash-down" />

              {/* Path: Gateway -> vseriald */}
              <path d="M 330,215 L 470,215" stroke="var(--line-strong)" strokeWidth="2" fill="none" />
              <path d="M 330,215 L 470,215" stroke="var(--accent)" strokeWidth="2" strokeDasharray="6,6" fill="none" className="flow-dash-right" />

              {/* Path: vseriald -> Physical */}
              <path d="M 590,250 L 590,310 L 400,310" stroke="var(--line-strong)" strokeWidth="2" fill="none" />
              <path d="M 590,250 L 590,310 L 400,310" stroke="var(--accent-2)" strokeWidth="2" strokeDasharray="6,6" fill="none" className="flow-dash-left" />

              {/* Path: Gateway -> Physical (Direct fallback/routes) */}
              <path d="M 210,250 L 210,310 L 400,310" stroke="var(--line-strong)" strokeWidth="2" fill="none" />
              <path d="M 210,250 L 210,310 L 400,310" stroke="var(--accent)" strokeWidth="2" strokeDasharray="6,6" fill="none" className="flow-dash-right" />

              {/* Path: Physical -> Fleet (Bus fanout) */}
              <path d="M 400,335 L 400,370" stroke="var(--line-strong)" strokeWidth="2" fill="none" />
              <path d="M 400,370 L 125,370 L 125,395" stroke="var(--line-strong)" strokeWidth="2" fill="none" />
              <path d="M 400,370 L 308,370 L 308,395" stroke="var(--line-strong)" strokeWidth="2" fill="none" />
              <path d="M 400,370 L 492,370 L 492,395" stroke="var(--line-strong)" strokeWidth="2" fill="none" />
              <path d="M 400,370 L 675,370 L 675,395" stroke="var(--line-strong)" strokeWidth="2" fill="none" />

              <path d="M 400,335 L 400,370 L 125,370 L 125,395" stroke="var(--accent)" strokeWidth="1.5" strokeDasharray="4,4" fill="none" className="flow-dash-down" />
              <path d="M 400,370 L 308,370 L 308,395" stroke="var(--accent)" strokeWidth="1.5" strokeDasharray="4,4" fill="none" className="flow-dash-down" />
              <path d="M 400,370 L 492,370 L 492,395" stroke="var(--accent)" strokeWidth="1.5" strokeDasharray="4,4" fill="none" className="flow-dash-down" />
              <path d="M 400,370 L 675,370 L 675,395" stroke="var(--accent)" strokeWidth="1.5" strokeDasharray="4,4" fill="none" className="flow-dash-down" />
            </svg>

            {/* Diagram Nodes Layer */}
            <div style={styles.gridOverlay}>
              
              {/* Row 1: Clients */}
              <div 
                className={`diagram-node ${activeNode === "browser" ? "active" : ""}`}
                style={{ ...styles.node, gridColumn: 1, gridRow: 1, borderColor: "var(--accent)" }}
                onMouseEnter={() => setActiveNode("browser")}
              >
                <div style={styles.nodeHeader}>
                  <span style={{ color: "var(--accent)" }}>🖥️</span>
                  <strong>Operator Web Console</strong>
                </div>
                <div style={styles.nodeBody}>React Single Page App</div>
                <div style={styles.nodeBadge}>Client REST/SSE</div>
              </div>

              <div 
                className={`diagram-node ${activeNode === "serviceTool" ? "active" : ""}`}
                style={{ ...styles.node, gridColumn: 3, gridRow: 1, borderColor: "var(--accent-2)" }}
                onMouseEnter={() => setActiveNode("serviceTool")}
              >
                <div style={styles.nodeHeader}>
                  <span style={{ color: "var(--accent-2)" }}>🛠️</span>
                  <strong>MeCom Service Software</strong>
                </div>
                <div style={styles.nodeBody}>Vendor Desktop Client</div>
                <div style={styles.nodeBadge}>Client MeCom TCP</div>
              </div>

              {/* Row 2: Daemons */}
              <div 
                className={`diagram-node ${activeNode === "mecomgw" ? "active" : ""}`}
                style={{ ...styles.node, gridColumn: 1, gridRow: 2, borderColor: "var(--accent)" }}
                onMouseEnter={() => setActiveNode("mecomgw")}
              >
                <div style={styles.nodeHeader}>
                  <span style={{ color: "var(--accent)" }}>⚙️</span>
                  <strong>MeCom Gateway</strong>
                </div>
                <div style={styles.nodeBody}>Go Daemon (Port 18082)</div>
                <div style={styles.nodeBadge}>JSON API & Lease registry</div>
              </div>

              <div 
                className={`diagram-node ${activeNode === "mecomvseriald" ? "active" : ""}`}
                style={{ ...styles.node, gridColumn: 3, gridRow: 2, borderColor: "var(--accent-2)" }}
                onMouseEnter={() => setActiveNode("mecomvseriald")}
              >
                <div style={styles.nodeHeader}>
                  <span style={{ color: "var(--accent-2)" }}>🔀</span>
                  <strong>Virtual Serial Router</strong>
                </div>
                <div style={styles.nodeBody}>mecomvseriald (Port 50000)</div>
                <div style={styles.nodeBadge}>Address-based Multiplexer</div>
              </div>

              {/* Row 3: Physical */}
              <div 
                className={`diagram-node ${activeNode === "physical" ? "active" : ""}`}
                style={{ 
                  ...styles.node, 
                  gridColumn: "1 / span 3", 
                  gridRow: 3, 
                  width: "280px", 
                  justifySelf: "center",
                  borderColor: "var(--text-soft)"
                }}
                onMouseEnter={() => setActiveNode("physical")}
              >
                <div style={styles.nodeHeader}>
                  <span>🔗</span>
                  <strong>Physical Transport layer</strong>
                </div>
                <div style={styles.nodeBody}>USB FTDI Bridges / SocketCAN</div>
                <div style={styles.nodeBadge}>Baud Rate: 57600</div>
              </div>

              {/* Row 4: Upstream Fleet */}
              <div 
                className={`diagram-node ${activeNode === "fleet" ? "active" : ""}`}
                style={{ ...styles.node, gridColumn: 1, gridRow: 4, width: "130px", fontSize: "11px" }}
                onMouseEnter={() => setActiveNode("fleet")}
              >
                <div style={{ ...styles.nodeHeader, fontSize: "11px" }}><strong>TEC-75</strong></div>
                <div style={{ fontSize: "10px", color: "var(--muted)" }}>Target Addr: 75</div>
                <div style={{ marginTop: 4, display: "flex", gap: 4 }}>
                  <span className="live-status-dot green"></span>
                  <span style={{ fontSize: "9px" }}>CONNECTED</span>
                </div>
              </div>

              <div 
                className={`diagram-node ${activeNode === "fleet" ? "active" : ""}`}
                style={{ ...styles.node, gridColumn: 2, gridRow: 4, width: "130px", fontSize: "11px" }}
                onMouseEnter={() => setActiveNode("fleet")}
              >
                <div style={{ ...styles.nodeHeader, fontSize: "11px" }}><strong>TEC-76</strong></div>
                <div style={{ fontSize: "10px", color: "var(--muted)" }}>Target Addr: 76</div>
                <div style={{ marginTop: 4, display: "flex", gap: 4 }}>
                  <span className="live-status-dot red"></span>
                  <span style={{ fontSize: "9px", color: "var(--bad)" }}>FAULT 104</span>
                </div>
              </div>

              <div 
                className={`diagram-node ${activeNode === "fleet" ? "active" : ""}`}
                style={{ ...styles.node, gridColumn: 3, gridRow: 4, width: "130px", fontSize: "11px" }}
                onMouseEnter={() => setActiveNode("fleet")}
              >
                <div style={{ ...styles.nodeHeader, fontSize: "11px" }}><strong>TEC-81</strong></div>
                <div style={{ fontSize: "10px", color: "var(--muted)" }}>Target Addr: 81</div>
                <div style={{ marginTop: 4, display: "flex", gap: 4 }}>
                  <span className="live-status-dot green"></span>
                  <span style={{ fontSize: "9px" }}>CONNECTED</span>
                </div>
              </div>

              <div 
                className={`diagram-node ${activeNode === "fleet" ? "active" : ""}`}
                style={{ ...styles.node, gridColumn: 4, gridRow: 4, width: "130px", fontSize: "11px" }}
                onMouseEnter={() => setActiveNode("fleet")}
              >
                <div style={{ ...styles.nodeHeader, fontSize: "11px" }}><strong>TEC-84</strong></div>
                <div style={{ fontSize: "10px", color: "var(--muted)" }}>Target Addr: 84</div>
                <div style={{ marginTop: 4, display: "flex", gap: 4 }}>
                  <span className="live-status-dot green"></span>
                  <span style={{ fontSize: "9px" }}>CONNECTED</span>
                </div>
              </div>

            </div>
          </div>
        </div>

        {/* Right Side: Component Details Inspector Card */}
        <div className="help-inspector-card" style={styles.inspectorCard}>
          <div style={{ padding: "16px 20px 12px", borderBottom: "1px solid var(--hairline)" }}>
            <h3 style={{ margin: 0, fontSize: 14, fontWeight: 600, color: "var(--accent)" }}>Component Inspector</h3>
            <span style={{ fontSize: "11px", color: "var(--muted)" }}>Selected Element Detail</span>
          </div>

          <div style={{ padding: "20px", display: "flex", flexDirection: "column", gap: "16px", flex: 1, overflowY: "auto" }}>
            <div>
              <span style={{ fontSize: 10, color: "var(--muted-2)", textTransform: "uppercase", fontWeight: "bold" }}>Component Name</span>
              <h4 style={{ margin: "2px 0 0", fontSize: 16, color: "var(--text)" }}>{selected.title}</h4>
              <span style={{ fontSize: 11, color: "var(--muted)" }}>{selected.subtitle}</span>
            </div>

            <div style={styles.inspectorGrid}>
              <div>
                <span style={styles.inspectorGridLabel}>Address / Port</span>
                <div style={styles.inspectorGridVal}>{selected.port}</div>
              </div>
              <div>
                <span style={styles.inspectorGridLabel}>Primary Source File</span>
                <div style={{ ...styles.inspectorGridVal, fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--accent)" }}>
                  {selected.file}
                </div>
              </div>
            </div>

            <div>
              <span style={styles.inspectorGridLabel}>Description</span>
              <p style={{ margin: "4px 0 0", fontSize: 12, lineHeight: 1.45, color: "var(--text-soft)" }}>
                {selected.desc}
              </p>
            </div>

            <div style={{ borderTop: "1px solid var(--hairline)", paddingTop: "12px" }}>
              <span style={styles.inspectorGridLabel}>Data Outflows</span>
              <div style={{ color: "var(--ok)", fontSize: 11, fontFamily: "var(--font-mono)", marginTop: 4 }}>
                ⮕ {selected.dataOut}
              </div>
            </div>

            <div>
              <span style={styles.inspectorGridLabel}>Data Inflows</span>
              <div style={{ color: "var(--accent)", fontSize: 11, fontFamily: "var(--font-mono)", marginTop: 4 }}>
                ⬅ {selected.dataIn}
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Details Sections */}
      <div className="help-content-sections" style={styles.contentSection}>
        
        {/* Tab Cards */}
        <div style={styles.tabGrid}>
          
          {/* Card 1: Telemetry Mechanism */}
          <div style={styles.tabCard}>
            <div style={styles.tabCardHeader}>
              <span style={{ fontSize: 18 }}>📊</span>
              <h4 style={{ margin: 0, fontSize: 14 }}>Telemetry & Oversampling</h4>
            </div>
            <p style={styles.tabCardBody}>
              The gateway (<code style={styles.code}>mecomgw</code>) runs a background polling loop that periodically queries 
              devices. For high-frequency thermal analysis, a <code style={styles.code}>RingReader</code> client utilizes the 
              device's local ring-buffer oversampling mechanism to read voltage (<code style={styles.code}>1021</code>) and 
              current (<code style={styles.code}>1020</code>) registers at sub-millisecond intervals. This feeds raw data 
              into memory, which is then made available to client UIs via Server-Sent Events (SSE).
            </p>
          </div>

          {/* Card 2: Concurrency & Lease Lock */}
          <div style={styles.tabCard}>
            <div style={styles.tabCardHeader}>
              <span style={{ fontSize: 18 }}>🔒</span>
              <h4 style={{ margin: 0, fontSize: 14 }}>Leases & Write Security</h4>
            </div>
            <p style={styles.tabCardBody}>
              To prevent concurrent write conflicts from multiple browser tabs or API clients, the gateway implements a 
              concurrency lock system called <strong>Leases</strong>. A client must request a lease via 
              <code style={styles.code}>POST /api/devices/{"{id}"}/lease</code>. This returns a temporary 
              <code style={styles.code}>lease_token</code>. Subsequent write requests must attach this token as 
              the <code style={styles.code}>X-Lease-Token</code> HTTP header, otherwise the gateway will reject 
              the command with a <code style={styles.code}>423 Locked</code> or <code style={styles.code}>409 Conflict</code>.
            </p>
          </div>

          {/* Card 3: Address-Based Routing */}
          <div style={styles.tabCard}>
            <div style={styles.tabCardHeader}>
              <span style={{ fontSize: 18 }}>🔀</span>
              <h4 style={{ margin: 0, fontSize: 14 }}>Virtual Serial Multiplexing</h4>
            </div>
            <p style={styles.tabCardBody}>
              The serial proxy daemon (<code style={styles.code}>mecomvseriald</code>) is crucial for permitting multiple 
              clients to share serial lines. It parses the binary MeCom protocol frames, extracts the target device address byte 
              from the header, and maps it to the respective physical USB FTDI device node. Address <code style={styles.code}>0</code> 
              is held to a configured default controller during bootstrap; duplicate serial/CAN routes are used as fixed-preference 
              or read-only fallback candidates.
            </p>
          </div>

        </div>

        {/* API Cheat Sheet */}
        <div style={{ ...styles.tabCard, marginTop: 16 }}>
          <div style={styles.tabCardHeader}>
            <span style={{ fontSize: 18 }}>🔌</span>
            <h4 style={{ margin: 0, fontSize: 14 }}>Gateway REST API Reference</h4>
          </div>
          <div style={{ overflowX: "auto" }}>
            <table className="dict-table" style={{ width: "100%", borderCollapse: "collapse", marginTop: 8 }}>
              <thead>
                <tr style={{ borderBottom: "1px solid var(--line)" }}>
                  <th style={styles.th}>HTTP Method</th>
                  <th style={styles.th}>API Endpoint</th>
                  <th style={styles.th}>Description</th>
                  <th style={styles.th}>Payload / Header Required</th>
                </tr>
              </thead>
              <tbody>
                <tr style={styles.tr}>
                  <td style={styles.td}><span className="badge-get">GET</span></td>
                  <td style={{ ...styles.td, fontFamily: "var(--font-mono)", color: "var(--accent)" }}>/api/devices</td>
                  <td style={styles.td}>List all configured devices and their connectivity and error states.</td>
                  <td style={styles.td}>None</td>
                </tr>
                <tr style={styles.tr}>
                  <td style={styles.td}><span className="badge-get">GET</span></td>
                  <td style={{ ...styles.td, fontFamily: "var(--font-mono)", color: "var(--accent)" }}>/api/devices/{"{id}"}/poll</td>
                  <td style={styles.td}>SSE stream of telemetry (pushes temperature, voltage, and current samples).</td>
                  <td style={styles.td}>None</td>
                </tr>
                <tr style={styles.tr}>
                  <td style={styles.td}><span className="badge-post">POST</span></td>
                  <td style={{ ...styles.td, fontFamily: "var(--font-mono)", color: "var(--accent)" }}>/api/devices/{"{id}"}/lease</td>
                  <td style={styles.td}>Acquire a temporary write lock token to allow configuring parameter values.</td>
                  <td style={styles.td}><code style={styles.code}>{"{ holder: string, ttl: string }"}</code></td>
                </tr>
                <tr style={styles.tr}>
                  <td style={styles.td}><span className="badge-post">POST</span></td>
                  <td style={{ ...styles.td, fontFamily: "var(--font-mono)", color: "var(--accent)" }}>/api/devices/{"{id}"}/write</td>
                  <td style={styles.td}>Execute parameter write. Rejects if write lock lease is held by another user.</td>
                  <td style={styles.td}>Header: <code style={styles.code}>X-Lease-Token</code></td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        {/* Error 104 Troubleshooting & Recovery */}
        <div style={{ ...styles.tabCard, marginTop: 16, borderLeft: "4px solid var(--bad)" }}>
          <div style={styles.tabCardHeader}>
            <span style={{ fontSize: 18, color: "var(--bad)" }}>🚨</span>
            <h4 style={{ margin: 0, fontSize: 14, color: "var(--bad)" }}>Hardware Error Recovery Guide (TEC Error 104)</h4>
          </div>
          <p style={{ ...styles.tabCardBody, marginTop: 8 }}>
            <strong>Error Description:</strong> Error 104 occurs when the output stage detects an 
            <strong> Overvoltage condition at OUT+</strong>. The measured output voltage has exceeded the defined 
            user threshold limit, and the controller has entered safety lockdown.
          </p>
          <div style={{ paddingLeft: 12, borderLeft: "2px dashed var(--line-strong)", marginTop: 12 }}>
            <h5 style={{ margin: "0 0 6px", fontSize: 12, color: "var(--text)" }}>Steps to Resolve Error 104 Programmatically:</h5>
            <ol style={{ margin: 0, paddingLeft: 16, fontSize: 12, color: "var(--text-soft)", display: "flex", flexDirection: "column", gap: 6 }}>
              <li>
                <strong>Disable Device Output stages (Write 2010):</strong> Set parameter <code style={styles.code}>2010 (Output Enable)</code> 
                to <code style={styles.code}>0 (Disabled)</code> for all 4 instances of the device. 
                <span style={{ color: "var(--warn)" }}> Note: Limits cannot be changed while the device status is in RUN mode.</span>
              </li>
              <li>
                <strong>Raise Error Voltage Threshold (Write 2033):</strong> Increase parameter <code style={styles.code}>2033 (Output Voltage Error Threshold)</code> 
                to a safe maximum value (e.g. <code style={styles.code}>60.0V</code>) to prevent false trips during high-load transients.
              </li>
              <li>
                <strong>Trigger Error Reset (Write 1084):</strong> Write <code style={styles.code}>1</code> to parameter <code style={styles.code}>1084 (Error Reset)</code> 
                to clear the fault flags on the microcontroller.
              </li>
              <li>
                <strong>Alternative (Soft Reboot / RS Frame):</strong> If the device remains unresponsive or stuck in Status 3 (Run), send a soft 
                reboot command via the MeCom protocol's <code style={styles.code}>RS</code> (Reset System) frame to cycle the device power.
              </li>
            </ol>
          </div>
        </div>

      </div>
    </div>
  );
}

// Inline Styles for visual design
const styles = {
  container: {
    padding: "24px",
    display: "flex",
    flexDirection: "column",
    gap: "20px",
    height: "100%",
    overflowY: "auto",
    background: "var(--bg)",
    color: "var(--text)",
    fontFamily: "var(--font-sans)"
  },
  header: {
    borderBottom: "1px solid var(--hairline)",
    paddingBottom: "12px"
  },
  layout: {
    display: "grid",
    gridTemplateColumns: "1.7fr 1fr",
    gap: "20px",
    alignItems: "stretch"
  },
  visualCard: {
    background: "var(--panel)",
    border: "1px solid var(--line)",
    borderRadius: "var(--radius-lg)",
    display: "flex",
    flexDirection: "column",
    position: "relative",
    overflow: "hidden",
    minHeight: "520px"
  },
  canvasContainer: {
    flex: 1,
    position: "relative",
    background: "#080b0f"
  },
  svgOverlay: {
    position: "absolute",
    top: 0,
    left: 0,
    width: "100%",
    height: "100%",
    pointerEvents: "none"
  },
  gridOverlay: {
    position: "absolute",
    top: 0,
    left: 0,
    width: "100%",
    height: "100%",
    display: "grid",
    gridTemplateColumns: "1fr 1fr 1fr 1fr",
    gridTemplateRows: "110px 120px 100px 100px",
    padding: "20px",
    gap: "10px",
    alignItems: "center"
  },
  node: {
    background: "var(--panel-2)",
    border: "1px solid var(--line)",
    borderRadius: "var(--radius)",
    padding: "10px 12px",
    cursor: "pointer",
    display: "flex",
    flexDirection: "column",
    gap: "4px",
    justifySelf: "center",
    width: "170px",
    height: "75px",
    boxShadow: "0 2px 5px rgba(0,0,0,0.3)",
    transition: "transform 0.2s, box-shadow 0.2s, border-color 0.2s",
    position: "relative",
    zIndex: 10
  },
  nodeHeader: {
    display: "flex",
    alignItems: "center",
    gap: "6px",
    fontSize: "11px",
    fontWeight: "bold",
    whiteSpace: "nowrap",
    overflow: "hidden",
    textOverflow: "ellipsis"
  },
  nodeBody: {
    fontSize: "10px",
    color: "var(--muted)",
    whiteSpace: "nowrap",
    overflow: "hidden",
    textOverflow: "ellipsis"
  },
  nodeBadge: {
    fontSize: "8px",
    background: "var(--panel-3)",
    border: "1px solid var(--hairline)",
    padding: "1px 4px",
    borderRadius: "2px",
    width: "fit-content",
    color: "var(--muted-2)"
  },
  inspectorCard: {
    background: "var(--panel)",
    border: "1px solid var(--line)",
    borderRadius: "var(--radius-lg)",
    display: "flex",
    flexDirection: "column",
    boxShadow: "0 4px 15px rgba(0,0,0,0.5)"
  },
  inspectorGrid: {
    display: "grid",
    gridTemplateColumns: "1fr 1.2fr",
    gap: "12px",
    borderTop: "1px solid var(--hairline)",
    borderBottom: "1px solid var(--hairline)",
    padding: "12px 0"
  },
  inspectorGridLabel: {
    fontSize: 9,
    color: "var(--muted-2)",
    textTransform: "uppercase",
    fontWeight: "bold",
    display: "block"
  },
  inspectorGridVal: {
    fontSize: 11,
    fontWeight: 600,
    marginTop: 2,
    color: "var(--text-soft)",
    wordBreak: "break-all"
  },
  contentSection: {
    display: "flex",
    flexDirection: "column",
    gap: "16px"
  },
  tabGrid: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr 1fr",
    gap: "16px"
  },
  tabCard: {
    background: "var(--panel)",
    border: "1px solid var(--line)",
    borderRadius: "var(--radius-lg)",
    padding: "16px",
    display: "flex",
    flexDirection: "column",
    gap: "10px"
  },
  tabCardHeader: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    fontWeight: 600,
    color: "var(--text)"
  },
  tabCardBody: {
    margin: 0,
    fontSize: "12px",
    lineHeight: 1.5,
    color: "var(--text-soft)"
  },
  code: {
    fontFamily: "var(--font-mono)",
    fontSize: "11px",
    background: "var(--panel-2)",
    border: "1px solid var(--hairline)",
    padding: "1px 4px",
    borderRadius: "3px",
    color: "var(--accent)"
  },
  th: {
    textAlign: "left",
    padding: "8px 12px",
    fontSize: "11px",
    textTransform: "uppercase",
    color: "var(--muted-2)",
    background: "var(--panel-2)"
  },
  tr: {
    borderBottom: "1px solid var(--hairline)"
  },
  td: {
    padding: "10px 12px",
    fontSize: "12px",
    color: "var(--text-soft)"
  }
};

const cssStyles = `
  .diagram-node:hover {
    transform: translateY(-2px);
    box-shadow: 0 4px 15px rgba(0,0,0,0.5), 0 0 10px var(--accent-soft);
    border-color: var(--accent) !important;
  }
  
  .diagram-node.active {
    transform: translateY(-2px);
    box-shadow: 0 4px 15px rgba(0,0,0,0.5), 0 0 12px var(--accent-soft);
    border-color: var(--accent) !important;
    background: var(--panel-3) !important;
  }

  .live-status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    display: inline-block;
    align-self: center;
  }
  .live-status-dot.green {
    background: var(--ok);
    box-shadow: 0 0 6px var(--ok);
    animation: pulse-green 1.5s infinite;
  }
  .live-status-dot.red {
    background: var(--bad);
    box-shadow: 0 0 6px var(--bad);
    animation: pulse-red 1.5s infinite;
  }

  @keyframes pulse-green {
    0% { transform: scale(1); opacity: 1; }
    50% { transform: scale(1.3); opacity: 0.6; }
    100% { transform: scale(1); opacity: 1; }
  }
  @keyframes pulse-red {
    0% { transform: scale(1); opacity: 1; }
    50% { transform: scale(1.3); opacity: 0.6; }
    100% { transform: scale(1); opacity: 1; }
  }

  .flow-dash-down {
    animation: flowDown 12s linear infinite;
  }
  .flow-dash-right {
    animation: flowRight 12s linear infinite;
  }
  .flow-dash-left {
    animation: flowLeft 12s linear infinite;
  }

  @keyframes flowDown {
    to { stroke-dashoffset: -120; }
  }
  @keyframes flowRight {
    to { stroke-dashoffset: -120; }
  }
  @keyframes flowLeft {
    to { stroke-dashoffset: 120; }
  }

  .badge-get {
    background: rgba(76, 220, 106, 0.15);
    color: var(--ok);
    border: 1px solid rgba(76, 220, 106, 0.3);
    font-size: 9px;
    font-weight: bold;
    padding: 2px 6px;
    border-radius: 3px;
  }
  .badge-post {
    background: rgba(107, 182, 255, 0.15);
    color: var(--accent);
    border: 1px solid rgba(107, 182, 255, 0.3);
    font-size: 9px;
    font-weight: bold;
    padding: 2px 6px;
    border-radius: 3px;
  }
`;
