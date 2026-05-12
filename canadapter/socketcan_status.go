package canadapter

import (
	"fmt"
	"strconv"
	"strings"
)

// Severity is the operator impact of a validation finding.
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// Finding is a bounded, operator-facing diagnosis from adapter evidence.
type Finding struct {
	Severity Severity
	Message  string
	Remedy   string
}

// SocketCANStatus is the subset of `ip -details link show canX` that matters
// for deterministic TEC polling.
type SocketCANStatus struct {
	Interface  string
	Type       string
	OperState  string
	CANState   string
	Mode       string
	Bitrate    int
	RestartMS  int
	Driver     string
	TxQueueLen int
}

// ParseSocketCANStatus parses the text emitted by:
//
//	ip -details -statistics link show can0
//
// It accepts partial output so callers can still report useful findings from
// constrained remote probes.
func ParseSocketCANStatus(output string) (SocketCANStatus, error) {
	var status SocketCANStatus
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if isIPLinkHeader(fields[0]) && len(fields) >= 2 {
			status.Interface = strings.TrimSuffix(fields[1], ":")
			status.OperState = tokenAfter(fields, "state")
			status.Mode = tokenAfter(fields, "mode")
			status.TxQueueLen = atoiDefault(tokenAfter(fields, "qlen"))
			continue
		}
		if fields[0] == "link/can" {
			status.Type = "can"
			continue
		}
		if fields[0] == "can" {
			status.Type = "can"
			if len(fields) > 2 && fields[1] == "state" {
				status.CANState = fields[2]
			}
			if value := tokenAfter(fields, "restart-ms"); value != "" {
				status.RestartMS = atoiDefault(value)
			}
			continue
		}
		if fields[0] == "bitrate" && len(fields) > 1 {
			status.Bitrate = atoiDefault(fields[1])
			continue
		}
		if strings.HasSuffix(fields[0], ":") && strings.Contains(line, "tseg1") {
			status.Driver = strings.TrimSuffix(fields[0], ":")
			continue
		}
	}
	if status.Interface == "" {
		return status, fmt.Errorf("socketcan status: interface line not found")
	}
	return status, nil
}

// ValidateSocketCANStatus checks parsed netdevice evidence against an adapter
// profile. It is intentionally advisory; bus traffic remains the final proof.
func ValidateSocketCANStatus(status SocketCANStatus, profile Profile) []Finding {
	var findings []Finding
	if status.Type != "can" {
		findings = append(findings, Finding{
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s is not reported as a CAN netdevice", status.Interface),
			Remedy:   "verify the adapter driver and create the CAN netdevice before MeCom probing",
		})
	}
	if status.CANState == "BUS-OFF" || status.OperState == "DOWN" {
		findings = append(findings, Finding{
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s is not usable: oper=%s can=%s", status.Interface, status.OperState, status.CANState),
			Remedy:   "check wiring, termination, bitrate, and bring the interface up again",
		})
	}
	if status.Bitrate == 0 {
		findings = append(findings, Finding{
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("%s has no parsed bitrate", status.Interface),
			Remedy:   "run `ip -details link show` after setting the CAN bitrate",
		})
	} else if len(profile.DefaultBitrates) > 0 && !containsInt(profile.DefaultBitrates, status.Bitrate) {
		findings = append(findings, Finding{
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("%s bitrate %d is outside profile defaults", status.Interface, status.Bitrate),
			Remedy:   "confirm the TEC bus rate before optimizing polling cadence",
		})
	}
	if status.Driver != "" && len(profile.SocketCANDriver) > 0 && !containsStringFold(profile.SocketCANDriver, status.Driver) {
		findings = append(findings, Finding{
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("%s driver %s does not match %s profile drivers", status.Interface, status.Driver, profile.ID),
			Remedy:   "select the correct adapter profile or re-check the hardware path",
		})
	}
	if len(findings) == 0 {
		findings = append(findings, Finding{
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("%s matches %s adapter expectations", status.Interface, profile.ID),
		})
	}
	return findings
}

func tokenAfter(fields []string, token string) string {
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == token {
			return fields[i+1]
		}
	}
	return ""
}

func isIPLinkHeader(token string) bool {
	token = strings.TrimSuffix(token, ":")
	_, err := strconv.Atoi(token)
	return err == nil
}

func atoiDefault(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}

func containsInt(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func containsStringFold(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}
