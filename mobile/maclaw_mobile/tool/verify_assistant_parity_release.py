#!/usr/bin/env python3
"""Read-only preflight for AI assistant GUI-parity release artifacts.

Offline (default):
  - Verifies Mobile APK + Hub binary exist
  - Prints size + SHA256 (optionally compares --expect-apk-sha256 / --expect-hub-sha256)

Optional live Hub probe (never writes config / never deploys):
  - GET {hub}/healthz
  - Optional: POST {hub}/api/mobile/search with Viewer token (stream=false smoke)

This script does NOT install APK, replace Hub binaries, or mutate production.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def repo_root() -> Path:
    return mobile_root().parents[1]


DEFAULT_APK = Path("build/app/outputs/flutter-apk/app-release.apk")
DEFAULT_HUB_EXE = Path("hub/dist/maclaw-hub.exe")
DEFAULT_HUB_PACKAGE_EXE = Path("hub/package/maclaw-hub/maclaw-hub.exe")


@dataclass(frozen=True)
class ArtifactInfo:
    path: Path
    size: int
    sha256: str

    @property
    def size_mb(self) -> float:
        return round(self.size / (1024 * 1024), 2)


@dataclass(frozen=True)
class CheckResult:
    name: str
    ok: bool
    detail: str


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest().upper()


def inspect_artifact(path: Path) -> ArtifactInfo:
    if not path.is_file():
        raise FileNotFoundError(f"missing artifact: {path}")
    return ArtifactInfo(path=path, size=path.stat().st_size, sha256=sha256_file(path))


def resolve_path(base: Path, value: Path) -> Path:
    return value if value.is_absolute() else (base / value)


def check_expect_hash(label: str, info: ArtifactInfo, expected: str | None) -> CheckResult:
    if not expected:
        return CheckResult(
            label,
            True,
            f"{info.path} size={info.size} ({info.size_mb} MB) sha256={info.sha256}",
        )
    want = expected.strip().upper()
    ok = info.sha256 == want
    detail = (
        f"{info.path} sha256={info.sha256}"
        if ok
        else f"{info.path} sha256 mismatch got={info.sha256} want={want}"
    )
    return CheckResult(label, ok, detail)


def probe_healthz(hub_url: str, timeout: float) -> CheckResult:
    url = hub_url.rstrip("/") + "/healthz"
    try:
        with urllib.request.urlopen(url, timeout=timeout) as resp:
            body = resp.read(4096)
            status = resp.status
    except urllib.error.HTTPError as exc:
        return CheckResult("hub healthz", False, f"HTTP {exc.code} for {url}")
    except Exception as exc:  # noqa: BLE001 — surface any network failure cleanly
        return CheckResult("hub healthz", False, f"{url}: {exc}")

    try:
        payload = json.loads(body.decode("utf-8", errors="replace"))
    except json.JSONDecodeError:
        return CheckResult("hub healthz", False, f"{url}: non-JSON body status={status}")

    ok = status == 200 and payload.get("ok") is True
    return CheckResult(
        "hub healthz",
        ok,
        f"{url} status={status} body={payload}",
    )


def probe_mobile_search(
    hub_url: str,
    token: str,
    timeout: float,
    *,
    stream: bool = False,
) -> CheckResult:
    url = hub_url.rstrip("/") + "/api/mobile/search"
    payload = {
        "query": "assistant parity release smoke",
        "messages": [
            {"role": "user", "content": "ping"},
            {"role": "assistant", "content": "pong"},
        ],
        "stream": stream,
    }
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        method="POST",
        headers={
            "Authorization": f"Bearer {token.strip()}",
            "Content-Type": "application/json",
            "Accept": "text/event-stream" if stream else "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            status = resp.status
            content_type = (resp.headers.get("Content-Type") or "").lower()
            body = resp.read(65536)
    except urllib.error.HTTPError as exc:
        err_body = exc.read(512).decode("utf-8", errors="replace")
        return CheckResult(
            "hub mobile search",
            False,
            f"HTTP {exc.code} for {url}: {err_body[:200]}",
        )
    except Exception as exc:  # noqa: BLE001
        return CheckResult("hub mobile search", False, f"{url}: {exc}")

    if stream:
        text = body.decode("utf-8", errors="replace")
        ok = (
            status == 200
            and "text/event-stream" in content_type
            and "event: meta" in text
            and ("event: delta" in text or "event: done" in text or "event: error" in text)
        )
        return CheckResult(
            "hub mobile search stream",
            ok,
            f"status={status} content-type={content_type!r} "
            f"has_meta={'event: meta' in text} sample={text[:160]!r}",
        )

    try:
        decoded = json.loads(body.decode("utf-8", errors="replace"))
    except json.JSONDecodeError:
        return CheckResult(
            "hub mobile search",
            False,
            f"status={status} non-JSON response content-type={content_type!r}",
        )
    answer = str(decoded.get("answer") or "").strip()
    ok = status == 200 and answer != ""
    return CheckResult(
        "hub mobile search",
        ok,
        f"status={status} llm_mode={decoded.get('llm_mode')!r} "
        f"answer_len={len(answer)} request_id={decoded.get('llm_request_id')!r}",
    )


def run(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Read-only assistant GUI-parity release preflight",
    )
    parser.add_argument(
        "--repo-root",
        type=Path,
        default=None,
        help="Repository root (default: auto-detect from tool path)",
    )
    parser.add_argument(
        "--apk",
        type=Path,
        default=DEFAULT_APK,
        help=f"APK path relative to mobile root or absolute (default: {DEFAULT_APK})",
    )
    parser.add_argument(
        "--hub-exe",
        type=Path,
        default=DEFAULT_HUB_EXE,
        help=f"Hub exe relative to repo root (default: {DEFAULT_HUB_EXE})",
    )
    parser.add_argument(
        "--hub-package-exe",
        type=Path,
        default=DEFAULT_HUB_PACKAGE_EXE,
        help=f"Packaged Hub exe relative to repo root (default: {DEFAULT_HUB_PACKAGE_EXE})",
    )
    parser.add_argument(
        "--expect-apk-sha256",
        default="",
        help="Optional expected APK SHA256 (hex)",
    )
    parser.add_argument(
        "--expect-hub-sha256",
        default="",
        help="Optional expected Hub exe SHA256 (hex)",
    )
    parser.add_argument(
        "--require-package",
        action="store_true",
        help="Also require hub/package/maclaw-hub/maclaw-hub.exe and match hub sha",
    )
    parser.add_argument(
        "--hub-url",
        default="",
        help="Optional live Hub base URL for read-only smoke (e.g. https://tenant.example)",
    )
    parser.add_argument(
        "--viewer-token",
        default="",
        help="Optional Viewer bearer token for POST /api/mobile/search smoke",
    )
    parser.add_argument(
        "--probe-stream",
        action="store_true",
        help="When probing search, also request stream=true SSE",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=20.0,
        help="HTTP timeout seconds for live probes (default: 20)",
    )
    args = parser.parse_args(argv)

    root = args.repo_root.resolve() if args.repo_root else repo_root()
    mobile = root / "mobile" / "maclaw_mobile"
    results: list[CheckResult] = []

    # --- Offline artifacts ---
    try:
        apk_info = inspect_artifact(resolve_path(mobile, args.apk))
        results.append(
            check_expect_hash("mobile apk", apk_info, args.expect_apk_sha256 or None)
        )
    except FileNotFoundError as exc:
        results.append(CheckResult("mobile apk", False, str(exc)))

    hub_info: ArtifactInfo | None = None
    try:
        hub_info = inspect_artifact(resolve_path(root, args.hub_exe))
        results.append(
            check_expect_hash("hub exe", hub_info, args.expect_hub_sha256 or None)
        )
    except FileNotFoundError as exc:
        results.append(CheckResult("hub exe", False, str(exc)))

    if args.require_package:
        try:
            pkg_info = inspect_artifact(resolve_path(root, args.hub_package_exe))
            if hub_info is not None and pkg_info.sha256 != hub_info.sha256:
                results.append(
                    CheckResult(
                        "hub package exe",
                        False,
                        f"package sha {pkg_info.sha256} != dist sha {hub_info.sha256}",
                    )
                )
            else:
                results.append(
                    check_expect_hash(
                        "hub package exe",
                        pkg_info,
                        args.expect_hub_sha256 or None,
                    )
                )
        except FileNotFoundError as exc:
            results.append(CheckResult("hub package exe", False, str(exc)))

    # --- Optional live probes (read-only) ---
    if args.hub_url.strip():
        results.append(probe_healthz(args.hub_url.strip(), args.timeout))
        if args.viewer_token.strip():
            results.append(
                probe_mobile_search(
                    args.hub_url.strip(),
                    args.viewer_token,
                    args.timeout,
                    stream=False,
                )
            )
            if args.probe_stream:
                results.append(
                    probe_mobile_search(
                        args.hub_url.strip(),
                        args.viewer_token,
                        args.timeout,
                        stream=True,
                    )
                )
        else:
            results.append(
                CheckResult(
                    "hub mobile search",
                    True,
                    "skipped (pass --viewer-token to enable)",
                )
            )

    failed = 0
    for item in results:
        mark = "OK  " if item.ok else "FAIL"
        print(f"[{mark}] {item.name}: {item.detail}")
        if not item.ok:
            failed += 1

    if failed:
        print(f"Assistant parity preflight: {failed} check(s) failed.", file=sys.stderr)
        return 1
    print("Assistant parity preflight: all checks passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(run())
