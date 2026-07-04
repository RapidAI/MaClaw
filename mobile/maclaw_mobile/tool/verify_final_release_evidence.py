from __future__ import annotations

import argparse
import sys
from pathlib import Path

import qa_release_evidence_links
import release_evidence_commands
import validate_qa_build_record
import validate_qa_build_records_dir


def default_records_dir() -> Path:
    return Path(__file__).resolve().parents[1] / "docs/qa-builds"


def default_release_evidence_path() -> Path:
    return Path(__file__).resolve().parents[1] / "docs/release_evidence.md"


def _version_for_record(path: Path) -> str | None:
    match = validate_qa_build_record.QA_BUILD_RECORD_FILENAME_RE.fullmatch(path.name)
    if match is None:
        return None
    return match.group("version")


def _record_link_errors(valid_records: list[Path], release_evidence_path: Path) -> list[str]:
    if not valid_records:
        return []
    if not release_evidence_path.exists():
        return [f"Release evidence document does not exist: {release_evidence_path}"]
    if release_evidence_path.is_dir():
        return [f"Release evidence path must be a markdown file: {release_evidence_path}"]
    text = release_evidence_path.read_text(encoding="utf-8")
    start = text.find(qa_release_evidence_links.QA_LINKS_START)
    end = text.find(qa_release_evidence_links.QA_LINKS_END)
    if start < 0 or end < 0 or end < start:
        return [
            "Release evidence document must contain the guarded QA build record link block markers.",
        ]
    link_block = text[start:end]
    missing = [
        path.name
        for path in valid_records
        if f"](docs/qa-builds/{path.name})" not in link_block
    ]
    if not missing:
        return []
    return [
        "Release evidence document must include Markdown links for every validated QA build record: "
        + ", ".join(missing),
    ]


def verify_final_release_evidence(
    records_dir: Path,
    release_evidence_path: Path | None = None,
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
) -> list[str]:
    if not records_dir.exists():
        return [f"QA build record directory does not exist: {records_dir}"]
    if not records_dir.is_dir():
        return [f"QA build record path is not a directory: {records_dir}"]
    if release_evidence_path is None:
        release_evidence_path = default_release_evidence_path()
    scope = release_evidence_commands.validate_scope(scope)

    results = validate_qa_build_records_dir.validate_directory(records_dir)
    errors: list[str] = []
    valid_records = []
    for result in results:
        if result.errors:
            if not validate_qa_build_records_dir.record_is_known_out_of_scope(
                result.path,
                scope,
            ):
                errors.append(f"{result.path}:")
                errors.extend(f"  - {error}" for error in result.errors)
        else:
            valid_records.append(result.path)

    relevant_records = [
        path
        for path in valid_records
        if validate_qa_build_records_dir.record_matches_scope(path, scope)
    ]

    if not results:
        if scope == release_evidence_commands.DEFAULT_SCOPE:
            errors.append(
                "Final release evidence requires at least one completed signed-build QA record.",
            )
        else:
            errors.append(
                "Final release evidence requires at least one completed "
                f"{release_evidence_commands.scope_label(scope)} signed-build QA record.",
            )

    platform_scopes = {
        validate_qa_build_records_dir.scope_for_record(path)
        for path in relevant_records
    }
    has_android = any(release_evidence_commands.scope_covers_android(scope) for scope in platform_scopes)
    has_ios = any(release_evidence_commands.scope_covers_ios(scope) for scope in platform_scopes)

    if (
        valid_records
        and release_evidence_commands.scope_covers_android(scope)
        and not has_android
    ):
        errors.append("Final release evidence requires a validated Android signed-build QA record.")
    if (
        valid_records
        and release_evidence_commands.scope_covers_ios(scope)
        and not has_ios
    ):
        errors.append("Final release evidence requires a validated iOS signed-build QA record.")
    versions = sorted(
        version
        for version in {_version_for_record(path) for path in relevant_records}
        if version is not None
    )
    if len(versions) > 1:
        errors.append(
            "Final release evidence records must use the same version/build: "
            + ", ".join(versions),
        )

    errors.extend(_record_link_errors(relevant_records, release_evidence_path))
    return errors


def next_action_hints(
    errors: list[str],
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
) -> list[str]:
    scope = release_evidence_commands.validate_scope(scope)
    hints: list[str] = []
    invalid_records = [
        error[:-1]
        for error in errors
        if error.endswith(":") and ".md" in error.lower()
    ]
    needs_signed_record = any(
        error.startswith("Final release evidence requires at least one")
        or error.startswith("Final release evidence requires a validated")
        for error in errors
    )
    needs_release_evidence_links = any(
        error.startswith("Release evidence document") for error in errors
    )
    needs_single_version = any(
        error.startswith("Final release evidence records must use the same version/build")
        for error in errors
    )
    if invalid_records:
        hints.append(
            release_evidence_commands.qa_build_record_report_hint(invalid_records[0]),
        )
    if needs_signed_record:
        hints.append(release_evidence_commands.signed_qa_record_hint(scope=scope))
    if needs_release_evidence_links:
        hints.append(release_evidence_commands.qa_release_evidence_link_hint(scope=scope))
    if needs_single_version:
        hints.append(release_evidence_commands.qa_record_version_mismatch_hint())
    if not hints:
        hints.append(release_evidence_commands.signed_qa_record_hint(scope=scope))
    return hints


def format_verified_summary(
    records_dir: Path,
    release_evidence_path: Path,
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
) -> str:
    scope = release_evidence_commands.validate_scope(scope)
    results = validate_qa_build_records_dir.validate_directory(records_dir)
    valid_records = [
        result.path
        for result in results
        if not result.errors
        and validate_qa_build_records_dir.record_matches_scope(result.path, scope)
    ]
    versions = sorted(
        version
        for version in {_version_for_record(path) for path in valid_records}
        if version is not None
    )
    scopes = {
        validate_qa_build_records_dir.scope_for_record(path)
        for path in valid_records
    }
    has_android = any(release_evidence_commands.scope_covers_android(scope) for scope in scopes)
    has_ios = any(release_evidence_commands.scope_covers_ios(scope) for scope in scopes)
    if has_android and has_ios:
        coverage = "Android and iOS"
    elif has_android:
        coverage = "Android"
    elif has_ios:
        coverage = "iOS"
    else:
        coverage = "incomplete"
    lines = [
        "Final MaClaw Mobile release evidence verified.",
        f"- Verification scope: {scope}",
        f"- QA records directory: {records_dir}",
        f"- Version/build: {versions[0] if len(versions) == 1 else 'unknown'}",
        f"- Platform coverage: {coverage}",
        f"- Release evidence: {release_evidence_path}",
        "- Validated QA records:",
    ]
    lines.extend(f"  - {path.name}" for path in valid_records)
    return "\n".join(lines) + "\n"


def format_failure(
    errors: list[str],
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
    records_dir: Path | None = None,
    release_evidence_path: Path | None = None,
) -> str:
    scope = release_evidence_commands.validate_scope(scope)
    lines = [
        "Final MaClaw Mobile release evidence validation failed:",
        f"- Verification scope: {scope}",
    ]
    if records_dir is not None:
        lines.append(f"- QA records directory: {records_dir}")
    if release_evidence_path is not None:
        lines.append(f"- Release evidence: {release_evidence_path}")
    lines.extend(f"- {error}" for error in errors)
    lines.append("Next action:")
    lines.extend(f"- {hint}" for hint in next_action_hints(errors, scope=scope))
    return "\n".join(lines) + "\n"


def write_log(path: Path, text: str, *, force: bool = False) -> None:
    if path.exists() and not force:
        raise FileExistsError(
            f"{path} already exists; pass --force to overwrite final release evidence log",
        )
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Verify that final MaClaw Mobile release evidence includes validated "
            "signed-build QA records for the requested platform scope."
        ),
    )
    parser.add_argument(
        "records_dir",
        nargs="?",
        type=Path,
        default=default_records_dir(),
        help="Directory containing completed signed-build QA records.",
    )
    parser.add_argument(
        "--release-evidence",
        type=Path,
        default=default_release_evidence_path(),
        help="Release evidence markdown that links validated QA build records.",
    )
    parser.add_argument(
        "--scope",
        choices=release_evidence_commands.VALID_SCOPES,
        default=release_evidence_commands.DEFAULT_SCOPE,
        help="Platform scope to verify. Defaults to android-ios final release coverage.",
    )
    parser.add_argument(
        "--log",
        type=Path,
        help="Optional path to write the final release evidence verification result.",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Overwrite an existing final release evidence log.",
    )
    args = parser.parse_args(argv)

    errors = verify_final_release_evidence(
        args.records_dir.resolve(),
        args.release_evidence.resolve(),
        scope=args.scope,
    )
    if not errors:
        output = format_verified_summary(
            args.records_dir.resolve(),
            args.release_evidence.resolve(),
            scope=args.scope,
        )
        if args.log:
            try:
                write_log(args.log, output, force=args.force)
            except FileExistsError as exc:
                print(f"Final release evidence log write failed: {exc}", file=sys.stderr)
                return 1
        print(output, end="")
        return 0

    output = format_failure(
        errors,
        scope=args.scope,
        records_dir=args.records_dir.resolve(),
        release_evidence_path=args.release_evidence.resolve(),
    )
    if args.log:
        try:
            write_log(args.log, output, force=args.force)
        except FileExistsError as exc:
            print(f"Final release evidence log write failed: {exc}", file=sys.stderr)
            return 1
    print(output, end="")
    return 1


if __name__ == "__main__":
    sys.exit(main())
