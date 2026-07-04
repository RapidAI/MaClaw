from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass, field
from pathlib import Path

import release_evidence_commands
import validate_qa_build_record
import validate_qa_build_records_dir

QA_LINKS_START = "<!-- QA_BUILD_RECORD_LINKS_START -->"
QA_LINKS_END = "<!-- QA_BUILD_RECORD_LINKS_END -->"


@dataclass(frozen=True)
class EvidenceLinkSummary:
    records_dir: Path
    scope: str
    valid_records: list[Path]
    invalid_records: list[validate_qa_build_records_dir.RecordValidationResult]
    versions: list[str]
    has_android: bool
    has_ios: bool
    out_of_scope_invalid_records: list[
        validate_qa_build_records_dir.RecordValidationResult
    ] = field(default_factory=list)


def _version_for_record(path: Path) -> str | None:
    match = validate_qa_build_record.QA_BUILD_RECORD_FILENAME_RE.fullmatch(path.name)
    if match is None:
        return None
    return match.group("version")


def summarize_records(
    records_dir: Path,
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
) -> EvidenceLinkSummary:
    scope = release_evidence_commands.validate_scope(scope)
    results = validate_qa_build_records_dir.validate_directory(records_dir)
    valid_records = [
        result.path
        for result in results
        if not result.errors
        and validate_qa_build_records_dir.record_matches_scope(result.path, scope)
    ]
    blocking_invalid_records = [
        result
        for result in results
        if result.errors
        and not validate_qa_build_records_dir.record_is_known_out_of_scope(
            result.path,
            scope,
        )
    ]
    out_of_scope_invalid_records = [
        result
        for result in results
        if result.errors
        and validate_qa_build_records_dir.record_is_known_out_of_scope(
            result.path,
            scope,
        )
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
    return EvidenceLinkSummary(
        records_dir=records_dir,
        scope=scope,
        valid_records=valid_records,
        invalid_records=blocking_invalid_records,
        versions=versions,
        has_android=has_android,
        has_ios=has_ios,
        out_of_scope_invalid_records=out_of_scope_invalid_records,
    )


def _record_link(path: Path, records_dir: Path) -> str:
    try:
        rel = path.relative_to(records_dir)
        target = f"docs/qa-builds/{rel.as_posix()}"
    except ValueError:
        target = path.as_posix()
    return f"- [{path.name}]({target})"


def default_release_evidence_path() -> Path:
    return Path(__file__).resolve().parents[1] / "docs/release_evidence.md"


def link_lines(summary: EvidenceLinkSummary) -> list[str]:
    return [_record_link(path, summary.records_dir) for path in summary.valid_records]


def _coverage_label(scope: str) -> str:
    if scope == release_evidence_commands.DEFAULT_SCOPE:
        return "Android and iOS"
    return release_evidence_commands.scope_label(scope)


def format_links(summary: EvidenceLinkSummary) -> str:
    lines: list[str] = [
        f"QA release evidence links for: {summary.records_dir}",
        f"Verification scope: {release_evidence_commands.scope_label(summary.scope)}",
    ]
    if summary.valid_records:
        lines.append("")
        if len(summary.versions) == 1:
            lines.append(f"Validated version/build: {summary.versions[0]}")
        elif len(summary.versions) > 1:
            lines.append(
                "Validated records use multiple version/build values and must not be linked as one release: "
                + ", ".join(summary.versions),
            )
        if has_required_platform_coverage(summary):
            lines.append(
                "Validated platform coverage: "
                + _coverage_label(summary.scope),
            )
        else:
            lines.append(
                "Validated records are missing final release platform coverage: "
                + ", ".join(missing_platform_coverage(summary)),
            )
        lines.append("Validated records to link from docs/release_evidence.md:")
        lines.extend(_record_link(path, summary.records_dir) for path in summary.valid_records)
        lines.append("")
        if len(summary.versions) == 1 and has_required_platform_coverage(summary):
            lines.append(
                "After adding these links, run: "
                + release_evidence_commands.verify_final_release_evidence_command(
                    scope=summary.scope,
                    version=summary.versions[0],
                ),
            )
        elif len(summary.versions) > 1:
            lines.append(
                "Final verifier is deferred until validated QA records use one version/build.",
            )
        else:
            lines.append(
                "Final verifier is deferred until validated records cover "
                + _coverage_label(summary.scope)
                + ".",
            )
    else:
        lines.append("")
        lines.append("No validated QA build records found.")

    if summary.invalid_records:
        lines.append("")
        lines.append("Records not linked because validation failed:")
        for result in summary.invalid_records:
            lines.append(f"- {result.path.name}")
            lines.extend(f"  - {error}" for error in result.errors)
    if summary.out_of_scope_invalid_records:
        lines.append("")
        lines.append(
            "Out-of-scope invalid records ignored for "
            f"{release_evidence_commands.scope_label(summary.scope)} links:",
        )
        for result in summary.out_of_scope_invalid_records:
            lines.append(f"- {result.path.name}")
            lines.extend(f"  - {error}" for error in result.errors)
    return "\n".join(lines).rstrip() + "\n"


def missing_platform_coverage(summary: EvidenceLinkSummary) -> list[str]:
    missing = []
    if (
        release_evidence_commands.scope_covers_android(summary.scope)
        and not summary.has_android
    ):
        missing.append("Android")
    if release_evidence_commands.scope_covers_ios(summary.scope) and not summary.has_ios:
        missing.append("iOS")
    return missing


def has_required_platform_coverage(summary: EvidenceLinkSummary) -> bool:
    return not missing_platform_coverage(summary)


def release_evidence_update_errors(summary: EvidenceLinkSummary) -> list[str]:
    errors: list[str] = []
    if not summary.valid_records:
        errors.append("at least one validated QA build record is required")
    if summary.invalid_records:
        errors.append("all QA build records must validate before links are updated")
    if len(summary.versions) > 1:
        errors.append(
            "validated QA build records must use one version/build: "
            + ", ".join(summary.versions),
        )
    if summary.valid_records and not has_required_platform_coverage(summary):
        errors.append(
            "validated QA build records are missing platform coverage: "
            + ", ".join(missing_platform_coverage(summary)),
        )
    return errors


def release_evidence_update_hints(summary: EvidenceLinkSummary) -> list[str]:
    hints: list[str] = []
    if summary.invalid_records:
        hints.append(
            release_evidence_commands.qa_build_record_report_hint(
                str(summary.invalid_records[0].path),
            ),
        )
    if not summary.valid_records and not summary.invalid_records:
        hints.append(release_evidence_commands.signed_qa_record_hint(scope=summary.scope))
    if len(summary.versions) > 1:
        hints.append(release_evidence_commands.qa_record_version_mismatch_hint())
    if summary.valid_records and not has_required_platform_coverage(summary):
        missing = missing_platform_coverage(summary)
        missing_scope = summary.scope
        if missing == ["Android"]:
            missing_scope = "android"
        elif missing == ["iOS"]:
            missing_scope = "ios"
        hints.append(release_evidence_commands.signed_qa_record_hint(scope=missing_scope))
    return hints


def update_release_evidence(summary: EvidenceLinkSummary, release_evidence_path: Path) -> None:
    update_errors = release_evidence_update_errors(summary)
    if update_errors:
        raise ValueError(
            "Release evidence links can only be updated after final QA records are ready: "
            + "; ".join(update_errors),
        )
    if not release_evidence_path.exists():
        raise FileNotFoundError(f"Release evidence document does not exist: {release_evidence_path}")
    if release_evidence_path.is_dir():
        raise IsADirectoryError(f"Release evidence path must be a markdown file: {release_evidence_path}")
    text = release_evidence_path.read_text(encoding="utf-8")
    start = text.find(QA_LINKS_START)
    end = text.find(QA_LINKS_END)
    if start < 0 or end < 0 or end < start:
        raise ValueError(
            f"Release evidence document must contain {QA_LINKS_START} and {QA_LINKS_END} markers."
        )
    before = text[: start + len(QA_LINKS_START)]
    after = text[end:]
    links = link_lines(summary)
    replacement = "\n"
    if links:
        replacement += "\n".join(links) + "\n"
    release_evidence_path.write_text(before + replacement + after, encoding="utf-8")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Print Markdown links for validated MaClaw Mobile QA build records "
            "that should be referenced from docs/release_evidence.md."
        ),
    )
    parser.add_argument(
        "records_dir",
        nargs="?",
        type=Path,
        default=validate_qa_build_records_dir.default_records_dir(),
        help="Directory containing completed signed-build QA records.",
    )
    parser.add_argument(
        "--update-release-evidence",
        nargs="?",
        const=default_release_evidence_path(),
        type=Path,
        help=(
            "Update the QA build record link block in docs/release_evidence.md "
            "after records validate, instead of requiring manual paste."
        ),
    )
    parser.add_argument(
        "--scope",
        choices=release_evidence_commands.VALID_SCOPES,
        default=release_evidence_commands.DEFAULT_SCOPE,
        help="Platform scope to require before updating release evidence links.",
    )
    args = parser.parse_args(argv)

    records_dir = args.records_dir.resolve()
    if not records_dir.exists():
        print(f"QA build record directory does not exist: {records_dir}", file=sys.stderr)
        return 1
    if not records_dir.is_dir():
        print(f"QA build record path is not a directory: {records_dir}", file=sys.stderr)
        return 1

    summary = summarize_records(records_dir, scope=args.scope)
    output = format_links(summary)
    if release_evidence_update_errors(summary):
        hints = release_evidence_update_hints(summary)
        if hints:
            output += "Next action:\n"
            output += "".join(f"- {hint}\n" for hint in hints)
        print(output, end="", file=sys.stderr)
        return 1
    if args.update_release_evidence is not None and summary.valid_records:
        try:
            update_release_evidence(summary, args.update_release_evidence.resolve())
        except (OSError, ValueError) as exc:
            print(output, end="", file=sys.stderr)
            print(f"Failed to update release evidence: {exc}", file=sys.stderr)
            return 1
        output += (
            f"Updated release evidence links in: {args.update_release_evidence.resolve()}\n"
        )
    print(output, end="")
    return 0


if __name__ == "__main__":
    sys.exit(main())
