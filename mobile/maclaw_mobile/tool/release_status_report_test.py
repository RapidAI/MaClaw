from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import qa_preflight
import release_evidence_commands
import release_status_report
import validate_qa_build_records_dir


class ReleaseStatusReportTest(unittest.TestCase):
    def make_root(self) -> Path:
        root = Path(self.tmp.name)
        (root / "docs" / "qa-builds").mkdir(parents=True)
        return root

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def test_not_ready_report_groups_preflight_records_and_final_errors(self) -> None:
        root = self.make_root()
        bad_record = validate_qa_build_records_dir.RecordValidationResult(
            path=root / "docs" / "qa-builds" / "bad.md",
            errors=["Branch"],
        )

        status = release_status_report.build_status(
            root,
            preflight=lambda *_args, **_kwargs: [
                qa_preflight.PreflightCheck("Android local signing inputs", "blocker", ["missing key.properties"]),
            ],
            validate_records=lambda _: [bad_record],
            verify_final=lambda _path, **_kwargs: [
                "Final release evidence requires a validated Android signed-build QA record.",
            ],
        )
        output = release_status_report.format_status(status)

        self.assertFalse(status.ready)
        self.assertIn("[BLOCKER] Android local signing inputs", output)
        self.assertIn(
            "QA build records: 0 in-scope valid, 0 out-of-scope valid, 1 invalid",
            output,
        )
        self.assertIn("Branch", output)
        self.assertIn("Result: NOT READY.", output)
        self.assertIn("Fix invalid signed-build QA records", output)
        self.assertIn(
            release_evidence_commands.qa_build_record_report_hint(str(bad_record.path)),
            output,
        )
        self.assertNotIn(
            "Create and validate in-scope signed-build QA records under docs/qa-builds/.",
            output,
        )
        self.assertNotIn(
            release_evidence_commands.release_handoff_command(),
            output,
        )

    def test_not_ready_without_records_points_to_create_record_flow(self) -> None:
        root = self.make_root()

        status = release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[qa_preflight.PreflightCheck("Stub", "ok", ["ready"])],
            record_results=[],
            final_errors=[
                "Final release evidence requires at least one completed signed-build QA record.",
            ],
        )
        output = release_status_report.format_status(status)

        self.assertIn(release_evidence_commands.release_handoff_command(), output)
        self.assertIn(
            "release handoff is only a QA plan, not a completed QA record",
            output,
        )
        self.assertIn(
            "Create and validate in-scope signed-build QA records under docs/qa-builds/.",
            output,
        )
        self.assertIn("--team-id <APPLE_TEAM_ID>", output)
        self.assertIn("--export-method <export-method>", output)
        self.assertIn("--output docs/qa-builds/handoff-<version+build>.md", output)
        self.assertIn("verify_runtime_boundary.py --log", output)
        self.assertIn("run_release_gates.py --log", output)
        self.assertIn("tool/create_qa_build_record.py", output)
        self.assertIn(
            '--release-handoff-result "release_handoff.py output saved to docs/qa-builds/handoff-<version+build>.md"',
            output,
        )
        self.assertIn(
            '--runtime-boundary-result "MaClaw Mobile runtime boundary verified. log: docs/qa-builds/runtime-boundary-<version+build>.log"',
            output,
        )
        self.assertIn(
            '--automated-gates-result "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-<version+build>.log"',
            output,
        )
        self.assertIn(
            release_evidence_commands.android_artifact_evidence_command(),
            output,
        )
        self.assertIn(
            release_evidence_commands.ios_artifact_evidence_command(
                archive_or_build="build/ios/archive/MaClawMobile.xcarchive",
            ),
            output,
        )

    def test_build_status_uses_custom_records_dir(self) -> None:
        root = self.make_root()
        records_dir = root / "tmp-qa"
        seen: dict[str, Path] = {}

        def validate_records(path: Path) -> list[validate_qa_build_records_dir.RecordValidationResult]:
            seen["validate"] = path
            return []

        def verify_final(path: Path, **_kwargs: object) -> list[str]:
            seen["verify"] = path
            return [
                "Final release evidence requires at least one completed signed-build QA record.",
            ]

        def preflight(*_args: object, **kwargs: object) -> list[qa_preflight.PreflightCheck]:
            seen["preflight"] = kwargs["records_dir"]  # type: ignore[assignment]
            return []

        status = release_status_report.build_status(
            root,
            records_dir=records_dir,
            preflight=preflight,
            validate_records=validate_records,
            verify_final=verify_final,
        )
        output = release_status_report.format_status(status)

        self.assertEqual(records_dir.resolve(), seen["preflight"])
        self.assertEqual(records_dir.resolve(), seen["validate"])
        self.assertEqual(records_dir.resolve(), seen["verify"])
        self.assertEqual(records_dir.resolve(), status.records_dir)
        self.assertIn(
            f"Create and validate in-scope signed-build QA records under {records_dir.resolve()}/.",
            output,
        )
        self.assertIn(
            release_evidence_commands.create_record_command(
                records_dir=str(records_dir.resolve()),
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.qa_preflight_command(
                team_id=release_evidence_commands.DEFAULT_TEAM_ID,
                export_method=release_evidence_commands.DEFAULT_EXPORT_METHOD,
                records_dir=str(records_dir.resolve()),
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(
                records_dir=str(records_dir.resolve()),
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                str(records_dir.resolve()),
                version=release_evidence_commands.DEFAULT_VERSION,
                log=release_evidence_commands.final_release_evidence_log_path(
                    release_evidence_commands.DEFAULT_VERSION,
                    records_dir=str(records_dir.resolve()),
                ),
            ),
            output,
        )

    def test_not_ready_without_records_preserves_supplied_ios_values(self) -> None:
        root = self.make_root()

        status = release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[qa_preflight.PreflightCheck("Stub", "ok", ["ready"])],
            record_results=[],
            final_errors=[
                "Final release evidence requires at least one completed signed-build QA record.",
            ],
            ios_team_id="ABCDE12345",
            ios_export_method="ad-hoc",
        )
        output = release_status_report.format_status(status)

        self.assertIn("--team-id ABCDE12345", output)
        self.assertIn("--export-method ad-hoc", output)
        self.assertNotIn("--team-id <APPLE_TEAM_ID>", output)
        self.assertNotIn("--export-method <export-method>", output)

    def test_preflight_blocker_next_action_uses_readable_scope_label(self) -> None:
        root = self.make_root()

        for scope, label in [
            ("android", "Android"),
            ("ios", "iOS"),
            ("android-ios", "Android/iOS"),
        ]:
            with self.subTest(scope=scope):
                status = release_status_report.ReleaseStatus(
                    root=root,
                    preflight_checks=[
                        qa_preflight.PreflightCheck("Stub", "blocker", ["blocked"]),
                    ],
                    record_results=[],
                    final_errors=[],
                    scope=scope,
                )
                output = release_status_report.format_status(status)

                self.assertIn(
                    f"Clear preflight blockers, then build signed {label} QA packages.",
                    output,
                )
                self.assertNotIn(f"signed {scope} QA packages", output)

    def test_preflight_blocker_defers_record_creation_flow_until_preflight_passes(self) -> None:
        root = self.make_root()
        status = release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[
                qa_preflight.PreflightCheck(
                    "Android local signing inputs",
                    "blocker",
                    ["Missing Android signing file: android/key.properties"],
                ),
            ],
            record_results=[],
            final_errors=[
                "Final release evidence requires at least one completed signed-build QA record.",
            ],
        )

        output = release_status_report.format_status(status)

        self.assertIn("- [Preflight] Android local signing inputs", output)
        self.assertIn("Missing Android signing file: android/key.properties", output)
        self.assertIn(
            "Re-run `"
            + release_evidence_commands.qa_preflight_command(
                team_id=release_evidence_commands.DEFAULT_TEAM_ID,
                export_method=release_evidence_commands.DEFAULT_EXPORT_METHOD,
            )
            + "`",
            output,
        )
        self.assertNotIn(
            "Create and validate in-scope signed-build QA records under docs/qa-builds/.",
            output,
        )
        self.assertNotIn(release_evidence_commands.release_handoff_command(), output)

    def test_not_ready_with_valid_records_points_to_final_evidence_blockers(self) -> None:
        root = self.make_root()
        valid_record = validate_qa_build_records_dir.RecordValidationResult(
            path=root / "docs" / "qa-builds" / "2026-07-02-android-1.0.0+42.md",
            errors=[],
        )
        status = release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[qa_preflight.PreflightCheck("Stub", "ok", ["ready"])],
            record_results=[valid_record],
            final_errors=[
                "Final release evidence requires a validated iOS signed-build QA record.",
            ],
        )

        output = release_status_report.format_status(status)

        self.assertFalse(status.ready)
        self.assertIn(
            "Resolve final evidence blockers above:",
            output,
        )
        self.assertIn(
            release_evidence_commands.signed_qa_record_hint(),
            output,
        )
        self.assertNotIn(
            "Link validated QA records in docs/release_evidence.md and rerun final verification.",
            output,
        )

    def test_final_evidence_next_actions_use_custom_records_dir(self) -> None:
        root = self.make_root()
        records_dir = root / "custom-records"
        valid_record = validate_qa_build_records_dir.RecordValidationResult(
            path=records_dir / "2026-07-02-android-ios-1.0.0+42.md",
            errors=[],
        )
        status = release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[qa_preflight.PreflightCheck("Stub", "ok", ["ready"])],
            record_results=[valid_record],
            final_errors=[
                "Release evidence document must include Markdown links for every validated QA build record: "
                "2026-07-02-android-ios-1.0.0+42.md",
            ],
            records_dir=records_dir,
        )

        output = release_status_report.format_status(status)

        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(
                records_dir=str(records_dir.resolve()),
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                str(records_dir.resolve()),
                version=release_evidence_commands.DEFAULT_VERSION,
                log=release_evidence_commands.final_release_evidence_log_path(
                    release_evidence_commands.DEFAULT_VERSION,
                    records_dir=str(records_dir.resolve()),
                ),
            ),
            output,
        )

    def test_android_scope_reports_ios_records_as_out_of_scope(self) -> None:
        root = self.make_root()
        ios_record = validate_qa_build_records_dir.RecordValidationResult(
            path=root / "docs" / "qa-builds" / "2026-07-02-ios-1.0.0+42.md",
            errors=[],
        )
        bad_ios_record = validate_qa_build_records_dir.RecordValidationResult(
            path=root / "docs" / "qa-builds" / "2026-07-02-ios-1.0.0+43.md",
            errors=["Missing SSH smoke evidence"],
        )
        status = release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[qa_preflight.PreflightCheck("Stub", "ok", ["ready"])],
            record_results=[ios_record, bad_ios_record],
            final_errors=[
                "Final release evidence requires a validated Android signed-build QA record.",
            ],
            scope="android",
        )

        output = release_status_report.format_status(status)

        self.assertIn(
            "QA build records: 0 in-scope valid, 1 out-of-scope valid, 1 invalid",
            output,
        )
        self.assertIn("[OUT-OF-SCOPE] 2026-07-02-ios-1.0.0+42.md", output)
        self.assertIn("[OUT-OF-SCOPE INVALID] 2026-07-02-ios-1.0.0+43.md", output)
        self.assertIn("Missing SSH smoke evidence", output)
        self.assertNotIn("[VALID] 2026-07-02-ios-1.0.0+42.md", output)
        self.assertIn(
            release_evidence_commands.signed_qa_record_hint(scope="android"),
            output,
        )
        self.assertIn(
            "Create and validate in-scope signed-build QA records under docs/qa-builds/.",
            output,
        )
        self.assertNotIn(
            release_evidence_commands.qa_build_record_report_hint(str(bad_ios_record.path)),
            output,
        )
        self.assertNotIn("Resolve final evidence blockers above:", output)

    def test_not_ready_with_version_mismatch_uses_final_verifier_next_action(self) -> None:
        root = self.make_root()
        valid_records = [
            validate_qa_build_records_dir.RecordValidationResult(
                path=root / "docs" / "qa-builds" / "2026-07-02-android-1.0.0+42.md",
                errors=[],
            ),
            validate_qa_build_records_dir.RecordValidationResult(
                path=root / "docs" / "qa-builds" / "2026-07-02-ios-1.0.0+43.md",
                errors=[],
            ),
        ]
        status = release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[qa_preflight.PreflightCheck("Stub", "ok", ["ready"])],
            record_results=valid_records,
            final_errors=[
                "Final release evidence records must use the same version/build: 1.0.0+42, 1.0.0+43",
            ],
        )

        output = release_status_report.format_status(status)

        self.assertIn(
            release_evidence_commands.qa_record_version_mismatch_hint(),
            output,
        )
        self.assertNotIn(
            release_evidence_commands.release_handoff_command(),
            output,
        )

    def test_ready_report_when_everything_passes(self) -> None:
        root = self.make_root()
        valid_record = validate_qa_build_records_dir.RecordValidationResult(
            path=root / "docs" / "qa-builds" / "2026-07-02-android-ios-1.0.0+42.md",
            errors=[],
        )

        status = release_status_report.build_status(
            root,
            preflight=lambda *_args, **_kwargs: [
                qa_preflight.PreflightCheck("Android local signing inputs", "ok", ["ready"]),
            ],
            validate_records=lambda _: [valid_record],
            verify_final=lambda _path, **_kwargs: [],
        )
        output = release_status_report.format_status(status)

        self.assertTrue(status.ready)
        self.assertIn("[VALID] 2026-07-02-android-ios-1.0.0+42.md", output)
        self.assertIn(
            "QA build records: 1 in-scope valid, 0 out-of-scope valid, 0 invalid",
            output,
        )
        self.assertIn("Result: READY for final release approval.", output)

        scoped_ready = release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[qa_preflight.PreflightCheck("Stub", "ok", ["ready"])],
            record_results=[
                validate_qa_build_records_dir.RecordValidationResult(
                    path=root / "docs" / "qa-builds" / "2026-07-02-android-1.0.0+42.md",
                    errors=[],
                ),
                validate_qa_build_records_dir.RecordValidationResult(
                    path=root / "docs" / "qa-builds" / "2026-07-02-ios-1.0.0+43.md",
                    errors=["Out-of-scope iOS evidence gap"],
                ),
            ],
            final_errors=[],
            scope="android",
        )
        scoped_output = release_status_report.format_status(scoped_ready)
        self.assertTrue(scoped_ready.ready)
        self.assertIn(
            "QA build records: 1 in-scope valid, 0 out-of-scope valid, 1 invalid",
            scoped_output,
        )
        self.assertIn("[OUT-OF-SCOPE INVALID] 2026-07-02-ios-1.0.0+43.md", scoped_output)
        self.assertIn("Result: READY for final release approval.", scoped_output)

    def test_build_status_passes_scope_to_final_verifier(self) -> None:
        root = self.make_root()
        seen: dict[str, str] = {}

        def verify_final(_path: Path, **kwargs: str) -> list[str]:
            seen["scope"] = kwargs["scope"]
            return []

        status = release_status_report.build_status(
            root,
            scope="android",
            preflight=lambda *_args, **_kwargs: [],
            validate_records=lambda _: [],
            verify_final=verify_final,
        )

        self.assertEqual("android", seen["scope"])
        self.assertEqual("android", status.scope)

    def test_main_prints_not_ready_to_stderr_for_current_empty_fixture(self) -> None:
        root = self.make_root()
        stderr = StringIO()

        with redirect_stderr(stderr):
            exit_code = release_status_report.main(["--root", str(root)])

        self.assertEqual(1, exit_code)
        self.assertIn("Result: NOT READY.", stderr.getvalue())

    def test_main_prints_ready_to_stdout_with_stubbed_status(self) -> None:
        root = self.make_root()
        ready = release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[qa_preflight.PreflightCheck("Stub", "ok", ["ready"])],
            record_results=[
                validate_qa_build_records_dir.RecordValidationResult(
                    path=root / "docs" / "qa-builds" / "2026-07-02-android-ios-1.0.0+42.md",
                    errors=[],
                ),
            ],
            final_errors=[],
        )
        stdout = StringIO()
        original_build_status = release_status_report.build_status
        try:
            release_status_report.build_status = lambda *_args, **_kwargs: ready
            with redirect_stdout(stdout):
                exit_code = release_status_report.main(["--root", str(root)])
        finally:
            release_status_report.build_status = original_build_status

        self.assertEqual(0, exit_code)
        self.assertIn("Result: READY", stdout.getvalue())

    def test_main_passes_ios_expected_values_to_status_builder(self) -> None:
        root = self.make_root()
        ready = release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[qa_preflight.PreflightCheck("Stub", "ok", ["ready"])],
            record_results=[],
            final_errors=[],
        )
        seen: dict[str, object] = {}
        stdout = StringIO()
        original_build_status = release_status_report.build_status
        try:
            def fake_build_status(*args: object, **kwargs: object) -> release_status_report.ReleaseStatus:
                seen["args"] = args
                seen["kwargs"] = kwargs
                return ready

            release_status_report.build_status = fake_build_status
            with redirect_stdout(stdout):
                exit_code = release_status_report.main(
                    [
                        "--root",
                        str(root),
                        "--team-id",
                        "abcde12345",
                        "--export-method",
                        "ad-hoc",
                    ],
                )
        finally:
            release_status_report.build_status = original_build_status

        self.assertEqual(0, exit_code)
        self.assertEqual((root,), seen["args"])
        self.assertEqual("ABCDE12345", seen["kwargs"]["ios_team_id"])
        self.assertEqual("ad-hoc", seen["kwargs"]["ios_export_method"])

    def test_main_passes_records_dir_and_scope_to_status_builder(self) -> None:
        root = self.make_root()
        records_dir = root / "tmp" / "qa-builds"
        seen: dict[str, object] = {}
        stderr = StringIO()
        original_build_status = release_status_report.build_status
        try:
            def fake_build_status(*args: object, **kwargs: object) -> release_status_report.ReleaseStatus:
                seen["args"] = args
                seen["kwargs"] = kwargs
                return release_status_report.ReleaseStatus(
                    root=root,
                    preflight_checks=[
                        qa_preflight.PreflightCheck("Stub", "ok", ["ready"]),
                    ],
                    record_results=[],
                    final_errors=[
                        "Final release evidence requires at least one completed Android signed-build QA record.",
                    ],
                    scope=str(kwargs["scope"]),
                    records_dir=Path(kwargs["records_dir"]),  # type: ignore[arg-type]
                )

            release_status_report.build_status = fake_build_status
            with redirect_stderr(stderr):
                exit_code = release_status_report.main(
                    [
                        "--root",
                        str(root),
                        "--scope",
                        "android",
                        "--records-dir",
                        str(records_dir),
                    ],
                )
        finally:
            release_status_report.build_status = original_build_status

        self.assertEqual(1, exit_code)
        self.assertEqual((root,), seen["args"])
        self.assertEqual("android", seen["kwargs"]["scope"])
        self.assertEqual(records_dir, seen["kwargs"]["records_dir"])
        output = stderr.getvalue()
        self.assertIn(
            f"Create and validate in-scope signed-build QA records under {records_dir.resolve()}/.",
            output,
        )
        self.assertIn(
            release_evidence_commands.qa_preflight_command(
                scope="android",
                records_dir=str(records_dir.resolve()),
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                str(records_dir.resolve()),
                scope="android",
                version=release_evidence_commands.DEFAULT_VERSION,
                log=release_evidence_commands.final_release_evidence_log_path(
                    release_evidence_commands.DEFAULT_VERSION,
                    scope="android",
                    records_dir=str(records_dir.resolve()),
                ),
            ),
            output,
        )


if __name__ == "__main__":
    unittest.main()
