from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import qa_release_evidence_links
import release_evidence_commands
import verify_manual_release_gates


def write_docs(
    root: Path,
    *,
    evidence_table: str,
    audit_blockers: str,
    checklist: str,
    final_decision: str,
    qa_builds_readme: str | None = None,
) -> None:
    docs = root / "docs"
    docs.mkdir()
    (docs / "qa-builds").mkdir()
    (docs / "release_evidence.md").write_text(
        "# Evidence\n\n## Manual Release Gates\n\n"
        + evidence_table
        + "\n\n"
        + verify_manual_release_gates.FINAL_RELEASE_EVIDENCE_LOG_COMMAND
        + "\n"
        + release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND
        + "\n\n"
        + qa_release_evidence_links.QA_LINKS_START
        + "\n"
        + qa_release_evidence_links.QA_LINKS_END
        + "\n\n## Build Record Template\n",
        encoding="utf-8",
    )
    (docs / "release_audit.md").write_text(
        "# Audit\n\n"
        "Completed signed-build QA records must pass "
        "`python3 tool/validate_qa_build_record.py` without secret redaction "
        "failures before the audit can count them as release evidence. If "
        "validation fails, run `python3 tool/qa_build_record_report.py "
        "docs/qa-builds/<record>.md` and replace raw secrets with redacted "
        "evidence.\n\n"
        "## Remaining Release Blockers\n\n"
        + audit_blockers
        + "\n\nRecord these results with `docs/qa_device_checklist.md`.\n",
        encoding="utf-8",
    )
    (docs / "qa_device_checklist.md").write_text(checklist, encoding="utf-8")
    (docs / "qa_build_record_template.md").write_text(
        "# Template\n\n## Final Release Decision\n\n```text\n"
        + final_decision
        + "\n```\n",
        encoding="utf-8",
    )
    (docs / "qa-builds" / "README.md").write_text(
        qa_builds_readme
        or (
            "# QA Builds\n\n"
            + verify_manual_release_gates.FINAL_RELEASE_EVIDENCE_LOG_COMMAND
            + "\n"
            + release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND
            + "\n"
            + "\n".join(
                verify_manual_release_gates.QA_BUILDS_README_SCOPED_INTERNAL_QA_COMMANDS
            )
            + "\n"
        ),
        encoding="utf-8",
    )


def valid_evidence_table() -> str:
    rows = [
        (
            gate.gate,
            (
                f"{gate.gate} QA notes with screenshot/log reference "
                f"and signed evidence {' '.join(gate.evidence_keywords)}"
            ).strip(),
        )
        for gate in verify_manual_release_gates.CANONICAL_MANUAL_GATES
    ]
    body = "\n".join(f"| {gate} | {required} |" for gate, required in rows)
    return "| Gate | Required evidence |\n| --- | --- |\n" + body


def valid_audit_blockers() -> str:
    return "\n".join(
        [
            "- Signed Android internal APK/AAB with install result on at least one Android 13+ device.",
            "- Android real-device share-to-app for text, URL, image, PDF, Word, Excel, and CSV.",
            "- Android runtime permission prompts for notification, camera, microphone, media/file access, and local network/SSH scenario if applicable, with permission-grant:<id> evidence.",
            "- iOS signed Runner and Share Extension target with official Team ID, provisioning profile, and app-group entitlement.",
            "- iOS real-device/TestFlight share-to-app for text, URL, image, PDF, Word, Excel, and CSV.",
            "- iOS runtime permission prompts for camera, microphone, speech recognition, photo library, local network, and notifications, with permission-grant:<id> evidence.",
            "- Real SSH maintenance smoke test against a server, including host type, auth mode, connect result, read-only command, command output excerpt, disconnect result, reconnect result, copied output evidence, AI analysis confirmation, and credential deletion confirmation.",
            "- Hub discovery smoke test with account, selected HubCenter, discovered Hub, tenant, LLM mode/QR authorization evidence, bootstrap, AI search with citations, voice transcription, photo/image assistant input, shared result, document draft, document upload/export, digital employee task, realtime status, notification delivery, network offline/recovery, API base URL, and realtime Hub URL confirmation.",
        ]
    )


def valid_final_decision() -> str:
    return "\n".join(
        [
            "Automated gates passed: passed / waived with reason",
            "Android manual gates passed: passed / waived with reason",
            "iOS manual gates passed: passed / waived with reason",
            "Hub discovery smoke passed: passed / waived with reason",
            "Manual SSH smoke passed: passed / waived with reason",
        ]
    )


def valid_checklist() -> str:
    return "\n".join(
        [
            "# MaClaw Mobile Device QA Checklist",
            "## Android Signed Build",
            "Install the signed APK/AAB on at least one Android 13+ device and record the install result.",
            "## Android Share-To-App",
            "Plain text, URL, Image/photo, PDF, Word, Excel, and CSV payloads must be shared into MaClaw Mobile.",
            "## Android Runtime Permissions",
            "Notification permission and Camera permission prompts must be recorded.",
            "Every permission prompt/result record must include permission-grant:<id>.",
            "## iOS Signing And Share Extension",
            "Share Extension wiring, official Team ID, and App group evidence must be recorded.",
            "## iOS Share-To-App",
            "Plain text, URL, Image/photo, PDF, Word, Excel, and CSV payloads must be shared into MaClaw Mobile.",
            "## iOS Runtime Permissions",
            "Speech recognition and Notification permission prompts must be recorded.",
            "Every permission prompt/result record must include permission-grant:<id>.",
            "## Hub Discovery And Service Smoke Test",
            "Record selected HubCenter, discovered Hub, tenant, API base URL, and realtime Hub URL evidence.",
            "Record typed notification payloads for document-export:, digital-employee-task:, and server-profile: targets.",
            "## Manual SSH Smoke Test",
            "Connect to the QA host, run a read-only command, copy output, and delete credentials.",
            verify_manual_release_gates.FINAL_RELEASE_EVIDENCE_LOG_COMMAND,
            release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND,
            *verify_manual_release_gates.SCOPED_INTERNAL_QA_COMMANDS,
        ]
    )


class VerifyManualReleaseGatesTest(unittest.TestCase):
    def test_current_release_docs_have_manual_gate_parity(self) -> None:
        self.assertEqual(
            [],
            verify_manual_release_gates.validate_manual_release_gates(
                verify_manual_release_gates.mobile_root()
            ),
        )

    def test_accepts_complete_manual_gate_docs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=valid_audit_blockers(),
                checklist=valid_checklist(),
                final_decision=valid_final_decision(),
            )

            self.assertEqual(
                [],
                verify_manual_release_gates.validate_manual_release_gates(root),
            )

    def test_rejects_missing_evidence_gate(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            table = "\n".join(
                line
                for line in valid_evidence_table().splitlines()
                if not line.startswith("| Hub discovery smoke test |")
            )
            write_docs(
                root,
                evidence_table=table,
                audit_blockers=valid_audit_blockers(),
                checklist=valid_checklist(),
                final_decision=valid_final_decision(),
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        self.assertIn("canonical gates in order", "\n".join(errors))

    def test_rejects_permission_gates_without_permission_grant_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            table = valid_evidence_table().replace(" permission-grant:<id>", "", 1)
            write_docs(
                root,
                evidence_table=table,
                audit_blockers=valid_audit_blockers(),
                checklist=valid_checklist(),
                final_decision=valid_final_decision(),
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        self.assertIn(
            "Manual Release Gates evidence for Android runtime permissions must include permission-grant:<id>.",
            "\n".join(errors),
        )

    def test_rejects_missing_audit_blocker(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            blockers = valid_audit_blockers().replace(
                "- Real SSH maintenance smoke test against a server, including host type, auth mode, connect result, read-only command, command output excerpt, disconnect result, reconnect result, copied output evidence, AI analysis confirmation, and credential deletion confirmation.\n",
                "",
            )
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=blockers,
                checklist=valid_checklist(),
                final_decision=valid_final_decision(),
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        self.assertIn(
            "release_audit.md Remaining Release Blockers must cover Manual SSH",
            "\n".join(errors),
        )

    def test_rejects_permission_blockers_without_permission_grant_id(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            blockers = valid_audit_blockers().replace(
                ", with permission-grant:<id> evidence",
                "",
                1,
            )
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=blockers,
                checklist=valid_checklist(),
                final_decision=valid_final_decision(),
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        self.assertIn(
            "release_audit.md Remaining Release Blockers must cover Android runtime permissions.",
            "\n".join(errors),
        )

    def test_rejects_missing_audit_qa_record_validation_rule(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=valid_audit_blockers(),
                checklist=valid_checklist(),
                final_decision=valid_final_decision(),
            )
            audit = root / "docs" / "release_audit.md"
            audit.write_text(
                audit.read_text(encoding="utf-8").replace(
                    "without secret redaction failures",
                    "after manual review",
                ),
                encoding="utf-8",
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        self.assertIn(
            "release_audit.md must require completed signed-build QA records",
            "\n".join(errors),
        )

    def test_rejects_missing_qa_checklist_steps(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            checklist = valid_checklist().replace(
                "## iOS Share-To-App\nPlain text, URL, Image/photo, PDF, Word, Excel, and CSV payloads must be shared into MaClaw Mobile.\n",
                "",
            )
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=valid_audit_blockers(),
                checklist=checklist,
                final_decision=valid_final_decision(),
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        self.assertIn(
            "qa_device_checklist.md must include executable QA steps for iOS share-to-app",
            "\n".join(errors),
        )

    def test_rejects_missing_typed_notification_payload_checklist(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            checklist = valid_checklist().replace(
                "Record typed notification payloads for document-export:, digital-employee-task:, and server-profile: targets.\n",
                "",
            )
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=valid_audit_blockers(),
                checklist=checklist,
                final_decision=valid_final_decision(),
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        self.assertIn(
            "qa_device_checklist.md must include executable QA steps for Hub discovery smoke test",
            "\n".join(errors),
        )

    def test_rejects_permission_checklist_without_permission_grant_id(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            checklist = valid_checklist().replace(
                "Every permission prompt/result record must include permission-grant:<id>.\n",
                "",
                1,
            )
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=valid_audit_blockers(),
                checklist=checklist,
                final_decision=valid_final_decision(),
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        self.assertIn(
            "qa_device_checklist.md must include executable QA steps for Android runtime permissions.",
            "\n".join(errors),
        )

    def test_rejects_missing_or_wrong_scoped_internal_qa_commands(self) -> None:
        android_handoff = release_evidence_commands.release_handoff_command(
            version=release_evidence_commands.DEFAULT_VERSION,
            scope="android",
        )
        readme_android_handoff = release_evidence_commands.release_handoff_command(
            version="1.0.0+42",
            scope="android",
        )
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=valid_audit_blockers(),
                checklist=valid_checklist().replace(android_handoff, ""),
                final_decision=valid_final_decision(),
                qa_builds_readme=(
                    "# QA Builds\n\n"
                    + verify_manual_release_gates.FINAL_RELEASE_EVIDENCE_LOG_COMMAND
                    + "\n"
                    + release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND
                    + "\n"
                    + "\n".join(
                        verify_manual_release_gates.QA_BUILDS_README_SCOPED_INTERNAL_QA_COMMANDS
                    )
                    + "\n"
                ).replace(
                    readme_android_handoff,
                    readme_android_handoff.replace(
                        " --output ",
                        " --team-id <APPLE_TEAM_ID> --output ",
                    ),
                ),
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        joined = "\n".join(errors)
        self.assertIn(
            "qa_device_checklist.md must document scoped internal QA command",
            joined,
        )
        self.assertIn(
            "qa-builds/README.md must not show Android-only handoff with Apple Team ID options",
            joined,
        )

    def test_rejects_missing_final_decision_field(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            final_decision = valid_final_decision().replace(
                "Hub discovery smoke passed: passed / waived with reason\n",
                "",
            )
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=valid_audit_blockers(),
                checklist=valid_checklist(),
                final_decision=final_decision,
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

            self.assertIn(
                "Hub discovery smoke passed: passed / waived with reason",
                "\n".join(errors),
            )

    def test_rejects_missing_final_release_evidence_log_command(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=valid_audit_blockers(),
                checklist=valid_checklist().replace(
                    verify_manual_release_gates.FINAL_RELEASE_EVIDENCE_LOG_COMMAND,
                    "python3 tool/verify_final_release_evidence.py docs/qa-builds",
                ),
                final_decision=valid_final_decision(),
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        self.assertIn(
            "qa_device_checklist.md must include final release evidence verifier log command",
            "\n".join(errors),
        )

    def test_rejects_missing_qa_release_evidence_link_command(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=valid_audit_blockers(),
                checklist=valid_checklist().replace(
                    release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND,
                    "python3 tool/qa_release_evidence_links.py docs/qa-builds",
                ),
                final_decision=valid_final_decision(),
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        self.assertIn(
            "qa_device_checklist.md must include QA release evidence link update command",
            "\n".join(errors),
        )

    def test_rejects_missing_guarded_qa_record_link_block(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_docs(
                root,
                evidence_table=valid_evidence_table(),
                audit_blockers=valid_audit_blockers(),
                checklist=valid_checklist(),
                final_decision=valid_final_decision(),
            )
            evidence = root / "docs" / "release_evidence.md"
            evidence.write_text(
                evidence.read_text(encoding="utf-8").replace(
                    qa_release_evidence_links.QA_LINKS_START
                    + "\n"
                    + qa_release_evidence_links.QA_LINKS_END,
                    "",
                ),
                encoding="utf-8",
            )

            errors = verify_manual_release_gates.validate_manual_release_gates(root)

        self.assertIn(
            "release_evidence.md must contain the guarded QA build record link block markers",
            "\n".join(errors),
        )


if __name__ == "__main__":
    unittest.main()
