#!/usr/bin/env python3
"""Exit 0 when stable-history.json's first release build equals the given tag.

Used by the release workflow's rollback publisher: a fetched catalogue that
does not start with the current build is stale (with the COS mirror paused
its updater never ran) and must be re-seeded instead of republished.
"""

import json
import sys


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
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
