from __future__ import annotations

import argparse
import sys
from pathlib import Path

import validate_qa_build_record
import validate_qa_build_records_dir


def default_records_dir() -> Path:
    return Path(__file__).resolve().parents[1] / "docs/qa-builds"


def default_release_evidence_path() -> Path:
    return Path(__file__).resolve().parents[1] / "docs/release_evidence.md"


def _scope_for_record(path: Path) -> str | None:
    match = validate_qa_build_record.QA_BUILD_RECORD_FILENAME_RE.fullmatch(path.name)
    if match is None:
        return None
    return match.group("scope")


def _record_link_errors(valid_records: list[Path], release_evidence_path: Path) -> list[str]:
    if not valid_records:
        return []
    if not release_evidence_path.exists():
        return [f"Release evidence document does not exist: {release_evidence_path}"]
    if release_evidence_path.is_dir():
        return [f"Release evidence path must be a markdown file: {release_evidence_path}"]
    text = release_evidence_path.read_text(encoding="utf-8")
    missing = [path.name for path in valid_records if path.name not in text]
    if not missing:
        return []
    return [
        "Release evidence document must link every validated QA build record: "
        + ", ".join(missing),
    ]


def verify_final_release_evidence(
    records_dir: Path,
    release_evidence_path: Path | None = None,
) -> list[str]:
    if not records_dir.exists():
        return [f"QA build record directory does not exist: {records_dir}"]
    if not records_dir.is_dir():
        return [f"QA build record path is not a directory: {records_dir}"]
    if release_evidence_path is None:
        release_evidence_path = default_release_evidence_path()

    results = validate_qa_build_records_dir.validate_directory(records_dir)
    errors: list[str] = []
    valid_records = []
    for result in results:
        if result.errors:
            errors.append(f"{result.path}:")
            errors.extend(f"  - {error}" for error in result.errors)
        else:
            valid_records.append(result.path)

    if not results:
        errors.append(
            "Final release evidence requires at least one completed signed-build QA record.",
        )

    platform_scopes = {_scope_for_record(path) for path in valid_records}
    has_android = any(scope in {"android", "android-ios", "ios-android"} for scope in platform_scopes)
    has_ios = any(scope in {"ios", "android-ios", "ios-android"} for scope in platform_scopes)

    if valid_records and not has_android:
        errors.append("Final release evidence requires a validated Android signed-build QA record.")
    if valid_records and not has_ios:
        errors.append("Final release evidence requires a validated iOS signed-build QA record.")

    errors.extend(_record_link_errors(valid_records, release_evidence_path))
    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Verify that final MaClaw Mobile release evidence includes validated "
            "signed-build QA records for both Android and iOS."
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
    args = parser.parse_args(argv)

    errors = verify_final_release_evidence(
        args.records_dir.resolve(),
        args.release_evidence.resolve(),
    )
    if not errors:
        print("Final MaClaw Mobile release evidence verified.")
        return 0

    print("Final MaClaw Mobile release evidence validation failed:")
    for error in errors:
        print(f"- {error}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
