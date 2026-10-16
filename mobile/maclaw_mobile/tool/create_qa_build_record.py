from __future__ import annotations

import argparse
import re
import sys
from datetime import datetime
from pathlib import Path

import release_evidence_commands
import validate_qa_build_record


VALID_SCOPES = release_evidence_commands.VALID_SCOPES
VERSION_BUILD_RE = re.compile(r"^\d+(?:\.\d+){1,3}\+\d+$")
FINAL_DECISION_PREFILL_FIELDS = {
    "release_handoff_result": "Release handoff result",
    "preflight_result": "Preflight result",
    "runtime_boundary_result": "Runtime boundary verification result",
    "automated_gates_result": "Automated release gates result",
}
OUT_OF_SCOPE_SECTION_PREFIXES = {
    "android": ("## iOS ",),
    "ios": ("## Android ",),
}
OUT_OF_SCOPE_FIELD_PREFIXES = {
    "android": ("iOS manual gates passed:",),
    "ios": ("Android manual gates passed:",),
}


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


def records_dir_command_arg(records_dir: Path) -> str:
    if records_dir.resolve() == default_records_dir().resolve():
        return release_evidence_commands.DEFAULT_QA_RECORDS_DIR
    return str(records_dir)


def render_record(
    template: str,
    record_date: str,
    version_build: str,
    scope: str = release_evidence_commands.DEFAULT_SCOPE,
    final_decision_prefills: dict[str, str] | None = None,
) -> str:
    rendered = (
        template.replace("Date: YYYY-MM-DD", f"Date: {record_date}", 1)
        .replace(
            "Version/build number: app version + build number, such as 1.0.0+42",
            f"Version/build number: {version_build}",
            1,
        )
    )
    for field, value in (final_decision_prefills or {}).items():
        if not value.strip():
            continue
        rendered = rendered.replace(f"{field}:", f"{field}: {value.strip()}", 1)
    return remove_out_of_scope_sections(rendered, scope)


def remove_out_of_scope_sections(markdown: str, scope: str) -> str:
    prefixes = OUT_OF_SCOPE_SECTION_PREFIXES.get(scope)
    field_prefixes = OUT_OF_SCOPE_FIELD_PREFIXES.get(scope, ())
    if not prefixes and not field_prefixes:
        return markdown
    lines = markdown.splitlines(keepends=True)
    kept: list[str] = []
    skip = False
    for line in lines:
        if line.startswith("## "):
            skip = any(line.startswith(prefix) for prefix in prefixes or ())
        if not skip and not any(line.startswith(prefix) for prefix in field_prefixes):
            kept.append(line)
    return "".join(kept)


def final_decision_prefill_errors(prefills: dict[str, str]) -> list[str]:
    values = {
        field: [value.strip()]
        for field, value in prefills.items()
        if field in FINAL_DECISION_PREFILL_FIELDS.values() and value.strip()
    }
    if not values:
        return []
    expected_prefixes = [
        f"{field} {hint}"
        for field, hint in validate_qa_build_record.FINAL_AUTOMATED_EVIDENCE_FIELDS.items()
    ]
    return [
        error
        for error in validate_qa_build_record.missing_required_fields(values)
        if error in expected_prefixes
    ]


def create_record(
    *,
    template_path: Path,
    records_dir: Path,
    record_date: str,
    scope: str,
    version_build: str,
    final_decision_prefills: dict[str, str] | None = None,
    force: bool = False,
) -> Path:
    if scope not in VALID_SCOPES:
        raise ValueError(f"unsupported scope: {scope}")
    if not template_path.exists():
        raise FileNotFoundError(f"QA build record template not found: {template_path}")
    prefill_errors = final_decision_prefill_errors(final_decision_prefills or {})
    if prefill_errors:
        raise ValueError(
            "invalid Final Release Decision prefill: " + "; ".join(prefill_errors),
        )
    records_dir.mkdir(parents=True, exist_ok=True)
    target = records_dir / record_filename(record_date, scope, version_build)
    if target.exists() and not force:
        raise FileExistsError(f"QA build record already exists: {target}")
    rendered = render_record(
        template_path.read_text(encoding="utf-8"),
        record_date,
        version_build,
        scope,
        final_decision_prefills,
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
    parser.add_argument(
        "--release-handoff-result",
        help="Optional handoff output path, transcript reference, or attachment ID to prefill.",
    )
    parser.add_argument(
        "--preflight-result",
        help="Optional qa_preflight.py READY output/log reference to prefill.",
    )
    parser.add_argument(
        "--runtime-boundary-result",
        help="Optional runtime-boundary verifier output/log reference to prefill.",
    )
    parser.add_argument(
        "--automated-gates-result",
        help="Optional release-gates output/log reference to prefill.",
    )
    args = parser.parse_args(argv)

    try:
        final_decision_prefills = {
            field: value
            for arg_name, field in FINAL_DECISION_PREFILL_FIELDS.items()
            for value in [getattr(args, arg_name)]
            if value
        }
        target = create_record(
            template_path=args.template,
            records_dir=args.records_dir,
            record_date=args.date,
            scope=args.scope,
            version_build=args.version,
            final_decision_prefills=final_decision_prefills,
            force=args.force,
        )
    except (FileExistsError, FileNotFoundError, ValueError) as exc:
        print(f"QA build record creation failed: {exc}", file=sys.stderr)
        return 1

    print(f"Created QA build record: {target}")
    print(
        "Complete manual evidence before validation: real-device share/permission, "
        "Hub discovery with post-SMS-verification official credits LLM proof including concrete llm-request-id and llm-usage-record, notification, and GUI-equivalent backend-managed SSH session evidence with "
        "the same GUI/agent-bound backend_session_id, GUI/agent claim "
        "or worker handoff plus explicit worker claim/update evidence and "
        "`ssh_session` realtime `output_chunk`/`output_seq` proof, copied-output GUI/agent evidence line with actual values for Hub session ID, backend_session_id, concrete claimed_by worker identity such as claimed_by desktop-agent-1, and numeric output_seq, preserving that evidence line while replacing credentials or private customer excerpts with redacted text or a traceable attachment ID, not phone-local/ad hoc terminal evidence, phone-initiated "
        "interrupt evidence through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt` showing GUI/agent Ctrl+C handling, and "
        "AI/digital-employee handoff evidence tied to that same GUI/agent-bound backend_session_id "
        "when used.",
    )
    records_dir_arg = records_dir_command_arg(args.records_dir)
    if release_evidence_commands.scope_covers_android(args.scope):
        print(
            "Generate Android signed artifact evidence: "
            f"{release_evidence_commands.android_artifact_evidence_command(args.version, record_dir=records_dir_arg)}",
        )
    if release_evidence_commands.scope_covers_ios(args.scope):
        print(
            "Generate iOS signed artifact evidence: "
            f"{release_evidence_commands.ios_artifact_evidence_command(record_dir=records_dir_arg)}",
        )
    print(
        "Validate after completing evidence: "
        f"{release_evidence_commands.validate_qa_build_record_command(str(target))}",
    )
    print(
        "Inspect missing evidence if validation fails: "
        f"{release_evidence_commands.qa_build_record_report_command(str(target))}",
    )
    print(
        "After validation passes, update release evidence links: "
        f"{release_evidence_commands.qa_release_evidence_link_command(records_dir=records_dir_arg, scope=args.scope)}",
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
