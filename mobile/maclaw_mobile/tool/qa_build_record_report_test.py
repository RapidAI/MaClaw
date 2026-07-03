from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import qa_build_record_report
from validate_qa_build_record_test import complete_record


class QaBuildRecordReportTest(unittest.TestCase):
    def write_record(self, root: Path, text: str, name: str | None = None) -> Path:
        records_dir = root / "qa-builds"
        records_dir.mkdir(exist_ok=True)
        record = records_dir / (name or "2026-07-02-android-ios-1.0.0+42.md")
        record.write_text(text, encoding="utf-8")
        return record

    def test_complete_record_reports_pass(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = self.write_record(Path(tmp), complete_record())

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertTrue(report.passed)
            self.assertIn("Status: PASS", output)
            self.assertIn("Required evidence: 100/100 occurrences filled", output)
            self.assertIn("No gaps found", output)

    def test_report_groups_missing_and_invalid_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            text = complete_record()
            text = text.replace("Branch: codex/mobile-release\n", "")
            text = text.replace(
                "Selected HubCenter URL: https://hubs.maclaw.top",
                "Selected HubCenter URL: https://custom.example.test",
            )
            record = self.write_record(Path(tmp), text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("Status: FAIL", output)
            self.assertIn("Required evidence: 99/100 occurrences filled", output)
            self.assertIn("Evidence fields and values:", output)
            self.assertIn("- Branch", output)
            self.assertIn(
                "- Selected HubCenter URL must be one of the preset official HubCenters",
                output,
            )

    def test_report_groups_filename_errors(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = self.write_record(
                Path(tmp),
                complete_record(),
                name="qa-record.md",
            )

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("Path / filename:", output)
            self.assertIn(
                "- QA build record filename must be YYYY-MM-DD-<android|ios|android-ios>-<version+build>.md",
                output,
            )

    def test_report_groups_local_artifact_hash_mismatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            artifact = (
                root
                / "qa-builds"
                / "build"
                / "app"
                / "outputs"
                / "flutter-apk"
                / "app-release.apk"
            )
            artifact.parent.mkdir(parents=True)
            artifact.write_bytes(b"signed release artifact")
            record = self.write_record(root, complete_record())

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("Local artifact hashes:", output)
            self.assertIn(
                "- SHA256 does not match local artifact build/app/outputs/flutter-apk/app-release.apk",
                output,
            )

    def test_main_prints_pass_to_stdout(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = self.write_record(Path(tmp), complete_record())
            stdout = StringIO()

            with redirect_stdout(stdout):
                exit_code = qa_build_record_report.main([str(record)])

            self.assertEqual(0, exit_code)
            self.assertIn("Status: PASS", stdout.getvalue())

    def test_main_prints_fail_to_stderr(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = self.write_record(
                Path(tmp),
                complete_record().replace("Branch: codex/mobile-release\n", ""),
            )
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = qa_build_record_report.main([str(record)])

            self.assertEqual(1, exit_code)
            self.assertIn("Status: FAIL", stderr.getvalue())
            self.assertIn("- Branch", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
