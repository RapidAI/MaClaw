from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import qa_preflight
import plan_ios_release
import release_evidence_commands
import validate_qa_build_records_dir
import verify_final_release_evidence


@dataclass(frozen=True)
class ReleaseStatus:
    root: Path
    preflight_checks: list[qa_preflight.PreflightCheck]
    record_results: list[validate_qa_build_records_dir.RecordValidationResult]
    final_errors: list[str]
    scope: str = release_evidence_commands.DEFAULT_SCOPE
    ios_team_id: str | None = None
    ios_export_method: str | None = None
    records_dir: Path | None = None

    @property
    def ready(self) -> bool:
        blocking_record_errors = any(
            result.errors
            and not validate_qa_build_records_dir.record_is_known_out_of_scope(
                result.path,
                self.scope,
            )
            for result in self.record_results
        )
        return (
            not any(check.is_blocker for check in self.preflight_checks)
            and not blocking_record_errors
            and not self.final_errors
        )


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def build_status(
    root: Path,
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
    ios_team_id: str | None = None,
    ios_export_method: str | None = None,
    records_dir: Path | None = None,
    preflight: Callable[..., list[qa_preflight.PreflightCheck]] = qa_preflight.run_preflight,
    validate_records: Callable[
        [Path],
        list[validate_qa_build_records_dir.RecordValidationResult],
    ] = validate_qa_build_records_dir.validate_directory,
    verify_final: Callable[..., list[str]] = verify_final_release_evidence.verify_final_release_evidence,
) -> ReleaseStatus:
    root = root.resolve()
    scope = release_evidence_commands.validate_scope(scope)
    resolved_records_dir = (
        records_dir if records_dir is not None else root / "docs" / "qa-builds"
    )
    resolved_records_dir = (
        resolved_records_dir
        if resolved_records_dir.is_absolute()
        else root / resolved_records_dir
    ).resolve()
    return ReleaseStatus(
        root=root,
        preflight_checks=preflight(
            root,
            scope=scope,
            ios_team_id=ios_team_id,
            ios_export_method=ios_export_method,
            records_dir=resolved_records_dir,
        ),
        record_results=validate_records(resolved_records_dir),
        final_errors=verify_final(
            resolved_records_dir,
            release_evidence_path=root / "docs" / "release_evidence.md",
            scope=scope,
        ),
        scope=scope,
        ios_team_id=ios_team_id,
        ios_export_method=ios_export_method,
        records_dir=resolved_records_dir,
    )


def _format_list(prefix: str, items: list[str]) -> list[str]:
    if not items:
        return [f"{prefix} none"]
    return [f"{prefix} {item}" for item in items]


def _scope_label(scope: str) -> str:
    return release_evidence_commands.scope_label(scope)


def _ready_result_line(scope: str) -> str:
    if scope == release_evidence_commands.DEFAULT_SCOPE:
        return "Result: READY for final release approval."
    return (
        "Result: READY for "
        + _scope_label(scope)
        + " scoped internal QA approval, not full Android/iOS release approval."
    )


def _preflight_command(status: ReleaseStatus) -> str:
    team_id = (
        status.ios_team_id or release_evidence_commands.DEFAULT_TEAM_ID
        if release_evidence_commands.scope_covers_ios(status.scope)
        else None
    )
    export_method = (
        status.ios_export_method or release_evidence_commands.DEFAULT_EXPORT_METHOD
        if release_evidence_commands.scope_covers_ios(status.scope)
        else None
    )
    return release_evidence_commands.qa_preflight_command(
        scope=status.scope,
        team_id=team_id,
        export_method=export_method,
        records_dir=_records_dir_label(status),
    )


def _records_dir(status: ReleaseStatus) -> Path:
    return status.records_dir or status.root / "docs" / "qa-builds"


def _records_dir_label(status: ReleaseStatus) -> str:
    records_dir = _records_dir(status).resolve()
    default_records_dir = (status.root / "docs" / "qa-builds").resolve()
    if records_dir == default_records_dir:
        return release_evidence_commands.DEFAULT_QA_RECORDS_DIR
    return str(records_dir)


def format_status(status: ReleaseStatus) -> str:
    records_dir_label = _records_dir_label(status)
    lines = [
        f"MaClaw Mobile release status: {status.root}",
        "",
        "Preflight:",
    ]
    for check in status.preflight_checks:
        lines.append(f"- [{check.status.upper()}] {check.name}")
        lines.extend(f"  - {detail}" for detail in check.details)

    valid_records = [result.path for result in status.record_results if not result.errors]
    in_scope_valid_records = [
        path
        for path in valid_records
        if validate_qa_build_records_dir.record_matches_scope(path, status.scope)
    ]
    out_of_scope_valid_records = [
        path for path in valid_records if path not in in_scope_valid_records
    ]
    invalid_records = [result for result in status.record_results if result.errors]
    blocking_invalid_records = [
        result
        for result in invalid_records
        if not validate_qa_build_records_dir.record_is_known_out_of_scope(
            result.path,
            status.scope,
        )
    ]
    out_of_scope_invalid_records = [
        result
        for result in invalid_records
        if result not in blocking_invalid_records
    ]
    lines.extend(
        [
            "",
            "QA build records: "
            f"{len(in_scope_valid_records)} in-scope valid, "
            f"{len(out_of_scope_valid_records)} out-of-scope valid, "
            f"{len(blocking_invalid_records)} blocking invalid, "
            f"{len(out_of_scope_invalid_records)} out-of-scope invalid",
        ],
    )
    if not valid_records and not invalid_records:
        lines.append("- No completed signed-build QA records found.")
    if valid_records:
        lines.extend(f"- [VALID] {path.name}" for path in in_scope_valid_records)
        lines.extend(
            f"- [OUT-OF-SCOPE] {path.name}" for path in out_of_scope_valid_records
        )
    for result in blocking_invalid_records:
        lines.append(f"- [INVALID] {result.path}")
        lines.extend(f"  - {error}" for error in result.errors)
    for result in out_of_scope_invalid_records:
        lines.append(f"- [OUT-OF-SCOPE INVALID] {result.path.name}")
        lines.extend(f"  - {error}" for error in result.errors)

    lines.append("")
    lines.append("Final release evidence:")
    if status.final_errors:
        lines.extend(_format_list("- [BLOCKER]", status.final_errors))
    else:
        lines.append("- [OK] Final release evidence verified.")

    lines.append("")
    if status.ready:
        lines.append(_ready_result_line(status.scope))
    else:
        preflight_blockers = [
            check for check in status.preflight_checks if check.is_blocker
        ]
        lines.append("Result: NOT READY.")
        lines.append("Next actions:")
        if preflight_blockers:
            lines.append(f"- Clear preflight blockers, then build signed {_scope_label(status.scope)} QA packages.")
            for check in preflight_blockers:
                lines.append(f"- [Preflight] {check.name}")
                lines.extend(f"  - {detail}" for detail in check.details)
            lines.append(
                f"- Re-run `{_preflight_command(status)}` after fixing the blockers.",
            )
        if blocking_invalid_records:
            lines.append(
                f"- Fix invalid signed-build QA records under {records_dir_label}.",
            )
            lines.append(
                "- "
                + release_evidence_commands.qa_build_record_report_hint(
                    str(blocking_invalid_records[0].path),
                ),
            )
        elif not in_scope_valid_records and not preflight_blockers:
            lines.append(
                f"- Create and validate in-scope signed-build QA records under {records_dir_label}/.",
            )
            lines.append(
                "- "
                + release_evidence_commands.signed_qa_record_hint(
                    scope=status.scope,
                    team_id=status.ios_team_id
                    or release_evidence_commands.DEFAULT_TEAM_ID,
                    export_method=status.ios_export_method
                    or release_evidence_commands.DEFAULT_EXPORT_METHOD,
                    records_dir=records_dir_label,
                ),
            )
        if in_scope_valid_records and status.final_errors:
            lines.append("- Resolve final evidence blockers above:")
            final_record_versions = sorted(
                version
                for version in {
                    verify_final_release_evidence._version_for_record(path)
                    for path in in_scope_valid_records
                }
                if version is not None
            )
            for hint in verify_final_release_evidence.next_action_hints(
                status.final_errors,
                scope=status.scope,
                records_dir=records_dir_label,
                existing_versions=final_record_versions,
            ):
                lines.append(f"  - {hint}")
        elif status.final_errors:
            lines.append("- Complete final release evidence verification after QA records exist.")
    return "\n".join(lines).rstrip() + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Print a MaClaw Mobile release readiness status report.",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=mobile_root(),
        help="Path to mobile/maclaw_mobile. Defaults to this script project root.",
    )
    parser.add_argument(
        "--scope",
        choices=release_evidence_commands.VALID_SCOPES,
        default=release_evidence_commands.DEFAULT_SCOPE,
        help="Platform scope for release readiness status.",
    )
    parser.add_argument(
        "--team-id",
        type=qa_preflight.validate_team_id_or_placeholder,
        help="Optional Apple Team ID to verify against ios/ExportOptions.plist.",
    )
    parser.add_argument(
        "--export-method",
        choices=plan_ios_release.VALID_EXPORT_METHODS,
        help="Optional Xcode export method to verify against ios/ExportOptions.plist.",
    )
    parser.add_argument(
        "--records-dir",
        type=Path,
        help="QA build records directory. Defaults to docs/qa-builds under root.",
    )
    args = parser.parse_args(argv)

    status = build_status(
        args.root,
        scope=args.scope,
        ios_team_id=args.team_id,
        ios_export_method=args.export_method,
        records_dir=args.records_dir,
    )
    output = format_status(status)
    if status.ready:
        print(output, end="")
        return 0
    print(output, end="", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
