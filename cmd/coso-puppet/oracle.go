package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type coSoOracle struct {
	SchemaVersion string                  `json:"schema_version"`
	ID            string                  `json:"id"`
	SourcePolicy  string                  `json:"source_policy"`
	Device        coSoOracleDevice        `json:"device"`
	Transport     coSoOracleTransport     `json:"transport"`
	ServerErrors  []coSoOracleServerError `json:"server_errors"`
	Phases        []coSoOraclePhase       `json:"phases"`
	PublicSafety  coSoOraclePublicSafety  `json:"public_safety"`
}

type coSoOracleDevice struct {
	Family         string `json:"family"`
	Definition     string `json:"definition"`
	IdentityString string `json:"identity_string"`
	FirmwareMin    string `json:"firmware_min"`
	FirmwareMax    string `json:"firmware_max"`
}

type coSoOracleTransport struct {
	SingleFlight                 bool   `json:"single_flight"`
	TimeoutRetries               int    `json:"timeout_retries"`
	BroadcastAddress             int    `json:"broadcast_address"`
	SequenceAddressMatchRequired bool   `json:"sequence_address_match_required"`
	ServerErrorPrefix            string `json:"server_error_prefix"`
	AckControl                   string `json:"ack_control"`
	RequestControl               string `json:"request_control"`
	FrameTerminator              string `json:"frame_terminator"`
}

type coSoOracleServerError struct {
	Code   int    `json:"code"`
	Name   string `json:"name"`
	Action string `json:"action"`
}

type coSoOraclePhase struct {
	Name                     string                   `json:"name"`
	Order                    int                      `json:"order"`
	Required                 bool                     `json:"required"`
	Description              string                   `json:"description"`
	UpdateIntervalMS         int                      `json:"update_interval_ms,omitempty"`
	Commands                 []coSoOracleCommand      `json:"commands"`
	DefaultCaptureParameters []coSoOracleParameterRef `json:"default_capture_parameters,omitempty"`
}

type coSoOracleCommand struct {
	Command              string                `json:"command"`
	Direction            string                `json:"direction,omitempty"`
	Optional             bool                  `json:"optional,omitempty"`
	FailureAction        string                `json:"failure_action,omitempty"`
	FallbackFor          string                `json:"fallback_for,omitempty"`
	ParameterID          int                   `json:"parameter_id,omitempty"`
	Instance             int                   `json:"instance,omitempty"`
	ValueType            string                `json:"value_type,omitempty"`
	Request              coSoOracleRequest     `json:"request,omitempty"`
	Response             coSoOracleResponse    `json:"response,omitempty"`
	RequestFields        []string              `json:"request_fields,omitempty"`
	ResponseFields       []string              `json:"response_fields,omitempty"`
	Foreach              string                `json:"foreach,omitempty"`
	UnavailableErrorCode int                   `json:"unavailable_error_code,omitempty"`
	UnavailableAction    string                `json:"unavailable_action,omitempty"`
	PreserveRequestOrder bool                  `json:"preserve_request_order,omitempty"`
	SampleParameters     []coSoOracleParameter `json:"sample_parameters,omitempty"`
	When                 string                `json:"when,omitempty"`
	CompatibilityOnly    bool                  `json:"compatibility_only,omitempty"`
	SafetyPolicy         string                `json:"safety_policy,omitempty"`
	Role                 string                `json:"role,omitempty"`
}

type coSoOracleParameter struct {
	ParameterID   int               `json:"parameter_id"`
	Instance      int               `json:"instance"`
	ValueType     string            `json:"value_type,omitempty"`
	CacheBehavior string            `json:"cache_behavior,omitempty"`
	Support       string            `json:"support,omitempty"`
	Writable      bool              `json:"writable,omitempty"`
	Request       coSoOracleRequest `json:"request,omitempty"`
}

type coSoOracleParameterRef struct {
	ParameterID int `json:"parameter_id"`
	Instance    int `json:"instance"`
}

type coSoOracleRequest struct {
	Payload string `json:"payload,omitempty"`
}

type coSoOracleResponse struct {
	Fields []string `json:"fields,omitempty"`
}

type coSoOraclePublicSafety struct {
	ForbiddenTokens      []string `json:"forbidden_tokens"`
	AllowedEvidenceKinds []string `json:"allowed_evidence_kinds"`
}

func runOracle(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("oracle", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("file", "", "public-safe CoSo oracle JSON path")
	requests := fs.Bool("requests", false, "print derived request payload smoke sequence")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" {
		fmt.Fprintln(stderr, "coso-puppet oracle: -file is required")
		return 2
	}
	oracle, err := loadCoSoOracle(*path)
	if err != nil {
		fmt.Fprintf(stderr, "coso-puppet oracle: %v\n", err)
		return 1
	}
	if err := validateCoSoOracle(oracle); err != nil {
		fmt.Fprintf(stderr, "coso-puppet oracle: %v\n", err)
		return 1
	}
	writeCoSoOracleReport(stdout, oracle, *requests)
	return 0
}

func loadCoSoOracle(path string) (coSoOracle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return coSoOracle{}, err
	}
	var oracle coSoOracle
	if err := json.Unmarshal(data, &oracle); err != nil {
		return coSoOracle{}, err
	}
	return oracle, nil
}

func validateCoSoOracle(oracle coSoOracle) error {
	if oracle.SchemaVersion != "coso_connection_oracle.v1" {
		return fmt.Errorf("unsupported oracle schema %q", oracle.SchemaVersion)
	}
	if strings.TrimSpace(oracle.ID) == "" {
		return errors.New("oracle id is required")
	}
	if strings.TrimSpace(oracle.Device.Definition) == "" {
		return errors.New("device definition is required")
	}
	if !oracle.Transport.SingleFlight {
		return errors.New("transport must be single-flight")
	}
	if oracle.Transport.TimeoutRetries <= 0 {
		return errors.New("transport timeout retries must be positive")
	}
	if len(oracle.Phases) == 0 {
		return errors.New("at least one connection phase is required")
	}
	lastOrder := 0
	phaseNames := map[string]struct{}{}
	for _, phase := range oracle.Phases {
		if strings.TrimSpace(phase.Name) == "" {
			return errors.New("phase name is required")
		}
		if _, ok := phaseNames[phase.Name]; ok {
			return fmt.Errorf("duplicate phase %q", phase.Name)
		}
		phaseNames[phase.Name] = struct{}{}
		if phase.Order <= lastOrder {
			return fmt.Errorf("phase %q order %d is not strictly increasing", phase.Name, phase.Order)
		}
		lastOrder = phase.Order
		if len(phase.Commands) == 0 {
			return fmt.Errorf("phase %q has no commands", phase.Name)
		}
		for _, cmd := range phase.Commands {
			if strings.TrimSpace(cmd.Command) == "" {
				return fmt.Errorf("phase %q has a command without a name", phase.Name)
			}
			if cmd.ParameterID < 0 || cmd.Instance < 0 {
				return fmt.Errorf("phase %q command %q has a negative parameter reference", phase.Name, cmd.Command)
			}
			for _, sample := range cmd.SampleParameters {
				if sample.ParameterID <= 0 {
					return fmt.Errorf("phase %q command %q has invalid sample parameter id %d", phase.Name, cmd.Command, sample.ParameterID)
				}
				if sample.Instance <= 0 {
					return fmt.Errorf("phase %q command %q has invalid sample instance %d", phase.Name, cmd.Command, sample.Instance)
				}
			}
		}
	}
	return validateCoSoOraclePublicSafety(oracle)
}

func validateCoSoOraclePublicSafety(oracle coSoOracle) error {
	tokens := oracle.PublicSafety.ForbiddenTokens
	if len(tokens) == 0 {
		tokens = []string{"C:\\", "/home/", "Trace.txt", "CL25", "OneDrive", "Downloads", "serial_number", "password", "custom_lock"}
	}
	s, err := coSoOracleSearchableText(oracle)
	if err != nil {
		return err
	}
	haystacks := []string{
		strings.ToLower(s),
		strings.ToLower(strings.ReplaceAll(s, `\\`, `\`)),
	}
	for _, token := range tokens {
		for _, variant := range tokenVariants(token) {
			if variant == "" {
				continue
			}
			for _, haystack := range haystacks {
				if strings.Contains(haystack, variant) {
					return fmt.Errorf("forbidden token %q found in oracle", token)
				}
			}
		}
	}
	return nil
}

func coSoOracleRequestPayloads(oracle coSoOracle) []string {
	var payloads []string
	for _, phase := range oracle.Phases {
		for _, cmd := range phase.Commands {
			if cmd.Request.Payload != "" {
				payloads = append(payloads, cmd.Request.Payload)
			}
			for _, sample := range cmd.SampleParameters {
				if sample.Request.Payload != "" {
					payloads = append(payloads, sample.Request.Payload)
				}
			}
		}
	}
	return payloads
}

func writeCoSoOracleReport(w io.Writer, oracle coSoOracle, includeRequests bool) {
	fmt.Fprintf(w, "oracle=%s schema=%s phases=%d\n", oracle.ID, oracle.SchemaVersion, len(oracle.Phases))
	fmt.Fprintf(w, "device=%s identity=%q\n", oracle.Device.Definition, oracle.Device.IdentityString)
	fmt.Fprintf(w, "transport=single_flight=%t retries=%d broadcast=%d sequence_address_match=%t\n",
		oracle.Transport.SingleFlight,
		oracle.Transport.TimeoutRetries,
		oracle.Transport.BroadcastAddress,
		oracle.Transport.SequenceAddressMatchRequired,
	)
	for _, phase := range oracle.Phases {
		fmt.Fprintf(w, "phase[%d]=%s commands=%s\n", phase.Order, phase.Name, phaseCommandNames(phase))
	}
	if includeRequests {
		fmt.Fprintln(w, "request_payloads")
		for _, payload := range coSoOracleRequestPayloads(oracle) {
			fmt.Fprintln(w, payload)
		}
	}
}

func phaseCommandNames(phase coSoOraclePhase) string {
	names := make([]string, 0, len(phase.Commands))
	for _, cmd := range phase.Commands {
		names = append(names, cmd.Command)
	}
	return strings.Join(names, ",")
}

func coSoOracleSearchableText(oracle coSoOracle) (string, error) {
	oracle.PublicSafety = coSoOraclePublicSafety{}
	data, err := json.Marshal(oracle)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func tokenVariants(token string) []string {
	lower := strings.ToLower(token)
	return []string{
		lower,
		strings.ReplaceAll(lower, `\\`, `\`),
	}
}
