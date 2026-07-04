from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import plan_ios_release
import release_evidence_commands
import release_status_report
import verify_final_release_evidence


DEFAULT_SCOPE = release_evidence_commands.DEFAULT_SCOPE
DEFAULT_VERSION = release_evidence_commands.DEFAULT_VERSION
DEFAULT_TEAM_ID = release_evidence_commands.DEFAULT_TEAM_ID
DEFAULT_EXPORT_METHOD = "development"
VERSION_BUILD_RE = re.compile(r"^\d+(?:\.\d+){1,3}\+\d+$")
APPLE_TEAM_ID_RE = re.compile(r"^[A-Z0-9]{10}$")


@dataclass(frozen=True)
class ReleaseHandoff:
    root: Path
    status: release_status_report.ReleaseStatus
    version: str
    scope: str
    team_id: str | None
    export_method: str


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def validate_version_build(value: str) -> str:
    normalized = value.strip()
    if VERSION_BUILD_RE.fullmatch(normalized) is None:
        raise argparse.ArgumentTypeError(
            "version must use app-version+build, for example 1.0.0+42",
        )
    return normalized


def validate_team_id(value: str) -> str:
    normalized = value.strip().upper()
    if APPLE_TEAM_ID_RE.fullmatch(normalized) is None:
        raise argparse.ArgumentTypeError(
            "team id must be a 10-character Apple team identifier",
        )
    return normalized


def build_handoff(
    root: Path,
    *,
    version: str = DEFAULT_VERSION,
    scope: str = DEFAULT_SCOPE,
    team_id: str = DEFAULT_TEAM_ID,
    export_method: str = DEFAULT_EXPORT_METHOD,
    build_status: Callable[..., release_status_report.ReleaseStatus] | None = None,
) -> ReleaseHandoff:
    root = root.resolve()
    scope = release_evidence_commands.validate_scope(scope)
    build_status = build_status or release_status_report.build_status
    return ReleaseHandoff(
        root=root,
        status=build_status(
            root,
            scope=scope,
            ios_team_id=team_id,
            ios_export_method=export_method,
        ),
        version=version,
        scope=scope,
        team_id=team_id,
        export_method=export_method,
    )


def final_decision_prefills(handoff: ReleaseHandoff) -> dict[str, str]:
    return release_evidence_commands.final_decision_prefills(
        handoff.version,
        scope=handoff.scope,
    )


def _status_lines(
    status: release_status_report.ReleaseStatus,
    *,
    version: str = DEFAULT_VERSION,
    team_id: str = DEFAULT_TEAM_ID,
    export_method: str = DEFAULT_EXPORT_METHOD,
) -> list[str]:
    blocker_checks = [check for check in status.preflight_checks if check.is_blocker]
    invalid_records = [result for result in status.record_results if result.errors]
    valid_records = [result for result in status.record_results if not result.errors]

    lines: list[str] = []
    if status.ready:
        return ["- Current status: READY for final release approval."]

    lines.append("- Current status: NOT READY.")
    if blocker_checks:
        lines.append("- Preflight blockers:")
        for check in blocker_checks:
            lines.append(f"  - {check.name}")
            lines.extend(f"    - {detail}" for detail in check.details)
    if invalid_records:
        lines.append("- Invalid QA records:")
        for result in invalid_records:
            lines.append(f"  - {result.path.name}")
            lines.extend(f"    - {error}" for error in result.errors)
        lines.append(
            "  - "
            + release_evidence_commands.qa_build_record_report_hint(
                str(invalid_records[0].path),
            ),
        )
    if not valid_records:
        lines.append("- Missing completed signed-build QA record under docs/qa-builds/.")
        lines.append(
            "  - "
            + release_evidence_commands.signed_qa_record_hint(
                scope=status.scope,
                version=version,
                team_id=team_id,
                export_method=export_method,
            ),
        )
    if status.final_errors:
        lines.append("- Final evidence blockers:")
        lines.extend(f"  - {error}" for error in status.final_errors)
        if valid_records:
            lines.append("- Final evidence next actions:")
            lines.extend(
                f"  - {hint}"
                for hint in verify_final_release_evidence.next_action_hints(
                    status.final_errors,
                    scope=status.scope,
                )
            )
    return lines


def format_handoff(handoff: ReleaseHandoff) -> str:
    record_path = release_evidence_commands.qa_record_path_placeholder(
        scope=handoff.scope,
        version=handoff.version,
    )
    handoff_evidence_path = release_evidence_commands.handoff_evidence_path(
        handoff.version,
        scope=handoff.scope,
    )
    runtime_boundary_command = release_evidence_commands.runtime_boundary_command(
        handoff.version,
    )
    release_gates_command = release_evidence_commands.release_gates_command(
        handoff.version,
    )
    ios_team_id = handoff.team_id or release_evidence_commands.DEFAULT_TEAM_ID
    setup_android_signing_command = (
        release_evidence_commands.setup_android_signing_command()
    )
    setup_ios_export_options_command = (
        release_evidence_commands.setup_ios_export_options_command(
            team_id=ios_team_id,
            export_method=handoff.export_method,
        )
    )
    create_record_command = release_evidence_commands.create_record_command(
        scope=handoff.scope,
        version=handoff.version,
    )
    validate_record_command = release_evidence_commands.validate_qa_build_record_command(
        record_path,
    )
    qa_record_report_command = release_evidence_commands.qa_build_record_report_command(
        record_path,
    )
    qa_release_evidence_link_command = (
        release_evidence_commands.qa_release_evidence_link_command(
            scope=handoff.scope,
        )
    )
    validate_records_dir_command = (
        release_evidence_commands.validate_qa_build_records_dir_command(
            scope=handoff.scope,
        )
    )
    verify_final_evidence_command = (
        release_evidence_commands.verify_final_release_evidence_command(
            scope=handoff.scope,
            version=handoff.version,
        )
    )
    release_status_report_command = (
        release_evidence_commands.release_status_report_command(
            scope=handoff.scope,
            team_id=ios_team_id,
            export_method=handoff.export_method,
        )
    )
    release_handoff_command = release_evidence_commands.release_handoff_command(
        version=handoff.version,
        scope=handoff.scope,
        team_id=ios_team_id,
        export_method=handoff.export_method,
        output=handoff_evidence_path,
    )
    qa_preflight_command = release_evidence_commands.qa_preflight_command(
        scope=handoff.scope,
        team_id=handoff.team_id if release_evidence_commands.scope_covers_ios(handoff.scope) else None,
        export_method=handoff.export_method if release_evidence_commands.scope_covers_ios(handoff.scope) else None,
    )
    android_build_dry_run_command = release_evidence_commands.android_release_build_command(
        handoff.version,
        dry_run=True,
    )
    android_build_command = release_evidence_commands.android_release_build_command(
        handoff.version,
    )
    android_appbundle_build_command = release_evidence_commands.android_release_build_command(
        handoff.version,
        artifact="appbundle",
    )
    android_artifact_evidence_command = (
        release_evidence_commands.android_artifact_evidence_command(handoff.version)
    )
    ios_plan_command = release_evidence_commands.ios_release_plan_command(
        team_id=ios_team_id,
        export_method=handoff.export_method,
    )
    ios_artifact_evidence_command = (
        release_evidence_commands.ios_artifact_evidence_command(
            team_id=ios_team_id,
        )
    )
    platform_setup_commands: list[str] = []
    platform_artifact_commands: list[str] = []
    platform_inputs: list[str] = []
    device_targets: list[str] = []
    platform_evidence: list[str] = []
    if release_evidence_commands.scope_covers_android(handoff.scope):
        platform_inputs.append("- Android release keystore path, alias, store password, and key password.")
        device_targets.append("Android")
        platform_setup_commands.append(setup_android_signing_command)
        platform_artifact_commands.extend(
            [
                android_build_dry_run_command,
                android_build_command,
                android_appbundle_build_command,
                android_artifact_evidence_command,
            ],
        )
        platform_evidence.append(
            "- Signed Android artifact path, byte size, SHA256, install result, signing identity, and distribution channel.",
        )
    if release_evidence_commands.scope_covers_ios(handoff.scope):
        platform_inputs.append("- Apple Team ID, Runner provisioning profile, and Share Extension provisioning profile.")
        device_targets.append("iOS")
        platform_setup_commands.append(setup_ios_export_options_command)
        platform_artifact_commands.extend(
            [
                ios_plan_command,
                ios_artifact_evidence_command,
            ],
        )
        platform_evidence.append(
            "- iOS archive/export path, Team ID, provisioning profile names or UUIDs, install/TestFlight result, and Share Extension result.",
        )

    lines = [
        "# MaClaw Mobile Release Handoff",
        "",
        f"Root: `{handoff.root}`",
        f"Scope: `{handoff.scope}`",
        f"Version: `{handoff.version}`",
        "",
        "## Current Status",
        "",
        *_status_lines(
            handoff.status,
            version=handoff.version,
            team_id=ios_team_id,
            export_method=handoff.export_method,
        ),
        "",
        "## Operator Inputs",
        "",
        *platform_inputs,
        "- Signed "
        + "/".join(device_targets)
        + " test devices with camera, microphone, photo library, file share, notification, SSH, and weak-network coverage.",
        "- Official MaClaw account, Hub tenant access, desktop QR source for third-party LLM access when required, and a safe SSH smoke target.",
        "",
        "## Command Sequence",
        "",
        "```bash",
        release_status_report_command,
        release_handoff_command,
        *platform_setup_commands,
        qa_preflight_command,
        runtime_boundary_command,
        *platform_artifact_commands,
        release_gates_command,
        create_record_command,
        validate_record_command,
        qa_record_report_command,
        qa_release_evidence_link_command,
        validate_records_dir_command,
        verify_final_evidence_command,
        "```",
        "",
        "## Evidence To Attach",
        "",
        "- Handoff output path or transcript, runtime-boundary verifier output, and full release-gate run result for the QA record final decision fields.",
        *platform_evidence,
        "- Runtime boundary verifier result proving mobile does not embed or bridge Go corelib.",
        "- HubCenter discovery result, discovered Hub URL, tenant, LLM access mode, and mobile bootstrap result.",
        "- Assistant search with citations, voice query, image/screenshot query, and share-to-app payload results.",
        "- Document import, AI transform, export/share, and notification evidence.",
        "- SSH login, read-only command output, reconnect, copied log, AI analysis, and credential deletion evidence.",
        "- Digital employee list, remote target invocation, completion/failure result, and notification evidence.",
        "- Weak-network/offline recovery evidence with timestamps.",
        "",
        "## Notes",
        "",
        "- Do not store signing secrets, SSH passwords, private keys, access tokens, or private customer content in QA records.",
        "- Use redacted screenshots, artifact hashes, task IDs, and attachment IDs for traceable evidence.",
        "- Completed QA records must pass `python3 tool/validate_qa_build_record.py` without secret redaction failures; use `python3 tool/qa_build_record_report.py` to fix gaps before linking them.",
        "- Link only validated QA records from docs/release_evidence.md.",
    ]
    return "\n".join(lines).rstrip() + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Generate a MaClaw Mobile signed-build QA/release handoff plan.",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=mobile_root(),
        help="Path to mobile/maclaw_mobile. Defaults to this script project root.",
    )
    parser.add_argument(
        "--version",
        required=True,
        type=validate_version_build,
        help="Version+build label, e.g. 1.0.0+42.",
    )
    parser.add_argument("--scope", default=DEFAULT_SCOPE, choices=release_evidence_commands.VALID_SCOPES)
    parser.add_argument("--team-id", type=validate_team_id)
    parser.add_argument(
        "--export-method",
        default=DEFAULT_EXPORT_METHOD,
        choices=plan_ios_release.VALID_EXPORT_METHODS,
    )
    parser.add_argument("--output", type=Path, help="Optional Markdown output path.")
    parser.add_argument(
        "--force",
        action="store_true",
        help="Overwrite an existing handoff output file.",
    )
    args = parser.parse_args(argv)
    if release_evidence_commands.scope_covers_ios(args.scope) and not args.team_id:
        parser.error("--team-id is required for iOS release handoff scopes")

    handoff = build_handoff(
        args.root,
        version=args.version,
        scope=args.scope,
        team_id=args.team_id,
        export_method=args.export_method,
    )
    output = format_handoff(handoff)
    if args.output:
        if args.output.exists() and not args.force:
            print(
                f"Release handoff output already exists: {args.output}; pass --force to overwrite.",
                file=sys.stderr,
            )
            return 1
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(output, encoding="utf-8")
        print(f"Wrote MaClaw Mobile release handoff: {args.output}")
    else:
        print(output, end="")
    return 0 if handoff.status.ready else 1


if __name__ == "__main__":
    sys.exit(main())
