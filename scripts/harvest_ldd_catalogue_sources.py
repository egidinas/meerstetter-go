#!/usr/bin/env python3
"""Harvest public LDD-130x catalogue evidence from a Meerstetter MSI payload.

The generated JSON files are source artefacts for the MeCom LDD catalogue. They
keep official defaults, CANopen object names, service-software labels, and
release-note findings separate from the active runtime catalogue.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import xml.etree.ElementTree as ET
import shutil
import subprocess
from collections import OrderedDict
from pathlib import Path
from typing import Any, Iterable, List, Mapping, MutableMapping, Optional, Tuple


variant = "ldd_130x"
version = "v221"
PAYLOAD_SOURCE_NAME = "LDD-130x Software v2.21 MSI payload"

DEFAULT_CONFIG_NAME = "5261H LDD-130x Default Config.ini"
EDS_NAME = "LDD-130x STM32 v2.21 CanOpen.eds"
RELEASE_NOTES_NAME = "LDD-130x Software Release Notes 5265M.pdf"
SERVICE_SOFTWARE_NAME = "ServiceSoftware.exe"
FIRMWARE_NAME = "LDD-130x STM32 v2.21.hex"
LOOKUP_CSV_NAME = "LookupTable Example.csv"
LOOKUP_XLSX_NAME = "LookupTable Example.xlsx"
UI_METADATA_NAME = "ldd_130x_ui_metadata.v221.json"
PROTOCOL_DOC_NAME = "LDD-130x Communication Protocol 5260H.pdf"
USER_MANUAL_NAME = "LDD-130x User Manual 5261H.pdf"
SERVICE_RESOURCE_STRING_SCAN_CANDIDATES = (
    ("utf-16le", ("-el",)),
    ("utf-16be", ("-eb",)),
    ("ascii", ()),
)
SERVICE_SOURCE_ID = "ldd_130x_service_software"
SERVICE_SOURCE_DETAIL = "ServiceSoftware.exe managed resource strings"

CONTROL_ID_RE = re.compile(
    r"^(?:"
    r"label|textBox|comboBox|groupBox|tabControl|tabPage|tab_|button|checkBox|pictureBox|panel|"
    r"listBox|progressBar|dataGrid|numericUpDown|radioButton|splitContainer|toolStrip|"
    r"statusStrip|menuStrip|timer|openFileDialog|saveFileDialog|folderBrowserDialog|"
    r"backgroundWorker|serialPort"
    r")[A-Za-z0-9_]*$"
)
PATH_ROOTS = {"Operation", "AnalogInterface", "Advanced", "ME", "Monitor", "Debug"}
PROTOCOL_FORMATS = ("INT32", "FLOAT32", "LATIN1", "UINT32", "UINT16", "UINT8", "BYTE", "STRING")
MONTHS = {
    "January": "01",
    "February": "02",
    "March": "03",
    "April": "04",
    "May": "05",
    "June": "06",
    "July": "07",
    "August": "08",
    "September": "09",
    "October": "10",
    "November": "11",
    "December": "12",
}
NOISE_PREFIXES = (
    "System.",
    "Microsoft.",
    "MeCom.",
    "Meerstetter.",
    "WindowsForms",
)
NOISE_SUFFIXES = (
    ".dll",
    ".exe",
    ".pdb",
    ".resources",
)

DEFINITION = OrderedDict(
    [
        ("definition_ref", f"meerstetter.{variant}.{version}"),
        ("system", "mecom"),
        ("family", "meerstetter"),
        ("sub_family", "ldd"),
        ("variant", variant),
        ("version", version),
    ]
)

LDD_OPERATOR_KEYS = {
    "LDD_OUTPUT_EN",
    "LDD_OUTPUT_RESET_OFF",
    "LDD_NOM_CURRENT_SOURCE",
    "LDD_NOM_CURRENT",
    "LDD_PID_KP",
    "LDD_PID_TI",
    "LDD_PID_TD",
    "LDD_NOM_SLOPE_LIMIT",
    "LDD_CURRENT_LIMIT_MAX",
    "LDD_CURRENT_LIMIT_MIN",
    "LDD_CURRENT_ERROR_THRESH",
    "LDD_VOLTAGE_ERROR_THRESH",
    "LDD_T_CORR_SOURCE",
    "LDD_T_CORR_OFFSET",
    "LDD_T_CORR_GAIN",
    "LDD_VOLTAGE_LIMIT_MAX",
    "LDD_VOLTAGE_LIMIT_MIN",
    "LDD_CURRENT_ERROR_FAST",
}

OPERATOR_KEYS = LDD_OPERATOR_KEYS | {
    "LPC_NOM_POWER_SOURCE",
    "LPC_NOM_POWER",
    "LPC_PID_KP",
    "LPC_PID_TI",
    "LPC_PID_TD",
    "LPC_NOM_SLOPE_LIMIT",
    "LPC_POWER_LIMIT_MAX",
    "LPC_POWER_LIMIT_MIN",
    "ANALOG_CURRENT_FACTOR",
    "ANALOG_POWER_FACTOR",
    "PHOTODIODE_RS",
    "AUTORESET_TIME",
}

METADATA_KEYS = {
    "CONNECT_WATCH_DOG",
    "DAC_OUT_SOURCE",
    "DAC_OUTPUT_VALUE",
    "DAC_OUTPUT_GAIN",
}


def compact_text(text: str, limit: int = 420) -> str:
    text = " ".join(normalize_text(text).split())
    if len(text) <= limit:
        return text
    return text[: limit - 1].rstrip() + "."


def normalize_text(text: str) -> str:
    replacements = {
        "\ufeff": "",
        "\u00a0": " ",
        "\u2013": "-",
        "\u2014": "-",
        "\u2018": "'",
        "\u2019": "'",
        "\u201c": '"',
        "\u201d": '"',
        "\u2022": "*",
        "\u2265": ">=",
        "\u2264": "<=",
    }
    for old, new in replacements.items():
        text = text.replace(old, new)
    return text


def normalize_catalogue_name(text: str) -> str:
    text = normalize_text(text)
    text = re.sub(r"\[[^\]]+\]", " ", text)
    text = text.replace(":", " ")
    text = re.sub(r"[^A-Za-z0-9]+", " ", text)
    return " ".join(text.lower().split())


def decode_text_bytes(raw: bytes) -> Tuple[str, str]:
    if raw.startswith(b"\xef\xbb\xbf"):
        return raw.decode("utf-8-sig"), "utf-8-sig"
    if raw.startswith(b"\xff\xfe"):
        return raw.decode("utf-16le"), "utf-16le"
    if raw.startswith(b"\xfe\xff"):
        return raw.decode("utf-16be"), "utf-16be"
    if all(byte < 0x80 for byte in raw):
        return raw.decode("ascii"), "ascii"
    try:
        return raw.decode("utf-8"), "utf-8"
    except UnicodeDecodeError:
        return raw.decode("latin-1"), "latin-1"


def read_text(path: Path) -> Tuple[str, str]:
    text, encoding = decode_text_bytes(path.read_bytes())
    return normalize_text(text), encoding


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def source_evidence(source_id: str, logical_name: str, detail: str = "") -> str:
    ref = f"{source_id}:{logical_name}"
    if detail:
        ref += f":{detail}"
    return ref


def payload_file(payload_dir: Path, logical_name: Optional[str], required: bool = True, patterns: Optional[List[str]] = None) -> Optional[Path]:
    if logical_name and logical_name.lower() in ("none", "null"):
        return None
    matches = []
    if logical_name:
        for path in payload_dir.iterdir():
            if not path.is_file():
                continue
            if path.name == logical_name or path.name.endswith("__" + logical_name):
                matches.append(path)
    if not matches and patterns:
        for pattern in patterns:
            for path in payload_dir.glob(pattern):
                if path.is_file():
                    matches.append(path)
            if matches:
                break
    if not matches:
        if required:
            raise FileNotFoundError(f"payload is missing file matching {logical_name or patterns}")
        return None
    if len(matches) > 1:
        if logical_name:
            exact = [m for m in matches if m.name == logical_name]
            if len(exact) == 1:
                return exact[0]
        raise RuntimeError(f"payload has multiple matches for {logical_name or patterns}: {matches}")
    return matches[0]



def parse_ini_sections(text: str) -> "OrderedDict[str, OrderedDict[str, str]]":
    sections: "OrderedDict[str, OrderedDict[str, str]]" = OrderedDict()
    current: Optional[str] = None
    for raw_line in text.splitlines():
        line = raw_line.strip()
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


def parse_scalar(text: str) -> Tuple[Any, str]:
    stripped = text.strip()
    if stripped == "":
        return "", "empty"
    if re.fullmatch(r"[-+]?\d+", stripped):
        try:
            return int(stripped), "integer"
        except ValueError:
            pass
    if re.fullmatch(r"[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?", stripped):
        try:
            return float(stripped), "number"
        except ValueError:
            pass
    return stripped, "text"


def label_key_to_key(label_key: str) -> str:
    if label_key.startswith("label_PAR_"):
        return label_key[len("label_PAR_") :]
    return label_key


def key_group(key: str) -> str:
    if key.startswith("label_"):
        return "label"
    if key == "CONNECT_WATCH_DOG":
        return "system"
    if key.startswith("RS485"):
        return "rs485"
    return key.split("_", 1)[0].lower()


def key_visibility(key: str, group: str) -> str:
    if key in OPERATOR_KEYS:
        return "operator"
    if key in METADATA_KEYS or group == "label":
        return "metadata"
    return "advanced"


def safety_note(key: str, group: str, visibility: str) -> str:
    if visibility == "metadata":
        return ""
    if key == "AUTORESET_TIME":
        return "Fault recovery timing parameter; review against safety procedures before changing."
    if visibility == "operator" and group in {"ldd", "lpc"}:
        return "Laser driver control parameter; require explicit operator intent and confirmed readback before changing."
    if visibility == "advanced":
        return "Advanced LDD setting harvested from vendor software; keep out of routine operator writes until mapped to a reviewed control surface."
    return ""


def definition_ref() -> OrderedDict:
    return OrderedDict(DEFINITION)


def to_snake(s: str) -> str:
    s = s.replace("IP Monitor", "IP_Monitor")
    s = s.replace("IP Settings", "IP_Settings")
    s = re.sub(r'[^a-zA-Z0-9]', '_', s)
    s = re.sub(r'_+', '_', s)
    return s.strip('_').upper()


def harvest_default_config(path: Path) -> Tuple[OrderedDict, str]:
    if path.suffix.lower() == ".xml":
        tree = ET.parse(path)
        root = tree.getroot()
        parameters: "OrderedDict[str, OrderedDict[str, Any]]" = OrderedDict()
        for node in root.findall(".//Parameter"):
            mepar_id = node.attrib["MeParID"]
            mepar_inst = node.attrib["MeParInst"]
            name = node.attrib.get("Name", "")
            value_text = (node.text or "").strip()
            
            # Key generation from Name
            parts = [p.strip() for p in name.split('/') if p.strip()]
            if not parts:
                key = f"PAR_{mepar_id}"
            else:
                parts_clean = [to_snake(p) for p in parts]
                if len(parts_clean) > 1:
                    key_parts = parts_clean[1:]
                else:
                    key_parts = parts_clean
                key = "_".join(key_parts)
            
            value, value_kind = parse_scalar(value_text)
            group = key_group(key)
            visibility = key_visibility(key, group)
            label_key = f"label_PAR_{key}"
            
            parameters[key] = OrderedDict(
                [
                    ("key", key),
                    ("label_key", label_key),
                    ("group", group),
                    ("visibility", visibility),
                    ("default_value", value),
                    ("default_value_text", value_text),
                    ("value_kind", value_kind),
                    ("safety_note", safety_note(key, group, visibility)),
                    ("mepar_id", mepar_id),
                    ("mepar_inst", mepar_inst),
                    ("name_path", name),
                    (
                        "source_evidence",
                        [
                            source_evidence(
                                PAYLOAD_SOURCE_NAME,
                                DEFAULT_CONFIG_NAME,
                                f"Parameter[MeParID={mepar_id}, Name={name}]",
                            )
                        ],
                    ),
                ]
            )
        return (
            OrderedDict(
                [
                    ("schema_version", f"mecom_{variant}_defaults.v1"),
                    ("source", DEFAULT_CONFIG_NAME),
                    ("definition", definition_ref()),
                    ("parameters", parameters),
                ]
            ),
            "utf-8",
        )

    text, encoding = read_text(path)
    sections = parse_ini_sections(text)
    values = sections.get("SRV_PAR", OrderedDict())
    parameters: "OrderedDict[str, OrderedDict[str, Any]]" = OrderedDict()
    for label_key, value_text in values.items():
        key = label_key_to_key(label_key)
        value, value_kind = parse_scalar(value_text)
        group = key_group(key)
        visibility = key_visibility(key, group)
        parameters[key] = OrderedDict(
            [
                ("key", key),
                ("label_key", label_key),
                ("group", group),
                ("visibility", visibility),
                ("default_value", value),
                ("default_value_text", value_text),
                ("value_kind", value_kind),
                ("safety_note", safety_note(key, group, visibility)),
                (
                    "source_evidence",
                    [
                        source_evidence(
                            PAYLOAD_SOURCE_NAME,
                            DEFAULT_CONFIG_NAME,
                            f"[SRV_PAR].{label_key}",
                        )
                    ],
                ),
            ]
        )
    return (
        OrderedDict(
            [
                ("schema_version", f"mecom_{variant}_defaults.v1"),
                ("source", DEFAULT_CONFIG_NAME),
                ("definition", definition_ref()),
                ("parameters", parameters),
            ]
        ),
        encoding,
    )


def harvest_eds(path: Path) -> Tuple[OrderedDict, str]:
    text, encoding = read_text(path)
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
                    ("default_value", values.get("DefaultValue", "")),
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
                    ("default_value", ""),
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

    return (
        OrderedDict(
            [
                ("schema_version", f"mecom_{variant}_canopen_eds.v1"),
                ("source", EDS_NAME),
                ("definition", definition_ref()),
                ("file_info", sections.get("FileInfo", OrderedDict())),
                ("device_info", sections.get("DeviceInfo", OrderedDict())),
                ("objects", objects),
            ]
        ),
        encoding,
    )


def document_header(text: str, document_pattern: str) -> OrderedDict:
    match = re.search(
        rf"Document\s+({document_pattern})(?:[\s\S]{{0,240}}?)Release date:\s+(\d{{1,2}})\s+([A-Za-z]+)\s+(\d{{4}})",
        text,
    )
    if not match:
        return OrderedDict([("document", ""), ("release_date", "")])
    month = MONTHS.get(match.group(3), "")
    release_date = ""
    if month:
        release_date = f"{match.group(4)}-{month}-{int(match.group(2)):02d}"
    return OrderedDict([("document", match.group(1)), ("release_date", release_date)])


def protocol_parameters_from_text(text: str) -> "OrderedDict[str, List[OrderedDict]]":
    by_name: "OrderedDict[str, List[OrderedDict]]" = OrderedDict()
    formats = "|".join(PROTOCOL_FORMATS)
    row_re = re.compile(rf"^\s*(\d{{3,5}})\s+(.+?)\s+({formats})\b")
    for idx, line in enumerate(text.splitlines()):
        match = row_re.match(line)
        if not match:
            continue
        parameter_id, name, value_type = match.groups()
        name = compact_text(name)
        normalized_name = normalize_catalogue_name(name)
        if not normalized_name:
            continue
        entry = OrderedDict(
            [
                ("id", parameter_id),
                ("name", name),
                ("value_type", value_type),
                ("source_evidence", [source_evidence(f"{variant}_communication_protocol", PROTOCOL_DOC_NAME, f"line {idx + 1}")]),
            ]
        )
        by_name.setdefault(normalized_name, []).append(entry)
    return by_name


def protocol_parameters_from_ui_metadata(ui_metadata: Mapping[str, Any]) -> "OrderedDict[str, List[OrderedDict]]":
    by_name: "OrderedDict[str, List[OrderedDict]]" = OrderedDict()
    for context in ui_metadata.get("parameter_contexts", {}).values():
        protocol_ids = context.get("protocol_ids", [])
        if not protocol_ids:
            continue
        primary = str(context.get("primary_display_candidate", ""))
        candidates = list(context.get("display_candidates", []))
        if primary and primary not in candidates:
            candidates.insert(0, primary)
        for name in candidates:
            normalized = normalize_catalogue_name(name)
            if not normalized:
                continue
            for pid in protocol_ids:
                entry = OrderedDict([
                    ("id", pid),
                    ("name", name),
                    ("value_type", "UNKNOWN"),
                    ("source_evidence", [source_evidence("fallback_ui_metadata", "fallback", f"ID {pid}")]),
                ])
                by_name.setdefault(normalized, []).append(entry)
    return by_name


def eds_objects_by_leaf_name(eds: Mapping[str, Any]) -> "OrderedDict[str, List[OrderedDict]]":
    by_name: "OrderedDict[str, List[OrderedDict]]" = OrderedDict()
    for index, obj in eds.get("objects", {}).items():
        raw_name = str(obj.get("parameter_name", ""))
        leaf_name = raw_name.split(":")[-1].strip()
        normalized_name = normalize_catalogue_name(leaf_name)
        if not normalized_name:
            continue
        by_name.setdefault(normalized_name, []).append(
            OrderedDict(
                [
                    ("index", str(index)),
                    ("name", raw_name),
                    ("source_evidence", [source_evidence(f"{variant}_canopen_eds", EDS_NAME, f"[{index}]")]),
                ]
            )
        )
    return by_name


def annotate_ui_metadata_protocol_refs(
    ui_metadata: MutableMapping[str, Any],
    *,
    protocol_parameters: Mapping[str, List[Mapping[str, Any]]],
    eds_objects: Mapping[str, List[Mapping[str, Any]]],
) -> None:
    contexts = ui_metadata.get("parameter_contexts", {})
    for context in contexts.values():
        primary_name = normalize_catalogue_name(str(context.get("primary_display_candidate", "")))
        fallback_names = []
        for candidate in context.get("display_candidates", []):
            normalized = normalize_catalogue_name(str(candidate))
            if normalized and normalized not in fallback_names:
                fallback_names.append(normalized)

        normalized_names = [primary_name] if primary_name else []
        if primary_name and primary_name not in fallback_names:
            fallback_names.insert(0, primary_name)

        protocol_refs: List[Mapping[str, Any]] = []
        canopen_refs: List[Mapping[str, Any]] = []
        for normalized in normalized_names:
            protocol_refs.extend(protocol_parameters.get(normalized, []))
            canopen_refs.extend(eds_objects.get(normalized, []))
        if not protocol_refs and not canopen_refs:
            for normalized in fallback_names:
                protocol_refs.extend(protocol_parameters.get(normalized, []))
                canopen_refs.extend(eds_objects.get(normalized, []))

        protocol_ids = sorted({str(ref["id"]) for ref in protocol_refs})
        canopen_indices = sorted({str(ref["index"]) for ref in canopen_refs})
        if protocol_ids:
            status = "protocol_documented"
        elif canopen_indices:
            status = "canopen_documented"
        else:
            status = "service_software_only"
        context["protocol_status"] = status
        context["protocol_ids"] = protocol_ids
        context["canopen_indices"] = canopen_indices


def run_command_text(command: List[str]) -> Tuple[str, str]:
    result = subprocess.run(command, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    text, encoding = decode_text_bytes(result.stdout)
    return normalize_text(text), encoding


def harvest_pdf_text(path: Path) -> Tuple[str, str]:
    if shutil.which("pdftotext") is None:
        raise RuntimeError("pdftotext is required to harvest PDF text")
    return run_command_text(["pdftotext", "-layout", str(path), "-"])


def harvest_service_labels(path: Path) -> Tuple[List[str], str]:
    if shutil.which("strings") is None:
        raise RuntimeError("strings is required to harvest service-software labels")
    lines, output_encoding, _ = harvest_service_strings(path)
    labels = harvest_service_labels_from_lines(lines)
    return labels, output_encoding


def harvest_service_strings(path: Path) -> Tuple[List[str], str, str]:
    if shutil.which("strings") is None:
        raise RuntimeError("strings is required to harvest service-software UI metadata")
    best: Optional[Tuple[int, int, List[str], str, str]] = None
    for resource_encoding, args in SERVICE_RESOURCE_STRING_SCAN_CANDIDATES:
        text, output_encoding = run_command_text(["strings", *args, str(path)])
        lines = [normalize_text(line.strip()) for line in text.splitlines() if line.strip()]
        score = score_service_strings(lines)
        candidate = (score, len(lines), lines, output_encoding, resource_encoding)
        if best is None or candidate[:2] > best[:2]:
            best = candidate
    if best is None or best[0] <= 0:
        raise RuntimeError(f"no useful service-software UI strings found in {path}")
    _, _, lines, output_encoding, resource_encoding = best
    return lines, output_encoding, resource_encoding


def harvest_service_labels_from_lines(lines: Iterable[str]) -> List[str]:
    return sorted({line.strip() for line in lines if re.fullmatch(r"label_PAR_[A-Za-z0-9_]+", line.strip())})


def score_service_strings(lines: Iterable[str]) -> int:
    score = 0
    for line in lines:
        stripped = line.strip()
        if re.fullmatch(r"label_PAR_[A-Za-z0-9_]+", stripped):
            score += 20
        elif looks_like_parameter_path(stripped):
            score += 8
        elif is_control_identifier(stripped):
            score += 2
        elif any(fragment in stripped.lower() for fragment in ("feature key", "bootloader", "output enable", "do not")):
            score += 3
    return score


def is_control_identifier(text: str) -> bool:
    return CONTROL_ID_RE.fullmatch(text.strip()) is not None


def looks_like_parameter_path(text: str) -> bool:
    text = text.strip()
    if "." not in text:
        return False
    root = text.split(".", 1)[0]
    return root in PATH_ROOTS


def is_noise_string(text: str) -> bool:
    stripped = text.strip()
    if not stripped:
        return True
    if stripped.startswith(("<", "{", "$")) or stripped.endswith(("}", "{")):
        return True
    if "|*." in stripped:
        return True
    if any(stripped.startswith(prefix) for prefix in NOISE_PREFIXES):
        return True
    if any(stripped.lower().endswith(suffix) for suffix in NOISE_SUFFIXES):
        return True
    if re.fullmatch(r"[A-Fa-f0-9]{16,}", stripped):
        return True
    if re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]{24,}", stripped) and "_" in stripped:
        return True
    return False


def is_user_visible_string(text: str) -> bool:
    stripped = text.strip()
    if is_noise_string(stripped):
        return False
    if is_control_identifier(stripped):
        return False
    return True


def classify_ui_string(text: str) -> str:
    lowered = text.lower()
    if looks_like_parameter_path(text):
        return "parameter_path"
    if any(term in lowered for term in ("caution", "warning", "do not", "interrupt power", "damage")):
        return "safety_warning"
    if "license" in lowered:
        return "license"
    if "firmware" in lowered or "bootloader" in lowered:
        return "firmware"
    if "calibration" in lowered:
        return "calibration"
    if "characterization" in lowered:
        return "characterization"
    if "voltage limit" in lowered or "current limit" in lowered:
        return "limit"
    if "error" in lowered:
        return "error"
    if len(text) > 90 or lowered.startswith(("only ", "import ", "before proceeding", "this feature", "the ldd will")):
        return "note"
    return "label"


def note_kind(text: str) -> str:
    kind = classify_ui_string(text)
    if kind in {"label", "parameter_path"}:
        return "note"
    return kind


def is_noteish(text: str) -> bool:
    lowered = text.lower()
    if len(text) >= 70:
        return True
    return any(
        term in lowered
        for term in (
            "caution",
            "warning",
            "feature license",
            "requires feature license",
            "nominal voltage limits",
            "do not",
            "interrupt power",
            "firmware update error",
            "firmware identification",
            "feature firmware update limit",
            "import calibration",
            "import modeling",
            "only the",
            "the license file",
            "before proceeding",
            "characterization",
            "license file is bound",
            "not possible to install",
            "intermediate firmware",
            "serial number",
        )
    )


def continues_note_block(text: str) -> bool:
    if not is_user_visible_string(text):
        return False
    if looks_like_parameter_path(text):
        return False
    if re.fullmatch(r"[A-Za-z0-9 ./*+-]{1,34}", text) and not is_noteish(text):
        return False
    return True


def ui_note_id(text: str, used: MutableMapping[str, int]) -> str:
    base = re.sub(r"[^a-z0-9]+", "_", text.lower()).strip("_")[:56]
    if not base:
        base = "note"
    count = used.get(base, 0)
    used[base] = count + 1
    if count:
        return f"{base}_{count + 1}"
    return base


def nearby_display_candidates(lines: List[str], start: int, end: int) -> List[str]:
    candidates: List[str] = []
    for item in lines[start:end]:
        if not is_user_visible_string(item):
            continue
        if looks_like_parameter_path(item):
            continue
        if item not in candidates:
            candidates.append(item)
    return candidates


def primary_display_candidate(lines: List[str], index: int) -> str:
    start = max(0, index - 12)
    for item in reversed(lines[start:index]):
        if not is_user_visible_string(item):
            continue
        if looks_like_parameter_path(item):
            continue
        return item
    return ""


def nearest_control_context(lines: List[str], index: int) -> List[str]:
    tab = ""
    groups: List[str] = []
    start = max(0, index - 220)
    for pos in range(start, index):
        item = lines[pos]
        if item.startswith(("tabPage", "tab_")):
            for candidate in nearby_display_candidates(lines, pos + 1, min(index, pos + 6)):
                tab = candidate
                break
            continue
        if item.startswith("groupBox"):
            for candidate in nearby_display_candidates(lines, pos + 1, min(index, pos + 8)):
                groups.append(candidate)
                break
    stack: List[str] = []
    if tab:
        stack.append(tab)
    for group in groups[-3:]:
        if group not in stack:
            stack.append(group)
    return stack


def harvest_parameter_contexts(lines: List[str], labels: Iterable[str]) -> OrderedDict:
    label_set = set(labels)
    contexts: "OrderedDict[str, OrderedDict[str, Any]]" = OrderedDict()
    for idx, line in enumerate(lines):
        if line not in label_set:
            continue
        key = label_key_to_key(line)
        window_start = max(0, idx - 10)
        window_end = min(len(lines), idx + 8)
        display_candidates = nearby_display_candidates(lines, window_start, window_end)
        context_stack = nearest_control_context(lines, idx)
        neighbor_controls = []
        for item in lines[window_start:window_end]:
            if is_control_identifier(item) and item not in neighbor_controls:
                neighbor_controls.append(item)
        contexts[key] = OrderedDict(
            [
                ("key", key),
                ("label_key", line),
                ("primary_display_candidate", primary_display_candidate(lines, idx)),
                ("display_candidates", display_candidates),
                ("context_stack", context_stack),
                ("neighbor_controls", neighbor_controls),
                ("source_evidence", [source_evidence(SERVICE_SOURCE_ID, SERVICE_SOURCE_DETAIL, f"strings[{idx + 1}]")]),
            ]
        )
    return contexts


def harvest_parameter_paths(lines: List[str]) -> List[OrderedDict]:
    paths: List[OrderedDict] = []
    seen = set()
    for idx, line in enumerate(lines):
        if not looks_like_parameter_path(line):
            continue
        if line in seen:
            continue
        seen.add(line)
        paths.append(
            OrderedDict(
                [
                    ("path", line),
                    ("tree", [part.strip() for part in line.split(".") if part.strip()]),
                    ("source_evidence", [source_evidence(SERVICE_SOURCE_ID, SERVICE_SOURCE_DETAIL, f"strings[{idx + 1}]")]),
                ]
            )
        )
    return paths


def harvest_ui_strings(lines: List[str]) -> List[OrderedDict]:
    strings: List[OrderedDict] = []
    seen = set()
    for idx, line in enumerate(lines):
        if not is_user_visible_string(line):
            continue
        if line in seen:
            continue
        seen.add(line)
        strings.append(
            OrderedDict(
                [
                    ("text", line),
                    ("kind", classify_ui_string(line)),
                    ("source_evidence", [source_evidence(SERVICE_SOURCE_ID, SERVICE_SOURCE_DETAIL, f"strings[{idx + 1}]")]),
                ]
            )
        )
    return strings


def harvest_ui_notes(lines: List[str]) -> List[OrderedDict]:
    notes: List[OrderedDict] = []
    used_ids: "OrderedDict[str, int]" = OrderedDict()
    current: List[str] = []
    start_idx = 0

    def flush(end_idx: int) -> None:
        nonlocal current, start_idx
        if not current:
            return
        text = compact_text(" ".join(current), limit=900)
        if is_noteish(text):
            notes.append(
                OrderedDict(
                    [
                        ("id", ui_note_id(text, used_ids)),
                        ("kind", note_kind(text)),
                        ("text", text),
                        (
                            "source_evidence",
                            [source_evidence(SERVICE_SOURCE_ID, SERVICE_SOURCE_DETAIL, f"strings[{start_idx + 1}-{end_idx}]")],
                        ),
                    ]
                )
            )
        current = []

    for idx, line in enumerate(lines):
        if not is_user_visible_string(line):
            flush(idx)
            continue
        if not current:
            if is_noteish(line):
                current = [line]
                start_idx = idx
            continue
        if continues_note_block(line):
            current.append(line)
            continue
        flush(idx)
        if is_noteish(line):
            current = [line]
            start_idx = idx
    flush(len(lines))
    return notes


def harvest_ui_metadata(
    lines: List[str],
    labels: List[str],
    *,
    strings_output_encoding: str,
    resource_string_encoding: str,
) -> OrderedDict:
    return OrderedDict(
        [
            ("schema_version", f"mecom_{variant}_ui_metadata.v1"),
            ("definition", definition_ref()),
            ("source", SERVICE_SOFTWARE_NAME),
            ("resource_string_encoding", resource_string_encoding),
            ("strings_output_encoding", strings_output_encoding),
            ("parameter_contexts", harvest_parameter_contexts(lines, labels)),
            ("parameter_paths", harvest_parameter_paths(lines)),
            ("ui_notes", harvest_ui_notes(lines)),
            ("ui_strings", harvest_ui_strings(lines)),
        ]
    )


def split_release_note_items(lines: Iterable[str]) -> List[str]:
    items: List[str] = []
    current: List[str] = []
    for raw_line in lines:
        line = raw_line.strip()
        if not line:
            continue
        bullet = False
        if line.startswith("*"):
            line = line[1:].strip()
            bullet = True
        elif re.match(r"o\s+\S", line):
            line = re.sub(r"^o\s+", "", line).strip()
            bullet = True
        if bullet:
            if current:
                items.append(compact_text(" ".join(current)))
            current = [line]
        elif current:
            current.append(line)
    if current:
        items.append(compact_text(" ".join(current)))
    return items


def supported_devices(items: Iterable[str]) -> List[OrderedDict]:
    devices: List[OrderedDict] = []
    for item in items:
        match = re.match(r"(LDD-\d+): hardware version(?: range)? ([0-9.]+(?:\s*-\s*[0-9.]+)?)\.?", item)
        if not match:
            continue
        device, hw = match.groups()
        devices.append(OrderedDict([("device", device), ("hardware_version", compact_text(hw.rstrip(".")))]))
    return devices


def release_section_name(name: str) -> str:
    normalized = name.lower()
    if normalized == "supported devices":
        return "supported_devices"
    if normalized == "new and improved features":
        return "new_features"
    if normalized == "resolved issues":
        return "resolved_issues"
    if normalized == "known issues":
        return "known_issues"
    return normalized.replace(" ", "_")


def parse_release_blocks(text: str) -> List[OrderedDict]:
    lines = normalize_text(text).splitlines()
    starts: List[Tuple[int, str]] = []
    for idx, line in enumerate(lines):
        match = re.match(r"\s*\d+\s+(?:Current|Old) Release Notes, Version ([0-9]+(?:\.[0-9]+)*)\s*$", line)
        if match:
            starts.append((idx, match.group(1)))

    versions: List[OrderedDict] = []
    for pos, (start, version) in enumerate(starts):
        end = starts[pos + 1][0] if pos + 1 < len(starts) else len(lines)
        block_lines = lines[start:end]
        block_text = "\n".join(block_lines)
        version_info = OrderedDict([("version", version)])
        headline = re.search(
            r"(\d{4}-\d{2}-\d{2});\s+LDD(?:-\S+)? Service Software v?([0-9.]+)(?:\s*\(no change\))?;\s+LDD(?:-\S+)? Firmware v?([0-9.]+)\.",
            block_text,
        )
        if headline:
            version_info["release_date"] = headline.group(1)
            version_info["service_software_version"] = headline.group(2)
            version_info["firmware_version"] = headline.group(3)
        else:
            date_match = re.search(r"(\d{1,2}\s+[A-Za-z]+\s+\d{4})", block_text)
            software_match = re.search(r"LDD\s+Service\s+Software\s+Version\s+([0-9.]+)", block_text)
            firmware_match = re.search(r"LDD\s+(?:STM32|Firmware)\s+Version\s+([0-9.]+)", block_text)
            if date_match or software_match or firmware_match:
                release_date = ""
                if date_match:
                    parts = date_match.group(1).split()
                    if len(parts) == 3:
                        m = MONTHS.get(parts[1], "")
                        if m:
                            try:
                                release_date = f"{parts[2]}-{m}-{int(parts[0]):02d}"
                            except ValueError:
                                pass
                if not release_date and date_match:
                    release_date = date_match.group(1)
                
                version_info["release_date"] = release_date
                if software_match:
                    version_info["service_software_version"] = software_match.group(1)
                if firmware_match:
                    version_info["firmware_version"] = firmware_match.group(1)

        section_spans: List[Tuple[int, str]] = []
        for idx, line in enumerate(block_lines):
            match = re.match(r"\s*\d+\.\d+\s+(Supported Devices|New and Improved Features|Resolved Issues|Known Issues)\s*$", line)
            if match:
                section_spans.append((idx, release_section_name(match.group(1))))
        for idx, (section_start, section_key) in enumerate(section_spans):
            section_end = section_spans[idx + 1][0] if idx + 1 < len(section_spans) else len(block_lines)
            items = split_release_note_items(block_lines[section_start + 1 : section_end])
            if section_key == "supported_devices":
                version_info[section_key] = supported_devices(items)
            else:
                version_info[section_key] = items
        versions.append(version_info)
    return versions


def release_risk_notes(versions: Iterable[Mapping[str, Any]]) -> List[OrderedDict]:
    notes: List[OrderedDict] = []
    for version in versions:
        version_id = str(version.get("version", ""))
        resolved = " ".join(version.get("resolved_issues", []))
        new_features = " ".join(version.get("new_features", []))
        if version_id == "2.01" and "damage to the device was possible" in resolved:
            notes.append(
                OrderedDict(
                    [
                        ("id", "firmware_v200_canopen_damage_risk"),
                        ("version", version_id),
                        ("summary", compact_text("Firmware v2.00 could damage LDD-1303 devices when CANopen and LDD output were enabled together; fixed in v2.01.")),
                        ("source_evidence", [source_evidence(f"{variant}_release_notes", RELEASE_NOTES_NAME, "Version 2.01 Resolved Issues")]),
                    ]
                )
            )
        if version_id == "2.21" and "ID 2100" in resolved:
            notes.append(
                OrderedDict(
                    [
                        ("id", "output_enable_import_behavior_fixed"),
                        ("version", version_id),
                        ("summary", compact_text("Output Enable parameter ID 2100 is no longer set to ON during specified older-firmware imports.")),
                        ("source_evidence", [source_evidence(f"{variant}_release_notes", RELEASE_NOTES_NAME, "Version 2.21 Resolved Issues")]),
                    ]
                )
            )
        if version_id == "2.20" and "feature unlock system" in new_features:
            notes.append(
                OrderedDict(
                    [
                        ("id", "feedforward_lpc_feature_unlock_added"),
                        ("version", version_id),
                        ("summary", compact_text("Firmware and service software v2.20 introduced feature unlocks for Feedforward and LPC.")),
                        ("source_evidence", [source_evidence(f"{variant}_release_notes", RELEASE_NOTES_NAME, "Version 2.20 New and Improved Features")]),
                    ]
                )
            )
    return notes


def protocol_parameter_match(
    protocol_parameters: Mapping[str, List[Mapping[str, Any]]],
    name: str,
    parameter_id: str,
) -> Optional[Mapping[str, Any]]:
    for entry in protocol_parameters.get(normalize_catalogue_name(name), []):
        if str(entry.get("id", "")) == parameter_id:
            return entry
    return None


def eds_object_match(eds: Mapping[str, Any], index: str, name_fragment: str) -> Optional[Mapping[str, Any]]:
    obj = eds.get("objects", {}).get(index)
    if not obj:
        return None
    if normalize_catalogue_name(name_fragment) not in normalize_catalogue_name(str(obj.get("parameter_name", ""))):
        return None
    return obj


def ui_path_evidence(ui_metadata: Mapping[str, Any], path: str) -> List[str]:
    for entry in ui_metadata.get("parameter_paths", []):
        if entry.get("path") == path:
            return list(entry.get("source_evidence", []))
    return []


def ui_note_evidence(ui_metadata: Mapping[str, Any], fragment: str) -> List[str]:
    fragment_normalized = normalize_catalogue_name(fragment)
    for entry in ui_metadata.get("ui_notes", []):
        if fragment_normalized in normalize_catalogue_name(str(entry.get("text", ""))):
            return list(entry.get("source_evidence", []))
    return []


def ui_context_status(ui_metadata: Mapping[str, Any], key: str) -> str:
    context = ui_metadata.get("parameter_contexts", {}).get(key, {})
    return str(context.get("protocol_status", ""))


def text_line_evidence(text: str, source_id: str, logical_name: str, *fragments: str) -> List[str]:
    normalized_fragments = [normalize_catalogue_name(fragment) for fragment in fragments]
    for idx, line in enumerate(text.splitlines()):
        normalized_line = normalize_catalogue_name(line)
        if all(fragment in normalized_line for fragment in normalized_fragments):
            return [source_evidence(source_id, logical_name, f"line {idx + 1}")]
    return []


def default_key_evidence(defaults: Mapping[str, Any], key: str) -> List[str]:
    parameter = defaults.get("parameters", {}).get(key)
    if not parameter:
        return []
    return list(parameter.get("source_evidence", []))


def protocol_check(
    check_id: str,
    *,
    status: str,
    summary: str,
    source_evidence_items: Iterable[Iterable[str]],
    extra: Optional[Mapping[str, Any]] = None,
) -> OrderedDict:
    evidence: List[str] = []
    for items in source_evidence_items:
        for item in items:
            if item and item not in evidence:
                evidence.append(item)
    record = OrderedDict([("id", check_id), ("status", status), ("summary", summary), ("source_evidence", evidence)])
    if extra:
        for key, value in extra.items():
            record[key] = value
    return record


def documentation_cross_checks(
    *,
    protocol_text: str,
    protocol_encoding: str,
    protocol_path: Optional[Path],
    manual_text: str,
    manual_encoding: str,
    manual_path: Optional[Path],
    defaults: Mapping[str, Any],
    eds: Mapping[str, Any],
    ui_metadata: Mapping[str, Any],
    protocol_parameters: Mapping[str, List[Mapping[str, Any]]],
) -> OrderedDict:
    protocol_header = document_header(protocol_text, r"5260[A-Z]") if protocol_text else OrderedDict()
    manual_header = document_header(manual_text, r"5261[A-Z]") if manual_text else OrderedDict()
    output_enable_protocol = protocol_parameter_match(protocol_parameters, "Output Enable", "2100")
    min_voltage_protocol = protocol_parameter_match(protocol_parameters, "Min Nominal Voltage", "2125")
    feature_key_protocol = protocol_parameter_match(protocol_parameters, "Feature Key Store", "54000")

    output_enable_eds = eds_object_match(eds, "2230", "Output Enable")
    min_voltage_eds = eds_object_match(eds, "2255", "Min Nominal Voltage")

    checks = [
        protocol_check(
            "output_enable_protocol_mapping",
            status="matched" if output_enable_protocol and output_enable_eds else "missing",
            summary="Output Enable is MeCom ID 2100 in the LDD protocol and CANopen object 0x2230 in the EDS; keep the UI/default key LDD_OUTPUT_EN mapped to those documented protocol identities.",
            source_evidence_items=[
                output_enable_protocol.get("source_evidence", []) if output_enable_protocol else [],
                [source_evidence(f"{variant}_canopen_eds", EDS_NAME, "[2230]")] if output_enable_eds else [],
                default_key_evidence(defaults, "LDD_OUTPUT_EN"),
                ui_path_evidence(ui_metadata, "Operation.Input Source Selection.Output Enable"),
            ],
            extra={
                "default_key": "LDD_OUTPUT_EN",
                "mecom_id": "2100",
                "canopen_index": "2230",
                "ui_path": "Operation.Input Source Selection.Output Enable",
            },
        ),
        protocol_check(
            "min_nominal_voltage_protocol_mapping",
            status="matched" if min_voltage_protocol and min_voltage_eds else "missing",
            summary="Min Nominal Voltage is MeCom ID 2125 in the LDD protocol and CANopen object 0x2255 in the EDS; the manual warning text explains the overcurrent risk of setting the lower voltage limit too high.",
            source_evidence_items=[
                min_voltage_protocol.get("source_evidence", []) if min_voltage_protocol else [],
                [source_evidence(f"{variant}_canopen_eds", EDS_NAME, "[2255]")] if min_voltage_eds else [],
                default_key_evidence(defaults, "LDD_VOLTAGE_LIMIT_MIN"),
                ui_note_evidence(ui_metadata, "minimal nominal voltage limit"),
                text_line_evidence(manual_text, f"{variant}_user_manual", USER_MANUAL_NAME, "minimal voltage", "overcurrent"),
            ],
            extra={
                "default_key": "LDD_VOLTAGE_LIMIT_MIN",
                "mecom_id": "2125",
                "canopen_index": "2255",
                "ui_path": "Operation.Output Stage Limits.Min Nominal Voltage",
            },
        ),
        protocol_check(
            "feature_unlock_license_metadata",
            status="matched" if feature_key_protocol else "missing",
            summary="Feature unlock metadata is documented by the protocol Feature Licenses table; ServiceSoftware adds license guidance text that is useful as tooltip-style metadata.",
            source_evidence_items=[
                feature_key_protocol.get("source_evidence", []) if feature_key_protocol else [],
                text_line_evidence(protocol_text, f"{variant}_communication_protocol", PROTOCOL_DOC_NAME, "Feature Licenses"),
                text_line_evidence(manual_text, f"{variant}_user_manual", USER_MANUAL_NAME, "Feature Unlock"),
                ui_note_evidence(ui_metadata, "bound to a specific device"),
            ],
            extra={"mecom_id": "54000", "protocol_name": "Feature Key Store"},
        ),
        protocol_check(
            "bootloader_ignore_feature_fw_limit_service_only",
            status=ui_context_status(ui_metadata, "IGNORE_FEATURE_FIRMW_LIM"),
            summary="Ignore Feature FW Limit is present in the ServiceSoftware bootloader UI strings, but it is not documented as an LDD protocol parameter in 5260H; treat it as service-software UI metadata only.",
            source_evidence_items=[
                ui_path_evidence(ui_metadata, "ME.Bootloader.Ignore Feature FW Limit"),
                text_line_evidence(protocol_text, f"{variant}_communication_protocol", PROTOCOL_DOC_NAME, "Bootloader"),
            ],
            extra={"ui_key": "IGNORE_FEATURE_FIRMW_LIM", "ui_path": "ME.Bootloader.Ignore Feature FW Limit"},
        ),
    ]

    return OrderedDict(
        [
            (
                "protocol_document",
                OrderedDict(
                    [
                        ("source", PROTOCOL_DOC_NAME if protocol_path else ""),
                        ("document", protocol_header.get("document", "")),
                        ("release_date", protocol_header.get("release_date", "")),
                        ("text_encoding", protocol_encoding),
                    ]
                ),
            ),
            (
                "manual_document",
                OrderedDict(
                    [
                        ("source", USER_MANUAL_NAME if manual_path else ""),
                        ("document", manual_header.get("document", "")),
                        ("release_date", manual_header.get("release_date", "")),
                        ("text_encoding", manual_encoding),
                    ]
                ),
            ),
            ("checks", checks),
        ]
    )


def harvest_release_notes(path: Path) -> Tuple[OrderedDict, str]:
    if shutil.which("pdftotext") is None:
        raise RuntimeError("pdftotext is required to harvest LDD release notes")
    text, output_encoding = run_command_text(["pdftotext", "-layout", str(path), "-"])
    versions = parse_release_blocks(text)
    current = versions[0] if versions else OrderedDict()
    document = ""
    release_date = ""
    doc_match = re.search(r"Document\s+(5265[A-Z])\s+Release date:\s+(\d{1,2})\s+([A-Za-z]+)\s+(\d{4})", text)
    if doc_match:
        document = doc_match.group(1)
        month = MONTHS.get(doc_match.group(3), "")
        if month:
            release_date = f"{doc_match.group(4)}-{month}-{int(doc_match.group(2)):02d}"

    notes = OrderedDict(
        [
            ("document", document),
            ("release_date", release_date),
            ("current_version", current.get("version", "")),
            ("current_release_date", current.get("release_date", "")),
            ("current_service_software_version", current.get("service_software_version", "")),
            ("current_firmware_version", current.get("firmware_version", "")),
            ("supported_devices", current.get("supported_devices", [])),
            ("versions", versions),
            ("risk_notes", release_risk_notes(versions)),
        ]
    )
    return notes, output_encoding


def hidden_candidates(parameters: Mapping[str, Mapping[str, Any]]) -> List[OrderedDict]:
    candidates: List[OrderedDict] = []
    for key, param in sorted(parameters.items()):
        visibility = str(param.get("visibility", ""))
        if visibility != "advanced":
            continue
        candidates.append(
            OrderedDict(
                [
                    ("key", key),
                    ("label_key", str(param.get("label_key", ""))),
                    ("group", str(param.get("group", ""))),
                    ("visibility", visibility),
                    ("source_evidence", list(param.get("source_evidence", []))),
                ]
            )
        )
    return candidates


def source_record(
    source_id: str,
    logical_path: str,
    method: str,
    status: str,
    path: Optional[Path] = None,
    *,
    text_encoding: str = "",
    extra: Optional[Mapping[str, Any]] = None,
) -> OrderedDict:
    record = OrderedDict([("id", source_id), ("path", logical_path), ("method", method), ("status", status)])
    if path is not None:
        record["sha256"] = sha256(path)
        record["bytes"] = path.stat().st_size
    if text_encoding:
        record["text_encoding"] = text_encoding
    if extra:
        for key, value in extra.items():
            record[key] = value
    return record


def metadata_index(
    payload_paths: Mapping[str, Path],
    *,
    defaults: Mapping[str, Any],
    default_encoding: str,
    eds: Mapping[str, Any],
    eds_encoding: str,
    labels: List[str],
    label_output_encoding: str,
    service_resource_encoding: str,
    ui_metadata: Mapping[str, Any],
    release_notes: Mapping[str, Any],
    release_notes_encoding: str,
    documentation_checks: Mapping[str, Any],
    protocol_path: Optional[Path],
    protocol_encoding: str,
    manual_path: Optional[Path],
    manual_encoding: str,
    msi_path: Optional[Path],
    cab_path: Optional[Path],
) -> OrderedDict:
    default_parameters = defaults.get("parameters", {})
    default_label_keys = {param["label_key"] for param in default_parameters.values()}
    sources: List[OrderedDict] = []
    if msi_path is not None and msi_path.exists():
        sources.append(
            source_record(
                f"{variant}_software_msi",
                msi_path.name,
                "downloaded from Meerstetter customer-center software package",
                "available",
                msi_path,
            )
        )
    if cab_path is not None and cab_path.exists():
        sources.append(
            source_record(
                f"{variant}_payload_cab",
                cab_path.name,
                "extracted MSI cabinet stream",
                "available",
                cab_path,
            )
        )
    if payload_paths.get(DEFAULT_CONFIG_NAME):
        sources.append(
            source_record(
                f"{variant}_default_config",
                DEFAULT_CONFIG_NAME,
                "parsed [SRV_PAR] key/value defaults",
                "harvested",
                payload_paths[DEFAULT_CONFIG_NAME],
                text_encoding=default_encoding,
                extra={"parameter_count": len(default_parameters)},
            )
        )
    if payload_paths.get(EDS_NAME):
        sources.append(
            source_record(
                f"{variant}_canopen_eds",
                EDS_NAME,
                "parsed CANopen EDS object dictionary",
                "harvested",
                payload_paths[EDS_NAME],
                text_encoding=eds_encoding,
                extra={"object_count": len(eds.get("objects", {}))},
            )
        )
    if payload_paths.get(SERVICE_SOFTWARE_NAME):
        sources.append(
            source_record(
                SERVICE_SOURCE_ID,
                SERVICE_SOFTWARE_NAME,
                "resource string scan for UI label keys, parameter context, paths, and tooltip-like notes",
                "harvested",
                payload_paths[SERVICE_SOFTWARE_NAME],
                text_encoding=service_resource_encoding,
                extra={
                    "strings_output_encoding": label_output_encoding,
                    "label_count": len(labels),
                    "software_only_label_count": len([label for label in labels if label not in default_label_keys]),
                    "parameter_context_count": len(ui_metadata.get("parameter_contexts", {})),
                    "parameter_path_count": len(ui_metadata.get("parameter_paths", [])),
                    "ui_note_count": len(ui_metadata.get("ui_notes", [])),
                    "ui_string_count": len(ui_metadata.get("ui_strings", [])),
                },
            )
        )
    if ui_metadata:
        sources.append(
            source_record(
                f"{variant}_ui_metadata",
                UI_METADATA_NAME,
                "generated source JSON from ServiceSoftware resource strings",
                "generated",
                text_encoding="utf-8",
                extra={
                    "parameter_context_count": len(ui_metadata.get("parameter_contexts", {})),
                    "parameter_path_count": len(ui_metadata.get("parameter_paths", [])),
                    "ui_note_count": len(ui_metadata.get("ui_notes", [])),
                    "ui_string_count": len(ui_metadata.get("ui_strings", [])),
                },
            )
        )
    if payload_paths.get(RELEASE_NOTES_NAME):
        sources.append(
            source_record(
                f"{variant}_release_notes",
                RELEASE_NOTES_NAME,
                "pdftotext release-note parse",
                "harvested",
                payload_paths[RELEASE_NOTES_NAME],
                text_encoding=release_notes_encoding,
            )
        )
    if protocol_path is not None and protocol_path.exists():
        sources.append(
            source_record(
                f"{variant}_communication_protocol",
                PROTOCOL_DOC_NAME,
                "pdftotext protocol cross-check against generated LDD catalogue sources",
                "cross_checked",
                protocol_path,
                text_encoding=protocol_encoding,
            )
        )
    if manual_path is not None and manual_path.exists():
        sources.append(
            source_record(
                f"{variant}_user_manual",
                USER_MANUAL_NAME,
                "pdftotext manual cross-check for tooltip-like UI metadata",
                "cross_checked",
                manual_path,
                text_encoding=manual_encoding,
            )
        )
    if payload_paths.get(FIRMWARE_NAME):
        sources.append(
            source_record(
                f"{variant}_firmware_hex",
                FIRMWARE_NAME,
                "packaged firmware image; retained as provenance only",
                "available",
                payload_paths[FIRMWARE_NAME],
            )
        )
    if payload_paths.get(LOOKUP_CSV_NAME):
        sources.append(
            source_record(
                f"{variant}_lookup_table_example",
                LOOKUP_CSV_NAME,
                "packaged CSV example; retained as lookup table provenance",
                "available",
                payload_paths[LOOKUP_CSV_NAME],
                text_encoding=read_text(payload_paths[LOOKUP_CSV_NAME])[1],
            )
        )
    if payload_paths.get(LOOKUP_XLSX_NAME):
        sources.append(
            source_record(
                f"{variant}_lookup_table_example_xlsx",
                LOOKUP_XLSX_NAME,
                "packaged XLSX example; retained as lookup table provenance only",
                "available",
                payload_paths[LOOKUP_XLSX_NAME],
            )
        )

    return OrderedDict(
        [
            ("schema_version", f"mecom_{variant}_metadata_index.v1"),
            ("definition", definition_ref()),
            ("sources", sources),
            ("software_labels", labels),
            ("release_notes", release_notes),
            ("documentation_cross_checks", documentation_checks),
            ("hidden_candidates", hidden_candidates(default_parameters)),
            (
                "safety_policy",
                "Advanced LDD source entries are metadata candidates only; do not add them to active polling or write controls without a separate safety review.",
            ),
        ]
    )


def write_json(path: Path, payload: OrderedDict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, ensure_ascii=True) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--payload", required=True, help="Directory containing files extracted from the LDD MSI cabinet.")
    parser.add_argument("--out", default="mecom/catalogues/sources")
    parser.add_argument("--variant", default="ldd_130x", help="The LDD subfamily variant identifier.")
    parser.add_argument("--version", default="v221", help="The version of the payload.")
    parser.add_argument("--default-config-name", default="", help="Optional specific default config filename.")
    parser.add_argument("--eds-name", default="", help="Optional specific EDS filename.")
    parser.add_argument("--release-notes-name", default="", help="Optional specific release notes filename.")
    parser.add_argument("--service-software-name", default="", help="Optional specific service software filename.")
    parser.add_argument("--firmware-name", default="", help="Optional specific firmware filename.")
    parser.add_argument("--lookup-csv-name", default="", help="Optional specific lookup CSV filename.")
    parser.add_argument("--lookup-xlsx-name", default="", help="Optional specific lookup XLSX filename.")
    parser.add_argument("--msi", default="", help="Optional original MSI path for provenance hashing.")
    parser.add_argument("--cab", default="", help="Optional extracted cabinet path for provenance hashing.")
    parser.add_argument("--protocol-pdf", default="", help="Optional official Communication Protocol PDF for cross-checking.")
    parser.add_argument("--manual-pdf", default="", help="Optional official User Manual PDF for tooltip-style metadata cross-checking.")
    parser.add_argument("--fallback-ui-metadata", default="", help="Optional path to a previously generated UI metadata JSON to use as a fallback for protocol parameter mapping.")
    args = parser.parse_args()

    global variant, version, DEFINITION, PAYLOAD_SOURCE_NAME
    global DEFAULT_CONFIG_NAME, EDS_NAME, RELEASE_NOTES_NAME, SERVICE_SOFTWARE_NAME, FIRMWARE_NAME, LOOKUP_CSV_NAME, LOOKUP_XLSX_NAME, UI_METADATA_NAME
    global SERVICE_SOURCE_ID, SERVICE_SOURCE_DETAIL

    variant = args.variant
    version = args.version
    DEFINITION = OrderedDict(
        [
            ("definition_ref", f"meerstetter.{variant}.{version}"),
            ("system", "mecom"),
            ("family", "meerstetter"),
            ("sub_family", "ldd"),
            ("variant", variant),
            ("version", version),
        ]
    )
    PAYLOAD_SOURCE_NAME = f"{variant.upper().replace('_', '-')} Software {version} MSI payload"

    payload_dir = Path(args.payload)
    out = Path(args.out)

    default_config_path = payload_file(payload_dir, args.default_config_name, required=True, patterns=["*Default*Config*.ini", "*Default*.ini", "*.ini", "*Default*Config*.xml", "*Default*.xml", "*.xml"])
    eds_path = payload_file(payload_dir, args.eds_name, required=False, patterns=["*CanOpen.eds", "*.eds"])
    release_notes_path = payload_file(payload_dir, args.release_notes_name, required=True, patterns=["*Release*Notes*.pdf", "*Release*.pdf"])
    service_software_path = payload_file(payload_dir, args.service_software_name, required=True, patterns=["*Service*.exe", "Service*.exe"])
    firmware_path = payload_file(payload_dir, args.firmware_name, required=False, patterns=["*.hex", "*.mcs"])
    lookup_csv_path = payload_file(payload_dir, args.lookup_csv_name, required=False, patterns=["*Lookup*.csv", "*.csv"])
    lookup_xlsx_path = payload_file(payload_dir, args.lookup_xlsx_name, required=False, patterns=["*Lookup*.xlsx", "*.xlsx"])

    DEFAULT_CONFIG_NAME = default_config_path.name if default_config_path else ""
    EDS_NAME = eds_path.name if eds_path else ""
    RELEASE_NOTES_NAME = release_notes_path.name if release_notes_path else ""
    SERVICE_SOFTWARE_NAME = service_software_path.name if service_software_path else ""
    FIRMWARE_NAME = firmware_path.name if firmware_path else ""
    LOOKUP_CSV_NAME = lookup_csv_path.name if lookup_csv_path else ""
    LOOKUP_XLSX_NAME = lookup_xlsx_path.name if lookup_xlsx_path else ""
    UI_METADATA_NAME = f"{variant}_ui_metadata.{version}.json"

    SERVICE_SOURCE_ID = f"{variant}_service_software"
    SERVICE_SOURCE_DETAIL = f"{SERVICE_SOFTWARE_NAME} managed resource strings"

    config_suffix = ""
    if DEFAULT_CONFIG_NAME:
        first_word = DEFAULT_CONFIG_NAME.split()[0]
        if re.match(r"^\d+[A-Za-z]+$", first_word):
            config_suffix = first_word.lower()
    if not config_suffix:
        config_suffix = "config"

    paths = {
        DEFAULT_CONFIG_NAME: default_config_path,
        EDS_NAME: eds_path,
        RELEASE_NOTES_NAME: release_notes_path,
        SERVICE_SOFTWARE_NAME: service_software_path,
        FIRMWARE_NAME: firmware_path,
        LOOKUP_CSV_NAME: lookup_csv_path,
        LOOKUP_XLSX_NAME: lookup_xlsx_path,
    }

    defaults, default_encoding = harvest_default_config(default_config_path)
    if eds_path:
        eds, eds_encoding = harvest_eds(eds_path)
    else:
        eds, eds_encoding = OrderedDict([("objects", OrderedDict())]), ""

    if default_config_path.suffix.lower() == ".xml":
        service_lines = []
        label_output_encoding = "utf-8"
        service_resource_encoding = "utf-8"
        labels = []
        
        parameter_contexts = OrderedDict()
        for key, p in defaults.get("parameters", {}).items():
            name_path = p.get("name_path", "")
            parts = [part.strip() for part in name_path.split("/") if part.strip()]
            if len(parts) > 1:
                primary_display_candidate = parts[-1]
                context_stack = parts[1:-1]
            else:
                primary_display_candidate = parts[0] if parts else key.replace("_", " ").title()
                context_stack = []
            
            parameter_contexts[key] = OrderedDict(
                [
                    ("key", key),
                    ("label_key", p["label_key"]),
                    ("primary_display_candidate", primary_display_candidate),
                    ("display_candidates", [primary_display_candidate]),
                    ("context_stack", context_stack),
                    ("neighbor_controls", []),
                    ("source_evidence", p["source_evidence"]),
                    ("protocol_status", "protocol_documented"),
                    ("protocol_ids", [p["mepar_id"]]),
                    ("canopen_indices", []),
                ]
            )

        ui_metadata = OrderedDict(
            [
                ("schema_version", f"mecom_{variant}_ui_metadata.v1"),
                ("definition", definition_ref()),
                ("source", SERVICE_SOFTWARE_NAME),
                ("resource_string_encoding", service_resource_encoding),
                ("strings_output_encoding", label_output_encoding),
                ("parameter_contexts", parameter_contexts),
                ("parameter_paths", []),
                ("ui_notes", []),
                ("ui_strings", []),
            ]
        )
    else:
        service_lines, label_output_encoding, service_resource_encoding = harvest_service_strings(service_software_path)
        labels = harvest_service_labels_from_lines(service_lines)
        ui_metadata = harvest_ui_metadata(
            service_lines,
            labels,
            strings_output_encoding=label_output_encoding,
            resource_string_encoding=service_resource_encoding,
        )
    release_notes, release_notes_encoding = harvest_release_notes(release_notes_path)
    protocol_path = Path(args.protocol_pdf) if args.protocol_pdf else None
    manual_path = Path(args.manual_pdf) if args.manual_pdf else None
    protocol_text = ""
    protocol_encoding = ""
    manual_text = ""
    manual_encoding = ""
    protocol_parameters: "OrderedDict[str, List[OrderedDict]]" = OrderedDict()
    if protocol_path is not None:
        protocol_text, protocol_encoding = harvest_pdf_text(protocol_path)
        protocol_parameters = protocol_parameters_from_text(protocol_text)
    
    fallback_ui_metadata_path = Path(args.fallback_ui_metadata) if args.fallback_ui_metadata else None
    if fallback_ui_metadata_path is not None and fallback_ui_metadata_path.exists():
        fallback_metadata = json.loads(fallback_ui_metadata_path.read_text(encoding="utf-8"))
        fallback_params = protocol_parameters_from_ui_metadata(fallback_metadata)
        for name, entries in fallback_params.items():
            if name not in protocol_parameters:
                protocol_parameters[name] = []
            for entry in entries:
                if not any(e["id"] == entry["id"] for e in protocol_parameters[name]):
                    protocol_parameters[name].append(entry)

    if manual_path is not None:
        manual_text, manual_encoding = harvest_pdf_text(manual_path)
    if default_config_path.suffix.lower() != ".xml":
        annotate_ui_metadata_protocol_refs(
            ui_metadata,
            protocol_parameters=protocol_parameters,
            eds_objects=eds_objects_by_leaf_name(eds),
        )
    doc_checks = documentation_cross_checks(
        protocol_text=protocol_text,
        protocol_encoding=protocol_encoding,
        protocol_path=protocol_path,
        manual_text=manual_text,
        manual_encoding=manual_encoding,
        manual_path=manual_path,
        defaults=defaults,
        eds=eds,
        ui_metadata=ui_metadata,
        protocol_parameters=protocol_parameters,
    )

    write_json(out / f"{variant}_default_config_{config_suffix}.{version}.json", defaults)
    if eds_path:
        write_json(out / f"{variant}_canopen_eds.{version}.json", eds)
    write_json(out / f"{variant}_ui_metadata.{version}.json", ui_metadata)
    write_json(
        out / f"{variant}_metadata_index.{version}.json",
        metadata_index(
            paths,
            defaults=defaults,
            default_encoding=default_encoding,
            eds=eds,
            eds_encoding=eds_encoding,
            labels=labels,
            label_output_encoding=label_output_encoding,
            service_resource_encoding=service_resource_encoding,
            ui_metadata=ui_metadata,
            release_notes=release_notes,
            release_notes_encoding=release_notes_encoding,
            documentation_checks=doc_checks,
            protocol_path=protocol_path,
            protocol_encoding=protocol_encoding,
            manual_path=manual_path,
            manual_encoding=manual_encoding,
            msi_path=Path(args.msi) if args.msi else None,
            cab_path=Path(args.cab) if args.cab else None,
        ),
    )


if __name__ == "__main__":
    main()
