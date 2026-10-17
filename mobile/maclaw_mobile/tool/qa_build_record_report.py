from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path

import release_evidence_commands
import validate_qa_build_record


_DEFAULT_FINAL_PREFILLS = release_evidence_commands.final_decision_prefills()
_FINAL_ARTIFACT_VERSION_NOTE = (
    " Artifact/log filenames must include the same Version/build number as the QA record."
)

EVIDENCE_FIELD_HINTS = {
    "Release handoff result": (
        f"Use `{_DEFAULT_FINAL_PREFILLS['Release handoff result']}` "
        f"after running `{release_evidence_commands.release_handoff_command()}`."
        + _FINAL_ARTIFACT_VERSION_NOTE
    ),
    "Preflight result": (
        f"Use `{_DEFAULT_FINAL_PREFILLS['Preflight result']}` after running "
        f"`{release_evidence_commands.qa_preflight_command(log=release_evidence_commands.preflight_log_path())}`."
        + _FINAL_ARTIFACT_VERSION_NOTE
    ),
    "Runtime boundary verification result": (
        f"Use `{_DEFAULT_FINAL_PREFILLS['Runtime boundary verification result']}` after running "
        f"`{release_evidence_commands.runtime_boundary_command()}`."
        + _FINAL_ARTIFACT_VERSION_NOTE
    ),
    "Automated release gates result": (
        f"Use `{_DEFAULT_FINAL_PREFILLS['Automated release gates result']}` after running "
        f"`{release_evidence_commands.release_gates_command()}`."
        + _FINAL_ARTIFACT_VERSION_NOTE
    ),
}


def _evidence_field_hints(
    version: str = release_evidence_commands.DEFAULT_VERSION,
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
    records_dir: str = release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
) -> dict[str, str]:
    prefills = release_evidence_commands.final_decision_prefills(
        version,
        scope=scope,
        records_dir=records_dir,
    )
    return {
        "Release handoff result": (
            f"Use `{prefills['Release handoff result']}` "
            "after running `"
            + release_evidence_commands.release_handoff_command(
                version=version,
                scope=scope,
                records_dir=records_dir,
            )
            + "`."
            + _FINAL_ARTIFACT_VERSION_NOTE
        ),
        "Preflight result": (
            f"Use `{prefills['Preflight result']}` after running `"
            + release_evidence_commands.qa_preflight_command(
                scope=scope,
                log=release_evidence_commands.preflight_log_path(
                    version,
                    scope=scope,
                    records_dir=records_dir,
                ),
                records_dir=records_dir,
            )
            + "`."
            + _FINAL_ARTIFACT_VERSION_NOTE
        ),
        "Runtime boundary verification result": (
            f"Use `{prefills['Runtime boundary verification result']}` after running "
            f"`{release_evidence_commands.runtime_boundary_command(version, records_dir=records_dir)}`."
            + _FINAL_ARTIFACT_VERSION_NOTE
        ),
        "Automated release gates result": (
            f"Use `{prefills['Automated release gates result']}` after running "
            f"`{release_evidence_commands.release_gates_command(version, records_dir=records_dir)}`."
            + _FINAL_ARTIFACT_VERSION_NOTE
        ),
    }


@dataclass(frozen=True)
class RequiredEvidenceProgress:
    filled: int
    required: int


@dataclass(frozen=True)
class QaBuildRecordReport:
    path: Path
    passed: bool
    progress: RequiredEvidenceProgress | None
    path_errors: list[str]
    secret_errors: list[str]
    evidence_errors: list[str]
    artifact_errors: list[str]
    unknown_errors: list[str]

    @property
    def errors(self) -> list[str]:
        return (
            self.path_errors
            + self.secret_errors
            + self.evidence_errors
            + self.artifact_errors
            + self.unknown_errors
        )


def _filled_required_count(values: dict[str, list[str]], field: str) -> int:
    return sum(1 for value in values.get(field, []) if value.strip())


def required_evidence_progress(
    values: dict[str, list[str]],
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
) -> RequiredEvidenceProgress:
    values = validate_qa_build_record.scoped_values(values, scope)
    filled = 0
    required = 0
    for field, required_count in validate_qa_build_record.required_field_counts_for_scope(
        scope,
    ).items():
        required += required_count
        filled += min(_filled_required_count(values, field), required_count)
    return RequiredEvidenceProgress(filled=filled, required=required)


def _path_validation_errors(path: Path) -> list[str]:
    if not path.exists():
        return [f"QA build record file does not exist: {path}"]
    if path.is_dir():
        return [f"QA build record path must be a markdown file, not a directory: {path}"]
    if path.suffix.lower() != ".md":
        return [f"QA build record path must be a markdown file: {path}"]
    if path.name.lower() == "readme.md":
        return ["QA build record path must point to a completed record, not README.md"]
    if path.name.lower().startswith("handoff-") and path.suffix.lower() == ".md":
        return [
            "QA build record path must point to a completed record, not release handoff evidence",
        ]
    if path.name == "qa_build_record_template.md":
        return ["QA build record path must point to a completed record, not the template"]
    return []


def generate_report(path: Path) -> QaBuildRecordReport:
    validation_errors = validate_qa_build_record.validate_file(path)
    path_errors = _path_validation_errors(path)
    if path_errors:
        return QaBuildRecordReport(
            path=path,
            passed=False,
            progress=None,
            path_errors=path_errors,
            secret_errors=[],
            evidence_errors=[],
            artifact_errors=[],
            unknown_errors=[],
        )

    text = path.read_text(encoding="utf-8")
    values = validate_qa_build_record.parse_record(text)
    scope = validate_qa_build_record.record_scope_from_path(path)
    in_scope_values = validate_qa_build_record.scoped_values(values, scope)
    secret_errors = validate_qa_build_record.raw_secret_errors(text)
    filename_errors = validate_qa_build_record.qa_build_record_filename_errors(
        path,
        in_scope_values,
    )
    evidence_errors = validate_qa_build_record.missing_required_fields(
        values,
        scope=scope,
    )
    artifact_errors = validate_qa_build_record.local_artifact_errors(
        in_scope_values,
        path.parent,
    )
    known_errors = set(
        path_errors + secret_errors + filename_errors + evidence_errors + artifact_errors
    )

    return QaBuildRecordReport(
        path=path,
        passed=not validation_errors,
        progress=required_evidence_progress(values, scope=scope),
        path_errors=path_errors + filename_errors,
        secret_errors=secret_errors,
        evidence_errors=evidence_errors,
        artifact_errors=artifact_errors,
        unknown_errors=[error for error in validation_errors if error not in known_errors],
    )


def _format_section(title: str, errors: list[str]) -> list[str]:
    if not errors:
        return []
    lines = [f"{title}:"]
    lines.extend(f"- {error}" for error in errors)
    return lines


def _evidence_hints(errors: list[str]) -> list[str]:
    return _evidence_hints_for_version(
        errors,
        release_evidence_commands.DEFAULT_VERSION,
    )


def _evidence_hints_for_version(
    errors: list[str],
    version: str,
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
    records_dir: str = release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
) -> list[str]:
    hints_by_field = _evidence_field_hints(
        version,
        scope=scope,
        records_dir=records_dir,
    )
    hints = []
    for field, hint in hints_by_field.items():
        if any(error == field or error.startswith(f"{field} ") for error in errors):
            hints.append(f"- {field}: {hint}")
    return hints


ANDROID_ARTIFACT_FIELDS = {
    "Artifact path",
    "SHA256",
    "Artifact size bytes",
    "Version/build number",
    "Signing identity",
    "Installer channel",
}
IOS_ARTIFACT_FIELDS = {
    "Archive/TestFlight build",
    "Team ID",
    "Provisioning profiles",
}
SIGNED_INSTALL_FIELDS = {
    "Android signed install result",
    "iOS signed install result",
}
ASSISTANT_INPUT_FIELDS = {
    "Voice/photo assistant input evidence",
}
ASSISTANT_FIRST_SCREEN_FIELDS = {
    "Assistant first screen evidence",
}
PERMISSION_EVIDENCE_FIELDS = {
    "Camera permission",
    "Local network / SSH scenario",
    "Local network permission",
    "Media/file access",
    "Microphone permission",
    "Notification permission",
    "Photo library permission",
    "Speech recognition permission",
}
SHARE_TO_APP_FIELDS = {
    "CSV",
    "Excel .xlsx or .xls",
    "Image/photo",
    "PDF",
    "Plain text",
    "URL",
    "Word .docx or .doc",
}
TASK_CHAIN_FIELDS = {
    "Digital employee task ID",
    "Document upload task ID",
    "Exported document share evidence",
    "Markdown export job ID",
    "Notification delivery evidence",
    "PDF export job ID",
    "Realtime update evidence",
    "Status polling result",
    "Word export job ID",
}
NOTIFICATION_DELIVERY_FIELDS = {
    "Notification delivery evidence",
}
NETWORK_RECOVERY_FIELDS = {
    "Network offline/recovery evidence",
}
ACCOUNT_PRIVACY_FIELDS = {
    "Local work records reset confirmation",
    "Server-profile metadata retained after local reset",
    "Server-profile cache clear confirmation",
    "Theme and speech language change result",
}
SSH_SMOKE_FIELDS = {
    "AI analysis confirmation and sensitive-data warning",
    "AI explanation / command draft result",
    "Auth mode",
    "Command output excerpt",
    "Connect result",
    "Copied backend session output evidence",
    "Backend SSH server-profile cache clear confirmation",
    "Disconnect result",
    "Host type",
    "Read-only command",
    "Reconnect result",
}
HUB_LLM_SETUP_FIELDS = {
    "Account screen shows selected Hub and tenant",
    "Bootstrap user/quota/feature flags/service status",
    "Desktop GUI QR authorization ID",
    "Discovered Hub URL",
    "Discovered Hub/tenant result",
    "HubCenter candidates",
    "HubCenter probe result",
    "LLM access evidence",
    "LLM access mode",
    "LLM setup surface restriction",
    "Login result",
    "MaClaw account",
    "No custom Hub URL setting found",
    "Selected HubCenter URL",
    "Tenant ID",
}


def _matches_field_error(error: str, fields: set[str]) -> bool:
    return any(error == field or error.startswith(f"{field} ") for field in fields)


def _artifact_hints_for_version(
    errors: list[str],
    version: str,
    *,
    records_dir: str = release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
) -> list[str]:
    hints: list[str] = []
    android_artifact_error = any(
        _matches_field_error(error, ANDROID_ARTIFACT_FIELDS)
        or error.startswith("Local signed artifact ")
        or error.startswith("SHA256 does not match local artifact ")
        for error in errors
    )
    ios_artifact_error = any(
        _matches_field_error(error, IOS_ARTIFACT_FIELDS)
        or error.startswith("Local iOS archive ")
        for error in errors
    )
    if android_artifact_error:
        hints.append(
            "- Android signed artifact: run `"
            + release_evidence_commands.android_artifact_evidence_command(
                version,
                record_dir=records_dir,
            )
            + "` and paste the generated fields into the QA record."
        )
    if ios_artifact_error:
        hints.append(
            "- iOS archive/TestFlight artifact: run `"
            + release_evidence_commands.ios_artifact_evidence_command(
                record_dir=records_dir,
            )
            + "` and paste the generated fields into the QA record."
        )
    return hints


def _signed_install_hints(errors: list[str]) -> list[str]:
    if not any(_matches_field_error(error, SIGNED_INSTALL_FIELDS) for error in errors):
        return []
    return [
        "- Signed install/app launch: add QA-device evidence that the signed build installed and opened, including a traceable screenshot/recording ID such as `screenshot install-launch-android-42` or `recording install-launch-ios-42`.",
    ]


def _assistant_input_hints(errors: list[str]) -> list[str]:
    if not any(_matches_field_error(error, ASSISTANT_INPUT_FIELDS) for error in errors):
        return []
    return [
        "- AI assistant voice/photo input: record the recognized voice transcript filling or being sent from the AI assistant composer, the photo/image/screenshot assistant input, a resulting citation URL or document upload task ID, and a traceable screenshot/recording ID such as `screenshot mobile-input-42`.",
    ]


def _assistant_first_screen_hints(errors: list[str]) -> list[str]:
    if not any(_matches_field_error(error, ASSISTANT_FIRST_SCREEN_FIELDS) for error in errors):
        return []
    return [
        "- AI assistant first screen: record a signed-build cold launch after login showing the AI assistant first screen, main-conversation/secondary-tab controls, microphone/voice input, and no legacy info-lookup entry, with a traceable screenshot/recording ID such as `screenshot assistant-first-screen-42`.",
    ]


def _permission_hints(errors: list[str]) -> list[str]:
    if not any(_matches_field_error(error, PERMISSION_EVIDENCE_FIELDS) for error in errors):
        return []
    return [
        "- Runtime permissions: capture the real permission prompt/result in the workflow that needs it, include a `permission-grant:<id>` token, and tie microphone/speech/camera/photo-library evidence to AI assistant voice/photo input, notification evidence to real task notification open, and local-network evidence to a backend-managed SSH read-only command executed through the GUI/agent session manager.",
    ]


def _share_to_app_hints(errors: list[str]) -> list[str]:
    if not any(_matches_field_error(error, SHARE_TO_APP_FIELDS) for error in errors):
        return []
    return [
        "- Share-to-app payloads: record each payload entering MaClaw Mobile from the OS share sheet; plain text and URL should land in AI assistant with URL/citation evidence, while image/PDF/Word/Excel/CSV should create document import/upload task evidence.",
    ]


def _task_chain_hints(errors: list[str]) -> list[str]:
    if not any(_matches_field_error(error, TASK_CHAIN_FIELDS) for error in errors):
        return []
    return [
        "- Task chain evidence: keep the same document upload task ID, PDF/Word/Markdown export job IDs, and digital employee task ID threaded through status polling, realtime updates, notification delivery/open evidence, and exported-document sharing.",
    ]


def _notification_delivery_hints(errors: list[str]) -> list[str]:
    if not any(_matches_field_error(error, NOTIFICATION_DELIVERY_FIELDS) for error in errors):
        return []
    return [
        "- Notification delivery/open evidence: record delivered document/export, digital employee, and SSH abnormal/disconnect notifications with typed payloads such as `document-export:<id>`, `digital-employee-task:<id>`, and `server-profile:<id>`, plus the tap/open target and redacted message preview.",
    ]


def _network_recovery_hints(errors: list[str]) -> list[str]:
    if not any(_matches_field_error(error, NETWORK_RECOVERY_FIELDS) for error in errors):
        return []
    return [
        "- Network offline/recovery evidence: record the offline warning and restored service for the selected HubCenter, discovered Hub URL, tenant ID, and at least one matching document/export/digital-employee task or job ID after connectivity returns.",
    ]


def _account_privacy_hints(errors: list[str]) -> list[str]:
    if not any(_matches_field_error(error, ACCOUNT_PRIVACY_FIELDS) for error in errors):
        return []
    return [
        "- Account privacy/local data evidence: separately record theme/speech-language changes, local work-record clearing for assistant history, document drafts, commands, digital employee prompts/tasks, and app preferences, retained sanitized server-profile metadata for server-profile:<id> after local reset, then the separate phone-side server-profile cache clear action with `server-profile-cache-clear:<id>`.",
    ]


def _ssh_smoke_hints(errors: list[str]) -> list[str]:
    if not any(_matches_field_error(error, SSH_SMOKE_FIELDS) for error in errors):
        return []
    return [
        "- GUI-equivalent backend SSH session management smoke: record the server profile ID, mobile create/attach control request, GUI/agent-bound backend_session_id, visible concrete claim/worker owner from the Hub/GUI update such as `claimed_by desktop-agent-1` (not generic `worker`, `GUI/agent worker`, or `MaClaw GUI agent worker`), GUI/agent claim or worker handoff evidence, explicit worker claim/update evidence, not phone-local/ad hoc terminal evidence, host/auth mode metadata, connect result, backend-managed read-only command and output, the `ssh_session` realtime event with `output_chunk`/`output_seq` tied to the same GUI/agent-bound backend_session_id, phone-initiated interrupt evidence through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt` plus GUI/agent Ctrl+C handling, disconnect/reconnect through the managed session path, copied backend session output with a GUI/agent evidence line containing actual values for Hub session ID, backend_session_id, concrete claimed_by worker identity such as claimed_by desktop-agent-1, and numeric output_seq, redacted AI analysis with sensitive-data warning tied to the same GUI/agent-bound backend_session_id when backend output is used, manual command draft ID, digital employee handoff evidence tied to the same GUI/agent-bound backend_session_id if used, and phone-side server-profile cache clear confirmation.",
    ]


def _hub_llm_setup_hints(errors: list[str]) -> list[str]:
    if not any(_matches_field_error(error, HUB_LLM_SETUP_FIELDS) for error in errors):
        return []
    return [
        "- Hub/account/LLM setup: record exactly the three preset HubCenter URLs, the selected HubCenter, discovered tenant Hub URL, tenant ID, phone:<digits> login/credits account, bootstrap quota/features/service status, and LLM access mode; third-party LLM evidence must come only from the desktop GUI QR authorization path, with no redemption-code login or arbitrary provider/base URL/API-key fields.",
    ]


def _secret_redaction_hints(
    errors: list[str],
    *,
    record_path: str,
    values: dict[str, list[str]],
) -> list[str]:
    if not errors:
        return []
    hints = [
        "- Remove raw secrets from the QA record, then replace them with redacted evidence, attachment IDs, task IDs, artifact hashes, or reviewer notes.",
    ]
    copied_output_values = values.get("Copied backend session output evidence", [])
    if any(
        validate_qa_build_record.raw_secret_errors(value)
        for value in copied_output_values
    ):
        hints.append(
            "- For copied backend session output evidence, keep the GUI/agent evidence line with real Hub session ID, backend_session_id, concrete claimed_by worker identity such as claimed_by desktop-agent-1, and numeric output_seq, while replacing credentials or private customer excerpts with redacted text or a traceable attachment ID."
        )
    hints.append(
        f"- Re-run `python3 tool/validate_qa_build_record.py {record_path}` before linking the record from docs/release_evidence.md.",
    )
    return hints


def _version_for_report(report: QaBuildRecordReport) -> str:
    match = validate_qa_build_record.QA_BUILD_RECORD_FILENAME_RE.fullmatch(report.path.name)
    if match is None:
        return release_evidence_commands.DEFAULT_VERSION
    return match.group("version")


def _scope_for_report(report: QaBuildRecordReport) -> str:
    return validate_qa_build_record.record_scope_from_path(report.path)


def _records_dir_for_report(report: QaBuildRecordReport) -> str:
    default_dir = Path(__file__).resolve().parents[1] / release_evidence_commands.DEFAULT_QA_RECORDS_DIR
    if report.path.parent.resolve() == default_dir.resolve():
        return release_evidence_commands.DEFAULT_QA_RECORDS_DIR
    return str(report.path.parent)


def format_report(report: QaBuildRecordReport) -> str:
    lines = [
        f"QA build record report: {report.path}",
        f"Status: {'PASS' if report.passed else 'FAIL'}",
    ]
    if report.progress is not None:
        lines.append(
            "Required evidence: "
            f"{report.progress.filled}/{report.progress.required} occurrences filled"
        )
    lines.append("")

    sections = (
        _format_section("Path / filename", report.path_errors)
        + _format_section("Secret redaction", report.secret_errors)
        + _format_section("Evidence fields and values", report.evidence_errors)
        + _format_section("Local artifact hashes", report.artifact_errors)
        + _format_section("Unclassified validator output", report.unknown_errors)
    )
    if sections:
        lines.extend(sections)
        version = _version_for_report(report)
        scope = _scope_for_report(report)
        records_dir = _records_dir_for_report(report)
        hints = _evidence_hints_for_version(
            report.evidence_errors,
            version,
            scope=scope,
            records_dir=records_dir,
        )
        if hints:
            lines.append("")
            lines.append("How to fill release decision evidence:")
            lines.extend(hints)
        artifact_hints = _artifact_hints_for_version(
            report.evidence_errors + report.artifact_errors,
            version,
            records_dir=records_dir,
        )
        if artifact_hints:
            lines.append("")
            lines.append("How to fill signed artifact evidence:")
            lines.extend(artifact_hints)
        signed_install_hints = _signed_install_hints(report.evidence_errors)
        if signed_install_hints:
            lines.append("")
            lines.append("How to fill signed install evidence:")
            lines.extend(signed_install_hints)
        assistant_input_hints = _assistant_input_hints(report.evidence_errors)
        if assistant_input_hints:
            lines.append("")
            lines.append("How to fill AI assistant voice/photo evidence:")
            lines.extend(assistant_input_hints)
        assistant_first_screen_hints = _assistant_first_screen_hints(report.evidence_errors)
        if assistant_first_screen_hints:
            lines.append("")
            lines.append("How to fill AI assistant first-screen evidence:")
            lines.extend(assistant_first_screen_hints)
        permission_hints = _permission_hints(report.evidence_errors)
        if permission_hints:
            lines.append("")
            lines.append("How to fill runtime permission evidence:")
            lines.extend(permission_hints)
        share_to_app_hints = _share_to_app_hints(report.evidence_errors)
        if share_to_app_hints:
            lines.append("")
            lines.append("How to fill share-to-app evidence:")
            lines.extend(share_to_app_hints)
        task_chain_hints = _task_chain_hints(report.evidence_errors)
        if task_chain_hints:
            lines.append("")
            lines.append("How to fill task chain evidence:")
            lines.extend(task_chain_hints)
        notification_delivery_hints = _notification_delivery_hints(report.evidence_errors)
        if notification_delivery_hints:
            lines.append("")
            lines.append("How to fill notification delivery evidence:")
            lines.extend(notification_delivery_hints)
        network_recovery_hints = _network_recovery_hints(report.evidence_errors)
        if network_recovery_hints:
            lines.append("")
            lines.append("How to fill network recovery evidence:")
            lines.extend(network_recovery_hints)
        account_privacy_hints = _account_privacy_hints(report.evidence_errors)
        if account_privacy_hints:
            lines.append("")
            lines.append("How to fill account privacy evidence:")
            lines.extend(account_privacy_hints)
        ssh_smoke_hints = _ssh_smoke_hints(report.evidence_errors)
        if ssh_smoke_hints:
            lines.append("")
            lines.append("How to fill GUI-equivalent backend SSH session management smoke evidence:")
            lines.extend(ssh_smoke_hints)
        hub_llm_setup_hints = _hub_llm_setup_hints(report.evidence_errors)
        if hub_llm_setup_hints:
            lines.append("")
            lines.append("How to fill Hub/account/LLM setup evidence:")
            lines.extend(hub_llm_setup_hints)
        secret_values = (
            validate_qa_build_record.parse_record(
                report.path.read_text(encoding="utf-8"),
            )
            if report.secret_errors and report.path.is_file()
            else {}
        )
        secret_hints = _secret_redaction_hints(
            report.secret_errors,
            record_path=str(report.path),
            values=secret_values,
        )
        if secret_hints:
            lines.append("")
            lines.append("How to fix secret redaction failures:")
            lines.extend(secret_hints)
    else:
        lines.append("No gaps found by the QA build record validator.")
        lines.append("")
        lines.append("Next action:")
        lines.append(
            "- "
            + release_evidence_commands.qa_release_evidence_link_hint(
                scope=_scope_for_report(report),
                records_dir=_records_dir_for_report(report),
                version=_version_for_report(report),
            )
        )
    return "\n".join(lines).rstrip() + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Print an actionable MaClaw Mobile QA build record gap report.",
    )
    parser.add_argument("record", type=Path, help="Completed or in-progress QA record")
    args = parser.parse_args(argv)

    report = generate_report(args.record)
    output = format_report(report)
    if report.passed:
        print(output, end="")
        return 0
    print(output, end="", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
