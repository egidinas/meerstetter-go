#!/usr/bin/env python3
import json
import argparse
from pathlib import Path

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--variant", default="ldd_130x")
    parser.add_argument("--version", default="v221")
    parser.add_argument("--out", default="")
    parser.add_argument("--fallback-sdo-map", default="", help="Optional SDO map to use as fallback for mecom_id resolution by canopen index.")
    args = parser.parse_args()

    variant = args.variant
    version = args.version

    ROOT = Path(__file__).resolve().parents[1]
    SOURCES = ROOT / "mecom" / "catalogues" / "sources"
    
    ui_metadata_file = SOURCES / f"{variant}_ui_metadata.{version}.json"
    eds_file = SOURCES / f"{variant}_canopen_eds.{version}.json"
    
    # Check if EDS file exists. If it doesn't, CANopen is not supported for this device.
    if not eds_file.exists():
        print(f"Skipping SDO map generation: {eds_file} does not exist (not applicable for {variant})")
        return

    out_file = Path(args.out) if args.out else SOURCES / f"{variant}_canopen_sdo_map.{version}.json"

    # Load UI metadata
    with ui_metadata_file.open("r", encoding="utf-8") as f:
        ui_metadata = json.load(f)
    
    # Load CANopen EDS JSON
    with eds_file.open("r", encoding="utf-8") as f:
        eds = json.load(f)
    
    eds_objects = eds.get("objects", {})
    
    fallback_canopen_to_mecom = {}
    if args.fallback_sdo_map:
        fallback_path = Path(args.fallback_sdo_map)
        if fallback_path.exists():
            with fallback_path.open("r", encoding="utf-8") as f:
                fallback_data = json.load(f)
                for mapping in fallback_data.get("mappings", []):
                    canopen_idx = mapping.get("canopen", {}).get("index", "")
                    mecom_id = mapping.get("mecom_id")
                    if canopen_idx and mecom_id is not None:
                        idx_hex = canopen_idx.lower()
                        if idx_hex.startswith("0x"):
                            idx_hex = idx_hex[2:]
                        fallback_canopen_to_mecom[idx_hex.zfill(4)] = mecom_id
    
    mappings = []
    seen_mecom_ids = set()

    contexts = ui_metadata.get("parameter_contexts", {})
    
    for key, ctx in contexts.items():
        protocol_ids = ctx.get("protocol_ids") or []
        canopen_indices = ctx.get("canopen_indices") or []
        
        if not protocol_ids and not canopen_indices:
            continue
            
        mecom_id = None
        if protocol_ids:
            try:
                mecom_id = int(protocol_ids[0])
            except ValueError:
                pass
                
        if mecom_id is None and canopen_indices:
            canopen_hex_check = canopen_indices[0].strip().lower()
            if canopen_hex_check.startswith("0x"):
                canopen_hex_check = canopen_hex_check[2:]
            canopen_hex_check = canopen_hex_check.zfill(4)
            mecom_id = fallback_canopen_to_mecom.get(canopen_hex_check)
            
        if mecom_id is None:
            continue
            
        if mecom_id in seen_mecom_ids:
            continue
            
        canopen_hex = canopen_indices[0].strip().upper()
        if canopen_hex.startswith("0X"):
            canopen_hex = canopen_hex[2:]
            
        # Normalize to 4 character hex string
        canopen_hex = canopen_hex.zfill(4)
        
        # Look up in EDS (EDS keys are lowercase/uppercase hex, let's try matching case-insensitively)
        eds_obj = None
        for k, v in eds_objects.items():
            if k.upper() == canopen_hex:
                eds_obj = v
                break
                
        if not eds_obj:
            print(f"Warning: CANopen index 0x{canopen_hex} (MeCom ID {mecom_id}) not found in EDS")
            continue
            
        name = ctx.get("primary_display_candidate") or eds_obj.get("parameter_name") or key
        
        # Determine value type & data type code
        eds_data_type = eds_obj.get("data_type") or ""
        subobjects = eds_obj.get("subobjects", {})
        
        # If there's a subobject "1" (Instance 1), inspect its data/access type
        sub1 = subobjects.get("1") or {}
        sub0 = subobjects.get("0") or {}
        
        data_type_code = eds_data_type or sub1.get("data_type") or sub0.get("data_type") or "0x0004"
        access_type = eds_obj.get("access_type") or sub1.get("access_type") or sub0.get("access_type") or "ro"
        
        # Map CANopen data type code to mecom value_type
        # 0x0004 -> int32, 0x0008 -> float32, 0x0007 -> int32 (uint32 in CANopen)
        dt_lower = data_type_code.lower()
        if "0x0008" in dt_lower:
            value_type = "float32"
            dt_out = "0x0008"
        elif "0x0004" in dt_lower or "0x0007" in dt_lower or "0x0005" in dt_lower:
            value_type = "int32"
            dt_out = "0x0004"
        else:
            value_type = "int32"
            dt_out = "0x0004"
            
        access = "rw" if "w" in access_type.lower() else "ro"
        
        # Determine instances configuration
        if subobjects and "1" in subobjects:
            instances = {
                "mode": "subindex",
                "min": 1,
                "max": 255
            }
            canopen = {
                "index": f"0x{canopen_hex}",
                "subindex": "instance",
                "subindex_mode": "instance",
                "data_type": dt_out
            }
        else:
            instances = {
                "mode": "fixed",
                "fixed": 1
            }
            canopen = {
                "index": f"0x{canopen_hex}",
                "subindex": "0x00",
                "subindex_mode": "fixed",
                "data_type": dt_out
            }
            
        aliases = [
            {"space": "canopen_index", "id": f"0x{canopen_hex}"},
            {"space": "canopen_object_decimal", "id": int(canopen_hex, 16)}
        ]
        
        mappings.append({
            "mecom_id": mecom_id,
            "name": name,
            "value_type": value_type,
            "access": access,
            "instances": instances,
            "canopen": canopen,
            "aliases": aliases,
            "source_evidence": [
                f"catalogues/sources/{variant}_ui_metadata.{version}.json#parameter_contexts.{key}",
                f"catalogues/sources/{variant}_canopen_eds.{version}.json#objects.{canopen_hex}"
            ]
        })
        seen_mecom_ids.add(mecom_id)
        
    # Sort mappings by MeCom ID
    mappings.sort(key=lambda x: x["mecom_id"])
    
    sdo_map = {
        "schema_version": f"mecom_{variant}_canopen_sdo_map.v1",
        "source_policy": "Runtime CANopen SDO routing uses MeCom parameter IDs as the primary key. CANopen object indexes and decimal object IDs are aliases, not replacement MeCom IDs.",
        "mappings": mappings,
        "bridge_transforms": []
    }
    
    with out_file.open("w", encoding="utf-8") as f:
        json.dump(sdo_map, f, indent=2)
        
    print(f"Generated {len(mappings)} mappings to {out_file}")

if __name__ == "__main__":
    main()
