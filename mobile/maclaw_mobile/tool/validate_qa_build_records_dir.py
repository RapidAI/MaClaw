from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import validate_qa_build_record
import release_evidence_commands


@dataclass(frozen=True)
class RecordValidationResult:
    path: Path
    errors: list[str]


def default_records_dir() -> Path:
    return Path(__file__).resolve().parents[1] / "docs/qa-builds"


def _is_evidence_attachment(path: Path) -> bool:
    name = path.name.lower()
    return name == "readme.md" or (
        name.startswith("handoff-") and name.endswith(".md")
    )


def completed_record_paths(records_dir: Path) -> list[Path]:
    if not records_dir.exists() or not records_dir.is_dir():
        return []
    return sorted(
        path
        for path in records_dir.iterdir()
        if path.is_file()
        and path.suffix.lower() == ".md"
        and not _is_evidence_attachment(path)
    )


def validate_directory(
    records_dir: Path,
    validate_file: Callable[[Path], list[str]] = validate_qa_build_record.validate_file,
) -> list[RecordValidationResult]:
    return [
        RecordValidationResult(path=path, errors=validate_file(path))
        for path in completed_record_paths(records_dir)
    ]


def _scope_for_records(paths: list[Path]) -> str:
    scopes = set()
    for path in paths:
        match = validate_qa_build_record.QA_BUILD_RECORD_FILENAME_RE.fullmatch(path.name)
        if match is not None:
            scopes.add(match.group("scope"))
    if scopes == {"android"}:
        return "android"
    if scopes == {"ios"}:
        return "ios"
    return release_evidence_commands.DEFAULT_SCOPE


def scope_for_record(path: Path) -> str | None:
    match = validate_qa_build_record.QA_BUILD_RECORD_FILENAME_RE.fullmatch(path.name)
    if match is None:
        return None
    return match.group("scope")


def record_matches_scope(path: Path, required_scope: str) -> bool:
    record_scope = scope_for_record(path)
    return (
        release_evidence_commands.scope_covers_android(required_scope)
        and release_evidence_commands.scope_covers_android(record_scope)
    ) or (
        release_evidence_commands.scope_covers_ios(required_scope)
        and release_evidence_commands.scope_covers_ios(record_scope)
    )


def record_is_known_out_of_scope(path: Path, required_scope: str) -> bool:
    return scope_for_record(path) is not None and not record_matches_scope(
        path,
        required_scope,
    )


_scope_for_record = scope_for_record
_record_matches_scope = record_matches_scope
_record_is_known_out_of_scope = record_is_known_out_of_scope


def _single_version_for_records(paths: list[Path]) -> str | None:
    versions = set()
    for path in paths:
        match = validate_qa_build_record.QA_BUILD_RECORD_FILENAME_RE.fullmatch(path.name)
        if match is not None:
            versions.add(match.group("version"))
    if len(versions) == 1:
        return next(iter(versions))
    return None


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Validate every completed MaClaw Mobile QA build record in a directory.",
    )
    parser.add_argument(
        "records_dir",
        nargs="?",
        type=Path,
        default=default_records_dir(),
        help="Directory containing completed QA build record markdown files.",
    )
    parser.add_argument(
        "--scope",
        choices=release_evidence_commands.VALID_SCOPES,
        help=(
            "Optional platform scope for the next final evidence command. "
            "When omitted, the scope is inferred from valid record filenames."
        ),
    )
    args = parser.parse_args(argv)

    records_dir = args.records_dir.resolve()
    if not records_dir.exists():
        print(f"QA build record directory does not exist: {records_dir}")
        return 1
    if not records_dir.is_dir():
        print(f"QA build record path is not a directory: {records_dir}")
        return 1

    results = validate_directory(records_dir)
    failed = [result for result in results if result.errors]
    if args.scope:
        blocking_failed = [
            result
            for result in failed
            if not record_is_known_out_of_scope(result.path, args.scope)
        ]
        out_of_scope_failed = [
            result for result in failed if result not in blocking_failed
        ]
    else:
        blocking_failed = failed
        out_of_scope_failed = []
    if blocking_failed:
        print("QA build record directory validation failed:")
        for result in blocking_failed:
            print(f"- {result.path}:")
            for error in result.errors:
                print(f"  - {error}")
        print("Next action:")
        print(
            "- Run `python3 tool/qa_build_record_report.py <failed-record>` "
            "for grouped gaps, redaction remediation, and signed artifact hints.",
        )
        return 1

    valid_records = [result.path for result in results if not result.errors]
    scope = args.scope or _scope_for_records(valid_records)
    print(f"QA build record directory validation passed: {len(results)} record(s).")
    if out_of_scope_failed:
        print(
            "Out-of-scope invalid records ignored for "
            f"{release_evidence_commands.scope_label(scope)} scope:",
        )
        for result in out_of_scope_failed:
            print(f"- {result.path.name}")
            for error in result.errors:
                print(f"  - {error}")
    if not results:
        print(
            "No completed signed-build QA records found; this directory check "
            "does not prove final release readiness.",
        )
        print("Next action:")
        print("- " + release_evidence_commands.signed_qa_record_hint(scope=scope))
    in_scope_records = [
        path for path in valid_records if record_matches_scope(path, scope)
    ]
    out_of_scope_records = [
        path for path in valid_records if path not in in_scope_records
    ]
    if args.scope:
        print(
            "Scoped record coverage: "
            f"{len(in_scope_records)} in-scope, "
            f"{len(out_of_scope_records)} out-of-scope for "
            f"{release_evidence_commands.scope_label(scope)}.",
        )
        if valid_records and not in_scope_records:
            print(
                "No validated in-scope QA build records found; final evidence "
                "verification will still require a matching signed-build QA record.",
            )
            print("Next action:")
            print("- " + release_evidence_commands.signed_qa_record_hint(scope=scope))
    version = _single_version_for_records(in_scope_records if args.scope else valid_records)
    final_evidence_command = release_evidence_commands.verify_final_release_evidence_command(
        str(records_dir),
        scope=scope,
        version=version,
    )
    print(
        "Next final evidence check: "
        f"`{final_evidence_command}`",
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
