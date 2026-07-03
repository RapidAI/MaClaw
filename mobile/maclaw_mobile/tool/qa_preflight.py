from __future__ import annotations

import argparse
import plistlib
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import build_android_release
import plan_ios_release
import validate_qa_build_records_dir
import verify_android_release_signing
import verify_ios_wrapper


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


def validate_ios_export_options(
    root: Path,
    *,
    team_id: str | None = None,
    export_method: str | None = None,
) -> list[str]:
    export_options = root / "ios" / "ExportOptions.plist"
    if not export_options.exists():
        return [
            f"Missing iOS export options plist: {export_options}",
            "Run `python3 tool/setup_ios_export_options.py --team-id <APPLE_TEAM_ID> --export-method development` before iOS signed-build planning.",
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
) -> list[PreflightCheck]:
    root = root.resolve()
    checks: list[PreflightCheck] = []

    android_config_errors = android_config_validator(root)
    checks.append(
        _blocker("Android release signing Gradle guard", android_config_errors)
        if android_config_errors
        else _ok("Android release signing Gradle guard", "release signing config is guarded against debug-key fallback")
    )

    _, android_key_errors = android_key_validator(root)
    checks.append(
        _blocker("Android local signing inputs", android_key_errors)
        if android_key_errors
        else _ok("Android local signing inputs", "android/key.properties and signing store are present")
    )

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

    records_dir = root / "docs" / "qa-builds"
    if not records_dir.exists():
        checks.append(_blocker("QA build record directory", [f"Missing QA build record directory: {records_dir}"]))
    elif not records_dir.is_dir():
        checks.append(_blocker("QA build record directory", [f"QA build record path is not a directory: {records_dir}"]))
    else:
        results = records_dir_validator(records_dir)
        invalid = [result for result in results if result.errors]
        if invalid:
            details: list[str] = []
            for result in invalid:
                details.append(f"{result.path}:")
                details.extend(f"  - {error}" for error in result.errors)
            checks.append(_blocker("Existing QA build records", details))
        elif results:
            checks.append(_ok("Existing QA build records", f"{len(results)} completed record(s) already validate"))
        else:
            checks.append(
                _info(
                    "Existing QA build records",
                    "no completed signed-build QA records yet; run tool/release_handoff.py --version <version+build> --team-id <APPLE_TEAM_ID> --export-method <export-method> --output docs/qa-builds/handoff-<version+build>.md and follow its prefilled create_qa_build_record.py command after capturing verify_runtime_boundary.py --log and run_release_gates.py --log evidence",
                ),
            )

    checks.append(
        _info(
            "Release evidence link step",
            "after QA records validate, run tool/qa_release_evidence_links.py docs/qa-builds and paste the generated links into docs/release_evidence.md",
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
