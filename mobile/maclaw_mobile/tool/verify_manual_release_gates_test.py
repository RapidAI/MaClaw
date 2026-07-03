from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import verify_manual_release_gates


def write_docs(
    root: Path,
    *,
    evidence_table: str,
    audit_blockers: str,
    checklist: str,
    final_decision: str,
) -> None:
    docs = root / "docs"
    docs.mkdir()
    (docs / "release_evidence.md").write_text(
        "# Evidence\n\n## Manual Release Gates\n\n"
        + evidence_table
        + "\n\n## Build Record Template\n",
        encoding="utf-8",
    )
    (docs / "release_audit.md").write_text(
        "# Audit\n\n## Remaining Release Blockers\n\n"
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


def valid_evidence_table() -> str:
    rows = [
        (
            gate.gate,
            f"{gate.gate} QA notes with screenshot/log reference and signed evidence",
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
            "- Android runtime permission prompts for notification, camera, microphone, media/file access, and local network/SSH scenario if applicable.",
            "- iOS signed Runner and Share Extension target with official Team ID, provisioning profile, and app-group entitlement.",
            "- iOS real-device/TestFlight share-to-app for text, URL, image, PDF, Word, Excel, and CSV.",
            "- iOS runtime permission prompts for camera, microphone, speech recognition, photo library, local network, and notifications.",
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
            "## iOS Signing And Share Extension",
            "Share Extension wiring, official Team ID, and App group evidence must be recorded.",
            "## iOS Share-To-App",
            "Plain text, URL, Image/photo, PDF, Word, Excel, and CSV payloads must be shared into MaClaw Mobile.",
            "## iOS Runtime Permissions",
            "Speech recognition and Notification permission prompts must be recorded.",
            "## Hub Discovery And Service Smoke Test",
            "Record selected HubCenter, discovered Hub, tenant, API base URL, and realtime Hub URL evidence.",
            "## Manual SSH Smoke Test",
            "Connect to the QA host, run a read-only command, copy output, and delete credentials.",
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


if __name__ == "__main__":
    unittest.main()
