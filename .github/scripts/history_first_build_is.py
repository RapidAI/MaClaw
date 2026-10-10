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
            first = json.load(source)["releases"][0]["build"].strip()
    except Exception as exc:
        print(f"[history-first-build] unreadable catalogue: {exc}", file=sys.stderr)
        return 1
    if first != tag:
        print(f"[history-first-build] stale: first build {first!r} != {tag!r}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
