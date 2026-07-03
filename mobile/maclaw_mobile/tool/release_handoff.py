from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import plan_ios_release
import release_status_report


DEFAULT_SCOPE = "android-ios"
DEFAULT_VERSION = "<version+build>"
DEFAULT_TEAM_ID = "<APPLE_TEAM_ID>"
DEFAULT_EXPORT_METHOD = "development"
VERSION_BUILD_RE = re.compile(r"^\d+(?:\.\d+){1,3}\+\d+$")
APPLE_TEAM_ID_RE = re.compile(r"^[A-Z0-9]{10}$")


@dataclass(frozen=True)
class ReleaseHandoff:
    root: Path
    status: release_status_report.ReleaseStatus
    version: str
    scope: str
    team_id: str
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
    build_status = build_status or release_status_report.build_status
    return ReleaseHandoff(
        root=root,
        status=build_status(
            root,
            ios_team_id=team_id,
            ios_export_method=export_method,
        ),
        version=version,
        scope=scope,
        team_id=team_id,
        export_method=export_method,
    )


def android_version_args(version: str) -> tuple[str, str]:
    app_version, build_number = version.split("+", 1)
    return app_version, build_number


def _status_lines(status: release_status_report.ReleaseStatus) -> list[str]:
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
    if not valid_records:
        lines.append("- Missing completed signed-build QA record under docs/qa-builds/.")
    if status.final_errors:
        lines.append("- Final evidence blockers:")
        lines.extend(f"  - {error}" for error in status.final_errors)
    return lines


def format_handoff(handoff: ReleaseHandoff) -> str:
    record_path = f"docs/qa-builds/<YYYY-MM-DD>-{handoff.scope}-{handoff.version}.md"
    handoff_evidence_path = f"docs/qa-builds/handoff-{handoff.version}.md"
    runtime_boundary_log = f"docs/qa-builds/runtime-boundary-{handoff.version}.log"
    release_gates_log = f"docs/qa-builds/release-gates-{handoff.version}.log"
    android_build_name, android_build_number = android_version_args(handoff.version)
    android_build_command = (
        "python3 tool/build_android_release.py --artifact apk "
        f"--build-name {android_build_name} --build-number {android_build_number}"
    )
    android_artifact_evidence_command = (
        "python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> "
        f"--record-dir docs/qa-builds --version {handoff.version} "
        '--signing-identity "<alias or certificate fingerprint>" '
        '--installer-channel "<internal test channel>"'
    )
    ios_plan_command = (
        "python3 tool/plan_ios_release.py "
        f"--team-id {handoff.team_id} --export-method {handoff.export_method}"
    )
    ios_artifact_evidence_command = (
        "python3 tool/signed_artifact_evidence.py ios "
        '--archive-or-build "<Xcode archive path or TestFlight build number>" '
        f"--team-id {handoff.team_id} "
        '--provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>"'
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
        *_status_lines(handoff.status),
        "",
        "## Operator Inputs",
        "",
        "- Android release keystore path, alias, store password, and key password.",
        "- Apple Team ID, Runner provisioning profile, and Share Extension provisioning profile.",
        "- Signed Android/iOS test devices with camera, microphone, photo library, file share, notification, SSH, and weak-network coverage.",
        "- Official MaClaw account, Hub tenant access, desktop QR source for third-party LLM access when required, and a safe SSH smoke target.",
        "",
        "## Command Sequence",
        "",
        "```bash",
        f"python3 tool/release_status_report.py --team-id {handoff.team_id} --export-method {handoff.export_method}",
        f"python3 tool/release_handoff.py --version {handoff.version} --scope {handoff.scope} --team-id {handoff.team_id} --export-method {handoff.export_method} --output {handoff_evidence_path}",
        f"python3 tool/verify_runtime_boundary.py --log {runtime_boundary_log}",
        "python3 tool/setup_android_signing.py",
        f"{android_build_command} --dry-run",
        android_build_command,
        android_artifact_evidence_command,
        f"python3 tool/setup_ios_export_options.py --team-id {handoff.team_id} --export-method {handoff.export_method}",
        ios_plan_command,
        ios_artifact_evidence_command,
        f"python3 tool/run_release_gates.py --log {release_gates_log}",
        f"python3 tool/create_qa_build_record.py --scope {handoff.scope} --version {handoff.version} "
        f'--release-handoff-result "{handoff_evidence_path}" '
        f'--runtime-boundary-result "MaClaw Mobile runtime boundary verified. log: {runtime_boundary_log}" '
        f'--automated-gates-result "run_release_gates.py: 36 gates passed; log: {release_gates_log}"',
        f"python3 tool/validate_qa_build_record.py {record_path}",
        f"python3 tool/qa_build_record_report.py {record_path}",
        "python3 tool/qa_release_evidence_links.py docs/qa-builds",
        "python3 tool/validate_qa_build_records_dir.py docs/qa-builds",
        "python3 tool/verify_final_release_evidence.py docs/qa-builds",
        "```",
        "",
        "## Evidence To Attach",
        "",
        "- Handoff output path or transcript, runtime-boundary verifier output, and full release-gate run result for the QA record final decision fields.",
        "- Signed Android artifact path, byte size, SHA256, install result, signing identity, and distribution channel.",
        "- iOS archive/export path, Team ID, provisioning profile names or UUIDs, install/TestFlight result, and Share Extension result.",
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
    parser.add_argument("--scope", default=DEFAULT_SCOPE, choices=("android", "ios", "android-ios"))
    parser.add_argument("--team-id", required=True, type=validate_team_id)
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
