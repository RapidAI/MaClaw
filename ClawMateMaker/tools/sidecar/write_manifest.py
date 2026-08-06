#!/usr/bin/env python3
"""Write the deterministic hash manifest consumed by ClawMate Maker."""

import argparse
import hashlib
import json
import os
from pathlib import Path


parser = argparse.ArgumentParser()
parser.add_argument("--dir", required=True)
parser.add_argument("--binary", required=True)
parser.add_argument("--version", required=True)
args = parser.parse_args()

root = Path(args.dir).resolve()
binary = root / args.binary
if binary.parent != root or not binary.is_file():
    raise SystemExit("sidecar binary must be a direct file in --dir")
if os.name != "nt" and binary.stat().st_mode & 0o111 == 0:
    raise SystemExit("sidecar binary is not executable")

digest = hashlib.sha256(binary.read_bytes()).hexdigest()
manifest = {
    "schemaVersion": 1,
    "tools": [{
        "name": "esptool",
        "path": args.binary,
        "sha256": f"sha256:{digest}",
        "version": args.version,
    }],
}
(root / "sidecar-manifest.json").write_text(
    json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n",
    encoding="utf-8",
)
