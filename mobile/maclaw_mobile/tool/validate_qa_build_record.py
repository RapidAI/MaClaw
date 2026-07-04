from __future__ import annotations

import argparse
import hashlib
import re
import sys
from datetime import datetime
from pathlib import Path
from urllib.parse import urlparse

from release_evidence_commands import (
    AUTOMATED_RELEASE_GATE_COUNT,
    AUTOMATED_RELEASE_GATE_SUCCESS_LINE,
    VALID_SCOPES,
)


OFFICIAL_HUBCENTER_URLS = [
    "https://hubs.mypapers.top",
    "https://hubs.maclaw.top",
    "https://hubs2.maclaw.top",
]
RUNNER_BUNDLE_ID = "top.mypapers.maclaw.mobile"
SHARE_EXTENSION_BUNDLE_ID = "top.mypapers.maclaw.mobile.ShareExtension"
APP_GROUP = "group.top.mypapers.maclaw.mobile"
OFFICIAL_LLM_QR_AUTH_ID = "not-used-official-mode"
HTTPS_URL_RE = re.compile(r"https://[A-Za-z0-9.-]+")
SHA256_RE = re.compile(r"^[0-9a-fA-F]{64}$")
DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
GIT_COMMIT_RE = re.compile(r"^[0-9a-fA-F]{7,40}$")
BRANCH_NAME_RE = re.compile(r"^[A-Za-z0-9._/-]{3,128}$")
APPLE_TEAM_ID_RE = re.compile(r"^[A-Z0-9]{10}$")
TENANT_ID_RE = re.compile(r"^[a-z0-9][a-z0-9._-]{2,63}$")
FLUTTER_VERSION_RE = re.compile(r"(?i)\bflutter\s+\d+\.\d+\.\d+(?:[-+][A-Za-z0-9._-]+)?\b")
MACLAW_ACCOUNT_RE = re.compile(
    r"(?i)(?:^[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}$|(?:account|user|id)[:#=\s-]*[A-Z0-9._@+-]{4,})"
)
VERSION_BUILD_RE = re.compile(
    r"(?i)(?:\b\d+(?:\.\d+){1,3}\+\d+\b|(?:version|v)\s*\d+(?:\.\d+){1,3}.*\bbuild\s*\d+\b)"
)
QA_BUILD_RECORD_FILENAME_RE = re.compile(
    r"^(?P<date>\d{4}-\d{2}-\d{2})-"
    r"(?P<scope>" + "|".join(re.escape(scope) for scope in VALID_SCOPES) + r")-"
    r"(?P<version>\d+(?:\.\d+){1,3}\+\d+)\.md$"
)
TESTFLIGHT_BUILD_RE = re.compile(r"(?i)\btestflight\s+build\s+\d+\b")
IOS_PROFILE_REFERENCE_RE = re.compile(
    r"(?i)(\buuid\s+[a-z0-9][a-z0-9-]{5,}\b|\.mobileprovision\b|\bprofile name\s+[^;,\n]{4,})"
)
PRIVATE_KEY_BLOCK_RE = re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----")
SECRET_ASSIGNMENT_RE = re.compile(
    r"(?im)\b(?:password|passwd|token|access[_-]?token|secret|api[_-]?key)\s*[:=]\s*['\"]?[^\s'\"<>]{8,}"
)
TOKEN_LITERAL_RE = re.compile(
    r"\b(?:sk-[A-Za-z0-9_-]{20,}|ghp_[A-Za-z0-9_]{20,}|(?:AKIA|ASIA)[A-Z0-9]{16}|AIza[A-Za-z0-9_-]{35})\b"
)
JWT_LITERAL_RE = re.compile(r"\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b")
AUTHORIZATION_HEADER_RE = re.compile(
    r"(?i)\bauthorization\s*:\s*(?:bearer\s+[A-Za-z0-9._~+/=-]{12,}|basic\s+[A-Za-z0-9+/=]{12,})"
)
HTTP_SECRET_HEADER_RE = re.compile(
    r"(?i)\b(?:cookie|set-cookie|private-token|x-api-key)\s*:\s*[^\s;,'\"<>][^,\n]{7,}"
)
URL_EMBEDDED_CREDENTIAL_RE = re.compile(
    r"(?i)\b[a-z][a-z0-9+.-]{2,}://[A-Za-z0-9._~%+-]{1,64}:[^@\s/'\"<>]{8,}@"
)
ANDROID_ARTIFACT_SUFFIXES = (".apk", ".aab")
DOCUMENT_TEMPLATE_MARKERS = (
    "notice",
    "report",
    "email",
    "proposal",
    "meeting minutes",
    "statement",
)

REQUIRED_FIELDS = [
    "Date",
    "Git commit",
    "Branch",
    "Tester",
    "Flutter version",
    "MaClaw account",
    "HubCenter candidates",
    "Selected HubCenter URL",
    "Discovered Hub URL",
    "Tenant ID",
    "LLM access mode",
    "Desktop GUI QR authorization ID",
    "Artifact path",
    "SHA256",
    "Version/build number",
    "Signing identity",
    "Installer channel",
    "Device model / OS",
    "Android signed install result",
    "Account screen shows selected Hub and tenant",
    "No custom Hub URL setting found",
    "Plain text",
    "URL",
    "Image/photo",
    "PDF",
    "Word .docx or .doc",
    "Excel .xlsx or .xls",
    "CSV",
    "Notification permission",
    "Camera permission",
    "Microphone permission",
    "Media/file access",
    "Local network / SSH scenario",
    "Archive/TestFlight build",
    "Runner bundle id",
    "Team ID",
    "Provisioning profiles",
    "Share Extension bundle id",
    "App group",
    "Device model / OS",
    "iOS signed install result",
    "URL schemes maclaw and ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER)",
    "Plain text",
    "URL",
    "Image/photo",
    "PDF",
    "Word .docx or .doc",
    "Excel .xlsx or .xls",
    "CSV",
    "Notification permission",
    "Camera permission",
    "Microphone permission",
    "Speech recognition permission",
    "Photo library permission",
    "Local network permission",
    "Login result",
    "Bootstrap user/quota/feature flags/service status",
    "HubCenter probe result",
    "Discovered Hub/tenant result",
    "LLM access evidence",
    "LLM setup surface restriction",
    "AI search query",
    "Voice/photo assistant input evidence",
    "Visible citations / sources",
    "Shared result",
    "Document draft created from search",
    "Document upload task ID",
    "PDF export job ID",
    "Word export job ID",
    "Markdown export job ID",
    "Exported document share evidence",
    "Digital employee task ID",
    "Status polling result",
    "Realtime update evidence",
    "Notification delivery evidence",
    "API base URL confirmation",
    "Realtime Hub URL confirmation",
    "Network offline/recovery evidence",
    "Theme and speech language change result",
    "Local work records reset confirmation",
    "Server credentials retained after local reset",
    "Server profiles/SSH credentials clear confirmation",
    "Host type",
    "Auth mode",
    "Connect result",
    "Read-only command",
    "Command output excerpt",
    "Disconnect result",
    "Reconnect result",
    "Copied output evidence",
    "AI analysis confirmation and sensitive-data warning",
    "AI explanation / command draft result",
    "Credential deletion confirmation",
    "Release handoff result",
    "Runtime boundary verification result",
    "Automated release gates result",
    "Automated gates passed",
    "Android manual gates passed",
    "iOS manual gates passed",
    "Hub discovery smoke passed",
    "Manual SSH smoke passed",
    "Approved by",
    "Approval date",
]

REQUIRED_FIELD_COUNTS = {
    field: REQUIRED_FIELDS.count(field) for field in set(REQUIRED_FIELDS)
}

ANDROID_ONLY_FIELDS = {
    "Account screen shows selected Hub and tenant",
    "Android manual gates passed",
    "Android signed install result",
    "Artifact path",
    "Installer channel",
    "Local network / SSH scenario",
    "Media/file access",
    "No custom Hub URL setting found",
    "SHA256",
    "Signing identity",
    "Version/build number",
}

OPTIONAL_FIELDS = {
    "Known issues / waivers",
    "Device logs / screenshots / recordings",
    "Screenshots / recordings",
    "Digital employee handoff warning, if used",
}

OPTIONAL_AUDITABLE_FIELDS = {
    "Device logs / screenshots / recordings",
    "Digital employee handoff warning, if used",
    "Known issues / waivers",
    "Screenshots / recordings",
}

OPTIONAL_ATTACHMENT_FIELDS = {
    "Device logs / screenshots / recordings",
    "Screenshots / recordings",
}

EXACT_VALUE_FIELDS = {
    "Runner bundle id": RUNNER_BUNDLE_ID,
    "Share Extension bundle id": SHARE_EXTENSION_BUNDLE_ID,
    "App group": APP_GROUP,
}

PASS_DECISION_FIELDS = [
    "Automated gates passed",
    "Android manual gates passed",
    "iOS manual gates passed",
    "Hub discovery smoke passed",
    "Manual SSH smoke passed",
]

WAIVER_SUMMARY_MARKERS = {
    "Automated gates passed": ("automated", "automation", "gate"),
    "Android manual gates passed": ("android",),
    "iOS manual gates passed": ("ios", "iphone", "ipad", "testflight"),
    "Hub discovery smoke passed": ("hub discovery", "hubcenter", "hub center"),
    "Manual SSH smoke passed": ("manual ssh", "ssh", "server"),
}

PASS_DECISION_WORDS = [
    "pass",
    "passed",
    "approved",
    "\u901a\u8fc7",  # 通过
    "\u5df2\u901a\u8fc7",  # 已通过
    "\u6279\u51c6",  # 批准
]

WAIVER_DECISION_WORDS = [
    "waived",
    "\u8c41\u514d",  # 豁免
]

DATE_FIELDS = [
    "Date",
    "Approval date",
]

TASK_ID_FIELDS = [
    "Document upload task ID",
    "PDF export job ID",
    "Word export job ID",
    "Markdown export job ID",
    "Digital employee task ID",
]

URL_FIELDS = [
    "Selected HubCenter URL",
    "Discovered Hub URL",
    "API base URL confirmation",
    "Realtime Hub URL confirmation",
]

PERMISSION_EVIDENCE_FIELDS = {
    "Camera permission": ("camera", "photo", "picture", "import"),
    "Local network / SSH scenario": ("local network", "ssh", "private-network", "server"),
    "Local network permission": ("local network", "ssh", "private-network", "server"),
    "Media/file access": ("media", "file", "document import", "share-to-app"),
    "Microphone permission": ("microphone", "voice", "speech"),
    "Notification permission": ("notification", "account screen"),
    "Photo library permission": ("photo library", "photos", "gallery", "album"),
    "Speech recognition permission": ("speech recognition", "speech", "voice"),
}

SHARE_TEXT_EVIDENCE_FIELDS = {
    "Plain text",
}

SHARE_URL_EVIDENCE_FIELDS = {
    "URL",
}

SHARE_DOCUMENT_EVIDENCE_FIELDS = {
    "CSV": ("csv",),
    "Excel .xlsx or .xls": ("excel", ".xlsx", ".xls", "spreadsheet"),
    "Image/photo": ("image", "photo", ".jpg", ".jpeg", ".png", ".heic"),
    "PDF": ("pdf",),
    "Word .docx or .doc": ("word", ".docx", ".doc"),
}

DUAL_PLATFORM_EVIDENCE_FIELDS = {
    "Plain text",
    "URL",
    "Image/photo",
    "PDF",
    "Word .docx or .doc",
    "Excel .xlsx or .xls",
    "CSV",
    "Notification permission",
    "Camera permission",
    "Microphone permission",
}

IOS_ONLY_FIELDS = {
    "App group",
    "Archive/TestFlight build",
    "iOS manual gates passed",
    "iOS signed install result",
    "Local network permission",
    "Photo library permission",
    "Provisioning profiles",
    "Runner bundle id",
    "Share Extension bundle id",
    "Speech recognition permission",
    "Team ID",
    "URL schemes maclaw and ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER)",
}

SCOPED_DUAL_PLATFORM_FIELDS = DUAL_PLATFORM_EVIDENCE_FIELDS | {
    "Device model / OS",
}

AI_SEARCH_EVIDENCE_FIELDS = {
    "AI search query",
}

MOBILE_INPUT_EVIDENCE_FIELDS = {
    "Voice/photo assistant input evidence",
}

CITATION_EVIDENCE_FIELDS = {
    "Visible citations / sources",
}

SHARED_RESULT_EVIDENCE_FIELDS = {
    "Shared result",
}

DOCUMENT_DRAFT_EVIDENCE_FIELDS = {
    "Document draft created from search",
}

SIGNED_INSTALL_RESULT_FIELDS = {
    "Android signed install result",
    "iOS signed install result",
}

SIGNED_INSTALL_PLATFORM_MARKERS = {
    "Android signed install result": ("android", "apk", "aab", "play", "internal app sharing"),
    "iOS signed install result": ("ios", "iphone", "ipad", "testflight"),
}

SSH_AI_ANALYSIS_WARNING_FIELDS = {
    "AI analysis confirmation and sensitive-data warning",
}

SSH_AI_RESULT_FIELDS = {
    "AI explanation / command draft result",
}

CREDENTIAL_DELETION_FIELDS = {
    "Credential deletion confirmation",
}

ACCOUNT_PREFERENCE_FIELDS = {
    "Theme and speech language change result",
}

LOCAL_WORK_RECORDS_RESET_FIELDS = {
    "Local work records reset confirmation",
}

SERVER_CREDENTIAL_RETENTION_FIELDS = {
    "Server credentials retained after local reset",
}

SERVER_CREDENTIAL_CLEAR_FIELDS = {
    "Server profiles/SSH credentials clear confirmation",
}

STATUS_POLLING_FIELDS = {
    "Status polling result",
}

REALTIME_UPDATE_FIELDS = {
    "Realtime update evidence",
}

NOTIFICATION_DELIVERY_FIELDS = {
    "Notification delivery evidence",
}

IOS_URL_SCHEME_FIELDS = {
    "URL schemes maclaw and ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER)",
}

ACCOUNT_HUB_TENANT_FIELDS = {
    "Account screen shows selected Hub and tenant",
}

NO_CUSTOM_HUB_URL_FIELDS = {
    "No custom Hub URL setting found",
}

BOOTSTRAP_SERVICE_FIELDS = {
    "Bootstrap user/quota/feature flags/service status",
}

NETWORK_RECOVERY_FIELDS = {
    "Network offline/recovery evidence",
}

HUBCENTER_PROBE_FIELDS = {
    "HubCenter probe result",
}

DISCOVERED_HUB_TENANT_FIELDS = {
    "Discovered Hub/tenant result",
}

LLM_ACCESS_EVIDENCE_FIELDS = {
    "LLM access evidence",
}

LLM_SETUP_RESTRICTION_FIELDS = {
    "LLM setup surface restriction",
}

LOGIN_RESULT_FIELDS = {
    "Login result",
}

SSH_EVIDENCE_FIELDS = {
    "Host type": ("host", "server", "linux", "ubuntu", "debian", "centos", "private", "cloud"),
    "Auth mode": ("password", "private key", "key auth", "ssh key", "passphrase"),
    "Connect result": ("connect", "connected", "ssh"),
    "Read-only command": ("read-only", "readonly", "whoami", "uptime", "pwd", "ls ", "df ", "free "),
    "Command output excerpt": ("output", "excerpt", "stdout", "whoami", "uptime"),
    "Disconnect result": ("disconnect", "disconnected", "closed"),
    "Reconnect result": ("reconnect", "reconnected", "connected again"),
    "Copied output evidence": ("copy", "copied", "clipboard", "terminal output"),
}

DOCUMENT_UPLOAD_TASK_FIELDS = {
    "Document upload task ID",
}

DOCUMENT_EXPORT_JOB_FIELDS = {
    "PDF export job ID": ("pdf",),
    "Word export job ID": ("word", "docx", "doc"),
    "Markdown export job ID": ("markdown", "md"),
}

DOCUMENT_EXPORT_SHARE_FIELDS = {
    "Exported document share evidence",
}

DIGITAL_EMPLOYEE_TASK_FIELDS = {
    "Digital employee task ID",
}

MANUAL_EVIDENCE_FIELDS = {
    "Device model / OS",
    "Android signed install result",
    "Account screen shows selected Hub and tenant",
    "No custom Hub URL setting found",
    "Plain text",
    "URL",
    "Image/photo",
    "PDF",
    "Word .docx or .doc",
    "Excel .xlsx or .xls",
    "CSV",
    "Notification permission",
    "Camera permission",
    "Microphone permission",
    "Media/file access",
    "Local network / SSH scenario",
    "iOS signed install result",
    "URL schemes maclaw and ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER)",
    "Speech recognition permission",
    "Photo library permission",
    "Local network permission",
    "Login result",
    "Bootstrap user/quota/feature flags/service status",
    "HubCenter probe result",
    "Discovered Hub/tenant result",
    "LLM access evidence",
    "LLM setup surface restriction",
    "AI search query",
    "Voice/photo assistant input evidence",
    "Visible citations / sources",
    "Shared result",
    "Document draft created from search",
    "Exported document share evidence",
    "Status polling result",
    "Realtime update evidence",
    "Notification delivery evidence",
    "Network offline/recovery evidence",
    "Theme and speech language change result",
    "Local work records reset confirmation",
    "Server credentials retained after local reset",
    "Server profiles/SSH credentials clear confirmation",
    "Host type",
    "Auth mode",
    "Connect result",
    "Read-only command",
    "Command output excerpt",
    "Disconnect result",
    "Reconnect result",
    "Copied output evidence",
    "AI analysis confirmation and sensitive-data warning",
    "AI explanation / command draft result",
    "Credential deletion confirmation",
    "Release handoff result",
    "Runtime boundary verification result",
    "Automated release gates result",
}

FINAL_AUTOMATED_EVIDENCE_FIELDS = {
    "Release handoff result": "must reference release_handoff.py output or saved handoff evidence",
    "Runtime boundary verification result": "must reference verify_runtime_boundary.py verified output or log evidence",
    "Automated release gates result": "must reference run_release_gates.py gate count and saved log evidence",
}

LLM_ACCESS_MODES = {
    "maclaw_official",
    "desktop_qr_third_party",
}

NON_PLACEHOLDER_FIELDS = {
    "Branch": 3,
    "Tester": 3,
    "Approved by": 3,
    "MaClaw account": 6,
    "Tenant ID": 6,
    "Version/build number": 5,
    "Signing identity": 6,
    "Installer channel": 6,
}

PLACEHOLDER_VALUES = {
    "ok",
    "pass",
    "passed",
    "yes",
    "done",
    "todo",
    "tbd",
    "n/a",
    "na",
    "none",
    "\u5df2\u901a\u8fc7",
    "\u901a\u8fc7",
}


def parse_record(text: str) -> dict[str, list[str]]:
    values: dict[str, list[str]] = {}
    known_fields = sorted(REQUIRED_FIELD_COUNTS, key=len, reverse=True)
    for raw_line in text.splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or line.startswith("```"):
            continue
        normalized = line.lstrip("\ufeff")
        key = ""
        value = ""
        for field in known_fields:
            prefix = f"{field}:"
            if normalized.startswith(prefix):
                key = field
                value = normalized[len(prefix) :].strip()
                break
        if not key:
            if ":" not in normalized:
                continue
            key, value = normalized.split(":", 1)
            key = key.strip().lstrip("\ufeff")
            value = value.strip()
        if not key:
            continue
        values.setdefault(key, []).append(value)
    return values


def _filled_count(values: dict[str, list[str]], field: str) -> int:
    return sum(1 for value in values.get(field, []) if value)


def _is_positive_decision(value: str) -> bool:
    normalized = value.strip().lower()
    if normalized == "passed / waived with reason":
        return False
    if re.match(r"^(pass|passed|approved)\b", normalized):
        return True
    if any(normalized.startswith(word) for word in PASS_DECISION_WORDS if not word.isascii()):
        return True
    return any(_has_waiver_reason(normalized, word) for word in WAIVER_DECISION_WORDS)


def _is_waiver_decision(value: str) -> bool:
    normalized = value.strip().lower()
    return any(_has_waiver_reason(normalized, word) for word in WAIVER_DECISION_WORDS)


def _waived_decision_fields(values: dict[str, list[str]]) -> list[str]:
    waived = []
    for field in PASS_DECISION_FIELDS:
        if any(value and _is_waiver_decision(value) for value in values.get(field, [])):
            waived.append(field)
    return waived


def _summarizes_waived_gate(field: str, waiver_notes: list[str]) -> bool:
    markers = WAIVER_SUMMARY_MARKERS[field]
    combined = "\n".join(waiver_notes).strip().lower()
    return any(marker in combined for marker in markers)


def _has_waiver_reason(normalized: str, word: str) -> bool:
    if not normalized.startswith(word):
        return False
    suffix = normalized.removeprefix(word).strip()
    if suffix.startswith("with reason"):
        suffix = suffix.removeprefix("with reason").strip()
    reason = suffix.strip(" :-\t")
    return len(reason) >= 6


def _is_trackable_id(value: str) -> bool:
    normalized = value.strip().lower()
    return len(normalized) >= 6 and normalized not in PLACEHOLDER_VALUES


def _mentions_any_trackable_id(value: str, task_ids: list[str]) -> bool:
    normalized = value.strip().lower()
    return any(task_id.strip().lower() in normalized for task_id in task_ids if _is_trackable_id(task_id))


def _is_non_placeholder(value: str, min_len: int) -> bool:
    normalized = value.strip().lower()
    return len(normalized) >= min_len and normalized not in PLACEHOLDER_VALUES


def _is_branch_name(value: str) -> bool:
    normalized = value.strip()
    lower = normalized.lower()
    return (
        len(normalized) >= 3
        and lower not in PLACEHOLDER_VALUES
        and BRANCH_NAME_RE.fullmatch(normalized) is not None
        and not normalized.startswith(("/", "."))
        and not normalized.endswith(("/", ".", ".lock"))
        and ".." not in normalized
        and "//" not in normalized
    )


def _is_flutter_version(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        len(normalized) >= 6
        and normalized not in PLACEHOLDER_VALUES
        and FLUTTER_VERSION_RE.search(value) is not None
    )


def _is_version_build_number(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        len(normalized) >= 5
        and normalized not in PLACEHOLDER_VALUES
        and VERSION_BUILD_RE.search(value) is not None
    )


def _is_maclaw_account(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        len(normalized) >= 6
        and normalized not in PLACEHOLDER_VALUES
        and MACLAW_ACCOUNT_RE.search(value.strip()) is not None
    )


def _is_tenant_id(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        len(normalized) >= 3
        and normalized not in PLACEHOLDER_VALUES
        and TENANT_ID_RE.fullmatch(normalized) is not None
    )


def _is_auditable_note(value: str) -> bool:
    normalized = value.strip().lower()
    return len(normalized) >= 12 and normalized not in PLACEHOLDER_VALUES


def _is_attachment_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return _is_auditable_note(value) and (
        any(
            marker in normalized
            for marker in (
                ".heic",
                ".jpg",
                ".jpeg",
                ".json",
                ".log",
                ".mov",
                ".mp4",
                ".png",
                ".txt",
                ".zip",
            )
        )
        or re.search(r"\battachment[-_ #:]?[a-z0-9][a-z0-9._-]{2,}\b", normalized)
        is not None
    )


def _is_release_handoff_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "handoff" in normalized
        and (
            "release_handoff.py" in normalized
            or "handoff-" in normalized
            or "handoff output" in normalized
            or "handoff evidence" in normalized
        )
        and any(marker in normalized for marker in (".md", "docs/qa-builds", "attachment"))
    )


def _is_runtime_boundary_result_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "runtime" in normalized
        and "boundary" in normalized
        and (
            "verify_runtime_boundary.py" in normalized
            or "runtime-boundary-" in normalized
            or "runtime boundary verified" in normalized
            or "runtime boundary verification" in normalized
        )
        and any(marker in normalized for marker in (".log", "docs/qa-builds", "attachment"))
    )


def _is_automated_release_gates_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    expected_gate_count = rf"\b{AUTOMATED_RELEASE_GATE_COUNT}\s+gates?\s+passed\b"
    expected_success_line = AUTOMATED_RELEASE_GATE_SUCCESS_LINE.lower()
    return (
        _is_auditable_note(value)
        and (
            "run_release_gates.py" in normalized
            or "release-gates-" in normalized
            or "release gates" in normalized
        )
        and (
            re.search(expected_gate_count, normalized) is not None
            or expected_success_line in normalized
        )
        and any(marker in normalized for marker in (".log", "docs/qa-builds", "attachment"))
    )


def _is_device_os_note(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(char.isdigit() for char in normalized)
        and ("android" in normalized or "ios" in normalized)
    )


def _mentions_android(value: str) -> bool:
    return "android" in value.strip().lower()


def _mentions_ios(value: str) -> bool:
    normalized = value.strip().lower()
    return any(marker in normalized for marker in ("ios", "iphone", "ipad", "testflight"))


def _is_permission_evidence(value: str, scenario_markers: tuple[str, ...]) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(
            marker in normalized
            for marker in (
                "prompt",
                "permission",
                "granted",
                "allowed",
                "denied",
                "settings",
                "screenshot",
                "recording",
                "local network",
                "media",
                "photo library",
            )
        )
        and any(marker in normalized for marker in scenario_markers)
    )


def _contains_any(value: str, markers: tuple[str, ...]) -> bool:
    normalized = value.strip().lower()
    return any(marker in normalized for marker in markers)


def _is_share_text_evidence(value: str) -> bool:
    return _is_auditable_note(value) and _contains_any(
        value,
        ("assistant", "shared text", "share text", "screenshot"),
    )


def _is_share_url_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return _is_auditable_note(value) and (
        "citation" in normalized
        or ("assistant" in normalized and ("url" in normalized or "link" in normalized))
    )


def _is_share_document_evidence(value: str, format_markers: tuple[str, ...]) -> bool:
    return (
        _is_auditable_note(value)
        and _contains_any(
            value,
            ("document import", "import task", "upload task", "task id"),
        )
        and _contains_any(value, format_markers)
    )


def _is_ai_search_query_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(
            marker in normalized
            for marker in ("query", "question", "search", "asked", "prompt", "?")
        )
        and any(char.isalpha() for char in normalized)
    )


def _is_mobile_input_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("voice", "speech", "microphone", "transcript", "recognized"))
        and any(marker in normalized for marker in ("photo", "camera", "image", "picture", "screenshot"))
        and any(marker in normalized for marker in ("assistant", "question", "query", "prompt"))
        and any(marker in normalized for marker in ("search", "citation", "document", "upload task", "task id", "answer"))
    )


def _is_citation_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("citation", "citations", "source", "sources"))
        and any(_is_https_url(url) for url in _listed_https_urls(value))
    )


def _is_shared_result_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("share", "shared", "copy", "copied", "export", "saved"))
        and any(
            marker in normalized
            for marker in (
                "mail",
                "email",
                "wechat",
                "微信",
                "im",
                "system share",
                "share sheet",
                "clipboard",
                "copied",
                ".pdf",
                ".docx",
                ".md",
                "file path",
                "saved to",
            )
        )
    )


def _is_document_export_share_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("export", "exported", "download", "downloaded", "saved"))
        and any(marker in normalized for marker in ("share", "shared", "system share", "share sheet", "mail", "wechat", "clipboard", "saved local path", "file path"))
        and "pdf" in normalized
        and any(marker in normalized for marker in ("word", "docx", ".doc"))
        and any(marker in normalized for marker in ("markdown", ".md"))
    )


def _is_document_draft_from_search_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("draft", "document", "template"))
        and any(marker in normalized for marker in ("search", "citation", "source", "assistant"))
        and all(marker in normalized for marker in DOCUMENT_TEMPLATE_MARKERS)
    )


def _is_signed_install_result_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("install", "installed", "testflight"))
        and any(
            marker in normalized
            for marker in ("launch", "launched", "opened", "open app", "home screen")
        )
    )


def _is_signed_install_platform_evidence(value: str, markers: tuple[str, ...]) -> bool:
    return _is_signed_install_result_evidence(value) and _contains_any(value, markers)


def _is_ios_url_scheme_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "maclaw" in normalized
        and "sharemedia" in normalized
    )


def _is_ssh_ai_analysis_warning_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "ai" in normalized
        and "analysis" in normalized
        and "sensitive" in normalized
        and any(marker in normalized for marker in ("preview", "confirm", "confirmation"))
    )


def _is_ssh_ai_result_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "ai" in normalized
        and any(marker in normalized for marker in ("explanation", "explained", "analysis", "reason"))
        and any(marker in normalized for marker in ("command draft", "command drafts", "draft command", "suggested command"))
        and any(marker in normalized for marker in ("manual", "confirm", "confirmation", "copy", "not auto", "not executed"))
    )


def _is_credential_deletion_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("delete", "deleted", "clear", "cleared", "removed"))
        and any(
            marker in normalized
            for marker in ("credential", "password", "private key", "passphrase", "secure storage", "vault")
        )
    )


def _is_account_preference_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "theme" in normalized
        and any(marker in normalized for marker in ("speech language", "voice language", "language"))
        and any(marker in normalized for marker in ("account", "settings", "preference"))
    )


def _is_local_work_records_reset_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("clear", "cleared", "reset", "deleted"))
        and any(marker in normalized for marker in ("local work", "work records", "cache"))
        and "search" in normalized
        and any(marker in normalized for marker in ("document", "draft"))
        and "command" in normalized
        and "digital employee" in normalized
        and any(marker in normalized for marker in ("preference", "preferences"))
    )


def _is_server_credential_retention_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("remain", "retained", "still available", "preserved"))
        and any(marker in normalized for marker in ("server profile", "server profiles"))
        and any(marker in normalized for marker in ("ssh credential", "ssh credentials", "password", "private key"))
        and any(marker in normalized for marker in ("local reset", "local clear", "work records", "cache clear"))
    )


def _is_server_credential_clear_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("clear", "cleared", "delete", "deleted", "revoked", "removed"))
        and any(marker in normalized for marker in ("server profile", "server profiles"))
        and any(marker in normalized for marker in ("ssh credential", "ssh credentials", "password", "private key"))
        and any(marker in normalized for marker in ("separate", "explicit", "account"))
    )


def _is_status_polling_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("poll", "polling", "status"))
        and any(marker in normalized for marker in ("task", "job", "digital employee", "document"))
        and any(marker in normalized for marker in ("done", "failed", "running", "queued", "completed", "result"))
    )


def _is_realtime_update_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "realtime" in normalized
        and any(marker in normalized for marker in ("event", "update", "websocket", "ws"))
        and any(marker in normalized for marker in ("task", "document", "digital employee", "status"))
    )


def _is_notification_delivery_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("notification", "notify", "提醒"))
        and any(marker in normalized for marker in ("delivered", "shown", "received", "displayed", "appeared"))
        and any(marker in normalized for marker in ("payload", "tap", "clicked", "opened", "deep link"))
        and any(marker in normalized for marker in ("document", "export"))
        and any(marker in normalized for marker in ("digital employee", "task"))
        and "ssh" in normalized
        and any(marker in normalized for marker in ("abnormal", "disconnect", "connection", "exception", "异常"))
    )


def _is_digital_employee_handoff_warning(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "hub" in normalized
        and "tenant" in normalized
        and any(marker in normalized for marker in ("handoff", "digital employee"))
        and any(marker in normalized for marker in ("warning", "confirm", "confirmation", "preview"))
        and any(marker in normalized for marker in ("terminal", "ssh", "pasted output", "copied output", "log"))
    )


def _is_account_hub_tenant_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "account" in normalized
        and "selected" in normalized
        and "hub" in normalized
        and "tenant" in normalized
    )


def _is_no_custom_hub_url_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("no custom", "not found", "absent", "disabled"))
        and "hub" in normalized
        and "url" in normalized
        and any(marker in normalized for marker in ("setting", "settings", "ui", "surface"))
    )


def _is_bootstrap_service_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "bootstrap" in normalized
        and "user" in normalized
        and any(marker in normalized for marker in ("quota", "limit", "limits"))
        and any(marker in normalized for marker in ("feature", "flag", "flags"))
        and "service" in normalized
        and "status" in normalized
    )


def _is_network_recovery_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("offline", "unavailable", "unreachable", "不可用", "不可达"))
        and any(marker in normalized for marker in ("recover", "recovered", "online", "reachable", "restored", "恢复", "可达"))
        and any(marker in normalized for marker in ("hubcenter", "hub center", "network", "网络"))
        and any(marker in normalized for marker in ("search", "document", "export", "digital employee", "realtime"))
    )


def _is_hubcenter_probe_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    urls = _listed_https_urls(value)
    return (
        _is_auditable_note(value)
        and "hubcenter" in normalized
        and any(marker in normalized for marker in ("probe", "probed", "candidate", "selected"))
        and sorted(set(urls)) == sorted(OFFICIAL_HUBCENTER_URLS)
    )


def _is_discovered_hub_tenant_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "discovered" in normalized
        and "hub" in normalized
        and "tenant" in normalized
        and any(_is_https_url(url) for url in _listed_https_urls(value))
    )


def _is_llm_access_evidence(value: str) -> bool:
    return _is_official_llm_access_evidence(value) or _is_desktop_qr_llm_access_evidence(value)


def _is_llm_setup_restriction_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "llm" in normalized
        and any(marker in normalized for marker in ("setup", "configuration", "config", "配置"))
        and any(marker in normalized for marker in ("redemption", "兑换码", "official service", "maclaw official"))
        and any(marker in normalized for marker in ("desktop gui", "qr"))
        and any(marker in normalized for marker in ("no arbitrary", "does not accept", "no custom", "not exposed", "absent"))
        and any(marker in normalized for marker in ("endpoint", "base url", "api key", "provider url", "third-party endpoint"))
    )


def _is_login_result_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and any(marker in normalized for marker in ("login", "logged in", "authenticated", "session"))
        and "maclaw" in normalized
        and any(marker in normalized for marker in ("official", "account"))
        and "hubcenter" in normalized
    )


def _is_official_llm_access_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "llm" in normalized
        and any(marker in normalized for marker in ("available", "authorized", "access", "mode"))
        and "maclaw" in normalized
        and "official" in normalized
    )


def _is_desktop_qr_llm_access_evidence(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_auditable_note(value)
        and "llm" in normalized
        and any(marker in normalized for marker in ("available", "authorized", "access", "mode"))
        and "desktop" in normalized
        and "qr" in normalized
        and ("third-party" in normalized or "third party" in normalized)
    )


def _is_document_upload_task_id(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_trackable_id(value)
        and "document" in normalized
        and any(marker in normalized for marker in ("upload", "import"))
        and "task" in normalized
    )


def _is_document_export_job_id(value: str, format_markers: tuple[str, ...]) -> bool:
    normalized = value.strip().lower()
    return (
        _is_trackable_id(value)
        and "export" in normalized
        and "job" in normalized
        and any(marker in normalized for marker in format_markers)
    )


def _is_digital_employee_task_id(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_trackable_id(value)
        and "task" in normalized
        and (
            "digital employee" in normalized
            or "digital-employee" in normalized
            or "employee" in normalized
        )
    )


def _is_ssh_evidence(value: str, markers: tuple[str, ...]) -> bool:
    return _is_auditable_note(value) and _contains_any(value, markers)


def _android_major_version(value: str) -> int | None:
    match = re.search(r"android\s+(\d+)", value.strip().lower())
    if not match:
        return None
    return int(match.group(1))


def _is_https_url(value: str) -> bool:
    normalized = value.strip().lower()
    return normalized.startswith("https://") and "." in normalized.removeprefix("https://")


def _is_calendar_date(value: str) -> bool:
    normalized = value.strip()
    if not DATE_RE.fullmatch(normalized):
        return False
    try:
        datetime.strptime(normalized, "%Y-%m-%d")
    except ValueError:
        return False
    return True


def _parse_calendar_date(value: str) -> datetime | None:
    normalized = value.strip()
    if not DATE_RE.fullmatch(normalized):
        return None
    try:
        return datetime.strptime(normalized, "%Y-%m-%d")
    except ValueError:
        return None


def _canonical_version_build(value: str) -> str | None:
    normalized = value.strip()
    plus_match = re.search(r"\b(\d+(?:\.\d+){1,3}\+\d+)\b", normalized)
    if plus_match:
        return plus_match.group(1)
    words_match = re.search(
        r"(?i)\b(?:version|v)\s*(\d+(?:\.\d+){1,3}).*\bbuild\s*(\d+)\b",
        normalized,
    )
    if words_match:
        return f"{words_match.group(1)}+{words_match.group(2)}"
    return None


def _listed_https_urls(value: str) -> list[str]:
    return HTTPS_URL_RE.findall(value.strip())


def _url_origin(value: str) -> tuple[str, str]:
    parsed = urlparse(value.strip())
    return parsed.scheme.lower(), parsed.netloc.lower()


def _same_origin(left: str, right: str) -> bool:
    return _url_origin(left) == _url_origin(right)


def _is_trackable_android_artifact_path(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        len(normalized) >= 6
        and normalized not in PLACEHOLDER_VALUES
        and "debug" not in normalized
        and normalized.endswith(ANDROID_ARTIFACT_SUFFIXES)
        and any(marker in normalized for marker in ("release", "signed", "internal"))
    )


def _is_android_signing_identity(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_non_placeholder(value, 6)
        and "debug" not in normalized
        and any(
            marker in normalized
            for marker in (
                "release",
                "signed",
                "internal",
                "upload",
                "keystore",
                "certificate",
                "cert",
                "play app signing",
            )
        )
        and any(
            marker in normalized
            for marker in (
                "alias",
                "sha-1",
                "sha1",
                "sha-256",
                "sha256",
                "fingerprint",
                "upload key",
                "upload certificate",
                "certificate id",
                "cert id",
            )
        )
    )


def _is_installer_channel(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        _is_non_placeholder(value, 6)
        and "debug" not in normalized
        and any(
            marker in normalized
            for marker in (
                "internal app sharing",
                "play internal",
                "internal testing",
                "closed testing",
                "open testing",
                "play console",
                "mdm",
                "enterprise",
                "firebase app distribution",
            )
        )
    )


def _is_trackable_ios_archive(value: str) -> bool:
    normalized = value.strip().lower()
    if len(normalized) < 6 or normalized in PLACEHOLDER_VALUES:
        return False
    return normalized.endswith(".xcarchive") or TESTFLIGHT_BUILD_RE.search(value) is not None


def _is_local_ios_archive_path(value: str) -> bool:
    return value.strip().lower().endswith(".xcarchive")


def _mentions_ios_profiles(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        len(normalized) >= 12
        and normalized not in PLACEHOLDER_VALUES
        and "runner" in normalized
        and ("share extension" in normalized or "shareextension" in normalized)
        and IOS_PROFILE_REFERENCE_RE.search(value) is not None
    )


def _resolve_artifact_path(value: str, record_dir: Path) -> Path:
    path = Path(value.strip().strip('"'))
    if path.is_absolute():
        return path
    return record_dir / path


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def record_scope_from_path(path: Path) -> str:
    match = QA_BUILD_RECORD_FILENAME_RE.fullmatch(path.name)
    if match is None:
        return "android-ios"
    return match.group("scope")


def scoped_values(values: dict[str, list[str]], scope: str) -> dict[str, list[str]]:
    if scope == "android-ios":
        return values
    scoped: dict[str, list[str]] = {}
    for field, field_values in values.items():
        if scope == "android" and field in IOS_ONLY_FIELDS:
            continue
        if scope == "ios" and field in ANDROID_ONLY_FIELDS:
            continue
        if field in SCOPED_DUAL_PLATFORM_FIELDS:
            if scope == "android":
                scoped[field] = field_values[:1]
            else:
                scoped[field] = field_values[1:2] if len(field_values) > 1 else field_values[:1]
            continue
        scoped[field] = field_values
    return scoped


def required_field_counts_for_scope(scope: str) -> dict[str, int]:
    if scope == "android-ios":
        return REQUIRED_FIELD_COUNTS
    scoped = scoped_values(
        {field: ["x"] * count for field, count in REQUIRED_FIELD_COUNTS.items()},
        scope,
    )
    return {field: len(values) for field, values in scoped.items()}


def missing_required_fields(
    values: dict[str, list[str]],
    *,
    scope: str = "android-ios",
) -> list[str]:
    missing = []
    values = scoped_values(values, scope)
    llm_modes = [value for value in values.get("LLM access mode", []) if value]
    for field, required_count in sorted(required_field_counts_for_scope(scope).items()):
        filled_count = _filled_count(values, field)
        if filled_count < required_count:
            suffix = (
                f" ({required_count} entries required, {filled_count} filled)"
                if required_count > 1
                else ""
            )
            missing.append(f"{field}{suffix}")
        if filled_count > required_count:
            missing.append(
                f"{field} ({required_count} entries required, {filled_count} filled)"
            )
    for field, expected in EXACT_VALUE_FIELDS.items():
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(value == expected for value in field_values):
            missing.append(f"{field} must be {expected}")
    hubcenter_values = [value for value in values.get("HubCenter candidates", []) if value]
    if hubcenter_values and not all(
        sorted(_listed_https_urls(value)) == sorted(OFFICIAL_HUBCENTER_URLS)
        for value in hubcenter_values
    ):
        missing.append("HubCenter candidates must list exactly the three preset official HubCenters")
    selected_hubcenter_values = [value for value in values.get("Selected HubCenter URL", []) if value]
    if selected_hubcenter_values and not all(
        value in OFFICIAL_HUBCENTER_URLS for value in selected_hubcenter_values
    ):
        missing.append("Selected HubCenter URL must be one of the preset official HubCenters")
    for field in URL_FIELDS:
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_https_url(value) for value in field_values):
            missing.append(f"{field} must be an HTTPS URL")
    discovered_hub_values = [
        value for value in values.get("Discovered Hub URL", []) if value and _is_https_url(value)
    ]
    if discovered_hub_values and not all(
        value not in OFFICIAL_HUBCENTER_URLS for value in discovered_hub_values
    ):
        missing.append("Discovered Hub URL must be a tenant Hub, not a HubCenter URL")
    for field in ["API base URL confirmation", "Realtime Hub URL confirmation"]:
        field_values = [value for value in values.get(field, []) if value and _is_https_url(value)]
        if (
            discovered_hub_values
            and field_values
            and not any(
                _same_origin(value, discovered_hub)
                for value in field_values
                for discovered_hub in discovered_hub_values
            )
        ):
            missing.append(f"{field} must use the discovered Hub origin")
    for field in sorted(MANUAL_EVIDENCE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_auditable_note(value) for value in field_values):
            missing.append(f"{field} must contain auditable QA evidence, not a placeholder")
    final_automated_checks = {
        "Release handoff result": _is_release_handoff_evidence,
        "Runtime boundary verification result": _is_runtime_boundary_result_evidence,
        "Automated release gates result": _is_automated_release_gates_evidence,
    }
    for field, check in sorted(final_automated_checks.items()):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(check(value) for value in field_values):
            missing.append(f"{field} {FINAL_AUTOMATED_EVIDENCE_FIELDS[field]}")
    for field, markers in sorted(PERMISSION_EVIDENCE_FIELDS.items()):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_permission_evidence(value, markers) for value in field_values
        ):
            missing.append(f"{field} must describe permission prompt/result evidence")
    for field in sorted(SHARE_TEXT_EVIDENCE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_share_text_evidence(value) for value in field_values):
            missing.append(f"{field} must describe assistant share-to-app evidence")
    for field in sorted(SHARE_URL_EVIDENCE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_share_url_evidence(value) for value in field_values):
            missing.append(f"{field} must describe assistant URL/citation share-to-app evidence")
    for field, markers in sorted(SHARE_DOCUMENT_EVIDENCE_FIELDS.items()):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_share_document_evidence(value, markers) for value in field_values
        ):
            missing.append(f"{field} must describe document import/upload share-to-app evidence")
    for field in sorted(AI_SEARCH_EVIDENCE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_ai_search_query_evidence(value) for value in field_values):
            missing.append(f"{field} must include the actual AI search query or question")
    for field in sorted(MOBILE_INPUT_EVIDENCE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_mobile_input_evidence(value) for value in field_values):
            missing.append(
                f"{field} must describe voice transcription and photo/image assistant input results"
            )
    for field in sorted(CITATION_EVIDENCE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_citation_evidence(value) for value in field_values):
            missing.append(f"{field} must identify visible citations, sources, or URLs")
    for field in sorted(SHARED_RESULT_EVIDENCE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_shared_result_evidence(value) for value in field_values):
            missing.append(f"{field} must describe copy, export, or system-share evidence")
    for field in sorted(DOCUMENT_DRAFT_EVIDENCE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_document_draft_from_search_evidence(value) for value in field_values
        ):
            missing.append(
                f"{field} must describe search-result draft creation for every document template"
            )
    for field in sorted(SIGNED_INSTALL_RESULT_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_signed_install_result_evidence(value) for value in field_values
        ):
            missing.append(f"{field} must describe signed install and app launch evidence")
    for field, markers in sorted(SIGNED_INSTALL_PLATFORM_MARKERS.items()):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_signed_install_platform_evidence(value, markers) for value in field_values
        ):
            missing.append(f"{field} must describe the matching platform install evidence")
    for field in sorted(SSH_AI_ANALYSIS_WARNING_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_ssh_ai_analysis_warning_evidence(value) for value in field_values
        ):
            missing.append(
                f"{field} must describe preview confirmation and sensitive-data warning evidence"
            )
    for field in sorted(SSH_AI_RESULT_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_ssh_ai_result_evidence(value) for value in field_values
        ):
            missing.append(
                f"{field} must describe AI explanation, command drafts, and manual execution evidence"
            )
    for field in sorted(CREDENTIAL_DELETION_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_credential_deletion_evidence(value) for value in field_values
        ):
            missing.append(
                f"{field} must describe cleared SSH credential storage evidence"
            )
    for field in sorted(ACCOUNT_PREFERENCE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_account_preference_evidence(value) for value in field_values
        ):
            missing.append(f"{field} must describe account theme and speech language evidence")
    for field in sorted(LOCAL_WORK_RECORDS_RESET_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_local_work_records_reset_evidence(value) for value in field_values
        ):
            missing.append(
                f"{field} must describe clearing local work records and app preferences"
            )
    for field in sorted(SERVER_CREDENTIAL_RETENTION_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_server_credential_retention_evidence(value) for value in field_values
        ):
            missing.append(
                f"{field} must describe server profiles and SSH credentials retained after local reset"
            )
    for field in sorted(SERVER_CREDENTIAL_CLEAR_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_server_credential_clear_evidence(value) for value in field_values
        ):
            missing.append(
                f"{field} must describe separate explicit server profile and SSH credential clearing"
            )
    for field in sorted(STATUS_POLLING_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_status_polling_evidence(value) for value in field_values
        ):
            missing.append(f"{field} must describe task/job status polling evidence")
    for field in sorted(REALTIME_UPDATE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_realtime_update_evidence(value) for value in field_values
        ):
            missing.append(f"{field} must describe realtime task/document update evidence")
    for field in sorted(NOTIFICATION_DELIVERY_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_notification_delivery_evidence(value) for value in field_values
        ):
            missing.append(
                f"{field} must describe delivered document, digital employee, and SSH abnormal notifications with payload/open evidence"
            )
    for field in sorted(IOS_URL_SCHEME_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_ios_url_scheme_evidence(value) for value in field_values):
            missing.append(f"{field} must mention both maclaw and ShareMedia URL schemes")
    handoff_values = [
        value for value in values.get("Digital employee handoff warning, if used", []) if value
    ]
    if handoff_values and not all(
        _is_digital_employee_handoff_warning(value) for value in handoff_values
    ):
        missing.append(
            "Digital employee handoff warning, if used must describe Hub/tenant handoff warning evidence"
        )
    for field in sorted(ACCOUNT_HUB_TENANT_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_account_hub_tenant_evidence(value) for value in field_values
        ):
            missing.append(f"{field} must describe selected Hub and tenant account-screen evidence")
    for field in sorted(NO_CUSTOM_HUB_URL_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_no_custom_hub_url_evidence(value) for value in field_values
        ):
            missing.append(f"{field} must describe absence of custom Hub URL settings")
    for field in sorted(BOOTSTRAP_SERVICE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_bootstrap_service_evidence(value) for value in field_values
        ):
            missing.append(f"{field} must describe bootstrap user/quota/features/service status")
    for field in sorted(NETWORK_RECOVERY_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_network_recovery_evidence(value) for value in field_values
        ):
            missing.append(
                f"{field} must describe offline warning and recovered HubCenter network/service evidence"
            )
    for field in sorted(HUBCENTER_PROBE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_hubcenter_probe_evidence(value) for value in field_values
        ):
            missing.append(f"{field} must describe probing exactly the three official HubCenters")
    for field in sorted(DISCOVERED_HUB_TENANT_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_discovered_hub_tenant_evidence(value) for value in field_values
        ):
            missing.append(f"{field} must describe discovered Hub URL and tenant evidence")
    for field in sorted(LLM_ACCESS_EVIDENCE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_llm_access_evidence(value) for value in field_values
        ):
            missing.append(f"{field} must describe MaClaw official or desktop QR LLM access evidence")
    for field in sorted(LLM_SETUP_RESTRICTION_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_llm_setup_restriction_evidence(value) for value in field_values
        ):
            missing.append(
                f"{field} must describe official redemption or desktop GUI QR only, with no arbitrary third-party endpoint fields"
            )
    for field in sorted(LOGIN_RESULT_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_login_result_evidence(value) for value in field_values
        ):
            missing.append(f"{field} must describe MaClaw official account login through HubCenter")
    llm_evidence_values = [value for value in values.get("LLM access evidence", []) if value]
    if llm_evidence_values and all(value == "maclaw_official" for value in llm_modes):
        if not all(_is_official_llm_access_evidence(value) for value in llm_evidence_values):
            missing.append("LLM access evidence must match maclaw_official mode")
    if llm_evidence_values and any(value == "desktop_qr_third_party" for value in llm_modes):
        if not all(_is_desktop_qr_llm_access_evidence(value) for value in llm_evidence_values):
            missing.append("LLM access evidence must match desktop_qr_third_party mode")
    for field, markers in sorted(SSH_EVIDENCE_FIELDS.items()):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_ssh_evidence(value, markers) for value in field_values):
            missing.append(f"{field} must describe the expected SSH smoke-test evidence")
    device_values = [value for value in values.get("Device model / OS", []) if value]
    if device_values and not all(_is_device_os_note(value) for value in device_values):
        missing.append("Device model / OS must include device model and Android/iOS OS version")
    if device_values:
        normalized_devices = [value.lower() for value in device_values]
        if scope == "android-ios" and len(normalized_devices) >= 2:
            if "android" not in normalized_devices[0]:
                missing.append("First Device model / OS entry must be the Android QA device")
            if "ios" not in normalized_devices[1]:
                missing.append("Second Device model / OS entry must be the iOS QA device")
        if scope == "android-ios" and (
            not any("android" in value for value in normalized_devices) or not any(
            "ios" in value for value in normalized_devices
            )
        ):
            missing.append("Device model / OS must include both Android and iOS devices")
        if scope == "android" and not any("android" in value for value in normalized_devices):
            missing.append("Device model / OS must include an Android QA device")
        if scope == "ios" and not any("ios" in value for value in normalized_devices):
            missing.append("Device model / OS must include an iOS QA device")
        android_versions = [
            version
            for value in device_values
            for version in [_android_major_version(value)]
            if version is not None
        ]
        if scope != "ios" and android_versions and not any(version >= 13 for version in android_versions):
            missing.append("Device model / OS must include at least one Android 13+ device")
    for field in sorted(DUAL_PLATFORM_EVIDENCE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if scope == "android-ios" and len(field_values) >= 2:
            if not _mentions_android(field_values[0]):
                missing.append(f"First {field} entry must be Android evidence")
            if not _mentions_ios(field_values[1]):
                missing.append(f"Second {field} entry must be iOS evidence")
            if not any(_mentions_android(value) for value in field_values) or not any(
                _mentions_ios(value) for value in field_values
            ):
                missing.append(f"{field} must include both Android and iOS evidence")
    for field in sorted(OPTIONAL_AUDITABLE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_auditable_note(value) for value in field_values):
            missing.append(f"{field} must contain auditable QA evidence, not a placeholder")
    for field in sorted(OPTIONAL_ATTACHMENT_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_attachment_evidence(value) for value in field_values):
            missing.append(f"{field} must reference a traceable evidence file or attachment ID")
    for field, min_len in sorted(NON_PLACEHOLDER_FIELDS.items()):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_non_placeholder(value, min_len) for value in field_values):
            missing.append(f"{field} must contain trackable QA evidence, not a placeholder")
    branch_values = [value for value in values.get("Branch", []) if value]
    if branch_values and not all(_is_branch_name(value) for value in branch_values):
        missing.append("Branch must be a trackable git branch name")
    maclaw_accounts = [value for value in values.get("MaClaw account", []) if value]
    if maclaw_accounts and not all(_is_maclaw_account(value) for value in maclaw_accounts):
        missing.append("MaClaw account must identify a trackable MaClaw account email or account ID")
    tenant_ids = [value for value in values.get("Tenant ID", []) if value]
    if tenant_ids and not all(_is_tenant_id(value) for value in tenant_ids):
        missing.append("Tenant ID must be a trackable tenant identifier")
    flutter_versions = [value for value in values.get("Flutter version", []) if value]
    if flutter_versions and not all(_is_flutter_version(value) for value in flutter_versions):
        missing.append("Flutter version must contain a trackable Flutter version")
    version_build_values = [value for value in values.get("Version/build number", []) if value]
    if version_build_values and not all(
        _is_version_build_number(value) for value in version_build_values
    ):
        missing.append("Version/build number must include app version and build number")
    if llm_modes and not all(value in LLM_ACCESS_MODES for value in llm_modes):
        missing.append("LLM access mode must be maclaw_official or desktop_qr_third_party")
    qr_values = [value for value in values.get("Desktop GUI QR authorization ID", []) if value]
    if all(value == "maclaw_official" for value in llm_modes) and qr_values:
        if not all(value == OFFICIAL_LLM_QR_AUTH_ID for value in qr_values):
            missing.append(
                "Desktop GUI QR authorization ID must be not-used-official-mode for official LLM access"
            )
    if any(value == "desktop_qr_third_party" for value in llm_modes):
        if not qr_values or not all(_is_trackable_id(value) for value in qr_values):
            missing.append(
                "Desktop GUI QR authorization ID must be trackable for third-party LLM access"
            )
    sha_values = [value for value in values.get("SHA256", []) if value]
    if sha_values and not all(SHA256_RE.fullmatch(value) for value in sha_values):
        missing.append("SHA256 must be 64 hexadecimal characters")
    artifact_paths = [value for value in values.get("Artifact path", []) if value]
    if artifact_paths and not all(
        _is_trackable_android_artifact_path(value) for value in artifact_paths
    ):
        missing.append("Artifact path must point to a signed .apk or .aab artifact")
    signing_values = [value for value in values.get("Signing identity", []) if value]
    if signing_values and not all(
        _is_android_signing_identity(value) for value in signing_values
    ):
        missing.append(
            "Signing identity must identify a non-debug release/internal signing identity"
        )
    installer_values = [value for value in values.get("Installer channel", []) if value]
    if installer_values and not all(
        _is_installer_channel(value) for value in installer_values
    ):
        missing.append(
            "Installer channel must identify a non-debug auditable distribution channel"
        )
    ios_archives = [value for value in values.get("Archive/TestFlight build", []) if value]
    if ios_archives and not all(_is_trackable_ios_archive(value) for value in ios_archives):
        missing.append("Archive/TestFlight build must identify an .xcarchive or TestFlight build")
    team_ids = [value for value in values.get("Team ID", []) if value]
    if team_ids and not all(APPLE_TEAM_ID_RE.fullmatch(value) for value in team_ids):
        missing.append("Team ID must be a 10-character Apple team identifier")
    provisioning_profiles = [
        value for value in values.get("Provisioning profiles", []) if value
    ]
    if provisioning_profiles and not all(
        _mentions_ios_profiles(value) for value in provisioning_profiles
    ):
        missing.append(
            "Provisioning profiles must mention Runner, Share Extension, and trackable profile ID/file/name"
        )
    commit_values = [value for value in values.get("Git commit", []) if value]
    if commit_values and not all(GIT_COMMIT_RE.fullmatch(value) for value in commit_values):
        missing.append("Git commit must be a 7-40 character hexadecimal SHA")
    for field in DATE_FIELDS:
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_calendar_date(value) for value in field_values):
            missing.append(f"{field} must use a valid YYYY-MM-DD calendar date")
        valid_dates = [
            parsed
            for value in field_values
            for parsed in [_parse_calendar_date(value)]
            if parsed is not None
        ]
        today = datetime.now().date()
        if valid_dates and any(value.date() > today for value in valid_dates):
            missing.append(f"{field} must not be in the future")
    date_values = [value for value in values.get("Date", []) if value]
    approval_date_values = [value for value in values.get("Approval date", []) if value]
    if date_values and approval_date_values:
        record_date = _parse_calendar_date(date_values[0])
        approval_date = _parse_calendar_date(approval_date_values[0])
        if record_date is not None and approval_date is not None and approval_date < record_date:
            missing.append("Approval date must be on or after Date")
    tester_values = [value.strip().lower() for value in values.get("Tester", []) if value]
    approver_values = [value.strip().lower() for value in values.get("Approved by", []) if value]
    if tester_values and approver_values and tester_values[0] == approver_values[0]:
        missing.append("Approved by must be different from Tester")
    for field in TASK_ID_FIELDS:
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_trackable_id(value) for value in field_values):
            missing.append(f"{field} must contain a trackable task/job ID")
    for field in sorted(DOCUMENT_UPLOAD_TASK_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_document_upload_task_id(value) for value in field_values
        ):
            missing.append(f"{field} must identify a document upload/import task")
    for field, markers in sorted(DOCUMENT_EXPORT_JOB_FIELDS.items()):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_document_export_job_id(value, markers) for value in field_values
        ):
            missing.append(f"{field} must identify a matching document export job")
    for field in sorted(DOCUMENT_EXPORT_SHARE_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_document_export_share_evidence(value) for value in field_values
        ):
            missing.append(
                f"{field} must describe exported PDF, Word, and Markdown download/share evidence"
            )
    for field in sorted(DIGITAL_EMPLOYEE_TASK_FIELDS):
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(
            _is_digital_employee_task_id(value) for value in field_values
        ):
            missing.append(f"{field} must identify a digital employee task")
    task_ids = [
        value
        for field in TASK_ID_FIELDS
        for value in values.get(field, [])
        if value and _is_trackable_id(value)
    ]
    status_values = [value for value in values.get("Status polling result", []) if value]
    if task_ids and status_values and not all(
        _mentions_any_trackable_id(value, task_ids) for value in status_values
    ):
        missing.append("Status polling result must reference a recorded task/job ID")
    realtime_values = [value for value in values.get("Realtime update evidence", []) if value]
    if task_ids and realtime_values and not all(
        _mentions_any_trackable_id(value, task_ids) for value in realtime_values
    ):
        missing.append("Realtime update evidence must reference a recorded task/job ID")
    for field in PASS_DECISION_FIELDS:
        field_values = [value for value in values.get(field, []) if value]
        if field_values and not all(_is_positive_decision(value) for value in field_values):
            missing.append(f"{field} must say passed or waived")
    waived_fields = _waived_decision_fields(values)
    if waived_fields:
        waiver_notes = [value for value in values.get("Known issues / waivers", []) if value]
        if (
            not waiver_notes
            or not all(_is_auditable_note(value) for value in waiver_notes)
            or not all(
                _summarizes_waived_gate(field, waiver_notes) for field in waived_fields
            )
        ):
            missing.append(
                "Known issues / waivers must summarize every final gate waiver"
            )
    return missing


def local_artifact_errors(values: dict[str, list[str]], record_dir: Path) -> list[str]:
    artifact_paths = [
        value
        for value in values.get("Artifact path", [])
        if value and _is_trackable_android_artifact_path(value)
    ]
    sha_values = [value.lower() for value in values.get("SHA256", []) if value]
    if not artifact_paths or not sha_values:
        return []
    errors = []
    for raw_path in artifact_paths:
        artifact_path = _resolve_artifact_path(raw_path, record_dir)
        if not artifact_path.exists():
            errors.append(f"Local signed artifact is missing: {raw_path}")
            continue
        if not artifact_path.is_file():
            errors.append(f"Local signed artifact is not a file: {raw_path}")
            continue
        actual = _sha256_file(artifact_path)
        if actual not in sha_values:
            errors.append(f"SHA256 does not match local artifact {raw_path}")
    for raw_path in values.get("Archive/TestFlight build", []):
        if not raw_path or not _is_local_ios_archive_path(raw_path):
            continue
        archive_path = _resolve_artifact_path(raw_path, record_dir)
        if not archive_path.exists():
            errors.append(f"Local iOS archive is missing: {raw_path}")
            continue
        if not archive_path.is_dir():
            errors.append(f"Local iOS archive is not a directory: {raw_path}")
    return errors


def qa_build_record_filename_errors(path: Path, values: dict[str, list[str]]) -> list[str]:
    if path.parent.name != "qa-builds":
        return []
    match = QA_BUILD_RECORD_FILENAME_RE.fullmatch(path.name)
    if not match:
        return [
            "QA build record filename must be YYYY-MM-DD-<android|ios|android-ios>-<version+build>.md",
        ]
    errors = []
    filename_date = match.group("date")
    record_dates = [value.strip() for value in values.get("Date", []) if value]
    if record_dates and filename_date not in record_dates:
        errors.append("QA build record filename date must match Date")
    filename_version = match.group("version")
    record_versions = [
        version
        for value in values.get("Version/build number", [])
        for version in [_canonical_version_build(value)]
        if version is not None
    ]
    if record_versions and filename_version not in record_versions:
        errors.append("QA build record filename version/build must match Version/build number")
    return errors


def raw_secret_errors(text: str) -> list[str]:
    if (
        PRIVATE_KEY_BLOCK_RE.search(text)
        or SECRET_ASSIGNMENT_RE.search(text)
        or TOKEN_LITERAL_RE.search(text)
        or JWT_LITERAL_RE.search(text)
        or AUTHORIZATION_HEADER_RE.search(text)
        or HTTP_SECRET_HEADER_RE.search(text)
        or URL_EMBEDDED_CREDENTIAL_RE.search(text)
    ):
        return [
            "QA build record must not contain raw secrets; use redacted evidence or attachment IDs",
        ]
    return []


def validate_file(path: Path) -> list[str]:
    if not path.exists():
        return [f"QA build record file does not exist: {path}"]
    if path.is_dir():
        return [f"QA build record path must be a markdown file, not a directory: {path}"]
    if path.suffix.lower() != ".md":
        return [f"QA build record path must be a markdown file: {path}"]
    if path.name.lower() == "readme.md":
        return ["QA build record path must point to a completed record, not README.md"]
    if path.name == "qa_build_record_template.md":
        return ["QA build record path must point to a completed record, not the template"]
    text = path.read_text(encoding="utf-8")
    values = parse_record(text)
    scope = record_scope_from_path(path)
    in_scope_values = scoped_values(values, scope)
    return (
        raw_secret_errors(text)
        + qa_build_record_filename_errors(path, in_scope_values)
        + missing_required_fields(values, scope=scope)
        + local_artifact_errors(in_scope_values, path.parent)
    )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Validate a completed MaClaw Mobile signed-build QA record.",
    )
    parser.add_argument("record", type=Path, help="Completed QA build record markdown file")
    args = parser.parse_args(argv)

    errors = validate_file(args.record)
    if errors:
        print("QA build record validation failed:")
        for error in errors:
            print(f"- {error}")
        return 1

    print("QA build record contains all required release evidence fields.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
