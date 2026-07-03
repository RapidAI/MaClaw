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
import verify_final_release_evidence


class VerifyFinalReleaseEvidenceTest(unittest.TestCase):
    def _release_evidence(self, records_dir: Path, text: str) -> Path:
        evidence = records_dir.parent / "release_evidence.md"
        evidence.write_text(text, encoding="utf-8")
        return evidence

    def test_empty_directory_is_not_final_release_ready(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            errors = verify_final_release_evidence.verify_final_release_evidence(Path(tmp))

        self.assertIn(
            "Final release evidence requires at least one completed signed-build QA record.",
            errors,
        )

    def test_android_ios_record_covers_both_platforms(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence(
                records_dir,
                f"- [{record.name}](docs/qa-builds/{record.name})\n",
            )

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(
                        path=record,
                        errors=[],
                    ),
                ],
            ):
                self.assertEqual(
                    [],
                    verify_final_release_evidence.verify_final_release_evidence(
                        records_dir,
                        evidence,
                    ),
                )

    def test_separate_android_and_ios_records_cover_final_release(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            android = records_dir / "2026-07-02-android-1.0.0+42.md"
            ios = records_dir / "2026-07-02-ios-1.0.0+42.md"
            android.write_text("android", encoding="utf-8")
            ios.write_text("ios", encoding="utf-8")
            evidence = self._release_evidence(
                records_dir,
                "\n".join(
                    [
                        f"- [{android.name}](docs/qa-builds/{android.name})",
                        f"- [{ios.name}](docs/qa-builds/{ios.name})",
                    ],
                ),
            )

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(android, []),
                    validate_qa_build_records_dir.RecordValidationResult(ios, []),
                ],
            ):
                self.assertEqual(
                    [],
                    verify_final_release_evidence.verify_final_release_evidence(
                        records_dir,
                        evidence,
                    ),
                )

    def test_android_only_record_reports_missing_ios(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            android = records_dir / "2026-07-02-android-1.0.0+42.md"
            android.write_text("android", encoding="utf-8")

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(android, []),
                ],
            ):
                errors = verify_final_release_evidence.verify_final_release_evidence(records_dir)

        self.assertIn(
            "Final release evidence requires a validated iOS signed-build QA record.",
            errors,
        )

    def test_record_validation_errors_are_reported(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("bad", encoding="utf-8")

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(
                        path=record,
                        errors=["Missing field"],
                    ),
                ],
            ):
                errors = verify_final_release_evidence.verify_final_release_evidence(records_dir)

        self.assertTrue(any(str(record) in error for error in errors))
        self.assertTrue(any("Missing field" in error for error in errors))

    def test_valid_records_must_be_linked_from_release_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence(records_dir, "No QA record link yet.\n")

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(record, []),
                ],
            ):
                errors = verify_final_release_evidence.verify_final_release_evidence(
                    records_dir,
                    evidence,
                )

        self.assertIn(
            "Release evidence document must include Markdown links for every validated QA build record: "
            "2026-07-02-android-ios-1.0.0+42.md",
            errors,
        )

    def test_record_filename_mention_without_markdown_link_is_not_enough(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence(
                records_dir,
                f"QA record filename only: {record.name}\n",
            )

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(record, []),
                ],
            ):
                errors = verify_final_release_evidence.verify_final_release_evidence(
                    records_dir,
                    evidence,
                )

        self.assertIn(
            "Release evidence document must include Markdown links for every validated QA build record: "
            "2026-07-02-android-ios-1.0.0+42.md",
            errors,
        )

    def test_valid_records_reject_missing_release_evidence_document(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            missing_evidence = records_dir.parent / "missing-release-evidence.md"

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(record, []),
                ],
            ):
                errors = verify_final_release_evidence.verify_final_release_evidence(
                    records_dir,
                    missing_evidence,
                )

        self.assertTrue(
            any("Release evidence document does not exist" in error for error in errors),
        )

    def test_main_rejects_missing_directory(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            missing = Path(tmp) / "missing"
            output = StringIO()

            with redirect_stdout(output):
                exit_code = verify_final_release_evidence.main([str(missing)])

        self.assertEqual(1, exit_code)
        self.assertIn("directory does not exist", output.getvalue())


if __name__ == "__main__":
    unittest.main()
