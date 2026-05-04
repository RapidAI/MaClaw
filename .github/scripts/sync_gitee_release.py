#!/usr/bin/env python3
import json
import mimetypes
import os
import pathlib
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed

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
UPLOAD_TIMEOUT = int(os.environ.get("GITEE_UPLOAD_TIMEOUT", "900"))
UPLOAD_RETRIES = int(os.environ.get("GITEE_UPLOAD_RETRIES", "3"))
UPLOAD_CONCURRENCY = int(os.environ.get("GITEE_UPLOAD_CONCURRENCY", "1"))
ONLY_ASSETS = {
    name.strip()
    for name in os.environ.get("GITEE_RELEASE_ONLY_ASSETS", "").splitlines()
    if name.strip()
}


def log(message):
    print(f"[gitee-release-sync] {message}", flush=True)


def api_url(path, params=None, include_token=True):
    query = {"access_token": TOKEN} if include_token else {}
    if params:
        query.update(params)
    suffix = f"?{urllib.parse.urlencode(query)}" if query else ""
    return f"{API}{path}{suffix}"


def request_json(method, path, data=None, headers=None, ok=(200, 201, 204), params=None):
    body = None
    include_token_in_url = method == "GET"
    log(f"{method} {path} params={params or {}} data_keys={sorted((data or {}).keys())}")
    if data is not None:
        data = {"access_token": TOKEN, **data}
        body = urllib.parse.urlencode(data).encode("utf-8")
    req = urllib.request.Request(
        api_url(path, params, include_token=include_token_in_url),
        data=body,
        method=method,
        headers=headers or {},
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            payload = resp.read().decode("utf-8")
            log(f"{method} {path} -> HTTP {resp.status} bytes={len(payload)}")
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
        log(f"search release tag={TAG} page={page}")
        releases = request_json("GET", "/releases", params={"per_page": 100, "page": page}) or []
        for release in releases:
            if release.get("tag_name") == TAG:
                log(f"found release by list id={release.get('id')} tag={release.get('tag_name')}")
                return release
        if len(releases) < 100:
            return None
        page += 1


def get_release_by_tag():
    try:
        release = request_json("GET", f"/releases/tags/{urllib.parse.quote(TAG, safe='')}")
        if release:
            log(f"found release by tag id={release.get('id')} tag={release.get('tag_name')}")
            return release
    except RuntimeError as exc:
        if "failed: 404" not in str(exc):
            raise
        log(f"release tag={TAG} not found by direct lookup; falling back to list")
    return list_release_by_tag()


def create_release():
    log(f"creating release tag={TAG} target={TARGET} name={NAME} prerelease={PRERELEASE}")
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


def multipart_field(boundary, name, value):
    return (
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="{name}"\r\n\r\n'
        f"{value}\r\n"
    ).encode("utf-8")


def multipart_file(boundary, field_name, file_path):
    mime = mimetypes.guess_type(file_path.name)[0] or "application/octet-stream"
    return (
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="{field_name}"; filename="{file_path.name}"\r\n'
        f"Content-Type: {mime}\r\n\r\n"
    ).encode("utf-8") + file_path.read_bytes() + b"\r\n"


def multipart_upload(path, file_path):
    if shutil.which("curl"):
        return curl_multipart_upload(path, file_path)

    boundary = "----gitee-release-" + uuid.uuid4().hex
    tail = f"--{boundary}--\r\n".encode("utf-8")
    body = multipart_field(boundary, "access_token", TOKEN) + multipart_file(boundary, "file", file_path) + tail
    headers = {"Content-Type": f"multipart/form-data; boundary={boundary}"}
    log(f"upload {file_path.name} size={file_path.stat().st_size} path={path}")
    req = urllib.request.Request(api_url(path, include_token=False), data=body, method="POST", headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=UPLOAD_TIMEOUT) as resp:
            payload = resp.read().decode("utf-8")
            log(f"upload {file_path.name} -> HTTP {resp.status} bytes={len(payload)}")
            if resp.status not in (200, 201):
                raise RuntimeError(f"upload failed: {resp.status} {payload}")
            return json.loads(payload) if payload else None
    except urllib.error.HTTPError as exc:
        payload = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"upload {file_path.name} failed: {exc.code} {payload}") from exc


def curl_multipart_upload(path, file_path):
    url = api_url(path, include_token=False)
    log(f"upload {file_path.name} size={file_path.stat().st_size} path={path} via=curl")
    proc = subprocess.run(
        [
            "curl",
            "--silent",
            "--show-error",
            "--fail-with-body",
            "--connect-timeout",
            "30",
            "--max-time",
            str(UPLOAD_TIMEOUT),
            "--request",
            "POST",
            "--header",
            "Expect:",
            "--form",
            f"access_token={TOKEN}",
            "--form",
            f"file=@{file_path}",
            "--write-out",
            "\n%{http_code}",
            url,
        ],
        check=False,
        capture_output=True,
        text=True,
        timeout=UPLOAD_TIMEOUT + 30,
    )
    output = proc.stdout or ""
    stderr = proc.stderr.strip()
    payload, _, status_text = output.rpartition("\n")
    try:
        status = int(status_text)
    except ValueError:
        status = 0
        payload = output
    log(f"upload {file_path.name} -> HTTP {status} curl_exit={proc.returncode} bytes={len(payload)}")
    if proc.returncode != 0 or status not in (200, 201):
        detail = payload.strip() or stderr or f"curl exit {proc.returncode}"
        raise RuntimeError(f"upload {file_path.name} failed: HTTP {status} {detail}")
    return json.loads(payload) if payload else None


def upload_with_retries(path, file_path):
    for attempt in range(1, UPLOAD_RETRIES + 1):
        try:
            if attempt > 1:
                log(f"retry upload {file_path.name}: attempt={attempt}/{UPLOAD_RETRIES}")
            return multipart_upload(path, file_path)
        except Exception as exc:
            if uploaded_asset_exists(file_path.name):
                log(f"upload {file_path.name} appears on Gitee after error; treating as success: {exc}")
                return None
            if isinstance(exc, (TimeoutError, subprocess.TimeoutExpired)):
                if attempt >= UPLOAD_RETRIES:
                    raise RuntimeError(
                        f"upload {file_path.name} timed out after {attempt} attempts; "
                        f"size={file_path.stat().st_size} timeout={UPLOAD_TIMEOUT}s"
                    ) from exc
                time.sleep(5 * attempt)
                continue
            if isinstance(exc, OSError):
                message = str(exc).lower()
                if "timed out" in message and attempt < UPLOAD_RETRIES:
                    time.sleep(5 * attempt)
                    continue
            raise


def uploaded_asset_exists(asset_name):
    try:
        release = get_release_by_tag()
        return bool(release and asset_name in attachment_names(release))
    except Exception as exc:
        log(f"could not verify uploaded asset {asset_name}: {exc}")
        return False


def main():
    log(f"repo={OWNER}/{REPO} target={TARGET} tag={TAG} assets_dir={ASSETS_DIR}")
    if not ASSETS_DIR.exists():
        raise RuntimeError(f"assets directory not found: {ASSETS_DIR}")

    assets = sorted(path for path in ASSETS_DIR.rglob("*") if path.is_file())
    if ONLY_ASSETS:
        found_names = {path.name for path in assets}
        missing = sorted(ONLY_ASSETS - found_names)
        if missing:
            raise RuntimeError("requested Gitee release assets not found: " + ", ".join(missing))
        assets = [path for path in assets if path.name in ONLY_ASSETS]
        log("only_assets=" + ", ".join(sorted(ONLY_ASSETS)))
    assets = sorted(assets, key=lambda path: (path.stat().st_size, path.name))
    if not assets:
        raise RuntimeError(f"no release assets found in {ASSETS_DIR}")
    log("assets=" + ", ".join(f"{asset.name}({asset.stat().st_size} bytes)" for asset in assets))

    release = get_release_by_tag() or create_release()
    release_id = release["id"]
    existing = attachment_names(release)
    log(f"using release id={release_id} existing_assets={sorted(existing)}")

    upload_assets = []
    skipped = 0
    for asset in assets:
        if asset.name in existing:
            print(f"skip existing asset: {asset.name}", flush=True)
            skipped += 1
            continue
        upload_assets.append(asset)

    uploaded = 0
    failures = []
    workers = max(1, min(UPLOAD_CONCURRENCY, len(upload_assets) or 1))
    log(f"upload_queue={len(upload_assets)} skipped={skipped} concurrency={workers}")
    with ThreadPoolExecutor(max_workers=workers) as executor:
        future_to_asset = {
            executor.submit(upload_with_retries, f"/releases/{release_id}/attach_files", asset): asset
            for asset in upload_assets
        }
        for future in as_completed(future_to_asset):
            asset = future_to_asset[future]
            try:
                future.result()
                print(f"uploaded asset: {asset.name}", flush=True)
                uploaded += 1
            except Exception as exc:
                failures.append(f"{asset.name}: {exc}")
                print(f"failed asset: {asset.name}: {exc}", file=sys.stderr, flush=True)

    if failures:
        raise RuntimeError("Gitee release upload failures: " + "; ".join(failures))

    print(f"synced Gitee release {TAG}: uploaded={uploaded} skipped={skipped}", flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)
