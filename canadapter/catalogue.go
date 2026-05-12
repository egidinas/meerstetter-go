package canadapter

import (
	"fmt"
	"sort"
	"strings"
)

// TransportClass identifies the host/runtime layer that owns the CAN adapter.
type TransportClass string

const (
	TransportSocketCAN       TransportClass = "socketcan"
	TransportKvaserCANlib    TransportClass = "kvaser-canlib"
	TransportSLCAN           TransportClass = "slcan"
	TransportEthernetGateway TransportClass = "ethernet-gateway"
	TransportRemoteBridge    TransportClass = "remote-bridge"
)

// Capability describes an adapter behavior that affects readout strategy.
type Capability string

const (
	CapabilityPassiveMonitor Capability = "passive-monitor"
	CapabilitySharedOpen     Capability = "shared-open"
	CapabilityLocalTimestamp Capability = "local-timestamp"
	CapabilityEdgeScript     Capability = "edge-script"
	CapabilityObjectBuffers  Capability = "object-buffers"
	CapabilitySocketCAN      Capability = "socketcan-netdev"
)

// Maturity captures whether this repository has bench confidence for a profile.
type Maturity string

const (
	MaturityFirstClass   Maturity = "first-class"
	MaturityEasyUnproven Maturity = "easy-unproven"
)

// DriverRef links an adapter profile to the driver/API expected to expose it.
type DriverRef struct {
	Name string
	URL  string
}

// Check is an operator-facing preflight or bring-up check for an adapter.
type Check struct {
	Name        string
	Command     string
	Expect      string
	FailureMode string
	Remedy      string
}

// Profile captures reusable bring-up assumptions for one adapter family.
// It deliberately avoids opening hardware; applications can render these
// checks into shell runbooks, UI diagnostics, or node-local probes.
type Profile struct {
	ID              string
	DisplayName     string
	FirstClass      bool
	Maturity        Maturity
	Transport       TransportClass
	SocketCANDriver []string
	InterfaceHints  []string
	DefaultBitrates []int
	Capabilities    []Capability
	DriverRefs      []DriverRef
	Checks          []Check
	Notes           []string
}

const (
	ProfilePiXtendV2L       = "pixtend-v2l"
	ProfileSocketCANGeneric = "socketcan-generic"
	ProfileCandleLight      = "candlelight-gs-usb"
	ProfilePCANUSB          = "peak-pcan-usb"
	ProfileEMSUSB           = "ems-cpc-usb"
	ProfileESDUSB           = "esd-usb"
	ProfileMicrochipCAN     = "microchip-can-bus-analyzer"
	ProfileCAN327           = "elm327-can327"
	ProfileKvaserUSB        = "kvaser-usb"
	ProfileKvaserEthernet   = "kvaser-ethernet"
	ProfileKvaserDINRail    = ProfileKvaserEthernet
	ProfileSLCANUSB         = "slcan-usb"
	ProfileRemoteSocketCAN  = "remote-socketcan"
)

// Profiles returns the built-in adapter catalogue ordered by profile ID.
func Profiles() []Profile {
	out := []Profile{
		pixtendV2LProfile(),
		socketCANGenericProfile(),
		candleLightProfile(),
		pcanUSBProfile(),
		emsUSBProfile(),
		esdUSBProfile(),
		microchipCANProfile(),
		can327Profile(),
		kvaserUSBProfile(),
		kvaserEthernetProfile(),
		slcanUSBProfile(),
		remoteSocketCANProfile(),
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// LookupProfile returns one built-in profile by stable ID.
func LookupProfile(id string) (Profile, bool) {
	id = strings.TrimSpace(strings.ToLower(id))
	for _, profile := range Profiles() {
		if profile.ID == id {
			return profile, true
		}
	}
	return Profile{}, false
}

// MustProfile returns a profile or panics. It is intended for package defaults
// and tests where an unknown ID is a programming error.
func MustProfile(id string) Profile {
	profile, ok := LookupProfile(id)
	if !ok {
		panic(fmt.Sprintf("canadapter: unknown profile %q", id))
	}
	return profile
}

// MatchSocketCANDriver returns profiles that commonly surface through a Linux
// SocketCAN netdevice backed by the given kernel driver name.
func MatchSocketCANDriver(driver string) []Profile {
	driver = strings.TrimSpace(strings.ToLower(driver))
	if driver == "" {
		return nil
	}
	var matches []Profile
	for _, profile := range Profiles() {
		for _, candidate := range profile.SocketCANDriver {
			if strings.EqualFold(candidate, driver) {
				matches = append(matches, profile)
				break
			}
		}
	}
	return matches
}

// RenderChecklist produces a compact operator checklist for the profile.
func RenderChecklist(profile Profile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", profile.DisplayName, profile.ID)
	if profile.FirstClass {
		fmt.Fprintf(&b, "tier: first-class\n")
	} else if profile.Maturity != "" {
		fmt.Fprintf(&b, "tier: %s\n", profile.Maturity)
	}
	fmt.Fprintf(&b, "transport: %s\n", profile.Transport)
	if len(profile.DefaultBitrates) > 0 {
		fmt.Fprintf(&b, "bitrates: %s\n", formatInts(profile.DefaultBitrates))
	}
	if len(profile.InterfaceHints) > 0 {
		fmt.Fprintf(&b, "interfaces: %s\n", strings.Join(profile.InterfaceHints, ","))
	}
	if len(profile.DriverRefs) > 0 {
		for _, ref := range profile.DriverRefs {
			if ref.URL == "" {
				fmt.Fprintf(&b, "driver: %s\n", ref.Name)
			} else {
				fmt.Fprintf(&b, "driver: %s <%s>\n", ref.Name, ref.URL)
			}
		}
	}
	for _, check := range profile.Checks {
		fmt.Fprintf(&b, "- %s: %s\n", check.Name, check.Expect)
		if check.Command != "" {
			fmt.Fprintf(&b, "  command: %s\n", check.Command)
		}
		if check.Remedy != "" {
			fmt.Fprintf(&b, "  remedy: %s\n", check.Remedy)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func pixtendV2LProfile() Profile {
	return Profile{
		ID:              ProfilePiXtendV2L,
		DisplayName:     "PiXtend V2-L MCP2515 on Raspberry Pi",
		FirstClass:      true,
		Maturity:        MaturityFirstClass,
		Transport:       TransportSocketCAN,
		SocketCANDriver: []string{"mcp251x"},
		InterfaceHints:  []string{"can0", "can1"},
		DefaultBitrates: []int{1000000, 500000, 250000, 125000},
		Capabilities:    []Capability{CapabilitySocketCAN, CapabilityPassiveMonitor},
		DriverRefs:      []DriverRef{{Name: "Linux SocketCAN mcp251x"}},
		Checks: []Check{
			{
				Name:        "root filesystem writable",
				Command:     "mount | grep ' / '",
				Expect:      "root filesystem is rw; no fresh EXT4 aborts in dmesg",
				FailureMode: "CAN diagnostics are noisy or meaningless when the Pi boots read-only",
				Remedy:      "repair or reflash the SD/USB boot medium before CAN probing",
			},
			{
				Name:        "SPI enabled",
				Command:     "ls -l /dev/spidev0.*",
				Expect:      "spidev0.0 and usually spidev0.1 exist",
				FailureMode: "MCP2515 overlay cannot bind without SPI",
				Remedy:      "ensure dtparam=spi=on is present in /boot/config.txt",
			},
			{
				Name:        "PiXtend overlay loaded",
				Command:     "dtoverlay -l",
				Expect:      "pixtendv2l is active; analog-output/CAN jumper is in CAN position",
				FailureMode: "PiXtend CAN and analog outputs share hardware resources",
				Remedy:      "verify the hardware jumper and keep analog output disabled while using CAN",
			},
			{
				Name:        "MCP2515 runtime overlay",
				Command:     "dtoverlay mcp2515-can0 oscillator=16000000 interrupt=25 spimaxfrequency=1000000",
				Expect:      "one of mcp2515-can0 or mcp2515-can1 creates can0/can1",
				FailureMode: "wrong chip select, wrong interrupt, missing power, or wrong jumper",
				Remedy:      "try CE0 then CE1; inspect dmesg for mcp251x err=110",
			},
			{
				Name:        "bounded PiXtend service probes",
				Command:     "timeout 2s pixtendsrv2",
				Expect:      "never run pixtendsrv2 unbounded during CAN debugging",
				FailureMode: "CRC spam can starve SSH and hide CAN evidence",
				Remedy:      "capture short logs only; stop immediately on CRC header spam",
			},
		},
		Notes: []string{
			"Use SocketCAN once the MCP2515 netdevice exists; keep packet I/O deterministic and single-owner.",
			"Probe bitrates from highest expected TEC rate downward, but keep each candump window bounded.",
		},
	}
}

func socketCANGenericProfile() Profile {
	return Profile{
		ID:              ProfileSocketCANGeneric,
		DisplayName:     "Generic Linux SocketCAN netdevice",
		Maturity:        MaturityEasyUnproven,
		Transport:       TransportSocketCAN,
		InterfaceHints:  []string{"can0"},
		DefaultBitrates: []int{1000000, 500000, 250000, 125000},
		Capabilities:    []Capability{CapabilitySocketCAN, CapabilityPassiveMonitor},
		Checks: []Check{
			{Name: "netdevice exists", Command: "ip -details link show can0", Expect: "interface type is can"},
			{Name: "interface up", Command: "ip link set can0 up type can bitrate <bitrate>", Expect: "state is UP or UNKNOWN, not BUS-OFF"},
		},
	}
}

func candleLightProfile() Profile {
	return Profile{
		ID:              ProfileCandleLight,
		DisplayName:     "CANable/candleLight/gs_usb USB adapter",
		Maturity:        MaturityEasyUnproven,
		Transport:       TransportSocketCAN,
		SocketCANDriver: []string{"gs_usb"},
		InterfaceHints:  []string{"can0"},
		DefaultBitrates: []int{1000000, 500000, 250000, 125000},
		Capabilities:    []Capability{CapabilitySocketCAN, CapabilityPassiveMonitor},
		DriverRefs: []DriverRef{{
			Name: "Linux SocketCAN gs_usb",
			URL:  "https://cateee.net/lkddb/web-lkddb/CAN_GS_USB.html",
		}},
		Checks: []Check{
			{Name: "driver loaded", Command: "dmesg | grep -i gs_usb", Expect: "adapter binds to gs_usb and creates canX"},
			{Name: "bus state", Command: "ip -details -statistics link show can0", Expect: "not bus-off; rx/tx error counters stable"},
		},
		Notes: []string{"Many CANable clones need candleLight firmware for native SocketCAN."},
	}
}

func pcanUSBProfile() Profile {
	return Profile{
		ID:              ProfilePCANUSB,
		DisplayName:     "PEAK PCAN-USB SocketCAN adapter",
		Maturity:        MaturityEasyUnproven,
		Transport:       TransportSocketCAN,
		SocketCANDriver: []string{"peak_usb"},
		InterfaceHints:  []string{"can0"},
		DefaultBitrates: []int{1000000, 500000, 250000, 125000},
		Capabilities:    []Capability{CapabilitySocketCAN, CapabilityPassiveMonitor, CapabilityLocalTimestamp},
		DriverRefs: []DriverRef{{
			Name: "Linux SocketCAN peak_usb",
			URL:  "https://cateee.net/lkddb/web-lkddb/CAN_PEAK_USB.html",
		}},
		Checks: []Check{
			{Name: "driver loaded", Command: "dmesg | grep -i peak_usb", Expect: "adapter binds to peak_usb and creates canX"},
			{Name: "SocketCAN status", Command: "ip -details -statistics link show can0", Expect: "state is not BUS-OFF; error counters not climbing"},
		},
	}
}

func emsUSBProfile() Profile {
	return easySocketCANUSBProfile(ProfileEMSUSB, "EMS CPC-USB/ARM7 SocketCAN adapter", "ems_usb", "https://www.kernelconfig.io/CONFIG_CAN_EMS_USB")
}

func esdUSBProfile() Profile {
	return easySocketCANUSBProfile(ProfileESDUSB, "esd electronics CAN-USB SocketCAN adapter", "esd_usb", "https://www.kernelconfig.io/config_can_esd_usb")
}

func microchipCANProfile() Profile {
	return easySocketCANUSBProfile(ProfileMicrochipCAN, "Microchip CAN BUS Analyzer SocketCAN adapter", "mcba_usb", "https://cateee.net/lkddb/web-lkddb/CAN_MCBA_USB.html")
}

func can327Profile() Profile {
	return Profile{
		ID:              ProfileCAN327,
		DisplayName:     "ELM327 CAN serial adapter through can327",
		Maturity:        MaturityEasyUnproven,
		Transport:       TransportSocketCAN,
		SocketCANDriver: []string{"can327"},
		InterfaceHints:  []string{"can0"},
		DefaultBitrates: []int{500000, 250000, 125000},
		Capabilities:    []Capability{CapabilitySocketCAN},
		DriverRefs: []DriverRef{{
			Name: "Linux SocketCAN can327",
			URL:  "https://www.kernel.org/doc/html/latest/networking/device_drivers/can/can327.html",
		}},
		Checks: []Check{
			{Name: "attach line discipline", Command: "ldattach --debug can327 /dev/ttyUSB0", Expect: "can327 creates a CAN netdevice"},
			{Name: "performance gate", Command: "bounded candump", Expect: "acceptable only for diagnostics or low-rate fallback"},
		},
		Notes: []string{"Treat ELM327/can327 as an emergency diagnostic path, not a deterministic TEC polling adapter."},
	}
}

func kvaserUSBProfile() Profile {
	return Profile{
		ID:              ProfileKvaserUSB,
		DisplayName:     "Kvaser USB adapter",
		FirstClass:      true,
		Maturity:        MaturityFirstClass,
		Transport:       TransportKvaserCANlib,
		SocketCANDriver: []string{"kvaser_usb"},
		InterfaceHints:  []string{"can0", "CANlib channel"},
		DefaultBitrates: []int{1000000, 500000, 250000, 125000},
		Capabilities:    []Capability{CapabilityPassiveMonitor, CapabilitySharedOpen, CapabilityLocalTimestamp},
		DriverRefs: []DriverRef{
			{Name: "Kvaser CANlib"},
			{Name: "Linux SocketCAN kvaser_usb", URL: "https://cateee.net/lkddb/web-lkddb/CAN_KVASER_USB.html"},
		},
		Checks: []Check{
			{Name: "ownership", Command: "canEnumHardwareEx or SocketCAN ip link", Expect: "channel is visible and not exclusively owned elsewhere"},
			{Name: "silent monitor", Command: "open shared read handle in silent mode", Expect: "observer does not perturb the bus"},
		},
		Notes: []string{
			"Use CANlib for official Windows/Kvaser ownership semantics; SocketCAN kvaser_usb is acceptable on Linux when channel ownership is simple.",
			"Host-only USB adapters do not provide honest device-local script/offload behavior.",
		},
	}
}

func kvaserEthernetProfile() Profile {
	return Profile{
		ID:              ProfileKvaserEthernet,
		DisplayName:     "Kvaser DIN-rail/Ethernet adapter",
		FirstClass:      true,
		Maturity:        MaturityFirstClass,
		Transport:       TransportKvaserCANlib,
		InterfaceHints:  []string{"kvrlib stored device", "CANlib channel"},
		DefaultBitrates: []int{1000000, 500000, 250000, 125000},
		Capabilities: []Capability{
			CapabilityPassiveMonitor,
			CapabilitySharedOpen,
			CapabilityLocalTimestamp,
			CapabilityObjectBuffers,
			CapabilityEdgeScript,
		},
		DriverRefs: []DriverRef{{Name: "Kvaser CANlib/kvrlib"}},
		Checks: []Check{
			{Name: "remote discovery", Command: "kvrlib discovery/stored-device attach", Expect: "device is reachable and bound to the intended host"},
			{Name: "owner clear", Command: "deviceStatus / official Kvaser tools", Expect: "no stale owner/session blocks the channel"},
			{Name: "same-process visibility", Command: "kvrlib attach then canEnumHardwareEx in same process", Expect: "remote channels appear in the owner process"},
			{Name: "edge capability", Command: "object-buffer or kvScript capability probe", Expect: "offload is used only after passive capture and host ownership are proven"},
		},
		Notes: []string{
			"Keep remote discovery, stored-device binding, and channel open in the same long-lived process.",
			"Use object buffers before richer kvScript; prove passive capture before mutation.",
		},
	}
}

func slcanUSBProfile() Profile {
	return Profile{
		ID:              ProfileSLCANUSB,
		DisplayName:     "Lawicel/SLCAN USB serial adapter",
		Maturity:        MaturityEasyUnproven,
		Transport:       TransportSLCAN,
		InterfaceHints:  []string{"slcan0"},
		DefaultBitrates: []int{1000000, 500000, 250000, 125000},
		Capabilities:    []Capability{CapabilitySocketCAN},
		DriverRefs: []DriverRef{{
			Name: "Linux SLCAN line discipline",
			URL:  "https://www.kernel.org/doc/html/latest/networking/can.html",
		}},
		Checks: []Check{
			{Name: "attach serial line discipline", Command: "slcand -o -c -s8 /dev/ttyACM0 slcan0", Expect: "slcan0 netdevice exists"},
			{Name: "latency budget", Command: "ip -details link show slcan0", Expect: "acceptable only for diagnostics or slow buses"},
		},
		Notes: []string{"Prefer native SocketCAN USB adapters over SLCAN for deterministic TEC polling."},
	}
}

func remoteSocketCANProfile() Profile {
	return Profile{
		ID:              ProfileRemoteSocketCAN,
		DisplayName:     "Remote SocketCAN bridge",
		Maturity:        MaturityEasyUnproven,
		Transport:       TransportRemoteBridge,
		InterfaceHints:  []string{"tcp://host:port", "nats subject", "ssh remote canX"},
		DefaultBitrates: []int{1000000, 500000, 250000, 125000},
		Capabilities:    []Capability{CapabilityPassiveMonitor},
		Checks: []Check{
			{Name: "remote owner", Command: "remote ip -details link show can0", Expect: "one node owns bus-on/off and bitrate"},
			{Name: "raw truth", Command: "bounded remote candump", Expect: "raw frames preserved before derived reduction"},
		},
		Notes: []string{"Keep bus configuration on the node physically attached to the CAN adapter."},
	}
}

func easySocketCANUSBProfile(id, display, driver, url string) Profile {
	return Profile{
		ID:              id,
		DisplayName:     display,
		Maturity:        MaturityEasyUnproven,
		Transport:       TransportSocketCAN,
		SocketCANDriver: []string{driver},
		InterfaceHints:  []string{"can0"},
		DefaultBitrates: []int{1000000, 500000, 250000, 125000},
		Capabilities:    []Capability{CapabilitySocketCAN, CapabilityPassiveMonitor},
		DriverRefs:      []DriverRef{{Name: "Linux SocketCAN " + driver, URL: url}},
		Checks: []Check{
			{Name: "driver loaded", Command: "dmesg | grep -i " + driver, Expect: "adapter binds to " + driver + " and creates canX"},
			{Name: "bus state", Command: "ip -details -statistics link show can0", Expect: "not bus-off; rx/tx error counters stable"},
		},
		Notes: []string{"Easy adapter option because it should surface as SocketCAN; not yet proven on this bench."},
	}
}

func formatInts(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = fmt.Sprintf("%d", value)
	}
	return strings.Join(parts, ",")
}
