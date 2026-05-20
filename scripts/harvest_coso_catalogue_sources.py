#!/usr/bin/env python3
"""Harvest public TEC catalogue evidence from the Meerstetter CoSo package.

The generated JSON files are source artefacts for the MeCom TEC catalogue. They
keep official defaults, CANopen object names, recovered help text, and hidden or
manufacturer-oriented candidates separate from the active runtime catalogue.
"""

from __future__ import annotations

import argparse
import json
import re
import zipfile
from collections import OrderedDict
from pathlib import Path
from typing import Any, Dict, Iterable, List, MutableMapping, Optional, Tuple
from xml.etree import ElementTree


DEFAULT_CONFIG_NAME = "TEC Default Config 5216O.xml"
EDS_NAME = "CanOpen.eds"


def source_ref(source_id: str, detail: str = "") -> str:
    if detail:
        return f"{source_id}:{detail}"
    return source_id


def compact_help(text: str, limit: int = 320) -> str:
    text = " ".join(text.replace("\r", " ").replace("\n", " ").split())
    if len(text) <= limit:
        return text
    return text[: limit - 1].rstrip() + "."


def parse_scalar(text: str) -> Tuple[Any, str]:
    stripped = text.strip()
    if stripped == "":
        return "", "empty"
    try:
        if re.fullmatch(r"[-+]?\d+", stripped):
            return int(stripped), "integer"
        if re.fullmatch(r"[-+]?(?:\d+\.\d*|\d*\.\d+)(?:[eE][-+]?\d+)?", stripped):
            return float(stripped), "number"
    except ValueError:
        pass
    return stripped, "text"


def normalize_default_path(mepar_id: int, path: str) -> str:
    replacements = {
        2030: ("Maximum Current", "Output Current Limit"),
        2031: ("Maximum Voltage", "Output Voltage Limit"),
    }
    if mepar_id in replacements:
        old, new = replacements[mepar_id]
        path = path.replace(old, new)
    return path


def default_safety_note(group: str, visibility: str, access: str) -> str:
    access = access.lower()
    visibility = visibility.lower()
    group = group.lower()
    is_writeable = "write" in access

    if visibility == "manufacturer":
        if is_writeable:
            return "Manufacturer-level setting; keep out of routine operator write controls unless separately reviewed."
        return "Manufacturer-level diagnostic or identity value; surface as read-only metadata unless separately reviewed."
    if visibility == "license":
        if is_writeable:
            return "Vendor license payload; do not expose as a routine operator write."
        return "Vendor license status metadata; surface read-only with provenance."
    if "firmware" in group:
        if is_writeable:
            return "Firmware-update control; keep disabled in normal operator surfaces."
        return "Firmware-update diagnostic value; surface read-only outside dedicated maintenance workflows."
    if visibility == "advanced":
        if is_writeable:
            return "Advanced writable setting; require explicit operator intent, command logging, and confirmed readback before changing."
        return "Advanced diagnostic value; hide by default and surface with provenance when needed."
    if is_writeable:
        if group in {"control", "cascade", "pid controller", "thermal model", "heat/cool only", "sensor input"}:
            return "Writable control parameter; log intended value, transport route, actual write result, and confirmed readback."
        return "Writable parameter; require explicit operator confirmation and confirmed readback."
    return ""


def harvest_default_config(package: zipfile.ZipFile) -> OrderedDict:
    raw = package.read(DEFAULT_CONFIG_NAME).decode("utf-8-sig")
    raw = raw.replace("</Parameter>>", "</Parameter>")
    root = ElementTree.fromstring(raw)

    info = OrderedDict()
    info_node = root.find("Info")
    if info_node is not None:
        for child in list(info_node):
            info[child.tag] = (child.text or "").strip()

    parameters: "OrderedDict[str, OrderedDict[str, Any]]" = OrderedDict()
    for node in root.findall(".//Parameter"):
        mepar_id = int(node.attrib["MeParID"])
        mepar_inst = int(node.attrib["MeParInst"])
        raw_path = node.attrib.get("Name", "").strip()
        path = normalize_default_path(mepar_id, raw_path)
        value_text = (node.text or "").strip()
        value, value_kind = parse_scalar(value_text)
        key = str(mepar_id)
        param = parameters.setdefault(
            key,
            OrderedDict(
                [
                    ("mepar_id", mepar_id),
                    ("instance_count", 0),
                    ("instances", OrderedDict()),
                ]
            ),
        )
        instance = OrderedDict(
            [
                ("mepar_inst", mepar_inst),
                ("default_value", value),
                ("default_value_text", value_text),
                ("value_kind", value_kind),
                ("path", path),
                ("tree", [part.strip() for part in path.split("/") if part.strip()]),
            ]
        )
        if raw_path != path:
            instance["source_path"] = raw_path
        param["instances"][str(mepar_inst)] = instance

    for param in parameters.values():
        param["instance_count"] = len(param["instances"])

    return OrderedDict(
        [
            ("schema_version", "mecom_tec_coso_defaults.v1"),
            ("source", DEFAULT_CONFIG_NAME),
            ("info", info),
            ("parameters", parameters),
        ]
    )


def parse_ini_sections(text: str) -> "OrderedDict[str, OrderedDict[str, str]]":
    sections: "OrderedDict[str, OrderedDict[str, str]]" = OrderedDict()
    current: Optional[str] = None
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith(";"):
            continue
        if line.startswith("[") and line.endswith("]"):
            current = line[1:-1]
            sections.setdefault(current, OrderedDict())
            continue
        if current is None or "=" not in line:
            continue
        key, value = line.split("=", 1)
        sections[current][key.strip()] = value.strip()
    return sections


def harvest_eds(package: zipfile.ZipFile) -> OrderedDict:
    text = package.read(EDS_NAME).decode("utf-8-sig", errors="replace")
    sections = parse_ini_sections(text)
    objects: "OrderedDict[str, OrderedDict[str, Any]]" = OrderedDict()

    for section, values in sections.items():
        match = re.fullmatch(r"([0-9A-Fa-f]{4})(?:sub([0-9A-Fa-f]+))?", section)
        if not match:
            continue
        index, subindex = match.groups()
        index = index.upper()
        if subindex is None:
            objects[index] = OrderedDict(
                [
                    ("index", index),
                    ("parameter_name", values.get("ParameterName", "")),
                    ("object_type", values.get("ObjectType", "")),
                    ("access_type", values.get("AccessType", "")),
                    ("data_type", values.get("DataType", "")),
                    ("pdo_mapping", values.get("PDOMapping", "")),
                    ("subobjects", OrderedDict()),
                ]
            )
            continue
        obj = objects.setdefault(
            index,
            OrderedDict(
                [
                    ("index", index),
                    ("parameter_name", ""),
                    ("object_type", ""),
                    ("access_type", ""),
                    ("data_type", ""),
                    ("pdo_mapping", ""),
                    ("subobjects", OrderedDict()),
                ]
            ),
        )
        obj["subobjects"][str(int(subindex, 16))] = OrderedDict(
            [
                ("subindex", subindex.upper()),
                ("parameter_name", values.get("ParameterName", "")),
                ("access_type", values.get("AccessType", "")),
                ("data_type", values.get("DataType", "")),
                ("pdo_mapping", values.get("PDOMapping", "")),
                ("default_value", values.get("DefaultValue", "")),
            ]
        )

    return OrderedDict(
        [
            ("schema_version", "mecom_tec_canopen_eds.v1"),
            ("source", EDS_NAME),
            ("file_info", sections.get("FileInfo", OrderedDict())),
            ("device_info", sections.get("DeviceInfo", OrderedDict())),
            ("objects", objects),
        ]
    )


def add_help(
    rows: MutableMapping[str, OrderedDict],
    mepar_id: int,
    name: str,
    group: str,
    visibility: str,
    help_text: str,
    evidence: Iterable[str],
    *,
    access: str = "",
    safety_note: str = "",
) -> None:
    safety_note = safety_note or default_safety_note(group, visibility, access)
    row = OrderedDict(
        [
            ("mepar_id", mepar_id),
            ("name", name),
            ("group", group),
            ("visibility", visibility),
            ("help", compact_help(help_text)),
            ("source_evidence", list(evidence)),
        ]
    )
    if access:
        row["access"] = access
    if safety_note:
        row["safety_note"] = compact_help(safety_note)
    rows[str(mepar_id)] = row


def tooltip_rows() -> OrderedDict:
    rows: "OrderedDict[str, OrderedDict]" = OrderedDict()
    temp = source_ref("coso_baml_tooltips", "view/tec/tectemperaturecontrollerwindow.baml:strings")
    hr = source_ref("coso_baml_tooltips", "view/tec/techrinputwindow.baml:strings")
    lr = source_ref("coso_baml_tooltips", "view/tec/teclrinputwindow.baml:strings")
    adv = source_ref("coso_baml_tooltips", "view/misctop/advancedsettingswindow.baml:strings")
    lic = source_ref("coso_baml_tooltips", "view/misctop/licensewindow.baml:strings")
    extra = source_ref("coso_baml_tooltips", "view/misctop/extrafunctionswindow.baml:strings")
    settings = source_ref("coso_baml_tooltips", "view/misctop/mesettingswindow.baml:strings")
    code = source_ref("coso_ilspy_decompile", "MeSoft.CoSoG2.CoSo/MainWindow.cs and related decompile")

    add_help(rows, 100, "Device Type", "Device identity", "manufacturer", "Device type identifier reported by the controller.", [settings], access="read")
    add_help(rows, 101, "Hardware Version", "Device identity", "manufacturer", "Hardware version identifier reported by the controller.", [settings], access="read")
    add_help(rows, 102, "Serial Number", "Device identity", "operator", "Controller serial number used to keep raw diagnostics tied to the physical unit.", [settings], access="read")
    add_help(rows, 120, "User Notes", "Metadata", "operator", "User note string stored on the controller as non-volatile text metadata.", ["repo:mecom/catalogues/tec.json"], access="write", safety_note="Writes change controller non-volatile metadata.")
    add_help(rows, 202, "Max Input Power Limit", "Input protection", "advanced", "Input protection limit: the controller calculates a maximum input current using driver input voltage.", [adv], access="write", safety_note="Advanced input-protection limit; changing it can alter current limiting from the measured driver input voltage.")
    add_help(rows, 203, "Input Voltage Range", "Input protection", "manufacturer", "Absolute maximum supply-voltage configuration. Wrong values risk hardware damage.", [settings], access="write", safety_note="Manufacturer setting; do not expose as routine operator write.")
    add_help(rows, 204, "Power Stage v3 Mode", "Hardware test", "manufacturer", "Hardware-test related PowerStage v3 mode flag.", [settings], access="write", safety_note="Manufacturer hardware test setting.")
    add_help(rows, 2053, "Device Address Zero", "Communication", "manufacturer", "Hardware-test address-zero option; CoSo notes this is usually zero and should not be changed without the TEC grandmaster.", [settings], access="write", safety_note="Can break addressing and bus recovery.")
    add_help(rows, 217, "FreeRTOS Statistics", "Big data", "advanced", "Read-only LATIN1 big-data diagnostics blob containing FreeRTOS statistics.", [source_ref("coso_ilspy_decompile", "MeSoft.CoSoG2.TEC/MeSettingsWindow.cs:41")], access="read")
    add_help(rows, 250, "Custom Lock Payload", "Custom lock", "manufacturer", "Raw custom-lock big-data payload used by CoSo to lock parameter controls and features.", [source_ref("coso_custom_lock_decompile", "MeSoft.CoSoG2.CustLock/CustLockManager.cs")], access="read_write", safety_note="Write changes the controller lock payload.")
    add_help(rows, 1000, "Object Temperature", "Thermal", "operator", "Measured temperature of the controlled object.", [temp], access="read")
    add_help(rows, 1001, "Sink Temperature", "Thermal", "operator", "Measured temperature of the heat sink.", [temp], access="read")
    add_help(rows, 1010, "Target Temperature: Nominal", "Thermal", "operator", "Nominal target temperature used in display-format examples.", [code], access="read")
    add_help(rows, 1011, "Nominal Temperature Ramp", "Thermal", "operator", "Effective nominal temperature for the PID controller, usually ramping between the current object temperature and the target.", [temp], access="read")
    add_help(rows, 1020, "Actual Output Current", "Power", "operator", "Measured output current of the TEC output stage.", [code], access="read")
    add_help(rows, 1021, "Actual Output Voltage", "Power", "operator", "Measured output voltage of the TEC output stage.", [code], access="read")
    add_help(rows, 1022, "Actual Output Power", "Power", "operator", "Derived or reported output power used for power-supply graphing and limits.", ["repo:mecom/catalogues/tec.json"], access="read")
    add_help(rows, 1200, "Temperature Stable", "Status", "operator", "Stability indicator comparing measured temperature against the configured stability window and time.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="read")
    add_help(rows, 2000, "Output Stage Input Selection", "Control", "operator", "Selects whether the output stage follows current or voltage input while in output-stage mode.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 2010, "Output Enable", "Control", "operator", "Toggles the output stage of the TEC controller.", [temp], access="write", safety_note="Energizes or disables the output stage.")
    add_help(rows, 2020, "Fixed Current", "Control", "operator", "Fixed output current command used when operating as a current-limited power supply.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 2021, "Fixed Voltage", "Control", "operator", "Fixed output voltage command used when operating as a voltage-limited power supply.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 2030, "Output Current Limit", "Control", "operator", "Output Current Limit restricts maximum output current and also limits the temperature PID controller to prevent integral windup.", [temp], access="write")
    add_help(rows, 2031, "Output Voltage Limit", "Control", "operator", "Output Voltage Limit restricts maximum output voltage. In temperature control mode it should not be set so low that the output stage continuously saturates.", [temp], access="write")
    add_help(rows, 2032, "Output Current Error Threshold", "Control", "advanced", "Overcurrent protection threshold. The controller enters error state and shuts off if output current exceeds this value.", [temp], access="write")
    add_help(rows, 2033, "Output Voltage Error Threshold", "Control", "advanced", "Overvoltage protection threshold. The controller enters error state and shuts off if output voltage exceeds this value.", [temp], access="write")
    add_help(rows, 2040, "Operating Mode", "Control", "operator", "General operating mode selection for off, closed-loop TEC control, constant-voltage supply, or constant-current supply.", [code], access="write", safety_note="Changing mode can reset control behavior and should be tracked as a command.")
    add_help(rows, 2050, "Communication Baudrate", "Communication", "manufacturer", "UART or RS485 baudrate setting; wrong values can cut off serial communication.", [code], access="write", safety_note="Can make the device unreachable until recovered locally.")
    add_help(rows, 2051, "RS485 Address", "Communication", "manufacturer", "MeCom address setting. Wrong address values can make the controller disappear from the expected bus address.", [code], access="write", safety_note="Addressing change must be coordinated with bus discovery.")
    add_help(rows, 2052, "Reply Delay", "Communication", "manufacturer", "UART or RS485 reply-delay timing parameter for bus interoperability.", [code], access="write")
    add_help(rows, 2060, "Connect Watchdog", "Communication", "manufacturer", "Connection watchdog setting that can influence fail-safe behavior.", [code], access="write")
    add_help(rows, 2070, "CANopen Node ID", "CANopen", "manufacturer", "CANopen node-ID setting; changing it changes where the controller appears on the bus.", [code], access="write", safety_note="Can make the controller disappear on CANopen until rediscovered.")
    add_help(rows, 2071, "CANopen Bitrate", "CANopen", "manufacturer", "CANopen bitrate setting; wrong values disconnect the controller from the CAN network.", [code], access="write", safety_note="Can break bus communication.")
    add_help(rows, 2072, "CANopen Enable", "CANopen", "manufacturer", "CANopen interface enable flag.", [code], access="write")
    add_help(rows, 3000, "Target Temperature", "Control", "operator", "Define a target temperature that the load should be regulated to by the temperature controller.", [temp], access="read_write")
    add_help(rows, 3002, "Proximity Width", "Control", "operator", "Temperature band around target where the nominal temperature ramp changes to a sine-shaped approach.", [temp], access="write")
    add_help(rows, 3003, "Coarse Temperature Ramp", "Control", "operator", "Temperature ramp rate used outside the proximity-width band.", [temp], access="write")
    add_help(rows, 3004, "Ramp Start Point", "Control", "operator", "Selects whether a new ramp starts from object temperature, target temperature, or current ramp temperature.", [temp], access="write")
    add_help(rows, 3010, "PID Kp", "PID controller", "operator", "Proportional gain of the temperature PID controller.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 3011, "PID Ti", "PID controller", "operator", "Integral time of the temperature PID controller.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 3012, "PID Td", "PID controller", "operator", "Derivative time of the temperature PID controller.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 3013, "PID D Part PT1", "PID controller", "operator", "PT1 damping for the derivative component of the temperature PID controller.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 3020, "Thermal Modelization", "Thermal model", "operator", "Thermal model selection for Peltier or resistive-heater control behavior.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 3030, "Peltier Maximal Current", "Thermal model", "operator", "Output Current Limit used by the Peltier thermal model to bound the nominal drive current.", [temp], access="write")
    add_help(rows, 3033, "Peltier Maximal Temperature Delta", "Thermal model", "operator", "Maximum temperature delta expected from the Peltier model.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 3034, "Peltier Polarity", "Thermal model", "operator", "Polarity setting that determines whether positive drive cools or heats the controlled object.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 3040, "Resistive Heater Resistance", "Thermal model", "operator", "Load resistance used by the resistive-heater model.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 3041, "Resistive Heater Maximal Current", "Thermal model", "operator", "Maximum current used by the resistive-heater thermal model.", [source_ref("tec_default_config_5216o_xml", DEFAULT_CONFIG_NAME)], access="write")
    add_help(rows, 3050, "Lower Boundary", "Heat/cool only", "operator", "Lower boundary for heat-only or cool-only Peltier operation.", [temp], access="write")
    add_help(rows, 3051, "Upper Boundary", "Heat/cool only", "operator", "Upper boundary for heat-only or cool-only Peltier operation.", [temp], access="write")
    add_help(rows, 40000, "Output Stage Temperature", "Thermal", "advanced", "Readable output-stage temperature. CoSo exposes individual power-stage temperatures in manufacturer diagnostics.", [settings], access="read")
    add_help(rows, 4034, "Sensor Type", "Sensor input", "advanced", "Read-only sensor type detected or configured for the temperature measurement input.", [hr], access="read")
    add_help(rows, 5001, "Temperature Calibration Offset", "Sensor input", "advanced", "Temperature calibration offset for the low-range input.", [lr], access="write")
    add_help(rows, 5002, "Temperature Calibration Gain", "Sensor input", "advanced", "Temperature calibration gain for the low-range input.", [lr], access="write")
    add_help(rows, 5005, "Temperature Filter PT1 Factor", "Sensor input", "advanced", "Low-range input PT1 filter factor. Smaller values filter more strongly but add delay.", [lr], access="write")
    add_help(rows, 5010, "Lower Temperature Threshold", "Sensor input", "advanced", "Temperature limit error threshold for low measured temperatures.", [lr], access="write")
    add_help(rows, 5011, "Upper Temperature Threshold", "Sensor input", "advanced", "Temperature limit error threshold for high measured temperatures.", [lr], access="write")
    add_help(rows, 5012, "Maximum Temperature Change", "Sensor input", "advanced", "Throws an error if measured temperature changes faster than this configured limit.", [lr], access="write")
    add_help(rows, 5013, "Temperature Limit Errors", "Sensor input", "advanced", "Selects which low-range temperature measurement thresholds produce errors.", [lr], access="write")
    add_help(rows, 6000, "ADC PGA Gain", "Sensor input", "advanced", "Programmable-gain amplifier gain for the sensor input path.", [hr], access="write", safety_note="Hardware-specific calibration setting.")
    add_help(rows, 6001, "Current Source", "Sensor input", "advanced", "Sensor-current source setting; IDAC1 always flows through the instrumentation amplifier.", [hr], access="write")
    add_help(rows, 6002, "Reference Series Resistor", "Sensor input", "advanced", "Reference series resistor used by the sensor measurement conversion.", [hr], access="write")
    add_help(rows, 6003, "Sensor Offset", "Sensor input", "advanced", "Factory calibration offset for the sensor input.", [hr], access="write")
    add_help(rows, 6004, "Sensor Gain", "Sensor input", "advanced", "Factory calibration gain for the sensor input.", [hr], access="write")
    add_help(rows, 6005, "Temperature Sensor Type", "Sensor input", "advanced", "Sensor type selection; changing it can require matching hardware and calibration.", [code], access="write", safety_note="Hardware-specific setting.")
    add_help(rows, 6007, "PGA Bypass", "Sensor input", "advanced", "PGA bypass option that can help measurement-noise behavior in some input configurations.", [hr], access="write")
    add_help(rows, 6008, "Current Source 2 Output", "Sensor input", "advanced", "Selects the ADC pin used for the second current source.", [hr], access="write")
    add_help(rows, 6009, "Measurement Type", "Sensor input", "advanced", "Selects resistance or voltage measurement type for the connected sensor.", [hr], access="write")
    add_help(rows, 6014, "ADC Limit Errors", "Sensor input", "advanced", "Selects which ADC thresholds trigger sensor measurement errors.", [lr], access="write")
    add_help(rows, 6200, "Fan Enable", "Fan controller", "advanced", "Fan-controller enable flag.", [code], access="write")
    add_help(rows, 6210, "Fan Temperature Source", "Fan controller", "advanced", "Temperature source selection for fan-control behavior.", [code], access="write")
    add_help(rows, 6230, "Fan PWM Frequency", "Fan controller", "advanced", "PWM frequency selection for fan control.", [code], access="write")
    add_help(rows, 6300, "Object Active Source", "Advanced control", "advanced", "Object active source selection for deciding which source drives object-temperature control.", [code], access="write")
    add_help(rows, 6301, "Control Cycle", "Advanced control", "advanced", "Temperature-control cycle selection affecting regulation timing.", [code], access="write")
    add_help(rows, 6302, "Object Observe Mode", "Advanced control", "advanced", "Object temperature observe-mode setting.", [code], access="write")
    add_help(rows, 6303, "Object Observe Mode Variant", "Advanced control", "advanced", "Second object observe-mode mapping recovered from CoSo import metadata.", [code], access="write")
    add_help(rows, 6304, "Sink Temperature Mode", "Advanced control", "advanced", "Sink temperature mode selection for external or fixed sink-temperature behavior.", [code], access="write")
    add_help(rows, 6310, "Error State Auto Reset Delay", "Safety", "advanced", "Delay before automatic restart after error state. Zero disables auto-reset; startup adds a delay.", [adv], access="write")
    add_help(rows, 6320, "Output Stage Controller Limit Error Delay", "Safety", "advanced", "Delay before output-stage controller-limit error 108 is raised. -1 disables the error and is not recommended.", [adv], access="write", safety_note="Safety-related error suppression timing.")
    add_help(rows, 6330, "Device Temperature Mode", "Safety", "advanced", "Selects standard or extended device temperature mode; extended mode widens temperature range with current limiting.", [adv], access="write")
    add_help(rows, 6400, "Voltage Conversion RT", "Sensor conversion", "advanced", "Voltage-conversion calibration coefficient for object-temperature input conversion.", [code], access="write")
    add_help(rows, 6401, "Voltage Conversion RU", "Sensor conversion", "advanced", "Voltage-conversion calibration coefficient for object-temperature input conversion.", [code], access="write")
    add_help(rows, 6402, "Voltage Conversion Slope", "Sensor conversion", "advanced", "Voltage-conversion slope calibration for object-temperature input conversion.", [code], access="write")
    add_help(rows, 53000, "License Key", "License", "license", "License key field provided by Meerstetter for feature unlocks.", [lic], access="write", safety_note="Vendor license payload.")
    add_help(rows, 53001, "License Key Status", "License", "license", "License key validation status including empty, valid, wrong structure, wrong signature, header failure, wrong device type, or unique ID mismatch.", [lic], access="read")
    add_help(rows, 53010, "Temperature Estimator License Status", "License", "license", "Feature license status for the temperature estimator.", [lic, extra], access="read")
    add_help(rows, 53015, "Cascade Temperature Control License Status", "License", "license", "Feature license status for cascade temperature control.", [lic, extra], access="read")
    add_help(rows, 53020, "Unipolar and Mix Operating Mode License Status", "License", "license", "Feature license status for unipolar and mixed operating modes.", [lic], access="read")
    add_help(rows, 53100, "Estimator Enable", "Extra functions", "advanced", "Enables the temperature estimator if a valid license is present.", [extra], access="write")
    add_help(rows, 53101, "Estimator Model Input Temperature", "Extra functions", "advanced", "Selects the temperature input used by the estimator model, such as HR or LR sensors.", [extra], access="write")
    add_help(rows, 53102, "Estimator Ambient Temperature", "Extra functions", "advanced", "Selects ambient temperature source for the estimator model, including fixed ambient temperature.", [extra], access="write")
    add_help(rows, 53103, "Fixed Ambient Temperature", "Extra functions", "advanced", "Fixed ambient temperature used by the estimator when no sensor source is selected.", [extra], access="write")
    add_help(rows, 53104, "Estimator Time Constant Damping", "Extra functions", "advanced", "Damping time constant for the temperature estimator model.", [extra], access="write")
    add_help(rows, 53105, "Estimator Heat Loss Factor", "Extra functions", "advanced", "Heat-loss factor used by the temperature estimator model.", [extra], access="write")
    add_help(rows, 53106, "Estimator Monitor Input", "Extra functions", "advanced", "Temperature-estimator monitor input value.", [extra], access="read")
    add_help(rows, 53107, "Estimator Monitor Output", "Extra functions", "advanced", "Temperature-estimator monitor output value.", [extra], access="read")
    add_help(rows, 53120, "Cascade Enable", "Cascade", "operator", "Enables cascade temperature control if a valid cascade license is present.", [extra], access="write", safety_note="Changes control-loop topology.")
    add_help(rows, 53121, "Cascade Current Temperature Selection", "Cascade", "operator", "Selects the current temperature input of the outer cascade control loop, similar to object temperature of that loop.", [extra], access="write")
    add_help(rows, 53122, "Cascade Sync Run With", "Cascade", "operator", "Selects the inner temperature-control instance synchronized with the cascade loop.", [extra], access="write")
    add_help(rows, 53123, "Cascade Target Temperature", "Cascade", "operator", "Target temperature for the outer cascade loop when cascade temperature control is active.", [extra], access="write")
    add_help(rows, 53124, "Cascade Coarse Temperature Ramp", "Cascade", "operator", "Coarse temperature ramp for cascade nominal-temperature generation.", [extra], access="write")
    add_help(rows, 53125, "Cascade Proximity Width", "Cascade", "operator", "Proximity width for cascade nominal-temperature ramping.", [extra], access="write")
    add_help(rows, 53126, "Cascade Ramp Start Point", "Cascade", "operator", "Selects whether cascade ramping starts from current temperature, target, or ramp value.", [extra], access="write")
    add_help(rows, 53128, "Cascade PID Kp", "Cascade", "operator", "Proportional gain for the outer cascade PID controller.", [extra], access="write")
    add_help(rows, 53129, "Cascade PID Ti", "Cascade", "operator", "Integral time for the outer cascade PID controller.", [extra], access="write")
    add_help(rows, 53130, "Cascade PID Td", "Cascade", "operator", "Derivative time for the outer cascade PID controller.", [extra], access="write")
    add_help(rows, 53131, "Cascade PID D Part PT1", "Cascade", "operator", "PT1 damping for the derivative component of the outer cascade PID controller.", [extra], access="write")
    add_help(rows, 53132, "Cascade I Freeze Triggered By", "Cascade", "advanced", "Freezes the outer PID integral action when the selected inner PID loop runs into limitation.", [extra], access="write")
    add_help(rows, 53133, "Cascade PID Upper Limit", "Cascade", "operator", "Upper output limit for the outer cascade PID controller.", [extra], access="write")
    add_help(rows, 53134, "Cascade PID Lower Limit", "Cascade", "operator", "Lower output limit for the outer cascade PID controller.", [extra], access="write")
    add_help(rows, 53135, "Cascade Range Around Target", "Cascade", "operator", "Limits cascade PID output around the target temperature by the configured range.", [extra], access="write")
    add_help(rows, 53136, "Cascade Current Temperature", "Cascade", "operator", "Monitor value for the cascade outer-loop current temperature.", [extra], access="read")
    add_help(rows, 53137, "Cascade Nominal Temperature Ramp", "Cascade", "operator", "Monitor value for the cascade nominal target after ramping.", [extra], access="read")
    add_help(rows, 53138, "Cascade PID Upper Limit Monitor", "Cascade", "operator", "Monitor value for the effective cascade PID upper limit.", [extra], access="read")
    add_help(rows, 53139, "Cascade PID Lower Limit Monitor", "Cascade", "operator", "Monitor value for the effective cascade PID lower limit.", [extra], access="read")
    add_help(rows, 53140, "Cascade Output", "Cascade", "operator", "Output of the outer cascade control loop.", [extra], access="read")
    add_help(rows, 65020, "MeParFlashV2 Write Cycles", "Diagnostics", "advanced", "Flash write-cycle counter; excessive cycles can force read-only behavior.", [settings], access="read")
    add_help(rows, 65100, "PAR_DEBUG", "Diagnostics", "manufacturer", "Debug parameter family exposed as PAR_DEBUG0 through PAR_DEBUG4 instances.", [settings], access="read")
    add_help(rows, 9000, "Firmware Update Status Text", "Firmware update", "advanced", "LATIN1 big-data status text reported by the firmware updater.", [code], access="read")
    add_help(rows, 9001, "Firmware Update Status", "Firmware update", "advanced", "Firmware updater status value.", [code], access="read")
    add_help(rows, 9002, "Firmware Update Progress", "Firmware update", "advanced", "Firmware updater progress value.", [code], access="read")
    add_help(rows, 9003, "Firmware Update Detail Status", "Firmware update", "advanced", "Additional firmware updater status value.", [code], access="read")
    add_help(rows, 9010, "Firmware Update Activate", "Firmware update", "manufacturer", "Firmware update activation command.", [code], access="write", safety_note="Write triggers firmware-update state transition.")
    return rows


def hidden_candidates() -> List[OrderedDict]:
    rows = [
        (202, "advanced", "advanced", "Input power-limit setting from advanced settings."),
        (203, "manufacturer", "manufacturer", "Input voltage range, hardware-damage risk."),
        (204, "manufacturer", "manufacturer", "PowerStage v3 hardware-test mode."),
        (2050, "communication", "manufacturer", "UART or RS485 baudrate setting."),
        (2051, "communication", "manufacturer", "RS485 address setting."),
        (2052, "communication", "manufacturer", "Reply delay setting."),
        (2053, "communication", "manufacturer", "Address-zero hardware-test setting."),
        (2060, "communication", "manufacturer", "Connection watchdog setting."),
        (2070, "canopen", "manufacturer", "CANopen node-ID setting."),
        (2071, "canopen", "manufacturer", "CANopen bitrate setting."),
        (2072, "canopen", "manufacturer", "CANopen interface enable setting."),
        (217, "big_data", "advanced", "FreeRTOS statistics big-data read."),
        (250, "custom_lock", "manufacturer", "Custom-lock big-data read/write payload."),
        (40000, "advanced", "advanced", "Output-stage temperature diagnostics."),
        (53000, "license", "license", "Vendor feature license key."),
        (53001, "license", "license", "Vendor feature license status."),
        (53010, "license", "license", "Temperature-estimator license status."),
        (53015, "license", "license", "Cascade-control license status."),
        (53020, "license", "license", "Unipolar and mixed-mode license status."),
        (53100, "extra_functions", "advanced", "Temperature estimator enable."),
        (53101, "extra_functions", "advanced", "Temperature estimator input source."),
        (53102, "extra_functions", "advanced", "Temperature estimator ambient source."),
        (6000, "advanced", "advanced", "Sensor input PGA gain."),
        (6005, "advanced", "advanced", "Sensor type selection."),
        (6200, "advanced", "advanced", "Fan controller enable."),
        (6300, "advanced", "advanced", "Object active source selection."),
        (6310, "advanced", "advanced", "Error-state auto-reset delay."),
        (6320, "advanced", "advanced", "Output-stage limit error delay."),
        (6330, "advanced", "advanced", "Device temperature mode."),
        (65004, "advanced", "advanced", "Power-stage voltage U1A/U2A diagnostics."),
        (65005, "advanced", "advanced", "Power-stage current I1A/I2A diagnostics."),
        (65006, "advanced", "advanced", "Power-stage voltage U1B/U2B diagnostics."),
        (65007, "advanced", "advanced", "Power-stage current I1B/I2B diagnostics."),
        (65008, "advanced", "advanced", "Parallel common-load symmetry correction."),
        (65009, "advanced", "advanced", "Relative PWM level diagnostics."),
        (65010, "advanced", "advanced", "Absolute PWM level A diagnostics."),
        (65011, "advanced", "advanced", "Absolute PWM level B diagnostics."),
        (65020, "advanced", "advanced", "Flash write-cycle counter."),
        (65100, "manufacturer", "manufacturer", "PAR_DEBUG diagnostic instances."),
        (9000, "firmware", "advanced", "Firmware updater status text."),
        (9010, "firmware", "manufacturer", "Firmware update activation command."),
    ]
    candidates = []
    for mepar_id, group, visibility, note in rows:
        candidates.append(
            OrderedDict(
                [
                    ("mepar_id", mepar_id),
                    ("group", group),
                    ("visibility", visibility),
                    ("note", note),
                    ("safety_note", default_safety_note(group, visibility, "write" if visibility in {"manufacturer", "license"} else "read")),
                ]
            )
        )
    return candidates


def metadata_index() -> OrderedDict:
    return OrderedDict(
        [
            ("schema_version", "mecom_tec_metadata_index.v1"),
            (
                "sources",
                [
                    OrderedDict(
                        [
                            ("id", "tec_default_config_5216o_xml"),
                            ("path", "Meerstetter TEC-Family Software v6.31 package:" + DEFAULT_CONFIG_NAME),
                            ("method", "xml_default_config_parse"),
                            ("status", "harvested"),
                        ]
                    ),
                    OrderedDict(
                        [
                            ("id", "canopen_eds_v631"),
                            ("path", "Meerstetter TEC-Family Software v6.31 package:" + EDS_NAME),
                            ("method", "eds_ini_parse"),
                            ("status", "harvested"),
                        ]
                    ),
                    OrderedDict(
                        [
                            ("id", "coso_baml_tooltips"),
                            ("path", "CoSo WPF BAML resources:view/**/*.baml"),
                            ("method", "sfextract_and_strings_review"),
                            ("status", "harvested"),
                        ]
                    ),
                    OrderedDict(
                        [
                            ("id", "coso_ilspy_decompile"),
                            ("path", "CoSo ILSpy decompile:**/*.cs"),
                            ("method", "ilspycmd_static_review"),
                            ("status", "harvested"),
                        ]
                    ),
                    OrderedDict(
                        [
                            ("id", "coso_custom_lock_decompile"),
                            ("path", "CoSo custom-lock library ILSpy decompile:**/*.cs"),
                            ("method", "ilspycmd_static_review"),
                            ("status", "harvested"),
                        ]
                    ),
                ],
            ),
            ("hidden_candidates", hidden_candidates()),
            (
                "safety_policy",
                "Hidden, manufacturer, license, firmware-update, and custom-lock entries are metadata candidates only; do not add them to active polling or write controls without a separate safety review.",
            ),
            ("manufacturer_gate_evidence", "CoSo contains manufacturer/settings gate logic; gate material is intentionally redacted from this public catalogue metadata."),
        ]
    )


def write_json(path: Path, payload: OrderedDict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--package", required=True, help="Path to the Meerstetter TEC-Family Software package ZIP.")
    parser.add_argument("--out", default="mecom/catalogues/sources")
    args = parser.parse_args()

    package_path = Path(args.package)
    out = Path(args.out)
    with zipfile.ZipFile(package_path) as package:
        write_json(out / "tec_default_config_5216o.v631.json", harvest_default_config(package))
        write_json(out / "canopen_eds.v631.json", harvest_eds(package))

    write_json(
        out / "tec_tooltips.v631.json",
        OrderedDict(
            [
                ("schema_version", "mecom_tec_help.v1"),
                ("source", "Meerstetter TEC Configuration Software v6.31 CoSo BAML/decompile review"),
                ("parameters", tooltip_rows()),
            ]
        ),
    )
    write_json(out / "tec_metadata_index.v631.json", metadata_index())


if __name__ == "__main__":
    main()
