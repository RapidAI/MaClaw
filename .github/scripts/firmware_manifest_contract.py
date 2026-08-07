#!/usr/bin/env python3
"""Shared, strict contract for the three ClawMateMaker release archives.

Both the manifest writer and its pre-publication verifier import this module so
there is one exact asset allow-list and one parser for the public index shape.
"""

import hashlib
import json
import pathlib
import urllib.parse
import zipfile


FIRMWARE_ASSETS = (
    "MaClaw-ESP32S3-EchoEar-2ST-firmware.clawfw",
    "MaClaw-ESP32S3-Bread-Compact-firmware.clawfw",
    "MaClaw-ESP32S3-Fangtang-4G-firmware.clawfw",
)


# These are product trust roots, not deployment-time preferences.  The desktop
# flasher ships the same two HTTPS origins in its allow-list; accepting a
# different public bucket URL in CI would create a release that looks healthy
# in the workflow but that no released desktop client can safely download.
R2_PUBLIC_BASE_URL = "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev"
COS_PUBLIC_BASE_URL = "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com"


def validate_public_mirror_base(label, value, expected):
    """Return the canonical public mirror base or reject configuration drift."""
    raw = (value or "").strip().rstrip("/")
    parsed = urllib.parse.urlsplit(raw)
    expected_parsed = urllib.parse.urlsplit(expected)
    if (
        parsed.scheme != "https"
        or parsed.username
        or parsed.password
        or parsed.query
        or parsed.fragment
        or parsed.path not in ("", "/")
        or parsed.hostname != expected_parsed.hostname
        or parsed.port is not None
    ):
        raise RuntimeError(
            f"{label} must be the desktop-approved public mirror {expected!r}; got {value!r}"
        )
    return expected


def validate_manifest_asset_urls(manifest, release_channel="stable"):
    """Validate the public URL topology advertised in every release index.

    Metadata equality alone is insufficient: a compromised or misconfigured
    publisher could place a correct hash beside links that the desktop client
    will reject.  Each firmware entry must advertise both independent mirrors
    in a deterministic R2-then-COS order and the path must match its asset
    name and channel prefix exactly.
    """
    if not isinstance(manifest, dict) or not isinstance(manifest.get("assets"), dict):
        raise RuntimeError("manifest has no assets map")
    prefix = "beta" if (release_channel or "stable").strip() == "beta" else "latest"
    roots = (R2_PUBLIC_BASE_URL, COS_PUBLIC_BASE_URL)
    for name in FIRMWARE_ASSETS:
        entry = manifest["assets"].get(name)
        if not isinstance(entry, dict):
            raise RuntimeError(f"{name}: manifest entry is missing")
        expected_urls = [f"{root}/{prefix}/{name}" for root in roots]
        urls = entry.get("urls")
        if urls != expected_urls or entry.get("url") != expected_urls[-1]:
            raise RuntimeError(f"{name}: manifest mirror URLs do not match the approved {prefix} topology")


def parse_sha256(value, label):
    if not isinstance(value, str) or len(value) != 64:
        raise RuntimeError(f"{label}: invalid SHA-256")
    try:
        int(value, 16)
    except ValueError as exc:
        raise RuntimeError(f"{label}: invalid SHA-256") from exc
    return value.lower()


def sha256_file(path):
    with pathlib.Path(path).open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def required_firmware(manifest, asset_dir, expected_tag=None):
    if not isinstance(manifest, dict) or not isinstance(manifest.get("assets"), dict):
        raise RuntimeError("manifest has no assets map")
    tag = manifest.get("tag")
    version = manifest.get("version")
    if not isinstance(tag, str) or not tag.strip() or tag != version:
        raise RuntimeError("manifest tag/version must be equal non-empty strings")
    if expected_tag is not None and tag != expected_tag:
        raise RuntimeError(f"manifest tag {tag!r} does not match expected release tag {expected_tag!r}")
    root = pathlib.Path(asset_dir)
    expected = {}
    for name in FIRMWARE_ASSETS:
        path = root / name
        entry = manifest["assets"].get(name)
        if not path.is_file() or not isinstance(entry, dict):
            raise RuntimeError(f"required firmware asset is missing from release output or manifest: {name}")
        if entry.get("name") != name:
            raise RuntimeError(f"{name}: manifest entry name is not exact")
        size = entry.get("size")
        if not isinstance(size, int) or size <= 0 or size != path.stat().st_size:
            raise RuntimeError(f"{name}: manifest size does not match release artifact")
        digest = parse_sha256(entry.get("sha256"), name)
        if sha256_file(path) != digest:
            raise RuntimeError(f"{name}: manifest SHA-256 does not match release artifact")
        expected[name] = {"size": size, "sha256": digest}
    return tag, expected


def require_split_firmware_archives(asset_dir):
    """Ensure every publishable firmware archive uses the new signed plan.

    This independently checks the ZIP payload rather than trusting a nearby
    evidence file.  It prevents a workflow refactor from silently publishing
    a legacy merged `full-flash.bin` after the desktop has gained a stronger
    per-image recovery protocol.
    """
    root = pathlib.Path(asset_dir)
    for name in FIRMWARE_ASSETS:
        path = root / name
        try:
            with zipfile.ZipFile(path) as archive:
                raw = archive.read("manifest.json")
        except (OSError, KeyError, zipfile.BadZipFile) as exc:
            raise RuntimeError(f"{name}: cannot read signed manifest") from exc
        try:
            manifest = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise RuntimeError(f"{name}: manifest is invalid JSON") from exc
        if manifest.get("mode") != "full":
            raise RuntimeError(f"{name}: official release package must be full install")
        if manifest.get("channel") not in ("stable", "beta"):
            raise RuntimeError(f"{name}: signed package must declare stable or beta channel")
        recovery = manifest.get("recovery")
        if not isinstance(recovery, dict) or recovery.get("powerLossBootable") is not False:
            raise RuntimeError(
                f"{name}: single-slot split package must explicitly declare recovery.powerLossBootable=false"
            )
        order = manifest.get("writeOrder")
        files = manifest.get("files")
        if not isinstance(order, list) or len(order) < 3 or not isinstance(files, list):
            raise RuntimeError(f"{name}: missing signed split writeOrder")
        images = [item for item in files if isinstance(item, dict) and item.get("region") != "metadata"]
        names = [item.get("name") for item in images]
        if len(images) < 3 or len(order) != len(images) or set(order) != set(names) or len(set(order)) != len(order):
            raise RuntimeError(f"{name}: signed writeOrder does not exactly cover split images")
        if order[-2:] != ["partition-table", "bootloader"]:
            raise RuntimeError(f"{name}: partition-table and bootloader must be the final signed image steps")


def require_archive_channel(asset_dir, release_channel):
    """Ensure the storage/release channel agrees with each signed package."""
    expected = "beta" if (release_channel or "stable").strip() == "beta" else "stable"
    root = pathlib.Path(asset_dir)
    for name in FIRMWARE_ASSETS:
        try:
            with zipfile.ZipFile(root / name) as archive:
                manifest = json.loads(archive.read("manifest.json"))
        except (OSError, KeyError, zipfile.BadZipFile, json.JSONDecodeError) as exc:
            raise RuntimeError(f"{name}: cannot read signed manifest channel") from exc
        if manifest.get("channel") != expected:
            raise RuntimeError(f"{name}: signed package channel {manifest.get('channel')!r} does not match release channel {expected!r}")
