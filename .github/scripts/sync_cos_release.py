#!/usr/bin/env python3
import hashlib
import json
import os
import pathlib
import urllib.parse

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
release_channel = os.environ.get("RELEASE_CHANNEL", "stable")
asset_prefix = "beta" if release_channel == "beta" else "latest"


def upload_file(client, local_path, key, cache_control):
    size = local_path.stat().st_size
    log(f"put {local_path.name} size={size} key={key}")
    with local_path.open("rb") as body:
        client.put_object(
            Bucket=bucket,
            Key=key,
            Body=body,
            CacheControl=cache_control,
        )


def list_objects_with_prefix(client, prefix):
    marker = ""
    keys = []
    while True:
        response = client.list_objects(
            Bucket=bucket,
            Prefix=prefix,
            Marker=marker,
            MaxKeys=1000,
        )
        contents = response.get("Contents") or []
        keys.extend(item["Key"] for item in contents)
        if response.get("IsTruncated") != "true":
            return keys
        marker = response.get("NextMarker") or keys[-1]


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
    parsed = urllib.parse.urlparse(value)
    if parsed.scheme != "https" or not parsed.netloc:
        raise RuntimeError(f"{name} must be an https URL with a host: {value!r}")
    return value.rstrip("/")


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
    latest = {
        "version": tag,
        "tag": tag,
        "assets": {path.name: manifest_asset(path) for path in assets},
    }
    latest_path = asset_dir / manifest_name
    latest_path.write_text(json.dumps(latest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    log(f"wrote manifest {latest_path} assets={len(assets)}")
    return latest_path


def main():
    manifest_name = os.environ.get("MANIFEST_OUTPUT_NAME", "latest.json")
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
    upload_file(client, latest_path, manifest_name, "public, max-age=60")

    old_keys = list_objects_with_prefix(client, "releases/")
    log(f"cleanup old tagged release objects count={len(old_keys)}")
    for key in old_keys:
        log(f"delete old object {key}")
        client.delete_object(Bucket=bucket, Key=key)

    log(f"synced COS release {tag}: uploaded={len(assets)} manifest={manifest_name} deleted_old={len(old_keys)}")


if __name__ == "__main__":
    import sys
    manifest_name = os.environ.get("MANIFEST_OUTPUT_NAME", "latest.json")
    # Parse --manifest-name from CLI args (overrides env var)
    args = sys.argv[1:]
    for i, arg in enumerate(args):
        if arg == "--manifest-name" and i + 1 < len(args):
            manifest_name = args[i + 1]
            break
    if "--manifest-only" in args:
        write_latest_manifest(collect_assets(), manifest_name)
    else:
        main()
