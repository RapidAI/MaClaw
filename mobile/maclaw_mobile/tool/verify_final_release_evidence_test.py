from __future__ import annotations

import hashlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

import validate_qa_build_records_dir
import verify_final_release_evidence
import release_evidence_commands
from validate_qa_build_record_test import scoped_record


class VerifyFinalReleaseEvidenceTest(unittest.TestCase):
    def _release_evidence(self, records_dir: Path, text: str) -> Path:
        evidence = records_dir.parent / "release_evidence.md"
        evidence.write_text(text, encoding="utf-8")
        return evidence

    def _release_evidence_with_links(self, records_dir: Path, links: list[str]) -> Path:
        return self._release_evidence(
            records_dir,
            "\n".join(
                [
                    "# Evidence",
                    "Background mention outside the guarded block is ignored.",
                    verify_final_release_evidence.qa_release_evidence_links.QA_LINKS_START,
                    *links,
                    verify_final_release_evidence.qa_release_evidence_links.QA_LINKS_END,
                ],
            )
            + "\n",
        )

    def _android_scoped_record_with_local_artifact(self, records_dir: Path) -> str:
        artifact = (
            records_dir
            / "build"
            / "app"
            / "outputs"
            / "flutter-apk"
            / "app-release.apk"
        )
        artifact.parent.mkdir(parents=True, exist_ok=True)
        artifact.write_bytes(b"signed release apk bytes")
        digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
        return scoped_record("android").replace("SHA256: " + "a" * 64, f"SHA256: {digest}")

    def test_empty_directory_is_not_final_release_ready(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            errors = verify_final_release_evidence.verify_final_release_evidence(Path(tmp))

        self.assertIn(
            "Final release evidence requires at least one completed signed-build QA record.",
            errors,
        )

    def test_handoff_evidence_files_do_not_count_as_signed_build_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            for name in [
                "handoff-1.0.0+42.md",
                "handoff-android-1.0.0+42.md",
                "handoff-ios-1.0.0+42.md",
            ]:
                (records_dir / name).write_text(
                    "# Release handoff evidence\n",
                    encoding="utf-8",
                )

            errors = verify_final_release_evidence.verify_final_release_evidence(
                records_dir,
            )

            self.assertIn(
                "Final release evidence requires at least one completed signed-build QA record.",
                errors,
            )
            self.assertEqual(
                [],
                validate_qa_build_records_dir.completed_record_paths(records_dir),
            )

    def test_android_ios_record_covers_both_platforms(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{record.name}](docs/qa-builds/{record.name})"],
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

    def test_legacy_ios_android_scope_does_not_cover_final_release(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-ios-android-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{record.name}](docs/qa-builds/{record.name})"],
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
                errors = verify_final_release_evidence.verify_final_release_evidence(
                    records_dir,
                    evidence,
                )

        self.assertIn(
            "Final release evidence requires a validated Android signed-build QA record.",
            errors,
        )
        self.assertIn(
            "Final release evidence requires a validated iOS signed-build QA record.",
            errors,
        )

    def test_separate_android_and_ios_records_cover_final_release(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            android = records_dir / "2026-07-02-android-1.0.0+42.md"
            ios = records_dir / "2026-07-02-ios-1.0.0+42.md"
            android.write_text("android", encoding="utf-8")
            ios.write_text("ios", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [
                    f"- [{android.name}](docs/qa-builds/{android.name})",
                    f"- [{ios.name}](docs/qa-builds/{ios.name})",
                ],
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

    def test_separate_android_and_ios_records_must_use_same_version_build(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            android = records_dir / "2026-07-02-android-1.0.0+42.md"
            ios = records_dir / "2026-07-02-ios-1.0.0+43.md"
            android.write_text("android", encoding="utf-8")
            ios.write_text("ios", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [
                    f"- [{android.name}](docs/qa-builds/{android.name})",
                    f"- [{ios.name}](docs/qa-builds/{ios.name})",
                ],
            )

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(android, []),
                    validate_qa_build_records_dir.RecordValidationResult(ios, []),
                ],
            ):
                errors = verify_final_release_evidence.verify_final_release_evidence(
                    records_dir,
                    evidence,
                )

        self.assertIn(
            "Final release evidence records must use the same version/build: 1.0.0+42, 1.0.0+43",
            errors,
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

    def test_android_scope_accepts_android_only_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            android = records_dir / "2026-07-02-android-1.0.0+42.md"
            ios = records_dir / "2026-07-02-ios-1.0.0+43.md"
            android.write_text("android", encoding="utf-8")
            ios.write_text("bad ios", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{android.name}](docs/qa-builds/{android.name})"],
            )

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(android, []),
                    validate_qa_build_records_dir.RecordValidationResult(
                        ios,
                        ["Missing iOS TestFlight evidence"],
                    ),
                ],
            ):
                self.assertEqual(
                    [],
                    verify_final_release_evidence.verify_final_release_evidence(
                        records_dir,
                        evidence,
                        scope="android",
                    ),
                )

    def test_android_scope_accepts_scoped_android_record_content_without_mocking_validation(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            android = records_dir / "2026-07-02-android-1.0.0+42.md"
            android.write_text(
                self._android_scoped_record_with_local_artifact(records_dir),
                encoding="utf-8",
            )
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{android.name}](docs/qa-builds/{android.name})"],
            )

            self.assertEqual(
                [],
                verify_final_release_evidence.verify_final_release_evidence(
                    records_dir,
                    evidence,
                    scope="android",
                ),
            )

    def test_ios_scope_accepts_scoped_ios_record_content_without_mocking_validation(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            ios = records_dir / "2026-07-02-ios-1.0.0+42.md"
            ios.write_text(scoped_record("ios"), encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{ios.name}](docs/qa-builds/{ios.name})"],
            )

            self.assertEqual(
                [],
                verify_final_release_evidence.verify_final_release_evidence(
                    records_dir,
                    evidence,
                    scope="ios",
                ),
            )

    def test_android_scope_rejects_ios_only_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            ios = records_dir / "2026-07-02-ios-1.0.0+42.md"
            ios.write_text("ios", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{ios.name}](docs/qa-builds/{ios.name})"],
            )

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(ios, []),
                ],
            ):
                errors = verify_final_release_evidence.verify_final_release_evidence(
                    records_dir,
                    evidence,
                    scope="android",
                )

        self.assertIn(
            "Final release evidence requires a validated Android signed-build QA record.",
            errors,
        )
        self.assertNotIn(
            "Final release evidence requires a validated iOS signed-build QA record.",
            errors,
        )

    def test_ios_scope_accepts_ios_only_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            ios = records_dir / "2026-07-02-ios-1.0.0+42.md"
            ios.write_text("ios", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{ios.name}](docs/qa-builds/{ios.name})"],
            )

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(ios, []),
                ],
            ):
                self.assertEqual(
                    [],
                    verify_final_release_evidence.verify_final_release_evidence(
                        records_dir,
                        evidence,
                        scope="ios",
                    ),
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
            evidence = self._release_evidence_with_links(records_dir, [])

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
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"QA record filename only: {record.name}"],
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

    def test_record_markdown_link_outside_guarded_block_is_not_enough(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence(
                records_dir,
                "\n".join(
                    [
                        f"- [{record.name}](docs/qa-builds/{record.name})",
                        verify_final_release_evidence.qa_release_evidence_links.QA_LINKS_START,
                        verify_final_release_evidence.qa_release_evidence_links.QA_LINKS_END,
                    ],
                )
                + "\n",
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

    def test_main_failure_prints_copyable_signed_qa_next_action(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            output = StringIO()

            with redirect_stdout(output):
                exit_code = verify_final_release_evidence.main([tmp])

        self.assertEqual(1, exit_code)
        text = output.getvalue()
        self.assertIn("Next action:", text)
        self.assertIn(release_evidence_commands.release_handoff_command(), text)
        self.assertIn("tool/verify_runtime_boundary.py --log", text)
        self.assertIn("tool/run_release_gates.py --log", text)
        self.assertIn("tool/create_qa_build_record.py", text)
        self.assertIn(
            'run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-<version+build>.log',
            text,
        )

    def test_main_link_failure_prints_release_evidence_update_action(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence_with_links(records_dir, [])
            output = StringIO()

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(record, []),
                ],
            ), redirect_stdout(output):
                exit_code = verify_final_release_evidence.main(
                    [str(records_dir), "--release-evidence", str(evidence)],
                )

        self.assertEqual(1, exit_code)
        text = output.getvalue()
        self.assertIn("Next action:", text)
        self.assertIn(
            release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND,
            text,
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_hint(),
            text,
        )
        self.assertEqual(
            [release_evidence_commands.qa_release_evidence_link_hint()],
            verify_final_release_evidence.next_action_hints(
                [
                    "Release evidence document must include Markdown links for every validated QA build record: "
                    + record.name,
                ],
            ),
        )
        self.assertNotIn(
            release_evidence_commands.release_handoff_command(),
            text,
        )
        self.assertNotIn("tool/create_qa_build_record.py", text)

    def test_main_invalid_record_failure_prints_gap_report_action(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("bad record", encoding="utf-8")
            output = StringIO()

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(
                        record,
                        ["Branch"],
                    ),
                ],
            ), redirect_stdout(output):
                exit_code = verify_final_release_evidence.main([str(records_dir)])

        self.assertEqual(1, exit_code)
        text = output.getvalue()
        self.assertIn("Next action:", text)
        self.assertIn(
            release_evidence_commands.qa_build_record_report_hint(str(record)),
            text,
        )
        self.assertEqual(
            [release_evidence_commands.qa_build_record_report_hint(str(record))],
            verify_final_release_evidence.next_action_hints([f"{record}:"]),
        )
        self.assertNotIn(
            release_evidence_commands.release_handoff_command(),
            text,
        )

    def test_main_version_mismatch_points_to_single_version_action(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            android = records_dir / "2026-07-02-android-1.0.0+42.md"
            ios = records_dir / "2026-07-02-ios-1.0.0+43.md"
            android.write_text("android", encoding="utf-8")
            ios.write_text("ios", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [
                    f"- [{android.name}](docs/qa-builds/{android.name})",
                    f"- [{ios.name}](docs/qa-builds/{ios.name})",
                ],
            )
            output = StringIO()

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(android, []),
                    validate_qa_build_records_dir.RecordValidationResult(ios, []),
                ],
            ), redirect_stdout(output):
                exit_code = verify_final_release_evidence.main(
                    [str(records_dir), "--release-evidence", str(evidence)],
                )

        self.assertEqual(1, exit_code)
        text = output.getvalue()
        self.assertIn(
            release_evidence_commands.qa_record_version_mismatch_hint(),
            text,
        )
        self.assertEqual(
            [release_evidence_commands.qa_record_version_mismatch_hint()],
            verify_final_release_evidence.next_action_hints(
                [
                    "Final release evidence records must use the same version/build: 1.0.0+42, 1.0.0+43",
                ],
            ),
        )
        self.assertNotIn(
            release_evidence_commands.release_handoff_command(),
            text,
        )

    def test_main_success_prints_auditable_summary(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{record.name}](docs/qa-builds/{record.name})"],
            )
            output = StringIO()

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(record, []),
                ],
            ), redirect_stdout(output):
                exit_code = verify_final_release_evidence.main(
                    [str(records_dir), "--release-evidence", str(evidence)],
                )

        self.assertEqual(0, exit_code)
        text = output.getvalue()
        self.assertIn("Final MaClaw Mobile release evidence verified.", text)
        self.assertIn("- Version/build: 1.0.0+42", text)
        self.assertIn("- Platform coverage: Android and iOS", text)
        self.assertIn(f"- Release evidence: {evidence.resolve()}", text)
        self.assertIn(f"  - {record.name}", text)

    def test_main_android_scope_success_prints_android_coverage(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{record.name}](docs/qa-builds/{record.name})"],
            )
            output = StringIO()

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(record, []),
                ],
            ), redirect_stdout(output):
                exit_code = verify_final_release_evidence.main(
                    [
                        str(records_dir),
                        "--release-evidence",
                        str(evidence),
                        "--scope",
                        "android",
                    ],
                )

        self.assertEqual(0, exit_code)
        text = output.getvalue()
        self.assertIn("- Verification scope: android", text)
        self.assertIn("- Platform coverage: Android", text)
        self.assertIn(f"  - {record.name}", text)

    def test_main_success_can_write_auditable_log(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{record.name}](docs/qa-builds/{record.name})"],
            )
            log_path = records_dir / "logs" / "final-release-evidence.log"

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(record, []),
                ],
            ), redirect_stdout(StringIO()):
                exit_code = verify_final_release_evidence.main(
                    [
                        str(records_dir),
                        "--release-evidence",
                        str(evidence),
                        "--log",
                        str(log_path),
                    ],
                )

            self.assertEqual(0, exit_code)
            text = log_path.read_text(encoding="utf-8")
            self.assertIn("Final MaClaw Mobile release evidence verified.", text)
            self.assertIn("- Verification scope: android-ios", text)
            self.assertIn(f"- QA records directory: {records_dir.resolve()}", text)
            self.assertIn("- Version/build: 1.0.0+42", text)
            self.assertIn(f"  - {record.name}", text)

    def test_main_failure_can_write_auditable_log(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            log_path = Path(tmp) / "logs" / "final-release-evidence.log"

            with redirect_stdout(StringIO()):
                exit_code = verify_final_release_evidence.main(
                    [tmp, "--log", str(log_path)],
                )

            self.assertEqual(1, exit_code)
            text = log_path.read_text(encoding="utf-8")
            self.assertIn("Final MaClaw Mobile release evidence validation failed:", text)
            self.assertIn("- Verification scope: android-ios", text)
            self.assertIn(f"- QA records directory: {Path(tmp).resolve()}", text)
            self.assertIn(
                f"- Release evidence: {verify_final_release_evidence.default_release_evidence_path()}",
                text,
            )
            self.assertIn("Final release evidence requires at least one", text)
            self.assertIn("Next action:", text)

    def test_main_refuses_to_overwrite_log_without_force(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            log_path = Path(tmp) / "final-release-evidence.log"
            log_path.write_text("existing final evidence", encoding="utf-8")
            stderr = StringIO()

            with redirect_stdout(StringIO()), redirect_stderr(stderr):
                exit_code = verify_final_release_evidence.main(
                    [tmp, "--log", str(log_path)],
                )

            self.assertEqual(1, exit_code)
            self.assertEqual(
                "existing final evidence",
                log_path.read_text(encoding="utf-8"),
            )
            self.assertIn("pass --force to overwrite", stderr.getvalue())

            with redirect_stdout(StringIO()):
                exit_code = verify_final_release_evidence.main(
                    [tmp, "--log", str(log_path), "--force"],
                )

            self.assertEqual(1, exit_code)
            self.assertIn(
                "Final MaClaw Mobile release evidence validation failed:",
                log_path.read_text(encoding="utf-8"),
            )


if __name__ == "__main__":
    unittest.main()
