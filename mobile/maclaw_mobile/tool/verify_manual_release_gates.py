from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path

import qa_release_evidence_links
import release_evidence_commands


@dataclass(frozen=True)
class ManualGate:
    gate: str
    audit_keywords: tuple[str, ...]
    checklist_keywords: tuple[str, ...]
    final_decision_field: str
    evidence_keywords: tuple[str, ...] = ()


CANONICAL_MANUAL_GATES = (
    ManualGate(
        "Android signed internal build",
        ("Signed Android", "Android 13+", "install result"),
        ("Android Signed Build", "signed APK/AAB", "Android 13+", "install", "result"),
        "Android manual gates passed",
    ),
    ManualGate(
        "Android share-to-app",
        ("Android real-device share-to-app", "text", "CSV"),
        ("Android Share-To-App", "Plain text", "URL", "CSV"),
        "Android manual gates passed",
    ),
    ManualGate(
        "Android runtime permissions",
        (
            "Android runtime permission prompts",
            "notification",
            "camera",
            "permission-grant:<id>",
        ),
        (
            "Android Runtime Permissions",
            "Notification permission",
            "Camera permission",
            "permission-grant:<id>",
        ),
        "Android manual gates passed",
        ("permission-grant:<id>",),
    ),
    ManualGate(
        "iOS Share Extension target",
        ("iOS signed Runner", "Share Extension", "app-group"),
        ("iOS Signing And Share Extension", "Share Extension", "group"),
        "iOS manual gates passed",
    ),
    ManualGate(
        "iOS share-to-app",
        ("iOS real-device/TestFlight share-to-app", "text", "CSV"),
        ("iOS Share-To-App", "Plain text", "URL", "CSV"),
        "iOS manual gates passed",
    ),
    ManualGate(
        "iOS runtime permissions",
        (
            "iOS runtime permission prompts",
            "speech recognition",
            "notifications",
            "permission-grant:<id>",
        ),
        (
            "iOS Runtime Permissions",
            "Speech recognition",
            "Notification permission",
            "permission-grant:<id>",
        ),
        "iOS manual gates passed",
        ("permission-grant:<id>",),
    ),
    ManualGate(
        "Backend SSH session against real server",
        (
            "Real backend SSH session smoke test",
            "connect result",
            "credential deletion",
        ),
        (
            "Backend SSH Session Smoke Test",
            "Connect",
            "read-only command",
            "credential",
        ),
        "Manual SSH smoke passed",
    ),
    ManualGate(
        "Hub discovery smoke test",
        (
            "Hub discovery smoke test",
            "selected HubCenter",
            "MaClaw logo",
            "Flutter placeholder",
            "AI助手",
            "查信息",
            "realtime Hub URL",
        ),
        (
            "Hub Discovery And Service Smoke Test",
            "selected HubCenter",
            "MaClaw logo",
            "Flutter placeholder",
            "AI助手",
            "查信息",
            "voice input",
            "realtime",
            "typed notification payloads",
            "digital-employee-task:",
            "server-profile:",
        ),
        "Hub discovery smoke passed",
    ),
)

FINAL_RELEASE_EVIDENCE_LOG_COMMAND = (
    release_evidence_commands.verify_final_release_evidence_command(
        scope=release_evidence_commands.DEFAULT_SCOPE,
        version=release_evidence_commands.DEFAULT_VERSION,
    )
)
QA_RELEASE_EVIDENCE_LINK_COMMAND = (
    release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND
)
AUDIT_QA_RECORD_VALIDATION_KEYWORDS = (
    "tool/validate_qa_build_record.py",
    "without secret redaction failures",
    "tool/qa_build_record_report.py",
    "redacted evidence",
)
SCOPED_INTERNAL_QA_COMMANDS = (
    release_evidence_commands.release_status_report_command(scope="android"),
    release_evidence_commands.release_handoff_command(
        version=release_evidence_commands.DEFAULT_VERSION,
        scope="android",
    ),
    release_evidence_commands.qa_preflight_command(scope="android"),
    release_evidence_commands.release_status_report_command(
        scope="ios",
        team_id=release_evidence_commands.DEFAULT_TEAM_ID,
        export_method="development",
    ),
    release_evidence_commands.release_handoff_command(
        version=release_evidence_commands.DEFAULT_VERSION,
        scope="ios",
        team_id=release_evidence_commands.DEFAULT_TEAM_ID,
        export_method="development",
    ),
    release_evidence_commands.qa_preflight_command(
        scope="ios",
        team_id=release_evidence_commands.DEFAULT_TEAM_ID,
        export_method="development",
    ),
)
QA_BUILDS_README_SCOPED_INTERNAL_QA_COMMANDS = (
    release_evidence_commands.release_status_report_command(scope="android"),
    release_evidence_commands.release_handoff_command(
        version="1.0.0+42",
        scope="android",
    ),
    release_evidence_commands.qa_preflight_command(scope="android"),
    release_evidence_commands.release_status_report_command(
        scope="ios",
        team_id=release_evidence_commands.DEFAULT_TEAM_ID,
        export_method="development",
    ),
    release_evidence_commands.release_handoff_command(
        version="1.0.0+42",
        scope="ios",
        team_id=release_evidence_commands.DEFAULT_TEAM_ID,
        export_method="development",
    ),
    release_evidence_commands.qa_preflight_command(
        scope="ios",
        team_id=release_evidence_commands.DEFAULT_TEAM_ID,
        export_method="development",
    ),
)


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _section(text: str, heading: str) -> str:
    marker = f"## {heading}"
    start = text.find(marker)
    if start < 0:
        raise ValueError(f"Missing section: {marker}")
    after_heading = text.find("\n", start)
    if after_heading < 0:
        return ""
    next_heading = text.find("\n## ", after_heading + 1)
    if next_heading < 0:
        return text[after_heading + 1 :]
    return text[after_heading + 1 : next_heading]


def _table_rows(section: str) -> list[dict[str, str]]:
    rows: list[dict[str, str]] = []
    headers: list[str] | None = None
    for line in section.splitlines():
        stripped = line.strip()
        if not stripped.startswith("|"):
            continue
        cells = [cell.strip() for cell in stripped.strip("|").split("|")]
        if all(set(cell) <= {"-", " "} for cell in cells):
            continue
        if headers is None:
            headers = cells
            continue
        if len(cells) != len(headers):
            raise ValueError(f"Malformed table row: {line}")
        rows.append(dict(zip(headers, cells)))
    if headers is None:
        raise ValueError("Missing manual release gate table.")
    return rows


def _bullet_items(section: str) -> list[str]:
    items: list[str] = []
    current: list[str] = []
    for line in section.splitlines():
        if line.startswith("- "):
            if current:
                items.append(" ".join(current).strip())
            current = [line[2:].strip()]
        elif current and line.startswith("  "):
            current.append(line.strip())
    if current:
        items.append(" ".join(current).strip())
    return items


def _contains_all(text: str, keywords: tuple[str, ...]) -> bool:
    normalized = text.casefold()
    return all(keyword.casefold() in normalized for keyword in keywords)


def _has_android_handoff_with_ios_options(text: str) -> bool:
    return any(
        "python3 tool/release_handoff.py" in line
        and re.search(r"--scope\s+android(?:\s|$)", line) is not None
        and "--team-id" in line
        for line in text.splitlines()
    )


def validate_manual_release_gates(root: Path) -> list[str]:
    docs = root / "docs"
    evidence = (docs / "release_evidence.md").read_text(encoding="utf-8")
    audit = (docs / "release_audit.md").read_text(encoding="utf-8")
    checklist = (docs / "qa_device_checklist.md").read_text(encoding="utf-8")
    template = (docs / "qa_build_record_template.md").read_text(encoding="utf-8")
    qa_builds_readme = (docs / "qa-builds" / "README.md").read_text(
        encoding="utf-8",
    )

    errors: list[str] = []
    expected_gates = [gate.gate for gate in CANONICAL_MANUAL_GATES]

    rows = _table_rows(_section(evidence, "Manual Release Gates"))
    actual_gates = [row.get("Gate", "") for row in rows]
    if actual_gates != expected_gates:
        errors.append(
            "Manual Release Gates table must list the canonical gates in order: "
            + ", ".join(expected_gates)
        )

    for row in rows:
        gate_name = row.get("Gate", "")
        required = row.get("Required evidence", "")
        if not gate_name or not required:
            errors.append("Manual Release Gates rows must include gate and evidence.")
        if required.lower() in {"ok", "yes", "done", "tbd", "todo"}:
            errors.append(f"{gate_name} required evidence is not auditable.")
        matching_gate = next(
            (gate for gate in CANONICAL_MANUAL_GATES if gate.gate == gate_name),
            None,
        )
        if matching_gate is not None and not _contains_all(
            required,
            matching_gate.evidence_keywords,
        ):
            errors.append(
                f"Manual Release Gates evidence for {gate_name} must include "
                + ", ".join(matching_gate.evidence_keywords)
                + ".",
            )

    blockers = _bullet_items(_section(audit, "Remaining Release Blockers"))
    if not _contains_all(audit, AUDIT_QA_RECORD_VALIDATION_KEYWORDS):
        errors.append(
            "release_audit.md must require completed signed-build QA records "
            "to pass validation without secret redaction failures and point "
            "operators to qa_build_record_report.py remediation."
        )
    for gate in CANONICAL_MANUAL_GATES:
        if not any(_contains_all(blocker, gate.audit_keywords) for blocker in blockers):
            errors.append(
                f"release_audit.md Remaining Release Blockers must cover {gate.gate}."
            )
        checklist_heading = gate.checklist_keywords[0]
        try:
            checklist_section = _section(checklist, checklist_heading)
        except ValueError:
            checklist_section = ""
        if not _contains_all(
            checklist_heading + "\n" + checklist_section,
            gate.checklist_keywords,
        ):
            errors.append(
                f"qa_device_checklist.md must include executable QA steps for {gate.gate}."
            )

    final_decision = _section(template, "Final Release Decision")
    for field in sorted({gate.final_decision_field for gate in CANONICAL_MANUAL_GATES}):
        expected = f"{field}: passed / waived with reason"
        if expected not in final_decision:
            errors.append(f"QA build record final decision must include `{expected}`.")
    if "Automated gates passed: passed / waived with reason" not in final_decision:
        errors.append("QA build record final decision must include automated gates.")

    final_evidence_docs = {
        "release_evidence.md": evidence,
        "qa_device_checklist.md": checklist,
        "qa-builds/README.md": qa_builds_readme,
    }
    for filename, text in final_evidence_docs.items():
        if FINAL_RELEASE_EVIDENCE_LOG_COMMAND not in text:
            errors.append(
                f"{filename} must include final release evidence verifier log command: "
                f"`{FINAL_RELEASE_EVIDENCE_LOG_COMMAND}`."
            )
        if QA_RELEASE_EVIDENCE_LINK_COMMAND not in text:
            errors.append(
                f"{filename} must include QA release evidence link update command: "
                f"`{QA_RELEASE_EVIDENCE_LINK_COMMAND}`."
            )

    scoped_command_docs = {
        "qa_device_checklist.md": (checklist, SCOPED_INTERNAL_QA_COMMANDS),
        "qa-builds/README.md": (
            qa_builds_readme,
            QA_BUILDS_README_SCOPED_INTERNAL_QA_COMMANDS,
        ),
    }
    for filename, (text, commands) in scoped_command_docs.items():
        for command in commands:
            if command not in text:
                errors.append(
                    f"{filename} must document scoped internal QA command: `{command}`."
                )
        if _has_android_handoff_with_ios_options(text):
            errors.append(
                f"{filename} must not show Android-only handoff with Apple Team ID options."
            )

    start = evidence.find(qa_release_evidence_links.QA_LINKS_START)
    end = evidence.find(qa_release_evidence_links.QA_LINKS_END)
    if start < 0 or end < 0 or end < start:
        errors.append(
            "release_evidence.md must contain the guarded QA build record link block markers."
        )

    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Verify parity for MaClaw Mobile manual release gates.",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=mobile_root(),
        help="Path to mobile/maclaw_mobile. Defaults to this script's project root.",
    )
    args = parser.parse_args(argv)

    errors = validate_manual_release_gates(args.root.resolve())
    if not errors:
        print("MaClaw Mobile manual release gates verified.")
        return 0

    print("MaClaw Mobile manual release gate violations found:", file=sys.stderr)
    for error in errors:
        print(f"- {error}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
