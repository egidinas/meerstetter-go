package mecom

// CommandSource identifies where a command definition was proven.
type CommandSource string

const (
	CommandSourceRMM1182ControllerSoftware CommandSource = "rmm_1182_controller_software"
)

// CommandDirection describes the request/reply behavior visible at the MeCom
// command layer.
type CommandDirection string

const (
	CommandDirectionHostToDevice    CommandDirection = "host_to_device"
	CommandDirectionRequestResponse CommandDirection = "request_response"
)

// CommandConfidence records how directly the command was proven from sources.
type CommandConfidence string

const (
	CommandConfidenceHigh   CommandConfidence = "high"
	CommandConfidenceMedium CommandConfidence = "medium"
)

// CommandOperation names the stable semantic operation behind a MeCom token.
type CommandOperation string

const (
	CommandOperationResetDevice           CommandOperation = "reset_device"
	CommandOperationSaveToFlash           CommandOperation = "save_to_flash"
	CommandOperationSetDeviceAddress      CommandOperation = "set_device_address"
	CommandOperationReadBranchID          CommandOperation = "read_branch_id"
	CommandOperationReadFirmwareVersion   CommandOperation = "read_firmware_version"
	CommandOperationReadParameterValue    CommandOperation = "read_parameter_value"
	CommandOperationWriteParameterValue   CommandOperation = "write_parameter_value"
	CommandOperationReadParameterLimits   CommandOperation = "read_parameter_limits"
	CommandOperationReadParameterMetadata CommandOperation = "read_parameter_metadata"
	CommandOperationBulkReadParameters    CommandOperation = "bulk_read_parameters"
	CommandOperationDownloadMeBlob        CommandOperation = "download_meblob"
	CommandOperationSendBootloaderCommand CommandOperation = "send_bootloader_command"
)

// CommandDefinition describes one MeCom command token. It is protocol metadata,
// not an assertion that every device family supports every command.
type CommandDefinition struct {
	Token        string
	Operation    CommandOperation
	Direction    CommandDirection
	PayloadShape string
	Confidence   CommandConfidence
	Source       CommandSource
	Notes        string
}

var defaultCommandInventory = []CommandDefinition{
	{
		Token:        "RS",
		Operation:    CommandOperationResetDevice,
		Direction:    CommandDirectionHostToDevice,
		PayloadShape: "RS",
		Confidence:   CommandConfidenceHigh,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "Proven from MeSoft.MeCom.Core ResetDevice.",
	},
	{
		Token:        "SP",
		Operation:    CommandOperationSaveToFlash,
		Direction:    CommandDirectionHostToDevice,
		PayloadShape: "SP",
		Confidence:   CommandConfidenceHigh,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "Proven from MeSoft.MeCom.Core TriggerParameterSaveToFlash.",
	},
	{
		Token:        "SA",
		Operation:    CommandOperationSetDeviceAddress,
		Direction:    CommandDirectionHostToDevice,
		PayloadShape: "SA<device-type><serial><address>",
		Confidence:   CommandConfidenceMedium,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "SetDeviceAddress proves the command token; exact field order still needs IL or capture validation.",
	},
	{
		Token:        "?BI",
		Operation:    CommandOperationReadBranchID,
		Direction:    CommandDirectionRequestResponse,
		PayloadShape: "?BI",
		Confidence:   CommandConfidenceHigh,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "Proven from GetBranchId and GetBranchIdSeSo.",
	},
	{
		Token:        "?VI",
		Operation:    CommandOperationReadFirmwareVersion,
		Direction:    CommandDirectionRequestResponse,
		PayloadShape: "?VI",
		Confidence:   CommandConfidenceHigh,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "Proven from GetFirmwareVersionInfo.",
	},
	{
		Token:        "?VR",
		Operation:    CommandOperationReadParameterValue,
		Direction:    CommandDirectionRequestResponse,
		PayloadShape: "?VR<param-id><instance>",
		Confidence:   CommandConfidenceHigh,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "Proven from typed and generic parameter read helpers.",
	},
	{
		Token:        "VS",
		Operation:    CommandOperationWriteParameterValue,
		Direction:    CommandDirectionHostToDevice,
		PayloadShape: "VS<param-id><instance><encoded-value>",
		Confidence:   CommandConfidenceHigh,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "Proven from typed and generic parameter write helpers.",
	},
	{
		Token:        "?VL",
		Operation:    CommandOperationReadParameterLimits,
		Direction:    CommandDirectionRequestResponse,
		PayloadShape: "?VL<param-id><instance>",
		Confidence:   CommandConfidenceHigh,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "Proven from GetLimits.",
	},
	{
		Token:        "?VM",
		Operation:    CommandOperationReadParameterMetadata,
		Direction:    CommandDirectionRequestResponse,
		PayloadShape: "?VM<param-id><instance>",
		Confidence:   CommandConfidenceHigh,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "Proven from GetMetaData.",
	},
	{
		Token:        "?VX",
		Operation:    CommandOperationBulkReadParameters,
		Direction:    CommandDirectionRequestResponse,
		PayloadShape: "?VX<count><param-id><instance>...",
		Confidence:   CommandConfidenceHigh,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "Proven from BulkParReadCom.",
	},
	{
		Token:        "?MB",
		Operation:    CommandOperationDownloadMeBlob,
		Direction:    CommandDirectionRequestResponse,
		PayloadShape: "?MB<blob-selector/status-fields>",
		Confidence:   CommandConfidenceHigh,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "DownloadMeBlob proves the command token; exact payload details still need deeper decode.",
	},
	{
		Token:        "?BC",
		Operation:    CommandOperationSendBootloaderCommand,
		Direction:    CommandDirectionRequestResponse,
		PayloadShape: "?BC<command-payload>",
		Confidence:   CommandConfidenceMedium,
		Source:       CommandSourceRMM1182ControllerSoftware,
		Notes:        "Proven from SendCommand; exact RMM use is not proven.",
	},
}

// DefaultCommandInventory returns the MeCom command definitions currently
// proven from public-safe controller software evidence.
func DefaultCommandInventory() []CommandDefinition {
	out := make([]CommandDefinition, len(defaultCommandInventory))
	copy(out, defaultCommandInventory)
	return out
}

// CommandDefinitionByToken looks up one command definition by exact token.
func CommandDefinitionByToken(token string) (CommandDefinition, bool) {
	for _, command := range defaultCommandInventory {
		if command.Token == token {
			return command, true
		}
	}
	return CommandDefinition{}, false
}
