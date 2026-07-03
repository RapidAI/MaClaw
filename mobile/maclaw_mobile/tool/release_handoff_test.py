from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import qa_preflight
import release_handoff
import release_status_report
import validate_qa_build_records_dir


class ReleaseHandoffTest(unittest.TestCase):
    def make_root(self) -> Path:
        root = Path(self.tmp.name)
        (root / "docs" / "qa-builds").mkdir(parents=True)
        return root

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def blocked_status(self, root: Path) -> release_status_report.ReleaseStatus:
        return release_status_report.ReleaseStatus(
            root=root,
            preflight_checks=[
                qa_preflight.PreflightCheck(
                    "Android local signing inputs",
                    "blocker",
                    ["Missing Android signing file: android/key.properties"],
                ),
                qa_preflight.PreflightCheck(
                    "iOS export options",
                    "blocker",
                    ["Missing iOS export options plist: ios/ExportOptions.plist"],
                ),
            ],
            record_results=[],
            final_errors=["Final release evidence requires at least one completed signed-build QA record."],
        )

    def ready_status(self, root: Path) -> release_status_report.ReleaseStatus:
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
        self.assertIn("python3 tool/setup_android_signing.py", output)
        self.assertIn("python3 tool/build_android_release.py --artifact apk", output)
        self.assertIn("python3 tool/setup_ios_export_options.py --team-id ABCDE12345 --export-method development", output)
        self.assertIn("python3 tool/create_qa_build_record.py --scope android-ios --version 1.0.0+42", output)
        self.assertIn("docs/qa-builds/<YYYY-MM-DD>-android-ios-1.0.0+42.md", output)
        self.assertIn("HubCenter discovery result", output)
        self.assertIn("Digital employee list", output)
        self.assertIn("Do not store signing secrets", output)

    def test_format_handoff_reports_ready_status(self) -> None:
        root = self.make_root()
        handoff = release_handoff.build_handoff(root, build_status=self.ready_status)

        output = release_handoff.format_handoff(handoff)

        self.assertIn("Current status: READY for final release approval.", output)
        self.assertNotIn("Current status: NOT READY", output)

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

    def test_main_prints_ready_handoff_to_stdout(self) -> None:
        root = self.make_root()
        stdout = StringIO()
        stderr = StringIO()
        original_build_status = release_handoff.release_status_report.build_status
        try:
            release_handoff.release_status_report.build_status = self.ready_status
            with redirect_stdout(stdout), redirect_stderr(stderr):
                exit_code = release_handoff.main(["--root", str(root)])
        finally:
            release_handoff.release_status_report.build_status = original_build_status

        self.assertEqual(0, exit_code)
        self.assertIn("MaClaw Mobile Release Handoff", stdout.getvalue())
        self.assertEqual("", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
