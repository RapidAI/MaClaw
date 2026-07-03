from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path

import validate_qa_build_records_dir


@dataclass(frozen=True)
class EvidenceLinkSummary:
    records_dir: Path
    valid_records: list[Path]
    invalid_records: list[validate_qa_build_records_dir.RecordValidationResult]


def summarize_records(records_dir: Path) -> EvidenceLinkSummary:
    results = validate_qa_build_records_dir.validate_directory(records_dir)
    valid_records = [result.path for result in results if not result.errors]
    invalid_records = [result for result in results if result.errors]
    return EvidenceLinkSummary(
        records_dir=records_dir,
        valid_records=valid_records,
        invalid_records=invalid_records,
    )


def _record_link(path: Path, records_dir: Path) -> str:
    try:
        rel = path.relative_to(records_dir)
        target = f"docs/qa-builds/{rel.as_posix()}"
    except ValueError:
        target = path.as_posix()
    return f"- [{path.name}]({target})"


def format_links(summary: EvidenceLinkSummary) -> str:
    lines: list[str] = [
        f"QA release evidence links for: {summary.records_dir}",
    ]
    if summary.valid_records:
        lines.append("")
        lines.append("Validated records to link from docs/release_evidence.md:")
        lines.extend(_record_link(path, summary.records_dir) for path in summary.valid_records)
    else:
        lines.append("")
        lines.append("No validated QA build records found.")

    if summary.invalid_records:
        lines.append("")
        lines.append("Records not linked because validation failed:")
        for result in summary.invalid_records:
            lines.append(f"- {result.path.name}")
            lines.extend(f"  - {error}" for error in result.errors)
    return "\n".join(lines).rstrip() + "\n"


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
    args = parser.parse_args(argv)

    records_dir = args.records_dir.resolve()
    if not records_dir.exists():
        print(f"QA build record directory does not exist: {records_dir}", file=sys.stderr)
        return 1
    if not records_dir.is_dir():
        print(f"QA build record path is not a directory: {records_dir}", file=sys.stderr)
        return 1

    summary = summarize_records(records_dir)
    output = format_links(summary)
    if summary.invalid_records:
        print(output, end="", file=sys.stderr)
        return 1
    print(output, end="")
    return 0


if __name__ == "__main__":
    sys.exit(main())
