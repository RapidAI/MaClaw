#!/usr/bin/env python3
import json
import mimetypes
import os
import pathlib
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid

TOKEN = os.environ["GITEE_TOKEN"]
OWNER = os.environ.get("GITEE_OWNER", "znsoft")
REPO = os.environ.get("GITEE_REPO", "maclaw")
TARGET = os.environ.get("GITEE_TARGET", "main")
TAG = os.environ["RELEASE_TAG"]
NAME = os.environ.get("RELEASE_NAME") or TAG
BODY = os.environ.get("RELEASE_BODY") or ""
BODY_FILE = os.environ.get("RELEASE_BODY_FILE")
if BODY_FILE:
    body_path = pathlib.Path(BODY_FILE)
    if body_path.exists():
        BODY = body_path.read_text(encoding="utf-8")
PRERELEASE = os.environ.get("RELEASE_PRERELEASE", "false").lower() == "true"
ASSETS_DIR = pathlib.Path(os.environ.get("RELEASE_ASSETS_DIR", "artifacts"))
API = f"https://gitee.com/api/v5/repos/{OWNER}/{REPO}"


def api_url(path, params=None):
    query = {"access_token": TOKEN}
    if params:
        query.update(params)
    return f"{API}{path}?{urllib.parse.urlencode(query)}"


def request_json(method, path, data=None, headers=None, ok=(200, 201, 204), params=None):
    body = None
    if data is not None:
        body = urllib.parse.urlencode(data).encode("utf-8")
    req = urllib.request.Request(api_url(path, params), data=body, method=method, headers=headers or {})
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            payload = resp.read().decode("utf-8")
            if resp.status not in ok:
                raise RuntimeError(f"{method} {path} failed: {resp.status} {payload}")
            return json.loads(payload) if payload else None
    except urllib.error.HTTPError as exc:
        payload = exc.read().decode("utf-8", errors="replace")
        if exc.code in ok:
            return json.loads(payload) if payload else None
        raise RuntimeError(f"{method} {path} failed: {exc.code} {payload}") from exc


def list_release_by_tag():
    page = 1
    while True:
        releases = request_json("GET", "/releases", params={"per_page": 100, "page": page}) or []
        for release in releases:
            if release.get("tag_name") == TAG:
                return release
        if len(releases) < 100:
            return None
        page += 1


def get_release_by_tag():
    try:
        release = request_json("GET", f"/releases/tags/{urllib.parse.quote(TAG, safe='')}")
        if release:
            return release
    except RuntimeError as exc:
        if "failed: 404" not in str(exc):
            raise
    return list_release_by_tag()


def create_release():
    return request_json(
        "POST",
        "/releases",
        data={
            "tag_name": TAG,
            "target_commitish": TARGET,
            "name": NAME,
            "body": BODY,
            "prerelease": "true" if PRERELEASE else "false",
        },
    )


def attachment_names(release):
    names = set()
    for key in ("attach_files", "assets"):
        for item in release.get(key) or []:
            name = item.get("name") or item.get("filename")
            if name:
                names.add(name)
    return names


def multipart_upload(path, file_path):
    boundary = "----gitee-release-" + uuid.uuid4().hex
    mime = mimetypes.guess_type(file_path.name)[0] or "application/octet-stream"
    head = (
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="file"; filename="{file_path.name}"\r\n'
        f"Content-Type: {mime}\r\n\r\n"
    ).encode("utf-8")
    tail = f"\r\n--{boundary}--\r\n".encode("utf-8")
    body = head + file_path.read_bytes() + tail
    headers = {"Content-Type": f"multipart/form-data; boundary={boundary}"}
    req = urllib.request.Request(api_url(path), data=body, method="POST", headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=300) as resp:
            payload = resp.read().decode("utf-8")
            if resp.status not in (200, 201):
                raise RuntimeError(f"upload failed: {resp.status} {payload}")
            return json.loads(payload) if payload else None
    except urllib.error.HTTPError as exc:
        payload = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"upload {file_path.name} failed: {exc.code} {payload}") from exc


def main():
    if not ASSETS_DIR.exists():
        raise RuntimeError(f"assets directory not found: {ASSETS_DIR}")

    assets = sorted(path for path in ASSETS_DIR.iterdir() if path.is_file())
    if not assets:
        raise RuntimeError(f"no release assets found in {ASSETS_DIR}")

    release = get_release_by_tag() or create_release()
    release_id = release["id"]
    existing = attachment_names(release)

    uploaded = 0
    skipped = 0
    for asset in assets:
        if asset.name in existing:
            print(f"skip existing asset: {asset.name}")
            skipped += 1
            continue
        multipart_upload(f"/releases/{release_id}/attach_files", asset)
        print(f"uploaded asset: {asset.name}")
        uploaded += 1

    print(f"synced Gitee release {TAG}: uploaded={uploaded} skipped={skipped}")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)