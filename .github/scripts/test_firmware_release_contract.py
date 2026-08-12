#!/usr/bin/env python3
"""CI contract tests for the four-board firmware publication format."""

import hashlib
import importlib.util
import json
import zipfile
import os
import pathlib
import re
import sys
import tempfile
import unittest
from unittest import mock


ROOT = pathlib.Path(__file__).resolve().parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


def load_module(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


contract = load_module("firmware_manifest_contract")
sys.modules["firmware_manifest_contract"] = contract
os.environ.setdefault("COS_PUBLIC_BASE_URL", "https://cos.example")
os.environ.setdefault("RELEASE_TAG", "v-test")
sync = load_module("sync_cos_release")
verify = load_module("verify_firmware_mirrors")


OFFICIAL_FIRMWARE_PROFILES = {
    "echoear-2st": {
        "firmware_board": "echoear-2st-r8",
        "layout_id": "maclaw-s3-16m-factory-v2",
        "flash_bytes": "16777216",
        "catalog_flash_bytes": "16 * 1024 * 1024",
        "asset": "MaClaw-ESP32S3-EchoEar-2ST-firmware.clawfw",
    },
    "bread-compact": {
        "firmware_board": "bread-compact-wifi-lcd-v1",
        "layout_id": "maclaw-s3-16m-factory-v2",
        "flash_bytes": "16777216",
        "catalog_flash_bytes": "16 * 1024 * 1024",
        "asset": "MaClaw-ESP32S3-Bread-Compact-firmware.clawfw",
    },
    "fangtang-4g": {
        "firmware_board": "fangtang-4g-v1",
        "layout_id": "maclaw-s3-16m-factory-v2",
        "flash_bytes": "16777216",
        "catalog_flash_bytes": "16 * 1024 * 1024",
        "asset": "MaClaw-ESP32S3-Fangtang-4G-firmware.clawfw",
    },
    "waveshare-amoled-1.75c": {
        "firmware_board": "waveshare-s3-touch-amoled-1.75c-v1",
        "layout_id": "maclaw-s3-32m-factory-v1",
        "flash_bytes": "33554432",
        "catalog_flash_bytes": "32 * 1024 * 1024",
        "asset": "MaClaw-ESP32S3-Waveshare-AMOLED-1.75C-firmware.clawfw",
    },
}


class FirmwareReleaseContractTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.assets = pathlib.Path(self.temp.name)
        self.payloads = {}
        for index, name in enumerate(contract.FIRMWARE_ASSETS):
            payload = f"signed firmware {index}".encode("utf-8")
            self._write_split_package(name, payload)
            self.payloads[name] = payload

    def _write_split_package(self, name, payload):
        manifest = {
            "mode": "full",
	    "channel": "stable",
	    "recovery": {"powerLossBootable": False},
            "writeOrder": ["storage", "app", "partition-table", "bootloader"],
            "files": [
                {"name": "storage", "region": "storage"},
                {"name": "app", "region": "app"},
                {"name": "partition-table", "region": "partition-table"},
                {"name": "bootloader", "region": "bootloader"},
                {"path": "metadata/partition-table.bin", "region": "metadata"},
            ],
        }
        with zipfile.ZipFile(self.assets / name, "w") as archive:
            archive.writestr("manifest.json", json.dumps(manifest))
            archive.writestr("payload.bin", payload)

    def manifest(self, mutate=None):
        entries = {}
        for name in self.payloads:
            path = self.assets / name
            entries[name] = {
                "name": name,
                "size": path.stat().st_size,
                "sha256": contract.sha256_file(path),
                "url": "https://cos.example/latest/" + name,
                "urls": [
                    "https://r2.example/latest/" + name,
                    "https://cos.example/latest/" + name,
                ],
            }
        result = {"tag": "v1.2.3", "version": "v1.2.3", "assets": entries}
        if mutate:
            mutate(result)
        return result

    def test_manifest_requires_all_four_exact_assets(self):
        _, found = contract.required_firmware(self.manifest(), self.assets, "v1.2.3")
        self.assertEqual(set(contract.FIRMWARE_ASSETS), set(found))
        self.assertEqual(4, len(found))

    def test_release_archives_require_signed_split_write_order(self):
        contract.require_split_firmware_archives(self.assets)
        with zipfile.ZipFile(self.assets / contract.FIRMWARE_ASSETS[0], "w") as archive:
            archive.writestr(
                "manifest.json",
                json.dumps({"mode": "full", "channel": "stable", "recovery": {"powerLossBootable": False}, "files": []}),
            )
        with self.assertRaisesRegex(RuntimeError, "split writeOrder"):
            contract.require_split_firmware_archives(self.assets)

    def test_release_archives_require_matching_signed_channel(self):
        contract.require_archive_channel(self.assets, "stable")
        with self.assertRaisesRegex(RuntimeError, "does not match release channel"):
            contract.require_archive_channel(self.assets, "beta")

    def test_release_archives_require_explicit_single_slot_recovery_risk(self):
        path = self.assets / contract.FIRMWARE_ASSETS[0]
        with zipfile.ZipFile(path) as archive:
            manifest = json.loads(archive.read("manifest.json"))
        manifest["recovery"]["powerLossBootable"] = True
        with zipfile.ZipFile(path, "w") as archive:
            archive.writestr("manifest.json", json.dumps(manifest))
        with self.assertRaisesRegex(RuntimeError, "powerLossBootable=false"):
            contract.require_split_firmware_archives(self.assets)

    def test_manifest_rejects_missing_or_mismatched_release_metadata(self):
        bad_tag = self.manifest(lambda value: value.update({"version": "v1.2.4"}))
        with self.assertRaisesRegex(RuntimeError, "tag/version"):
            contract.required_firmware(bad_tag, self.assets)
        missing = self.manifest(lambda value: value["assets"].pop(contract.FIRMWARE_ASSETS[1]))
        with self.assertRaisesRegex(RuntimeError, "missing"):
            contract.required_firmware(missing, self.assets)
        bad_digest = self.manifest(lambda value: value["assets"][contract.FIRMWARE_ASSETS[0]].update({"sha256": "0" * 64}))
        with self.assertRaisesRegex(RuntimeError, "SHA-256"):
            contract.required_firmware(bad_digest, self.assets)

    def test_manifest_writer_is_gated_by_the_same_four_board_contract(self):
        previous = {
            "tag": sync.tag,
            "asset_dir": sync.asset_dir,
            "r2_public_base_url": sync.r2_public_base_url,
            "public_base_url": sync.public_base_url,
        }
        try:
            sync.tag = "v1.2.3"
            sync.asset_dir = self.assets
            sync.r2_public_base_url = contract.R2_PUBLIC_BASE_URL
            sync.public_base_url = contract.COS_PUBLIC_BASE_URL
            result = sync.write_latest_manifest([self.assets / name for name in contract.FIRMWARE_ASSETS])
            manifest = json.loads(result.read_text(encoding="utf-8"))
            _, found = contract.required_firmware(manifest, self.assets, "v1.2.3")
            self.assertEqual(4, len(found))
            with self.assertRaisesRegex(RuntimeError, "missing"):
                sync.write_latest_manifest([self.assets / name for name in contract.FIRMWARE_ASSETS[:-1]])
        finally:
            for key, value in previous.items():
                setattr(sync, key, value)

    def test_public_mirror_bases_must_match_desktop_allowlist(self):
        self.assertEqual(
            "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev",
            contract.validate_public_mirror_base(
                "R2_PUBLIC_BASE_URL",
                "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/",
                contract.R2_PUBLIC_BASE_URL,
            ),
        )
        self.assertEqual(
            "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com",
            contract.validate_public_mirror_base(
                "COS_PUBLIC_BASE_URL",
                "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com",
                contract.COS_PUBLIC_BASE_URL,
            ),
        )
        for label, value, expected in (
            ("R2_PUBLIC_BASE_URL", "http://pub-c837069cbe31469590a5fea6235b436b.r2.dev", contract.R2_PUBLIC_BASE_URL),
            ("R2_PUBLIC_BASE_URL", "https://other.example", contract.R2_PUBLIC_BASE_URL),
            ("COS_PUBLIC_BASE_URL", "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/proxy", contract.COS_PUBLIC_BASE_URL),
        ):
            with self.assertRaisesRegex(RuntimeError, "desktop-approved"):
                contract.validate_public_mirror_base(label, value, expected)

    def test_public_manifest_may_change_format_but_not_four_board_metadata(self):
        local = self.manifest()
        _, expected = contract.required_firmware(local, self.assets, "v1.2.3")
        remote = json.loads(json.dumps(local))
        remote["generatedAt"] = "2026-08-07T00:00:00Z"
        remote["assets"][contract.FIRMWARE_ASSETS[0]]["urls"] = [
            f"{contract.R2_PUBLIC_BASE_URL}/latest/{contract.FIRMWARE_ASSETS[0]}",
            f"{contract.COS_PUBLIC_BASE_URL}/latest/{contract.FIRMWARE_ASSETS[0]}",
        ]
        remote["assets"][contract.FIRMWARE_ASSETS[0]]["url"] = remote["assets"][contract.FIRMWARE_ASSETS[0]]["urls"][-1]
        for name in contract.FIRMWARE_ASSETS[1:]:
            remote["assets"][name]["urls"] = [
                f"{contract.R2_PUBLIC_BASE_URL}/latest/{name}",
                f"{contract.COS_PUBLIC_BASE_URL}/latest/{name}",
            ]
            remote["assets"][name]["url"] = remote["assets"][name]["urls"][-1]
        verify.validate_remote_manifest(remote, local, expected, "R2")
        remote["assets"][contract.FIRMWARE_ASSETS[0]]["size"] += 1
        with self.assertRaisesRegex(RuntimeError, "metadata differs"):
            verify.validate_remote_manifest(remote, local, expected, "R2")

    def test_manifest_rejects_wrong_mirror_path_or_missing_independent_source(self):
        manifest = self.manifest()
        for name, entry in manifest["assets"].items():
            entry["urls"] = [
                f"{contract.R2_PUBLIC_BASE_URL}/latest/{name}",
                f"{contract.COS_PUBLIC_BASE_URL}/latest/{name}",
            ]
            entry["url"] = entry["urls"][-1]
        contract.validate_manifest_asset_urls(manifest)
        manifest["assets"][contract.FIRMWARE_ASSETS[0]]["urls"] = [
            f"{contract.R2_PUBLIC_BASE_URL}/latest/{contract.FIRMWARE_ASSETS[0]}"
        ]
        with self.assertRaisesRegex(RuntimeError, "approved latest topology"):
            contract.validate_manifest_asset_urls(manifest)

    def test_release_workflow_runs_contract_test_before_publication(self):
        workflow = (ROOT.parent / "workflows" / "main.yml").read_text(encoding="utf-8")
        command = "python3 .github/scripts/test_firmware_release_contract.py"
        self.assertIn(command, workflow)
        self.assertIn("  verify-firmware-release-contract:", workflow)
        self.assertIn("needs: [verify-firmware-release-contract]", workflow)
        self.assertLess(workflow.index(command), workflow.index("- name: Generate latest manifest"))
        self.assertIn("Verify firmware publication on Cloudflare R2 and Tencent COS", workflow)
        self.assertLess(workflow.index("Verify firmware publication on Cloudflare R2 and Tencent COS"), workflow.index("- name: Create GitHub Release"))
        self.assertNotIn("COS_PUBLIC_BASE_URL: ${{ secrets.COS_PUBLIC_BASE_URL }}", workflow)
        self.assertIn("COS_PUBLIC_BASE_URL: https://maclaw-1252723594.cos.ap-beijing.myqcloud.com", workflow)

    def test_firmware_build_and_mirror_publication_use_protected_release_environment(self):
        workflow = (ROOT.parent / "workflows" / "main.yml").read_text(encoding="utf-8")
        firmware_job = workflow[workflow.index("  build-esp32-firmware:") : workflow.index("  build-clawmate-maker:")]
        desktop_job = workflow[workflow.index("  build-clawmate-maker:") : workflow.index("  # ============================================================\n  # Release:")]
        release_job = workflow[workflow.index("  release:") :]
        self.assertIn("environment: firmware-release", firmware_job)
        self.assertIn("github.ref_type == 'tag'", firmware_job)
        self.assertIn("if: github.ref_type == 'tag'", desktop_job)
        self.assertIn("environment: firmware-release", release_job)
        self.assertIn("if: github.ref_type == 'tag'", release_job)

    def test_firmware_pipeline_can_be_temporarily_paused_without_blocking_desktop_release(self):
        workflow = (ROOT.parent / "workflows" / "main.yml").read_text(encoding="utf-8")
        firmware_job = workflow[workflow.index("  build-esp32-firmware:") : workflow.index("  build-clawmate-maker:")]
        desktop_job = workflow[workflow.index("  build-clawmate-maker:") : workflow.index("  # ============================================================\n  # Release:")]
        release_job = workflow[workflow.index("  release:") :]

        self.assertIn("ESP32_FIRMWARE_ENABLED: 'false'", workflow)
        self.assertIn("if: ${{ github.ref_type == 'tag' && false }}", firmware_job)
        self.assertNotIn("needs: [build-esp32-firmware]", desktop_job)
        self.assertNotIn("build-esp32-firmware", release_job.split("runs-on:", 1)[0])
        self.assertIn("if: env.ESP32_FIRMWARE_ENABLED == 'true'", release_job)

    def test_firmware_packaging_binds_manifest_identity_to_generated_sdkconfig(self):
        workflow = (ROOT.parent / "workflows" / "main.yml").read_text(encoding="utf-8")
        firmware_job = workflow[workflow.index("  build-esp32-firmware:") : workflow.index("  build-clawmate-maker:")]
        self.assertIn('--sdkconfig-header "$project_dir/$build_dir/config/sdkconfig.h"', firmware_job)

    def test_firmware_workflow_builds_all_catalog_profiles_with_their_own_locks(self):
        workflow = (ROOT.parent / "workflows" / "main.yml").read_text(encoding="utf-8")
        firmware_job = workflow[workflow.index("  build-esp32-firmware:") : workflow.index("  build-clawmate-maker:")]
        for profile in ("echoear-2st", "bread-compact", "fangtang-4g", "waveshare-amoled-1.75c"):
            self.assertIn(f"profile: {profile}", firmware_job)
        self.assertIn('MACLAW_PROFILE: ${{ matrix.profile }}', firmware_job)
        self.assertIn('IDF_TARGET: esp32s3', firmware_job)
        self.assertIn('-e IDF_TARGET', firmware_job)
        self.assertIn('test "$IDF_TARGET" = esp32s3', firmware_job)
        for profile, directory in (
            ("echoear-2st", "profile_components/echoear_deps"),
            ("bread-compact", "''"),
            ("fangtang-4g", "profile_components/fangtang_deps"),
            ("waveshare-amoled-1.75c", "profile_components/waveshare_deps"),
        ):
            matrix = re.search(
                rf"(?ms)^          - device: {re.escape(profile)}$.*?(?=^          - device:|^    steps:)",
                firmware_job,
            )
            self.assertIsNotNone(matrix, f"workflow matrix lacks {profile}")
            self.assertIn(f"extra_component_dirs: {directory}", matrix.group(0))
        self.assertIn('EXTRA_COMPONENT_DIRS: ${{ matrix.extra_component_dirs }}', firmware_job)
        self.assertIn('-e EXTRA_COMPONENT_DIRS', firmware_job)
        self.assertIn('-D EXTRA_COMPONENT_DIRS="$EXTRA_COMPONENT_DIRS"', firmware_job)
        self.assertIn('if [ "$MACLAW_PROFILE" = fangtang-4g ]; then', firmware_job)
        self.assertIn('test -f managed_components/78__esp_lcd_nv3023/CMakeLists.txt', firmware_job)
        self.assertIn('test -f managed_components/78__esp-ml307/CMakeLists.txt', firmware_job)
        self.assertIn('-D MACLAW_PROFILE="$MACLAW_PROFILE"', firmware_job)

    def test_firmware_workflow_passes_each_profile_flash_capacity_to_fwpack(self):
        workflow = (ROOT.parent / "workflows" / "main.yml").read_text(encoding="utf-8")
        firmware_job = workflow[workflow.index("  build-esp32-firmware:") : workflow.index("  build-clawmate-maker:")]
        self.assertIn("flash_bytes: 16777216", firmware_job)
        self.assertIn("flash_bytes: 33554432", firmware_job)
        self.assertIn("--flash-bytes '${{ matrix.flash_bytes }}'", firmware_job)

    def test_catalog_workflow_and_manifest_share_one_exact_four_board_contract(self):
        workflow = (ROOT.parent / "workflows" / "main.yml").read_text(encoding="utf-8")
        firmware_job = workflow[workflow.index("  build-esp32-firmware:") : workflow.index("  build-clawmate-maker:")]
        catalog = (ROOT.parent.parent / "ClawMateMaker" / "internal" / "catalog" / "catalog.go").read_text(encoding="utf-8")

        self.assertEqual(
            {profile["asset"] for profile in OFFICIAL_FIRMWARE_PROFILES.values()},
            set(contract.FIRMWARE_ASSETS),
        )
        for device, expected in OFFICIAL_FIRMWARE_PROFILES.items():
            matrix = re.search(
                rf"(?ms)^          - device: {re.escape(device)}$.*?(?=^          - device:|^    steps:)",
                firmware_job,
            )
            self.assertIsNotNone(matrix, f"workflow matrix lacks {device}")
            for field in ("firmware_board", "layout_id", "flash_bytes"):
                self.assertIn(f"{field}: {expected[field]}", matrix.group(0))
            self.assertIn(f'AssetName: "{expected["asset"]}"', catalog)
            self.assertIn(f'FirmwareBoardID: "{expected["firmware_board"]}"', catalog)
            self.assertIn(f'FlashBytes: {expected["catalog_flash_bytes"]}', catalog)
            self.assertIn(f'"{expected["asset"]}"', workflow)

    def test_firmware_version_uses_one_monotonic_ci_build_sequence(self):
        workflow = (ROOT.parent / "workflows" / "main.yml").read_text(encoding="utf-8")
        firmware_job = workflow[workflow.index("  build-esp32-firmware:") : workflow.index("  build-clawmate-maker:")]
        self.assertIn("printf 'CONFIG_MACLAW_RELEASE_SEQUENCE=%s\\n' \"${{ github.run_number }}\"", firmware_job)
        self.assertIn("--release-sequence '${{ github.run_number }}'", firmware_job)
        identity = (ROOT.parent.parent / "iot-agentos" / "main" / "firmware_identity.c").read_text(encoding="utf-8")
        self.assertIn('"release_sequence", CONFIG_MACLAW_RELEASE_SEQUENCE', identity)
        self.assertIn('"firmware_version", CONFIG_MACLAW_RELEASE_SEQUENCE', identity)

        boot_verify = (ROOT.parent.parent / "ClawMateMaker" / "internal" / "verify" / "boot.go").read_text(encoding="utf-8")
        self.assertIn('json:"firmware_version"', boot_verify)
        self.assertIn('s.FirmwareVersion != expected.ReleaseSequence', boot_verify)

    def test_desktop_distributions_verify_signed_sidecar_before_packaging(self):
        workflow = (ROOT.parent / "workflows" / "main.yml").read_text(encoding="utf-8")
        desktop_job = workflow[workflow.index("  build-clawmate-maker:") : workflow.index("  # ============================================================\n  # Release:")]
        verify = "Verify signed managed sidecar against desktop release trust root"
        self.assertIn(verify, desktop_job)
        self.assertIn("go run ./tools/sidecarverify", desktop_job)
        self.assertLess(desktop_job.index(verify), desktop_job.index("- name: Package desktop flasher"))

    def test_desktop_distribution_tests_the_embedded_trust_boundary(self):
        workflow = (ROOT.parent / "workflows" / "main.yml").read_text(encoding="utf-8")
        desktop_job = workflow[workflow.index("  build-clawmate-maker:") : workflow.index("  # ============================================================\n  # Release:")]
        self.assertIn("- name: Test desktop flasher safety boundary", desktop_job)
        self.assertIn("go test ./...", desktop_job)
        verifier = (ROOT.parent.parent / "ClawMateMaker" / "tools" / "sidecarverify" / "main.go").read_text(encoding="utf-8")
        self.assertIn("verifyEmbeddedTrustMetadata(os.Args[1], keyID, publicKey)", verifier)
        self.assertLess(verifier.index("verifyEmbeddedTrustMetadata(os.Args[1], keyID, publicKey)"), verifier.index("flash.ConfigureSidecar(os.Args[1], true)"))

    def test_desktop_ui_exposes_only_signed_stable_and_beta_channels(self):
        ui_path = ROOT.parent.parent / "ClawMateMaker" / "frontend" / "dist" / "index.html"
        ui = ui_path.read_text(encoding="utf-8")
        self.assertIn('id="releaseChannel"', ui)
        self.assertIn('value="stable"', ui)
        self.assertIn('value="beta"', ui)
        self.assertIn("GetLatestFirmwareForChannel", ui)
        self.assertIn("clawmate-maker.release-channel", ui)
        self.assertNotIn('value="dev"', ui)


if __name__ == "__main__":
    unittest.main()
