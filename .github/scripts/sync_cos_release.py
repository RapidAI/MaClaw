#!/usr/bin/env python3
import json
import os
import pathlib

def log(message):
    print(f"[cos-release-sync] {message}", flush=True)


secret_id = os.environ.get("COS_SECRET_ID", "")
secret_key = os.environ.get("COS_SECRET_KEY", "")
bucket = os.environ.get("COS_BUCKET", "")
region = os.environ.get("COS_REGION", "")
public_base_url = os.environ["COS_PUBLIC_BASE_URL"].rstrip("/")
tag = os.environ["RELEASE_TAG"]
asset_dir = pathlib.Path(os.environ.get("RELEASE_ASSETS_DIR", "release-assets"))
only_assets = [
    name.strip()
    for name in os.environ.get("COS_RELEASE_ONLY_ASSETS", "").splitlines()
    if name.strip()
]


def upload_file(client, local_path, key, cache_control):
    size = local_path.stat().st_size
    log(f"upload {local_path.name} size={size} key={key}")
    client.upload_file(
        Bucket=bucket,
        Key=key,
        LocalFilePath=str(local_path),
        PartSize=int(os.environ.get("COS_UPLOAD_PART_SIZE_MB", "8")),
        MAXThread=int(os.environ.get("COS_UPLOAD_THREADS", "8")),
        CacheControl=cache_control,
    )


def list_release_objects(client):
    marker = ""
    keys = []
    while True:
        response = client.list_objects(
            Bucket=bucket,
            Prefix="releases/",
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


def write_latest_manifest(assets):
    latest = {
        "version": tag,
        "tag": tag,
        "assets": {
            path.name: {
                "name": path.name,
                "size": path.stat().st_size,
                "url": f"{public_base_url}/releases/{tag}/{path.name}",
            }
            for path in assets
        },
    }
    latest_path = asset_dir / "latest.json"
    latest_path.write_text(json.dumps(latest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    log(f"wrote latest manifest {latest_path} assets={len(assets)}")
    return latest_path


def main():
    assets = collect_assets()
    latest_path = write_latest_manifest(assets)
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

    log(f"bucket={bucket} region={region} tag={tag} keep_prefix=releases/{tag}/")
    for path in assets:
        upload_file(
            client,
            path,
            f"releases/{tag}/{path.name}",
            "public, max-age=31536000, immutable",
        )
    upload_file(client, latest_path, "latest.json", "public, max-age=60")

    keep_prefix = f"releases/{tag}/"
    old_keys = [key for key in list_release_objects(client) if not key.startswith(keep_prefix)]
    log(f"cleanup old release objects count={len(old_keys)}")
    for key in old_keys:
        log(f"delete old object {key}")
        client.delete_object(Bucket=bucket, Key=key)

    log(f"synced COS release {tag}: uploaded={len(assets)} latest=latest.json deleted_old={len(old_keys)}")


if __name__ == "__main__":
    if "--manifest-only" in os.sys.argv:
        write_latest_manifest(collect_assets())
    else:
        main()
