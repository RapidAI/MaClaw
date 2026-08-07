#!/usr/bin/env python3
"""Release gate for the three ClawMateMaker firmware mirror objects.

The desktop flasher can select the fastest of GitHub, Cloudflare R2 and
Tencent COS only after it independently validates the signed archive.  This
gate makes the release workflow's public-mirror promise observable before the
GitHub Release itself becomes visible.
"""

import hashlib
import json
import os
import pathlib
import sys
import urllib.error
import urllib.parse
import urllib.request

from firmware_manifest_contract import (
    COS_PUBLIC_BASE_URL,
    R2_PUBLIC_BASE_URL,
    required_firmware,
    require_split_firmware_archives,
    require_archive_channel,
    validate_manifest_asset_urls,
    validate_public_mirror_base,
)


MANIFEST_LIMIT = 2 * 1024 * 1024
HTTP_TIMEOUT_SECONDS = 45


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise urllib.error.HTTPError(req.full_url, code, "unexpected redirect", headers, fp)


OPENER = urllib.request.build_opener(NoRedirect)


def required_env(name):
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


def public_base(name):
    expected = {
        "R2_PUBLIC_BASE_URL": R2_PUBLIC_BASE_URL,
        "COS_PUBLIC_BASE_URL": COS_PUBLIC_BASE_URL,
    }.get(name)
    if expected is None:
        raise RuntimeError(f"unsupported public mirror label: {name}")
    return validate_public_mirror_base(name, required_env(name), expected)


def url_at(base, *segments):
    return base + "/" + "/".join(urllib.parse.quote(segment, safe="-._~") for segment in segments)


def read_response(url, limit):
    request = urllib.request.Request(url, headers={"User-Agent": "ClawMateMaker-release-gate/1"})
    try:
        with OPENER.open(request, timeout=HTTP_TIMEOUT_SECONDS) as response:
            if response.status != 200:
                raise RuntimeError(f"GET {url}: HTTP {response.status}")
            body = response.read(limit + 1)
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f"GET {url}: HTTP {exc.code}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"GET {url}: {exc.reason}") from exc
    if len(body) > limit:
        raise RuntimeError(f"GET {url}: response exceeds allowed size")
    return body


def verify_asset(base, prefix, name, expected):
    url = url_at(base, prefix, name)
    body = read_response(url, expected["size"])
    digest = hashlib.sha256(body).hexdigest()
    if len(body) != expected["size"] or digest != expected["sha256"]:
        raise RuntimeError(f"{url}: firmware bytes do not match the generated manifest")
    print(f"[firmware-mirror-verify] verified {urllib.parse.urlsplit(url).hostname} {name} bytes={len(body)}", flush=True)


def validate_remote_manifest(remote_manifest, local_manifest, expected, source, release_channel="stable"):
    if remote_manifest.get("tag") != local_manifest.get("tag") or remote_manifest.get("version") != local_manifest.get("version"):
        raise RuntimeError(f"{source}: public manifest tag/version differs from the generated release manifest")
    assets = remote_manifest.get("assets")
    if not isinstance(assets, dict):
        raise RuntimeError(f"{source}: public manifest has no assets map")
    for name, value in expected.items():
        entry = assets.get(name)
        if not isinstance(entry, dict):
            raise RuntimeError(f"{source}: public manifest is missing {name}")
        if entry.get("name") != name or entry.get("size") != value["size"] or str(entry.get("sha256", "")).lower() != value["sha256"]:
            raise RuntimeError(f"{source}: public manifest metadata differs for {name}")
    try:
        validate_manifest_asset_urls(remote_manifest, release_channel)
    except RuntimeError as exc:
        raise RuntimeError(f"{source}: {exc}") from exc


def main():
    asset_dir = pathlib.Path(required_env("RELEASE_ASSETS_DIR"))
    manifest_name = required_env("MANIFEST_NAME")
    if pathlib.PurePosixPath(manifest_name).name != manifest_name:
        raise RuntimeError("MANIFEST_NAME must be a file name")
    local_manifest_path = asset_dir / manifest_name
    local_manifest_bytes = local_manifest_path.read_bytes()
    if len(local_manifest_bytes) > MANIFEST_LIMIT:
        raise RuntimeError("local manifest exceeds size limit")
    try:
        local_manifest = json.loads(local_manifest_bytes)
    except json.JSONDecodeError as exc:
        raise RuntimeError("local manifest is invalid JSON") from exc
    _, expected = required_firmware(local_manifest, asset_dir, os.environ.get("RELEASE_TAG", "").strip() or None)
    require_split_firmware_archives(asset_dir)
    require_archive_channel(asset_dir, os.environ.get("RELEASE_CHANNEL", "stable"))
    prefix = "beta" if os.environ.get("RELEASE_CHANNEL", "stable").strip() == "beta" else "latest"
    validate_manifest_asset_urls(local_manifest, os.environ.get("RELEASE_CHANNEL", "stable"))

    for label in ("R2_PUBLIC_BASE_URL", "COS_PUBLIC_BASE_URL"):
        base = public_base(label)
        remote_manifest = read_response(url_at(base, manifest_name), MANIFEST_LIMIT)
        try:
            remote_manifest_json = json.loads(remote_manifest)
        except json.JSONDecodeError as exc:
            raise RuntimeError(f"{label}: public manifest is invalid JSON") from exc
        validate_remote_manifest(remote_manifest_json, local_manifest, expected, label, os.environ.get("RELEASE_CHANNEL", "stable"))
        for name, entry in expected.items():
            verify_asset(base, prefix, name, entry)
        print(f"[firmware-mirror-verify] verified {label} manifest={manifest_name}", flush=True)


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError) as exc:
        print(f"[firmware-mirror-verify] error: {exc}", file=sys.stderr)
        raise SystemExit(1)
