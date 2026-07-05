from __future__ import annotations

import shlex
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import create_qa_build_record
import build_android_release
import release_evidence_commands


class ReleaseEvidenceCommandsTest(unittest.TestCase):
    def test_valid_scopes_match_qa_record_creator(self) -> None:
        self.assertIs(
            create_qa_build_record.VALID_SCOPES,
            release_evidence_commands.VALID_SCOPES,
        )

    def test_final_decision_prefills_match_qa_record_creator_validation(self) -> None:
        prefills = release_evidence_commands.final_decision_prefills("1.0.0+42")

        self.assertEqual(
            {
                "Release handoff result": (
                    "release_handoff.py output saved to "
                    "docs/qa-builds/handoff-1.0.0+42.md"
                ),
                "Runtime boundary verification result": (
                    "MaClaw Mobile runtime boundary verified. "
                    "log: docs/qa-builds/runtime-boundary-1.0.0+42.log"
                ),
                "Automated release gates result": (
                    "run_release_gates.py: 38 gates passed; "
                    "log: docs/qa-builds/release-gates-1.0.0+42.log"
                ),
            },
            prefills,
        )
        self.assertEqual(
            [],
            create_qa_build_record.final_decision_prefill_errors(prefills),
        )
        android_prefills = release_evidence_commands.final_decision_prefills(
            "1.0.0+42",
            scope="android",
        )
        self.assertEqual(
            "release_handoff.py output saved to "
            "docs/qa-builds/handoff-android-1.0.0+42.md",
            android_prefills["Release handoff result"],
        )
        self.assertEqual(
            [],
            create_qa_build_record.final_decision_prefill_errors(android_prefills),
        )

    def test_create_record_command_uses_validator_compatible_prefills(self) -> None:
        command = release_evidence_commands.create_record_command(
            scope="android-ios",
            version="1.0.0+42",
        )

        self.assertEqual(
            'python3 tool/create_qa_build_record.py --scope android-ios --version 1.0.0+42 '
            '--release-handoff-result "release_handoff.py output saved to docs/qa-builds/handoff-1.0.0+42.md" '
            '--runtime-boundary-result "MaClaw Mobile runtime boundary verified. log: docs/qa-builds/runtime-boundary-1.0.0+42.log" '
            '--automated-gates-result "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log"',
            command,
        )

    def test_custom_records_dir_flows_through_record_prefills_and_logs(self) -> None:
        records_dir = "tmp/qa-builds"

        self.assertEqual(
            {
                "Release handoff result": (
                    "release_handoff.py output saved to "
                    "tmp/qa-builds/handoff-android-1.0.0+42.md"
                ),
                "Runtime boundary verification result": (
                    "MaClaw Mobile runtime boundary verified. "
                    "log: tmp/qa-builds/runtime-boundary-1.0.0+42.log"
                ),
                "Automated release gates result": (
                    "run_release_gates.py: 38 gates passed; "
                    "log: tmp/qa-builds/release-gates-1.0.0+42.log"
                ),
            },
            release_evidence_commands.final_decision_prefills(
                "1.0.0+42",
                scope="android",
                records_dir=records_dir,
            ),
        )
        self.assertEqual(
            'python3 tool/create_qa_build_record.py --scope android --version 1.0.0+42 '
            '--release-handoff-result "release_handoff.py output saved to tmp/qa-builds/handoff-android-1.0.0+42.md" '
            '--runtime-boundary-result "MaClaw Mobile runtime boundary verified. log: tmp/qa-builds/runtime-boundary-1.0.0+42.log" '
            '--automated-gates-result "run_release_gates.py: 38 gates passed; log: tmp/qa-builds/release-gates-1.0.0+42.log" '
            "--records-dir tmp/qa-builds",
            release_evidence_commands.create_record_command(
                scope="android",
                version="1.0.0+42",
                records_dir=records_dir,
            ),
        )

    def test_custom_records_dir_flows_through_signed_qa_record_hint(self) -> None:
        hint = release_evidence_commands.signed_qa_record_hint(
            scope="android",
            version="1.0.0+42",
            records_dir="tmp/qa-builds",
        )

        for expected in [
            release_evidence_commands.release_handoff_command(
                scope="android",
                version="1.0.0+42",
                records_dir="tmp/qa-builds",
            ),
            release_evidence_commands.runtime_boundary_command(
                "1.0.0+42",
                records_dir="tmp/qa-builds",
            ),
            release_evidence_commands.release_gates_command(
                "1.0.0+42",
                records_dir="tmp/qa-builds",
            ),
            release_evidence_commands.create_record_command(
                scope="android",
                version="1.0.0+42",
                records_dir="tmp/qa-builds",
            ),
            release_evidence_commands.android_artifact_evidence_command(
                "1.0.0+42",
                record_dir="tmp/qa-builds",
            ),
            release_evidence_commands.validate_qa_build_record_command(
                "tmp/qa-builds/<YYYY-MM-DD>-android-1.0.0+42.md",
            ),
            release_evidence_commands.qa_build_record_report_command(
                "tmp/qa-builds/<YYYY-MM-DD>-android-1.0.0+42.md",
            ),
            release_evidence_commands.qa_release_evidence_link_command(
                scope="android",
                records_dir="tmp/qa-builds",
            ),
            release_evidence_commands.verify_final_release_evidence_command(
                "tmp/qa-builds",
                scope="android",
                version="1.0.0+42",
                log="tmp/qa-builds/final-release-evidence-android-1.0.0+42.log",
            ),
        ]:
            self.assertIn(expected, hint)
        self.assertNotIn("--record-dir docs/qa-builds", hint)

    def test_handoff_evidence_path_is_shared(self) -> None:
        self.assertEqual(
            "docs/qa-builds/handoff-1.0.0+42.md",
            release_evidence_commands.handoff_evidence_path("1.0.0+42"),
        )
        prefills = release_evidence_commands.final_decision_prefills("1.0.0+42")
        self.assertIn(
            release_evidence_commands.handoff_evidence_path("1.0.0+42"),
            prefills["Release handoff result"],
        )
        self.assertEqual(
            "docs/qa-builds/handoff-android-1.0.0+42.md",
            release_evidence_commands.handoff_evidence_path(
                "1.0.0+42",
                scope="android",
            ),
        )
        self.assertEqual(
            "docs/qa-builds/handoff-ios-1.0.0+42.md",
            release_evidence_commands.handoff_evidence_path(
                "1.0.0+42",
                scope="ios",
            ),
        )

    def test_qa_record_path_placeholder_is_shared(self) -> None:
        self.assertEqual(
            "docs/qa-builds/<YYYY-MM-DD>-android-ios-1.0.0+42.md",
            release_evidence_commands.qa_record_path_placeholder(
                scope="android-ios",
                version="1.0.0+42",
            ),
        )
        self.assertEqual(
            "docs/qa-builds/2026-07-02-ios-1.0.0+42.md",
            release_evidence_commands.qa_record_path_placeholder(
                scope="ios",
                version="1.0.0+42",
                date="2026-07-02",
            ),
        )

    def test_final_release_evidence_command_always_includes_scope(self) -> None:
        self.assertEqual(
            "python3 tool/verify_final_release_evidence.py docs/qa-builds "
            "--scope android-ios --log docs/qa-builds/final-release-evidence-1.0.0+42.log",
            release_evidence_commands.verify_final_release_evidence_command(
                version="1.0.0+42",
            ),
        )
        self.assertEqual(
            "python3 tool/verify_final_release_evidence.py docs/qa-builds "
            "--scope android --log docs/qa-builds/final-release-evidence-android-1.0.0+42.log",
            release_evidence_commands.verify_final_release_evidence_command(
                scope="android",
                version="1.0.0+42",
            ),
        )
        self.assertEqual(
            "python3 tool/verify_final_release_evidence.py docs/qa-builds "
            "--scope ios --log docs/qa-builds/final-release-evidence-ios-1.0.0+42.log",
            release_evidence_commands.verify_final_release_evidence_command(
                scope="ios",
                version="1.0.0+42",
            ),
        )

    def test_create_record_command_can_create_prefilled_record(self) -> None:
        command = release_evidence_commands.create_record_command(
            scope="android-ios",
            version="1.0.0+42",
        )
        argv = shlex.split(command)[2:]

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text(
                "Date: YYYY-MM-DD\n"
                "Version/build number: app version + build number, such as 1.0.0+42\n"
                "Release handoff result:\n"
                "Runtime boundary verification result:\n"
                "Automated release gates result:\n",
                encoding="utf-8",
            )

            with redirect_stdout(StringIO()):
                exit_code = create_qa_build_record.main(
                    [
                        "--date",
                        "2026-07-02",
                        *argv,
                        "--template",
                        str(template),
                        "--records-dir",
                        str(records_dir),
                    ],
                )

            self.assertEqual(0, exit_code)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            text = record.read_text(encoding="utf-8")
            for value in release_evidence_commands.final_decision_prefills(
                "1.0.0+42",
            ).values():
                self.assertIn(value, text)

    def test_create_record_command_prefills_scoped_handoff_path(self) -> None:
        command = release_evidence_commands.create_record_command(
            scope="android",
            version="1.0.0+42",
        )
        argv = shlex.split(command)[2:]

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text(
                "Date: YYYY-MM-DD\n"
                "Version/build number: app version + build number, such as 1.0.0+42\n"
                "Release handoff result:\n"
                "Runtime boundary verification result:\n"
                "Automated release gates result:\n",
                encoding="utf-8",
            )

            with redirect_stdout(StringIO()):
                exit_code = create_qa_build_record.main(
                    [
                        "--date",
                        "2026-07-02",
                        *argv,
                        "--template",
                        str(template),
                        "--records-dir",
                        str(records_dir),
                    ],
                )

            self.assertEqual(0, exit_code)
            record = records_dir / "2026-07-02-android-1.0.0+42.md"
            text = record.read_text(encoding="utf-8")
            self.assertIn(
                "Release handoff result: release_handoff.py output saved to "
                "docs/qa-builds/handoff-android-1.0.0+42.md",
                text,
            )
            self.assertNotIn(
                "Release handoff result: release_handoff.py output saved to "
                "docs/qa-builds/handoff-1.0.0+42.md",
                text,
            )

    def test_signed_qa_record_hint_uses_default_placeholders(self) -> None:
        hint = release_evidence_commands.signed_qa_record_hint()

        self.assertIn(
            "release handoff is only a QA plan, not a completed QA record",
            hint,
        )
        for expected in [
            release_evidence_commands.release_handoff_command(),
            "--team-id <APPLE_TEAM_ID>",
            "--export-method <export-method>",
            "docs/qa-builds/handoff-<version+build>.md",
            release_evidence_commands.setup_android_signing_command(),
            release_evidence_commands.setup_ios_export_options_command(),
            release_evidence_commands.qa_preflight_command(
                team_id="<APPLE_TEAM_ID>",
                export_method="<export-method>",
            ),
            release_evidence_commands.runtime_boundary_command(),
            release_evidence_commands.release_gates_command(),
            release_evidence_commands.create_record_command(),
            release_evidence_commands.android_artifact_evidence_command(),
            release_evidence_commands.ios_release_plan_command(),
            release_evidence_commands.ios_release_plan_command(
                provisioning_profiles="<Runner profile UUID/name; Share Extension profile UUID/name>",
                record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
            ),
            release_evidence_commands.ios_artifact_evidence_command(
                archive_or_build="build/ios/archive/MaClawMobile.xcarchive",
            ),
            release_evidence_commands.validate_qa_build_record_command(),
            release_evidence_commands.qa_build_record_report_command(),
            release_evidence_commands.qa_release_evidence_link_command(),
            release_evidence_commands.verify_final_release_evidence_command(
                version="<version+build>",
            ),
        ]:
            self.assertIn(expected, hint)
        self.assertIn(
            "plan iOS archive/export first with",
            hint,
        )
        self.assertIn(
            "after the signed .xcarchive/TestFlight build exists",
            hint,
        )
        self.assertNotIn(
            "generate iOS artifact evidence during archive planning",
            hint,
        )
        self.assertLess(
            hint.index(release_evidence_commands.release_handoff_command()),
            hint.index(release_evidence_commands.setup_android_signing_command()),
        )
        self.assertLess(
            hint.index(release_evidence_commands.setup_android_signing_command()),
            hint.index(release_evidence_commands.setup_ios_export_options_command()),
        )
        self.assertLess(
            hint.index(release_evidence_commands.setup_ios_export_options_command()),
            hint.index(
                release_evidence_commands.qa_preflight_command(
                    team_id="<APPLE_TEAM_ID>",
                    export_method="<export-method>",
                ),
            ),
        )
        self.assertLess(
            hint.index(
                release_evidence_commands.qa_preflight_command(
                    team_id="<APPLE_TEAM_ID>",
                    export_method="<export-method>",
                ),
            ),
            hint.index(release_evidence_commands.runtime_boundary_command()),
        )
        self.assertLess(
            hint.index(release_evidence_commands.create_record_command()),
            hint.index(release_evidence_commands.android_artifact_evidence_command()),
        )
        self.assertLess(
            hint.index(release_evidence_commands.ios_release_plan_command()),
            hint.index(
                release_evidence_commands.ios_release_plan_command(
                    provisioning_profiles="<Runner profile UUID/name; Share Extension profile UUID/name>",
                    record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
                ),
            ),
        )
        self.assertLess(
            hint.index(
                release_evidence_commands.ios_release_plan_command(
                    provisioning_profiles="<Runner profile UUID/name; Share Extension profile UUID/name>",
                    record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
                ),
            ),
            hint.index(
                release_evidence_commands.ios_artifact_evidence_command(
                    archive_or_build="build/ios/archive/MaClawMobile.xcarchive",
                ),
            ),
        )
        self.assertLess(
            hint.index(
                release_evidence_commands.ios_artifact_evidence_command(
                    archive_or_build="build/ios/archive/MaClawMobile.xcarchive",
                ),
            ),
            hint.index(release_evidence_commands.validate_qa_build_record_command()),
        )

    def test_signed_qa_record_hint_distinguishes_handoff_from_completed_record(self) -> None:
        hint = release_evidence_commands.signed_qa_record_hint(
            scope="android",
            version="1.0.0+42",
        )

        self.assertIn("no completed signed-build QA records yet", hint)
        self.assertIn(
            "release handoff is only a QA plan, not a completed QA record",
            hint,
        )
        self.assertIn(
            release_evidence_commands.release_handoff_command(
                scope="android",
                version="1.0.0+42",
            ),
            hint,
        )
        self.assertLess(
            hint.index("not a completed QA record"),
            hint.index(release_evidence_commands.release_handoff_command(
                scope="android",
                version="1.0.0+42",
            )),
        )

    def test_signed_qa_record_hint_uses_scope_specific_artifact_commands(self) -> None:
        android_hint = release_evidence_commands.signed_qa_record_hint(
            scope="android",
            version="1.0.0+42",
            team_id="ABCDE12345",
        )

        self.assertIn(
            release_evidence_commands.android_artifact_evidence_command("1.0.0+42"),
            android_hint,
        )
        self.assertIn(
            release_evidence_commands.setup_android_signing_command(),
            android_hint,
        )
        self.assertIn(
            f"then run `{release_evidence_commands.qa_preflight_command(scope='android')}`",
            android_hint,
        )
        self.assertIn(
            release_evidence_commands.release_handoff_command(
                scope="android",
                version="1.0.0+42",
            ),
            android_hint,
        )
        self.assertNotIn(
            release_evidence_commands.qa_preflight_command(
                team_id="ABCDE12345",
                export_method="<export-method>",
            ),
            android_hint,
        )
        self.assertNotIn("--scope android --team-id", android_hint)
        self.assertNotIn("setup_ios_export_options.py", android_hint)
        self.assertNotIn(
            release_evidence_commands.ios_artifact_evidence_command(
                team_id="ABCDE12345",
            ),
            android_hint,
        )
        self.assertNotIn(
            release_evidence_commands.ios_release_plan_command(
                team_id="ABCDE12345",
                provisioning_profiles="<Runner profile UUID/name; Share Extension profile UUID/name>",
                record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
            ),
            android_hint,
        )

        ios_hint = release_evidence_commands.signed_qa_record_hint(
            scope="ios",
            version="1.0.0+42",
            team_id="ABCDE12345",
        )

        self.assertIn(
            release_evidence_commands.ios_artifact_evidence_command(
                archive_or_build="build/ios/archive/MaClawMobile.xcarchive",
                team_id="ABCDE12345",
            ),
            ios_hint,
        )
        self.assertIn(
            release_evidence_commands.ios_release_plan_command(
                team_id="ABCDE12345",
                export_method="<export-method>",
            ),
            ios_hint,
        )
        self.assertIn(
            release_evidence_commands.ios_release_plan_command(
                team_id="ABCDE12345",
                export_method="<export-method>",
                provisioning_profiles="<Runner profile UUID/name; Share Extension profile UUID/name>",
                record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
            ),
            ios_hint,
        )
        self.assertIn(
            release_evidence_commands.setup_ios_export_options_command(
                team_id="ABCDE12345",
            ),
            ios_hint,
        )
        self.assertIn(
            release_evidence_commands.qa_preflight_command(
                scope="ios",
                team_id="ABCDE12345",
                export_method="<export-method>",
            ),
            ios_hint,
        )
        self.assertNotIn("setup_android_signing.py", ios_hint)
        self.assertNotIn(
            release_evidence_commands.android_artifact_evidence_command("1.0.0+42"),
            ios_hint,
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(scope="android"),
            android_hint,
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(scope="ios"),
            ios_hint,
        )

    def test_scope_helpers_reject_unknown_scope(self) -> None:
        for call in [
            lambda: release_evidence_commands.validate_scope("ios-android"),
            lambda: release_evidence_commands.create_record_command(
                scope="ios-android",
            ),
            lambda: release_evidence_commands.release_handoff_command(
                scope="ios-android",
            ),
            lambda: release_evidence_commands.qa_record_path_placeholder(
                scope="ios-android",
            ),
            lambda: release_evidence_commands.signed_qa_record_hint(
                scope="ios-android",
            ),
        ]:
            with self.assertRaisesRegex(ValueError, "unsupported scope"):
                call()

    def test_scope_coverage_helpers_exclude_legacy_ios_android_scope(self) -> None:
        self.assertTrue(release_evidence_commands.scope_covers_android("android"))
        self.assertTrue(release_evidence_commands.scope_covers_android("android-ios"))
        self.assertFalse(release_evidence_commands.scope_covers_android("ios"))
        self.assertFalse(release_evidence_commands.scope_covers_android("ios-android"))
        self.assertFalse(release_evidence_commands.scope_covers_android(None))

        self.assertTrue(release_evidence_commands.scope_covers_ios("ios"))
        self.assertTrue(release_evidence_commands.scope_covers_ios("android-ios"))
        self.assertFalse(release_evidence_commands.scope_covers_ios("android"))
        self.assertFalse(release_evidence_commands.scope_covers_ios("ios-android"))
        self.assertFalse(release_evidence_commands.scope_covers_ios(None))

    def test_legacy_ios_android_scope_only_appears_in_tests(self) -> None:
        tool_dir = Path(__file__).resolve().parent

        offenders = [
            path.relative_to(tool_dir).as_posix()
            for path in sorted(tool_dir.glob("*.py"))
            if not path.name.endswith("_test.py")
            and "ios-android" in path.read_text(encoding="utf-8")
        ]

        self.assertEqual([], offenders)

    def test_release_status_and_handoff_commands_are_shared(self) -> None:
        self.assertEqual(
            "python3 tool/release_status_report.py --scope android-ios --team-id ABCDE12345 --export-method ad-hoc",
            release_evidence_commands.release_status_report_command(
                team_id="ABCDE12345",
                export_method="ad-hoc",
            ),
        )
        self.assertEqual(
            "python3 tool/release_status_report.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method <export-method>",
            release_evidence_commands.release_status_report_command(),
        )
        self.assertEqual(
            "python3 tool/release_status_report.py --scope android",
            release_evidence_commands.release_status_report_command(
                scope="android",
            ),
        )
        self.assertEqual(
            "python3 tool/release_handoff.py --version 1.0.0+42 --scope ios "
            "--team-id ABCDE12345 --export-method ad-hoc --output docs/qa-builds/handoff-ios-1.0.0+42.md",
            release_evidence_commands.release_handoff_command(
                version="1.0.0+42",
                scope="ios",
                team_id="ABCDE12345",
                export_method="ad-hoc",
            ),
        )
        self.assertEqual(
            "python3 tool/release_handoff.py --version 1.0.0+42 --scope android "
            "--output docs/qa-builds/handoff-android-1.0.0+42.md",
            release_evidence_commands.release_handoff_command(
                version="1.0.0+42",
                scope="android",
                team_id="ABCDE12345",
                export_method="ad-hoc",
            ),
        )
        self.assertEqual(
            "python3 tool/release_handoff.py --version 1.0.0+42 --scope android-ios "
            "--team-id ABCDE12345 --export-method ad-hoc --output custom-handoff.md",
            release_evidence_commands.release_handoff_command(
                version="1.0.0+42",
                team_id="ABCDE12345",
                export_method="ad-hoc",
                output="custom-handoff.md",
            ),
        )
        self.assertEqual(
            "python3 tool/qa_preflight.py --scope android-ios",
            release_evidence_commands.qa_preflight_command(),
        )
        self.assertEqual(
            "python3 tool/qa_preflight.py --scope android-ios --team-id ABCDE12345 --export-method ad-hoc",
            release_evidence_commands.qa_preflight_command(
                team_id="ABCDE12345",
                export_method="ad-hoc",
            ),
        )
        self.assertEqual(
            "python3 tool/qa_preflight.py --scope android --records-dir custom-records",
            release_evidence_commands.qa_preflight_command(
                scope="android",
                records_dir="custom-records",
            ),
        )

    def test_runtime_boundary_and_release_gate_commands_are_shared(self) -> None:
        self.assertEqual(
            "python3 tool/verify_runtime_boundary.py --log docs/qa-builds/runtime-boundary-1.0.0+42.log",
            release_evidence_commands.runtime_boundary_command("1.0.0+42"),
        )
        self.assertEqual(
            "python3 tool/run_release_gates.py --log docs/qa-builds/release-gates-1.0.0+42.log",
            release_evidence_commands.release_gates_command("1.0.0+42"),
        )

    def test_signing_setup_commands_are_shared(self) -> None:
        self.assertEqual(
            "python3 tool/setup_android_signing.py",
            release_evidence_commands.setup_android_signing_command(),
        )
        self.assertEqual(
            "python3 tool/setup_ios_export_options.py --team-id ABCDE12345 --export-method ad-hoc",
            release_evidence_commands.setup_ios_export_options_command(
                team_id="ABCDE12345",
                export_method="ad-hoc",
            ),
        )
        self.assertEqual(
            "python3 tool/setup_ios_export_options.py --team-id <APPLE_TEAM_ID> --export-method <export-method>",
            release_evidence_commands.setup_ios_export_options_command(),
        )

    def test_ios_release_plan_command_is_shared(self) -> None:
        self.assertEqual(
            "python3 tool/plan_ios_release.py --team-id ABCDE12345 --export-method ad-hoc",
            release_evidence_commands.ios_release_plan_command(
                team_id="ABCDE12345",
                export_method="ad-hoc",
            ),
        )
        self.assertEqual(
            "python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method <export-method>",
            release_evidence_commands.ios_release_plan_command(),
        )
        self.assertEqual(
            'python3 tool/plan_ios_release.py --team-id ABCDE12345 --export-method ad-hoc '
            '--provisioning-profiles "Runner profile; Share profile" '
            "--record-dir custom-records",
            release_evidence_commands.ios_release_plan_command(
                team_id="ABCDE12345",
                export_method="ad-hoc",
                provisioning_profiles="Runner profile; Share profile",
                record_dir="custom-records",
            ),
        )

    def test_ios_release_plan_command_requires_complete_evidence_options(self) -> None:
        with self.assertRaisesRegex(
            ValueError,
            "requires provisioning_profiles and record_dir together",
        ):
            release_evidence_commands.ios_release_plan_command(
                provisioning_profiles="Runner profile; Share profile",
            )
        with self.assertRaisesRegex(
            ValueError,
            "requires provisioning_profiles and record_dir together",
        ):
            release_evidence_commands.ios_release_plan_command(
                record_dir="custom-records",
            )

    def test_android_release_build_command_splits_version_build(self) -> None:
        self.assertEqual(
            build_android_release.ANDROID_RELEASE_ARTIFACTS,
            release_evidence_commands.VALID_ANDROID_RELEASE_ARTIFACTS,
        )
        self.assertEqual(
            "python3 tool/build_android_release.py --artifact apk --build-name 1.0.0 --build-number 42",
            release_evidence_commands.android_release_build_command("1.0.0+42"),
        )
        self.assertEqual(
            "python3 tool/build_android_release.py --artifact apk --build-name 1.0.0 --build-number 42 --dry-run",
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                dry_run=True,
            ),
        )
        self.assertEqual(
            "python3 tool/build_android_release.py --artifact appbundle --build-name 1.0.0 --build-number 42",
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                artifact="appbundle",
            ),
        )
        self.assertEqual(
            'python3 tool/build_android_release.py --artifact apk --build-name 1.0.0 --build-number 42 '
            '--record-dir custom-records --signing-identity "release alias SHA256:AA" '
            '--installer-channel "Firebase App Distribution internal track"',
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                record_dir="custom-records",
                signing_identity="release alias SHA256:AA",
                installer_channel="Firebase App Distribution internal track",
            ),
        )

    def test_android_release_build_command_rejects_malformed_version(self) -> None:
        with self.assertRaisesRegex(ValueError, r"<app-version>\+<build-number>"):
            release_evidence_commands.android_release_build_command("1.0.0")
        with self.assertRaisesRegex(ValueError, "both app version and build number"):
            release_evidence_commands.android_release_build_command("1.0.0+")
        with self.assertRaisesRegex(ValueError, "both app version and build number"):
            release_evidence_commands.android_release_build_command("+42")

    def test_android_release_build_command_rejects_unknown_artifact(self) -> None:
        with self.assertRaisesRegex(ValueError, "unsupported Android release artifact"):
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                artifact="aab",
            )

    def test_android_release_build_command_requires_complete_evidence_options(self) -> None:
        with self.assertRaisesRegex(ValueError, "requires record_dir"):
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                record_dir="custom-records",
            )

    def test_android_artifact_evidence_command_is_shared(self) -> None:
        self.assertEqual(
            'python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> '
            '--record-dir docs/qa-builds --version 1.0.0+42 '
            '--signing-identity "<alias or certificate fingerprint>" '
            '--installer-channel "<internal test channel>"',
            release_evidence_commands.android_artifact_evidence_command("1.0.0+42"),
        )
        self.assertEqual(
            'python3 tool/signed_artifact_evidence.py android app-release.aab '
            '--record-dir custom-records --version 1.0.0+42 '
            '--signing-identity "release alias upload key SHA256:AA" '
            '--installer-channel "Firebase App Distribution internal track"',
            release_evidence_commands.android_artifact_evidence_command(
                "1.0.0+42",
                artifact="app-release.aab",
                record_dir="custom-records",
                signing_identity="release alias upload key SHA256:AA",
                installer_channel="Firebase App Distribution internal track",
            ),
        )

    def test_android_artifact_evidence_command_rejects_weak_custom_values(self) -> None:
        with self.assertRaisesRegex(ValueError, "artifact path"):
            release_evidence_commands.android_artifact_evidence_command(
                "1.0.0+42",
                artifact="app-debug.apk",
            )
        with self.assertRaisesRegex(ValueError, "signing identity"):
            release_evidence_commands.android_artifact_evidence_command(
                "1.0.0+42",
                signing_identity="release-key",
            )
        with self.assertRaisesRegex(ValueError, "installer channel"):
            release_evidence_commands.android_artifact_evidence_command(
                "1.0.0+42",
                installer_channel="internal",
            )

    def test_ios_artifact_evidence_command_is_shared(self) -> None:
        self.assertEqual(
            'python3 tool/signed_artifact_evidence.py ios '
            '--archive-or-build "build/ios/archive/MaClawMobile.xcarchive" '
            '--team-id ABCDE12345 '
            '--provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" '
            "--record-dir docs/qa-builds",
            release_evidence_commands.ios_artifact_evidence_command(
                team_id="ABCDE12345",
            ),
        )
        self.assertEqual(
            'python3 tool/signed_artifact_evidence.py ios '
            '--archive-or-build "TestFlight build 42" '
            '--team-id ABCDE12345 '
            '--provisioning-profiles "Runner profile name MaClaw Runner; Share Extension profile name MaClaw Share Extension" '
            "--record-dir custom-records",
            release_evidence_commands.ios_artifact_evidence_command(
                archive_or_build="TestFlight build 42",
                team_id="ABCDE12345",
                provisioning_profiles="Runner profile name MaClaw Runner; Share Extension profile name MaClaw Share Extension",
                record_dir="custom-records",
            ),
        )

    def test_ios_artifact_evidence_command_rejects_weak_custom_values(self) -> None:
        with self.assertRaisesRegex(ValueError, "archive"):
            release_evidence_commands.ios_artifact_evidence_command(
                archive_or_build="TF-42",
                team_id="ABCDE12345",
            )
        with self.assertRaisesRegex(ValueError, "Team ID"):
            release_evidence_commands.ios_artifact_evidence_command(
                team_id="TEAM",
            )
        with self.assertRaisesRegex(ValueError, "provisioning profile"):
            release_evidence_commands.ios_artifact_evidence_command(
                team_id="ABCDE12345",
                provisioning_profiles="Runner profile; Share profile",
            )

    def test_release_docs_use_shared_android_artifact_evidence_command(self) -> None:
        mobile_root = Path(__file__).resolve().parents[1]
        command = release_evidence_commands.android_artifact_evidence_command()

        for relative_path in [
            "docs/qa_build_record_template.md",
            "docs/qa_device_checklist.md",
            "docs/release_checklist.md",
        ]:
            text = (mobile_root / relative_path).read_text(encoding="utf-8")
            self.assertIn(command, text, relative_path)

    def test_release_docs_use_shared_ios_artifact_evidence_command(self) -> None:
        mobile_root = Path(__file__).resolve().parents[1]
        command = release_evidence_commands.ios_artifact_evidence_command()

        for relative_path in [
            "docs/qa_build_record_template.md",
            "docs/qa_device_checklist.md",
            "docs/release_checklist.md",
            "docs/qa-builds/README.md",
        ]:
            text = (mobile_root / relative_path).read_text(encoding="utf-8")
            self.assertIn(command, text, relative_path)

    def test_release_docs_use_shared_final_evidence_commands(self) -> None:
        mobile_root = Path(__file__).resolve().parents[1]
        default_link_command = release_evidence_commands.qa_release_evidence_link_command()
        default_final_command = (
            release_evidence_commands.verify_final_release_evidence_command(
                version=release_evidence_commands.DEFAULT_VERSION,
            )
        )
        android_link_command = release_evidence_commands.qa_release_evidence_link_command(
            scope="android",
        )
        android_final_command = (
            release_evidence_commands.verify_final_release_evidence_command(
                scope="android",
                version=release_evidence_commands.DEFAULT_VERSION,
            )
        )

        for relative_path in [
            "docs/qa_device_checklist.md",
            "docs/release_checklist.md",
            "docs/release_evidence.md",
            "docs/qa-builds/README.md",
        ]:
            text = (mobile_root / relative_path).read_text(encoding="utf-8")
            self.assertIn(default_link_command, text, relative_path)
            self.assertIn(default_final_command, text, relative_path)
        for relative_path in [
            "docs/qa_device_checklist.md",
            "docs/release_checklist.md",
            "docs/qa-builds/README.md",
        ]:
            text = (mobile_root / relative_path).read_text(encoding="utf-8")
            self.assertIn(android_link_command, text, relative_path)
            self.assertIn(android_final_command, text, relative_path)

    def test_release_docs_use_shared_preflight_and_log_commands(self) -> None:
        mobile_root = Path(__file__).resolve().parents[1]
        cases = {
            "docs/qa_device_checklist.md": "<version+build>",
            "docs/qa-builds/README.md": "1.0.0+42",
        }

        for relative_path, version in cases.items():
            text = (mobile_root / relative_path).read_text(encoding="utf-8")
            for command in [
                release_evidence_commands.release_status_report_command(
                    export_method="development",
                ),
                release_evidence_commands.qa_preflight_command(
                    scope="android-ios",
                    team_id="<APPLE_TEAM_ID>",
                    export_method="development",
                ),
                release_evidence_commands.runtime_boundary_command(version),
                release_evidence_commands.release_gates_command(version),
            ]:
                self.assertIn(command, text, relative_path)
        scoped_text = (mobile_root / "docs/qa-builds/README.md").read_text(
            encoding="utf-8",
        )
        for command in [
            release_evidence_commands.release_status_report_command(scope="android"),
            release_evidence_commands.qa_preflight_command(scope="android"),
            release_evidence_commands.release_status_report_command(
                scope="ios",
                export_method="development",
            ),
            release_evidence_commands.qa_preflight_command(
                scope="ios",
                team_id="<APPLE_TEAM_ID>",
                export_method="development",
            ),
        ]:
            self.assertIn(command, scoped_text)

    def test_release_docs_use_shared_handoff_commands(self) -> None:
        mobile_root = Path(__file__).resolve().parents[1]
        cases = {
            "docs/qa_device_checklist.md": "<version+build>",
            "docs/qa-builds/README.md": "1.0.0+42",
        }

        for relative_path, version in cases.items():
            text = (mobile_root / relative_path).read_text(encoding="utf-8")
            for command in [
                release_evidence_commands.release_handoff_command(
                    version=version,
                    export_method="development",
                ),
                release_evidence_commands.release_handoff_command(
                    version=version,
                    scope="android",
                    export_method="development",
                ),
                release_evidence_commands.release_handoff_command(
                    version=version,
                    scope="ios",
                    export_method="development",
                ),
            ]:
                self.assertIn(command, text, relative_path)

    def test_release_docs_use_shared_record_prefill_fragments(self) -> None:
        mobile_root = Path(__file__).resolve().parents[1]
        cases = {
            "docs/qa_device_checklist.md": "<version+build>",
            "docs/qa-builds/README.md": "1.0.0+42",
        }

        for relative_path, version in cases.items():
            command = release_evidence_commands.create_record_command(version=version)
            text = (mobile_root / relative_path).read_text(encoding="utf-8")
            for fragment in [
                f"--scope android-ios --version {version}",
                f'--release-handoff-result "release_handoff.py output saved to docs/qa-builds/handoff-{version}.md"',
                f'--runtime-boundary-result "MaClaw Mobile runtime boundary verified. log: docs/qa-builds/runtime-boundary-{version}.log"',
                f'--automated-gates-result "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-{version}.log"',
            ]:
                self.assertIn(fragment, command)
                self.assertIn(fragment, text, relative_path)

    def test_qa_build_record_validate_and_report_commands_are_shared(self) -> None:
        record = "docs/qa-builds/2026-07-02-android-ios-1.0.0+42.md"

        self.assertEqual(
            "python3 tool/validate_qa_build_record.py "
            "docs/qa-builds/2026-07-02-android-ios-1.0.0+42.md",
            release_evidence_commands.validate_qa_build_record_command(record),
        )
        self.assertEqual(
            "python3 tool/qa_build_record_report.py "
            "docs/qa-builds/2026-07-02-android-ios-1.0.0+42.md",
            release_evidence_commands.qa_build_record_report_command(record),
        )
        self.assertEqual(
            "python3 tool/validate_qa_build_record.py "
            "docs/qa-builds/<YYYY-MM-DD>-android-ios-<version+build>.md",
            release_evidence_commands.validate_qa_build_record_command(),
        )
        self.assertEqual(
            "python3 tool/qa_build_record_report.py "
            "docs/qa-builds/<YYYY-MM-DD>-android-ios-<version+build>.md",
            release_evidence_commands.qa_build_record_report_command(),
        )

    def test_qa_directory_and_final_evidence_commands_are_shared(self) -> None:
        self.assertEqual(
            "python3 tool/validate_qa_build_records_dir.py docs/qa-builds",
            release_evidence_commands.validate_qa_build_records_dir_command(),
        )
        self.assertEqual(
            "python3 tool/validate_qa_build_records_dir.py docs/qa-builds --scope android",
            release_evidence_commands.validate_qa_build_records_dir_command(
                scope="android",
            ),
        )
        self.assertEqual(
            "python3 tool/verify_final_release_evidence.py docs/qa-builds --scope android-ios",
            release_evidence_commands.verify_final_release_evidence_command(),
        )
        self.assertEqual(
            "docs/qa-builds/final-release-evidence-1.0.0+42.log",
            release_evidence_commands.final_release_evidence_log_path("1.0.0+42"),
        )
        self.assertEqual(
            "docs/qa-builds/final-release-evidence-android-1.0.0+42.log",
            release_evidence_commands.final_release_evidence_log_path(
                "1.0.0+42",
                scope="android",
            ),
        )
        self.assertEqual(
            "python3 tool/verify_final_release_evidence.py docs/qa-builds "
            "--scope android-ios --log docs/qa-builds/final-release-evidence-1.0.0+42.log",
            release_evidence_commands.verify_final_release_evidence_command(
                version="1.0.0+42",
            ),
        )
        self.assertEqual(
            "python3 tool/verify_final_release_evidence.py docs/qa-builds "
            "--scope android-ios --log custom-final.log",
            release_evidence_commands.verify_final_release_evidence_command(
                log="custom-final.log",
            ),
        )
        self.assertEqual(
            "python3 tool/validate_qa_build_records_dir.py custom-records",
            release_evidence_commands.validate_qa_build_records_dir_command(
                "custom-records",
            ),
        )
        self.assertEqual(
            "python3 tool/validate_qa_build_records_dir.py custom-records --scope ios",
            release_evidence_commands.validate_qa_build_records_dir_command(
                "custom-records",
                scope="ios",
            ),
        )
        self.assertEqual(
            "python3 tool/verify_final_release_evidence.py custom-records --scope android-ios",
            release_evidence_commands.verify_final_release_evidence_command(
                "custom-records",
            ),
        )
        self.assertEqual(
            "python3 tool/verify_final_release_evidence.py custom-records "
            "--scope ios --log custom-records/final-release-evidence-ios-1.0.0+42.log",
            release_evidence_commands.verify_final_release_evidence_command(
                "custom-records",
                scope="ios",
                version="1.0.0+42",
            ),
        )

    def test_automated_release_gate_success_line_is_exact_runner_evidence(self) -> None:
        self.assertEqual(
            "All MaClaw Mobile automated release gates passed: 38 gates passed.",
            release_evidence_commands.AUTOMATED_RELEASE_GATE_SUCCESS_LINE,
        )
        self.assertIn(
            "MaClaw Mobile",
            release_evidence_commands.AUTOMATED_RELEASE_GATE_SUCCESS_LINE,
        )
        self.assertIn(
            f"{release_evidence_commands.AUTOMATED_RELEASE_GATE_COUNT} gates passed",
            release_evidence_commands.AUTOMATED_RELEASE_GATE_SUCCESS_LINE,
        )

    def test_qa_release_evidence_link_command_is_shared(self) -> None:
        self.assertEqual(
            "python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence",
            release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND,
        )
        self.assertEqual(
            release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND,
            release_evidence_commands.qa_release_evidence_link_command(),
        )
        self.assertEqual(
            "python3 tool/qa_release_evidence_links.py docs/qa-builds "
            "--update-release-evidence --scope android",
            release_evidence_commands.qa_release_evidence_link_command(
                scope="android",
            ),
        )
        self.assertEqual(
            "python3 tool/qa_release_evidence_links.py custom-records "
            "--update-release-evidence --scope ios",
            release_evidence_commands.qa_release_evidence_link_command(
                records_dir="custom-records",
                scope="ios",
            ),
        )
        self.assertIn(
            release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND,
            release_evidence_commands.qa_release_evidence_link_hint(),
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(scope="android"),
            release_evidence_commands.qa_release_evidence_link_hint(scope="android"),
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(
                records_dir="custom-records",
                scope="ios",
            ),
            release_evidence_commands.qa_release_evidence_link_hint(
                records_dir="custom-records",
                scope="ios",
            ),
        )
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                version="<version+build>",
            ),
            release_evidence_commands.qa_release_evidence_link_hint(),
        )
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                "custom-records",
                scope="ios",
                version="<version+build>",
                log="custom-records/final-release-evidence-ios-<version+build>.log",
            ),
            release_evidence_commands.qa_release_evidence_link_hint(
                records_dir="custom-records",
                scope="ios",
            ),
        )
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                scope="android",
                version="<version+build>",
            ),
            release_evidence_commands.qa_release_evidence_link_hint(scope="android"),
        )
        self.assertIn(
            "docs/release_evidence.md",
            release_evidence_commands.qa_release_evidence_link_hint(),
        )

    def test_qa_build_record_report_hint_points_to_record_gap_report(self) -> None:
        hint = release_evidence_commands.qa_build_record_report_hint(
            "docs/qa-builds/bad-record.md",
        )

        self.assertIn(
            "python3 tool/qa_build_record_report.py docs/qa-builds/bad-record.md",
            hint,
        )
        self.assertIn("missing or invalid signed-build QA evidence", hint)

    def test_qa_record_version_mismatch_hint_is_shared(self) -> None:
        hint = release_evidence_commands.qa_record_version_mismatch_hint()

        self.assertIn("one version/build", hint)
        self.assertIn("docs/qa-builds", hint)
        self.assertIn("--version <version+build>", hint)
        self.assertIn(
            "custom-records",
            release_evidence_commands.qa_record_version_mismatch_hint(
                records_dir="custom-records",
            ),
        )


if __name__ == "__main__":
    unittest.main()
