#!/usr/bin/env python3
"""Generate the public release manifest (latest.json / beta.json).

The Tencent COS mirror was retired (2026-10-08). Assets are published to
GitHub Releases and mirrored to Cloudflare R2; this script only writes the
manifest that GitHub Releases and R2 carry.
"""
import hashlib
import json
import os
import pathlib
import urllib.parse

from firmware_manifest_contract import (
    R2_PUBLIC_BASE_URL,
    required_firmware,
    require_split_firmware_archives,
    require_archive_channel,
    validate_manifest_asset_urls,
    validate_public_mirror_base,
)

def log(message):
    print(f"[release-sync] {message}", flush=True)


r2_public_base_url = os.environ["R2_PUBLIC_BASE_URL"].rstrip("/")
tag = os.environ["RELEASE_TAG"]
asset_dir = pathlib.Path(os.environ.get("RELEASE_ASSETS_DIR", "release-assets"))
only_assets = [
    name.strip()
    for name in os.environ.get("RELEASE_ONLY_ASSETS", "").splitlines()
    if name.strip()
]

# Release channel: "stable" or "beta". Determines the storage prefix for assets.
# stable → latest/  |  beta → beta/
release_channel = (os.environ.get("RELEASE_CHANNEL") or "stable").strip()
asset_prefix = "beta" if release_channel == "beta" else "latest"
# net/url.PathEscape's allowed characters for a single path segment. Keep
# release history URLs identical to the desktop client's construction.
GO_PATH_SEGMENT_SAFE = "$&+-.0123456789:=@ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz~"


def resolve_manifest_name(value=None):
    name = (value or "").strip()
    return name or "latest.json"


def collect_assets():
    if not asset_dir.exists():
        raise RuntimeError(f"assets directory not found: {asset_dir}")
    if not only_assets:
        raise RuntimeError("RELEASE_ONLY_ASSETS is required")

    assets = []
    for name in only_assets:
        path = asset_dir / name
        if not path.exists():
            raise RuntimeError(f"release asset not found: {name}")
        assets.append(path)
    return assets


def sha256_file(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def validate_public_base_url(name, value):
    expected = {
        "R2_PUBLIC_BASE_URL": R2_PUBLIC_BASE_URL,
    }.get(name)
    if expected is None:
        raise RuntimeError(f"unsupported public mirror label: {name}")
    return validate_public_mirror_base(name, value, expected)


def asset_urls(path):
    urls = []
    if r2_public_base_url:
        urls.append(f"{validate_public_base_url('R2_PUBLIC_BASE_URL', r2_public_base_url)}/{asset_prefix}/{path.name}")
    return urls


def manifest_asset(path):
    urls = asset_urls(path)
    if not urls:
        raise RuntimeError(f"no public URLs configured for {path.name}")
    return {
        "name": path.name,
        "size": path.stat().st_size,
        "sha256": sha256_file(path),
        "url": urls[-1],
        "urls": urls,
    }


def write_latest_manifest(assets, manifest_name="latest.json"):
    manifest_name = resolve_manifest_name(manifest_name)
    latest = {
        "version": tag,
        "tag": tag,
        "assets": {path.name: manifest_asset(path) for path in assets},
    }
    latest_path = asset_dir / manifest_name
    latest_path.write_text(json.dumps(latest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    # Desktop-only releases still need latest.json so MaClaw GUI can discover
    # the new installer from GitHub. Firmware-specific invariants apply only
    # when the firmware artifacts are actually part of this release.
    if os.environ.get("REQUIRE_FIRMWARE_MANIFEST", "").strip().lower() == "true":
        required_firmware(latest, asset_dir, tag)
        require_split_firmware_archives(asset_dir)
        require_archive_channel(asset_dir, release_channel)
        validate_manifest_asset_urls(latest, release_channel)
    log(f"wrote manifest {latest_path} assets={len(assets)}")
    return latest_path


if __name__ == "__main__":
    import sys
    manifest_name = resolve_manifest_name(os.environ.get("MANIFEST_OUTPUT_NAME"))
    # Parse --manifest-name from CLI args (overrides env var)
    args = sys.argv[1:]
    for i, arg in enumerate(args):
        if arg == "--manifest-name" and i + 1 < len(args):
            manifest_name = resolve_manifest_name(args[i + 1])
            break
    write_latest_manifest(collect_assets(), manifest_name)
