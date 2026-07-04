from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path

import release_evidence_commands
import validate_qa_build_record


_DEFAULT_FINAL_PREFILLS = release_evidence_commands.final_decision_prefills()

EVIDENCE_FIELD_HINTS = {
    "Release handoff result": (
        f"Use `{_DEFAULT_FINAL_PREFILLS['Release handoff result']}` "
        f"after running `{release_evidence_commands.release_handoff_command()}`."
    ),
    "Runtime boundary verification result": (
        f"Use `{_DEFAULT_FINAL_PREFILLS['Runtime boundary verification result']}` after running "
        "`python3 tool/verify_runtime_boundary.py --log "
        "docs/qa-builds/runtime-boundary-<version+build>.log`."
    ),
    "Automated release gates result": (
        f"Use `{_DEFAULT_FINAL_PREFILLS['Automated release gates result']}` after running "
        "`python3 tool/run_release_gates.py --log "
        "docs/qa-builds/release-gates-<version+build>.log`."
    ),
}


def _evidence_field_hints(
    version: str = release_evidence_commands.DEFAULT_VERSION,
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
) -> dict[str, str]:
    prefills = release_evidence_commands.final_decision_prefills(version)
    return {
        "Release handoff result": (
            f"Use `{prefills['Release handoff result']}` "
            f"after running `{release_evidence_commands.release_handoff_command(version=version, scope=scope)}`."
        ),
        "Runtime boundary verification result": (
            f"Use `{prefills['Runtime boundary verification result']}` after running "
            f"`{release_evidence_commands.runtime_boundary_command(version)}`."
        ),
        "Automated release gates result": (
            f"Use `{prefills['Automated release gates result']}` after running "
            f"`{release_evidence_commands.release_gates_command(version)}`."
        ),
    }


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
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
) -> RequiredEvidenceProgress:
    values = validate_qa_build_record.scoped_values(values, scope)
    filled = 0
    required = 0
    for field, required_count in validate_qa_build_record.required_field_counts_for_scope(
        scope,
    ).items():
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
    scope = validate_qa_build_record.record_scope_from_path(path)
    in_scope_values = validate_qa_build_record.scoped_values(values, scope)
    secret_errors = validate_qa_build_record.raw_secret_errors(text)
    filename_errors = validate_qa_build_record.qa_build_record_filename_errors(
        path,
        in_scope_values,
    )
    evidence_errors = validate_qa_build_record.missing_required_fields(
        values,
        scope=scope,
    )
    artifact_errors = validate_qa_build_record.local_artifact_errors(
        in_scope_values,
        path.parent,
    )
    known_errors = set(
        path_errors + secret_errors + filename_errors + evidence_errors + artifact_errors
    )

    return QaBuildRecordReport(
        path=path,
        passed=not validation_errors,
        progress=required_evidence_progress(values, scope=scope),
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


def _evidence_hints(errors: list[str]) -> list[str]:
    return _evidence_hints_for_version(
        errors,
        release_evidence_commands.DEFAULT_VERSION,
    )


def _evidence_hints_for_version(
    errors: list[str],
    version: str,
    *,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
) -> list[str]:
    hints_by_field = _evidence_field_hints(version, scope=scope)
    hints = []
    for field, hint in hints_by_field.items():
        if any(error == field or error.startswith(f"{field} ") for error in errors):
            hints.append(f"- {field}: {hint}")
    return hints


ANDROID_ARTIFACT_FIELDS = {
    "Artifact path",
    "SHA256",
    "Artifact size bytes",
    "Version/build number",
    "Signing identity",
    "Installer channel",
}
IOS_ARTIFACT_FIELDS = {
    "Archive/TestFlight build",
    "Team ID",
    "Provisioning profiles",
}


def _matches_field_error(error: str, fields: set[str]) -> bool:
    return any(error == field or error.startswith(f"{field} ") for field in fields)


def _artifact_hints_for_version(errors: list[str], version: str) -> list[str]:
    hints: list[str] = []
    if any(_matches_field_error(error, ANDROID_ARTIFACT_FIELDS) for error in errors):
        hints.append(
            "- Android signed artifact: run `"
            + release_evidence_commands.android_artifact_evidence_command(version)
            + "` and paste the generated fields into the QA record."
        )
    if any(_matches_field_error(error, IOS_ARTIFACT_FIELDS) for error in errors):
        hints.append(
            "- iOS archive/TestFlight artifact: run `"
            + release_evidence_commands.ios_artifact_evidence_command()
            + "` and paste the generated fields into the QA record."
        )
    return hints


def _secret_redaction_hints(errors: list[str]) -> list[str]:
    if not errors:
        return []
    return [
        "- Remove raw secrets from the QA record, then replace them with redacted evidence, attachment IDs, task IDs, artifact hashes, or reviewer notes.",
        "- Re-run `python3 tool/validate_qa_build_record.py docs/qa-builds/<record>.md` before linking the record from docs/release_evidence.md.",
    ]


def _version_for_report(report: QaBuildRecordReport) -> str:
    match = validate_qa_build_record.QA_BUILD_RECORD_FILENAME_RE.fullmatch(report.path.name)
    if match is None:
        return release_evidence_commands.DEFAULT_VERSION
    return match.group("version")


def _scope_for_report(report: QaBuildRecordReport) -> str:
    return validate_qa_build_record.record_scope_from_path(report.path)


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
        version = _version_for_report(report)
        scope = _scope_for_report(report)
        hints = _evidence_hints_for_version(
            report.evidence_errors,
            version,
            scope=scope,
        )
        if hints:
            lines.append("")
            lines.append("How to fill release decision evidence:")
            lines.extend(hints)
        artifact_hints = _artifact_hints_for_version(
            report.evidence_errors + report.artifact_errors,
            version,
        )
        if artifact_hints:
            lines.append("")
            lines.append("How to fill signed artifact evidence:")
            lines.extend(artifact_hints)
        secret_hints = _secret_redaction_hints(report.secret_errors)
        if secret_hints:
            lines.append("")
            lines.append("How to fix secret redaction failures:")
            lines.extend(secret_hints)
    else:
        lines.append("No gaps found by the QA build record validator.")
        lines.append("")
        lines.append("Next action:")
        lines.append(
            f"- Run `{release_evidence_commands.qa_release_evidence_link_command(scope=_scope_for_report(report))}` "
            "to link validated QA records in docs/release_evidence.md."
        )
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
