from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import create_qa_build_record
import release_evidence_commands


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

    def test_default_template_creates_assistant_query_fields(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            target = create_qa_build_record.create_record(
                template_path=create_qa_build_record.default_template_path(),
                records_dir=Path(tmp),
                record_date="2026-07-02",
                scope="android-ios",
                version_build="1.0.0+42",
            )

            text = target.read_text(encoding="utf-8")

            self.assertIn("AI assistant query:", text)
            self.assertIn("Document draft created from assistant result:", text)
            self.assertNotIn("AI search query:", text)
            self.assertNotIn("Document draft created from search:", text)

    def test_create_record_prefills_final_decision_evidence_when_provided(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text(
                "Date: YYYY-MM-DD\n"
                "Version/build number: app version + build number, such as 1.0.0+42\n"
                "Release handoff result:\n"
                "Preflight result:\n"
                "Runtime boundary verification result:\n"
                "Automated release gates result:\n",
                encoding="utf-8",
            )

            target = create_qa_build_record.create_record(
                template_path=template,
                records_dir=records_dir,
                record_date="2026-07-02",
                scope="android-ios",
                version_build="1.0.0+42",
                final_decision_prefills={
                    "Release handoff result": "release_handoff.py output saved to docs/qa-builds/handoff-1.0.0+42.md",
                    "Preflight result": "qa_preflight.py: Result READY for signed-build QA preparation; log: docs/qa-builds/preflight-1.0.0+42.log",
                    "Runtime boundary verification result": "MaClaw Mobile runtime boundary verified: no corelib, phone-local SSH, terminal emulator, phone-side SSH credential, custom Hub URL, redemption-code login, or third-party LLM provider/base URL/API-key regressions; log: docs/qa-builds/runtime-boundary-1.0.0+42.log",
                    "Automated release gates result": "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log",
                },
            )

            text = target.read_text(encoding="utf-8")
            self.assertIn(
                "Release handoff result: release_handoff.py output saved to docs/qa-builds/handoff-1.0.0+42.md",
                text,
            )
            self.assertIn(
                "Preflight result: qa_preflight.py: Result READY for signed-build QA preparation; log: docs/qa-builds/preflight-1.0.0+42.log",
                text,
            )
            self.assertIn(
                "Runtime boundary verification result: MaClaw Mobile runtime boundary verified: no corelib, phone-local SSH, terminal emulator, phone-side SSH credential, custom Hub URL, redemption-code login, or third-party LLM provider/base URL/API-key regressions; log: docs/qa-builds/runtime-boundary-1.0.0+42.log",
                text,
            )
            self.assertIn(
                "Automated release gates result: run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log",
                text,
            )

    def test_create_record_rejects_invalid_final_decision_prefill(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text(
                "Date: YYYY-MM-DD\n"
                "Release handoff result:\n"
                "Preflight result:\n"
                "Runtime boundary verification result:\n"
                "Automated release gates result:\n",
                encoding="utf-8",
            )

            with self.assertRaisesRegex(
                ValueError,
                "invalid Final Release Decision prefill",
            ):
                create_qa_build_record.create_record(
                    template_path=template,
                    records_dir=records_dir,
                    record_date="2026-07-02",
                    scope="android-ios",
                    version_build="1.0.0+42",
                    final_decision_prefills={
                        "Release handoff result": "QA screenshot captured",
                        "Preflight result": "QA screenshot captured",
                        "Runtime boundary verification result": "QA screenshot captured",
                        "Automated release gates result": "QA screenshot captured",
                    },
                )

            self.assertFalse(records_dir.exists())

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

    def test_create_record_removes_out_of_scope_platform_sections(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text(
                "# Template\n\n"
                "## Build Identity\n\n"
                "Date: YYYY-MM-DD\n\n"
                "## Android Signed Build\n\n"
                "Artifact path:\n\n"
                "## iOS Signed Build And Share Extension\n\n"
                "Archive/TestFlight build:\n\n"
                "## Final Release Decision\n\n"
                "Release handoff result:\n",
                encoding="utf-8",
            )

            android_target = create_qa_build_record.create_record(
                template_path=template,
                records_dir=records_dir,
                record_date="2026-07-02",
                scope="android",
                version_build="1.0.0+42",
            )
            ios_target = create_qa_build_record.create_record(
                template_path=template,
                records_dir=records_dir,
                record_date="2026-07-02",
                scope="ios",
                version_build="1.0.0+42",
            )

            android_text = android_target.read_text(encoding="utf-8")
            ios_text = ios_target.read_text(encoding="utf-8")
            self.assertIn("## Android Signed Build", android_text)
            self.assertNotIn("## iOS Signed Build", android_text)
            self.assertNotIn("iOS manual gates passed:", android_text)
            self.assertIn("## iOS Signed Build", ios_text)
            self.assertNotIn("## Android Signed Build", ios_text)
            self.assertNotIn("Android manual gates passed:", ios_text)
            self.assertIn("## Build Identity", android_text)
            self.assertIn("## Final Release Decision", ios_text)

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
            record = records_dir / "2026-07-02-android-1.0.0+42.md"
            self.assertTrue(record.exists())
            self.assertIn(
                "Complete manual evidence before validation: real-device share/permission, "
                "Hub discovery with post-SMS-verification official credits LLM proof including concrete llm-request-id and llm-usage-record, notification, and GUI-equivalent backend-managed SSH session evidence",
                output.getvalue(),
            )
            self.assertIn("post-SMS-verification", output.getvalue())
            self.assertIn("official credits", output.getvalue())
            self.assertIn("llm-request-id", output.getvalue())
            self.assertIn("llm-usage-record", output.getvalue())
            self.assertIn(
                "`ssh_session` realtime `output_chunk`/`output_seq` proof",
                output.getvalue(),
            )
            self.assertIn(
                "GUI/agent claim or worker handoff",
                output.getvalue(),
            )
            self.assertIn("explicit worker claim/update evidence", output.getvalue())
            self.assertIn("GUI/agent-bound backend_session_id", output.getvalue())
            self.assertIn(
                "not phone-local/ad hoc terminal evidence",
                output.getvalue(),
            )
            self.assertIn("phone-initiated interrupt evidence", output.getvalue())
            self.assertIn("Hub control record", output.getvalue())
            self.assertIn(
                "/api/mobile/ssh/sessions/{session_id}/interrupt",
                output.getvalue(),
            )
            self.assertIn("GUI/agent Ctrl+C handling", output.getvalue())
            self.assertIn(
                "copied-output GUI/agent evidence line with actual values for Hub session ID, backend_session_id, concrete claimed_by worker identity such as claimed_by desktop-agent-1, and numeric output_seq",
                output.getvalue(),
            )
            self.assertIn(
                "preserving that evidence line while replacing credentials or private customer excerpts with redacted text or a traceable attachment ID",
                output.getvalue(),
            )
            self.assertIn(
                "AI/digital-employee handoff evidence tied to that same GUI/agent-bound backend_session_id",
                output.getvalue(),
            )
            self.assertIn("Validate after completing evidence", output.getvalue())
            self.assertIn(
                release_evidence_commands.validate_qa_build_record_command(str(record)),
                output.getvalue(),
            )
            self.assertIn(
                release_evidence_commands.qa_build_record_report_command(str(record)),
                output.getvalue(),
            )
            self.assertIn(
                release_evidence_commands.qa_release_evidence_link_command(
                    records_dir=str(records_dir),
                    scope="android",
                ),
                output.getvalue(),
            )

    def test_main_prints_scope_specific_signed_artifact_evidence_hints(self) -> None:
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
                        "android-ios",
                        "--version",
                        "1.0.0+42",
                        "--template",
                        str(template),
                        "--records-dir",
                        str(records_dir),
                    ],
                )

            self.assertEqual(0, exit_code)
            text = output.getvalue()
            self.assertIn(
                release_evidence_commands.android_artifact_evidence_command(
                    "1.0.0+42",
                    record_dir=str(records_dir),
                ),
                text,
            )
            self.assertIn(
                release_evidence_commands.ios_artifact_evidence_command(
                    record_dir=str(records_dir),
                ),
                text,
            )

    def test_main_omits_unneeded_signed_artifact_evidence_hints(self) -> None:
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
            text = output.getvalue()
            self.assertIn(
                release_evidence_commands.android_artifact_evidence_command(
                    "1.0.0+42",
                    record_dir=str(records_dir),
                ),
                text,
            )
            self.assertNotIn(
                release_evidence_commands.ios_artifact_evidence_command(
                    record_dir=str(records_dir),
                ),
                text,
            )

    def test_main_default_records_dir_keeps_short_follow_up_commands(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            default_records_dir = root / "docs" / "qa-builds"
            template.write_text("Date: YYYY-MM-DD\n", encoding="utf-8")
            output = StringIO()
            original_default_records_dir = create_qa_build_record.default_records_dir

            try:
                create_qa_build_record.default_records_dir = lambda: default_records_dir
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
                            str(default_records_dir),
                            "--force",
                        ],
                    )
            finally:
                create_qa_build_record.default_records_dir = original_default_records_dir

            self.assertEqual(0, exit_code)
            text = output.getvalue()
            self.assertIn(
                release_evidence_commands.android_artifact_evidence_command(
                    "1.0.0+42",
                ),
                text,
            )
            self.assertIn(
                release_evidence_commands.qa_release_evidence_link_command(
                    scope="android",
                ),
                text,
            )

    def test_main_accepts_final_decision_prefill_arguments(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text(
                "Date: YYYY-MM-DD\n"
                "Version/build number: app version + build number, such as 1.0.0+42\n"
                "Release handoff result:\n"
                "Preflight result:\n"
                "Runtime boundary verification result:\n"
                "Automated release gates result:\n",
                encoding="utf-8",
            )

            with redirect_stdout(StringIO()):
                exit_code = create_qa_build_record.main(
                    [
                        "--date",
                        "2026-07-02",
                        "--scope",
                        "android-ios",
                        "--version",
                        "1.0.0+42",
                        "--template",
                        str(template),
                        "--records-dir",
                        str(records_dir),
                        "--release-handoff-result",
                        "release_handoff.py output saved to docs/qa-builds/handoff-1.0.0+42.md",
                        "--preflight-result",
                        "qa_preflight.py: Result READY for signed-build QA preparation; log: docs/qa-builds/preflight-1.0.0+42.log",
                        "--runtime-boundary-result",
                        "MaClaw Mobile runtime boundary verified: no corelib, phone-local SSH, terminal emulator, phone-side SSH credential, custom Hub URL, redemption-code login, or third-party LLM provider/base URL/API-key regressions; log: docs/qa-builds/runtime-boundary-1.0.0+42.log",
                        "--automated-gates-result",
                        "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log",
                    ],
                )

            self.assertEqual(0, exit_code)
            text = (records_dir / "2026-07-02-android-ios-1.0.0+42.md").read_text(
                encoding="utf-8",
            )
            self.assertIn(
                "Release handoff result: release_handoff.py output saved to docs/qa-builds/handoff-1.0.0+42.md",
                text,
            )
            self.assertIn(
                "Preflight result: qa_preflight.py: Result READY for signed-build QA preparation; log: docs/qa-builds/preflight-1.0.0+42.log",
                text,
            )
            self.assertIn(
                "Runtime boundary verification result: MaClaw Mobile runtime boundary verified: no corelib, phone-local SSH, terminal emulator, phone-side SSH credential, custom Hub URL, redemption-code login, or third-party LLM provider/base URL/API-key regressions; log: docs/qa-builds/runtime-boundary-1.0.0+42.log",
                text,
            )
            self.assertIn(
                "Automated release gates result: run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log",
                text,
            )

    def test_main_rejects_invalid_final_decision_prefill_without_traceback(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            template = root / "template.md"
            records_dir = root / "qa-builds"
            template.write_text(
                "Date: YYYY-MM-DD\n"
                "Release handoff result:\n",
                encoding="utf-8",
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
                        "--release-handoff-result",
                        "QA screenshot captured",
                    ],
                )

            self.assertEqual(1, exit_code)
            self.assertIn("invalid Final Release Decision prefill", error.getvalue())
            self.assertFalse(records_dir.exists())

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
