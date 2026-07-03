from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import validate_qa_build_record


@dataclass(frozen=True)
class RecordValidationResult:
    path: Path
    errors: list[str]


def default_records_dir() -> Path:
    return Path(__file__).resolve().parents[1] / "docs/qa-builds"


def completed_record_paths(records_dir: Path) -> list[Path]:
    if not records_dir.exists() or not records_dir.is_dir():
        return []
    return sorted(
        path
        for path in records_dir.iterdir()
        if path.is_file()
        and path.suffix.lower() == ".md"
        and path.name.lower() != "readme.md"
    )


def validate_directory(
    records_dir: Path,
    validate_file: Callable[[Path], list[str]] = validate_qa_build_record.validate_file,
) -> list[RecordValidationResult]:
    return [
        RecordValidationResult(path=path, errors=validate_file(path))
        for path in completed_record_paths(records_dir)
    ]


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
    if failed:
        print("QA build record directory validation failed:")
        for result in failed:
            print(f"- {result.path}:")
            for error in result.errors:
                print(f"  - {error}")
        return 1

    print(f"QA build record directory validation passed: {len(results)} record(s).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
