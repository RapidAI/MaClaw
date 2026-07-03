from __future__ import annotations

import argparse
import re
import sys
from datetime import datetime
from pathlib import Path


VALID_SCOPES = ("android", "ios", "android-ios")
VERSION_BUILD_RE = re.compile(r"^\d+(?:\.\d+){1,3}\+\d+$")


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def default_template_path() -> Path:
    return mobile_root() / "docs" / "qa_build_record_template.md"


def default_records_dir() -> Path:
    return mobile_root() / "docs" / "qa-builds"


def today_string() -> str:
    return datetime.now().date().isoformat()


def validate_record_date(value: str) -> str:
    try:
        parsed = datetime.strptime(value, "%Y-%m-%d").date()
    except ValueError as exc:
        raise argparse.ArgumentTypeError("date must use YYYY-MM-DD") from exc
    today = datetime.now().date()
    if parsed > today:
        raise argparse.ArgumentTypeError("date must not be in the future")
    return parsed.isoformat()


def validate_version_build(value: str) -> str:
    normalized = value.strip()
    if VERSION_BUILD_RE.fullmatch(normalized) is None:
        raise argparse.ArgumentTypeError(
            "version must use app-version+build, for example 1.0.0+42",
        )
    return normalized


def record_filename(record_date: str, scope: str, version_build: str) -> str:
    return f"{record_date}-{scope}-{version_build}.md"


def render_record(template: str, record_date: str, version_build: str) -> str:
    return (
        template.replace("Date: YYYY-MM-DD", f"Date: {record_date}", 1)
        .replace(
            "Version/build number: app version + build number, such as 1.0.0+42",
            f"Version/build number: {version_build}",
            1,
        )
    )


def create_record(
    *,
    template_path: Path,
    records_dir: Path,
    record_date: str,
    scope: str,
    version_build: str,
    force: bool = False,
) -> Path:
    if scope not in VALID_SCOPES:
        raise ValueError(f"unsupported scope: {scope}")
    if not template_path.exists():
        raise FileNotFoundError(f"QA build record template not found: {template_path}")
    records_dir.mkdir(parents=True, exist_ok=True)
    target = records_dir / record_filename(record_date, scope, version_build)
    if target.exists() and not force:
        raise FileExistsError(f"QA build record already exists: {target}")
    rendered = render_record(
        template_path.read_text(encoding="utf-8"),
        record_date,
        version_build,
    )
    target.write_text(rendered, encoding="utf-8")
    return target


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Create a validator-named MaClaw Mobile QA build record from the template.",
    )
    parser.add_argument(
        "--date",
        default=today_string(),
        type=validate_record_date,
        help="QA record date in YYYY-MM-DD form. Defaults to today.",
    )
    parser.add_argument(
        "--scope",
        required=True,
        choices=VALID_SCOPES,
        help="Platform evidence scope covered by this signed QA build record.",
    )
    parser.add_argument(
        "--version",
        required=True,
        type=validate_version_build,
        help="App version plus build number, for example 1.0.0+42.",
    )
    parser.add_argument(
        "--records-dir",
        type=Path,
        default=default_records_dir(),
        help="Directory for generated QA build records.",
    )
    parser.add_argument(
        "--template",
        type=Path,
        default=default_template_path(),
        help="QA build record template path.",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Overwrite an existing generated record with the same name.",
    )
    args = parser.parse_args(argv)

    try:
        target = create_record(
            template_path=args.template,
            records_dir=args.records_dir,
            record_date=args.date,
            scope=args.scope,
            version_build=args.version,
            force=args.force,
        )
    except (FileExistsError, FileNotFoundError, ValueError) as exc:
        print(f"QA build record creation failed: {exc}", file=sys.stderr)
        return 1

    print(f"Created QA build record: {target}")
    print(f"Validate after completing evidence: python3 tool/validate_qa_build_record.py {target}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
