#!/usr/bin/env python3
import pdfplumber
import json
import glob
import os
import re

def parse_pdf(path):
    print(f"Parsing {path}...")
    metadata = {}
    with pdfplumber.open(path) as pdf:
        for page in pdf.pages:
            tables = page.extract_tables()
            if not tables:
                continue
            for table in tables:
                if not table or len(table) < 2:
                    continue
                header = [str(c).strip().replace("\n", " ") if c else "" for c in table[0]]
                if not header or header[0] != "ID":
                    continue
                
                # We found a parameter table!
                for row in table[1:]:
                    if not row or not row[0]:
                        continue
                    
                    param_id_str = str(row[0]).strip().replace("\n", "")
                    # Ensure it's a numeric ID
                    if not param_id_str.isdigit():
                        continue
                        
                    param_id = int(param_id_str)
                    
                    # Some tables have 5 cols, some have 6 (like Instance).
                    # 'ID', 'Instance', 'Name', 'Format', 'Value Range', 'Description'
                    # Or 'ID', 'Name', 'Format', 'Value Range', 'Description'
                    entry = {}
                    try:
                        if len(header) >= 6 and header[1] == "Instance":
                            entry["Instance"] = str(row[1]).strip()
                            entry["Name"] = str(row[2]).strip().replace("\n", " ")
                            entry["Format"] = str(row[3]).strip().replace("\n", " ")
                            entry["ValueRange"] = str(row[4]).strip().replace("\n", " ")
                            entry["Description"] = str(row[5]).strip().replace("\n", " ")
                        else:
                            entry["Name"] = str(row[1]).strip().replace("\n", " ")
                            entry["Format"] = str(row[2]).strip().replace("\n", " ")
                            entry["ValueRange"] = str(row[3]).strip().replace("\n", " ")
                            entry["Description"] = str(row[4]).strip().replace("\n", " ") if len(row) > 4 else ""
                    except IndexError:
                        continue
                    
                    # Merge if already exists (usually the PDF lists it once, but just in case)
                    if param_id not in metadata:
                        metadata[param_id] = entry
                    else:
                        # Append descriptions if they differ
                        if entry["Description"] and entry["Description"] not in metadata[param_id]["Description"]:
                            metadata[param_id]["Description"] += " " + entry["Description"]
    
    return metadata

def main():
    pdfs = glob.glob("docs/pdfs/*.pdf")
    structured_metadata = {"ldd": {}, "tec": {}, "general": {}}
    for pdf in pdfs:
        doc_meta = parse_pdf(pdf)
        filename = os.path.basename(pdf).upper()
        if "LDD" in filename:
            category = "ldd"
        elif "TEC" in filename:
            category = "tec"
        else:
            category = "general"
        
        target = structured_metadata[category]
        for pid, data in doc_meta.items():
            pid_str = str(pid)
            if pid_str not in target:
                target[pid_str] = data
            else:
                # Merge logic - we keep the most descriptive one
                if len(data.get("Description", "")) > len(target[pid_str].get("Description", "")):
                    target[pid_str]["Description"] = data["Description"]
                if data.get("ValueRange", "") and data["ValueRange"] != "..":
                    target[pid_str]["ValueRange"] = data["ValueRange"]

    # Save to a generic source file
    out_path = "mecom/catalogues/sources/pdf_extracted_metadata.json"
    with open(out_path, "w") as f:
        json.dump(structured_metadata, f, indent=2)
    print(f"Extracted metadata. Saved to {out_path}")

if __name__ == "__main__":
    main()
