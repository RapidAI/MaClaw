#!/usr/bin/env python3
import hashlib
import json
import os
import pathlib
import urllib.parse

from firmware_manifest_contract import (
    COS_PUBLIC_BASE_URL,
    R2_PUBLIC_BASE_URL,
    required_firmware,
    require_split_firmware_archives,
    require_archive_channel,
    validate_manifest_asset_urls,
    validate_public_mirror_base,
)

def log(message):
    print(f"[cos-release-sync] {message}", flush=True)


secret_id = os.environ.get("COS_SECRET_ID", "")
secret_key = os.environ.get("COS_SECRET_KEY", "")
bucket = os.environ.get("COS_BUCKET", "")
region = os.environ.get("COS_REGION", "")
public_base_url = os.environ["COS_PUBLIC_BASE_URL"].rstrip("/")
r2_public_base_url = os.environ.get("R2_PUBLIC_BASE_URL", "").rstrip("/")
tag = os.environ["RELEASE_TAG"]
asset_dir = pathlib.Path(os.environ.get("RELEASE_ASSETS_DIR", "release-assets"))
only_assets = [
    name.strip()
    for name in os.environ.get("COS_RELEASE_ONLY_ASSETS", "").splitlines()
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


def upload_file(client, local_path, key, cache_control):
    size = local_path.stat().st_size
    log(f"put {local_path.name} size={size} key={key}")
    with local_path.open("rb") as body:
        client.put_object(
            Bucket=bucket,
            Key=key,
            Body=body,
            # The public endpoint is a desktop update mirror. Explicitly set
            # object ACLs instead of relying on a bucket-default policy.
            ACL="public-read",
            CacheControl=cache_control,
        )



def collect_assets():
    if not asset_dir.exists():
        raise RuntimeError(f"assets directory not found: {asset_dir}")
    if not only_assets:
        raise RuntimeError("COS_RELEASE_ONLY_ASSETS is required")

    assets = []
    for name in only_assets:
        path = asset_dir / name
        if not path.exists():
            raise RuntimeError(f"COS release asset not found: {name}")
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
        "COS_PUBLIC_BASE_URL": COS_PUBLIC_BASE_URL,
    }.get(name)
    if expected is None:
        raise RuntimeError(f"unsupported public mirror label: {name}")
    return validate_public_mirror_base(name, value, expected)


def asset_urls(path):
    urls = []
    if r2_public_base_url:
        urls.append(f"{validate_public_base_url('R2_PUBLIC_BASE_URL', r2_public_base_url)}/{asset_prefix}/{path.name}")
    urls.append(f"{validate_public_base_url('COS_PUBLIC_BASE_URL', public_base_url)}/{asset_prefix}/{path.name}")
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


def stable_history_asset(path):
    # Keep history URLs byte-for-byte aligned with the Go client's PathEscape
    # construction. A release build remains an opaque server value.
    build_path = urllib.parse.quote(tag, safe=GO_PATH_SEGMENT_SAFE)
    asset_path = urllib.parse.quote(path.name, safe=GO_PATH_SEGMENT_SAFE)
    urls = []
    if r2_public_base_url:
        urls.append(f"{validate_public_base_url('R2_PUBLIC_BASE_URL', r2_public_base_url)}/releases/{build_path}/{asset_path}")
    urls.append(f"{validate_public_base_url('COS_PUBLIC_BASE_URL', public_base_url)}/releases/{build_path}/{asset_path}")
    return {
        "name": path.name,
        "size": path.stat().st_size,
        "sha256": sha256_file(path),
        "url": urls[-1],
        "urls": urls,
    }


def stable_history_entry(assets, published_at):
    return {
        "build": tag,
        "published_at": published_at,
        "assets": {path.name: stable_history_asset(path) for path in assets},
    }


def stable_history_release_sort_key(release):
    """Sort persisted records newest-first without interpreting build strings."""
    if not isinstance(release, dict):
        return ""
    published_at = release.get("published_at")
    return published_at.strip() if isinstance(published_at, str) else ""


def history_build_key(value):
    """Canonical identity for an opaque server build value."""
    return value.strip() if isinstance(value, str) else ""


def cos_error_status(exc):
    """Return COS's HTTP status across the SDK's supported error shapes."""
    status = getattr(exc, "get_status_code", lambda: None)()
    if status is None:
        response = getattr(exc, "get_response", lambda: None)()
        status = getattr(response, "status_code", None)
    try:
        return int(status)
    except (TypeError, ValueError):
        return None


def update_stable_history(client, assets):
    """Prepend the formal build and retain the five newest server records."""
    from datetime import datetime, timezone

    history_name = "stable-history.json"
    existing = {"releases": []}
    try:
        response = client.get_object(Bucket=bucket, Key=history_name)
        raw = response["Body"].get_raw_stream().read()
        candidate = json.loads(raw)
        if not isinstance(candidate, dict) or not isinstance(candidate.get("releases"), list):
            raise RuntimeError(f"{history_name} has an invalid release list")
        existing = candidate
    except Exception as exc:
        # A missing history is expected only for the first stable release. Do
        # not silently discard a valid rollback catalogue on transient errors.
        if cos_error_status(exc) != 404:
            raise RuntimeError(f"failed to load {history_name}: {exc}") from exc
        log(f"{history_name} does not exist yet; creating it")

    published_at = datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
    current = stable_history_entry(assets, published_at)
    releases = [current]
    current_build = history_build_key(tag)

    def is_valid_prior_release(release):
        if not isinstance(release, dict) or not history_build_key(release.get("build")) or not isinstance(release.get("assets"), dict):
            return False
        published = stable_history_release_sort_key(release)
        if not published:
            return False
        try:
            datetime.fromisoformat(published.replace("Z", "+00:00"))
        except ValueError:
            return False
        return True

    # Do not assume an interrupted legacy publication left the array ordered.
    # ISO-8601 UTC timestamps sort chronologically as strings, and the build
    # itself remains an opaque identity rather than a locally parsed version.
    prior_releases = sorted(existing["releases"], key=stable_history_release_sort_key, reverse=True)
    for release in prior_releases:
        if not is_valid_prior_release(release) or history_build_key(release.get("build")) == current_build:
            continue
        releases.append(release)
        if len(releases) == 5:
            break

    history_path = asset_dir / history_name
    history_path.write_text(json.dumps({"releases": releases}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    upload_file(client, history_path, history_name, "public, max-age=60")
    log(f"updated {history_name} releases={len(releases)}")


def main():
    manifest_name = resolve_manifest_name(os.environ.get("MANIFEST_OUTPUT_NAME"))
    assets = collect_assets()
    latest_path = write_latest_manifest(assets, manifest_name)
    missing = [
        name
        for name, value in {
            "COS_SECRET_ID": secret_id,
            "COS_SECRET_KEY": secret_key,
            "COS_BUCKET": bucket,
            "COS_REGION": region,
        }.items()
        if not value
    ]
    if missing:
        raise RuntimeError(f"missing COS upload environment: {', '.join(missing)}")

    from qcloud_cos import CosConfig, CosS3Client

    config = CosConfig(Region=region, SecretId=secret_id, SecretKey=secret_key, Scheme="https")
    client = CosS3Client(config)

    log(f"bucket={bucket} region={region} tag={tag} channel={release_channel} prefix={asset_prefix}/")
    for path in assets:
        upload_file(
            client,
            path,
            f"{asset_prefix}/{path.name}",
            "public, max-age=31536000, immutable",
        )
        if release_channel == "stable":
            # Preserve each formal installer under its build number. The
            # rollback client will only construct URLs under this namespace.
            upload_file(
                client,
                path,
                # Object-store keys are raw names. Their public URL is
                # percent-encoded separately by stable_history_asset, just as
                # Go's URL client does when it requests the object.
                f"releases/{tag}/{path.name}",
                "public, max-age=31536000, immutable",
            )
    upload_file(client, latest_path, manifest_name, "public, max-age=60")
    if release_channel == "stable":
        update_stable_history(client, assets)

    log(f"synced COS release {tag}: uploaded={len(assets)} manifest={manifest_name}")


if __name__ == "__main__":
    import sys
    manifest_name = resolve_manifest_name(os.environ.get("MANIFEST_OUTPUT_NAME"))
    # Parse --manifest-name from CLI args (overrides env var)
    args = sys.argv[1:]
    for i, arg in enumerate(args):
        if arg == "--manifest-name" and i + 1 < len(args):
            manifest_name = resolve_manifest_name(args[i + 1])
            break
    if "--manifest-only" in args:
        write_latest_manifest(collect_assets(), manifest_name)
    else:
        main()
