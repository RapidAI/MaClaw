from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import qa_preflight
import create_qa_build_record
import release_evidence_commands
import release_handoff
import release_status_report
import validate_qa_build_records_dir


def command_sequence(output: str) -> list[str]:
    start = output.index("```bash")
    end = output.index("```", start + len("```bash"))
    return [
        line.strip()
        for line in output[start + len("```bash") : end].splitlines()
        if line.strip()
    ]


class ReleaseHandoffTest(unittest.TestCase):
    def make_root(self) -> Path:
        root = Path(self.tmp.name)
        (root / "docs" / "qa-builds").mkdir(parents=True)
        return root

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def blocked_status(self, root: Path, **kwargs: object) -> release_status_report.ReleaseStatus:
        scope = str(kwargs.get("scope", release_evidence_commands.DEFAULT_SCOPE))
        checks: list[qa_preflight.PreflightCheck] = []
        if release_evidence_commands.scope_covers_android(scope):
            checks.append(
                qa_preflight.PreflightCheck(
                    "Android local signing inputs",
                    "blocker",
                    ["Missing Android signing file: android/key.properties"],
                ),
            )
        if release_evidence_commands.scope_covers_ios(scope):
            checks.append(
                qa_preflight.PreflightCheck(
                    "iOS export options",
                    "blocker",
                    ["Missing iOS export options plist: ios/ExportOptions.plist"],
                ),
            )
        return release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=checks,
            record_results=[],
            final_errors=["Final release evidence requires at least one completed signed-build QA record."],
            scope=scope,
        )

    def ready_status(self, root: Path, **kwargs: object) -> release_status_report.ReleaseStatus:
        scope = str(kwargs.get("scope", release_evidence_commands.DEFAULT_SCOPE))
        return release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[qa_preflight.PreflightCheck("Stub", "ok", ["ready"])],
            record_results=[
                validate_qa_build_records_dir.RecordValidationResult(
                    path=root / "docs" / "qa-builds" / "2026-07-02-android-ios-1.0.0+42.md",
                    errors=[],
                ),
            ],
            final_errors=[],
            scope=scope,
        )

    def status_with_invalid_record(self, root: Path, **_: object) -> release_status_report.ReleaseStatus:
        record = root / "docs" / "qa-builds" / "bad.md"
        return release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[],
            record_results=[
                validate_qa_build_records_dir.RecordValidationResult(
                    path=record,
                    errors=["Branch"],
                ),
            ],
            final_errors=["Final release evidence requires a validated Android signed-build QA record."],
        )

    def status_with_version_mismatch(self, root: Path, **_: object) -> release_status_report.ReleaseStatus:
        records_dir = root / "docs" / "qa-builds"
        android = records_dir / "2026-07-02-android-1.0.0+42.md"
        ios = records_dir / "2026-07-02-ios-1.0.0+43.md"
        return release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[],
            record_results=[
                validate_qa_build_records_dir.RecordValidationResult(
                    path=android,
                    errors=[],
                ),
                validate_qa_build_records_dir.RecordValidationResult(
                    path=ios,
                    errors=[],
                ),
            ],
            final_errors=[
                "Final release evidence records must use the same version/build: "
                "1.0.0+42, 1.0.0+43",
            ],
        )

    def status_with_missing_release_evidence_links(
        self,
        root: Path,
        **kwargs: object,
    ) -> release_status_report.ReleaseStatus:
        scope = str(kwargs.get("scope", release_evidence_commands.DEFAULT_SCOPE))
        record = root / "docs" / "qa-builds" / f"2026-07-02-{scope}-1.0.0+42.md"
        return release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[],
            record_results=[
                validate_qa_build_records_dir.RecordValidationResult(
                    path=record,
                    errors=[],
                ),
            ],
            final_errors=[
                "Release evidence document must include Markdown links for "
                f"every validated QA build record: 2026-07-02-{scope}-1.0.0+42.md",
            ],
            scope=scope,
        )

    def test_format_handoff_includes_blockers_inputs_commands_and_evidence(self) -> None:
        root = self.make_root()
        handoff = release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            team_id="ABCDE12345",
            build_status=self.blocked_status,
        )

        output = release_handoff.format_handoff(handoff)

        self.assertIn("Current status: NOT READY", output)
        self.assertIn("Android local signing inputs", output)
        self.assertIn("Missing iOS export options plist", output)
        self.assertIn(
            release_evidence_commands.release_status_report_command(
                team_id="ABCDE12345",
                export_method="development",
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.release_handoff_command(
                version="1.0.0+42",
                team_id="ABCDE12345",
                export_method="development",
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.runtime_boundary_command("1.0.0+42"),
            output,
        )
        self.assertIn(release_evidence_commands.setup_android_signing_command(), output)
        self.assertIn(
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                dry_run=True,
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
                signing_identity="<alias or certificate fingerprint>",
                installer_channel="<internal test channel>",
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                artifact="appbundle",
                record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
                signing_identity="<alias or certificate fingerprint>",
                installer_channel="<internal test channel>",
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.android_artifact_evidence_command("1.0.0+42"),
            output,
        )
        self.assertIn('--signing-identity "<alias or certificate fingerprint>"', output)
        self.assertIn('--installer-channel "<internal test channel>"', output)
        self.assertIn(
            release_evidence_commands.setup_ios_export_options_command(
                team_id="ABCDE12345",
                export_method="development",
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.ios_artifact_evidence_command(
                archive_or_build="build/ios/archive/MaClawMobile.xcarchive",
                team_id="ABCDE12345",
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.ios_release_plan_command(
                team_id="ABCDE12345",
                export_method="development",
                provisioning_profiles="<Runner profile UUID/name; Share Extension profile UUID/name>",
                record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
            ),
            output,
        )
        self.assertIn('--archive-or-build "build/ios/archive/MaClawMobile.xcarchive"', output)
        self.assertIn('--provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>"', output)
        self.assertIn(
            release_evidence_commands.release_gates_command("1.0.0+42"),
            output,
        )
        self.assertIn("python3 tool/create_qa_build_record.py --scope android-ios --version 1.0.0+42", output)
        self.assertIn(
            '--release-handoff-result "release_handoff.py output saved to '
            + release_evidence_commands.handoff_evidence_path("1.0.0+42")
            + '"',
            output,
        )
        self.assertIn('--runtime-boundary-result "MaClaw Mobile runtime boundary verified. log: docs/qa-builds/runtime-boundary-1.0.0+42.log"', output)
        self.assertIn('--automated-gates-result "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log"', output)
        self.assertIn(
            release_evidence_commands.qa_record_path_placeholder(
                scope="android-ios",
                version="1.0.0+42",
            ),
            output,
        )
        self.assertIn("Handoff output path or transcript", output)
        self.assertIn("full release-gate run result", output)
        self.assertIn("HubCenter discovery result", output)
        self.assertIn("Runtime boundary verifier result", output)
        self.assertIn("Digital employee list", output)
        self.assertIn("Do not store signing secrets", output)
        self.assertIn("Missing completed signed-build QA record", output)
        self.assertIn(
            "release handoff is only a QA plan, not a completed QA record",
            output,
        )
        self.assertIn(
            release_evidence_commands.signed_qa_record_hint(
                version="1.0.0+42",
                team_id="ABCDE12345",
                export_method="development",
            ),
            output,
        )
        self.assertIn("python3 tool/validate_qa_build_record.py", output)
        self.assertIn("without secret redaction failures", output)
        self.assertIn("python3 tool/qa_build_record_report.py", output)

    def test_format_handoff_points_invalid_records_to_gap_report(self) -> None:
        root = self.make_root()
        handoff = release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            team_id="ABCDE12345",
            build_status=self.status_with_invalid_record,
        )

        output = release_handoff.format_handoff(handoff)
        bad_record = root / "docs" / "qa-builds" / "bad.md"

        self.assertIn("Invalid QA records:", output)
        self.assertIn("bad.md", output)
        self.assertIn("Branch", output)
        self.assertIn(
            release_evidence_commands.qa_build_record_report_hint(str(bad_record)),
            output,
        )

    def test_format_handoff_points_version_mismatch_to_single_version_action(self) -> None:
        root = self.make_root()
        handoff = release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            team_id="ABCDE12345",
            build_status=self.status_with_version_mismatch,
        )

        output = release_handoff.format_handoff(handoff)

        self.assertIn("Final evidence blockers:", output)
        self.assertIn(
            "Final release evidence records must use the same version/build",
            output,
        )
        self.assertIn("Final evidence next actions:", output)
        self.assertIn(
            release_evidence_commands.qa_record_version_mismatch_hint(),
            output,
        )
        self.assertNotIn(
            "no completed signed-build QA records yet",
            output,
        )

        scoped_handoff = release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            scope="android",
            team_id="ABCDE12345",
            build_status=self.status_with_missing_release_evidence_links,
        )
        scoped_output = release_handoff.format_handoff(scoped_handoff)
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_hint(scope="android"),
            scoped_output,
        )
        self.assertIn(
            "docs/qa-builds/final-release-evidence-android-<version+build>.log",
            scoped_output,
        )
        self.assertNotIn(
            "docs/qa-builds/final-release-evidence-<version+build>.log",
            scoped_output,
        )

    def test_format_handoff_points_missing_release_links_to_update_action(self) -> None:
        root = self.make_root()
        handoff = release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            team_id="ABCDE12345",
            build_status=self.status_with_missing_release_evidence_links,
        )

        output = release_handoff.format_handoff(handoff)

        self.assertIn("Final evidence blockers:", output)
        self.assertIn(
            "Release evidence document must include Markdown links",
            output,
        )
        self.assertIn("Final evidence next actions:", output)
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_hint(),
            output,
        )
        self.assertNotIn(
            "no completed signed-build QA records yet",
            output,
        )

    def test_format_handoff_command_sequence_is_copyable_in_order(self) -> None:
        root = self.make_root()
        handoff = release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            team_id="ABCDE12345",
            export_method="ad-hoc",
            build_status=self.blocked_status,
        )

        self.assertEqual(
            [
                release_evidence_commands.release_status_report_command(
                    team_id="ABCDE12345",
                    export_method="ad-hoc",
                ),
                release_evidence_commands.release_handoff_command(
                    version="1.0.0+42",
                    team_id="ABCDE12345",
                    export_method="ad-hoc",
                ),
                release_evidence_commands.setup_android_signing_command(),
                release_evidence_commands.setup_ios_export_options_command(
                    team_id="ABCDE12345",
                    export_method="ad-hoc",
                ),
                release_evidence_commands.qa_preflight_command(
                    team_id="ABCDE12345",
                    export_method="ad-hoc",
                ),
                release_evidence_commands.runtime_boundary_command("1.0.0+42"),
                release_evidence_commands.android_release_build_command(
                    "1.0.0+42",
                    dry_run=True,
                ),
                release_evidence_commands.android_release_build_command(
                    "1.0.0+42",
                    record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
                    signing_identity="<alias or certificate fingerprint>",
                    installer_channel="<internal test channel>",
                ),
                release_evidence_commands.android_release_build_command(
                    "1.0.0+42",
                    artifact="appbundle",
                    record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
                    signing_identity="<alias or certificate fingerprint>",
                    installer_channel="<internal test channel>",
                ),
                release_evidence_commands.android_artifact_evidence_command(
                    "1.0.0+42",
                ),
                release_evidence_commands.ios_release_plan_command(
                    team_id="ABCDE12345",
                    export_method="ad-hoc",
                ),
                release_evidence_commands.ios_release_plan_command(
                    team_id="ABCDE12345",
                    export_method="ad-hoc",
                    provisioning_profiles="<Runner profile UUID/name; Share Extension profile UUID/name>",
                    record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
                ),
                release_evidence_commands.ios_artifact_evidence_command(
                    archive_or_build="build/ios/archive/MaClawMobile.xcarchive",
                    team_id="ABCDE12345",
                ),
                release_evidence_commands.release_gates_command("1.0.0+42"),
                release_evidence_commands.create_record_command(
                    scope="android-ios",
                    version="1.0.0+42",
                ),
                release_evidence_commands.validate_qa_build_record_command(
                    release_evidence_commands.qa_record_path_placeholder(
                        scope="android-ios",
                        version="1.0.0+42",
                    ),
                ),
                release_evidence_commands.qa_build_record_report_command(
                    release_evidence_commands.qa_record_path_placeholder(
                        scope="android-ios",
                        version="1.0.0+42",
                    ),
                ),
                release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND,
                release_evidence_commands.validate_qa_build_records_dir_command(),
                release_evidence_commands.verify_final_release_evidence_command(
                    version="1.0.0+42",
                ),
            ],
            command_sequence(release_handoff.format_handoff(handoff)),
        )
        self.assertIn(
            release_evidence_commands.create_record_command(
                scope="android-ios",
                version="1.0.0+42",
            ),
            command_sequence(release_handoff.format_handoff(handoff)),
        )

    def test_format_handoff_threads_custom_records_dir_through_commands(self) -> None:
        root = self.make_root()
        handoff = release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            scope="android",
            records_dir="tmp/qa-builds",
            build_status=self.blocked_status,
        )

        output = release_handoff.format_handoff(handoff)
        commands = command_sequence(output)

        self.assertIn(
            "Missing completed signed-build QA record under tmp/qa-builds.",
            output,
        )
        self.assertIn(
            "tmp/qa-builds/<YYYY-MM-DD>-android-1.0.0+42.md",
            output,
        )
        self.assertIn(
            release_evidence_commands.signed_qa_record_hint(
                scope="android",
                version="1.0.0+42",
                records_dir="tmp/qa-builds",
            ),
            output,
        )
        self.assertNotIn(
            release_evidence_commands.signed_qa_record_hint(
                scope="android",
                version="1.0.0+42",
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.release_status_report_command(
                scope="android",
                records_dir="tmp/qa-builds",
            ),
            commands,
        )
        self.assertIn(
            release_evidence_commands.release_handoff_command(
                version="1.0.0+42",
                scope="android",
                output="tmp/qa-builds/handoff-android-1.0.0+42.md",
                records_dir="tmp/qa-builds",
            ),
            commands,
        )
        self.assertIn(
            release_evidence_commands.runtime_boundary_command(
                "1.0.0+42",
                records_dir="tmp/qa-builds",
            ),
            commands,
        )
        self.assertIn(
            release_evidence_commands.qa_preflight_command(
                scope="android",
                records_dir="tmp/qa-builds",
            ),
            commands,
        )
        self.assertIn(
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                record_dir="tmp/qa-builds",
                signing_identity="<alias or certificate fingerprint>",
                installer_channel="<internal test channel>",
            ),
            commands,
        )
        self.assertIn(
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                artifact="appbundle",
                record_dir="tmp/qa-builds",
                signing_identity="<alias or certificate fingerprint>",
                installer_channel="<internal test channel>",
            ),
            commands,
        )
        self.assertIn(
            release_evidence_commands.create_record_command(
                scope="android",
                version="1.0.0+42",
                records_dir="tmp/qa-builds",
            ),
            commands,
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(
                scope="android",
                records_dir="tmp/qa-builds",
            ),
            commands,
        )
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                "tmp/qa-builds",
                scope="android",
                version="1.0.0+42",
                log="tmp/qa-builds/final-release-evidence-android-1.0.0+42.log",
            ),
            commands,
        )

    def test_format_handoff_final_evidence_hints_use_custom_records_dir(self) -> None:
        root = self.make_root()
        records_dir = "tmp/qa-builds"

        def status_with_custom_final_error(
            root: Path,
            **kwargs: object,
        ) -> release_status_report.ReleaseStatus:
            scope = str(kwargs.get("scope", release_evidence_commands.DEFAULT_SCOPE))
            return release_status_report.ReleaseStatus(
                root=root,
                preflight_checks=[],
                record_results=[
                    validate_qa_build_records_dir.RecordValidationResult(
                        path=root / records_dir / f"2026-07-02-{scope}-1.0.0+42.md",
                        errors=[],
                    ),
                ],
                final_errors=[
                    "Release evidence document must include Markdown links for every validated QA build record: "
                    f"2026-07-02-{scope}-1.0.0+42.md",
                ],
                scope=scope,
                records_dir=root / records_dir,
            )

        handoff = release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            scope="android",
            records_dir=records_dir,
            build_status=status_with_custom_final_error,
        )

        output = release_handoff.format_handoff(handoff)

        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(
                scope="android",
                records_dir=records_dir,
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                records_dir,
                scope="android",
                version=release_evidence_commands.DEFAULT_VERSION,
                log=release_evidence_commands.final_release_evidence_log_path(
                    release_evidence_commands.DEFAULT_VERSION,
                    scope="android",
                    records_dir=records_dir,
                ),
            ),
            output,
        )

    def test_format_handoff_scopes_platform_commands_and_evidence(self) -> None:
        root = self.make_root()
        android_handoff = release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            scope="android",
            team_id="ABCDE12345",
            export_method="ad-hoc",
            build_status=self.blocked_status,
        )
        ios_handoff = release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            scope="ios",
            team_id="ABCDE12345",
            export_method="ad-hoc",
            build_status=self.blocked_status,
        )

        android_output = release_handoff.format_handoff(android_handoff)
        ios_output = release_handoff.format_handoff(ios_handoff)

        self.assertIn(
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
                signing_identity="<alias or certificate fingerprint>",
                installer_channel="<internal test channel>",
            ),
            android_output,
        )
        self.assertIn(
            release_evidence_commands.android_artifact_evidence_command("1.0.0+42"),
            android_output,
        )
        self.assertIn(
            release_evidence_commands.release_status_report_command(
                scope="android",
            ),
            android_output,
        )
        self.assertIn(
            release_evidence_commands.release_handoff_command(
                version="1.0.0+42",
                scope="android",
            ),
            android_output,
        )
        self.assertNotIn("--scope android --team-id", android_output)
        self.assertIn(
            release_evidence_commands.qa_preflight_command(scope="android"),
            android_output,
        )
        self.assertIn(
            release_evidence_commands.validate_qa_build_records_dir_command(
                scope="android",
            ),
            android_output,
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(
                scope="android",
            ),
            android_output,
        )
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                scope="android",
                version="1.0.0+42",
            ),
            android_output,
        )
        self.assertIn(
            "docs/qa-builds/final-release-evidence-android-1.0.0+42.log",
            android_output,
        )
        self.assertIn("Signed Android artifact path", android_output)
        self.assertIn("Android release keystore path", android_output)
        self.assertIn("Signed Android test devices", android_output)
        self.assertNotIn("Apple Team ID, Runner provisioning profile", android_output)
        self.assertNotIn("Signed Android/iOS test devices", android_output)
        self.assertIn("Android local signing inputs", android_output)
        self.assertNotIn("iOS export options", android_output)
        self.assertNotIn(
            release_evidence_commands.ios_release_plan_command(
                team_id="ABCDE12345",
                export_method="ad-hoc",
                provisioning_profiles="<Runner profile UUID/name; Share Extension profile UUID/name>",
                record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
            ),
            android_output,
        )
        self.assertNotIn(
            release_evidence_commands.ios_artifact_evidence_command(
                archive_or_build="build/ios/archive/MaClawMobile.xcarchive",
                team_id="ABCDE12345",
            ),
            android_output,
        )
        self.assertNotIn("iOS archive/export path", android_output)

        self.assertIn(
            release_evidence_commands.ios_release_plan_command(
                team_id="ABCDE12345",
                export_method="ad-hoc",
                provisioning_profiles="<Runner profile UUID/name; Share Extension profile UUID/name>",
                record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
            ),
            ios_output,
        )
        self.assertIn(
            release_evidence_commands.ios_artifact_evidence_command(
                archive_or_build="build/ios/archive/MaClawMobile.xcarchive",
                team_id="ABCDE12345",
            ),
            ios_output,
        )
        self.assertIn(
            release_evidence_commands.release_status_report_command(
                scope="ios",
                team_id="ABCDE12345",
                export_method="ad-hoc",
            ),
            ios_output,
        )
        self.assertIn(
            release_evidence_commands.release_handoff_command(
                version="1.0.0+42",
                scope="ios",
                team_id="ABCDE12345",
                export_method="ad-hoc",
            ),
            ios_output,
        )
        self.assertIn(
            release_evidence_commands.qa_preflight_command(
                scope="ios",
                team_id="ABCDE12345",
                export_method="ad-hoc",
            ),
            ios_output,
        )
        self.assertIn(
            release_evidence_commands.validate_qa_build_records_dir_command(
                scope="ios",
            ),
            ios_output,
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(
                scope="ios",
            ),
            ios_output,
        )
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                scope="ios",
                version="1.0.0+42",
            ),
            ios_output,
        )
        self.assertIn(
            "docs/qa-builds/final-release-evidence-ios-1.0.0+42.log",
            ios_output,
        )
        self.assertIn("iOS archive/export path", ios_output)
        self.assertIn("Apple Team ID, Runner provisioning profile", ios_output)
        self.assertIn("Signed iOS test devices", ios_output)
        self.assertNotIn("Android release keystore path", ios_output)
        self.assertNotIn("Signed Android/iOS test devices", ios_output)
        self.assertIn("iOS export options", ios_output)
        self.assertNotIn("Android local signing inputs", ios_output)
        self.assertNotIn(
            release_evidence_commands.android_release_build_command(
                "1.0.0+42",
                record_dir=release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
                signing_identity="<alias or certificate fingerprint>",
                installer_channel="<internal test channel>",
            ),
            ios_output,
        )
        self.assertNotIn(
            release_evidence_commands.android_artifact_evidence_command("1.0.0+42"),
            ios_output,
        )
        self.assertNotIn("Signed Android artifact path", ios_output)

    def test_final_decision_prefills_match_create_record_validator(self) -> None:
        root = self.make_root()
        handoff = release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            team_id="ABCDE12345",
            build_status=self.blocked_status,
        )

        self.assertEqual(
            [],
            create_qa_build_record.final_decision_prefill_errors(
                release_handoff.final_decision_prefills(handoff),
            ),
        )

    def test_format_handoff_reports_ready_status(self) -> None:
        root = self.make_root()
        handoff = release_handoff.build_handoff(root, build_status=self.ready_status)

        output = release_handoff.format_handoff(handoff)

        self.assertIn("Current status: READY for final release approval.", output)
        self.assertNotIn("Current status: NOT READY", output)

    def test_build_handoff_passes_ios_expected_values_to_status_builder(self) -> None:
        root = self.make_root()
        seen: dict[str, object] = {}

        def fake_status(*args: object, **kwargs: object) -> release_status_report.ReleaseStatus:
            seen["args"] = args
            seen["kwargs"] = kwargs
            return self.ready_status(root)

        release_handoff.build_handoff(
            root,
            version="1.0.0+42",
            team_id="ABCDE12345",
            export_method="ad-hoc",
            build_status=fake_status,
        )

        self.assertEqual((root.resolve(),), seen["args"])
        self.assertEqual("ABCDE12345", seen["kwargs"]["ios_team_id"])
        self.assertEqual("ad-hoc", seen["kwargs"]["ios_export_method"])

    def test_build_handoff_rejects_unknown_scope_before_status_builder(self) -> None:
        root = self.make_root()
        called = False

        def fake_status(*_args: object, **_kwargs: object) -> release_status_report.ReleaseStatus:
            nonlocal called
            called = True
            return self.ready_status(root)

        with self.assertRaisesRegex(ValueError, "unsupported scope"):
            release_handoff.build_handoff(
                root,
                scope="ios-android",
                build_status=fake_status,
            )

        self.assertFalse(called)

    def test_main_writes_output_file_and_returns_blocked_status(self) -> None:
        root = self.make_root()
        target = root / "handoff.md"
        stdout = StringIO()
        original_build_status = release_handoff.release_status_report.build_status
        try:
            release_handoff.release_status_report.build_status = self.blocked_status
            with redirect_stdout(stdout):
                exit_code = release_handoff.main(
                    [
                        "--root",
                        str(root),
                        "--version",
                        "1.0.0+42",
                        "--team-id",
                        "ABCDE12345",
                        "--output",
                        str(target),
                    ],
                )
        finally:
            release_handoff.release_status_report.build_status = original_build_status

        self.assertEqual(1, exit_code)
        self.assertTrue(target.exists())
        self.assertIn("Wrote MaClaw Mobile release handoff", stdout.getvalue())
        self.assertIn("1.0.0+42", target.read_text(encoding="utf-8"))

    def test_main_allows_handoff_named_output_inside_records_dir(self) -> None:
        root = self.make_root()
        target = root / "docs" / "qa-builds" / "handoff-1.0.0+42.md"
        stdout = StringIO()
        original_build_status = release_handoff.release_status_report.build_status
        try:
            release_handoff.release_status_report.build_status = self.blocked_status
            with redirect_stdout(stdout):
                exit_code = release_handoff.main(
                    [
                        "--root",
                        str(root),
                        "--version",
                        "1.0.0+42",
                        "--team-id",
                        "ABCDE12345",
                        "--output",
                        str(target),
                    ],
                )
        finally:
            release_handoff.release_status_report.build_status = original_build_status

        self.assertEqual(1, exit_code)
        self.assertTrue(target.exists())
        self.assertIn("Wrote MaClaw Mobile release handoff", stdout.getvalue())

    def test_main_rejects_non_handoff_output_inside_records_dir(self) -> None:
        root = self.make_root()
        target = root / "docs" / "qa-builds" / "tmp-handoff-check.md"
        stderr = StringIO()
        original_build_status = release_handoff.release_status_report.build_status
        try:
            release_handoff.release_status_report.build_status = self.blocked_status
            with redirect_stderr(stderr):
                exit_code = release_handoff.main(
                    [
                        "--root",
                        str(root),
                        "--version",
                        "1.0.0+42",
                        "--team-id",
                        "ABCDE12345",
                        "--output",
                        str(target),
                    ],
                )
        finally:
            release_handoff.release_status_report.build_status = original_build_status

        self.assertEqual(1, exit_code)
        self.assertFalse(target.exists())
        self.assertIn("Release handoff output path is invalid", stderr.getvalue())
        self.assertIn("handoff-*.md", stderr.getvalue())

    def test_main_refuses_to_overwrite_existing_output_without_force(self) -> None:
        root = self.make_root()
        target = root / "handoff.md"
        target.write_text("existing handoff evidence", encoding="utf-8")
        stderr = StringIO()
        original_build_status = release_handoff.release_status_report.build_status
        try:
            release_handoff.release_status_report.build_status = self.ready_status
            with redirect_stderr(stderr):
                exit_code = release_handoff.main(
                    [
                        "--root",
                        str(root),
                        "--version",
                        "1.0.0+42",
                        "--team-id",
                        "ABCDE12345",
                        "--output",
                        str(target),
                    ],
                )
        finally:
            release_handoff.release_status_report.build_status = original_build_status

        self.assertEqual(1, exit_code)
        self.assertEqual("existing handoff evidence", target.read_text(encoding="utf-8"))
        self.assertIn("pass --force to overwrite", stderr.getvalue())

    def test_main_force_overwrites_existing_output(self) -> None:
        root = self.make_root()
        target = root / "handoff.md"
        target.write_text("existing handoff evidence", encoding="utf-8")
        stdout = StringIO()
        original_build_status = release_handoff.release_status_report.build_status
        try:
            release_handoff.release_status_report.build_status = self.ready_status
            with redirect_stdout(stdout):
                exit_code = release_handoff.main(
                    [
                        "--root",
                        str(root),
                        "--version",
                        "1.0.0+42",
                        "--team-id",
                        "ABCDE12345",
                        "--output",
                        str(target),
                        "--force",
                    ],
                )
        finally:
            release_handoff.release_status_report.build_status = original_build_status

        self.assertEqual(0, exit_code)
        self.assertIn("MaClaw Mobile Release Handoff", target.read_text(encoding="utf-8"))
        self.assertIn("Wrote MaClaw Mobile release handoff", stdout.getvalue())

    def test_main_prints_ready_handoff_to_stdout(self) -> None:
        root = self.make_root()
        stdout = StringIO()
        stderr = StringIO()
        original_build_status = release_handoff.release_status_report.build_status
        try:
            release_handoff.release_status_report.build_status = self.ready_status
            with redirect_stdout(stdout), redirect_stderr(stderr):
                exit_code = release_handoff.main(
                    [
                        "--root",
                        str(root),
                        "--version",
                        "1.0.0+42",
                        "--team-id",
                        "abcde12345",
                    ],
                )
        finally:
            release_handoff.release_status_report.build_status = original_build_status

        self.assertEqual(0, exit_code)
        self.assertIn("MaClaw Mobile Release Handoff", stdout.getvalue())
        self.assertIn("--team-id ABCDE12345", stdout.getvalue())
        self.assertEqual("", stderr.getvalue())

    def test_main_allows_android_handoff_without_team_id(self) -> None:
        root = self.make_root()
        target = root / "android-handoff.md"
        stdout = StringIO()
        original_build_status = release_handoff.release_status_report.build_status
        try:
            release_handoff.release_status_report.build_status = self.blocked_status
            with redirect_stdout(stdout):
                exit_code = release_handoff.main(
                    [
                        "--root",
                        str(root),
                        "--version",
                        "1.0.0+42",
                        "--scope",
                        "android",
                        "--output",
                        str(target),
                    ],
                )
        finally:
            release_handoff.release_status_report.build_status = original_build_status

        self.assertEqual(1, exit_code)
        text = target.read_text(encoding="utf-8")
        self.assertIn("Scope: `android`", text)
        self.assertNotIn("--team-id", text)
        self.assertIn("Wrote MaClaw Mobile release handoff", stdout.getvalue())

    def test_main_requires_real_version_and_ios_team_id(self) -> None:
        root = self.make_root()
        stderr = StringIO()

        with redirect_stderr(stderr), self.assertRaises(SystemExit):
            release_handoff.main(["--root", str(root), "--team-id", "ABCDE12345"])

        with redirect_stderr(stderr), self.assertRaises(SystemExit):
            release_handoff.main(["--root", str(root), "--version", "1.0.0+42"])

        with redirect_stderr(stderr), self.assertRaises(SystemExit):
            release_handoff.main(
                [
                    "--root",
                    str(root),
                    "--version",
                    "1.0.0+42",
                    "--scope",
                    "ios",
                ],
            )

        with redirect_stderr(stderr), self.assertRaises(SystemExit):
            release_handoff.main(
                [
                    "--root",
                    str(root),
                    "--version",
                    "<version+build>",
                    "--team-id",
                    "ABCDE12345",
                ],
            )

        with redirect_stderr(stderr), self.assertRaises(SystemExit):
            release_handoff.main(
                [
                    "--root",
                    str(root),
                    "--version",
                    "1.0.0+42",
                    "--team-id",
                    "<APPLE_TEAM_ID>",
                ],
            )
        with redirect_stderr(stderr), self.assertRaises(SystemExit):
            release_handoff.main(
                [
                    "--root",
                    str(root),
                    "--version",
                    "1.0.0+42",
                    "--team-id",
                    "ABCDE12345",
                    "--export-method",
                    "beta-channel",
                ],
            )
        with redirect_stderr(stderr), self.assertRaises(SystemExit):
            release_handoff.main(
                [
                    "--root",
                    str(root),
                    "--version",
                    "1.0.0+42",
                    "--team-id",
                    "ABCDE12345",
                    "--scope",
                    "ios-android",
                ],
            )
        self.assertIn("version must use app-version+build", stderr.getvalue())
        self.assertIn("--team-id is required for iOS release handoff scopes", stderr.getvalue())
        self.assertIn("team id must be a 10-character Apple team identifier", stderr.getvalue())
        self.assertIn("invalid choice: 'beta-channel'", stderr.getvalue())
        self.assertIn("invalid choice: 'ios-android'", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
