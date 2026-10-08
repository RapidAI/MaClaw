#!/usr/bin/env python3
"""Seed stable-history.json from the current release when COS is unreachable.

The rollback catalogue normally lives on the COS mirror and is updated by
sync_cos_release.py during upload. While the COS upload step is paused the
catalogue cannot be refreshed there; the R2 rollback publisher then falls back
to this script, which builds a single-entry catalogue from the local release
assets using the same schema as sync_cos_release.stable_history_entry.
"""

import hashlib
import json
import os
import pathlib
import sys
import urllib.parse
from datetime import datetime, timezone

GO_PATH_SEGMENT_SAFE = "$&+-.0123456789:=@ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz~"


def required_env(name):
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


def release_asset_paths(asset_dir):
    names = []
    for raw in required_env("RELEASE_ONLY_ASSETS").splitlines():
        name = raw.strip()
        if not name:
            continue
        if pathlib.PurePosixPath(name).name != name:
            raise RuntimeError(f"release asset name must be a file name: {name!r}")
        path = asset_dir / name
        if path.is_file():
            names.append(path)
    if not names:
        raise RuntimeError("no release assets found to seed the history from")
    return names


def main():
    tag = required_env("RELEASE_TAG")
    asset_dir = pathlib.Path(required_env("RELEASE_ASSETS_DIR"))
    build_path = urllib.parse.quote(tag, safe=GO_PATH_SEGMENT_SAFE)
    assets = {}
    for path in release_asset_paths(asset_dir):
        asset_path = urllib.parse.quote(path.name, safe=GO_PATH_SEGMENT_SAFE)
        digest = hashlib.sha256()
        with path.open("rb") as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                digest.update(chunk)
        assets[path.name] = {
            "name": path.name,
            "size": path.stat().st_size,
            "sha256": digest.hexdigest(),
            "url": f"/releases/{build_path}/{asset_path}",
        }
    published_at = datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
    history = {
        "releases": [
            {
                "build": tag,
                "published_at": published_at,
                "assets": assets,
            }
        ]
    }
    out = pathlib.Path("stable-history.json")
    out.write_text(json.dumps(history, indent=2), encoding="utf-8")
    print(f"seeded {out} from release {tag}: assets={len(assets)}")


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError) as exc:
        print(f"[seed-stable-history] error: {exc}", file=sys.stderr)
        raise SystemExit(1)
