from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import build_android_release
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


def validate_ios_export_options(root: Path) -> list[str]:
    export_options = root / "ios" / "ExportOptions.plist"
    if not export_options.exists():
        return [
            f"Missing iOS export options plist: {export_options}",
            "Run `python3 tool/setup_ios_export_options.py --team-id <APPLE_TEAM_ID> --export-method development` before iOS signed-build planning.",
        ]
    if not export_options.is_file():
        return [f"iOS export options path must be a file: {export_options}"]
    return []


def run_preflight(
    root: Path,
    android_config_validator: Callable[[Path], list[str]] = verify_android_release_signing.verify_android_release_signing,
    android_key_validator: Callable[[Path], tuple[dict[str, str], list[str]]] = build_android_release.validate_key_properties,
    ios_wrapper_validator: Callable[[Path], list[str]] = verify_ios_wrapper.verify_ios_wrapper,
    ios_export_options_validator: Callable[[Path], list[str]] = validate_ios_export_options,
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

    ios_export_errors = ios_export_options_validator(root)
    checks.append(
        _blocker("iOS export options", ios_export_errors)
        if ios_export_errors
        else _ok("iOS export options", "ios/ExportOptions.plist is present for Xcode export")
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
            checks.append(_info("Existing QA build records", "no completed signed-build QA records yet; create one with tool/create_qa_build_record.py"))

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
    args = parser.parse_args(argv)

    checks = run_preflight(args.root)
    output = format_preflight(checks)
    if any(check.is_blocker for check in checks):
        print(output, end="", file=sys.stderr)
        return 1
    print(output, end="")
    return 0


if __name__ == "__main__":
    sys.exit(main())
