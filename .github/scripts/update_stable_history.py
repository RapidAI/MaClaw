#!/usr/bin/env python3
"""Publish stable-history.json without dropping older formal builds.

The desktop rollback list keeps the five newest formal builds. Replacing the
catalogue with only the release currently being published left OEM clients
that cannot read GitHub with a single row: the version they already have
installed.

This script puts the current release first, keeps older usable entries by
publication date, and fills any remaining slots from public GitHub releases
whose immutable R2 objects already exist. Stored dates are UTC RFC 3339
timestamps with whole seconds. TigerClaw, MetaStaff, and MaClaw installers
stay in the same history. A missing GitHub response does not fail the release.
"""

import json
import os
import pathlib
import sys
import urllib.error
import urllib.request
from datetime import datetime, timezone

import seed_stable_history

MAX_RELEASES = 5
REMOTE_CATALOGUE = pathlib.Path("stable-history.remote.json")
OUTPUT_CATALOGUE = pathlib.Path("stable-history.json")
# The desktop client reads up to three pages of 10. One CI request for the
# same thirty newest releases is enough; a shorter page can omit a formal
# build the client would still show.
GITHUB_RELEASES_URL = "https://api.github.com/repos/RapidAI/MaClaw/releases?per_page=30"
MIRROR_PROBE_TIMEOUT = 8
HEX = set("0123456789abcdefABCDEF")
# Desktop rollback selects one of these names from the active brand. The
# release allowlist also contains service binaries, AppImages, and firmware.
# Those files are not rollback slots, and a row that lacks any one of the
# allowlisted names from this set is invisible to that brand.
ROLLBACK_INSTALLERS = (
    "MaClaw-Setup.exe",
    "MaClaw-Universal.pkg",
    "TigerClaw-Setup.exe",
    "TigerClaw-Universal.pkg",
    "MetaStaff-Setup.exe",
    "MetaStaff-Universal.pkg",
)


def release_build(release):
    if not isinstance(release, dict):
        return ""
    return str(release.get("build") or "").strip()


def published_datetime(value):
    if isinstance(value, datetime):
        return value if value.tzinfo is not None else None
    text = str(value or "").strip()
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    try:
        parsed = datetime.fromisoformat(text)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return None
    return parsed


def canonical_published_at(value):
    """UTC RFC 3339 with whole seconds.

    The desktop list displays the date prefix and parses this field as a
    timestamp. Fractional seconds and numeric offsets still describe the same
    instant, but they must not be copied into the catalogue as-is.
    """
    parsed = published_datetime(value)
    if parsed is None:
        return ""
    return parsed.astimezone(timezone.utc).replace(microsecond=0).strftime("%Y-%m-%dT%H:%M:%SZ")


def with_canonical_published_at(release):
    if not isinstance(release, dict):
        return release
    canonical = canonical_published_at(release.get("published_at"))
    if not canonical or canonical == release.get("published_at"):
        return release
    copied = dict(release)
    copied["published_at"] = canonical
    return copied


def valid_sha256(value):
    text = str(value or "").strip()
    return len(text) == 64 and all(char in HEX for char in text)


def rollback_names(allowlist):
    allowed = set(allowlist or [])
    return [name for name in ROLLBACK_INSTALLERS if name in allowed]


def asset_matches_mirror(entry, expected):
    if not isinstance(entry, dict) or not valid_sha256(entry.get("sha256")):
        return False
    urls = entry.get("urls") if isinstance(entry.get("urls"), list) else []
    candidates = [str(item).strip() for item in urls]
    candidates.append(str(entry.get("url") or "").strip())
    return expected in candidates


def release_usable(release, r2_base, allowlist):
    """A row every allowlisted desktop brand can roll back to.

    The client looks up one installer name and then keeps five builds. A row
    that validates for MaClaw but not TigerClaw still consumes one of those
    five shared slots, so every allowlisted rollback installer must carry a
    64-hex sha256 and the immutable R2 URL.
    """
    build = release_build(release)
    required = rollback_names(allowlist)
    published = published_datetime(release.get("published_at") if isinstance(release, dict) else None)
    if not build or not required or published is None:
        return False
    assets = release.get("assets") if isinstance(release, dict) else None
    if not isinstance(assets, dict):
        return False
    for name in required:
        expected = seed_stable_history.immutable_release_url(r2_base, build, name)
        if not asset_matches_mirror(assets.get(name), expected):
            return False
    return True


def prefer_newer(current, candidate):
    """Keep the later publication when one build is listed more than once."""
    if current is None:
        return candidate
    current_at = published_datetime(current.get("published_at") if isinstance(current, dict) else None)
    candidate_at = published_datetime(candidate.get("published_at") if isinstance(candidate, dict) else None)
    if candidate_at is not None and (current_at is None or candidate_at > current_at):
        return candidate
    return current


def choose_catalogue(current, remote, backfill, r2_base, allowlist):
    """Keep the release being published, then the newest other usable builds.

    The current build stays first even when a clock-skewed timestamp is older.
    verify_update_mirror requires that position. Every other slot is the
    newest remaining formal build, not whatever order the remote file used.
    """
    current_build = release_build(current)
    if not current_build:
        raise RuntimeError("current release build is empty")
    chosen = {}
    for source in (remote or [], backfill or []):
        for release in source:
            build = release_build(release)
            if not build or build == current_build or not release_usable(release, r2_base, allowlist):
                continue
            chosen[build] = prefer_newer(chosen.get(build), release)
    pool = list(chosen.values())
    pool.sort(key=lambda release: published_datetime(release.get("published_at")), reverse=True)
    selected = [with_canonical_published_at(current)]
    selected.extend(with_canonical_published_at(release) for release in pool[: MAX_RELEASES - 1])
    return selected


def merge_releases(existing, current, r2_base, allowlist):
    """Put the release being published first and keep the newest older rows."""
    return choose_catalogue(current, existing, [], r2_base, allowlist)


def digest_sha256(digest):
    text = str(digest or "").strip()
    prefix = "sha256:"
    if len(text) != len(prefix) + 64 or text[:len(prefix)].lower() != prefix:
        return ""
    hex_part = text[len(prefix):].lower()
    if any(char not in "0123456789abcdef" for char in hex_part):
        return ""
    return hex_part


def allowlist_names():
    names = []
    for raw in os.environ.get("RELEASE_ONLY_ASSETS", "").splitlines():
        name = raw.strip()
        if name and pathlib.PurePosixPath(name).name == name:
            names.append(name)
    return names


def oldest_retained_published_at(releases):
    """Publication date of the oldest build that already fills the catalogue.

    Fewer than four older builds means empty slots remain, so there is no
    cutoff. Once four older builds are usable, a GitHub release at or before
    this date cannot enter the five-build list.
    """
    dates = []
    for release in releases or []:
        parsed = published_datetime(release.get("published_at") if isinstance(release, dict) else None)
        if parsed is not None:
            dates.append(parsed)
    dates.sort(reverse=True)
    if len(dates) < MAX_RELEASES - 1:
        return None
    return dates[MAX_RELEASES - 2]


def releases_from_github_payload(payload, allowlist, r2_base, already, mirror_size=None, oldest_kept=None):
    """Turn a GitHub releases JSON array into catalogue rows.

    Only the desktop rollback installers named in the allowlist are copied.
    The download URL is the immutable R2 path the desktop client already trusts;
    the GitHub API is not allowed to choose a different host. mirror_size is
    called only after every one of those installers is present with a digest
    and a size. A release missing any brand is skipped with no network probe.
    A copied release must then be publicly readable at the GitHub asset size.
    A 404 mirror must not be advertised. published_at is stored as UTC RFC 3339
    with whole seconds.
    """
    if not isinstance(payload, list):
        raise RuntimeError("github releases payload is not a list")
    required = rollback_names(allowlist)
    if not required:
        return []
    required_names = set(required)
    found = []
    seen = set(already)
    # Sort before the age cutoff. The public API is newest-first, but a
    # cutoff that stops the walk is only correct after that order is enforced.
    ordered = []
    for summary in payload:
        if not isinstance(summary, dict) or summary.get("draft") or summary.get("prerelease"):
            continue
        published = published_datetime(summary.get("published_at"))
        if published is None:
            continue
        ordered.append((published, summary))
    ordered.sort(key=lambda item: item[0], reverse=True)
    for published, summary in ordered:
        if len(found) >= MAX_RELEASES - 1:
            break
        build = str(summary.get("tag_name") or "").strip()
        published_at = canonical_published_at(published)
        if not build or not published_at:
            continue
        # Once a row is too old to displace the builds already retained,
        # every later row is older too. Compare the parsed instant, not the
        # whole-second catalogue form, so a fraction cannot fall on the cutoff.
        if oldest_kept is not None and published <= oldest_kept:
            break
        if build in seen:
            continue
        # Stage every required installer before any mirror read. Probing a
        # release that is already missing one brand spends the release job on
        # objects that cannot become a rollback row.
        staged = {}
        for asset in summary.get("assets") or []:
            if not isinstance(asset, dict):
                continue
            name = str(asset.get("name") or "").strip()
            if name not in required_names:
                continue
            sha = digest_sha256(asset.get("digest"))
            size = asset.get("size")
            if not sha or isinstance(size, bool) or not isinstance(size, int) or size < 0:
                continue
            staged[name] = (sha, size)
        if set(staged) != required_names:
            continue
        assets = {}
        mirror_mismatch = False
        for name in required:
            sha, size = staged[name]
            url = seed_stable_history.immutable_release_url(r2_base, build, name)
            if mirror_size is not None:
                try:
                    actual = mirror_size(url)
                except (OSError, RuntimeError, TypeError, ValueError):
                    actual = None
                if actual != size:
                    mirror_mismatch = True
                    break
            assets[name] = {
                "name": name,
                "size": size,
                "sha256": sha,
                "urls": [url],
                "url": url,
            }
        if mirror_mismatch:
            continue
        found.append({"build": build, "published_at": published_at, "assets": assets})
        seen.add(build)
    return found


def load_remote_releases(path):
    if not path.is_file():
        return []
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        print(f"[update-stable-history] ignoring unreadable catalogue: {exc}", file=sys.stderr)
        return []
    releases = document.get("releases") if isinstance(document, dict) else None
    if not isinstance(releases, list):
        print("[update-stable-history] ignoring catalogue without a releases list", file=sys.stderr)
        return []
    return releases


def fetch_github_releases():
    request = urllib.request.Request(
        GITHUB_RELEASES_URL,
        headers={"User-Agent": "MaClaw-release-publisher", "Accept": "application/vnd.github+json"},
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def live_mirror_size(url):
    import verify_update_mirror
    return verify_update_mirror.public_content_length(url, timeout=MIRROR_PROBE_TIMEOUT)


def main():
    tag = seed_stable_history.required_env("RELEASE_TAG")
    asset_dir = pathlib.Path(seed_stable_history.required_env("RELEASE_ASSETS_DIR"))
    r2_base = seed_stable_history.r2_public_base()
    allowlist = allowlist_names()
    published_at = canonical_published_at(os.environ.get("RELEASE_PUBLISHED_AT", ""))
    if not published_at:
        published_at = canonical_published_at(datetime.now(timezone.utc))
    current = seed_stable_history.build_release_entry(
        tag,
        seed_stable_history.release_asset_paths(asset_dir),
        r2_base,
        published_at,
    )
    remote = load_remote_releases(REMOTE_CATALOGUE)
    current_build = release_build(current)
    usable_by_build = {}
    for release in remote:
        build = release_build(release)
        if build == current_build or not release_usable(release, r2_base, allowlist):
            continue
        usable_by_build[build] = prefer_newer(usable_by_build.get(build), release)
    usable_remote = list(usable_by_build.values())
    already = [current_build, *usable_by_build.keys()]
    backfill = []
    try:
        payload = fetch_github_releases()
        backfill = releases_from_github_payload(
            payload,
            allowlist,
            r2_base,
            already,
            mirror_size=live_mirror_size,
            oldest_kept=oldest_retained_published_at(usable_remote),
        )
    except (OSError, RuntimeError, json.JSONDecodeError, urllib.error.URLError) as exc:
        print(f"[update-stable-history] github backfill skipped: {exc}", file=sys.stderr)
    releases = choose_catalogue(current, usable_remote, backfill, r2_base, allowlist)
    OUTPUT_CATALOGUE.write_text(json.dumps({"releases": releases}, indent=2), encoding="utf-8")
    builds = ", ".join(release_build(item) for item in releases)
    print(f"[update-stable-history] wrote {OUTPUT_CATALOGUE} releases={len(releases)} builds={builds}")


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError) as exc:
        print(f"[update-stable-history] error: {exc}", file=sys.stderr)
        raise SystemExit(1)
