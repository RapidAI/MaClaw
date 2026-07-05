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
        normalized_links = []
        for link in links:
            normalized = link
            for record in records_dir.glob("*.md"):
                normalized = normalized.replace(
                    f"](docs/qa-builds/{record.name})",
                    "]("
                    + verify_final_release_evidence.qa_release_evidence_links.record_link_target(
                        record,
                        records_dir,
                    )
                    + ")",
                )
            normalized_links.append(normalized)
        return self._release_evidence(
            records_dir,
            "\n".join(
                [
                    "# Evidence",
                    "Background mention outside the guarded block is ignored.",
                    verify_final_release_evidence.qa_release_evidence_links.QA_LINKS_START,
                    *normalized_links,
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

    def test_custom_record_directory_requires_actual_release_evidence_link(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp) / "custom-records"
            records_dir.mkdir()
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            correct_link = (
                f"- [{record.name}]("
                + verify_final_release_evidence.qa_release_evidence_links.record_link_target(
                    record,
                    records_dir,
                )
                + ")"
            )
            correct_evidence = self._release_evidence_with_links(
                records_dir,
                [correct_link],
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
                        correct_evidence,
                    ),
                )
                wrong_evidence = self._release_evidence(
                    records_dir,
                    "\n".join(
                        [
                            "# Evidence",
                            verify_final_release_evidence.qa_release_evidence_links.QA_LINKS_START,
                            f"- [{record.name}](docs/qa-builds/{record.name})",
                            verify_final_release_evidence.qa_release_evidence_links.QA_LINKS_END,
                        ],
                    )
                    + "\n",
                )
                self.assertIn(
                    "Release evidence document must include Markdown links",
                    "\n".join(
                        verify_final_release_evidence.verify_final_release_evidence(
                            records_dir,
                            wrong_evidence,
                        ),
                    ),
                )

    def test_custom_qa_builds_directory_requires_actual_release_evidence_link(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp) / "tmp" / "qa-builds"
            records_dir.mkdir(parents=True)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            correct_target = (
                verify_final_release_evidence.qa_release_evidence_links.record_link_target(
                    record,
                    records_dir,
                )
            )
            correct_evidence = self._release_evidence(
                records_dir,
                "\n".join(
                    [
                        "# Evidence",
                        verify_final_release_evidence.qa_release_evidence_links.QA_LINKS_START,
                        f"- [{record.name}]({correct_target})",
                        verify_final_release_evidence.qa_release_evidence_links.QA_LINKS_END,
                    ],
                )
                + "\n",
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
                        correct_evidence,
                    ),
                )
                wrong_evidence = self._release_evidence(
                    records_dir,
                    "\n".join(
                        [
                            "# Evidence",
                            verify_final_release_evidence.qa_release_evidence_links.QA_LINKS_START,
                            f"- [{record.name}](docs/qa-builds/{record.name})",
                            verify_final_release_evidence.qa_release_evidence_links.QA_LINKS_END,
                        ],
                    )
                    + "\n",
                )
                joined_errors = "\n".join(
                    verify_final_release_evidence.verify_final_release_evidence(
                        records_dir,
                        wrong_evidence,
                    ),
                )

            self.assertIn(
                "Release evidence document must include Markdown links",
                joined_errors,
            )
            self.assertIn(record.name, joined_errors)

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

    def test_main_missing_ios_points_to_same_version_ios_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            android = records_dir / "2026-07-02-android-1.0.0+42.md"
            android.write_text("android", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{android.name}](docs/qa-builds/{android.name})"],
            )
            output = StringIO()

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(android, []),
                ],
            ), redirect_stdout(output):
                exit_code = verify_final_release_evidence.main(
                    [str(records_dir), "--release-evidence", str(evidence)],
                )

        text = output.getvalue()
        self.assertEqual(1, exit_code)
        self.assertIn(
            release_evidence_commands.signed_qa_record_hint(
                scope="ios",
                version="1.0.0+42",
                records_dir=str(records_dir.resolve()),
            ),
            text,
        )
        self.assertNotIn("setup_android_signing.py", text)
        self.assertNotIn("signed_artifact_evidence.py android", text)

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

    def test_android_scope_requires_record_when_only_out_of_scope_invalid_records_exist(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            ios = records_dir / "2026-07-02-ios-1.0.0+42.md"
            ios.write_text("bad ios", encoding="utf-8")

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(
                        ios,
                        ["Missing iOS TestFlight evidence"],
                    ),
                ],
            ):
                errors = verify_final_release_evidence.verify_final_release_evidence(
                    records_dir,
                    scope="android",
                )

        self.assertEqual(
            [
                "Final release evidence requires at least one completed Android signed-build QA record.",
            ],
            errors,
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

    def test_record_link_label_must_include_validated_record_filename(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [Completed QA record](docs/qa-builds/{record.name})"],
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
            "Release evidence document QA build record links must use labels containing the validated record filename: "
            "2026-07-02-android-ios-1.0.0+42.md",
            errors,
        )

    def test_guarded_block_rejects_stale_or_unvalidated_qa_record_links(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            stale_record = records_dir / "2026-07-02-android-ios-1.0.0+41.md"
            record.write_text("record", encoding="utf-8")
            stale_record.write_text("stale", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [
                    f"- [{record.name}](docs/qa-builds/{record.name})",
                    f"- [{stale_record.name}](docs/qa-builds/{stale_record.name})",
                ],
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

        joined_errors = "\n".join(errors)
        self.assertIn(
            "Release evidence document guarded QA build record link block must not include stale or unvalidated QA record links:",
            joined_errors,
        )
        self.assertIn(
            "2026-07-02-android-ios-1.0.0+41.md",
            joined_errors,
        )

    def test_guarded_block_reports_missing_and_stale_links_together(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            android = records_dir / "2026-07-02-android-1.0.0+42.md"
            ios = records_dir / "2026-07-02-ios-1.0.0+42.md"
            stale_record = records_dir / "2026-07-02-android-ios-1.0.0+41.md"
            android.write_text("android", encoding="utf-8")
            ios.write_text("ios", encoding="utf-8")
            stale_record.write_text("stale", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [
                    f"- [{android.name}](docs/qa-builds/{android.name})",
                    f"- [{stale_record.name}](docs/qa-builds/{stale_record.name})",
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

        joined_errors = "\n".join(errors)
        self.assertIn(
            "Release evidence document must include Markdown links for every validated QA build record: "
            "2026-07-02-ios-1.0.0+42.md",
            errors,
        )
        self.assertIn(
            "Release evidence document guarded QA build record link block must not include stale or unvalidated QA record links:",
            joined_errors,
        )
        self.assertIn(
            "2026-07-02-android-ios-1.0.0+41.md",
            joined_errors,
        )

    def test_guarded_block_allows_non_qa_markdown_links(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [
                    f"- [{record.name}](docs/qa-builds/{record.name})",
                    "- [QA notes](docs/qa-builds/notes.md)",
                ],
            )

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    validate_qa_build_records_dir.RecordValidationResult(record, []),
                ],
            ):
                self.assertEqual(
                    [],
                    verify_final_release_evidence.verify_final_release_evidence(
                        records_dir,
                        evidence,
                    ),
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
            records_dir = Path(tmp)
            output = StringIO()

            with redirect_stdout(output):
                exit_code = verify_final_release_evidence.main([tmp])

        self.assertEqual(1, exit_code)
        text = output.getvalue()
        self.assertIn("Next action:", text)
        self.assertIn(
            release_evidence_commands.release_handoff_command(
                records_dir=str(records_dir.resolve()),
            ),
            text,
        )
        self.assertIn("tool/verify_runtime_boundary.py --log", text)
        self.assertIn("tool/run_release_gates.py --log", text)
        self.assertIn("tool/create_qa_build_record.py", text)
        self.assertIn(
            f"run_release_gates.py: 38 gates passed; log: {records_dir.resolve()}/release-gates-<version+build>.log",
            text,
        )

    def test_main_failure_uses_custom_records_dir_in_next_action(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp) / "custom-records"
            records_dir.mkdir()
            output = StringIO()

            with redirect_stdout(output):
                exit_code = verify_final_release_evidence.main([str(records_dir)])

        self.assertEqual(1, exit_code)
        text = output.getvalue()
        self.assertIn(
            release_evidence_commands.create_record_command(
                records_dir=str(records_dir.resolve()),
            ),
            text,
        )
        self.assertIn(
            release_evidence_commands.qa_preflight_command(
                team_id=release_evidence_commands.DEFAULT_TEAM_ID,
                export_method=release_evidence_commands.DEFAULT_EXPORT_METHOD,
                records_dir=str(records_dir.resolve()),
            ),
            text,
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
            text,
        )
        self.assertNotIn("--records-dir docs/qa-builds", text)

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
            release_evidence_commands.qa_release_evidence_link_command(
                records_dir=str(records_dir.resolve()),
            ),
            text,
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_hint(
                records_dir=str(records_dir.resolve()),
                version="1.0.0+42",
            ),
            text,
        )
        self.assertEqual(
            [
                release_evidence_commands.qa_release_evidence_link_hint(
                    records_dir=str(records_dir.resolve()),
                    version="1.0.0+42",
                ),
            ],
            verify_final_release_evidence.next_action_hints(
                [
                    "Release evidence document must include Markdown links for every validated QA build record: "
                    + record.name,
                ],
                records_dir=str(records_dir.resolve()),
            ),
        )
        self.assertNotIn(
            release_evidence_commands.release_handoff_command(),
            text,
        )
        self.assertNotIn("tool/create_qa_build_record.py", text)

    def test_link_failure_uses_custom_records_dir_in_next_action(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp) / "custom-records"
            records_dir.mkdir()
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
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_hint(
                records_dir=str(records_dir.resolve()),
                version="1.0.0+42",
            ),
            text,
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(
                records_dir=str(records_dir.resolve()),
            ),
            text,
        )

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
            release_evidence_commands.qa_record_version_mismatch_hint(
                records_dir=str(records_dir.resolve()),
            ),
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
            log_path = records_dir / "logs" / "final-release-evidence-1.0.0+42.log"

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

    def test_main_success_rejects_log_filename_with_wrong_version(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-ios-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{record.name}](docs/qa-builds/{record.name})"],
            )
            log_path = records_dir / "logs" / "final-release-evidence-1.0.0+41.log"
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
                        "--log",
                        str(log_path),
                    ],
                )

            self.assertEqual(1, exit_code)
            self.assertIn(
                "Final release evidence log filename must match the validated version/build: expected final-release-evidence-1.0.0+42.log",
                output.getvalue(),
            )
            self.assertIn(
                release_evidence_commands.verify_final_release_evidence_command(
                    str(records_dir.resolve()),
                    log=(records_dir.resolve() / "final-release-evidence-1.0.0+42.log").as_posix(),
                ),
                output.getvalue(),
            )
            self.assertNotIn(
                release_evidence_commands.create_record_command(),
                output.getvalue(),
            )
            self.assertFalse(log_path.exists())

    def test_main_android_scope_success_requires_scope_qualified_log_name(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = Path(tmp)
            record = records_dir / "2026-07-02-android-1.0.0+42.md"
            record.write_text("record", encoding="utf-8")
            evidence = self._release_evidence_with_links(
                records_dir,
                [f"- [{record.name}](docs/qa-builds/{record.name})"],
            )
            log_path = records_dir / "logs" / "final-release-evidence-android-1.0.0+42.log"

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
                        "--scope",
                        "android",
                        "--log",
                        str(log_path),
                    ],
                )

            self.assertEqual(0, exit_code)
            self.assertIn(
                "- Verification scope: android",
                log_path.read_text(encoding="utf-8"),
            )

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
