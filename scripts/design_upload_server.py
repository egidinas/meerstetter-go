#!/usr/bin/env python3
"""LAN upload surface for reviewing Claude Design UI bundles.

The server stages uploaded zip/tar archives under /tmp by default. It does not
modify the repository or the live served UI.
"""

from __future__ import annotations

import argparse
import html
import os
import shutil
import tarfile
import tempfile
import time
import zipfile
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

MAX_UPLOAD_BYTES = 80 * 1024 * 1024
DEFAULT_ROOT = Path("/tmp/meerstetter-ui-uploads")
ALLOWED_SUFFIXES = {".zip", ".tgz", ".gz", ".tar"}


def safe_name(name: str) -> str:
    clean = "".join(ch if ch.isalnum() or ch in ".-_" else "_" for ch in name)
    return clean.strip("._") or "upload"


def is_safe_member(root: Path, target: Path) -> bool:
    try:
        target.resolve().relative_to(root.resolve())
        return True
    except ValueError:
        return False


def extract_archive(archive: Path, dest: Path) -> list[str]:
    names: list[str] = []
    if zipfile.is_zipfile(archive):
        with zipfile.ZipFile(archive) as zf:
            for info in zf.infolist():
                target = dest / info.filename
                if not is_safe_member(dest, target):
                    raise ValueError(f"unsafe zip path: {info.filename}")
                if not info.is_dir():
                    names.append(info.filename)
            zf.extractall(dest)
        return names
    if tarfile.is_tarfile(archive):
        with tarfile.open(archive) as tf:
            for member in tf.getmembers():
                target = dest / member.name
                if not is_safe_member(dest, target):
                    raise ValueError(f"unsafe tar path: {member.name}")
                if member.isfile():
                    names.append(member.name)
            tf.extractall(dest, filter="data")
        return names
    raise ValueError("unsupported archive format; upload .zip, .tar, or .tgz")


def find_preview_files(root: Path) -> tuple[Path | None, list[Path]]:
    handover = None
    candidates: list[Path] = []
    for path in root.rglob("*"):
        if not path.is_file():
            continue
        rel = path.relative_to(root)
        if path.name.lower() == "handover.md":
            handover = rel
        if path.suffix in {".tsx", ".ts", ".css", ".md"} and (
            "web/src" in rel.as_posix() or path.name.lower() == "handover.md"
        ):
            candidates.append(rel)
    return handover, sorted(candidates, key=lambda p: p.as_posix())


def render_page(title: str, body: str) -> bytes:
    return f"""<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{html.escape(title)}</title>
  <style>
    body {{ margin: 0; font: 15px/1.45 system-ui, -apple-system, Segoe UI, sans-serif; color: #18212f; background: #f6f7f9; }}
    main {{ max-width: 980px; margin: 0 auto; padding: 28px; }}
    h1 {{ font-size: 24px; margin: 0 0 14px; }}
    section {{ background: white; border: 1px solid #dce1e8; border-radius: 8px; padding: 18px; margin: 16px 0; }}
    input, button {{ font: inherit; }}
    button {{ padding: 8px 12px; border: 1px solid #abb5c3; border-radius: 6px; background: #17324d; color: white; cursor: pointer; }}
    code, pre {{ font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }}
    pre {{ overflow: auto; background: #101820; color: #eef6ff; border-radius: 6px; padding: 14px; }}
    a {{ color: #0b5cad; }}
    .muted {{ color: #66758a; }}
    .error {{ color: #a61d24; }}
    .ok {{ color: #126b38; }}
    ul {{ padding-left: 22px; }}
  </style>
</head>
<body><main>{body}</main></body></html>""".encode()


class UploadHandler(BaseHTTPRequestHandler):
    server_version = "MeerstetterDesignUpload/1.0"

    def do_GET(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path == "/":
            self.respond_html(self.index())
            return
        if parsed.path == "/preview":
            self.respond_html(self.preview(parse_qs(parsed.query).get("id", [""])[0]))
            return
        self.send_error(HTTPStatus.NOT_FOUND)

    def do_POST(self) -> None:
        if urlparse(self.path).path != "/upload":
            self.send_error(HTTPStatus.NOT_FOUND)
            return
        length = int(self.headers.get("Content-Length", "0"))
        if length <= 0 or length > MAX_UPLOAD_BYTES:
            self.respond_html(self.message("Upload rejected", f"Size must be 1 byte to {MAX_UPLOAD_BYTES} bytes.", True), HTTPStatus.BAD_REQUEST)
            return
        ctype = self.headers.get("Content-Type", "")
        boundary_token = "boundary="
        if boundary_token not in ctype:
            self.respond_html(self.message("Upload rejected", "Expected multipart/form-data.", True), HTTPStatus.BAD_REQUEST)
            return
        boundary = ctype.split(boundary_token, 1)[1].strip().strip('"').encode()
        raw = self.rfile.read(length)
        try:
            filename, payload = self.parse_multipart(raw, boundary)
            upload_id = time.strftime("%Y%m%d-%H%M%S") + "-" + safe_name(filename)
            dest = self.server.upload_root / upload_id
            dest.mkdir(parents=True, exist_ok=False)
            archive = dest / safe_name(filename)
            archive.write_bytes(payload)
            extract_dir = dest / "extracted"
            extract_dir.mkdir()
            extracted = extract_archive(archive, extract_dir)
            handover, candidates = find_preview_files(extract_dir)
        except Exception as exc:
            self.respond_html(self.message("Upload failed", html.escape(str(exc)), True), HTTPStatus.BAD_REQUEST)
            return

        body = f"""
<h1>Upload staged</h1>
<section>
  <p class="ok">Stored package <code>{html.escape(filename)}</code>.</p>
  <p>Staging directory: <code>{html.escape(str(dest))}</code></p>
  <p>Extracted files: <strong>{len(extracted)}</strong></p>
  <p>HANDOVER.md: <strong>{'found' if handover else 'not found'}</strong></p>
  <p>Candidate integration files: <strong>{len(candidates)}</strong></p>
  <p><a href="/preview?id={html.escape(upload_id)}">Open staged preview</a></p>
</section>
"""
        self.respond_html(render_page("Upload staged", body))

    def parse_multipart(self, raw: bytes, boundary: bytes) -> tuple[str, bytes]:
        marker = b"--" + boundary
        for part in raw.split(marker):
            if b"Content-Disposition:" not in part:
                continue
            header, _, payload = part.partition(b"\r\n\r\n")
            if not payload:
                continue
            disposition = header.decode("utf-8", "replace")
            if 'name="package"' not in disposition:
                continue
            filename = "claude-design-package.zip"
            if "filename=" in disposition:
                filename = disposition.split("filename=", 1)[1].split("\r\n", 1)[0].strip().strip('"')
            return filename, payload.rstrip(b"\r\n-")
        raise ValueError("file field 'package' not found")

    def index(self) -> bytes:
        uploads = sorted([p for p in self.server.upload_root.iterdir() if p.is_dir()], reverse=True) if self.server.upload_root.exists() else []
        items = "\n".join(f'<li><a href="/preview?id={html.escape(p.name)}">{html.escape(p.name)}</a></li>' for p in uploads[:20])
        body = f"""
<h1>Meerstetter UI Package Upload</h1>
<section>
  <form action="/upload" method="post" enctype="multipart/form-data">
    <p>Upload the Claude Design package. Expected content: <code>HANDOVER.md</code> and changed <code>web/src/*.tsx</code> files for branch <code>checkpoint/meerstetter-webui-20260516</code>.</p>
    <p><input type="file" name="package" accept=".zip,.tar,.tgz,.gz" required></p>
    <button type="submit">Stage Package</button>
  </form>
  <p class="muted">Uploads are staged only. Live UI and repository files are not modified.</p>
</section>
<section>
  <h2>Recent staged packages</h2>
  <ul>{items or '<li class="muted">No uploads yet.</li>'}</ul>
</section>
"""
        return render_page("Meerstetter UI Upload", body)

    def preview(self, upload_id: str) -> bytes:
        clean = safe_name(upload_id)
        root = self.server.upload_root / clean / "extracted"
        if not root.exists():
            return self.message("Not found", "No staged package with that id.", True)
        handover, candidates = find_preview_files(root)
        handover_text = ""
        if handover:
            handover_text = (root / handover).read_text("utf-8", "replace")[:50000]
        file_items = "\n".join(f"<li><code>{html.escape(p.as_posix())}</code></li>" for p in candidates[:200])
        body = f"""
<h1>Staged Preview</h1>
<section>
  <p>Package id: <code>{html.escape(clean)}</code></p>
  <p>Extracted root: <code>{html.escape(str(root))}</code></p>
  <p><a href="/">Back to upload</a></p>
</section>
<section>
  <h2>HANDOVER.md</h2>
  {'<pre>' + html.escape(handover_text) + '</pre>' if handover else '<p class="error">HANDOVER.md not found.</p>'}
</section>
<section>
  <h2>Candidate files</h2>
  <ul>{file_items or '<li class="muted">No web/src candidates found.</li>'}</ul>
</section>
"""
        return render_page("Staged Preview", body)

    def message(self, title: str, text: str, error: bool = False) -> bytes:
        cls = "error" if error else "ok"
        return render_page(title, f'<h1>{html.escape(title)}</h1><section><p class="{cls}">{text}</p><p><a href="/">Back</a></p></section>')

    def respond_html(self, body: bytes, status: HTTPStatus = HTTPStatus.OK) -> None:
        self.send_response(status)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main() -> None:
    parser = argparse.ArgumentParser(description="Stage Claude Design UI bundles over LAN.")
    parser.add_argument("--listen", default="0.0.0.0:18083", help="listen address, default 0.0.0.0:18083")
    parser.add_argument("--upload-root", default=str(DEFAULT_ROOT), help="staging directory")
    args = parser.parse_args()
    host, port_text = args.listen.rsplit(":", 1)
    upload_root = Path(args.upload_root)
    upload_root.mkdir(parents=True, exist_ok=True)
    server = ThreadingHTTPServer((host, int(port_text)), UploadHandler)
    server.upload_root = upload_root
    print(f"upload UI listening on http://{args.listen}/")
    print(f"staging uploads under {upload_root}")
    server.serve_forever()


if __name__ == "__main__":
    main()
