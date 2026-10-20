#!/usr/bin/env python3
"""Build one stable-history.json release entry from local release assets.

update_stable_history.py is what the release workflow runs. It prepends the
current release onto the existing catalogue and keeps the five newest formal
builds. Invoking this module directly still writes a single-entry catalogue.
The workflow shares the URL and hash helpers defined here.
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


def r2_public_base():
    r2_base = os.environ.get("R2_PUBLIC_BASE_URL", "").strip().rstrip("/")
    if not r2_base:
        raise RuntimeError("R2_PUBLIC_BASE_URL is required")
    return r2_base


def immutable_release_url(r2_base, tag, name):
    # Mirror URLs must match verify_update_mirror's MIRROR_BASES exactly.
    # The Tencent COS mirror is retired, so the catalogue advertises the
    # single R2 root and "url" mirrors urls[-1].
    build_path = urllib.parse.quote(tag, safe=GO_PATH_SEGMENT_SAFE)
    asset_path = urllib.parse.quote(name, safe=GO_PATH_SEGMENT_SAFE)
    return f"{r2_base}/releases/{build_path}/{asset_path}"


def build_release_entry(tag, asset_paths, r2_base, published_at):
    assets = {}
    for path in asset_paths:
        digest = hashlib.sha256()
        with path.open("rb") as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                digest.update(chunk)
        url = immutable_release_url(r2_base, tag, path.name)
        assets[path.name] = {
            "name": path.name,
            "size": path.stat().st_size,
            "sha256": digest.hexdigest(),
            "urls": [url],
            "url": url,
        }
    return {"build": tag, "published_at": published_at, "assets": assets}


def main():
    tag = required_env("RELEASE_TAG")
    asset_dir = pathlib.Path(required_env("RELEASE_ASSETS_DIR"))
    published_at = datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
    entry = build_release_entry(tag, release_asset_paths(asset_dir), r2_public_base(), published_at)
    history = {"releases": [entry]}
    out = pathlib.Path("stable-history.json")
    out.write_text(json.dumps(history, indent=2), encoding="utf-8")
    print(f"seeded {out} from release {tag}: assets={len(entry['assets'])}")


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError) as exc:
        print(f"[seed-stable-history] error: {exc}", file=sys.stderr)
        raise SystemExit(1)
