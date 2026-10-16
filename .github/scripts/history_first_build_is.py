#!/usr/bin/env python3
"""Exit 0 when stable-history.json's first release build equals the given tag.

Used by the release workflow's rollback publisher: a fetched catalogue that
does not start with the current build is stale (with the COS mirror paused
its updater never ran) and must be re-seeded instead of republished.

A catalogue can also carry the right build while describing different bytes:
re-running the same tag produces fresh binaries, so a first entry that still
lists the previous run's sizes/hashes would make verify_update_mirror reject
the published assets. When the local release-assets directory is available,
compare sizes and hashes against it and re-seed on any mismatch.
"""

import hashlib
import json
import pathlib
import os
import sys


def local_assets():
    """Size/hash of the assets this run publishes, keyed by name.

    The catalogue only covers RELEASE_ONLY_ASSETS (the mirror allowlist the
    seed script walks), so the comparison uses the same set.
    """
    directory = os.environ.get("RELEASE_ASSETS_DIR", "").strip()
    allowlist = [line.strip() for line in os.environ.get("RELEASE_ONLY_ASSETS", "").splitlines() if line.strip()]
    if not directory or not allowlist:
        return {}
    root = pathlib.Path(directory)
    if not root.is_dir():
        return {}
    found = {}
    for name in allowlist:
        path = root / name
        if not path.is_file():
            continue
        digest = hashlib.sha256()
        with path.open("rb") as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                digest.update(chunk)
        found[name] = (path.stat().st_size, digest.hexdigest())
    return found


def main():
    if len(sys.argv) != 2:
        print("usage: history_first_build_is.py TAG", file=sys.stderr)
        return 2
    tag = sys.argv[1].strip()
    try:
        with open("stable-history.json", encoding="utf-8") as source:
            first_release = json.load(source)["releases"][0]
        first = first_release["build"].strip()
    except Exception as exc:
        print(f"[history-first-build] unreadable catalogue: {exc}", file=sys.stderr)
        return 1
    if first != tag:
        print(f"[history-first-build] stale: first build {first!r} != {tag!r}", file=sys.stderr)
        return 1
    # A catalogue from the retired COS-sync writer carries a relative "url"
    # and no "urls" list; verify_update_mirror rejects that shape. Treat any
    # entry missing the two-mirror "urls" contract as stale so it is rebuilt.
    assets = first_release.get("assets")
    if not isinstance(assets, dict) or not assets:
        print("[history-first-build] stale: no assets in current release", file=sys.stderr)
        return 1
    for name, entry in assets.items():
        if not isinstance(entry, dict) or not isinstance(entry.get("urls"), list) or not entry["urls"]:
            print(f"[history-first-build] stale: {name} lacks the urls list", file=sys.stderr)
            return 1

    # Same tag, different bytes: a re-run rebuilt the binaries. Compare
    # against the assets this run is publishing and re-seed on any drift.
    expected = local_assets()
    if expected:
        for name, (size, digest) in expected.items():
            entry = assets.get(name)
            if not isinstance(entry, dict):
                print(f"[history-first-build] stale: {name} missing from catalogue", file=sys.stderr)
                return 1
            if int(entry.get("size", -1)) != size or str(entry.get("sha256", "")).lower() != digest:
                print(
                    f"[history-first-build] stale: {name} metadata differs "
                    f"(catalogue {entry.get('size')}/{str(entry.get('sha256'))[:12]}, "
                    f"local {size}/{digest[:12]})",
                    file=sys.stderr,
                )
                return 1
        for name in assets:
            if name not in expected:
                print(f"[history-first-build] stale: {name} is not in this run's assets", file=sys.stderr)
                return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
