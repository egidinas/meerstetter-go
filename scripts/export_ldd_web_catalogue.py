#!/usr/bin/env python3
"""Export harvested LDD-130x definition sources into the web dictionary shape."""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SOURCES = ROOT / "mecom" / "catalogues" / "sources"


def load_json(name: str) -> dict:
    if not name:
        return {}
    path = SOURCES / name
    if not path.exists():
        return {}
    with path.open("r", encoding="utf-8") as fh:
        return json.load(fh)


def compact(parts):
    return [str(part).strip() for part in parts if str(part or "").strip()]


def first(values, default=""):
    for value in values or []:
        value = str(value or "").strip()
        if value:
            return value
    return default


def unit_from_label(label: str) -> str:
    matches = re.findall(r"\[([^\]]+)\]", label or "")
    if not matches:
        return ""
    unit = matches[-1].strip()
    return {"degC": "degC", "\u00b0C": "degC", "rpm": "rpm"}.get(unit, unit)


def canopen_hex(raw: str) -> str:
    value = str(raw).strip().upper()
    if value.startswith("0X"):
        value = value[2:]
    return "0x" + value


def value_type(default_param: dict | None, ctx: dict | None) -> str:
    kind = str((default_param or {}).get("value_kind") or "").lower()
    if kind in {"integer", "bool", "boolean"}:
        return "int32"
    if kind in {"number", "float"}:
        return "float32"
    if kind == "text":
        return "string"
    label = str((ctx or {}).get("primary_display_candidate") or "")
    if unit_from_label(label):
        return "float32"
    return "string"


def role_for(default_param: dict | None, ctx: dict | None) -> str:
    status = str((ctx or {}).get("protocol_status") or "")
    if status == "service_software_only":
        return "metadata"
    visibility = str((default_param or {}).get("visibility") or "")
    if visibility == "operator":
        return "control"
    return "monitor"


def kind_for(default_param: dict | None, ctx: dict | None) -> str:
    status = str((ctx or {}).get("protocol_status") or "")
    if status == "service_software_only":
        return "metadata"
    if value_type(default_param, ctx) == "string":
        return "metadata"
    return "continuous"


def parse_candidate_ids(ctx: dict) -> list[tuple[int, str, str]]:
    out = []
    for raw in ctx.get("protocol_ids") or []:
        try:
            out.append((int(str(raw), 10), "mecom", str(raw)))
        except ValueError:
            pass
    for raw in ctx.get("canopen_indices") or []:
        try:
            out.append((int(str(raw), 16), "canopen", str(raw)))
        except ValueError:
            pass
    return out


def row_id(ctx: dict, used: set[int], synthetic: list[int]) -> int:
    for candidate, _, _ in parse_candidate_ids(ctx):
        if candidate > 0 and candidate not in used:
            used.add(candidate)
            return candidate
    while synthetic[0] in used:
        synthetic[0] += 1
    candidate = synthetic[0]
    synthetic[0] += 1
    used.add(candidate)
    return candidate


def cross_checks_by_key(checks: list[dict]) -> dict[str, dict]:
    out = {}
    for check in checks:
        for key in (check.get("default_key"), check.get("ui_key")):
            if key:
                out[str(key)] = check
    return out


def protocol_aliases(ctx: dict, check: dict | None) -> dict:
    aliases = {}
    protocol_ids = [str(item) for item in ctx.get("protocol_ids") or [] if str(item).strip()]
    canopen_indices = [str(item) for item in ctx.get("canopen_indices") or [] if str(item).strip()]
    if protocol_ids:
        try:
            aliases["mecom_parameter_id"] = int(protocol_ids[0])
        except ValueError:
            aliases["mecom_parameter_id"] = protocol_ids[0]
        aliases["mecom_parameter_ids"] = protocol_ids
    elif check and check.get("mecom_id"):
        try:
            aliases["mecom_parameter_id"] = int(str(check["mecom_id"]))
        except ValueError:
            aliases["mecom_parameter_id"] = str(check["mecom_id"])
    if canopen_indices:
        aliases["canopen_index"] = canopen_hex(canopen_indices[0])
        aliases["canopen_indices"] = [canopen_hex(item) for item in canopen_indices]
        try:
            aliases["canopen_object_decimal"] = int(canopen_indices[0], 16)
        except ValueError:
            pass
    elif check and check.get("canopen_index"):
        raw = str(check["canopen_index"])
        aliases["canopen_index"] = canopen_hex(raw)
    aliases["source_map"] = []
    return aliases


def help_text(label: str, ctx: dict, default_param: dict | None, check: dict | None, safety_policy: str) -> str:
    lines = []
    if label:
        lines.append(label)
    if ctx.get("context_stack"):
        lines.append("Context: " + " / ".join(compact(ctx["context_stack"])))
    if ctx.get("protocol_status"):
        lines.append("Protocol status: " + str(ctx["protocol_status"]))
    ids = []
    if ctx.get("protocol_ids"):
        ids.append("MeCom " + ", ".join(str(item) for item in ctx["protocol_ids"]))
    if ctx.get("canopen_indices"):
        ids.append("CANopen " + ", ".join(canopen_hex(str(item)) for item in ctx["canopen_indices"]))
    if ids:
        lines.append("; ".join(ids))
    if check and check.get("summary"):
        lines.append(str(check["summary"]))
    if default_param and default_param.get("safety_note"):
        lines.append(str(default_param["safety_note"]))
    elif ctx.get("protocol_status") == "service_software_only" and safety_policy:
        lines.append(safety_policy)
    return " ".join(lines)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--variant", default="ldd_130x")
    parser.add_argument("--version", default="v221")
    parser.add_argument("--out", default="")
    args = parser.parse_args()

    variant = args.variant
    version = args.version
    group_name = variant.replace("_", "-").upper()
    bundle_name = f"meerstetter.{variant}.{version}"

    default_config_files = list(SOURCES.glob(f"{variant}_default_config_*.{version}.json"))
    default_config_name = default_config_files[0].name if default_config_files else ""

    defaults = load_json(default_config_name)
    ui = load_json(f"{variant}_ui_metadata.{version}.json")
    index = load_json(f"{variant}_metadata_index.{version}.json")
    pdf_meta = load_json("pdf_extracted_metadata.json")
    ldd_meta = pdf_meta.get("ldd", {})
    general_meta = pdf_meta.get("general", {})
    cross_checks = (index.get("documentation_cross_checks") or {}).get("checks") or []
    checks_by_key = cross_checks_by_key(cross_checks)
    definition = ui.get("definition") or defaults.get("definition") or {}
    default_params = defaults.get("parameters") or {}
    contexts = ui.get("parameter_contexts") or {}
    keys = sorted(set(default_params) | set(contexts))
    used: set[int] = set()
    synthetic = [930000]
    rows = []

    for key in keys:
        ctx = contexts.get(key) or {
            "key": key,
            "label_key": (default_params.get(key) or {}).get("label_key", ""),
            "primary_display_candidate": key.replace("_", " ").title(),
            "display_candidates": [],
            "context_stack": [],
            "neighbor_controls": [],
            "source_evidence": [],
            "protocol_status": "default_config_only",
            "protocol_ids": [],
            "canopen_indices": [],
        }
        default_param = default_params.get(key)
        check = checks_by_key.get(key)
        label = first([ctx.get("primary_display_candidate"), key.replace("_", " ").title()])
        protocol_status = str(ctx.get("protocol_status") or "software_label")
        group_path = compact(["Meerstetter", "LDD", group_name, *(ctx.get("context_stack") or []), label])
        entry_id = row_id(ctx, used, synthetic)
        metadata = {
            "definition_ref": definition.get("definition_ref"),
            "definition_system": definition.get("system"),
            "definition_family": definition.get("family"),
            "definition_sub_family": definition.get("sub_family"),
            "definition_variant": definition.get("variant"),
            "definition_version": definition.get("version"),
            "ldd_key": key,
            "label_key": ctx.get("label_key") or (default_param or {}).get("label_key"),
            "protocol_status": protocol_status,
            "source_text_encoding": ui.get("strings_output_encoding"),
            "resource_string_encoding": ui.get("resource_string_encoding"),
        }
        if ctx.get("protocol_ids"):
            metadata["protocol_ids"] = ",".join(str(item) for item in ctx["protocol_ids"])
        if ctx.get("canopen_indices"):
            metadata["canopen_indices"] = ",".join(canopen_hex(str(item)) for item in ctx["canopen_indices"])
        if ctx.get("context_stack"):
            metadata["context_stack"] = " / ".join(compact(ctx["context_stack"]))
        if default_param and default_param.get("default_value_text") is not None:
            metadata["default_value"] = str(default_param["default_value_text"])
        if check:
            metadata["documentation_cross_check"] = check.get("id")
            metadata["documentation_cross_check_status"] = check.get("status")
        role = role_for(default_param, ctx)
        
        pdf_enrichment = {}
        if ctx.get("protocol_ids"):
            pid = str(ctx["protocol_ids"][0])
            if pid in ldd_meta:
                pdf_enrichment = ldd_meta[pid]
            elif pid in general_meta:
                pdf_enrichment = general_meta[pid]

        row = {
            "id": entry_id,
            "definition_ref": definition.get("definition_ref"),
            "sid": key.lower(),
            "name": label,
            "raw_name": key,
            "unit": unit_from_label(label),
            "type": value_type(default_param, ctx),
            "kind": kind_for(default_param, ctx),
            "role": role,
            "group": group_name,
            "subgroup": first(ctx.get("context_stack"), "Definition metadata"),
            "access": "metadata",
            "source_status": protocol_status,
            "visibility": (default_param or {}).get("visibility") or "advanced",
            "writable": False,
            "readout_priority": "metadata",
            "preferred_readout": "definition_catalogue",
            "applicableModes": ["ldd"] if protocol_status != "service_software_only" else ["metadata"],
            "help_text": (pdf_enrichment.get("Description") + " " + help_text(label, ctx, default_param, check, index.get("safety_policy", ""))).strip() if pdf_enrichment.get("Description") else help_text(label, ctx, default_param, check, index.get("safety_policy", "")),
            "hover_help": (pdf_enrichment.get("Description") + " " + help_text(label, ctx, default_param, check, index.get("safety_policy", ""))).strip() if pdf_enrichment.get("Description") else help_text(label, ctx, default_param, check, index.get("safety_policy", "")),
            "value_range": pdf_enrichment.get("ValueRange") or "",
            "mecom_format": pdf_enrichment.get("Format") or "",
            "safety_note": (default_param or {}).get("safety_note") or (index.get("safety_policy") if protocol_status == "service_software_only" else ""),
            "source_evidence": compact([*(default_param or {}).get("source_evidence", []), *ctx.get("source_evidence", []), *(check or {}).get("source_evidence", [])]),
            "protocol_aliases": protocol_aliases(ctx, check),
            "tree_path": {
                "id": f"{group_name.lower()}-ui",
                "label": group_name,
                "path": group_path,
                "default": True,
                "bundle": bundle_name,
                "instance_scope": "device",
                "sort": 20,
            },
            "tree_paths": [
                {
                    "id": f"{group_name.lower()}-ui",
                    "label": group_name,
                    "path": group_path,
                    "default": True,
                    "bundle": bundle_name,
                    "instance_scope": "device",
                    "sort": 20,
                },
                {
                    "id": "protocol",
                    "label": "LDD protocol",
                    "path": compact(["LDD protocol", first(ctx.get("protocol_ids"), "software metadata"), label]),
                    "default": False,
                    "bundle": bundle_name,
                    "instance_scope": "device",
                    "sort": 90,
                },
            ],
            "metadata": {key: value for key, value in metadata.items() if value not in (None, "")},
        }
        if default_param and default_param.get("default_value_text") is not None:
            row["default_value"] = str(default_param["default_value_text"])
        rows.append(row)

    feature_check = next((check for check in cross_checks if check.get("id") == "feature_unlock_license_metadata"), None)
    if feature_check and 54000 not in used:
        rows.append({
            "id": 54000,
            "definition_ref": definition.get("definition_ref"),
            "sid": "ldd_feature_key_store",
            "name": feature_check.get("protocol_name", "Feature Key Store"),
            "raw_name": "FEATURE_KEY_STORE",
            "unit": "",
            "type": "string",
            "kind": "metadata",
            "role": "metadata",
            "group": group_name,
            "subgroup": "Feature Licenses",
            "access": "metadata",
            "source_status": "protocol_documented",
            "visibility": "advanced",
            "writable": False,
            "readout_priority": "metadata",
            "preferred_readout": "definition_catalogue",
            "applicableModes": ["ldd"],
            "help_text": feature_check.get("summary", ""),
            "hover_help": feature_check.get("summary", ""),
            "source_evidence": feature_check.get("source_evidence", []),
            "protocol_aliases": {"mecom_parameter_id": 54000, "source_map": []},
            "tree_path": {
                "id": f"{group_name.lower()}-ui",
                "label": group_name,
                "path": ["Meerstetter", "LDD", group_name, "Feature Licenses", "Feature Key Store"],
                "default": True,
                "bundle": bundle_name,
                "instance_scope": "device",
                "sort": 20,
            },
            "metadata": {
                "definition_ref": definition.get("definition_ref"),
                "definition_system": definition.get("system"),
                "definition_family": definition.get("family"),
                "definition_sub_family": definition.get("sub_family"),
                "definition_variant": definition.get("variant"),
                "definition_version": definition.get("version"),
                "protocol_status": "protocol_documented",
                "documentation_cross_check": feature_check.get("id"),
                "documentation_cross_check_status": feature_check.get("status"),
                "source_text_encoding": ui.get("strings_output_encoding"),
                "resource_string_encoding": ui.get("resource_string_encoding"),
            },
        })

    rows.sort(key=lambda item: (item["group"], item["subgroup"], int(item["id"]), item["raw_name"]))
    out_file = Path(args.out) if args.out else ROOT / "web" / "src" / "data" / f"mecom-{variant.replace('_', '-')}-catalogue.json"
    out_file.write_text(json.dumps(rows, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"wrote {out_file.relative_to(ROOT)} with {len(rows)} {group_name} rows")


if __name__ == "__main__":
    main()
