from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import qa_release_evidence_links
from validate_qa_build_record_test import complete_record


class QaReleaseEvidenceLinksTest(unittest.TestCase):
    def records_dir(self, root: Path) -> Path:
        records_dir = root / "qa-builds"
        records_dir.mkdir()
        return records_dir

    def write_record(self, records_dir: Path, text: str, name: str) -> Path:
        path = records_dir / name
        path.write_text(text, encoding="utf-8")
        return path

    def test_empty_directory_reports_no_validated_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))

            summary = qa_release_evidence_links.summarize_records(records_dir)
            output = qa_release_evidence_links.format_links(summary)

            self.assertEqual([], summary.valid_records)
            self.assertIn("No validated QA build records found.", output)

    def test_valid_record_formats_release_evidence_link(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                complete_record(),
                "2026-07-02-android-ios-1.0.0+42.md",
            )

            summary = qa_release_evidence_links.summarize_records(records_dir)
            output = qa_release_evidence_links.format_links(summary)

            self.assertEqual(1, len(summary.valid_records))
            self.assertIn(
                "- [2026-07-02-android-ios-1.0.0+42.md](docs/qa-builds/2026-07-02-android-ios-1.0.0+42.md)",
                output,
            )

    def test_invalid_record_is_reported_but_not_linked(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                complete_record().replace("Branch: codex/mobile-release\n", ""),
                "2026-07-02-android-ios-1.0.0+42.md",
            )

            summary = qa_release_evidence_links.summarize_records(records_dir)
            output = qa_release_evidence_links.format_links(summary)

            self.assertEqual([], summary.valid_records)
            self.assertEqual(1, len(summary.invalid_records))
            self.assertIn("Records not linked because validation failed:", output)
            self.assertIn("- 2026-07-02-android-ios-1.0.0+42.md", output)
            self.assertIn("  - Branch", output)
            self.assertNotIn(
                "[2026-07-02-android-ios-1.0.0+42.md](",
                output,
            )

    def test_main_returns_failure_for_invalid_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                complete_record().replace("Branch: codex/mobile-release\n", ""),
                "2026-07-02-android-ios-1.0.0+42.md",
            )
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = qa_release_evidence_links.main([str(records_dir)])

            self.assertEqual(1, exit_code)
            self.assertIn("Records not linked", stderr.getvalue())

    def test_main_prints_links_for_valid_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                complete_record(),
                "2026-07-02-android-ios-1.0.0+42.md",
            )
            stdout = StringIO()

            with redirect_stdout(stdout):
                exit_code = qa_release_evidence_links.main([str(records_dir)])

            self.assertEqual(0, exit_code)
            self.assertIn("Validated records to link", stdout.getvalue())


if __name__ == "__main__":
    unittest.main()
