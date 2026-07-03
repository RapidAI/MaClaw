from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path

import validate_qa_build_record


@dataclass(frozen=True)
class RequiredEvidenceProgress:
    filled: int
    required: int


@dataclass(frozen=True)
class QaBuildRecordReport:
    path: Path
    passed: bool
    progress: RequiredEvidenceProgress | None
    path_errors: list[str]
    secret_errors: list[str]
    evidence_errors: list[str]
    artifact_errors: list[str]
    unknown_errors: list[str]

    @property
    def errors(self) -> list[str]:
        return (
            self.path_errors
            + self.secret_errors
            + self.evidence_errors
            + self.artifact_errors
            + self.unknown_errors
        )


def _filled_required_count(values: dict[str, list[str]], field: str) -> int:
    return sum(1 for value in values.get(field, []) if value.strip())


def required_evidence_progress(
    values: dict[str, list[str]],
) -> RequiredEvidenceProgress:
    filled = 0
    required = 0
    for field, required_count in validate_qa_build_record.REQUIRED_FIELD_COUNTS.items():
        required += required_count
        filled += min(_filled_required_count(values, field), required_count)
    return RequiredEvidenceProgress(filled=filled, required=required)


def _path_validation_errors(path: Path) -> list[str]:
    if not path.exists():
        return [f"QA build record file does not exist: {path}"]
    if path.is_dir():
        return [f"QA build record path must be a markdown file, not a directory: {path}"]
    if path.suffix.lower() != ".md":
        return [f"QA build record path must be a markdown file: {path}"]
    if path.name.lower() == "readme.md":
        return ["QA build record path must point to a completed record, not README.md"]
    if path.name == "qa_build_record_template.md":
        return ["QA build record path must point to a completed record, not the template"]
    return []


def generate_report(path: Path) -> QaBuildRecordReport:
    validation_errors = validate_qa_build_record.validate_file(path)
    path_errors = _path_validation_errors(path)
    if path_errors:
        return QaBuildRecordReport(
            path=path,
            passed=False,
            progress=None,
            path_errors=path_errors,
            secret_errors=[],
            evidence_errors=[],
            artifact_errors=[],
            unknown_errors=[
                error for error in validation_errors if error not in path_errors
            ],
        )

    text = path.read_text(encoding="utf-8")
    values = validate_qa_build_record.parse_record(text)
    secret_errors = validate_qa_build_record.raw_secret_errors(text)
    filename_errors = validate_qa_build_record.qa_build_record_filename_errors(path, values)
    evidence_errors = validate_qa_build_record.missing_required_fields(values)
    artifact_errors = validate_qa_build_record.local_artifact_errors(values, path.parent)
    known_errors = set(
        path_errors + secret_errors + filename_errors + evidence_errors + artifact_errors
    )

    return QaBuildRecordReport(
        path=path,
        passed=not validation_errors,
        progress=required_evidence_progress(values),
        path_errors=path_errors + filename_errors,
        secret_errors=secret_errors,
        evidence_errors=evidence_errors,
        artifact_errors=artifact_errors,
        unknown_errors=[error for error in validation_errors if error not in known_errors],
    )


def _format_section(title: str, errors: list[str]) -> list[str]:
    if not errors:
        return []
    lines = [f"{title}:"]
    lines.extend(f"- {error}" for error in errors)
    return lines


def format_report(report: QaBuildRecordReport) -> str:
    lines = [
        f"QA build record report: {report.path}",
        f"Status: {'PASS' if report.passed else 'FAIL'}",
    ]
    if report.progress is not None:
        lines.append(
            "Required evidence: "
            f"{report.progress.filled}/{report.progress.required} occurrences filled"
        )
    lines.append("")

    sections = (
        _format_section("Path / filename", report.path_errors)
        + _format_section("Secret redaction", report.secret_errors)
        + _format_section("Evidence fields and values", report.evidence_errors)
        + _format_section("Local artifact hashes", report.artifact_errors)
        + _format_section("Unclassified validator output", report.unknown_errors)
    )
    if sections:
        lines.extend(sections)
    else:
        lines.append("No gaps found by the QA build record validator.")
    return "\n".join(lines).rstrip() + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Print an actionable MaClaw Mobile QA build record gap report.",
    )
    parser.add_argument("record", type=Path, help="Completed or in-progress QA record")
    args = parser.parse_args(argv)

    report = generate_report(args.record)
    output = format_report(report)
    if report.passed:
        print(output, end="")
        return 0
    print(output, end="", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
