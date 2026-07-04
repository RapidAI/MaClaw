from __future__ import annotations

import argparse
import plistlib
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import build_android_release
import plan_ios_release
import release_evidence_commands
import validate_qa_build_records_dir
import verify_android_release_signing
import verify_ios_wrapper
import verify_manual_release_gates


SIGNED_QA_RECORD_HINT = release_evidence_commands.signed_qa_record_hint()


@dataclass(frozen=True)
class PreflightCheck:
    name: str
    status: str
    details: list[str]

    @property
    def is_blocker(self) -> bool:
        return self.status == "blocker"


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _ok(name: str, detail: str) -> PreflightCheck:
    return PreflightCheck(name=name, status="ok", details=[detail])


def _info(name: str, detail: str) -> PreflightCheck:
    return PreflightCheck(name=name, status="info", details=[detail])


def _blocker(name: str, details: list[str]) -> PreflightCheck:
    return PreflightCheck(name=name, status="blocker", details=details)


def _scope_label(scope: str) -> str:
    return release_evidence_commands.scope_label(scope)


def validate_ios_export_options(
    root: Path,
    *,
    team_id: str | None = None,
    export_method: str | None = None,
) -> list[str]:
    export_options = root / "ios" / "ExportOptions.plist"
    if not export_options.exists():
        setup_team_id = team_id or "<APPLE_TEAM_ID>"
        setup_export_method = export_method or "development"
        return [
            f"Missing iOS export options plist: {export_options}",
            "Run `"
            + release_evidence_commands.setup_ios_export_options_command(
                team_id=setup_team_id,
                export_method=setup_export_method,
            )
            + "` before iOS signed-build planning.",
        ]
    if not export_options.is_file():
        return [f"iOS export options path must be a file: {export_options}"]
    try:
        with export_options.open("rb") as handle:
            payload = plistlib.load(handle)
    except (plistlib.InvalidFileException, OSError, ValueError) as exc:
        return [f"iOS export options plist is not readable: {export_options}: {exc}"]

    errors: list[str] = []
    actual_team_id = payload.get("teamID")
    if not isinstance(actual_team_id, str) or plan_ios_release.APPLE_TEAM_ID_RE.fullmatch(actual_team_id) is None:
        errors.append("iOS export options teamID must be a 10-character Apple team identifier.")
    actual_method = payload.get("method")
    if actual_method not in plan_ios_release.VALID_EXPORT_METHODS:
        errors.append(
            "iOS export options method must be one of: "
            + ", ".join(plan_ios_release.VALID_EXPORT_METHODS),
        )
    if team_id and actual_team_id != team_id:
        errors.append(
            f"iOS export options teamID must match {team_id}: found {actual_team_id!r}",
        )
    if export_method and actual_method != export_method:
        errors.append(
            f"iOS export options method must match {export_method}: found {actual_method!r}",
        )
    return errors


def run_preflight(
    root: Path,
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
    ios_team_id: str | None = None,
    ios_export_method: str | None = None,
    android_config_validator: Callable[[Path], list[str]] = verify_android_release_signing.verify_android_release_signing,
    android_key_validator: Callable[[Path], tuple[dict[str, str], list[str]]] = build_android_release.validate_key_properties,
    ios_wrapper_validator: Callable[[Path], list[str]] = verify_ios_wrapper.verify_ios_wrapper,
    ios_export_options_validator: Callable[..., list[str]] = validate_ios_export_options,
    records_dir_validator: Callable[
        [Path],
        list[validate_qa_build_records_dir.RecordValidationResult],
    ] = validate_qa_build_records_dir.validate_directory,
    manual_gate_validator: Callable[
        [Path],
        list[str],
    ] = verify_manual_release_gates.validate_manual_release_gates,
) -> list[PreflightCheck]:
    root = root.resolve()
    scope = release_evidence_commands.validate_scope(scope)
    checks: list[PreflightCheck] = []
    signed_record_hint = release_evidence_commands.signed_qa_record_hint(
        scope=scope,
        team_id=ios_team_id or release_evidence_commands.DEFAULT_TEAM_ID,
        export_method=ios_export_method or release_evidence_commands.DEFAULT_EXPORT_METHOD,
    )

    if release_evidence_commands.scope_covers_android(scope):
        android_config_errors = android_config_validator(root)
        checks.append(
            _blocker("Android release signing Gradle guard", android_config_errors)
            if android_config_errors
            else _ok("Android release signing Gradle guard", "release signing config is guarded against debug-key fallback")
        )

        _, android_key_errors = android_key_validator(root)
        if android_key_errors:
            android_key_errors = [
                *android_key_errors,
                "Run `"
                + release_evidence_commands.setup_android_signing_command()
                + "` with MACLAW_ANDROID_STORE_FILE, MACLAW_ANDROID_STORE_PASSWORD, "
                "MACLAW_ANDROID_KEY_ALIAS, and MACLAW_ANDROID_KEY_PASSWORD set "
                "before Android signed-build planning.",
            ]
        checks.append(
            _blocker("Android local signing inputs", android_key_errors)
            if android_key_errors
            else _ok("Android local signing inputs", "android/key.properties and signing store are present")
        )

    if release_evidence_commands.scope_covers_ios(scope):
        ios_errors = ios_wrapper_validator(root)
        checks.append(
            _blocker("iOS wrapper and Share Extension", ios_errors)
            if ios_errors
            else _ok("iOS wrapper and Share Extension", "Runner, Share Extension, URL schemes, and app group wiring are present")
        )

        ios_export_errors = ios_export_options_validator(
            root,
            team_id=ios_team_id,
            export_method=ios_export_method,
        )
        checks.append(
            _blocker("iOS export options", ios_export_errors)
            if ios_export_errors
            else _ok("iOS export options", "ios/ExportOptions.plist is readable and has a valid Team ID/export method")
        )

    try:
        manual_gate_errors = manual_gate_validator(root)
    except (OSError, ValueError) as exc:
        manual_gate_errors = [
            f"Manual release gate documentation cannot be verified: {exc}",
        ]
    checks.append(
        _blocker("Manual release gate documentation", manual_gate_errors)
        if manual_gate_errors
        else _ok(
            "Manual release gate documentation",
            "release audit, QA checklist, QA record template, final evidence log command, "
            "QA record link block, scoped internal QA commands, and QA record "
            "validation/redaction rules are aligned without secret redaction failures",
        )
    )

    has_preflight_blockers = any(check.is_blocker for check in checks)
    records_dir = root / "docs" / "qa-builds"
    if not records_dir.exists():
        checks.append(_blocker("QA build record directory", [f"Missing QA build record directory: {records_dir}"]))
    elif not records_dir.is_dir():
        checks.append(_blocker("QA build record directory", [f"QA build record path is not a directory: {records_dir}"]))
    else:
        results = records_dir_validator(records_dir)
        invalid = [result for result in results if result.errors]
        valid = [result.path for result in results if not result.errors]
        in_scope_valid = [
            path
            for path in valid
            if validate_qa_build_records_dir.record_matches_scope(path, scope)
        ]
        out_of_scope_valid = [
            path for path in valid if path not in in_scope_valid
        ]
        if invalid:
            details: list[str] = []
            for result in invalid:
                details.append(f"{result.path}:")
                details.extend(f"  - {error}" for error in result.errors)
            checks.append(_blocker("Existing QA build records", details))
        elif in_scope_valid:
            detail = f"{len(in_scope_valid)} in-scope completed record(s) already validate"
            if out_of_scope_valid:
                detail += f"; {len(out_of_scope_valid)} out-of-scope record(s) ignored for {_scope_label(scope)} preflight"
            checks.append(_ok("Existing QA build records", detail))
        else:
            if has_preflight_blockers:
                detail = (
                    "signed-build QA record creation is deferred until preflight "
                    "blockers are clear; re-run `"
                    + release_evidence_commands.qa_preflight_command(
                        scope=scope,
                        team_id=ios_team_id
                        or release_evidence_commands.DEFAULT_TEAM_ID
                        if release_evidence_commands.scope_covers_ios(scope)
                        else None,
                        export_method=ios_export_method
                        or release_evidence_commands.DEFAULT_EXPORT_METHOD
                        if release_evidence_commands.scope_covers_ios(scope)
                        else None,
                    )
                    + "` after fixing blockers"
                )
            else:
                detail = signed_record_hint
            if out_of_scope_valid:
                detail = (
                    f"{len(out_of_scope_valid)} out-of-scope completed record(s) already validate; "
                    f"create an in-scope {_scope_label(scope)} QA record. "
                    + detail
                )
            checks.append(
                _info(
                    "Existing QA build records",
                    detail,
                ),
            )

    checks.append(
        _info(
            "Release evidence link step",
            "after QA records validate, "
            + release_evidence_commands.qa_release_evidence_link_hint(scope=scope),
        ),
    )
    return checks


def format_preflight(checks: list[PreflightCheck]) -> str:
    lines = ["MaClaw Mobile QA preflight:"]
    for check in checks:
        lines.append(f"- [{check.status.upper()}] {check.name}")
        lines.extend(f"  - {detail}" for detail in check.details)
    blockers = sum(1 for check in checks if check.is_blocker)
    lines.append("")
    if blockers:
        lines.append(f"Result: BLOCKED ({blockers} blocker check(s)).")
    else:
        lines.append("Result: READY for signed-build QA preparation.")
    return "\n".join(lines).rstrip() + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Run local MaClaw Mobile preflight checks before signed-build QA.",
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
        help="Platform scope for local signed-build QA preflight.",
    )
    parser.add_argument(
        "--team-id",
        type=plan_ios_release.validate_team_id,
        help="Optional Apple Team ID to verify against ios/ExportOptions.plist.",
    )
    parser.add_argument(
        "--export-method",
        choices=plan_ios_release.VALID_EXPORT_METHODS,
        help="Optional Xcode export method to verify against ios/ExportOptions.plist.",
    )
    args = parser.parse_args(argv)

    checks = run_preflight(
        args.root,
        scope=args.scope,
        ios_team_id=args.team_id,
        ios_export_method=args.export_method,
    )
    output = format_preflight(checks)
    if any(check.is_blocker for check in checks):
        print(output, end="", file=sys.stderr)
        return 1
    print(output, end="")
    return 0


if __name__ == "__main__":
    sys.exit(main())
