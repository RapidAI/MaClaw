from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

import qa_release_evidence_links
import release_evidence_commands
import validate_qa_build_record
import validate_qa_build_records_dir

QA_MARKDOWN_LINK_RE = re.compile(r"\[[^\]]+\]\((?P<target>[^)]+\.md)\)")
EXPECTED_LOG_FILENAME_RE = re.compile(r"expected (?P<name>final-release-evidence[^\s]+\.log)")


def default_records_dir() -> Path:
    return Path(__file__).resolve().parents[1] / "docs/qa-builds"


def default_release_evidence_path() -> Path:
    return Path(__file__).resolve().parents[1] / "docs/release_evidence.md"


def _version_for_record(path: Path) -> str | None:
    match = validate_qa_build_record.QA_BUILD_RECORD_FILENAME_RE.fullmatch(path.name)
    if match is None:
        return None
    return match.group("version")


def _expected_log_filename(version: str, *, scope: str) -> str:
    return Path(
        release_evidence_commands.final_release_evidence_log_path(
            version,
            scope=scope,
            records_dir=".",
        ),
    ).name


def _successful_record_versions(records_dir: Path, *, scope: str) -> list[str]:
    results = validate_qa_build_records_dir.validate_directory(records_dir)
    valid_records = [
        result.path
        for result in results
        if not result.errors
        and validate_qa_build_records_dir.record_matches_scope(result.path, scope)
    ]
    return sorted(
        version
        for version in {_version_for_record(path) for path in valid_records}
        if version is not None
    )


def _versions_from_record_error_text(errors: list[str]) -> list[str]:
    versions = set()
    for error in errors:
        for raw in re.findall(r"[^,\s]+\.md", error):
            name = Path(raw).name
            match = validate_qa_build_record.QA_BUILD_RECORD_FILENAME_RE.fullmatch(name)
            if match is not None:
                versions.add(match.group("version"))
    return sorted(versions)


def final_log_path_errors(log_path: Path, records_dir: Path, *, scope: str) -> list[str]:
    scope = release_evidence_commands.validate_scope(scope)
    versions = _successful_record_versions(records_dir, scope=scope)
    if len(versions) != 1:
        return []
    expected_name = _expected_log_filename(versions[0], scope=scope)
    if log_path.name == expected_name:
        return []
    return [
        "Final release evidence log filename must match the validated "
        f"version/build: expected {expected_name}",
    ]


def _record_link_errors(
    valid_records: list[Path],
    records_dir: Path,
    release_evidence_path: Path,
) -> list[str]:
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
    expected_targets = {
        qa_release_evidence_links.record_link_target(path, records_dir)
        for path in valid_records
    }
    missing = [
        path.name
        for path in valid_records
        if f"]({qa_release_evidence_links.record_link_target(path, records_dir)})"
        not in link_block
    ]
    extra_targets = sorted(
        target
        for target in _qa_record_link_targets(link_block)
        if target not in expected_targets
    )
    errors = []
    if missing:
        errors.append(
            "Release evidence document must include Markdown links for every validated QA build record: "
            + ", ".join(missing),
        )
    if extra_targets:
        errors.append(
            "Release evidence document guarded QA build record link block must not include stale or unvalidated QA record links: "
            + ", ".join(extra_targets),
        )
    return errors


def _qa_record_link_targets(markdown: str) -> list[str]:
    targets: list[str] = []
    for match in QA_MARKDOWN_LINK_RE.finditer(markdown):
        target = match.group("target").strip()
        name = Path(target).name
        if validate_qa_build_record.QA_BUILD_RECORD_FILENAME_RE.fullmatch(name):
            targets.append(target)
    return targets


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
    elif not relevant_records:
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

    errors.extend(_record_link_errors(relevant_records, records_dir, release_evidence_path))
    return errors


def next_action_hints(
    errors: list[str],
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
    records_dir: str = release_evidence_commands.DEFAULT_QA_RECORDS_DIR,
    existing_versions: list[str] | None = None,
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
    expected_log_names = [
        match.group("name")
        for error in errors
        for match in [EXPECTED_LOG_FILENAME_RE.search(error)]
        if match is not None
    ]
    missing_platforms = []
    if any("requires a validated Android signed-build QA record" in error for error in errors):
        missing_platforms.append("android")
    if any("requires a validated iOS signed-build QA record" in error for error in errors):
        missing_platforms.append("ios")
    if invalid_records:
        hints.append(
            release_evidence_commands.qa_build_record_report_hint(invalid_records[0]),
        )
    if expected_log_names:
        expected_log_path = Path(records_dir) / expected_log_names[0]
        hints.append(
            "rerun final release evidence verification with the matching log path: "
            + release_evidence_commands.verify_final_release_evidence_command(
                records_dir,
                scope=scope,
                log=expected_log_path.as_posix(),
            ),
        )
    if needs_signed_record:
        signed_scope = missing_platforms[0] if len(missing_platforms) == 1 else scope
        versions = existing_versions
        if versions is None:
            versions = _successful_record_versions(Path(records_dir), scope=scope)
        version = (
            versions[0]
            if len(versions) == 1
            else release_evidence_commands.DEFAULT_VERSION
        )
        hints.append(
            release_evidence_commands.signed_qa_record_hint(
                scope=signed_scope,
                version=version,
                records_dir=records_dir,
            ),
        )
    if needs_release_evidence_links:
        link_versions = _versions_from_record_error_text(errors)
        version = (
            link_versions[0]
            if len(link_versions) == 1
            else release_evidence_commands.DEFAULT_VERSION
        )
        hints.append(
            release_evidence_commands.qa_release_evidence_link_hint(
                scope=scope,
                records_dir=records_dir,
                version=version,
            ),
        )
    if needs_single_version:
        hints.append(
            release_evidence_commands.qa_record_version_mismatch_hint(
                records_dir=records_dir,
            ),
        )
    if not hints:
        hints.append(
            release_evidence_commands.signed_qa_record_hint(
                scope=scope,
                records_dir=records_dir,
            ),
        )
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
    records_dir_arg = (
        str(records_dir) if records_dir is not None else release_evidence_commands.DEFAULT_QA_RECORDS_DIR
    )
    lines.extend(
        f"- {hint}"
        for hint in next_action_hints(
            errors,
            scope=scope,
            records_dir=records_dir_arg,
        )
    )
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
    log_path_validation_errors: list[str] = []
    if not errors:
        if args.log:
            log_path_validation_errors = final_log_path_errors(
                args.log,
                args.records_dir.resolve(),
                scope=args.scope,
            )
            errors.extend(log_path_validation_errors)
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
    if args.log and not log_path_validation_errors:
        try:
            write_log(args.log, output, force=args.force)
        except FileExistsError as exc:
            print(f"Final release evidence log write failed: {exc}", file=sys.stderr)
            return 1
    print(output, end="")
    return 1


if __name__ == "__main__":
    sys.exit(main())
