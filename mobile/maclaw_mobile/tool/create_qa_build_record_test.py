from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import create_qa_build_record


class CreateQaBuildRecordTest(unittest.TestCase):
    def test_create_record_uses_validator_filename_and_prefills_identity(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text(
                "Date: YYYY-MM-DD\n"
                "Version/build number: app version + build number, such as 1.0.0+42\n",
                encoding="utf-8",
            )

            target = create_qa_build_record.create_record(
                template_path=template,
                records_dir=records_dir,
                record_date="2026-07-02",
                scope="android-ios",
                version_build="1.0.0+42",
            )

            self.assertEqual(
                records_dir / "2026-07-02-android-ios-1.0.0+42.md",
                target,
            )
            text = target.read_text(encoding="utf-8")
            self.assertIn("Date: 2026-07-02", text)
            self.assertIn("Version/build number: 1.0.0+42", text)

    def test_create_record_rejects_overwrite_without_force(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text("Date: YYYY-MM-DD\n", encoding="utf-8")

            create_qa_build_record.create_record(
                template_path=template,
                records_dir=records_dir,
                record_date="2026-07-02",
                scope="android",
                version_build="1.0.0+42",
            )

            with self.assertRaises(FileExistsError):
                create_qa_build_record.create_record(
                    template_path=template,
                    records_dir=records_dir,
                    record_date="2026-07-02",
                    scope="android",
                    version_build="1.0.0+42",
                )

    def test_create_record_force_overwrites_existing_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text("Date: YYYY-MM-DD\n", encoding="utf-8")
            target = records_dir / "2026-07-02-ios-1.0.0+42.md"
            records_dir.mkdir()
            target.write_text("old", encoding="utf-8")

            create_qa_build_record.create_record(
                template_path=template,
                records_dir=records_dir,
                record_date="2026-07-02",
                scope="ios",
                version_build="1.0.0+42",
                force=True,
            )

            self.assertIn("Date: 2026-07-02", target.read_text(encoding="utf-8"))

    def test_validate_version_build_rejects_missing_build_number(self) -> None:
        with self.assertRaises(create_qa_build_record.argparse.ArgumentTypeError):
            create_qa_build_record.validate_version_build("1.0.0")

    def test_main_creates_record_and_prints_validation_hint(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text("Date: YYYY-MM-DD\n", encoding="utf-8")
            output = StringIO()

            with redirect_stdout(output):
                exit_code = create_qa_build_record.main(
                    [
                        "--date",
                        "2026-07-02",
                        "--scope",
                        "android",
                        "--version",
                        "1.0.0+42",
                        "--template",
                        str(template),
                        "--records-dir",
                        str(records_dir),
                    ],
                )

            self.assertEqual(0, exit_code)
            self.assertTrue((records_dir / "2026-07-02-android-1.0.0+42.md").exists())
            self.assertIn("Validate after completing evidence", output.getvalue())

    def test_main_reports_existing_record_without_traceback(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text("Date: YYYY-MM-DD\n", encoding="utf-8")
            create_qa_build_record.create_record(
                template_path=template,
                records_dir=records_dir,
                record_date="2026-07-02",
                scope="android",
                version_build="1.0.0+42",
            )
            error = StringIO()

            with redirect_stderr(error):
                exit_code = create_qa_build_record.main(
                    [
                        "--date",
                        "2026-07-02",
                        "--scope",
                        "android",
                        "--version",
                        "1.0.0+42",
                        "--template",
                        str(template),
                        "--records-dir",
                        str(records_dir),
                    ],
                )

            self.assertEqual(1, exit_code)
            self.assertIn("already exists", error.getvalue())


if __name__ == "__main__":
    unittest.main()
