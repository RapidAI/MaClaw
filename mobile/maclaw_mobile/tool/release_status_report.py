from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import qa_preflight
import validate_qa_build_records_dir
import verify_final_release_evidence


@dataclass(frozen=True)
class ReleaseStatus:
    root: Path
    preflight_checks: list[qa_preflight.PreflightCheck]
    record_results: list[validate_qa_build_records_dir.RecordValidationResult]
    final_errors: list[str]

    @property
    def ready(self) -> bool:
        return (
            not any(check.is_blocker for check in self.preflight_checks)
            and not any(result.errors for result in self.record_results)
            and not self.final_errors
        )


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def build_status(
    root: Path,
    *,
    preflight: Callable[[Path], list[qa_preflight.PreflightCheck]] = qa_preflight.run_preflight,
    validate_records: Callable[
        [Path],
        list[validate_qa_build_records_dir.RecordValidationResult],
    ] = validate_qa_build_records_dir.validate_directory,
    verify_final: Callable[[Path], list[str]] = verify_final_release_evidence.verify_final_release_evidence,
) -> ReleaseStatus:
    root = root.resolve()
    records_dir = root / "docs" / "qa-builds"
    return ReleaseStatus(
        root=root,
        preflight_checks=preflight(root),
        record_results=validate_records(records_dir),
        final_errors=verify_final(records_dir),
    )


def _format_list(prefix: str, items: list[str]) -> list[str]:
    if not items:
        return [f"{prefix} none"]
    return [f"{prefix} {item}" for item in items]


def format_status(status: ReleaseStatus) -> str:
    lines = [
        f"MaClaw Mobile release status: {status.root}",
        "",
        "Preflight:",
    ]
    for check in status.preflight_checks:
        lines.append(f"- [{check.status.upper()}] {check.name}")
        lines.extend(f"  - {detail}" for detail in check.details)

    valid_records = [result.path for result in status.record_results if not result.errors]
    invalid_records = [result for result in status.record_results if result.errors]
    lines.extend(
        [
            "",
            f"QA build records: {len(valid_records)} valid, {len(invalid_records)} invalid",
        ],
    )
    if invalid_records:
        for result in invalid_records:
            lines.append(f"- [INVALID] {result.path}")
            lines.extend(f"  - {error}" for error in result.errors)
    elif not valid_records:
        lines.append("- No completed signed-build QA records found.")
    else:
        lines.extend(f"- [VALID] {path.name}" for path in valid_records)

    lines.append("")
    lines.append("Final release evidence:")
    if status.final_errors:
        lines.extend(_format_list("- [BLOCKER]", status.final_errors))
    else:
        lines.append("- [OK] Final release evidence verified.")

    lines.append("")
    if status.ready:
        lines.append("Result: READY for final release approval.")
    else:
        lines.append("Result: NOT READY.")
        lines.append("Next actions:")
        if any(check.is_blocker for check in status.preflight_checks):
            lines.append("- Clear preflight blockers, then build signed Android/iOS QA packages.")
        if not valid_records:
            lines.append("- Create and validate signed-build QA records under docs/qa-builds/.")
        if valid_records and status.final_errors:
            lines.append("- Link validated QA records in docs/release_evidence.md and rerun final verification.")
        elif status.final_errors:
            lines.append("- Complete final release evidence verification after QA records exist.")
    return "\n".join(lines).rstrip() + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Print a MaClaw Mobile release readiness status report.",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=mobile_root(),
        help="Path to mobile/maclaw_mobile. Defaults to this script project root.",
    )
    args = parser.parse_args(argv)

    status = build_status(args.root)
    output = format_status(status)
    if status.ready:
        print(output, end="")
        return 0
    print(output, end="", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
