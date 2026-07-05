from __future__ import annotations

import hashlib
import sys
import tempfile
import unittest
import re
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import validate_qa_build_record


DIGITAL_EMPLOYEE_TASK_CONTEXT = (
    "Digital employee task digital-employee-task-id-12345 submitted "
    "through selected HubCenter https://hubs.maclaw.top and "
    "discovered Hub https://tenant-a.maclaw.top for tenant tenant-a "
    "with LLM credits phone:8613800138000 and manual confirmation "
    "execution boundary draft_only_until_mobile_user_confirms; "
    "screenshot digital-employee-task-context-42"
)

LOGIN_RESULT_CONTEXT = (
    "MaClaw official phone account phone:8613800138000 authenticated through "
    "HubCenter after SMS verification code sms-verification:sms-login-12345; "
    "mobile session opened with official credits bound to the phone account; "
    "first LLM call after verification used phone:8613800138000 credits; "
    "screenshot login-result-42"
)

OFFICIAL_LLM_ACCESS_CONTEXT = (
    "MaClaw official LLM access mode available using phone account "
    "phone:8613800138000 official credits for tenant tenant-a; "
    "screenshot llm-access-42; after SMS verification passed, LLM request "
    "request id llm-request-id-12345 has usage record charged to the verified "
    "phone account's MaClaw official credits"
)

DOCUMENT_EXPORT_SHARE_CONTEXT = (
    "Exported PDF, Word/docx, and Markdown/.md files were downloaded and shared "
    "through the system share sheet to Mail after redacted document preview "
    "check redaction-check:document-export-12345 for "
    "pdf-export-job-id-12345, word-export-job-id-12345, and "
    "markdown-export-job-id-12345; saved local path evidence export-share-42"
)

SERVER_CREDENTIAL_RETENTION_CONTEXT = (
    "After local reset, server profiles and SSH credentials password and private "
    "key remained available in secure storage for server-profile:srv-prod; "
    "screenshot credential-retain-42"
)


def complete_record() -> str:
    lines = []
    device_model_count = 0
    field_counts: dict[str, int] = {}
    for field in validate_qa_build_record.REQUIRED_FIELDS:
        field_counts[field] = field_counts.get(field, 0) + 1
        occurrence = field_counts[field]
        platform = "Android" if occurrence == 1 else "iOS"
        value = "ok"
        if field == "Date":
            value = "2026-07-02"
        if field == "Approval date":
            value = "2026-07-02"
        if field == "Git commit":
            value = "abcdef1234567890"
        if field == "Branch":
            value = "codex/mobile-release"
        if field == "Tester":
            value = "qa-operator"
        if field == "Flutter version":
            value = "Flutter 3.41.5"
        if field == "MaClaw account":
            value = "phone:8613800138000"
        if field == "Approved by":
            value = "release-owner"
        if field in validate_qa_build_record.TASK_ID_FIELDS:
            value = f"{field.lower().replace(' ', '-')}-12345"
        if field in validate_qa_build_record.DIGITAL_EMPLOYEE_TASK_FIELDS:
            value = DIGITAL_EMPLOYEE_TASK_CONTEXT
        if field in validate_qa_build_record.PASS_DECISION_FIELDS:
            value = "passed"
        if field in validate_qa_build_record.MANUAL_EVIDENCE_FIELDS:
            value = f"QA evidence captured for {field} with screenshot/log reference"
        if field == "Release handoff result":
            value = "release_handoff.py output saved to docs/qa-builds/handoff-1.0.0+42.md"
        if field == "Runtime boundary verification result":
            value = (
                "MaClaw Mobile runtime boundary verified; log: "
                "docs/qa-builds/runtime-boundary-1.0.0+42.log"
            )
        if field == "Automated release gates result":
            value = (
                "run_release_gates.py: 38 gates passed; log: "
                "docs/qa-builds/release-gates-1.0.0+42.log"
            )
        if field in validate_qa_build_record.LOGIN_RESULT_FIELDS:
            value = LOGIN_RESULT_CONTEXT
        if field in validate_qa_build_record.SHARE_TEXT_EVIDENCE_FIELDS:
            value = f"{platform} Assistant opened from shared text for {field}; screenshot share-text-{field.lower().replace(' ', '-')}-{platform.lower()}"
        if field in validate_qa_build_record.SHARE_URL_EVIDENCE_FIELDS:
            value = f"{platform} Assistant opened shared URL with visible citation for {field}; screenshot share-url-{field.lower().replace(' ', '-')}-{platform.lower()}"
        if field in validate_qa_build_record.SHARE_DOCUMENT_EVIDENCE_FIELDS:
            value = f"{platform} document import upload task ID share-{field.lower().replace(' ', '-')}-{platform.lower()}-12345"
        if field in validate_qa_build_record.AI_ASSISTANT_QUERY_EVIDENCE_FIELDS:
            value = (
                "AI assistant query asked: what changed in mobile emergency "
                "SSH maintenance? Assistant result showed citation "
                "https://example.test/source-a; screenshot assistant-query-42"
            )
        if field in validate_qa_build_record.MOBILE_INPUT_EVIDENCE_FIELDS:
            value = (
                "Voice input recognized transcript for assistant question and "
                "photo/image assistant input produced assistant citation answer "
                "with source https://example.test/source-a; screenshot "
                "mobile-input-42"
            )
        if field in validate_qa_build_record.CITATION_EVIDENCE_FIELDS:
            value = "Visible citations shown with source URLs https://example.test/source-a screenshot citations-42"
        if field in validate_qa_build_record.SHARED_RESULT_EVIDENCE_FIELDS:
            value = (
                "System share sheet opened and shared result with citation "
                "https://example.test/source-a to Mail target after redacted "
                "answer preview check redaction-check:shared-result-12345; "
                "screenshot shared-result-42"
            )
        if field in validate_qa_build_record.DOCUMENT_DRAFT_EVIDENCE_FIELDS:
            value = (
                "Assistant result with citations created document draft "
                "document-draft:draft-from-assistant-12345 for "
                "templates for notice, report, email, proposal, meeting minutes, "
                "and statement from source https://example.test/source-a; "
                "screenshot draft-from-assistant-42"
            )
        if field in validate_qa_build_record.DOCUMENT_EXPORT_SHARE_FIELDS:
            value = DOCUMENT_EXPORT_SHARE_CONTEXT
        if field in validate_qa_build_record.SIGNED_INSTALL_RESULT_FIELDS:
            value = (
                f"Signed build installed and launched for {field}; "
                f"screenshot install-launch-{field.lower().replace(' ', '-')}"
            )
        if field == "Host type":
            value = "Linux cloud server host type recorded for server-profile:srv-prod; screenshot ssh-host-42"
        if field == "Auth mode":
            value = "Password auth mode used for QA SSH server server-profile:srv-prod; screenshot ssh-auth-42"
        if field == "Connect result":
            value = "SSH connected successfully to QA server server-profile:srv-prod; screenshot ssh-connect-42"
        if field == "Read-only command":
            value = "Read-only command whoami executed on server-profile:srv-prod; screenshot ssh-command-42"
        if field == "Command output excerpt":
            value = "Command output excerpt for server-profile:srv-prod shows stdout for whoami: qa-user; screenshot ssh-output-42"
        if field == "Disconnect result":
            value = "SSH disconnected from server-profile:srv-prod and terminal closed cleanly; screenshot ssh-disconnect-42"
        if field == "Reconnect result":
            value = "SSH reconnected to QA server server-profile:srv-prod after disconnect; screenshot ssh-reconnect-42"
        if field == "Copied output evidence":
            value = "Copied terminal output from server-profile:srv-prod to clipboard; screenshot ssh-copy-42"
        if field in validate_qa_build_record.SSH_AI_ANALYSIS_WARNING_FIELDS:
            value = (
                "SSH terminal output preview from server-profile:srv-prod was redacted before AI analysis "
                "confirmation after sensitive-data warning; screenshot "
                "ssh-ai-analysis-warning-42"
            )
        if field in validate_qa_build_record.SSH_AI_RESULT_FIELDS:
            value = (
                "AI explanation returned from redacted SSH terminal output with command draft suggestions for "
                "manual confirmation as command-draft:ssh-ai-draft-12345 on server-profile:srv-prod, not auto executed; screenshot ssh-ai-result-42"
            )
        if field in validate_qa_build_record.CREDENTIAL_DELETION_FIELDS:
            value = (
                "Deleted server profile and cleared password/private key "
                "credentials for server-profile:srv-prod from secure storage; screenshot credential-delete-42"
            )
        if field in validate_qa_build_record.ACCOUNT_PREFERENCE_FIELDS:
            value = (
                "Account settings changed theme and speech language preferences; "
                "screenshot account-preferences-42"
            )
        if field in validate_qa_build_record.LOCAL_WORK_RECORDS_RESET_FIELDS:
            value = (
                "Cleared local work records cache including assistant history, "
                "document drafts, command history, digital employee prompts, "
                "and app preferences while preserving server-profile:srv-prod; "
                "screenshot local-reset-42"
            )
        if field in validate_qa_build_record.SERVER_CREDENTIAL_RETENTION_FIELDS:
            value = SERVER_CREDENTIAL_RETENTION_CONTEXT
        if field in validate_qa_build_record.SERVER_CREDENTIAL_CLEAR_FIELDS:
            value = (
                "Separate explicit account action cleared server profiles and SSH "
                "credentials including password/private key for "
                "server-profile:srv-prod with credential-clear:server-clear-12345; "
                "screenshot credential-clear-42"
            )
        if field in validate_qa_build_record.STATUS_POLLING_FIELDS:
            value = (
                "Status polling result for document upload task "
                "document-upload-task-id-12345 returned parsed draft, and "
                "document export job pdf-export-job-id-12345 returned ready, "
                "and "
                "digital employee task digital-employee-task-id-12345 returned "
                "done with result message; screenshot status-polling-42"
            )
        if field in validate_qa_build_record.REALTIME_UPDATE_FIELDS:
            value = (
                "Realtime WebSocket event updated document upload task "
                "document-upload-task-id-12345 to parsed draft, document "
                "export job pdf-export-job-id-12345 to ready, and digital "
                "employee task digital-employee-task-id-12345 status to done; "
                "screenshot realtime-task-update-42"
            )
        if field in validate_qa_build_record.NOTIFICATION_DELIVERY_FIELDS:
            value = (
                "Notification delivered and shown for document export completion, "
                "digital employee task completion, and SSH abnormal disconnect; "
                "tap opened typed payloads document-export:pdf-export-job-id-12345, "
                "digital-employee-task:digital-employee-task-id-12345, "
                "and server-profile:srv-prod for the matching task or export; "
                "notification message previews were redacted before display; "
                "screenshot notification-delivery-42"
            )
        if field in validate_qa_build_record.ACCOUNT_HUB_TENANT_FIELDS:
            value = (
                "Account screen shows selected Hub https://tenant-a.maclaw.top "
                "and tenant tenant-a; screenshot account-hub-tenant-42"
            )
        if field in validate_qa_build_record.NO_CUSTOM_HUB_URL_FIELDS:
            value = (
                "No custom Hub URL setting surface found in account/settings UI; "
                "screenshot no-custom-hub-url-42"
            )
        if field in validate_qa_build_record.BOOTSTRAP_SERVICE_FIELDS:
            value = (
                "Bootstrap response shows user phone:8613800138000 for tenant "
                "tenant-a, quota limits, feature flags, and service status; "
                "screenshot bootstrap-status-42"
            )
        if field in validate_qa_build_record.NETWORK_RECOVERY_FIELDS:
            value = (
                "Network offline warning shown when HubCenter was unreachable; "
                "network-recovery-id-12345 captured the offline and restored probes; "
                "after recovery selected HubCenter https://hubs.maclaw.top "
                "and discovered Hub https://tenant-a.maclaw.top for tenant "
                "tenant-a returned online status, while assistant online "
                "answers, document export pdf-export-job-id-12345, digital "
                "employee task digital-employee-task-id-12345, and realtime "
                "surfaces resumed; "
                "screenshot network-recovery-42"
            )
        if field in validate_qa_build_record.HUBCENTER_PROBE_FIELDS:
            value = (
                "HubCenter probe candidates "
                "https://hubs.mypapers.top, https://hubs.maclaw.top, "
                "https://hubs2.maclaw.top selected https://hubs.maclaw.top; "
                "screenshot hubcenter-probe-42"
            )
        if field in validate_qa_build_record.DISCOVERED_HUB_TENANT_FIELDS:
            value = (
                "Discovered Hub https://tenant-a.maclaw.top for tenant tenant-a; "
                "screenshot discovered-hub-tenant-42"
            )
        if field in validate_qa_build_record.LLM_ACCESS_EVIDENCE_FIELDS:
            value = OFFICIAL_LLM_ACCESS_CONTEXT
        if field in validate_qa_build_record.LLM_SETUP_RESTRICTION_FIELDS:
            value = (
                "LLM setup configuration shows phone registration/login first and "
                "optional account/settings desktop GUI QR authorization only; "
                "no redemption-code login and no arbitrary third-party endpoint, base URL, "
                "provider URL, or API key fields are exposed; "
                "screenshot llm-setup-restriction-42"
            )
        if field in validate_qa_build_record.PERMISSION_EVIDENCE_FIELDS:
            permission_scope = (
                platform
                if validate_qa_build_record.REQUIRED_FIELD_COUNTS[field] > 1
                else "QA device"
            )
            value = (
                f"{permission_scope} {field} prompt granted on QA device; "
                f"permission-grant:{field.lower().replace(' ', '-')}-12345; "
                f"screenshot permission-{field.lower().replace(' ', '-')}"
            )
        if field == "Notification permission":
            value = (
                f"{platform} Notification permission prompt granted from account "
                "screen, then real task notifications delivered and opened for "
                "document export document-export:pdf-export-job-id-12345, "
                "digital employee digital-employee-task:digital-employee-task-id-12345, "
                "and SSH abnormal server-profile:srv-prod; screenshot "
                f"permission-notification-task-flow-{platform.lower()}; "
                f"permission-grant:notification-{platform.lower()}-12345"
            )
        if field == "Camera permission":
            value = (
                f"{platform} Camera permission prompt granted while capturing "
                "photo/image assistant input for the mobile AI question; "
                f"screenshot permission-camera-assistant-input; "
                f"permission-grant:camera-{platform.lower()}-12345"
            )
        if field == "Microphone permission":
            value = (
                f"{platform} Microphone permission prompt granted while recording "
                "voice assistant question input with transcript; screenshot "
                f"permission-microphone-voice-input; "
                f"permission-grant:microphone-{platform.lower()}-12345"
            )
        if field == "Media/file access":
            value = (
                "Android Media/file access permission granted through file "
                "picker and share-to-app document import/upload for PDF, Word "
                ".docx, Excel .xlsx, CSV, and image/photo payloads; screenshot "
                "permission-media-file-document-import; "
                "permission-grant:media-file-android-12345"
            )
        if field == "Local network / SSH scenario":
            value = (
                "Android Local network permission prompt granted, then SSH "
                "connected to server-profile:srv-prod and read-only command "
                "whoami returned output; screenshot permission-local-network-ssh; "
                "permission-grant:local-network-android-12345"
            )
        if field == "Local network permission":
            value = (
                "iOS Local network permission prompt granted, then SSH "
                "connected to server-profile:srv-prod and read-only command "
                "whoami returned output; screenshot permission-local-network-ssh; "
                "permission-grant:local-network-ios-12345"
            )
        if field == "Speech recognition permission":
            value = (
                "iOS Speech recognition permission prompt granted while "
                "transcribing voice assistant question input; screenshot "
                "permission-speech-recognition-voice-input; "
                "permission-grant:speech-ios-12345"
            )
        if field == "Photo library permission":
            value = (
                "iOS Photo library permission prompt granted while importing "
                "photo/image/screenshot assistant input for the mobile AI "
                "question; screenshot permission-photo-library-assistant-input; "
                "permission-grant:photo-library-ios-12345"
            )
        if field == "Device model / OS":
            device_model_count += 1
            value = (
                "Pixel 8 / Android 14 QA device"
                if device_model_count == 1
                else "iPhone 15 / iOS 18 TestFlight QA device"
            )
        if field == "SHA256":
            value = "a" * 64
        if field == "Artifact path":
            value = "build/app/outputs/flutter-apk/app-release.apk"
        if field == "Version/build number":
            value = "1.0.0+42"
        if field == "Signing identity":
            value = "Android release keystore alias maclaw-mobile"
        if field == "Installer channel":
            value = "internal app sharing"
        if field == "HubCenter candidates":
            value = ", ".join(validate_qa_build_record.OFFICIAL_HUBCENTER_URLS)
        if field == "Selected HubCenter URL":
            value = "https://hubs.maclaw.top"
        if field == "Discovered Hub URL":
            value = "https://tenant-a.maclaw.top"
        if field == "Tenant ID":
            value = "tenant-a"
        if field == "LLM access mode":
            value = "maclaw_official"
        if field == "Desktop GUI QR authorization ID":
            value = validate_qa_build_record.OFFICIAL_LLM_QR_AUTH_ID
        if field == "Runner bundle id":
            value = validate_qa_build_record.RUNNER_BUNDLE_ID
        if field == "Share Extension bundle id":
            value = validate_qa_build_record.SHARE_EXTENSION_BUNDLE_ID
        if field == "Archive/TestFlight build":
            value = "TestFlight build 42"
        if field == "Team ID":
            value = "A1B2C3D4E5"
        if field == "Provisioning profiles":
            value = "Runner profile UUID abc123; Share Extension profile UUID def456"
        if field == "App group":
            value = validate_qa_build_record.APP_GROUP
        if field == "API base URL confirmation":
            value = (
                "API client base URL confirmed using discovered Hub "
                "https://tenant-a.maclaw.top; screenshot api-base-url-42"
            )
        if field == "Realtime Hub URL confirmation":
            value = (
                "Realtime WebSocket Hub URL connected to discovered Hub "
                "https://tenant-a.maclaw.top; screenshot realtime-hub-url-42"
            )
        lines.append(f"{field}: {value}")
    return "\n".join(lines)


def scoped_record(scope: str) -> str:
    lines = []
    dual_seen: dict[str, int] = {}
    for line in complete_record().splitlines():
        field = line.split(":", 1)[0]
        if scope == "android" and field in validate_qa_build_record.IOS_ONLY_FIELDS:
            continue
        if scope == "ios" and field in validate_qa_build_record.ANDROID_ONLY_FIELDS:
            continue
        if field in validate_qa_build_record.SCOPED_DUAL_PLATFORM_FIELDS:
            occurrence = dual_seen.get(field, 0)
            dual_seen[field] = occurrence + 1
            if scope == "android" and occurrence != 0:
                continue
            if scope == "ios" and occurrence != 1:
                continue
        lines.append(line)
    return "\n".join(lines)


class ValidateQABuildRecordTest(unittest.TestCase):
    def test_template_fields_are_required_or_explicitly_optional(self) -> None:
        template = (
            Path(__file__).resolve().parent.parent
            / "docs"
            / "qa_build_record_template.md"
        ).read_text(encoding="utf-8")
        template_fields = {
            match.group(1).strip()
            for match in re.finditer(r"(?m)^([^#`\n][^:\n]+):", template)
        }

        unknown = template_fields - set(validate_qa_build_record.REQUIRED_FIELD_COUNTS)
        unknown -= validate_qa_build_record.OPTIONAL_FIELDS

        self.assertEqual(set(), unknown)

    def test_template_contains_every_required_field_occurrence(self) -> None:
        template = (
            Path(__file__).resolve().parent.parent
            / "docs"
            / "qa_build_record_template.md"
        ).read_text(encoding="utf-8")
        template_fields = [
            match.group(1).strip()
            for match in re.finditer(r"(?m)^([^#`\n][^:\n]+):", template)
        ]

        missing = []
        for field, required_count in sorted(
            validate_qa_build_record.REQUIRED_FIELD_COUNTS.items()
        ):
            actual_count = template_fields.count(field)
            if actual_count < required_count:
                missing.append(f"{field}: expected {required_count}, found {actual_count}")

        self.assertEqual([], missing)

    def test_template_is_not_accepted_as_completed_evidence(self) -> None:
        template = (
            Path(__file__).resolve().parent.parent
            / "docs"
            / "qa_build_record_template.md"
        )

        missing = validate_qa_build_record.validate_file(template)

        self.assertEqual(
            ["QA build record path must point to a completed record, not the template"],
            missing,
        )

    def test_completed_record_passes(self) -> None:
        values = validate_qa_build_record.parse_record(complete_record())

        self.assertEqual([], validate_qa_build_record.missing_required_fields(values))

    def test_android_scoped_record_does_not_require_ios_fields(self) -> None:
        values = validate_qa_build_record.parse_record(scoped_record("android"))

        self.assertEqual(
            [],
            validate_qa_build_record.missing_required_fields(values, scope="android"),
        )

    def test_ios_scoped_record_does_not_require_android_fields(self) -> None:
        values = validate_qa_build_record.parse_record(scoped_record("ios"))

        self.assertEqual(
            [],
            validate_qa_build_record.missing_required_fields(values, scope="ios"),
        )

    def test_scoped_record_files_validate_against_filename_scope(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            artifact = root / "build" / "app" / "outputs" / "flutter-apk" / "app-release.apk"
            artifact.parent.mkdir(parents=True)
            artifact.write_bytes(b"signed release apk bytes")
            digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
            android_record = root / "2026-07-02-android-1.0.0+42.md"
            android_record.write_text(
                scoped_record("android").replace("SHA256: " + "a" * 64, f"SHA256: {digest}"),
                encoding="utf-8",
            )
            ios_record = root / "2026-07-02-ios-1.0.0+42.md"
            ios_record.write_text(scoped_record("ios"), encoding="utf-8")

            self.assertEqual([], validate_qa_build_record.validate_file(android_record))
            self.assertEqual([], validate_qa_build_record.validate_file(ios_record))

    def test_completed_record_file_passes_with_matching_local_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            artifact = (
                Path(tmp)
                / "build"
                / "app"
                / "outputs"
                / "flutter-apk"
                / "app-release.apk"
            )
            artifact.parent.mkdir(parents=True)
            artifact.write_bytes(b"signed release apk bytes")
            digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
            record.write_text(
                complete_record().replace("SHA256: " + "a" * 64, f"SHA256: {digest}"),
                encoding="utf-8",
            )

            self.assertEqual([], validate_qa_build_record.validate_file(record))

    def test_completed_record_file_requires_local_signed_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(complete_record(), encoding="utf-8")

            self.assertIn(
                "Local signed artifact is missing: build/app/outputs/flutter-apk/app-release.apk",
                validate_qa_build_record.validate_file(record),
            )

    def test_completed_record_file_requires_local_ios_archive_when_archive_path_is_recorded(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            artifact = (
                Path(tmp)
                / "build"
                / "app"
                / "outputs"
                / "flutter-apk"
                / "app-release.apk"
            )
            artifact.parent.mkdir(parents=True)
            artifact.write_bytes(b"signed release apk bytes")
            digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
            record.write_text(
                complete_record()
                .replace("SHA256: " + "a" * 64, f"SHA256: {digest}")
                .replace(
                    "Archive/TestFlight build: TestFlight build 42",
                    "Archive/TestFlight build: build/ios/archive/MaClawMobile.xcarchive",
                ),
                encoding="utf-8",
            )

            self.assertIn(
                "Local iOS archive is missing: build/ios/archive/MaClawMobile.xcarchive",
                validate_qa_build_record.validate_file(record),
            )

    def test_completed_record_file_accepts_existing_local_ios_archive_directory(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            artifact = (
                Path(tmp)
                / "build"
                / "app"
                / "outputs"
                / "flutter-apk"
                / "app-release.apk"
            )
            artifact.parent.mkdir(parents=True)
            artifact.write_bytes(b"signed release apk bytes")
            digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
            archive = Path(tmp) / "build" / "ios" / "archive" / "MaClawMobile.xcarchive"
            archive.mkdir(parents=True)
            record.write_text(
                complete_record()
                .replace("SHA256: " + "a" * 64, f"SHA256: {digest}")
                .replace(
                    "Archive/TestFlight build: TestFlight build 42",
                    "Archive/TestFlight build: build/ios/archive/MaClawMobile.xcarchive",
                ),
                encoding="utf-8",
            )

            self.assertEqual([], validate_qa_build_record.validate_file(record))

    def test_completed_record_file_rejects_local_ios_archive_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            artifact = (
                Path(tmp)
                / "build"
                / "app"
                / "outputs"
                / "flutter-apk"
                / "app-release.apk"
            )
            artifact.parent.mkdir(parents=True)
            artifact.write_bytes(b"signed release apk bytes")
            digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
            archive = Path(tmp) / "build" / "ios" / "archive" / "MaClawMobile.xcarchive"
            archive.parent.mkdir(parents=True)
            archive.write_text("not an archive directory", encoding="utf-8")
            record.write_text(
                complete_record()
                .replace("SHA256: " + "a" * 64, f"SHA256: {digest}")
                .replace(
                    "Archive/TestFlight build: TestFlight build 42",
                    "Archive/TestFlight build: build/ios/archive/MaClawMobile.xcarchive",
                ),
                encoding="utf-8",
            )

            self.assertIn(
                "Local iOS archive is not a directory: build/ios/archive/MaClawMobile.xcarchive",
                validate_qa_build_record.validate_file(record),
            )

    def test_branch_is_required_for_release_traceability(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace("Branch: codex/mobile-release\n", ""),
        )

        self.assertIn(
            "Branch",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_branch_must_be_trackable_git_branch_name(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Branch: codex/mobile-release",
                "Branch: release candidate branch",
            ),
        )

        self.assertIn(
            "Branch must be a trackable git branch name",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_version_build_number_must_include_version_and_build(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Version/build number: 1.0.0+42",
                "Version/build number: release candidate",
            ),
        )

        self.assertIn(
            "Version/build number must include app version and build number",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_flutter_version_must_include_flutter_sdk_semver(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Flutter version: Flutter 3.41.5",
                "Flutter version: version 3",
            ),
        )

        self.assertIn(
            "Flutter version must contain a trackable Flutter version",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_maclaw_account_must_be_trackable_account_identity(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "MaClaw account: phone:8613800138000",
                "MaClaw account: release tester",
            ),
        )

        self.assertIn(
            "MaClaw account must identify a trackable phone:<digits> MaClaw Mobile account",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_maclaw_account_rejects_desktop_email_identity(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "MaClaw account: phone:8613800138000",
                "MaClaw account: qa.mobile@example.test",
            ),
        )

        self.assertIn(
            "MaClaw account must identify a trackable phone:<digits> MaClaw Mobile account",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_maclaw_account_rejects_display_formatted_phone_credits(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "MaClaw account: phone:8613800138000",
                "MaClaw account: phone:+8613800138000",
            ),
        )

        self.assertIn(
            "MaClaw account must identify a trackable phone:<digits> MaClaw Mobile account",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_maclaw_account_must_match_login_result_phone_account(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "MaClaw account: phone:8613800138000",
                "MaClaw account: phone:8613900139000",
            ),
        )

        self.assertIn(
            "Login result must reference the recorded MaClaw phone account",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_maclaw_account_accepts_masked_phone_matching_login_tail(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "MaClaw account: phone:8613800138000",
                "MaClaw account: phone:***8000",
            ),
        )

        self.assertEqual([], validate_qa_build_record.missing_required_fields(values))

    def test_tenant_id_must_be_trackable_identifier(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Tenant ID: tenant-a",
                "Tenant ID: tenant display name",
            ),
        )

        self.assertIn(
            "Tenant ID must be a trackable tenant identifier",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_approval_date_must_not_precede_record_date(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Approval date: 2026-07-02",
                "Approval date: 2026-07-01",
            ),
        )

        self.assertIn(
            "Approval date must be on or after Date",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_release_dates_must_not_be_future_dates(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace("Date: 2026-07-02", "Date: 2999-01-01")
            .replace("Approval date: 2026-07-02", "Approval date: 2999-01-02"),
        )

        missing = validate_qa_build_record.missing_required_fields(values)
        self.assertIn("Date must not be in the future", missing)
        self.assertIn("Approval date must not be in the future", missing)

    def test_approver_must_be_different_from_tester(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Approved by: release-owner",
                "Approved by: qa-operator",
            ),
        )

        self.assertIn(
            "Approved by must be different from Tester",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_selected_hubcenter_must_match_presets(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Selected HubCenter URL: https://hubs.maclaw.top",
                "Selected HubCenter URL: https://example.invalid",
            ),
        )

        self.assertIn(
            "Selected HubCenter URL must be one of the preset official HubCenters",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_hubcenter_candidates_must_list_all_presets(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "HubCenter candidates: https://hubs.mypapers.top, https://hubs.maclaw.top, https://hubs2.maclaw.top",
                "HubCenter candidates: https://hubs.mypapers.top",
            ),
        )

        self.assertIn(
            "HubCenter candidates must list exactly the three preset official HubCenters",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_hubcenter_candidates_must_not_include_extra_urls(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "HubCenter candidates: https://hubs.mypapers.top, https://hubs.maclaw.top, https://hubs2.maclaw.top",
                "HubCenter candidates: https://hubs.mypapers.top, https://hubs.maclaw.top, https://hubs2.maclaw.top, https://example.invalid",
            ),
        )

        self.assertIn(
            "HubCenter candidates must list exactly the three preset official HubCenters",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_hubcenter_probe_result_must_reference_selected_hubcenter(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "HubCenter probe result: HubCenter probe candidates https://hubs.mypapers.top, https://hubs.maclaw.top, https://hubs2.maclaw.top selected https://hubs.maclaw.top; screenshot hubcenter-probe-42",
                "HubCenter probe result: HubCenter probe candidates https://hubs.mypapers.top, https://hubs.maclaw.top, https://hubs2.maclaw.top selected https://hubs2.maclaw.top; screenshot hubcenter-probe-42",
            ),
        )

        self.assertIn(
            "HubCenter probe result must reference the selected HubCenter URL",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_api_and_realtime_urls_must_use_discovered_hub_origin(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "API base URL confirmation: API client base URL confirmed using discovered Hub https://tenant-a.maclaw.top; screenshot api-base-url-42",
                "API base URL confirmation: API client base URL confirmed using discovered Hub https://other-tenant.maclaw.top; screenshot api-base-url-42",
            )
            .replace(
                "Realtime Hub URL confirmation: Realtime WebSocket Hub URL connected to discovered Hub https://tenant-a.maclaw.top; screenshot realtime-hub-url-42",
                "Realtime Hub URL confirmation: Realtime WebSocket Hub URL connected to discovered Hub https://realtime.example.invalid; screenshot realtime-hub-url-42",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "API base URL confirmation must match the recorded Discovered Hub URL",
            missing,
        )
        self.assertIn(
            "Realtime Hub URL confirmation must match the recorded Discovered Hub URL",
            missing,
        )

    def test_hub_base_urls_must_be_origin_urls(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Discovered Hub URL: https://tenant-a.maclaw.top",
                "Discovered Hub URL: https://tenant-a.maclaw.top/api/mobile/bootstrap",
            )
            .replace(
                "API base URL confirmation: API client base URL confirmed using discovered Hub https://tenant-a.maclaw.top; screenshot api-base-url-42",
                "API base URL confirmation: API client base URL confirmed using discovered Hub https://tenant-a.maclaw.top/api/mobile/search; screenshot api-base-url-42",
            )
            .replace(
                "Realtime Hub URL confirmation: Realtime WebSocket Hub URL connected to discovered Hub https://tenant-a.maclaw.top; screenshot realtime-hub-url-42",
                "Realtime Hub URL confirmation: Realtime WebSocket Hub URL connected to discovered Hub https://tenant-a.maclaw.top/api/mobile/realtime?token=redacted; screenshot realtime-hub-url-42",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Discovered Hub URL must be the tenant Hub origin URL",
            missing,
        )
        self.assertIn(
            "API base URL confirmation must be the discovered Hub origin URL",
            missing,
        )
        self.assertIn(
            "Realtime Hub URL confirmation must be the discovered Hub origin URL",
            missing,
        )

    def test_api_and_realtime_url_confirmations_must_be_evidence_notes(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "API base URL confirmation: API client base URL confirmed using discovered Hub https://tenant-a.maclaw.top; screenshot api-base-url-42",
                "API base URL confirmation: https://tenant-a.maclaw.top",
            )
            .replace(
                "Realtime Hub URL confirmation: Realtime WebSocket Hub URL connected to discovered Hub https://tenant-a.maclaw.top; screenshot realtime-hub-url-42",
                "Realtime Hub URL confirmation: https://tenant-a.maclaw.top",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "API base URL confirmation must describe API client base URL evidence",
            missing,
        )
        self.assertIn(
            "Realtime Hub URL confirmation must describe realtime WebSocket Hub URL evidence",
            missing,
        )

    def test_discovered_hub_must_not_be_a_hubcenter_url(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Discovered Hub URL: https://tenant-a.maclaw.top",
                "Discovered Hub URL: https://hubs.maclaw.top",
            ),
        )

        self.assertIn(
            "Discovered Hub URL must be a tenant Hub, not a HubCenter URL",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_discovered_hub_tenant_result_must_match_recorded_values(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Discovered Hub/tenant result: Discovered Hub https://tenant-a.maclaw.top for tenant tenant-a; screenshot discovered-hub-tenant-42",
                "Discovered Hub/tenant result: Discovered Hub https://tenant-b.maclaw.top for tenant tenant-b; screenshot discovered-hub-tenant-42",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Discovered Hub/tenant result must reference the recorded Discovered Hub URL",
            missing,
        )
        self.assertIn(
            "Discovered Hub/tenant result must reference the recorded Tenant ID",
            missing,
        )

    def test_third_party_llm_requires_desktop_gui_qr_authorization(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "LLM access mode: maclaw_official",
                "LLM access mode: desktop_qr_third_party",
            )
            .replace(
                "Desktop GUI QR authorization ID: not-used-official-mode",
                "Desktop GUI QR authorization ID: ok",
            ),
        )

        self.assertIn(
            "Desktop GUI QR authorization ID must be trackable for third-party LLM access",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_official_llm_requires_no_desktop_gui_qr_authorization(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Desktop GUI QR authorization ID: not-used-official-mode",
                "Desktop GUI QR authorization ID: qr-auth-1",
            ),
        )

        self.assertIn(
            "Desktop GUI QR authorization ID must be not-used-official-mode for official LLM access",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_third_party_llm_accepts_trackable_desktop_gui_qr_authorization(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "LLM access mode: maclaw_official",
                "LLM access mode: desktop_qr_third_party",
            )
            .replace(
                "Desktop GUI QR authorization ID: not-used-official-mode",
                "Desktop GUI QR authorization ID: qr-auth-20260702",
            )
            .replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: MaClaw desktop GUI QR third-party LLM access authorized by qr-auth-20260702 for tenant tenant-a; screenshot llm-access-42",
            ),
        )

        self.assertEqual([], validate_qa_build_record.missing_required_fields(values))

    def test_third_party_llm_evidence_must_use_maclaw_desktop_gui_qr(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "LLM access mode: maclaw_official",
                "LLM access mode: desktop_qr_third_party",
            )
            .replace(
                "Desktop GUI QR authorization ID: not-used-official-mode",
                "Desktop GUI QR authorization ID: qr-auth-20260702",
            )
            .replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: Generic desktop QR third-party LLM access authorized by qr-auth-20260702 for tenant tenant-a; screenshot llm-access-42",
            ),
        )

        self.assertIn(
            "LLM access evidence must match desktop_qr_third_party mode",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_third_party_llm_rejects_official_mode_qr_sentinel(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "LLM access mode: maclaw_official",
                "LLM access mode: desktop_qr_third_party",
            )
            .replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: Desktop GUI QR third-party LLM access authorized by not-used-official-mode for tenant tenant-a; screenshot llm-access-42",
            ),
        )

        self.assertIn(
            "Desktop GUI QR authorization ID must be a real desktop GUI QR authorization for third-party LLM access",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_third_party_llm_evidence_must_reference_qr_authorization_id(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "LLM access mode: maclaw_official",
                "LLM access mode: desktop_qr_third_party",
            )
            .replace(
                "Desktop GUI QR authorization ID: not-used-official-mode",
                "Desktop GUI QR authorization ID: qr-auth-20260702",
            )
            .replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: Desktop GUI QR third-party LLM access authorized for tenant tenant-a; screenshot llm-access-42",
            ),
        )

        self.assertIn(
            "LLM access evidence must reference the Desktop GUI QR authorization ID",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_llm_access_evidence_must_match_selected_mode(self) -> None:
        official_values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: Desktop GUI QR third-party LLM access authorized for tenant tenant-a; screenshot llm-access-42",
            ),
        )

        self.assertIn(
            "LLM access evidence must match maclaw_official mode",
            validate_qa_build_record.missing_required_fields(official_values),
        )

        third_party_values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "LLM access mode: maclaw_official",
                "LLM access mode: desktop_qr_third_party",
            )
            .replace(
                "Desktop GUI QR authorization ID: not-used-official-mode",
                "Desktop GUI QR authorization ID: qr-auth-20260702",
            ),
        )

        self.assertIn(
            "LLM access evidence must match desktop_qr_third_party mode",
            validate_qa_build_record.missing_required_fields(third_party_values),
        )

    def test_official_llm_access_evidence_must_name_phone_account_credits(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: MaClaw official LLM access mode available for tenant tenant-a; screenshot llm-access-42",
            ),
        )

        self.assertIn(
            "LLM access evidence must match maclaw_official mode",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_official_llm_access_evidence_must_follow_sms_verification(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: MaClaw official LLM access mode available "
                "using phone account phone:8613800138000 official credits for "
                "tenant tenant-a; screenshot llm-access-42",
            ),
        )

        self.assertIn(
            "LLM access evidence must match maclaw_official mode",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_official_llm_access_evidence_requires_usage_record_after_login(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: MaClaw official LLM access mode available "
                "using phone account phone:8613800138000 official credits for "
                "tenant tenant-a; screenshot llm-access-42; after SMS "
                "verification passed, LLM calls use the verified phone "
                "account's MaClaw official credits",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)
        self.assertIn(
            "LLM access evidence must include official phone-credit usage record",
            missing,
        )

    def test_official_llm_access_evidence_requires_digits_only_phone_credits(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: MaClaw official LLM access mode available "
                "using phone account phone:+8613800138000 official credits for "
                "tenant tenant-a; screenshot llm-access-42; after SMS verification "
                "passed, LLM calls use the verified phone account's MaClaw "
                "official credits",
            ),
        )

        self.assertIn(
            "LLM access evidence must match maclaw_official mode",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_official_llm_access_evidence_must_match_recorded_phone_account(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: MaClaw official LLM access mode available using phone account phone:8613900139000 official credits for tenant tenant-a; screenshot llm-access-42",
            ),
        )

        self.assertIn(
            "LLM access evidence must reference the recorded MaClaw phone account",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_llm_access_evidence_must_match_recorded_tenant_id(self) -> None:
        official_values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: MaClaw official LLM access mode available using phone account phone:8613800138000 official credits for tenant tenant-b; screenshot llm-access-42",
            ),
        )

        self.assertIn(
            "LLM access evidence must reference the recorded Tenant ID",
            validate_qa_build_record.missing_required_fields(official_values),
        )

        third_party_values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "LLM access mode: maclaw_official",
                "LLM access mode: desktop_qr_third_party",
            )
            .replace(
                "Desktop GUI QR authorization ID: not-used-official-mode",
                "Desktop GUI QR authorization ID: qr-auth-20260702",
            )
            .replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: Desktop GUI QR third-party LLM access authorized by qr-auth-20260702 for tenant tenant-b; screenshot llm-access-42",
            ),
        )

        self.assertIn(
            "LLM access evidence must reference the recorded Tenant ID",
            validate_qa_build_record.missing_required_fields(third_party_values),
        )

    def test_llm_setup_surface_restriction_must_exclude_arbitrary_endpoints(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "LLM setup surface restriction: LLM setup configuration shows phone registration/login first and optional account/settings desktop GUI QR authorization only; no redemption-code login and no arbitrary third-party endpoint, base URL, provider URL, or API key fields are exposed; screenshot llm-setup-restriction-42",
                "LLM setup surface restriction: LLM setup page screenshot captured during QA run",
            ),
        )

        self.assertIn(
            "LLM setup surface restriction must describe phone login plus optional account/settings desktop GUI QR only, with no redemption-code or arbitrary third-party endpoint fields",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_llm_setup_surface_rejects_legacy_redemption_login_evidence(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "LLM setup surface restriction: LLM setup configuration shows phone registration/login first and optional account/settings desktop GUI QR authorization only; no redemption-code login and no arbitrary third-party endpoint, base URL, provider URL, or API key fields are exposed; screenshot llm-setup-restriction-42",
                "LLM setup surface restriction: LLM setup configuration shows MaClaw official service redemption code and desktop GUI QR options only; no arbitrary third-party endpoint, base URL, provider URL, or API key fields are exposed; screenshot llm-setup-restriction-42",
            ),
        )

        self.assertIn(
            "LLM setup surface restriction must describe phone login plus optional account/settings desktop GUI QR only, with no redemption-code or arbitrary third-party endpoint fields",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_voice_photo_assistant_input_must_describe_expected_flow(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Voice/photo assistant input evidence: Voice input recognized transcript for assistant question and photo/image assistant input produced assistant citation answer with source https://example.test/source-a; screenshot mobile-input-42",
                "Voice/photo assistant input evidence: Mobile input screenshot captured during QA run",
            ),
        )

        self.assertIn(
            "Voice/photo assistant input evidence must describe voice transcription and photo/image assistant input results",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_voice_photo_assistant_input_must_reference_recorded_result(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Voice/photo assistant input evidence: Voice input recognized transcript for assistant question and photo/image assistant input produced assistant citation answer with source https://example.test/source-a; screenshot mobile-input-42",
                "Voice/photo assistant input evidence: Voice input recognized transcript for assistant question and photo/image assistant input produced assistant citation answer; screenshot mobile-input-42",
            ),
        )

        self.assertIn(
            "Voice/photo assistant input evidence must reference a recorded citation URL or document upload task ID",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_fixed_identity_fields_must_match_official_values(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(validate_qa_build_record.RUNNER_BUNDLE_ID, "example.mobile")
            .replace(validate_qa_build_record.APP_GROUP, "group.example.mobile"),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn("Runner bundle id must be top.mypapers.maclaw.mobile", missing)
        self.assertIn("App group must be group.top.mypapers.maclaw.mobile", missing)

    def test_sha256_must_be_a_full_hex_digest(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace("SHA256: " + "a" * 64, "SHA256: not-a-sha"),
        )

        self.assertIn(
            "SHA256 must be 64 hexadecimal characters",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_artifact_path_must_be_android_release_artifact(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Artifact path: build/app/outputs/flutter-apk/app-release.apk",
                "Artifact path: ok",
            ),
        )

        self.assertIn(
            "Artifact path must point to a signed .apk or .aab artifact",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_artifact_path_rejects_debug_builds(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Artifact path: build/app/outputs/flutter-apk/app-release.apk",
                "Artifact path: build/app/outputs/flutter-apk/app-debug.apk",
            ),
        )

        self.assertIn(
            "Artifact path must point to a signed .apk or .aab artifact",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_existing_local_artifact_sha256_must_match_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            artifact = root / "app-release.apk"
            artifact.write_bytes(b"signed apk bytes")
            good_sha = (
                "8646fe442192b5d2b5f32e142d780fc5"
                "f2b01084702e8b76c33beb7131805736"
            )
            record = root / "qa.md"
            record.write_text(
                complete_record()
                .replace(
                    "Artifact path: build/app/outputs/flutter-apk/app-release.apk",
                    "Artifact path: app-release.apk",
                )
                .replace("SHA256: " + "a" * 64, f"SHA256: {good_sha}"),
                encoding="utf-8",
            )

            self.assertEqual([], validate_qa_build_record.validate_file(record))

            record.write_text(
                record.read_text(encoding="utf-8").replace(
                    good_sha,
                    "b" * 64,
                ),
                encoding="utf-8",
            )
            self.assertIn(
                "SHA256 does not match local artifact app-release.apk",
                validate_qa_build_record.validate_file(record),
            )

    def test_ios_signed_build_fields_must_be_auditable(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace("Archive/TestFlight build: TestFlight build 42", "Archive/TestFlight build: ok")
            .replace("Team ID: A1B2C3D4E5", "Team ID: team")
            .replace(
                "Provisioning profiles: Runner profile UUID abc123; Share Extension profile UUID def456",
                "Provisioning profiles: ok",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Archive/TestFlight build must identify an .xcarchive or TestFlight build",
            missing,
        )
        self.assertIn(
            "Team ID must be a 10-character Apple team identifier",
            missing,
        )
        self.assertIn(
            "Provisioning profiles must mention Runner, Share Extension, and trackable profile ID/file/name",
            missing,
        )

    def test_ios_signed_build_rejects_generic_build_labels(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Archive/TestFlight build: TestFlight build 42",
                "Archive/TestFlight build: release build 42",
            ),
        )

        self.assertIn(
            "Archive/TestFlight build must identify an .xcarchive or TestFlight build",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_ios_provisioning_profiles_must_be_trackable(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Provisioning profiles: Runner profile UUID abc123; Share Extension profile UUID def456",
                "Provisioning profiles: Runner and Share Extension profiles present",
            ),
        )

        self.assertIn(
            "Provisioning profiles must mention Runner, Share Extension, and trackable profile ID/file/name",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_ios_provisioning_profiles_reject_bare_uuid_words(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Provisioning profiles: Runner profile UUID abc123; Share Extension profile UUID def456",
                "Provisioning profiles: Runner profile UUID and Share Extension profile UUID",
            ),
        )

        self.assertIn(
            "Provisioning profiles must mention Runner, Share Extension, and trackable profile ID/file/name",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_ios_artifact_fields_reject_documented_placeholders(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Archive/TestFlight build: TestFlight build 42",
                "Archive/TestFlight build: .xcarchive path or TestFlight build number",
            )
            .replace(
                "Provisioning profiles: Runner profile UUID abc123; Share Extension profile UUID def456",
                "Provisioning profiles: <Runner profile UUID/name; Share Extension profile UUID/name>",
            ),
        )
        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Archive/TestFlight build must identify an .xcarchive or TestFlight build",
            missing,
        )
        self.assertIn(
            "Provisioning profiles must mention Runner, Share Extension, and trackable profile ID/file/name",
            missing,
        )

    def test_ios_provisioning_profiles_accept_files_and_profile_names(self) -> None:
        file_values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Provisioning profiles: Runner profile UUID abc123; Share Extension profile UUID def456",
                "Provisioning profiles: Runner Release.mobileprovision; Share Extension Release.mobileprovision",
            ),
        )
        name_values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Provisioning profiles: Runner profile UUID abc123; Share Extension profile UUID def456",
                "Provisioning profiles: Runner profile name MaClaw Runner Release; Share Extension profile name MaClaw Share Extension Release",
            ),
        )

        self.assertNotIn(
            "Provisioning profiles must mention Runner, Share Extension, and trackable profile ID/file/name",
            validate_qa_build_record.missing_required_fields(file_values),
        )
        self.assertNotIn(
            "Provisioning profiles must mention Runner, Share Extension, and trackable profile ID/file/name",
            validate_qa_build_record.missing_required_fields(name_values),
        )

    def test_ios_testflight_build_must_use_explicit_build_number_label(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Archive/TestFlight build: TestFlight build 42",
                "Archive/TestFlight build: TestFlight beta 42",
            ),
        )

        self.assertIn(
            "Archive/TestFlight build must identify an .xcarchive or TestFlight build",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_ios_url_scheme_evidence_must_name_both_schemes(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "URL schemes maclaw and ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER): "
                "QA evidence captured for URL schemes maclaw and "
                "ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER) with screenshot/log reference",
                "URL schemes maclaw and ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER): "
                "maclaw URL scheme verified on iOS with screenshot url-scheme-42",
            ),
        )

        self.assertIn(
            "URL schemes maclaw and ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER) must mention both maclaw and ShareMedia URL schemes",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_date_and_commit_fields_must_be_auditable(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace("Date: 2026-07-02", "Date: yesterday")
            .replace("Approval date: 2026-07-02", "Approval date: soon")
            .replace("Git commit: abcdef1234567890", "Git commit: branch-main"),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn("Date must use a valid YYYY-MM-DD calendar date", missing)
        self.assertIn(
            "Approval date must use a valid YYYY-MM-DD calendar date",
            missing,
        )
        self.assertIn("Git commit must be a 7-40 character hexadecimal SHA", missing)

    def test_dates_must_be_real_calendar_dates(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace("Date: 2026-07-02", "Date: 2026-02-30")
            .replace("Approval date: 2026-07-02", "Approval date: 2026-13-01"),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn("Date must use a valid YYYY-MM-DD calendar date", missing)
        self.assertIn(
            "Approval date must use a valid YYYY-MM-DD calendar date",
            missing,
        )

    def test_task_and_job_ids_must_be_trackable(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Document upload task ID: document-upload-task-id-12345",
                "Document upload task ID: ok",
            )
            .replace("PDF export job ID: pdf-export-job-id-12345", "PDF export job ID: yes"),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Document upload task ID must contain a trackable task/job ID",
            missing,
        )
        self.assertIn("PDF export job ID must contain a trackable task/job ID", missing)

    def test_document_task_and_export_job_ids_must_match_expected_formats(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Document upload task ID: document-upload-task-id-12345",
                "Document upload task ID: generic-task-12345",
            )
            .replace(
                "PDF export job ID: pdf-export-job-id-12345",
                "PDF export job ID: word-export-job-id-12345",
            )
            .replace(
                "Word export job ID: word-export-job-id-12345",
                "Word export job ID: pdf-export-job-id-12345",
            )
            .replace(
                "Markdown export job ID: markdown-export-job-id-12345",
                "Markdown export job ID: pdf-export-job-id-67890",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Document upload task ID must identify a document upload/import task",
            missing,
        )
        self.assertIn(
            "PDF export job ID must identify a matching document export job",
            missing,
        )
        self.assertIn(
            "Word export job ID must identify a matching document export job",
            missing,
        )
        self.assertIn(
            "Markdown export job ID must identify a matching document export job",
            missing,
        )

    def test_document_export_share_evidence_must_cover_all_formats(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Exported document share evidence: {DOCUMENT_EXPORT_SHARE_CONTEXT}",
                "Exported document share evidence: Exported PDF file was downloaded and shared to Mail",
            ),
        )

        self.assertIn(
            "Exported document share evidence must describe exported PDF, Word, and Markdown download/share evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_document_export_share_evidence_must_reference_recorded_export_jobs(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Exported document share evidence: {DOCUMENT_EXPORT_SHARE_CONTEXT}",
                "Exported document share evidence: Exported PDF, Word/docx, and Markdown/.md files were downloaded and shared through the system share sheet to Mail; saved local path evidence export-share-42",
            ),
        )

        self.assertIn(
            "Exported document share evidence must reference recorded PDF, Word, and Markdown export job IDs",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_document_export_share_evidence_must_include_download_or_saved_path(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Exported document share evidence: {DOCUMENT_EXPORT_SHARE_CONTEXT}",
                "Exported document share evidence: Exported PDF, Word/docx, and Markdown/.md files were shared through the system share sheet to Mail after redacted document preview check redaction-check:document-export-12345 for pdf-export-job-id-12345, word-export-job-id-12345, and markdown-export-job-id-12345",
            ),
        )

        self.assertIn(
            "Exported document share evidence must describe exported PDF, Word, and Markdown download/share evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_document_export_share_evidence_must_describe_redacted_preview(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                " after redacted document preview check redaction-check:document-export-12345",
                "",
            ),
        )

        self.assertIn(
            "Exported document share evidence must describe exported PDF, Word, and Markdown download/share evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_document_export_share_evidence_requires_redaction_check_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                " redaction-check:document-export-12345",
                "",
            ),
        )

        self.assertIn(
            "Exported document share evidence must describe exported PDF, Word, and Markdown download/share evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_digital_employee_task_id_must_identify_employee_task(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Digital employee task ID: {DIGITAL_EMPLOYEE_TASK_CONTEXT}",
                "Digital employee task ID: generic-task-12345",
            ),
        )

        self.assertIn(
            "Digital employee task ID must identify a digital employee task",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_digital_employee_task_id_must_include_mobile_context(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Digital employee task ID: {DIGITAL_EMPLOYEE_TASK_CONTEXT}",
                "Digital employee task ID: Digital employee task "
                "digital-employee-task-id-12345 submitted; "
                "screenshot digital-employee-task-context-42",
            ),
        )

        self.assertIn(
            "Digital employee task ID must describe Hub/tenant/LLM credits and manual confirmation context",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_digital_employee_task_id_must_match_recorded_hub_tenant_and_credits(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Digital employee task ID: {DIGITAL_EMPLOYEE_TASK_CONTEXT}",
                "Digital employee task ID: Digital employee task "
                "digital-employee-task-id-12345 submitted through selected "
                "HubCenter https://hubs2.maclaw.top and discovered Hub "
                "https://tenant-b.maclaw.top for tenant tenant-b with LLM "
                "credits phone:8613900139000 and manual confirmation "
                "execution boundary draft_only_until_mobile_user_confirms; "
                "screenshot digital-employee-task-context-42",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Digital employee task ID must reference the recorded selected HubCenter URL",
            missing,
        )
        self.assertIn(
            "Digital employee task ID must reference the recorded Discovered Hub URL",
            missing,
        )
        self.assertIn(
            "Digital employee task ID must reference the recorded Tenant ID",
            missing,
        )
        self.assertIn(
            "Digital employee task ID must reference the recorded MaClaw phone account credits",
            missing,
        )

    def test_digital_employee_task_id_requires_digits_only_phone_credits(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Digital employee task ID: {DIGITAL_EMPLOYEE_TASK_CONTEXT}",
                "Digital employee task ID: Digital employee task "
                "digital-employee-task-id-12345 submitted through selected "
                "HubCenter https://hubs.maclaw.top and discovered Hub "
                "https://tenant-a.maclaw.top for tenant tenant-a with LLM "
                "credits phone:+8613800138000 and manual confirmation "
                "execution boundary draft_only_until_mobile_user_confirms; "
                "screenshot digital-employee-task-context-42",
            ),
        )

        self.assertIn(
            "Digital employee task ID must reference the recorded MaClaw phone account credits",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_duplicate_android_ios_fields_require_both_entries(self) -> None:
        one_sided = complete_record().replace(
            "CSV: Android document import upload task ID share-csv-android-12345\n",
            "",
            1,
        )

        missing = validate_qa_build_record.missing_required_fields(
            validate_qa_build_record.parse_record(one_sided),
        )

        self.assertIn("CSV (2 entries required, 1 filled)", missing)

    def test_duplicate_android_ios_fields_require_platform_order(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Plain text: Android Assistant opened from shared text for Plain text; "
                "screenshot share-text-plain-text-android",
                "Plain text: iOS Assistant opened from shared text for Plain text; "
                "screenshot share-text-plain-text-ios-wrong-order",
                1,
            ),
        )

        self.assertIn(
            "First Plain text entry must be Android evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_required_fields_reject_extra_filled_entries(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            + "\nDate: 2026-07-03"
            + "\nCSV: QA evidence captured for extra CSV entry with screenshot/log reference",
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn("Date (1 entries required, 2 filled)", missing)
        self.assertIn("CSV (2 entries required, 3 filled)", missing)

    def test_duplicate_format_fields_reject_any_invalid_entry(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            + "\nSelected HubCenter URL: https://example.invalid"
            + "\nTeam ID: team"
            + "\nSHA256: not-a-sha"
            + "\nManual SSH smoke passed: ok",
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        for expected in [
            "Selected HubCenter URL must be one of the preset official HubCenters",
            "Team ID must be a 10-character Apple team identifier",
            "SHA256 must be 64 hexadecimal characters",
            "Manual SSH smoke passed must say passed or waived",
        ]:
            self.assertIn(expected, missing)

    def test_manual_evidence_fields_reject_placeholder_values(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Android signed install result: Signed build installed and launched for Android signed install result; screenshot install-launch-android-signed-install-result",
                "Android signed install result: ok",
            )
            .replace(
                "Camera permission: Android Camera permission prompt granted while capturing photo/image assistant input for the mobile AI question; screenshot permission-camera-assistant-input; permission-grant:camera-android-12345",
                "Camera permission: yes",
                1,
            )
            .replace(
                "Connect result: SSH connected successfully to QA server server-profile:srv-prod; screenshot ssh-connect-42",
                "Connect result: done",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Android signed install result must contain auditable QA evidence, not a placeholder",
            missing,
        )
        self.assertIn(
            "Camera permission must contain auditable QA evidence, not a placeholder",
            missing,
        )
        self.assertIn(
            "Connect result must contain auditable QA evidence, not a placeholder",
            missing,
        )

    def test_permission_fields_must_describe_prompt_or_result_evidence(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Android Notification permission prompt granted from account screen, then real task notifications delivered and opened for document export document-export:pdf-export-job-id-12345, digital employee digital-employee-task:digital-employee-task-id-12345, and SSH abnormal server-profile:srv-prod; screenshot permission-notification-task-flow-android; permission-grant:notification-android-12345",
                "Runtime evidence captured in release notes",
                1,
            )
            .replace(
                "Android Camera permission prompt granted while capturing photo/image assistant input for the mobile AI question; screenshot permission-camera-assistant-input; permission-grant:camera-android-12345",
                "Permission prompt granted on QA device; screenshot permission-generic-a",
                1,
            )
            .replace(
                "Android Microphone permission prompt granted while recording voice assistant question input with transcript; screenshot permission-microphone-voice-input; permission-grant:microphone-android-12345",
                "Permission prompt granted on QA device; screenshot permission-generic-b",
                1,
            )
            .replace(
                "iOS Local network permission prompt granted, then SSH connected to server-profile:srv-prod and read-only command whoami returned output; screenshot permission-local-network-ssh; permission-grant:local-network-ios-12345",
                "Permission prompt granted on QA device; screenshot permission-generic-c",
                1,
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        for expected in [
            "Notification permission must describe permission prompt/result evidence",
            "Camera permission must describe permission prompt/result evidence",
            "Microphone permission must describe permission prompt/result evidence",
            "Local network permission must describe local-network permission evidence tied to a real SSH connection and read-only command",
        ]:
            self.assertIn(expected, missing)

    def test_permission_fields_must_include_trackable_permission_grant_ids(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace("; permission-grant:camera-android-12345", "", 1)
            .replace("; permission-grant:local-network-ios-12345", "", 1),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Camera permission must include a trackable permission-grant ID",
            missing,
        )
        self.assertIn(
            "Local network permission must include a trackable permission-grant ID",
            missing,
        )

    def test_notification_permission_must_link_to_real_task_notifications(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Android Notification permission prompt granted from account screen, then real task notifications delivered and opened for document export document-export:pdf-export-job-id-12345, digital employee digital-employee-task:digital-employee-task-id-12345, and SSH abnormal server-profile:srv-prod; screenshot permission-notification-task-flow-android; permission-grant:notification-android-12345",
                "Android Notification permission prompt granted from account screen; screenshot permission-notification-settings-android",
                1,
            )
            .replace(
                "iOS Notification permission prompt granted from account screen, then real task notifications delivered and opened for document export document-export:pdf-export-job-id-12345, digital employee digital-employee-task:digital-employee-task-id-12345, and SSH abnormal server-profile:srv-prod; screenshot permission-notification-task-flow-ios; permission-grant:notification-ios-12345",
                "iOS Notification permission prompt granted from account screen; screenshot permission-notification-settings-ios",
                1,
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Notification permission must link permission evidence to real task notification delivery/open",
            missing,
        )

    def test_mobile_input_permissions_must_link_to_assistant_flows(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Android Camera permission prompt granted while capturing photo/image assistant input for the mobile AI question; screenshot permission-camera-assistant-input; permission-grant:camera-android-12345",
                "Android Camera permission prompt granted for QA device settings; screenshot permission-camera-settings",
                1,
            )
            .replace(
                "Android Microphone permission prompt granted while recording voice assistant question input with transcript; screenshot permission-microphone-voice-input; permission-grant:microphone-android-12345",
                "Android Microphone permission prompt granted for QA device settings; screenshot permission-microphone-settings",
                1,
            )
            .replace(
                "iOS Speech recognition permission prompt granted while transcribing voice assistant question input; screenshot permission-speech-recognition-voice-input; permission-grant:speech-ios-12345",
                "iOS Speech recognition permission prompt granted in Settings; screenshot permission-speech-recognition-settings",
                1,
            )
            .replace(
                "iOS Photo library permission prompt granted while importing photo/image/screenshot assistant input for the mobile AI question; screenshot permission-photo-library-assistant-input; permission-grant:photo-library-ios-12345",
                "iOS Photo library permission prompt granted in Settings; screenshot permission-photo-library-settings",
                1,
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        for expected in [
            "Camera permission must link permission evidence to voice/photo assistant input",
            "Microphone permission must link permission evidence to voice/photo assistant input",
            "Photo library permission must link permission evidence to voice/photo assistant input",
            "Speech recognition permission must link permission evidence to voice/photo assistant input",
        ]:
            self.assertIn(expected, missing)

    def test_media_file_access_must_cover_document_import_formats(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Media/file access: Android Media/file access permission "
                "granted through file picker and share-to-app document "
                "import/upload for PDF, Word .docx, Excel .xlsx, CSV, and "
                "image/photo payloads; screenshot "
                "permission-media-file-document-import; "
                "permission-grant:media-file-android-12345",
                "Media/file access: Android Media/file access permission "
                "granted through Settings; screenshot permission-media-file",
            ),
        )

        self.assertIn(
            "Media/file access must describe file/media access for PDF, Word, Excel, CSV, and image/photo imports",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_local_network_permission_must_link_to_real_ssh_connection(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Local network / SSH scenario: Android Local network "
                "permission prompt granted, then SSH connected to "
                "server-profile:srv-prod and read-only command whoami "
                "returned output; screenshot permission-local-network-ssh; "
                "permission-grant:local-network-android-12345",
                "Local network / SSH scenario: Android Local network "
                "permission prompt granted in Settings; screenshot "
                "permission-local-network-settings",
            )
            .replace(
                "Local network permission: iOS Local network permission prompt "
                "granted, then SSH connected to server-profile:srv-prod and "
                "read-only command whoami returned output; screenshot "
                "permission-local-network-ssh; "
                "permission-grant:local-network-ios-12345",
                "Local network permission: iOS Local network permission prompt "
                "granted in Settings; screenshot permission-local-network-settings",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Local network / SSH scenario must describe local-network permission evidence tied to a real SSH connection and read-only command",
            missing,
        )
        self.assertIn(
            "Local network permission must describe local-network permission evidence tied to a real SSH connection and read-only command",
            missing,
        )

    def test_share_payload_fields_must_describe_expected_flow(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Android Assistant opened from shared text for Plain text; screenshot share-text-plain-text-android",
                "Plain text QA evidence captured in release notes",
                1,
            )
            .replace(
                "Android Assistant opened shared URL with visible citation for URL; screenshot share-url-url-android",
                "URL QA evidence captured in release notes",
                1,
            )
            .replace(
                "Android document import upload task ID share-pdf-android-12345",
                "PDF QA evidence captured in release notes",
                1,
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        for expected in [
            "Plain text must describe assistant share-to-app evidence",
            "URL must describe assistant URL/citation share-to-app evidence",
            "PDF must describe document import/upload share-to-app evidence",
        ]:
            self.assertIn(expected, missing)

    def test_share_document_payloads_must_name_expected_file_format(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Android document import upload task ID share-image/photo-android-12345",
                "Android document import upload task ID share-document-12345",
                1,
            )
            .replace(
                "Android document import upload task ID share-pdf-android-12345",
                "Android document import upload task ID share-document-22345",
                1,
            )
            .replace(
                "Android document import upload task ID share-word-.docx-or-.doc-android-12345",
                "Android document import upload task ID share-document-32345",
                1,
            )
            .replace(
                "Android document import upload task ID share-excel-.xlsx-or-.xls-android-12345",
                "Android document import upload task ID share-document-42345",
                1,
            )
            .replace(
                "Android document import upload task ID share-csv-android-12345",
                "Android document import upload task ID share-document-52345",
                1,
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        for expected in [
            "Image/photo must describe document import/upload share-to-app evidence",
            "PDF must describe document import/upload share-to-app evidence",
            "Word .docx or .doc must describe document import/upload share-to-app evidence",
            "Excel .xlsx or .xls must describe document import/upload share-to-app evidence",
            "CSV must describe document import/upload share-to-app evidence",
        ]:
            self.assertIn(expected, missing)

    def test_share_document_payloads_must_include_trackable_upload_task_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Android document import upload task ID share-pdf-android-12345",
                "Android document import upload task ID captured for PDF in screenshot",
                1,
            ),
        )

        self.assertIn(
            "PDF must describe document import/upload share-to-app evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_ai_assistant_smoke_fields_must_describe_expected_flow(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "AI assistant query asked: what changed in mobile emergency SSH maintenance? Assistant result showed citation https://example.test/source-a; screenshot assistant-query-42",
                "Assistant evidence captured in release notes",
            )
            .replace(
                "Visible citations shown with source URLs https://example.test/source-a screenshot citations-42",
                "Answer area verified in screenshot bundle",
            )
            .replace(
                "System share sheet opened and shared result with citation https://example.test/source-a to Mail target after redacted answer preview check redaction-check:shared-result-12345; screenshot shared-result-42",
                "Result evidence captured in release notes",
            )
            .replace(
                "Assistant result with citations created document draft document-draft:draft-from-assistant-12345 for templates for notice, report, email, proposal, meeting minutes, and statement from source https://example.test/source-a; screenshot draft-from-assistant-42",
                "Draft evidence captured in release notes",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        for expected in [
            "AI assistant query must include the actual AI assistant query or question",
            "Visible citations / sources must identify visible citations, sources, or URLs",
            "Shared result must describe copy, export, or system-share evidence",
            "Document draft created from assistant result must describe assistant-result draft creation for every document template",
        ]:
            self.assertIn(expected, missing)

    def test_ai_assistant_query_must_name_assistant_context(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "AI assistant query asked: what changed in mobile emergency SSH maintenance? Assistant result showed citation https://example.test/source-a; screenshot assistant-query-42",
                "Search query asked: what changed in mobile emergency SSH maintenance? Result showed citation https://example.test/source-a; screenshot search-query-42",
            ),
        )

        self.assertIn(
            "AI assistant query must include the actual AI assistant query or question",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_document_draft_from_assistant_result_must_name_assistant_result(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Document draft created from assistant result: Assistant result with citations created document draft document-draft:draft-from-assistant-12345 for templates for notice, report, email, proposal, meeting minutes, and statement from source https://example.test/source-a; screenshot draft-from-assistant-42",
                "Document draft created from assistant result: Search result with citations created document draft document-draft:draft-from-search-12345 for templates for notice, report, email, proposal, meeting minutes, and statement from source https://example.test/source-a; screenshot draft-from-search-42",
            ),
        )

        self.assertIn(
            "Document draft created from assistant result must describe assistant-result draft creation for every document template",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_document_draft_from_assistant_result_must_cover_every_template_type(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Document draft created from assistant result: Assistant result with citations created document draft document-draft:draft-from-assistant-12345 for templates for notice, report, email, proposal, meeting minutes, and statement from source https://example.test/source-a; screenshot draft-from-assistant-42",
                "Document draft created from assistant result: Assistant result with citations created report document draft template; screenshot draft-from-assistant-42",
            ),
        )

        self.assertIn(
            "Document draft created from assistant result must describe assistant-result draft creation for every document template",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_document_draft_from_assistant_result_must_reference_recorded_citation_url(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Document draft created from assistant result: Assistant result with citations created document draft document-draft:draft-from-assistant-12345 for templates for notice, report, email, proposal, meeting minutes, and statement from source https://example.test/source-a; screenshot draft-from-assistant-42",
                "Document draft created from assistant result: Assistant result with citations created document draft templates for notice, report, email, proposal, meeting minutes, and statement; screenshot draft-from-assistant-42",
            ),
        )

        self.assertIn(
            "Document draft created from assistant result must reference a recorded citation URL",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_document_draft_from_assistant_result_requires_trackable_draft_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Document draft created from assistant result: Assistant result with citations created document draft document-draft:draft-from-assistant-12345 for templates for notice, report, email, proposal, meeting minutes, and statement from source https://example.test/source-a; screenshot draft-from-assistant-42",
                "Document draft created from assistant result: Assistant result with citations created document draft for templates for notice, report, email, proposal, meeting minutes, and statement from source https://example.test/source-a; screenshot draft-from-assistant-42",
            ),
        )

        self.assertIn(
            "Document draft created from assistant result must describe assistant-result draft creation for every document template",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_ai_assistant_smoke_fields_accept_specific_evidence(self) -> None:
        values = validate_qa_build_record.parse_record(complete_record())

        self.assertEqual([], validate_qa_build_record.missing_required_fields(values))

    def test_legacy_ai_search_field_names_map_to_assistant_fields(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace("AI assistant query:", "AI search query:")
            .replace(
                "Document draft created from assistant result:",
                "Document draft created from search:",
            )
        )

        self.assertIn(
            "AI assistant query",
            values,
        )
        self.assertIn(
            "Document draft created from assistant result",
            values,
        )
        self.assertEqual([], validate_qa_build_record.missing_required_fields(values))

    def test_ai_assistant_query_must_reference_recorded_citation_url(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "AI assistant query asked: what changed in mobile emergency SSH maintenance? Assistant result showed citation https://example.test/source-a; screenshot assistant-query-42",
                "AI assistant query asked: what changed in mobile emergency SSH maintenance? screenshot assistant-query-42",
            ),
        )

        self.assertIn(
            "AI assistant query must reference a recorded citation URL",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_citation_evidence_requires_visible_https_source_url(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Visible citations shown with source URLs https://example.test/source-a screenshot citations-42",
                "Visible citations shown with source names only in screenshot citations-42",
            ),
        )

        self.assertIn(
            "Visible citations / sources must identify visible citations, sources, or URLs",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_citation_evidence_must_be_visible_in_answer_or_result(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Visible citations shown with source URLs https://example.test/source-a screenshot citations-42",
                "Citation source URL https://example.test/source-a captured in backend log citations-42",
            ),
        )

        self.assertIn(
            "Visible citations / sources must identify visible citations, sources, or URLs",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_shared_result_evidence_requires_target_or_output(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "System share sheet opened and shared result with citation https://example.test/source-a to Mail target after redacted answer preview check redaction-check:shared-result-12345; screenshot shared-result-42",
                "Shared result button was tapped and screenshot shared-result-42",
            ),
        )

        self.assertIn(
            "Shared result must describe copy, export, or system-share evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_shared_result_evidence_must_reference_recorded_citation_url(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "System share sheet opened and shared result with citation https://example.test/source-a to Mail target after redacted answer preview check redaction-check:shared-result-12345; screenshot shared-result-42",
                "System share sheet opened and shared result to Mail target; screenshot shared-result-42",
            ),
        )

        self.assertIn(
            "Shared result must reference a recorded citation URL",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_shared_result_evidence_must_describe_redacted_externalized_content(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                " after redacted answer preview check redaction-check:shared-result-12345",
                "",
            ),
        )

        self.assertIn(
            "Shared result must describe copy, export, or system-share evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_shared_result_evidence_requires_redaction_check_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                " redaction-check:shared-result-12345",
                "",
            ),
        )

        self.assertIn(
            "Shared result must describe copy, export, or system-share evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_signed_install_results_must_describe_install_and_launch(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Android signed install result: Signed build installed and launched for Android signed install result; screenshot install-launch-android-signed-install-result",
                "Android signed install result: QA evidence captured for Android signed install result with screenshot/log reference",
            )
            .replace(
                "iOS signed install result: Signed build installed and launched for iOS signed install result; screenshot install-launch-ios-signed-install-result",
                "iOS signed install result: TestFlight build available in screenshot bundle",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Android signed install result must describe signed install and app launch evidence",
            missing,
        )
        self.assertIn(
            "iOS signed install result must describe signed install and app launch evidence",
            missing,
        )

    def test_signed_install_results_must_match_platform(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Android signed install result: Signed build installed and launched for Android signed install result; screenshot install-launch-android-signed-install-result",
                "Android signed install result: TestFlight build installed and launched on iPhone; screenshot install-launch-ios-wrong-slot",
            )
            .replace(
                "iOS signed install result: Signed build installed and launched for iOS signed install result; screenshot install-launch-ios-signed-install-result",
                "iOS signed install result: Android APK installed and launched on Pixel; screenshot install-launch-android-wrong-slot",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Android signed install result must describe the matching platform install evidence",
            missing,
        )
        self.assertIn(
            "iOS signed install result must describe the matching platform install evidence",
            missing,
        )

    def test_ssh_ai_and_credential_fields_must_include_safety_specifics(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "AI analysis confirmation and sensitive-data warning: SSH terminal output preview from server-profile:srv-prod was redacted before AI analysis confirmation after sensitive-data warning; screenshot ssh-ai-analysis-warning-42",
                "AI analysis confirmation and sensitive-data warning: SSH analysis completed with screenshot/log reference",
            )
            .replace(
                "AI explanation / command draft result: AI explanation returned from redacted SSH terminal output with command draft suggestions for manual confirmation as command-draft:ssh-ai-draft-12345 on server-profile:srv-prod, not auto executed; screenshot ssh-ai-result-42",
                "AI explanation / command draft result: AI result screenshot captured",
            )
            .replace(
                "Credential deletion confirmation: Deleted server profile and cleared password/private key credentials for server-profile:srv-prod from secure storage; screenshot credential-delete-42",
                "Credential deletion confirmation: Server profile cleanup screenshot captured",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "AI analysis confirmation and sensitive-data warning must describe preview confirmation and sensitive-data warning evidence",
            missing,
        )
        self.assertIn(
            "AI explanation / command draft result must describe AI explanation, command drafts, manual execution evidence, and redacted SSH output context",
            missing,
        )
        self.assertIn(
            "Credential deletion confirmation must describe cleared SSH credential storage evidence",
            missing,
        )

    def test_ssh_ai_result_must_include_redacted_terminal_output_context(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "AI explanation / command draft result: AI explanation returned from redacted SSH terminal output with command draft suggestions for manual confirmation as command-draft:ssh-ai-draft-12345 on server-profile:srv-prod, not auto executed; screenshot ssh-ai-result-42",
                "AI explanation / command draft result: AI explanation returned with command draft suggestions for manual confirmation as command-draft:ssh-ai-draft-12345 on server-profile:srv-prod, not auto executed; screenshot ssh-ai-result-42",
            ),
        )

        self.assertIn(
            "AI explanation / command draft result must describe AI explanation, command drafts, manual execution evidence, and redacted SSH output context",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_ssh_ai_result_requires_trackable_command_draft_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "AI explanation / command draft result: AI explanation returned from redacted SSH terminal output with command draft suggestions for manual confirmation as command-draft:ssh-ai-draft-12345 on server-profile:srv-prod, not auto executed; screenshot ssh-ai-result-42",
                "AI explanation / command draft result: AI explanation returned from redacted SSH terminal output with command draft suggestions for manual confirmation on server-profile:srv-prod, not auto executed; screenshot ssh-ai-result-42",
            ),
        )

        self.assertIn(
            "AI explanation / command draft result must describe AI explanation, command drafts, manual execution evidence, and redacted SSH output context",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_ssh_ai_analysis_warning_must_include_redacted_output_preview(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "AI analysis confirmation and sensitive-data warning: SSH terminal output preview from server-profile:srv-prod was redacted before AI analysis confirmation after sensitive-data warning; screenshot ssh-ai-analysis-warning-42",
                "AI analysis confirmation and sensitive-data warning: AI analysis preview confirmation accepted after sensitive-data warning; screenshot ssh-ai-analysis-warning-42",
            ),
        )

        self.assertIn(
            "AI analysis confirmation and sensitive-data warning must describe preview confirmation and sensitive-data warning evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_account_privacy_and_local_data_fields_must_be_specific(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Theme and speech language change result: Account settings changed theme and speech language preferences; screenshot account-preferences-42",
                "Theme and speech language change result: Settings screenshot captured",
            )
            .replace(
                "Local work records reset confirmation: Cleared local work records cache including assistant history, document drafts, command history, digital employee prompts, and app preferences while preserving server-profile:srv-prod; screenshot local-reset-42",
                "Local work records reset confirmation: Cache cleared screenshot captured",
            )
            .replace(
                f"Server credentials retained after local reset: {SERVER_CREDENTIAL_RETENTION_CONTEXT}",
                "Server credentials retained after local reset: Credentials screenshot captured",
            )
            .replace(
                "Server profiles/SSH credentials clear confirmation: Separate explicit account action cleared server profiles and SSH credentials including password/private key for server-profile:srv-prod with credential-clear:server-clear-12345; screenshot credential-clear-42",
                "Server profiles/SSH credentials clear confirmation: Cleanup screenshot captured",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Theme and speech language change result must describe account theme and speech language evidence",
            missing,
        )
        self.assertIn(
            "Local work records reset confirmation must describe clearing local work records and app preferences",
            missing,
        )
        self.assertIn(
            "Server credentials retained after local reset must describe server profiles and SSH credentials retained after local reset",
            missing,
        )
        self.assertIn(
            "Server profiles/SSH credentials clear confirmation must describe separate explicit server profile and SSH credential clearing",
            missing,
        )

    def test_account_privacy_server_credentials_must_reference_recorded_server_profile(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Local work records reset confirmation: Cleared local work records cache including assistant history, document drafts, command history, digital employee prompts, and app preferences while preserving server-profile:srv-prod; screenshot local-reset-42",
                "Local work records reset confirmation: Cleared local work records cache including assistant history, document drafts, command history, digital employee prompts, and app preferences while preserving server profiles; screenshot local-reset-42",
            )
            .replace(
                f"Server credentials retained after local reset: {SERVER_CREDENTIAL_RETENTION_CONTEXT}",
                "Server credentials retained after local reset: After local reset, server profiles and SSH credentials password and private key remained available; screenshot credential-retain-42",
            )
            .replace(
                "Server profiles/SSH credentials clear confirmation: Separate explicit account action cleared server profiles and SSH credentials including password/private key for server-profile:srv-prod with credential-clear:server-clear-12345; screenshot credential-clear-42",
                "Server profiles/SSH credentials clear confirmation: Separate explicit account action cleared server profiles and SSH credentials including password/private key with credential-clear:server-clear-12345; screenshot credential-clear-42",
            ),
        )

        self.assertIn(
            "Account privacy server credential evidence must reference the recorded server-profile notification ID",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_local_work_records_reset_accepts_legacy_search_history_wording(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Local work records reset confirmation: Cleared local work records cache including assistant history, document drafts, command history, digital employee prompts, and app preferences while preserving server-profile:srv-prod; screenshot local-reset-42",
                "Local work records reset confirmation: Cleared local work records cache including search history, document drafts, command history, digital employee prompts, and app preferences while preserving server-profile:srv-prod; screenshot local-reset-42",
            ),
        )

        self.assertNotIn(
            "Local work records reset confirmation must describe clearing local work records and app preferences",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_server_credentials_retention_must_reference_secure_storage(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Server credentials retained after local reset: {SERVER_CREDENTIAL_RETENTION_CONTEXT}",
                "Server credentials retained after local reset: After local "
                "reset, server profiles and SSH credentials password and "
                "private key remained available for server-profile:srv-prod; "
                "screenshot credential-retain-42",
            ),
        )

        self.assertIn(
            "Server credentials retained after local reset must describe server profiles and SSH credentials retained after local reset",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_server_credential_clear_requires_trackable_clear_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Server profiles/SSH credentials clear confirmation: Separate explicit account action cleared server profiles and SSH credentials including password/private key for server-profile:srv-prod with credential-clear:server-clear-12345; screenshot credential-clear-42",
                "Server profiles/SSH credentials clear confirmation: Separate explicit account action cleared server profiles and SSH credentials including password/private key for server-profile:srv-prod; screenshot credential-clear-42",
            ),
        )

        self.assertIn(
            "Server profiles/SSH credentials clear confirmation must describe separate explicit server profile and SSH credential clearing",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_ssh_smoke_fields_must_describe_expected_actions(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Host type: Linux cloud server host type recorded for server-profile:srv-prod; screenshot ssh-host-42",
                "Host type: QA screenshot captured",
            )
            .replace(
                "Auth mode: Password auth mode used for QA SSH server server-profile:srv-prod; screenshot ssh-auth-42",
                "Auth mode: QA screenshot captured",
            )
            .replace(
                "Connect result: SSH connected successfully to QA server server-profile:srv-prod; screenshot ssh-connect-42",
                "Connect result: QA screenshot captured",
            )
            .replace(
                "Read-only command: Read-only command whoami executed on server-profile:srv-prod; screenshot ssh-command-42",
                "Read-only command: QA screenshot captured",
            )
            .replace(
                "Command output excerpt: Command output excerpt for server-profile:srv-prod shows stdout for whoami: qa-user; screenshot ssh-output-42",
                "Command output excerpt: QA screenshot captured",
            )
            .replace(
                "Disconnect result: SSH disconnected from server-profile:srv-prod and terminal closed cleanly; screenshot ssh-disconnect-42",
                "Disconnect result: QA screenshot captured",
            )
            .replace(
                "Reconnect result: SSH reconnected to QA server server-profile:srv-prod after disconnect; screenshot ssh-reconnect-42",
                "Reconnect result: QA screenshot captured",
            )
            .replace(
                "Copied output evidence: Copied terminal output from server-profile:srv-prod to clipboard; screenshot ssh-copy-42",
                "Copied output evidence: QA screenshot captured",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        for expected in [
            "Host type must describe the expected SSH smoke-test evidence",
            "Auth mode must describe the expected SSH smoke-test evidence",
            "Connect result must describe the expected SSH smoke-test evidence",
            "Read-only command must describe the expected SSH smoke-test evidence",
            "Command output excerpt must describe the expected SSH smoke-test evidence",
            "Disconnect result must describe the expected SSH smoke-test evidence",
            "Reconnect result must describe the expected SSH smoke-test evidence",
            "Copied output evidence must describe the expected SSH smoke-test evidence",
        ]:
            self.assertIn(expected, missing)

    def test_ssh_read_only_command_rejects_high_risk_commands(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Read-only command: Read-only command whoami executed on server-profile:srv-prod; screenshot ssh-command-42",
                "Read-only command: Read-only command rm -rf /tmp/cache executed on server-profile:srv-prod; screenshot ssh-command-42",
            ),
        )

        self.assertIn(
            "Read-only command must describe the expected SSH smoke-test evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_ssh_read_only_command_accepts_safe_diagnostic_commands(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Read-only command: Read-only command whoami executed on server-profile:srv-prod; screenshot ssh-command-42",
                "Read-only command: Read-only command df -h executed on server-profile:srv-prod; screenshot ssh-command-42",
            )
            .replace(
                "Command output excerpt: Command output excerpt for server-profile:srv-prod shows stdout for whoami: qa-user; screenshot ssh-output-42",
                "Command output excerpt: Command output excerpt for server-profile:srv-prod shows stdout for df -h root filesystem usage; screenshot ssh-output-42",
            ),
        )

        self.assertEqual([], validate_qa_build_record.missing_required_fields(values))

    def test_ssh_smoke_fields_must_reference_recorded_server_profile_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Host type: Linux cloud server host type recorded for server-profile:srv-prod; screenshot ssh-host-42",
                "Host type: Linux cloud server host type recorded; screenshot ssh-host-42",
            )
            .replace(
                "Auth mode: Password auth mode used for QA SSH server server-profile:srv-prod; screenshot ssh-auth-42",
                "Auth mode: Password auth mode used for QA SSH server; screenshot ssh-auth-42",
            )
            .replace(
                "Connect result: SSH connected successfully to QA server server-profile:srv-prod; screenshot ssh-connect-42",
                "Connect result: SSH connected successfully to QA server; screenshot ssh-connect-42",
            )
            .replace(
                "Read-only command: Read-only command whoami executed on server-profile:srv-prod; screenshot ssh-command-42",
                "Read-only command: Read-only command whoami executed; screenshot ssh-command-42",
            )
            .replace(
                "Command output excerpt: Command output excerpt for server-profile:srv-prod shows stdout for whoami: qa-user; screenshot ssh-output-42",
                "Command output excerpt: Command output excerpt shows stdout for whoami: qa-user; screenshot ssh-output-42",
            )
            .replace(
                "Disconnect result: SSH disconnected from server-profile:srv-prod and terminal closed cleanly; screenshot ssh-disconnect-42",
                "Disconnect result: SSH disconnected and terminal closed cleanly; screenshot ssh-disconnect-42",
            )
            .replace(
                "Reconnect result: SSH reconnected to QA server server-profile:srv-prod after disconnect; screenshot ssh-reconnect-42",
                "Reconnect result: SSH reconnected to QA server after disconnect; screenshot ssh-reconnect-42",
            )
            .replace(
                "Copied output evidence: Copied terminal output from server-profile:srv-prod to clipboard; screenshot ssh-copy-42",
                "Copied output evidence: Copied terminal output to clipboard; screenshot ssh-copy-42",
            )
            .replace(
                "AI analysis confirmation and sensitive-data warning: SSH terminal output preview from server-profile:srv-prod was redacted before AI analysis confirmation after sensitive-data warning; screenshot ssh-ai-analysis-warning-42",
                "AI analysis confirmation and sensitive-data warning: SSH terminal output preview was redacted before AI analysis confirmation after sensitive-data warning; screenshot ssh-ai-analysis-warning-42",
            )
            .replace(
                "AI explanation / command draft result: AI explanation returned from redacted SSH terminal output with command draft suggestions for manual confirmation as command-draft:ssh-ai-draft-12345 on server-profile:srv-prod, not auto executed; screenshot ssh-ai-result-42",
                "AI explanation / command draft result: AI explanation returned with command draft suggestions for manual confirmation as command-draft:ssh-ai-draft-12345, not auto executed; screenshot ssh-ai-result-42",
            )
            .replace(
                "Credential deletion confirmation: Deleted server profile and cleared password/private key credentials for server-profile:srv-prod from secure storage; screenshot credential-delete-42",
                "Credential deletion confirmation: Deleted server profile and cleared password/private key credentials from secure storage; screenshot credential-delete-42",
            )
        )

        self.assertIn(
            "Manual SSH smoke evidence must reference the recorded server-profile notification ID",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_status_polling_and_realtime_fields_must_describe_task_updates(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Status polling result: Status polling result for document upload task document-upload-task-id-12345 returned parsed draft, and document export job pdf-export-job-id-12345 returned ready, and digital employee task digital-employee-task-id-12345 returned done with result message; screenshot status-polling-42",
                "Status polling result: Service smoke screenshot captured during QA run",
            )
            .replace(
                "Realtime update evidence: Realtime WebSocket event updated document upload task document-upload-task-id-12345 to parsed draft, document export job pdf-export-job-id-12345 to ready, and digital employee task digital-employee-task-id-12345 status to done; screenshot realtime-task-update-42",
                "Realtime update evidence: Account page realtime check screenshot captured",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Status polling result must describe task/job status polling evidence",
            missing,
        )
        self.assertIn(
            "Realtime update evidence must describe realtime task/document update evidence",
            missing,
        )

    def test_status_polling_and_realtime_must_reference_recorded_task_ids(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Status polling result: Status polling result for document upload task document-upload-task-id-12345 returned parsed draft, and document export job pdf-export-job-id-12345 returned ready, and digital employee task digital-employee-task-id-12345 returned done with result message; screenshot status-polling-42",
                "Status polling result: Status polling result returned parsed draft, document export returned ready, and digital employee task returned done with result message; screenshot status-polling-42",
            )
            .replace(
                "Realtime update evidence: Realtime WebSocket event updated document upload task document-upload-task-id-12345 to parsed draft, document export job pdf-export-job-id-12345 to ready, and digital employee task digital-employee-task-id-12345 status to done; screenshot realtime-task-update-42",
                "Realtime update evidence: Realtime WebSocket event updated document upload task to parsed draft, document export job to ready, and digital employee task status to done; screenshot realtime-task-update-42",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Status polling result must reference a recorded task/job ID",
            missing,
        )
        self.assertIn(
            "Realtime update evidence must reference a recorded task/job ID",
            missing,
        )

    def test_status_and_realtime_must_reference_document_upload_task_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Status polling result: Status polling result for document upload task document-upload-task-id-12345 returned parsed draft, and document export job pdf-export-job-id-12345 returned ready, and digital employee task digital-employee-task-id-12345 returned done with result message; screenshot status-polling-42",
                "Status polling result: Status polling result for document upload task returned parsed draft, and document export job pdf-export-job-id-12345 returned ready, and digital employee task digital-employee-task-id-12345 returned done with result message; screenshot status-polling-42",
            )
            .replace(
                "Realtime update evidence: Realtime WebSocket event updated document upload task document-upload-task-id-12345 to parsed draft, document export job pdf-export-job-id-12345 to ready, and digital employee task digital-employee-task-id-12345 status to done; screenshot realtime-task-update-42",
                "Realtime update evidence: Realtime WebSocket event updated document upload task to parsed draft, document export job pdf-export-job-id-12345 to ready, and digital employee task digital-employee-task-id-12345 status to done; screenshot realtime-task-update-42",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Status polling result must reference the recorded document upload task ID",
            missing,
        )
        self.assertIn(
            "Realtime update evidence must reference the recorded document upload task ID",
            missing,
        )

    def test_status_and_realtime_must_reference_document_export_job_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Status polling result: Status polling result for document upload task document-upload-task-id-12345 returned parsed draft, and document export job pdf-export-job-id-12345 returned ready, and digital employee task digital-employee-task-id-12345 returned done with result message; screenshot status-polling-42",
                "Status polling result: Status polling result for document upload task document-upload-task-id-12345 returned parsed draft, and document export job returned ready, and digital employee task digital-employee-task-id-12345 returned done with result message; screenshot status-polling-42",
            )
            .replace(
                "Realtime update evidence: Realtime WebSocket event updated document upload task document-upload-task-id-12345 to parsed draft, document export job pdf-export-job-id-12345 to ready, and digital employee task digital-employee-task-id-12345 status to done; screenshot realtime-task-update-42",
                "Realtime update evidence: Realtime WebSocket event updated document upload task document-upload-task-id-12345 to parsed draft, document export job to ready, and digital employee task digital-employee-task-id-12345 status to done; screenshot realtime-task-update-42",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Status polling result must reference a recorded document export job ID",
            missing,
        )
        self.assertIn(
            "Realtime update evidence must reference a recorded document export job ID",
            missing,
        )

    def test_status_and_realtime_must_reference_digital_employee_task_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Status polling result: Status polling result for document upload task document-upload-task-id-12345 returned parsed draft, and document export job pdf-export-job-id-12345 returned ready, and digital employee task digital-employee-task-id-12345 returned done with result message; screenshot status-polling-42",
                "Status polling result: Status polling result for document upload task document-upload-task-id-12345 returned parsed draft, and document export job pdf-export-job-id-12345 returned ready, and digital employee task returned done with result message; screenshot status-polling-42",
            )
            .replace(
                "Realtime update evidence: Realtime WebSocket event updated document upload task document-upload-task-id-12345 to parsed draft, document export job pdf-export-job-id-12345 to ready, and digital employee task digital-employee-task-id-12345 status to done; screenshot realtime-task-update-42",
                "Realtime update evidence: Realtime WebSocket event updated document upload task document-upload-task-id-12345 to parsed draft, document export job pdf-export-job-id-12345 to ready, and digital employee task status to done; screenshot realtime-task-update-42",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Status polling result must reference the recorded digital employee task ID",
            missing,
        )
        self.assertIn(
            "Realtime update evidence must reference the recorded digital employee task ID",
            missing,
        )

    def test_notification_delivery_must_cover_tasks_payload_and_ssh_abnormal(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Notification delivery evidence: Notification delivered and shown for document export completion, digital employee task completion, and SSH abnormal disconnect; tap opened typed payloads document-export:pdf-export-job-id-12345, digital-employee-task:digital-employee-task-id-12345, and server-profile:srv-prod for the matching task or export; notification message previews were redacted before display; screenshot notification-delivery-42",
                "Notification delivery evidence: Notification screenshot captured during QA run",
            ),
        )

        self.assertIn(
            "Notification delivery evidence must describe delivered document, digital employee, and SSH abnormal notifications with typed payload/open evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_notification_delivery_must_describe_redacted_message_preview(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "notification message previews were redacted before display; ",
                "",
            ),
        )

        self.assertIn(
            "Notification delivery evidence must describe delivered document, digital employee, and SSH abnormal notifications with typed payload/open evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_notification_delivery_rejects_untyped_payload_evidence(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "tap opened typed payloads document-export:pdf-export-job-id-12345, digital-employee-task:digital-employee-task-id-12345, and server-profile:srv-prod for the matching task or export",
                "tap opened the payload/deep link for the matching task or export",
            ),
        )

        self.assertIn(
            "Notification delivery evidence must describe delivered document, digital employee, and SSH abnormal notifications with typed payload/open evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_notification_delivery_rejects_empty_typed_payload_ids(self) -> None:
        missing_document_payload = validate_qa_build_record.parse_record(
            complete_record().replace(
                "document-export:pdf-export-job-id-12345",
                "document-export:",
            ),
        )

        self.assertIn(
            "Notification delivery evidence must describe delivered document, digital employee, and SSH abnormal notifications with typed payload/open evidence",
            validate_qa_build_record.missing_required_fields(missing_document_payload),
        )

        missing_digital_employee_payload = validate_qa_build_record.parse_record(
            complete_record().replace(
                "digital-employee-task:digital-employee-task-id-12345",
                "digital-employee-task:",
            ),
        )

        self.assertIn(
            "Notification delivery evidence must describe delivered document, digital employee, and SSH abnormal notifications with typed payload/open evidence",
            validate_qa_build_record.missing_required_fields(missing_digital_employee_payload),
        )

    def test_notification_delivery_must_reference_recorded_task_ids(self) -> None:
        missing_document_job = validate_qa_build_record.parse_record(
            complete_record().replace(
                "document-export:pdf-export-job-id-12345",
                "document-export:export-unknown-42",
            ),
        )

        self.assertIn(
            "Notification delivery evidence must reference a recorded document export job ID",
            validate_qa_build_record.missing_required_fields(missing_document_job),
        )

        missing_digital_employee_task = validate_qa_build_record.parse_record(
            complete_record().replace(
                "digital-employee-task:digital-employee-task-id-12345",
                "digital-employee-task:unknown-task-42",
            ),
        )

        self.assertIn(
            "Notification delivery evidence must reference a recorded digital employee task ID",
            validate_qa_build_record.missing_required_fields(missing_digital_employee_task),
        )

    def test_official_connection_fields_must_describe_hub_tenant_and_bootstrap(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Account screen shows selected Hub and tenant: Account screen shows selected Hub https://tenant-a.maclaw.top and tenant tenant-a; screenshot account-hub-tenant-42",
                "Account screen shows selected Hub and tenant: Account page screenshot captured during QA run",
            )
            .replace(
                "No custom Hub URL setting found: No custom Hub URL setting surface found in account/settings UI; screenshot no-custom-hub-url-42",
                "No custom Hub URL setting found: Settings screen screenshot captured during QA run",
            )
            .replace(
                "Bootstrap user/quota/feature flags/service status: Bootstrap response shows user phone:8613800138000 for tenant tenant-a, quota limits, feature flags, and service status; screenshot bootstrap-status-42",
                "Bootstrap user/quota/feature flags/service status: Bootstrap smoke screenshot captured during QA run",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Account screen shows selected Hub and tenant must describe selected Hub and tenant account-screen evidence",
            missing,
        )
        self.assertIn(
            "No custom Hub URL setting found must describe absence of custom Hub URL settings",
            missing,
        )
        self.assertIn(
            "Bootstrap user/quota/feature flags/service status must describe bootstrap user/quota/features/service status",
            missing,
        )

    def test_bootstrap_evidence_must_reference_recorded_phone_and_tenant(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Bootstrap user/quota/feature flags/service status: Bootstrap response shows user phone:8613800138000 for tenant tenant-a, quota limits, feature flags, and service status; screenshot bootstrap-status-42",
                "Bootstrap user/quota/feature flags/service status: Bootstrap response shows user for tenant tenant-b, quota limits, feature flags, and service status; screenshot bootstrap-status-42",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Bootstrap user/quota/feature flags/service status must reference the recorded MaClaw phone account",
            missing,
        )
        self.assertIn(
            "Bootstrap user/quota/feature flags/service status must reference the recorded Tenant ID",
            missing,
        )

    def test_account_screen_hub_tenant_must_match_recorded_values(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Account screen shows selected Hub and tenant: Account screen shows selected Hub https://tenant-a.maclaw.top and tenant tenant-a; screenshot account-hub-tenant-42",
                "Account screen shows selected Hub and tenant: Account screen shows selected Hub https://tenant-b.maclaw.top and tenant tenant-b; screenshot account-hub-tenant-42",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Account screen shows selected Hub and tenant must reference the recorded Discovered Hub URL",
            missing,
        )
        self.assertIn(
            "Account screen shows selected Hub and tenant must reference the recorded Tenant ID",
            missing,
        )

    def test_login_result_must_describe_official_hubcenter_login(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Login result: {LOGIN_RESULT_CONTEXT}",
                "Login result: Login screenshot captured during QA run",
            ),
        )

        self.assertIn(
            "Login result must describe phone/SMS login through HubCenter and official credits binding",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_login_result_must_name_phone_sms_and_credits_binding(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Login result: {LOGIN_RESULT_CONTEXT}",
                "Login result: MaClaw official account authenticated through HubCenter and mobile session opened; screenshot login-result-42",
            ),
        )

        self.assertIn(
            "Login result must describe phone/SMS login through HubCenter and official credits binding",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_login_result_must_prove_first_llm_call_uses_phone_credits(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Login result: {LOGIN_RESULT_CONTEXT}",
                "Login result: MaClaw official phone account phone:8613800138000 "
                "authenticated through HubCenter after SMS verification code; "
                "mobile session opened with official credits bound to the phone "
                "account; screenshot login-result-42",
            ),
        )

        self.assertIn(
            "Login result must describe phone/SMS login through HubCenter and official credits binding",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_login_result_requires_digits_only_phone_credits(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                f"Login result: {LOGIN_RESULT_CONTEXT}",
                "Login result: MaClaw official phone account phone:+8613800138000 "
                "authenticated through HubCenter after SMS verification code; "
                "mobile session opened with official credits bound to the phone "
                "account; first LLM call after verification used "
                "phone:+8613800138000 credits; screenshot login-result-42",
            ),
        )

        self.assertIn(
            "Login result must describe phone/SMS login through HubCenter and official credits binding",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_login_result_requires_trackable_sms_verification_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                " sms-verification:sms-login-12345",
                "",
            ),
        )

        self.assertIn(
            "Login result must include a trackable SMS verification ID",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_network_recovery_evidence_must_describe_offline_and_recovered_services(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Network offline/recovery evidence: Network offline warning shown when HubCenter was unreachable; network-recovery-id-12345 captured the offline and restored probes; after recovery selected HubCenter https://hubs.maclaw.top and discovered Hub https://tenant-a.maclaw.top for tenant tenant-a returned online status, while assistant online answers, document export pdf-export-job-id-12345, digital employee task digital-employee-task-id-12345, and realtime surfaces resumed; screenshot network-recovery-42",
                "Network offline/recovery evidence: Network status screenshot captured during QA run",
            ),
        )

        self.assertIn(
            "Network offline/recovery evidence must describe offline warning and recovered HubCenter network/service evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_network_recovery_evidence_must_reference_recorded_hub_and_task(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Network offline/recovery evidence: Network offline warning shown when HubCenter was unreachable; network-recovery-id-12345 captured the offline and restored probes; after recovery selected HubCenter https://hubs.maclaw.top and discovered Hub https://tenant-a.maclaw.top for tenant tenant-a returned online status, while assistant online answers, document export pdf-export-job-id-12345, digital employee task digital-employee-task-id-12345, and realtime surfaces resumed; screenshot network-recovery-42",
                "Network offline/recovery evidence: Network offline warning shown when HubCenter was unreachable; after recovery HubCenter online status returned and assistant online answers, document export, digital employee, and realtime surfaces resumed; screenshot network-recovery-42",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Network offline/recovery evidence must reference the selected HubCenter URL",
            missing,
        )
        self.assertIn(
            "Network offline/recovery evidence must reference the recorded Discovered Hub URL",
            missing,
        )
        self.assertIn(
            "Network offline/recovery evidence must reference the recorded Tenant ID",
            missing,
        )
        self.assertIn(
            "Network offline/recovery evidence must reference a recorded task/job ID",
            missing,
        )

    def test_network_recovery_evidence_requires_trace_id(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Network offline/recovery evidence: Network offline warning shown when HubCenter was unreachable; network-recovery-id-12345 captured the offline and restored probes; after recovery selected HubCenter https://hubs.maclaw.top and discovered Hub https://tenant-a.maclaw.top for tenant tenant-a returned online status, while assistant online answers, document export pdf-export-job-id-12345, digital employee task digital-employee-task-id-12345, and realtime surfaces resumed; screenshot network-recovery-42",
                "Network offline/recovery evidence: Network offline warning shown when HubCenter was unreachable; after recovery selected HubCenter https://hubs.maclaw.top and discovered Hub https://tenant-a.maclaw.top for tenant tenant-a returned online status, while assistant online answers, document export pdf-export-job-id-12345, digital employee task digital-employee-task-id-12345, and realtime surfaces resumed; screenshot network-recovery-42",
            ),
        )

        self.assertIn(
            "Network offline/recovery evidence must include a trackable network recovery trace ID",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_device_model_os_requires_platform_and_version(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace("Pixel 8 / Android 14 QA device", "Pixel QA phone")
            .replace("iPhone 15 / iOS 18 TestFlight QA device", "iPhone QA phone"),
        )

        self.assertIn(
            "Device model / OS must include device model and Android/iOS OS version",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_device_model_os_requires_android_and_ios_entries(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "iPhone 15 / iOS 18 TestFlight QA device",
                "Pixel 9 / Android 15 second QA device",
            ),
        )

        self.assertIn(
            "Device model / OS must include both Android and iOS devices",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_device_model_os_requires_android_13_or_newer(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Pixel 8 / Android 14 QA device",
                "Pixel 6 / Android 12 QA device",
            ),
        )

        self.assertIn(
            "Device model / OS must include at least one Android 13+ device",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_device_model_os_entries_must_match_android_then_ios_sections(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Device model / OS: Pixel 8 / Android 14 QA device\n"
                "Android signed install result:",
                "Device model / OS: iPhone 15 / iOS 18 TestFlight QA device\n"
                "Android signed install result:",
            ).replace(
                "Device model / OS: iPhone 15 / iOS 18 TestFlight QA device\n"
                "iOS signed install result:",
                "Device model / OS: Pixel 8 / Android 14 QA device\n"
                "iOS signed install result:",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "First Device model / OS entry must be the Android QA device",
            missing,
        )
        self.assertIn(
            "Second Device model / OS entry must be the iOS QA device",
            missing,
        )

    def test_optional_digital_employee_handoff_warning_must_be_auditable_when_present(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record() + "\nDigital employee handoff warning, if used: ok",
        )

        self.assertIn(
            "Digital employee handoff warning, if used must contain auditable QA evidence, not a placeholder",
            validate_qa_build_record.missing_required_fields(values),
        )

        vague_values = validate_qa_build_record.parse_record(
            complete_record()
            + "\nDigital employee handoff warning, if used: Screenshot qa-ssh-42 shows digital employee task submit sheet",
        )

        self.assertIn(
            "Digital employee handoff warning, if used must describe Hub/tenant handoff warning evidence",
            validate_qa_build_record.missing_required_fields(vague_values),
        )

        auditable_values = validate_qa_build_record.parse_record(
            complete_record()
            + "\nDigital employee handoff warning, if used: Screenshot qa-ssh-42 shows Hub tenant handoff warning confirmation before digital employee receives SSH terminal copied output",
        )

        self.assertEqual(
            [],
            validate_qa_build_record.missing_required_fields(auditable_values),
        )

    def test_optional_attachment_and_known_issue_fields_must_be_auditable_when_present(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            + "\nDevice logs / screenshots / recordings: ok"
            + "\nScreenshots / recordings: yes"
            + "\nKnown issues / waivers: done",
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        for expected in [
            "Device logs / screenshots / recordings must contain auditable QA evidence, not a placeholder",
            "Screenshots / recordings must contain auditable QA evidence, not a placeholder",
            "Known issues / waivers must contain auditable QA evidence, not a placeholder",
        ]:
            self.assertIn(expected, missing)

        auditable_values = validate_qa_build_record.parse_record(
            complete_record()
            + "\nDevice logs / screenshots / recordings: Android share recording qa-android-share-42.mp4"
            + "\nScreenshots / recordings: iOS permission screenshots qa-ios-perm-42.zip"
            + "\nKnown issues / waivers: No known release issues for signed QA build 42",
        )

        self.assertEqual(
            [],
            validate_qa_build_record.missing_required_fields(auditable_values),
        )

    def test_optional_attachment_fields_must_reference_real_artifacts(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            + "\nDevice logs / screenshots / recordings: Android evidence captured during QA run"
            + "\nScreenshots / recordings: iOS permission evidence captured during QA run",
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Device logs / screenshots / recordings must reference a traceable evidence file or attachment ID",
            missing,
        )
        self.assertIn(
            "Screenshots / recordings must reference a traceable evidence file or attachment ID",
            missing,
        )

        artifact_values = validate_qa_build_record.parse_record(
            complete_record()
            + "\nDevice logs / screenshots / recordings: Android device log qa-android-share-42.log"
            + "\nScreenshots / recordings: iOS permission recording attachment qa-ios-perm-42",
        )

        self.assertEqual(
            [],
            validate_qa_build_record.missing_required_fields(artifact_values),
        )

    def test_build_identity_and_signing_fields_reject_placeholders(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace("Tester: qa-operator", "Tester: ok")
            .replace("Branch: codex/mobile-release", "Branch: ok")
            .replace("Flutter version: Flutter 3.41.5", "Flutter version: soon")
            .replace("MaClaw account: phone:8613800138000", "MaClaw account: yes")
            .replace("Tenant ID: tenant-a", "Tenant ID: ok")
            .replace("Version/build number: 1.0.0+42", "Version/build number: ok")
            .replace(
                "Signing identity: Android release keystore alias maclaw-mobile",
                "Signing identity: done",
            )
            .replace("Installer channel: internal app sharing", "Installer channel: ok")
            .replace("Approved by: release-owner", "Approved by: ok"),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        for expected in [
            "Tester must contain trackable QA evidence, not a placeholder",
            "Branch must contain trackable QA evidence, not a placeholder",
            "Flutter version must contain a trackable Flutter version",
            "MaClaw account must contain trackable QA evidence, not a placeholder",
            "Tenant ID must contain trackable QA evidence, not a placeholder",
            "Version/build number must contain trackable QA evidence, not a placeholder",
            "Signing identity must contain trackable QA evidence, not a placeholder",
            "Installer channel must contain trackable QA evidence, not a placeholder",
            "Approved by must contain trackable QA evidence, not a placeholder",
        ]:
            self.assertIn(expected, missing)

    def test_signing_identity_rejects_debug_keystore(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Signing identity: Android release keystore alias maclaw-mobile",
                "Signing identity: Android debug keystore",
            ),
        )

        self.assertIn(
            "Signing identity must identify a non-debug release/internal signing identity",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_signing_identity_requires_trackable_alias_or_certificate(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Signing identity: Android release keystore alias maclaw-mobile",
                "Signing identity: Android release keystore maclaw-mobile",
            ),
        )

        self.assertIn(
            "Signing identity must identify a non-debug release/internal signing identity",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_signing_identity_accepts_upload_certificate_evidence(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Signing identity: Android release keystore alias maclaw-mobile",
                "Signing identity: Play App Signing upload certificate QA-42",
            ),
        )

        self.assertEqual([], validate_qa_build_record.missing_required_fields(values))

    def test_installer_channel_rejects_debug_or_generic_release_labels(self) -> None:
        for channel in (
            "debug sideload",
            "release build",
            "TestFlight build 42",
            "App Store Connect internal test",
        ):
            with self.subTest(channel=channel):
                values = validate_qa_build_record.parse_record(
                    complete_record().replace(
                        "Installer channel: internal app sharing",
                        f"Installer channel: {channel}",
                    ),
                )

                self.assertIn(
                    "Installer channel must identify a non-debug auditable distribution channel",
                    validate_qa_build_record.missing_required_fields(values),
                )

    def test_installer_channel_accepts_play_internal_testing(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Installer channel: internal app sharing",
                "Installer channel: Play internal testing track build 42",
            ),
        )

        self.assertEqual([], validate_qa_build_record.missing_required_fields(values))

    def test_hub_discovery_smoke_fields_are_required_and_auditable(self) -> None:
        missing_record = (
            complete_record()
            .replace(
                "HubCenter probe result: HubCenter probe candidates https://hubs.mypapers.top, https://hubs.maclaw.top, https://hubs2.maclaw.top selected https://hubs.maclaw.top; screenshot hubcenter-probe-42\n",
                "",
            )
            .replace(
                "Discovered Hub/tenant result: Discovered Hub https://tenant-a.maclaw.top for tenant tenant-a; screenshot discovered-hub-tenant-42\n",
                "",
            )
            .replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}\n",
                "",
            )
        )

        missing = validate_qa_build_record.missing_required_fields(
            validate_qa_build_record.parse_record(missing_record),
        )

        self.assertIn("HubCenter probe result", missing)
        self.assertIn("Discovered Hub/tenant result", missing)
        self.assertIn("LLM access evidence", missing)

        placeholder_values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "HubCenter probe result: HubCenter probe candidates https://hubs.mypapers.top, https://hubs.maclaw.top, https://hubs2.maclaw.top selected https://hubs.maclaw.top; screenshot hubcenter-probe-42",
                "HubCenter probe result: ok",
            )
            .replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: yes",
            ),
        )

        placeholder_missing = validate_qa_build_record.missing_required_fields(
            placeholder_values,
        )
        self.assertIn(
            "HubCenter probe result must contain auditable QA evidence, not a placeholder",
            placeholder_missing,
        )
        self.assertIn(
            "LLM access evidence must contain auditable QA evidence, not a placeholder",
            placeholder_missing,
        )

        vague_values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "HubCenter probe result: HubCenter probe candidates https://hubs.mypapers.top, https://hubs.maclaw.top, https://hubs2.maclaw.top selected https://hubs.maclaw.top; screenshot hubcenter-probe-42",
                "HubCenter probe result: HubCenter probe screenshot captured during QA run",
            )
            .replace(
                "Discovered Hub/tenant result: Discovered Hub https://tenant-a.maclaw.top for tenant tenant-a; screenshot discovered-hub-tenant-42",
                "Discovered Hub/tenant result: Tenant routing screenshot captured during QA run",
            )
            .replace(
                f"LLM access evidence: {OFFICIAL_LLM_ACCESS_CONTEXT}",
                "LLM access evidence: LLM screenshot captured during QA run",
            ),
        )

        vague_missing = validate_qa_build_record.missing_required_fields(vague_values)
        self.assertIn(
            "HubCenter probe result must describe probing exactly the three official HubCenters",
            vague_missing,
        )
        self.assertIn(
            "Discovered Hub/tenant result must describe discovered Hub URL and tenant evidence",
            vague_missing,
        )
        self.assertIn(
            "LLM access evidence must describe MaClaw official or desktop QR LLM access evidence",
            vague_missing,
        )

    def test_final_decision_fields_must_say_passed_or_waived(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Android manual gates passed: passed",
                "Android manual gates passed: ok",
            ),
        )

        self.assertIn(
            "Android manual gates passed must say passed or waived",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_automated_gate_evidence_fields_are_required(self) -> None:
        record = complete_record()
        for field in (
            "Release handoff result",
            "Runtime boundary verification result",
            "Automated release gates result",
        ):
            record = re.sub(rf"^{re.escape(field)}: .*$", "", record, flags=re.MULTILINE)

        missing = validate_qa_build_record.missing_required_fields(
            validate_qa_build_record.parse_record(record),
        )

        self.assertIn("Release handoff result", missing)
        self.assertIn("Runtime boundary verification result", missing)
        self.assertIn("Automated release gates result", missing)

    def test_automated_gate_evidence_fields_must_match_expected_artifacts(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "Release handoff result: release_handoff.py output saved to docs/qa-builds/handoff-1.0.0+42.md",
                "Release handoff result: QA evidence screenshot captured for release approval",
            )
            .replace(
                "Runtime boundary verification result: MaClaw Mobile runtime boundary verified; log: docs/qa-builds/runtime-boundary-1.0.0+42.log",
                "Runtime boundary verification result: QA evidence screenshot captured for release approval",
            )
            .replace(
                "Automated release gates result: run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log",
                "Automated release gates result: QA evidence screenshot captured for release approval",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn(
            "Release handoff result must reference release_handoff.py output or saved handoff evidence",
            missing,
        )
        self.assertIn(
            "Runtime boundary verification result must reference verify_runtime_boundary.py verified output or log evidence",
            missing,
        )
        self.assertIn(
            "Automated release gates result must reference run_release_gates.py gate count and saved log evidence",
            missing,
        )

    def test_automated_gate_evidence_requires_current_gate_count(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Automated release gates result: run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log",
                "Automated release gates result: run_release_gates.py: 36 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log",
            ),
        )

        self.assertIn(
            "Automated release gates result must reference run_release_gates.py gate count and saved log evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_automated_gate_evidence_accepts_real_runner_success_line(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Automated release gates result: run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log",
                "Automated release gates result: run_release_gates.py log docs/qa-builds/release-gates-1.0.0+42.log includes "
                + validate_qa_build_record.AUTOMATED_RELEASE_GATE_SUCCESS_LINE,
            ),
        )

        self.assertNotIn(
            "Automated release gates result must reference run_release_gates.py gate count and saved log evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_automated_gate_evidence_rejects_legacy_uncounted_success_line(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Automated release gates result: run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log",
                "Automated release gates result: run_release_gates.py log docs/qa-builds/release-gates-1.0.0+42.log includes "
                "All MaClaw Mobile automated release gates passed.",
            ),
        )

        self.assertIn(
            "Automated release gates result must reference run_release_gates.py gate count and saved log evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_automated_gate_evidence_rejects_generic_all_gates_passed(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Automated release gates result: run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log",
                "Automated release gates result: run_release_gates.py log docs/qa-builds/release-gates-1.0.0+42.log says all gates passed.",
            ),
        )

        self.assertIn(
            "Automated release gates result must reference run_release_gates.py gate count and saved log evidence",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_final_decision_yes_is_not_specific_enough(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Android manual gates passed: passed",
                "Android manual gates passed: yes",
            ),
        )

        self.assertIn(
            "Android manual gates passed must say passed or waived",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_final_decision_negative_passed_phrase_is_rejected(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Android manual gates passed: passed",
                "Android manual gates passed: not passed because share evidence is missing",
            ),
        )

        self.assertIn(
            "Android manual gates passed must say passed or waived",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_final_decision_passed_with_evidence_note_is_accepted(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Android manual gates passed: passed",
                "Android manual gates passed: passed with QA record QA-20260702-A",
            ),
        )

        self.assertEqual([], validate_qa_build_record.missing_required_fields(values))

    def test_final_decision_placeholders_are_not_accepted(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Manual SSH smoke passed: passed",
                "Manual SSH smoke passed: passed / waived with reason",
            ),
        )

        self.assertIn(
            "Manual SSH smoke passed must say passed or waived",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_final_decision_waivers_must_include_reason(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record()
            .replace(
                "iOS manual gates passed: passed",
                "iOS manual gates passed: waived",
            )
            .replace(
                "Hub discovery smoke passed: passed",
                "Hub discovery smoke passed: waived with reason",
            )
            .replace(
                "Manual SSH smoke passed: passed",
                "Manual SSH smoke passed: \u8c41\u514d",
            ),
        )

        missing = validate_qa_build_record.missing_required_fields(values)

        self.assertIn("iOS manual gates passed must say passed or waived", missing)
        self.assertIn("Hub discovery smoke passed must say passed or waived", missing)
        self.assertIn("Manual SSH smoke passed must say passed or waived", missing)

    def test_final_decision_fields_accept_chinese_pass_or_waiver(self) -> None:
        values = validate_qa_build_record.parse_record(
            (
                complete_record()
                + "\nKnown issues / waivers: Manual SSH waiver recorded in QA exception ticket QA-42"
            )
            .replace(
                "Hub discovery smoke passed: passed",
                "Hub discovery smoke passed: \u5df2\u901a\u8fc7",
            )
            .replace(
                "Manual SSH smoke passed: passed",
                "Manual SSH smoke passed: \u8c41\u514d: QA server unavailable",
            ),
        )

        self.assertEqual([], validate_qa_build_record.missing_required_fields(values))

    def test_final_decision_fields_accept_english_waiver_with_reason(self) -> None:
        values = validate_qa_build_record.parse_record(
            (
                complete_record()
                + "\nKnown issues / waivers: Manual SSH waiver recorded in QA exception ticket QA-42"
            )
            .replace(
                "Manual SSH smoke passed: passed",
                "Manual SSH smoke passed: waived with reason: QA server unavailable",
            ),
        )

        self.assertEqual([], validate_qa_build_record.missing_required_fields(values))

    def test_final_decision_waivers_must_be_summarized(self) -> None:
        values = validate_qa_build_record.parse_record(
            complete_record().replace(
                "Manual SSH smoke passed: passed",
                "Manual SSH smoke passed: waived with reason: QA server unavailable",
            ),
        )

        self.assertIn(
            "Known issues / waivers must summarize every final gate waiver",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_waiver_summary_must_name_each_waived_final_gate(self) -> None:
        values = validate_qa_build_record.parse_record(
            (
                complete_record()
                + "\nKnown issues / waivers: Manual SSH waiver recorded in QA exception ticket QA-42"
            )
            .replace(
                "Android manual gates passed: passed",
                "Android manual gates passed: waived with reason: signed Android device unavailable",
            )
            .replace(
                "Manual SSH smoke passed: passed",
                "Manual SSH smoke passed: waived with reason: QA server unavailable",
            ),
        )

        self.assertIn(
            "Known issues / waivers must summarize every final gate waiver",
            validate_qa_build_record.missing_required_fields(values),
        )

        complete_values = validate_qa_build_record.parse_record(
            (
                complete_record()
                + "\nKnown issues / waivers: Android signed-device waiver and Manual SSH waiver recorded in QA exception ticket QA-42"
            )
            .replace(
                "Android manual gates passed: passed",
                "Android manual gates passed: waived with reason: signed Android device unavailable",
            )
            .replace(
                "Manual SSH smoke passed: passed",
                "Manual SSH smoke passed: waived with reason: QA server unavailable",
            ),
        )

        self.assertEqual(
            [],
            validate_qa_build_record.missing_required_fields(complete_values),
        )

    def test_waiver_summary_must_include_trackable_ticket_or_approval(
        self,
    ) -> None:
        values = validate_qa_build_record.parse_record(
            (
                complete_record()
                + "\nKnown issues / waivers: Manual SSH waiver recorded after release-owner approval"
            ).replace(
                "Manual SSH smoke passed: passed",
                "Manual SSH smoke passed: waived with reason: QA server unavailable",
            ),
        )

        self.assertIn(
            "Known issues / waivers must include a trackable waiver ticket or approval reference",
            validate_qa_build_record.missing_required_fields(values),
        )

    def test_parser_accepts_utf8_bom_on_first_field(self) -> None:
        values = validate_qa_build_record.parse_record("\ufeffDate: 2026-07-02\n")

        self.assertEqual(["2026-07-02"], values["Date"])

    def test_parser_accepts_known_field_names_that_contain_urls(self) -> None:
        values = validate_qa_build_record.parse_record(
            "Account screen shows selected Hub and tenant: yes\n",
        )

        self.assertEqual(
            ["yes"],
            values["Account screen shows selected Hub and tenant"],
        )

    def test_cli_returns_nonzero_for_incomplete_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "record.md"
            record.write_text("Date: 2026-07-02\n", encoding="utf-8")

            output = StringIO()
            with redirect_stdout(output):
                self.assertEqual(1, validate_qa_build_record.main([str(record)]))
            self.assertIn("QA build record validation failed", output.getvalue())

    def test_cli_returns_nonzero_and_clear_message_for_raw_secret(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nKnown issues / waivers: token=SuperSecretToken123456",
                encoding="utf-8",
            )

            output = StringIO()
            with redirect_stdout(output):
                self.assertEqual(1, validate_qa_build_record.main([str(record)]))

            text = output.getvalue()
            self.assertIn("QA build record validation failed", text)
            self.assertIn("raw secrets", text)
            self.assertIn("redacted evidence or attachment IDs", text)

    def test_validate_file_rejects_directory_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            errors = validate_qa_build_record.validate_file(Path(tmp))

        self.assertIn("QA build record path must be a markdown file", errors[0])

    def test_validate_file_rejects_non_markdown_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.txt"
            record.write_text(complete_record(), encoding="utf-8")

            self.assertEqual(
                [f"QA build record path must be a markdown file: {record}"],
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_readme_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            readme = Path(tmp) / "README.md"
            readme.write_text("# QA records\n", encoding="utf-8")

            self.assertEqual(
                ["QA build record path must point to a completed record, not README.md"],
                validate_qa_build_record.validate_file(readme),
            )

    def test_validate_file_rejects_template_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            template = Path(tmp) / "qa_build_record_template.md"
            template.write_text("Date: YYYY-MM-DD\n", encoding="utf-8")

            self.assertEqual(
                ["QA build record path must point to a completed record, not the template"],
                validate_qa_build_record.validate_file(template),
            )

    def test_validate_file_accepts_named_qa_build_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            qa_builds = Path(tmp) / "qa-builds"
            qa_builds.mkdir()
            record = qa_builds / "2026-07-02-android-ios-1.0.0+42.md"
            artifact = (
                qa_builds
                / "build"
                / "app"
                / "outputs"
                / "flutter-apk"
                / "app-release.apk"
            )
            artifact.parent.mkdir(parents=True)
            artifact.write_bytes(b"signed release apk bytes")
            digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
            record.write_text(
                complete_record().replace("SHA256: " + "a" * 64, f"SHA256: {digest}"),
                encoding="utf-8",
            )

            self.assertEqual([], validate_qa_build_record.validate_file(record))

    def test_validate_file_rejects_bad_qa_build_record_filename(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            qa_builds = Path(tmp) / "qa-builds"
            qa_builds.mkdir()
            record = qa_builds / "qa-record.md"
            record.write_text(complete_record(), encoding="utf-8")

            self.assertIn(
                "QA build record filename must be YYYY-MM-DD-<android|ios|android-ios>-<version+build>.md",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_legacy_ios_android_scope_filename(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            qa_builds = Path(tmp) / "qa-builds"
            qa_builds.mkdir()
            record = qa_builds / "2026-07-02-ios-android-1.0.0+42.md"
            record.write_text(complete_record(), encoding="utf-8")

            self.assertIn(
                "QA build record filename must be YYYY-MM-DD-<android|ios|android-ios>-<version+build>.md",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_qa_build_record_filename_date_mismatch(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            qa_builds = Path(tmp) / "qa-builds"
            qa_builds.mkdir()
            record = qa_builds / "2026-07-01-android-ios-1.0.0+42.md"
            record.write_text(complete_record(), encoding="utf-8")

            self.assertIn(
                "QA build record filename date must match Date",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_qa_build_record_filename_version_mismatch(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            qa_builds = Path(tmp) / "qa-builds"
            qa_builds.mkdir()
            record = qa_builds / "2026-07-02-android-ios-1.0.0+43.md"
            record.write_text(complete_record(), encoding="utf-8")

            self.assertIn(
                "QA build record filename version/build must match Version/build number",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_raw_password_assignment(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record().replace(
                    "Command output excerpt: Command output excerpt for server-profile:srv-prod shows stdout for whoami: qa-user; screenshot ssh-output-42",
                    "Command output excerpt: Command output excerpt for server-profile:srv-prod shows stdout password=SuperSecret123; screenshot ssh-output-42",
                ),
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_private_key_block(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nDevice logs / screenshots / recordings: -----BEGIN OPENSSH PRIVATE KEY-----",
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_literal_api_tokens(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nKnown issues / waivers: leaked token sk-abcdefghijklmnopqrstuvwxyz123456",
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_literal_cloud_access_key_ids(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nManual SSH notes: cloud log showed key id AKIAIOSFODNN7EXAMPLE",
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_literal_google_api_keys(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nPhoto assistant notes: image API key AIzaabcdefghijklmnopqrstuvwxyz123456789",
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_literal_jwt_tokens(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nHub discovery notes: bootstrap token eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ0ZW5hbnQiOiJxYSIsInN1YiI6InVzZXIifQ.c2lnbmF0dXJlMTIzNDU2Nzg5",
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_authorization_bearer_headers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nDevice logs / screenshots / recordings: Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456",
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_authorization_basic_headers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nDevice logs / screenshots / recordings: Authorization: Basic dXNlcjpzdXBlcnNlY3JldA==",
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_cookie_headers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nHub discovery notes: Cookie: session=abcdefghijklmnopqrstuvwxyz; tenant=qa",
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_set_cookie_headers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nHub discovery notes: Set-Cookie: maclaw_session=abcdefghijklmnopqrstuvwxyz; HttpOnly",
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_private_token_headers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nHub discovery notes: PRIVATE-TOKEN: abcdefghijklmnopqrstuvwxyz123456",
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )

    def test_validate_file_rejects_url_embedded_credentials(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = Path(tmp) / "qa-record.md"
            record.write_text(
                complete_record()
                + "\nManual SSH notes: reproduced with ssh://qa-user:SuperSecret123@server.example.com:22",
                encoding="utf-8",
            )

            self.assertIn(
                "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
                validate_qa_build_record.validate_file(record),
            )


if __name__ == "__main__":
    unittest.main()
