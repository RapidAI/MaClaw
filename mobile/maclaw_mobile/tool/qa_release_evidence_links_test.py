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

import qa_release_evidence_links
import release_evidence_commands
from validate_qa_build_record_test import complete_record, scoped_record


class QaReleaseEvidenceLinksTest(unittest.TestCase):
    def records_dir(self, root: Path) -> Path:
        records_dir = root / "qa-builds"
        records_dir.mkdir()
        return records_dir

    def write_record(self, records_dir: Path, text: str, name: str) -> Path:
        path = records_dir / name
        path.write_text(text, encoding="utf-8")
        return path

    def complete_record_with_local_artifact(self, records_dir: Path) -> str:
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
        return complete_record().replace("SHA256: " + "a" * 64, f"SHA256: {digest}")

    def android_scoped_record_with_local_artifact(self, records_dir: Path) -> str:
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

    def test_empty_directory_reports_no_validated_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            stderr = StringIO()

            summary = qa_release_evidence_links.summarize_records(records_dir)
            output = qa_release_evidence_links.format_links(summary)
            with redirect_stderr(stderr):
                exit_code = qa_release_evidence_links.main([str(records_dir)])

            self.assertEqual([], summary.valid_records)
            self.assertIn("No validated QA build records found.", output)
            self.assertEqual(1, exit_code)
            self.assertIn("No validated QA build records found.", stderr.getvalue())
            self.assertIn("Next action:", stderr.getvalue())
            self.assertIn(
                "release handoff is only a QA plan, not a completed QA record",
                stderr.getvalue(),
            )
            self.assertIn(
                release_evidence_commands.create_record_command(),
                stderr.getvalue(),
            )
            self.assertEqual(
                [release_evidence_commands.signed_qa_record_hint()],
                qa_release_evidence_links.release_evidence_update_hints(summary),
            )

    def test_handoff_evidence_files_are_not_linked_as_qa_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            for name in [
                "handoff-1.0.0+42.md",
                "handoff-android-1.0.0+42.md",
                "handoff-ios-1.0.0+42.md",
            ]:
                self.write_record(
                    records_dir,
                    "# Release handoff evidence\n",
                    name,
                )

            summary = qa_release_evidence_links.summarize_records(records_dir)
            output = qa_release_evidence_links.format_links(summary)

            self.assertEqual([], summary.valid_records)
            self.assertEqual([], summary.invalid_records)
            self.assertEqual([], qa_release_evidence_links.link_lines(summary))
            self.assertIn("No validated QA build records found.", output)
            self.assertNotIn("handoff-", output)

    def test_valid_record_formats_release_evidence_link(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-android-ios-1.0.0+42.md",
            )

            summary = qa_release_evidence_links.summarize_records(records_dir)
            output = qa_release_evidence_links.format_links(summary)

            self.assertEqual(1, len(summary.valid_records))
            self.assertEqual(["1.0.0+42"], summary.versions)
            self.assertTrue(summary.has_android)
            self.assertTrue(summary.has_ios)
            self.assertIn("Validated version/build: 1.0.0+42", output)
            self.assertIn("Validated platform coverage: Android and iOS", output)
            self.assertIn(
                "- [2026-07-02-android-ios-1.0.0+42.md](docs/qa-builds/2026-07-02-android-ios-1.0.0+42.md)",
                output,
            )

    def test_invalid_record_is_reported_but_not_linked(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir).replace(
                    "Branch: codex/mobile-release\n",
                    "",
                ),
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
                self.complete_record_with_local_artifact(records_dir).replace(
                    "Branch: codex/mobile-release\n",
                    "",
                ),
                "2026-07-02-android-ios-1.0.0+42.md",
            )
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = qa_release_evidence_links.main([str(records_dir)])
            summary_for_hints = qa_release_evidence_links.summarize_records(records_dir)

            self.assertEqual(1, exit_code)
            text = stderr.getvalue()
            self.assertIn("Records not linked", text)
            self.assertIn("Next action:", text)
            self.assertIn(
                release_evidence_commands.qa_build_record_report_hint(
                    str(records_dir / "2026-07-02-android-ios-1.0.0+42.md"),
                ),
                text,
            )
            self.assertEqual(
                [
                    release_evidence_commands.qa_build_record_report_hint(
                        str(records_dir / "2026-07-02-android-ios-1.0.0+42.md"),
                    ),
                ],
                qa_release_evidence_links.release_evidence_update_hints(
                    qa_release_evidence_links.summarize_records(records_dir),
                ),
            )
            self.assertNotIn("tool/create_qa_build_record.py", text)

    def test_main_rejects_valid_records_missing_ios_platform_coverage(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-android-1.0.0+42.md",
            )
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = qa_release_evidence_links.main([str(records_dir)])
            summary_for_hints = qa_release_evidence_links.summarize_records(records_dir)

        self.assertEqual(1, exit_code)
        self.assertIn(
            "Validated records are missing final release platform coverage: iOS",
            stderr.getvalue(),
        )
        self.assertIn("2026-07-02-android-1.0.0+42.md", stderr.getvalue())
        self.assertIn("Next action:", stderr.getvalue())
        self.assertIn(
            release_evidence_commands.release_handoff_command(scope="ios"),
            stderr.getvalue(),
        )
        self.assertIn(
            release_evidence_commands.signed_qa_record_hint(scope="ios"),
            stderr.getvalue(),
        )
        self.assertEqual(
            [release_evidence_commands.signed_qa_record_hint(scope="ios")],
            qa_release_evidence_links.release_evidence_update_hints(summary_for_hints),
        )
        self.assertNotIn("setup_android_signing.py", stderr.getvalue())
        self.assertNotIn("signed_artifact_evidence.py android", stderr.getvalue())
        self.assertNotIn("After adding these links, run:", stderr.getvalue())
        self.assertIn(
            "Final verifier is deferred until validated records cover Android and iOS.",
            stderr.getvalue(),
        )
        self.assertEqual(1, stderr.getvalue().count("Next action:"))

    def test_legacy_ios_android_scope_does_not_count_as_platform_coverage(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            record = self.write_record(
                records_dir,
                "record",
                "2026-07-02-ios-android-1.0.0+42.md",
            )

            with patch(
                "validate_qa_build_records_dir.validate_directory",
                return_value=[
                    qa_release_evidence_links.validate_qa_build_records_dir.RecordValidationResult(
                        path=record,
                        errors=[],
                    ),
                ],
            ):
                summary = qa_release_evidence_links.summarize_records(records_dir)
                output = qa_release_evidence_links.format_links(summary)

        self.assertEqual([], summary.valid_records)
        self.assertFalse(summary.has_android)
        self.assertFalse(summary.has_ios)
        self.assertIn(
            "No validated QA build records found.",
            output,
        )

    def test_main_rejects_valid_records_with_mismatched_version_builds(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-android-1.0.0+42.md",
            )
            self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir).replace(
                    "Version/build number: 1.0.0+42",
                    "Version/build number: 1.0.0+43",
                ),
                "2026-07-02-ios-1.0.0+43.md",
            )
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = qa_release_evidence_links.main([str(records_dir)])
            summary_for_hints = qa_release_evidence_links.summarize_records(records_dir)

        self.assertEqual(1, exit_code)
        self.assertIn(
            "Validated records use multiple version/build values and must not be linked as one release: 1.0.0+42, 1.0.0+43",
            stderr.getvalue(),
        )
        self.assertIn("2026-07-02-android-1.0.0+42.md", stderr.getvalue())
        self.assertIn("2026-07-02-ios-1.0.0+43.md", stderr.getvalue())
        self.assertIn("Next action:", stderr.getvalue())
        self.assertIn(
            release_evidence_commands.qa_record_version_mismatch_hint(),
            stderr.getvalue(),
        )
        self.assertEqual(
            [release_evidence_commands.qa_record_version_mismatch_hint()],
            qa_release_evidence_links.release_evidence_update_hints(summary_for_hints),
        )
        self.assertNotIn(
            release_evidence_commands.release_handoff_command(),
            stderr.getvalue(),
        )
        self.assertNotIn("After adding these links, run:", stderr.getvalue())
        self.assertIn(
            "Final verifier is deferred until validated QA records use one version/build.",
            stderr.getvalue(),
        )
        self.assertEqual(1, stderr.getvalue().count("Next action:"))

    def test_main_prints_links_for_valid_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-android-ios-1.0.0+42.md",
            )
            stdout = StringIO()

            with redirect_stdout(stdout):
                exit_code = qa_release_evidence_links.main([str(records_dir)])

            self.assertEqual(0, exit_code)
            self.assertIn("Validated records to link", stdout.getvalue())

    def test_valid_output_reminds_operator_to_run_final_verifier(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-android-ios-1.0.0+42.md",
            )

            output = qa_release_evidence_links.format_links(
                qa_release_evidence_links.summarize_records(records_dir),
            )

        self.assertIn(
            "After adding these links, run: "
            + release_evidence_commands.verify_final_release_evidence_command(
                version="1.0.0+42",
            ),
            output,
        )

    def test_main_can_update_release_evidence_link_block(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            records_dir = self.records_dir(root)
            record = self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-android-ios-1.0.0+42.md",
            )
            release_evidence = root / "release_evidence.md"
            release_evidence.write_text(
                "\n".join(
                    [
                        "# Evidence",
                        qa_release_evidence_links.QA_LINKS_START,
                        qa_release_evidence_links.QA_LINKS_END,
                    ],
                )
                + "\n",
                encoding="utf-8",
            )
            stdout = StringIO()

            with redirect_stdout(stdout):
                exit_code = qa_release_evidence_links.main(
                    [
                        str(records_dir),
                        "--update-release-evidence",
                        str(release_evidence),
                    ],
                )

            self.assertEqual(0, exit_code)
            self.assertIn("Updated release evidence links", stdout.getvalue())
            self.assertIn(
                f"- [{record.name}](docs/qa-builds/{record.name})",
                release_evidence.read_text(encoding="utf-8"),
            )

    def test_main_update_release_evidence_requires_markers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            records_dir = self.records_dir(root)
            self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-android-ios-1.0.0+42.md",
            )
            release_evidence = root / "release_evidence.md"
            release_evidence.write_text("# Evidence\n", encoding="utf-8")
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = qa_release_evidence_links.main(
                    [
                        str(records_dir),
                        "--update-release-evidence",
                        str(release_evidence),
                    ],
                )

            self.assertEqual(1, exit_code)
            self.assertIn("Failed to update release evidence", stderr.getvalue())
            self.assertIn("QA_BUILD_RECORD_LINKS_START", stderr.getvalue())

    def test_update_release_evidence_rejects_empty_valid_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            release_evidence = root / "release_evidence.md"
            release_evidence.write_text(
                "\n".join(
                    [
                        "# Evidence",
                        qa_release_evidence_links.QA_LINKS_START,
                        qa_release_evidence_links.QA_LINKS_END,
                    ],
                )
                + "\n",
                encoding="utf-8",
            )
            summary = qa_release_evidence_links.EvidenceLinkSummary(
                records_dir=root / "qa-builds",
                scope=release_evidence_commands.DEFAULT_SCOPE,
                valid_records=[],
                invalid_records=[],
                versions=[],
                has_android=False,
                has_ios=False,
            )

            with self.assertRaisesRegex(ValueError, "at least one validated QA build record"):
                qa_release_evidence_links.update_release_evidence(summary, release_evidence)

            self.assertEqual(
                "# Evidence\n"
                f"{qa_release_evidence_links.QA_LINKS_START}\n"
                f"{qa_release_evidence_links.QA_LINKS_END}\n",
                release_evidence.read_text(encoding="utf-8"),
            )

    def test_update_release_evidence_rejects_missing_platform_coverage(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            records_dir = self.records_dir(root)
            record = self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-android-1.0.0+42.md",
            )
            release_evidence = root / "release_evidence.md"
            release_evidence.write_text(
                "\n".join(
                    [
                        "# Evidence",
                        qa_release_evidence_links.QA_LINKS_START,
                        qa_release_evidence_links.QA_LINKS_END,
                    ],
                )
                + "\n",
                encoding="utf-8",
            )
            summary = qa_release_evidence_links.EvidenceLinkSummary(
                records_dir=records_dir,
                scope=release_evidence_commands.DEFAULT_SCOPE,
                valid_records=[record],
                invalid_records=[],
                versions=["1.0.0+42"],
                has_android=True,
                has_ios=False,
            )

            with self.assertRaisesRegex(ValueError, "missing platform coverage: iOS"):
                qa_release_evidence_links.update_release_evidence(summary, release_evidence)

            self.assertNotIn(record.name, release_evidence.read_text(encoding="utf-8"))

    def test_android_scope_accepts_android_only_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-android-1.0.0+42.md",
            )

            summary = qa_release_evidence_links.summarize_records(
                records_dir,
                scope="android",
            )
            output = qa_release_evidence_links.format_links(summary)

        self.assertTrue(summary.has_android)
        self.assertFalse(summary.has_ios)
        self.assertEqual([], qa_release_evidence_links.release_evidence_update_errors(summary))
        self.assertIn("Verification scope: Android", output)
        self.assertIn("Validated platform coverage: Android", output)
        self.assertIn(
            "After adding these links, run: "
            + release_evidence_commands.verify_final_release_evidence_command(
                scope="android",
                version="1.0.0+42",
            ),
            output,
        )

    def test_android_scope_accepts_scoped_android_record_content(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            record = self.write_record(
                records_dir,
                self.android_scoped_record_with_local_artifact(records_dir),
                "2026-07-02-android-1.0.0+42.md",
            )

            summary = qa_release_evidence_links.summarize_records(
                records_dir,
                scope="android",
            )

        self.assertEqual([record], summary.valid_records)
        self.assertEqual([], summary.invalid_records)

    def test_android_scope_ignores_out_of_scope_ios_record_versions(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            android_record = self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-android-1.0.0+42.md",
            )
            ios_record = self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir).replace(
                    "Version/build number: 1.0.0+42",
                    "Version/build number: 1.0.0+43",
                ),
                "2026-07-02-ios-1.0.0+43.md",
            )
            bad_ios_record = self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir)
                .replace(
                    "Version/build number: 1.0.0+42",
                    "Version/build number: 1.0.0+44",
                )
                .replace("Branch: codex/mobile-release\n", ""),
                "2026-07-03-ios-1.0.0+44.md",
            )

            summary = qa_release_evidence_links.summarize_records(
                records_dir,
                scope="android",
            )
            output = qa_release_evidence_links.format_links(summary)

        self.assertEqual([android_record], summary.valid_records)
        self.assertEqual([], summary.invalid_records)
        self.assertEqual([bad_ios_record], [result.path for result in summary.out_of_scope_invalid_records])
        self.assertEqual(["1.0.0+42"], summary.versions)
        self.assertEqual([], qa_release_evidence_links.release_evidence_update_errors(summary))
        self.assertIn(
            f"- [{android_record.name}](docs/qa-builds/{android_record.name})",
            output,
        )
        self.assertNotIn(
            f"- [{ios_record.name}](docs/qa-builds/{ios_record.name})",
            output,
        )
        self.assertNotIn(
            f"- [{bad_ios_record.name}](docs/qa-builds/{bad_ios_record.name})",
            output,
        )
        self.assertIn(
            "Out-of-scope invalid records ignored for Android links:",
            output,
        )
        self.assertIn(bad_ios_record.name, output)
        self.assertIn("Branch", output)
        self.assertNotIn("Records not linked because validation failed:", output)
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                scope="android",
                version="1.0.0+42",
            ),
            output,
        )

    def test_ios_scope_accepts_ios_only_records(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            records_dir = self.records_dir(Path(tmp))
            self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-ios-1.0.0+42.md",
            )

            summary = qa_release_evidence_links.summarize_records(
                records_dir,
                scope="ios",
            )
            output = qa_release_evidence_links.format_links(summary)

        self.assertFalse(summary.has_android)
        self.assertTrue(summary.has_ios)
        self.assertEqual([], qa_release_evidence_links.release_evidence_update_errors(summary))
        self.assertIn("Verification scope: iOS", output)
        self.assertIn("Validated platform coverage: iOS", output)
        self.assertIn(
            "After adding these links, run: "
            + release_evidence_commands.verify_final_release_evidence_command(
                scope="ios",
                version="1.0.0+42",
            ),
            output,
        )

    def test_android_scope_can_update_release_evidence_with_android_only_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            records_dir = self.records_dir(root)
            record = self.write_record(
                records_dir,
                self.complete_record_with_local_artifact(records_dir),
                "2026-07-02-android-1.0.0+42.md",
            )
            release_evidence = root / "release_evidence.md"
            release_evidence.write_text(
                "\n".join(
                    [
                        "# Evidence",
                        qa_release_evidence_links.QA_LINKS_START,
                        qa_release_evidence_links.QA_LINKS_END,
                    ],
                )
                + "\n",
                encoding="utf-8",
            )
            stdout = StringIO()

            with redirect_stdout(stdout):
                exit_code = qa_release_evidence_links.main(
                    [
                        str(records_dir),
                        "--update-release-evidence",
                        str(release_evidence),
                        "--scope",
                        "android",
                    ],
                )

            self.assertEqual(0, exit_code)
            self.assertIn("Updated release evidence links", stdout.getvalue())
            self.assertIn(
                f"- [{record.name}](docs/qa-builds/{record.name})",
                release_evidence.read_text(encoding="utf-8"),
            )


if __name__ == "__main__":
    unittest.main()
