#!/usr/bin/env python3
"""Catalogue retention for desktop rollback history."""

import unittest

import update_stable_history as history

MIRROR = "https://mirror.example"
NAMES = ["TigerClaw-Setup.exe", "MaClaw-Setup.exe"]


def entry(build, published_at="2026-10-19T00:00:00Z", names=None):
    assets = {}
    for name in names or NAMES:
        url = f"{MIRROR}/releases/{build}/{name}"
        assets[name] = {"name": name, "size": 10, "sha256": "a" * 64, "urls": [url], "url": url}
    return {"build": build, "published_at": published_at, "assets": assets}


def gh_assets(size=10, digest="a", names=None):
    return [
        {"name": name, "size": size, "digest": "sha256:" + (digest * 64)}
        for name in (names or NAMES)
    ]


class UpdateStableHistoryTest(unittest.TestCase):
    def test_merge_keeps_older_oem_builds_and_replaces_same_tag(self):
        current = entry("V8.1.0.5")
        current["assets"]["TigerClaw-Setup.exe"]["sha256"] = "b" * 64
        existing = [entry("V8.1.0.5"), entry("V8.1.0.4"), entry("V8.1.0.3"), {"build": "broken"}]
        merged = history.merge_releases(existing, current, MIRROR, NAMES)
        self.assertEqual([item["build"] for item in merged], ["V8.1.0.5", "V8.1.0.4", "V8.1.0.3"])
        self.assertEqual(merged[0]["assets"]["TigerClaw-Setup.exe"]["sha256"], "b" * 64)

    def test_merge_caps_at_five(self):
        current = entry("V6")
        existing = [entry(f"V{index}") for index in range(5, 0, -1)]
        merged = history.merge_releases(existing, current, MIRROR, NAMES)
        self.assertEqual([item["build"] for item in merged], ["V6", "V5", "V4", "V3", "V2"])

    def test_merge_keeps_newest_when_remote_is_oldest_first(self):
        current = entry("V10", published_at="2026-10-20T00:00:00Z")
        remote = [entry(f"OLD{index}", published_at=f"2020-0{index}-01T00:00:00Z") for index in range(1, 5)]
        backfill = [entry(f"NEW{index}", published_at=f"2026-10-1{index}T00:00:00Z") for index in range(4)]
        chosen = history.choose_catalogue(current, remote, backfill, MIRROR, NAMES)
        self.assertEqual([item["build"] for item in chosen], ["V10", "NEW3", "NEW2", "NEW1", "NEW0"])

    def test_bad_url_or_hash_does_not_occupy_a_slot(self):
        current = entry("NOW", published_at="2026-12-01T00:00:00Z")
        bad_rows = []
        for index in range(4):
            bad = entry(f"BAD{index}", published_at=f"2026-11-0{index + 1}T00:00:00Z")
            bad["assets"]["TigerClaw-Setup.exe"]["sha256"] = "z" * 64
            bad["assets"]["TigerClaw-Setup.exe"]["urls"] = ["https://evil.example/installer.exe"]
            bad["assets"]["TigerClaw-Setup.exe"]["url"] = "https://evil.example/installer.exe"
            bad_rows.append(bad)
        good = entry("GOOD", published_at="2020-01-01T00:00:00Z")
        merged = history.merge_releases([*bad_rows, good], current, MIRROR, NAMES)
        self.assertEqual([item["build"] for item in merged], ["NOW", "GOOD"])

    def test_github_backfill_adds_older_oem_installer_without_using_remote_hosts(self):
        payload = [
            {
                "tag_name": "V8.1.0.5",
                "published_at": "2026-10-19T00:00:00Z",
                "draft": False,
                "prerelease": False,
                "assets": gh_assets(size=20, digest="c") + [{
                    "name": "Ins-maclaw-windows-amd64.exe",
                    "size": 3,
                    "digest": "sha256:" + ("f" * 64),
                    "browser_download_url": "https://evil.example/Ins-maclaw-windows-amd64.exe",
                }],
            },
            {
                "tag_name": "V8.1.0.4",
                "published_at": "2026-10-18T00:00:00Z",
                "draft": False,
                "prerelease": False,
                "assets": gh_assets(size=21, digest="d") + [
                    {"name": "notes.txt", "size": 1, "digest": "sha256:" + ("e" * 64)},
                    {"name": "Ins-maclaw-windows-amd64.exe", "size": 3, "digest": "sha256:" + ("f" * 64)},
                ],
            },
            {"tag_name": "V8.1.0.3", "draft": True, "prerelease": False, "published_at": "2026-10-17T00:00:00Z", "assets": []},
        ]
        found = history.releases_from_github_payload(
            payload,
            NAMES + ["Ins-maclaw-windows-amd64.exe", "notes.txt"],
            MIRROR,
            ["V8.1.0.5"],
        )
        self.assertEqual([item["build"] for item in found], ["V8.1.0.4"])
        self.assertEqual(set(found[0]["assets"]), set(NAMES))
        asset = found[0]["assets"]["TigerClaw-Setup.exe"]
        self.assertEqual(asset["sha256"], "d" * 64)
        self.assertEqual(asset["urls"], ["https://mirror.example/releases/V8.1.0.4/TigerClaw-Setup.exe"])
        self.assertNotIn("notes.txt", found[0]["assets"])
        self.assertNotIn("Ins-maclaw-windows-amd64.exe", found[0]["assets"])

    def test_backfill_skips_mirror_objects_that_are_missing_or_the_wrong_size(self):
        def sizes(url):
            if "/V8.1.0.3/" in url:
                return 21
            if "/V8.1.0.2/" in url:
                return 99
            return None

        payload = []
        for tag, size in (("V8.1.0.4", 21), ("V8.1.0.3", 21), ("V8.1.0.2", 21)):
            payload.append({
                "tag_name": tag,
                "published_at": "2026-10-18T00:00:00Z",
                "draft": False,
                "prerelease": False,
                "assets": gh_assets(size=size, digest="d"),
            })
        found = history.releases_from_github_payload(payload, NAMES, MIRROR, [], mirror_size=sizes)
        self.assertEqual([item["build"] for item in found], ["V8.1.0.3"])

    def test_backfill_stops_once_retained_builds_are_newer(self):
        payload = [
            {
                "tag_name": "V9",
                "published_at": "2026-10-20T00:00:00Z",
                "assets": gh_assets(digest="a"),
            },
            {
                "tag_name": "V1",
                "published_at": "2020-01-01T00:00:00Z",
                "assets": gh_assets(digest="b"),
            },
        ]
        cutoff = history.published_datetime("2026-10-01T00:00:00Z")
        found = history.releases_from_github_payload(
            payload,
            NAMES,
            MIRROR,
            ["CURRENT"],
            oldest_kept=cutoff,
        )
        self.assertEqual([item["build"] for item in found], ["V9"])

    def test_backfill_cutoff_does_not_depend_on_payload_order(self):
        payload = [
            {
                "tag_name": "V1",
                "published_at": "2020-01-01T00:00:00Z",
                "assets": gh_assets(digest="b"),
            },
            {
                "tag_name": "V9",
                "published_at": "2026-10-20T00:00:00Z",
                "assets": gh_assets(digest="a"),
            },
        ]
        cutoff = history.published_datetime("2026-10-01T00:00:00Z")
        found = history.releases_from_github_payload(
            payload,
            NAMES,
            MIRROR,
            ["CURRENT"],
            oldest_kept=cutoff,
        )
        self.assertEqual([item["build"] for item in found], ["V9"])

    def test_backfill_skips_a_build_missing_one_brand_installer(self):
        payload = [{
            "tag_name": "V8.1.0.4",
            "published_at": "2026-10-18T00:00:00Z",
            "assets": gh_assets(names=["TigerClaw-Setup.exe"]),
        }]
        found = history.releases_from_github_payload(payload, NAMES, MIRROR, [])
        self.assertEqual(found, [])

    def test_backfill_probes_only_after_every_brand_installer_is_present(self):
        probed = []

        def sizes(url):
            probed.append(url)
            return 10

        missing_digest = gh_assets()
        missing_digest[0]["digest"] = ""
        payload = [
            {
                "tag_name": "V3",
                "published_at": "2026-10-19T00:00:00Z",
                "assets": gh_assets(names=["TigerClaw-Setup.exe"]),
            },
            {
                "tag_name": "V2",
                "published_at": "2026-10-18T00:00:00Z",
                "assets": missing_digest,
            },
            {
                "tag_name": "V1",
                "published_at": "2026-10-17T15:37:47.123456Z",
                "assets": gh_assets(),
            },
        ]
        found = history.releases_from_github_payload(payload, NAMES, MIRROR, [], mirror_size=sizes)
        self.assertEqual([item["build"] for item in found], ["V1"])
        self.assertEqual(found[0]["published_at"], "2026-10-17T15:37:47Z")
        self.assertEqual(
            probed,
            [f"{MIRROR}/releases/V1/{name}" for name in history.rollback_names(NAMES)],
        )

    def test_catalogue_stores_utc_rfc3339_seconds(self):
        current = entry("NOW", published_at="2026-12-01T08:00:00.9+08:00")
        older = entry("V1", published_at="2026-06-01T00:00:00.123456Z")
        chosen = history.choose_catalogue(current, [older], [], MIRROR, NAMES)
        self.assertEqual([item["build"] for item in chosen], ["NOW", "V1"])
        self.assertEqual(chosen[0]["published_at"], "2026-12-01T00:00:00Z")
        self.assertEqual(chosen[1]["published_at"], "2026-06-01T00:00:00Z")
        self.assertEqual(history.canonical_published_at("not-a-date"), "")
        self.assertEqual(history.canonical_published_at("2026-10-18T15:37:47"), "")

    def test_github_backfill_uses_the_desktop_release_page(self):
        self.assertIn("per_page=30", history.GITHUB_RELEASES_URL)

    def test_duplicate_build_keeps_the_newer_publication(self):
        current = entry("NOW", published_at="2026-12-01T00:00:00Z")
        older = entry("V1", published_at="2026-01-01T00:00:00Z")
        newer = entry("V1", published_at="2026-06-01T00:00:00Z")
        newer["assets"]["TigerClaw-Setup.exe"]["sha256"] = "b" * 64
        other = entry("V0", published_at="2026-03-01T00:00:00Z")
        chosen = history.choose_catalogue(current, [older, other], [newer], MIRROR, NAMES)
        self.assertEqual([item["build"] for item in chosen], ["NOW", "V1", "V0"])
        self.assertEqual(chosen[1]["published_at"], "2026-06-01T00:00:00Z")
        self.assertEqual(chosen[1]["assets"]["TigerClaw-Setup.exe"]["sha256"], "b" * 64)


if __name__ == "__main__":
    unittest.main()
