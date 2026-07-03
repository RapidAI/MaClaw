from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import qa_preflight
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
            verify_final=lambda _: ["Final release evidence requires a validated Android signed-build QA record."],
        )
        output = release_status_report.format_status(status)

        self.assertFalse(status.ready)
        self.assertIn("[BLOCKER] Android local signing inputs", output)
        self.assertIn("QA build records: 0 valid, 1 invalid", output)
        self.assertIn("Branch", output)
        self.assertIn("Result: NOT READY.", output)
        self.assertIn("release_handoff.py with --version <version+build>", output)
        self.assertIn("--team-id <APPLE_TEAM_ID>", output)
        self.assertIn("--export-method <export-method>", output)
        self.assertIn("--output docs/qa-builds/handoff-<version+build>.md", output)
        self.assertIn("verify_runtime_boundary.py --log", output)
        self.assertIn("run_release_gates.py --log", output)
        self.assertIn("prefilled create_qa_build_record.py", output)

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
            verify_final=lambda _: [],
        )
        output = release_status_report.format_status(status)

        self.assertTrue(status.ready)
        self.assertIn("[VALID] 2026-07-02-android-ios-1.0.0+42.md", output)
        self.assertIn("Result: READY for final release approval.", output)

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


if __name__ == "__main__":
    unittest.main()
