from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

import validate_qa_build_records_dir


class ValidateQaBuildRecordsDirTest(unittest.TestCase):
    def test_completed_record_paths_skip_readme_and_attachments(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            readme = records_dir / "README.md"
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            attachment = records_dir / "device-log.txt"
            readme.write_text("instructions", encoding="utf-8")
            record.write_text("Date: 2026-07-02\n", encoding="utf-8")
            attachment.write_text("log", encoding="utf-8")

            self.assertEqual(
                [record],
                validate_qa_build_records_dir.completed_record_paths(records_dir),
            )

    def test_validate_directory_collects_errors_per_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            good = records_dir / "2026-07-02-android-1.0.0+42.md"
            bad = records_dir / "2026-07-02-ios-1.0.0+42.md"
            good.write_text("good", encoding="utf-8")
            bad.write_text("bad", encoding="utf-8")

            results = validate_qa_build_records_dir.validate_directory(
                records_dir,
                validate_file=lambda path: ["missing field"] if path == bad else [],
            )

            self.assertEqual([good, bad], [result.path for result in results])
            self.assertEqual([], results[0].errors)
            self.assertEqual(["missing field"], results[1].errors)

    def test_main_passes_empty_directory_without_requiring_signed_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            output = StringIO()
            with redirect_stdout(output):
                exit_code = validate_qa_build_records_dir.main([tmp])

            self.assertEqual(0, exit_code)
            self.assertIn("0 record(s)", output.getvalue())

    def test_main_rejects_missing_directory(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            missing = Path(tmp) / "missing-qa-builds"
            output = StringIO()

            with redirect_stdout(output):
                exit_code = validate_qa_build_records_dir.main([str(missing)])

            self.assertEqual(1, exit_code)
            self.assertIn("directory does not exist", output.getvalue())

    def test_main_rejects_file_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "2026-07-02-android-1.0.0+42.md"
            record.write_text("Date: 2026-07-02\n", encoding="utf-8")
            output = StringIO()

            with redirect_stdout(output):
                exit_code = validate_qa_build_records_dir.main([str(record)])

            self.assertEqual(1, exit_code)
            self.assertIn("path is not a directory", output.getvalue())

    def test_main_returns_failure_when_any_record_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-1.0.0+42.md"
            record.write_text("bad", encoding="utf-8")
            output = StringIO()

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(
                        path=record,
                        errors=["Missing field"],
                    ),
                ],
            ), redirect_stdout(output):
                exit_code = validate_qa_build_records_dir.main([tmp])

            self.assertEqual(1, exit_code)
            self.assertIn("QA build record directory validation failed", output.getvalue())
            self.assertIn("Missing field", output.getvalue())


if __name__ == "__main__":
    unittest.main()
