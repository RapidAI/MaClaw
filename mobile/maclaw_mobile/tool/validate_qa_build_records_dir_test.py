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
import release_evidence_commands


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

    def test_completed_record_paths_skip_release_handoff_markdown_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            handoff = records_dir / "handoff-1.0.0+42.md"
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            handoff.write_text("# Release handoff evidence", encoding="utf-8")
            record.write_text("Date: 2026-07-02\n", encoding="utf-8")

            self.assertEqual(
                [record],
                validate_qa_build_records_dir.completed_record_paths(records_dir),
            )

    def test_completed_record_paths_skip_scoped_release_handoff_markdown_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            android_handoff = records_dir / "handoff-android-1.0.0+42.md"
            ios_handoff = records_dir / "handoff-ios-1.0.0+42.md"
            android_record = records_dir / "2026-07-02-android-1.0.0+42.md"
            ios_record = records_dir / "2026-07-02-ios-1.0.0+42.md"
            for path in [android_handoff, ios_handoff]:
                path.write_text("# Scoped release handoff evidence", encoding="utf-8")
            for path in [android_record, ios_record]:
                path.write_text("Date: 2026-07-02\n", encoding="utf-8")

            self.assertEqual(
                [android_record, ios_record],
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
            self.assertIn(
                "does not prove final release readiness",
                output.getvalue(),
            )
            self.assertIn("Next action:", output.getvalue())
            self.assertIn(
                "release handoff is only a QA plan, not a completed QA record",
                output.getvalue(),
            )
            self.assertIn(
                release_evidence_commands.create_record_command(),
                output.getvalue(),
            )
            self.assertIn(
                f"python3 tool/verify_final_release_evidence.py {Path(tmp).resolve()}",
                output.getvalue(),
            )
            self.assertIn(
                f"Next final evidence check: `python3 tool/verify_final_release_evidence.py {Path(tmp).resolve()} --scope android-ios`",
                output.getvalue(),
            )

    def test_main_points_valid_directory_to_final_evidence_verifier(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("Date: 2026-07-02\n", encoding="utf-8")
            output = StringIO()

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(
                        path=record,
                        errors=[],
                    ),
                ],
            ), redirect_stdout(output):
                exit_code = validate_qa_build_records_dir.main([tmp])

            self.assertEqual(0, exit_code)
            self.assertIn("1 record(s)", output.getvalue())
            self.assertNotIn("does not prove final release readiness", output.getvalue())
            self.assertIn(
                f"python3 tool/verify_final_release_evidence.py {records_dir.resolve()}",
                output.getvalue(),
            )
            self.assertIn("--scope android-ios", output.getvalue())
            self.assertIn(
                "--log docs/qa-builds/final-release-evidence-1.0.0+42.log",
                output.getvalue(),
            )

    def test_main_points_scoped_valid_directory_to_scoped_final_log(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-1.0.0+42.md"
            record.write_text("Date: 2026-07-02\n", encoding="utf-8")
            output = StringIO()

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(
                        path=record,
                        errors=[],
                    ),
                ],
            ), redirect_stdout(output):
                exit_code = validate_qa_build_records_dir.main([tmp])

            self.assertEqual(0, exit_code)
            self.assertIn("--scope android", output.getvalue())
            self.assertIn(
                "--log docs/qa-builds/final-release-evidence-android-1.0.0+42.log",
                output.getvalue(),
            )

    def test_main_explicit_scope_uses_only_in_scope_records_for_final_log(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            android_record = records_dir / "2026-07-02-android-1.0.0+42.md"
            ios_record = records_dir / "2026-07-02-ios-1.0.0+43.md"
            bad_ios_record = records_dir / "2026-07-03-ios-1.0.0+44.md"
            android_record.write_text("Date: 2026-07-02\n", encoding="utf-8")
            ios_record.write_text("Date: 2026-07-02\n", encoding="utf-8")
            bad_ios_record.write_text("bad", encoding="utf-8")
            output = StringIO()

            self.assertEqual(
                "android",
                validate_qa_build_records_dir.scope_for_record(android_record),
            )
            self.assertTrue(
                validate_qa_build_records_dir.record_matches_scope(
                    android_record,
                    "android",
                ),
            )
            self.assertFalse(
                validate_qa_build_records_dir.record_matches_scope(
                    ios_record,
                    "android",
                ),
            )
            self.assertTrue(
                validate_qa_build_records_dir.record_is_known_out_of_scope(
                    bad_ios_record,
                    "android",
                ),
            )

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(
                        path=android_record,
                        errors=[],
                    ),
                    validate_qa_build_records_dir.RecordValidationResult(
                        path=ios_record,
                        errors=[],
                    ),
                    validate_qa_build_records_dir.RecordValidationResult(
                        path=bad_ios_record,
                        errors=["Missing iOS TestFlight evidence"],
                    ),
                ],
            ), redirect_stdout(output):
                exit_code = validate_qa_build_records_dir.main(
                    [tmp, "--scope", "android"],
                )

            self.assertEqual(0, exit_code)
            self.assertIn(
                "Scoped record coverage: 1 in-scope, 1 out-of-scope for Android.",
                output.getvalue(),
            )
            self.assertIn(
                "Out-of-scope invalid records ignored for Android scope:",
                output.getvalue(),
            )
            self.assertIn("2026-07-03-ios-1.0.0+44.md", output.getvalue())
            self.assertIn("Missing iOS TestFlight evidence", output.getvalue())
            self.assertIn("--scope android", output.getvalue())
            self.assertIn(
                "--log docs/qa-builds/final-release-evidence-android-1.0.0+42.log",
                output.getvalue(),
            )
            self.assertNotIn("1.0.0+43.log", output.getvalue())
            self.assertNotIn("QA build record directory validation failed", output.getvalue())
            self.assertNotIn("qa_build_record_report.py <failed-record>", output.getvalue())

    def test_main_explicit_scope_reports_out_of_scope_only_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            ios_record = records_dir / "2026-07-02-ios-1.0.0+43.md"
            ios_record.write_text("Date: 2026-07-02\n", encoding="utf-8")
            output = StringIO()

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(
                        path=ios_record,
                        errors=[],
                    ),
                ],
            ), redirect_stdout(output):
                exit_code = validate_qa_build_records_dir.main(
                    [tmp, "--scope", "android"],
                )

            self.assertEqual(0, exit_code)
            self.assertIn(
                "Scoped record coverage: 0 in-scope, 1 out-of-scope for Android.",
                output.getvalue(),
            )
            self.assertIn(
                "No validated in-scope QA build records found",
                output.getvalue(),
            )
            self.assertIn(
                f"python3 tool/verify_final_release_evidence.py {records_dir.resolve()} --scope android",
                output.getvalue(),
            )
            self.assertIn("Next action:", output.getvalue())
            self.assertIn(
                release_evidence_commands.signed_qa_record_hint(scope="android"),
                output.getvalue(),
            )
            self.assertIn(
                "tool/create_qa_build_record.py --scope android",
                output.getvalue(),
            )
            self.assertNotIn("setup_ios_export_options.py", output.getvalue())
            self.assertNotIn("signed_artifact_evidence.py ios", output.getvalue())
            self.assertIn(
                f"Next final evidence check: `python3 tool/verify_final_release_evidence.py {records_dir.resolve()} --scope android`",
                output.getvalue(),
            )

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

    def test_main_failure_points_to_gap_report(self) -> None:
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
            self.assertIn("Next action:", output.getvalue())
            self.assertIn(
                "python3 tool/qa_build_record_report.py <failed-record>",
                output.getvalue(),
            )
            self.assertIn("redaction remediation", output.getvalue())


if __name__ == "__main__":
    unittest.main()
