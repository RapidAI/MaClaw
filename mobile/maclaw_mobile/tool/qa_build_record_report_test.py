from __future__ import annotations

import hashlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import qa_build_record_report
import release_evidence_commands
import validate_qa_build_record
from validate_qa_build_record_test import complete_record, scoped_record


REQUIRED_EVIDENCE_COUNT = len(validate_qa_build_record.REQUIRED_FIELDS)


class QaBuildRecordReportTest(unittest.TestCase):
    def write_record(self, root: Path, text: str, name: str | None = None) -> Path:
        records_dir = root / "qa-builds"
        records_dir.mkdir(exist_ok=True)
        record = records_dir / (name or "2026-07-02-android-ios-1.0.0+42.md")
        record.write_text(text, encoding="utf-8")
        return record

    def complete_record_with_local_artifact(self, root: Path) -> str:
        artifact = (
            root
            / "qa-builds"
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

    def android_scoped_record_with_local_artifact(self, root: Path) -> str:
        artifact = (
            root
            / "qa-builds"
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

    def test_complete_record_reports_pass(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            record = self.write_record(root, self.complete_record_with_local_artifact(root))

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertTrue(report.passed)
            self.assertIn("Status: PASS", output)
            self.assertIn(
                f"Required evidence: {REQUIRED_EVIDENCE_COUNT}/{REQUIRED_EVIDENCE_COUNT} occurrences filled",
                output,
            )
            self.assertIn("No gaps found", output)
            self.assertIn(
                release_evidence_commands.qa_release_evidence_link_command(
                    records_dir=str(record.parent),
                ),
                output,
            )
            self.assertIn(
                release_evidence_commands.verify_final_release_evidence_command(
                    str(record.parent),
                    version="1.0.0+42",
                    log=release_evidence_commands.final_release_evidence_log_path(
                        "1.0.0+42",
                        records_dir=str(record.parent),
                    ),
                ),
                output,
            )

    def test_scoped_ios_record_report_does_not_request_android_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            record = self.write_record(
                root,
                scoped_record("ios"),
                name="2026-07-02-ios-1.0.0+42.md",
            )

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertTrue(report.passed)
            self.assertIn("Status: PASS", output)
            self.assertNotIn(
                release_evidence_commands.android_artifact_evidence_command("1.0.0+42"),
                output,
            )
            self.assertNotIn("Artifact path", output)

    def test_scoped_android_record_report_uses_scoped_next_action(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            record = self.write_record(
                root,
                self.android_scoped_record_with_local_artifact(root),
                name="2026-07-02-android-1.0.0+42.md",
            )

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertTrue(report.passed)
            self.assertIn(
                release_evidence_commands.qa_release_evidence_link_command(
                    scope="android",
                    records_dir=str(record.parent),
                ),
                output,
            )
            self.assertIn(
                release_evidence_commands.verify_final_release_evidence_command(
                    str(record.parent),
                    scope="android",
                    version="1.0.0+42",
                    log=release_evidence_commands.final_release_evidence_log_path(
                        "1.0.0+42",
                        scope="android",
                        records_dir=str(record.parent),
                    ),
                ),
                output,
            )
            self.assertNotIn("iOS manual gates passed", output)

    def test_scoped_android_report_uses_scoped_handoff_hint(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = self.android_scoped_record_with_local_artifact(root)
            text = "\n".join(
                line
                for line in text.splitlines()
                if not line.startswith("Release handoff result:")
            )
            record = self.write_record(
                root,
                text,
                name="2026-07-02-android-1.0.0+42.md",
            )

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn(
                release_evidence_commands.release_handoff_command(
                    version="1.0.0+42",
                    scope="android",
                    records_dir=str(record.parent),
                ),
                output,
            )
            self.assertNotIn(
                release_evidence_commands.release_handoff_command(
                    version="1.0.0+42",
                    scope="android-ios",
                ),
                output,
            )

    def test_report_groups_missing_and_invalid_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = self.complete_record_with_local_artifact(root)
            text = text.replace("Branch: codex/mobile-release\n", "")
            text = text.replace(
                "Selected HubCenter URL: https://hubs.maclaw.top",
                "Selected HubCenter URL: https://custom.example.test",
            )
            record = self.write_record(root, text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("Status: FAIL", output)
            self.assertIn(
                f"Required evidence: {REQUIRED_EVIDENCE_COUNT - 1}/{REQUIRED_EVIDENCE_COUNT} occurrences filled",
                output,
            )
            self.assertIn("Evidence fields and values:", output)
            self.assertIn("- Branch", output)
            self.assertIn(
                "- Selected HubCenter URL must be one of the preset official HubCenters",
                output,
            )

    def test_report_calls_out_automated_gate_evidence_gaps(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = self.complete_record_with_local_artifact(root)
            for field in (
                "Release handoff result",
                "Runtime boundary verification result",
                "Automated release gates result",
            ):
                text = "\n".join(
                    line for line in text.splitlines() if not line.startswith(f"{field}:")
                )
            record = self.write_record(root, text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn(
                f"Required evidence: {REQUIRED_EVIDENCE_COUNT - 3}/{REQUIRED_EVIDENCE_COUNT} occurrences filled",
                output,
            )
            self.assertIn("Evidence fields and values:", output)
            self.assertIn("- Release handoff result", output)
            self.assertIn("- Runtime boundary verification result", output)
            self.assertIn("- Automated release gates result", output)
            self.assertIn("How to fill release decision evidence:", output)
            self.assertIn(
                "- Release handoff result: Use `release_handoff.py output saved to "
                + str(record.parent)
                + "/handoff-1.0.0+42.md`",
                output,
            )
            self.assertIn(
                "- Runtime boundary verification result: Use `MaClaw Mobile runtime boundary verified. log: "
                + str(record.parent)
                + "/runtime-boundary-1.0.0+42.log`",
                output,
            )
            self.assertIn(
                "- Automated release gates result: Use `run_release_gates.py: 38 gates passed; log: "
                + str(record.parent)
                + "/release-gates-1.0.0+42.log`",
                output,
            )
            self.assertIn(
                release_evidence_commands.release_handoff_command(
                    version="1.0.0+42",
                    records_dir=str(record.parent),
                ),
                output,
            )

    def test_report_points_signed_artifact_gaps_to_evidence_helpers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = self.complete_record_with_local_artifact(root)
            for field in (
                "Artifact path",
                "SHA256",
                "Archive/TestFlight build",
                "Provisioning profiles",
            ):
                text = "\n".join(
                    line for line in text.splitlines() if not line.startswith(f"{field}:")
                )
            record = self.write_record(root, text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("How to fill signed artifact evidence:", output)
            self.assertIn(
                release_evidence_commands.android_artifact_evidence_command(
                    "1.0.0+42",
                    record_dir=str(record.parent),
                ),
                output,
            )
            self.assertIn(
                release_evidence_commands.ios_artifact_evidence_command(
                    record_dir=str(record.parent),
                ),
                output,
            )

    def test_report_points_signed_install_gaps_to_visual_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = self.complete_record_with_local_artifact(root)
            text = text.replace(
                "Android signed install result: Signed build installed and launched for Android signed install result; screenshot install-launch-android-signed-install-result-42",
                "Android signed install result: Signed build installed and launched on Android Pixel device",
            )
            record = self.write_record(root, text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("How to fill signed install evidence:", output)
            self.assertIn(
                "add QA-device evidence that the signed build installed and opened",
                output,
            )
            self.assertIn("screenshot install-launch-android-42", output)

    def test_report_points_voice_photo_gaps_to_assistant_input_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = self.complete_record_with_local_artifact(root)
            text = text.replace(
                "Voice/photo assistant input evidence: Voice input recognized transcript for assistant question and filled the assistant composer before being sent to AI助手; photo/image assistant input produced assistant citation answer with source https://example.test/source-a; screenshot mobile-input-42",
                "Voice/photo assistant input evidence: Voice and photo input tested on phone",
            )
            record = self.write_record(root, text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("How to fill AI助手 voice/photo evidence:", output)
            self.assertIn("recognized voice transcript", output)
            self.assertIn("AI助手 composer", output)
            self.assertIn("photo/image/screenshot assistant input", output)
            self.assertIn("citation URL or document upload task ID", output)
            self.assertIn("screenshot mobile-input-42", output)

    def test_report_points_permission_and_share_gaps_to_qa_flows(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = self.complete_record_with_local_artifact(root)
            text = text.replace(
                "Camera permission: Android Camera permission prompt granted while capturing photo/image assistant input for the mobile AI question; screenshot permission-camera-assistant-input; permission-grant:camera-android-12345",
                "Camera permission: Android Camera permission granted in device Settings",
            )
            text = text.replace(
                "URL: Android Assistant opened shared URL with visible citation for URL; screenshot share-url-url-android",
                "URL: Android URL QA evidence captured in notes",
            )
            record = self.write_record(root, text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("How to fill runtime permission evidence:", output)
            self.assertIn("permission-grant:<id>", output)
            self.assertIn("AI助手 voice/photo input", output)
            self.assertIn("real task notification open", output)
            self.assertIn("real SSH read-only command", output)
            self.assertIn("How to fill share-to-app evidence:", output)
            self.assertIn("OS share sheet", output)
            self.assertIn("URL/citation evidence", output)
            self.assertIn("document import/upload task evidence", output)

    def test_report_points_task_chain_gaps_to_correlated_ids(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = self.complete_record_with_local_artifact(root)
            text = text.replace(
                "Status polling result: Status polling result for document upload task document-upload-task-id-12345 returned parsed draft, and document export job pdf-export-job-id-12345 returned ready, and digital employee task digital-employee-task-id-12345 returned done with result message; screenshot status-polling-42",
                "Status polling result: Status polling result returned parsed draft and done with result message; screenshot status-polling-42",
            )
            text = text.replace(
                "Notification delivery evidence: Notification delivered and shown for document export completion, digital employee task completion, and SSH abnormal disconnect; tap opened typed payloads document-export:pdf-export-job-id-12345, digital-employee-task:digital-employee-task-id-12345, and server-profile:srv-prod for the matching task or export; notification message previews were redacted before display; screenshot notification-delivery-42",
                "Notification delivery evidence: Notification delivered and opened during QA; screenshot notification-delivery-42",
            )
            record = self.write_record(root, text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("How to fill task chain evidence:", output)
            self.assertIn("document upload task ID", output)
            self.assertIn("PDF/Word/Markdown export job IDs", output)
            self.assertIn("digital employee task ID", output)
            self.assertIn("status polling", output)
            self.assertIn("realtime updates", output)
            self.assertIn("notification delivery/open evidence", output)
            self.assertIn("exported-document sharing", output)

    def test_report_points_ssh_gaps_to_manual_smoke_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = self.complete_record_with_local_artifact(root)
            text = text.replace(
                "Connect result: SSH connected successfully to QA server server-profile:srv-prod; screenshot ssh-connect-42",
                "Connect result: SSH connection checked during QA",
            )
            text = text.replace(
                "AI analysis confirmation and sensitive-data warning: SSH terminal output preview from server-profile:srv-prod was redacted before AI analysis confirmation after sensitive-data warning; screenshot ssh-ai-analysis-warning-42",
                "AI analysis confirmation and sensitive-data warning: AI analysis checked",
            )
            text = text.replace(
                "Credential deletion confirmation: Deleted server profile and cleared password/private key credentials for server-profile:srv-prod from secure storage; screenshot credential-delete-42",
                "Credential deletion confirmation: Credentials cleaned up",
            )
            record = self.write_record(root, text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("How to fill backend SSH session smoke evidence:", output)
            self.assertIn("server profile ID", output)
            self.assertIn("backend session ID", output)
            self.assertIn("attach/create evidence", output)
            self.assertIn("host/auth mode", output)
            self.assertIn("read-only command and output", output)
            self.assertIn("disconnect/reconnect", output)
            self.assertIn("redacted AI analysis", output)
            self.assertIn("manual command draft ID", output)
            self.assertIn("credential deletion confirmation", output)

    def test_report_points_hub_llm_gaps_to_official_setup_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = self.complete_record_with_local_artifact(root)
            text = text.replace(
                "Selected HubCenter URL: https://hubs.maclaw.top",
                "Selected HubCenter URL: https://custom.example.test",
            )
            text = text.replace(
                "LLM setup surface restriction: LLM setup configuration shows phone registration/login first and optional account/settings desktop GUI QR authorization only; no redemption-code login and no arbitrary third-party endpoint, base URL, provider URL, or API key fields are exposed; screenshot llm-setup-restriction-42",
                "LLM setup surface restriction: LLM setup accepted redemption code and custom provider URL during QA",
            )
            record = self.write_record(root, text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("How to fill Hub/account/LLM setup evidence:", output)
            self.assertIn("three preset HubCenter URLs", output)
            self.assertIn("selected HubCenter", output)
            self.assertIn("discovered tenant Hub URL", output)
            self.assertIn("phone:<digits> login/credits account", output)
            self.assertIn("bootstrap quota/features/service status", output)
            self.assertIn("desktop GUI QR authorization path", output)
            self.assertIn("no redemption-code login", output)
            self.assertIn("arbitrary provider/base URL/API-key fields", output)

    def test_release_decision_hints_use_shared_evidence_prefills(self) -> None:
        prefills = release_evidence_commands.final_decision_prefills()

        for field, value in prefills.items():
            self.assertIn(value, qa_build_record_report.EVIDENCE_FIELD_HINTS[field])
        self.assertIn(
            release_evidence_commands.release_handoff_command(),
            qa_build_record_report.EVIDENCE_FIELD_HINTS["Release handoff result"],
        )

    def test_report_groups_filename_errors(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            record = self.write_record(
                root,
                self.complete_record_with_local_artifact(root),
                name="qa-record.md",
            )

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("Path / filename:", output)
            self.assertIn(
                "- QA build record filename must be YYYY-MM-DD-<android|ios|android-ios>-<version+build>.md",
                output,
            )

    def test_report_rejects_handoff_evidence_path_without_field_noise(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            record = self.write_record(
                root,
                "# MaClaw Mobile Release Handoff\n",
                name="handoff-1.0.0+42.md",
            )

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("Path / filename:", output)
            self.assertIn(
                "- QA build record path must point to a completed record, not release handoff evidence",
                output,
            )
            self.assertNotIn("Evidence fields and values:", output)
            self.assertNotIn("Release handoff result", output)

    def test_report_groups_local_artifact_hash_mismatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            artifact = (
                root
                / "qa-builds"
                / "build"
                / "app"
                / "outputs"
                / "flutter-apk"
                / "app-release.apk"
            )
            artifact.parent.mkdir(parents=True)
            artifact.write_bytes(b"signed release artifact")
            record = self.write_record(root, complete_record())

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("Local artifact hashes:", output)
            self.assertIn(
                "- SHA256 does not match local artifact build/app/outputs/flutter-apk/app-release.apk",
                output,
            )
            self.assertIn("How to fill signed artifact evidence:", output)
            self.assertIn(
                release_evidence_commands.android_artifact_evidence_command(
                    "1.0.0+42",
                    record_dir=str(record.parent),
                ),
                output,
            )

    def test_report_points_missing_local_ios_archive_to_artifact_helper(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = self.complete_record_with_local_artifact(root).replace(
                "Archive/TestFlight build: TestFlight build 42",
                "Archive/TestFlight build: build/ios/archive/MaClawMobile.xcarchive",
            )
            record = self.write_record(root, text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("Local artifact hashes:", output)
            self.assertIn(
                "- Local iOS archive is missing: build/ios/archive/MaClawMobile.xcarchive",
                output,
            )
            self.assertIn("How to fill signed artifact evidence:", output)
            self.assertIn(
                release_evidence_commands.ios_artifact_evidence_command(
                    record_dir=str(record.parent),
                ),
                output,
            )

    def test_report_points_secret_redaction_failures_to_redacted_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            text = (
                self.complete_record_with_local_artifact(root)
                + "\nDevice logs / screenshots / recordings: Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456"
            )
            record = self.write_record(root, text)

            report = qa_build_record_report.generate_report(record)
            output = qa_build_record_report.format_report(report)

            self.assertFalse(report.passed)
            self.assertIn("Secret redaction:", output)
            self.assertIn(
                "- QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                output,
            )
            self.assertIn("How to fix secret redaction failures:", output)
            self.assertIn("redacted evidence, attachment IDs, task IDs", output)
            self.assertIn(
                f"python3 tool/validate_qa_build_record.py {record}",
                output,
            )

    def test_main_prints_pass_to_stdout(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            record = self.write_record(root, self.complete_record_with_local_artifact(root))
            stdout = StringIO()

            with redirect_stdout(stdout):
                exit_code = qa_build_record_report.main([str(record)])

            self.assertEqual(0, exit_code)
            self.assertIn("Status: PASS", stdout.getvalue())

    def test_main_prints_fail_to_stderr(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            record = self.write_record(
                root,
                self.complete_record_with_local_artifact(root).replace(
                    "Branch: codex/mobile-release\n",
                    "",
                ),
            )
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = qa_build_record_report.main([str(record)])

            self.assertEqual(1, exit_code)
            self.assertIn("Status: FAIL", stderr.getvalue())
            self.assertIn("- Branch", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
