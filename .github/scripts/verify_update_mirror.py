#!/usr/bin/env python3
"""Release gate for the public desktop-update mirrors.

GitHub publishes the release only after this script confirms that R2 and COS
advertise the exact local release artifacts and every object is publicly
readable at the expected size. This prevents a new manifest from sending
desktop clients to a stale or private mirror.
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
    validate_public_mirror_base,
)


MANIFEST_LIMIT = 2 * 1024 * 1024
HTTP_TIMEOUT_SECONDS = 45
MIRROR_BASES = {
    "R2": R2_PUBLIC_BASE_URL,
    "COS": COS_PUBLIC_BASE_URL,
}
# Matches Go's net/url.PathEscape for a single path segment. The history
# publisher uses the same set so validation remains correct for opaque build
# identifiers that contain characters such as '+' or ':'.
GO_PATH_SEGMENT_SAFE = "$&+-.0123456789:=@ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz~"


def required_env(name):
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


def release_asset_names():
    names = []
    seen = set()
    for raw in required_env("RELEASE_ONLY_ASSETS").splitlines():
        name = raw.strip()
        if not name:
            continue
        if pathlib.PurePosixPath(name).name != name:
            raise RuntimeError(f"release asset name must be a file name: {name!r}")
        if name not in seen:
            seen.add(name)
            names.append(name)
    if not names:
        raise RuntimeError("RELEASE_ONLY_ASSETS has no asset names")
    return names


def sha256_file(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def read_json(url):
    request = urllib.request.Request(url, headers={"User-Agent": "MaClaw-update-mirror-gate/1"})
    with urllib.request.urlopen(request, timeout=HTTP_TIMEOUT_SECONDS) as response:
        if response.status != 200:
            raise RuntimeError(f"GET {url}: HTTP {response.status}")
        body = response.read(MANIFEST_LIMIT + 1)
    if len(body) > MANIFEST_LIMIT:
        raise RuntimeError(f"GET {url}: response exceeds allowed size")
    try:
        return json.loads(body)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"GET {url}: response is not valid JSON") from exc


def public_content_length(url):
    """Prove public read access and return the object's declared size."""
    request = urllib.request.Request(url, method="HEAD", headers={"User-Agent": "MaClaw-update-mirror-gate/1"})
    try:
        with urllib.request.urlopen(request, timeout=HTTP_TIMEOUT_SECONDS) as response:
            if response.status != 200:
                raise RuntimeError(f"HEAD {url}: HTTP {response.status}")
            value = response.headers.get("Content-Length")
        return int(value)
    except (urllib.error.HTTPError, urllib.error.URLError, TypeError, ValueError):
        # HEAD is often disabled by a CDN. A one-byte range GET exercises the
        # exact anonymous read path used by the desktop client.
        request = urllib.request.Request(
            url,
            headers={"User-Agent": "MaClaw-update-mirror-gate/1", "Range": "bytes=0-0"},
        )
        with urllib.request.urlopen(request, timeout=HTTP_TIMEOUT_SECONDS) as response:
            if response.status not in (200, 206):
                raise RuntimeError(f"GET {url}: HTTP {response.status}")
            body = response.read(2)
            if not body:
                raise RuntimeError(f"GET {url}: empty response")
            content_range = response.headers.get("Content-Range", "")
            if "/" in content_range:
                value = content_range.rsplit("/", 1)[1]
            else:
                # A server that ignores Range may return 200 and omit
                # Content-Range; it is still publicly readable, and the
                # Content-Length remains authoritative.
                value = response.headers.get("Content-Length")
        try:
            return int(value)
        except (TypeError, ValueError) as exc:
            raise RuntimeError(f"GET {url}: missing or invalid object size") from exc


def verify_mirror(label, base, manifest_name, tag, prefix, asset_dir, names):
    expected_base = MIRROR_BASES[label]
    base = validate_public_mirror_base(f"{label}_PUBLIC_BASE_URL", base, expected_base)
    manifest_url = f"{base}/{urllib.parse.quote(manifest_name, safe='-._~')}"
    manifest = read_json(manifest_url)
    if manifest.get("tag") != tag or manifest.get("version") != tag:
        raise RuntimeError(f"{label}: public manifest tag/version does not match release tag {tag!r}")
    assets = manifest.get("assets")
    if not isinstance(assets, dict):
        raise RuntimeError(f"{label}: public manifest has no assets map")
    for name in names:
        path = asset_dir / name
        if not path.is_file():
            raise RuntimeError(f"local release asset is missing: {name}")
        entry = assets.get(name)
        if not isinstance(entry, dict):
            raise RuntimeError(f"{label}: public manifest is missing {name}")
        size = path.stat().st_size
        digest = sha256_file(path)
        if entry.get("name") != name or entry.get("size") != size or str(entry.get("sha256", "")).lower() != digest:
            raise RuntimeError(f"{label}: public manifest metadata differs for {name}")
        expected_urls = [f"{root}/{prefix}/{urllib.parse.quote(name, safe='-._~')}" for root in MIRROR_BASES.values()]
        urls = entry.get("urls")
        if urls != expected_urls or entry.get("url") != expected_urls[-1]:
            raise RuntimeError(f"{label}: public manifest mirror URLs differ for {name}")
        url = f"{base}/{prefix}/{urllib.parse.quote(name, safe='-._~')}"
        if public_content_length(url) != size:
            raise RuntimeError(f"{label}: public mirror size differs for {name}")
        print(f"[update-mirror-verify] verified {urllib.parse.urlsplit(url).hostname} {name} bytes={size}", flush=True)
    print(f"[update-mirror-verify] verified {label} manifest={manifest_name} tag={tag}", flush=True)


def verify_stable_history(base, tag, asset_dir, names):
    history = read_json(f"{base}/stable-history.json")
    releases = history.get("releases") if isinstance(history, dict) else None
    if not isinstance(releases, list) or not releases:
        raise RuntimeError("stable history has no releases")
    if len(releases) > 5:
        raise RuntimeError("stable history has more than five releases")

    current = releases[0]
    if (
        not isinstance(current, dict)
        or not isinstance(current.get("build"), str)
        or current.get("build").strip() != tag
        or not isinstance(current.get("published_at"), str)
        or not current["published_at"].strip()
    ):
        raise RuntimeError("stable history does not begin with the current formal build")
    assets = current.get("assets")
    if not isinstance(assets, dict):
        raise RuntimeError("stable history current build has no assets")

    for name in names:
        entry = assets.get(name)
        path = asset_dir / name
        if not isinstance(entry, dict) or not path.is_file():
            raise RuntimeError(f"stable history current build is missing {name}")
        build_path = urllib.parse.quote(tag, safe=GO_PATH_SEGMENT_SAFE)
        asset_path = urllib.parse.quote(name, safe=GO_PATH_SEGMENT_SAFE)
        expected_urls = [f"{root}/releases/{build_path}/{asset_path}" for root in MIRROR_BASES.values()]
        expected_sha256 = sha256_file(path)
        if (
            entry.get("name") != name
            or entry.get("size") != path.stat().st_size
            or str(entry.get("sha256", "")).lower() != expected_sha256
            or entry.get("urls") != expected_urls
            or entry.get("url") != expected_urls[-1]
        ):
            raise RuntimeError(f"stable history metadata differs for {name}")
        archive_url = f"{base}/releases/{build_path}/{asset_path}"
        if public_content_length(archive_url) != path.stat().st_size:
            raise RuntimeError(f"stable history archive differs for {name}")
    print(f"[update-mirror-verify] verified stable history current build={tag}", flush=True)


def main():
    tag = required_env("RELEASE_TAG")
    manifest_name = required_env("MANIFEST_NAME")
    if pathlib.PurePosixPath(manifest_name).name != manifest_name:
        raise RuntimeError("MANIFEST_NAME must be a file name")
    prefix = "beta" if os.environ.get("RELEASE_CHANNEL", "stable").strip() == "beta" else "latest"
    asset_dir = pathlib.Path(required_env("RELEASE_ASSETS_DIR"))
    names = release_asset_names()
    r2_base = required_env("R2_PUBLIC_BASE_URL")
    cos_base = required_env("COS_PUBLIC_BASE_URL")
    verify_mirror("R2", r2_base, manifest_name, tag, prefix, asset_dir, names)
    verify_mirror("COS", cos_base, manifest_name, tag, prefix, asset_dir, names)
    if prefix == "latest":
        verify_stable_history(r2_base, tag, asset_dir, names)
        verify_stable_history(cos_base, tag, asset_dir, names)


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError) as exc:
        print(f"[update-mirror-verify] error: {exc}", file=sys.stderr)
        raise SystemExit(1)
