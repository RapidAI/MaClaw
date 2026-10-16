from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

import verify_debug_apk_evidence


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def debug_apk_evidence_lines(root: Path, artifact: Path) -> list[str]:
    artifact_path = verify_debug_apk_evidence.resolve_artifact_path(root, artifact)
    if not artifact_path.exists():
        raise FileNotFoundError(f"Debug APK artifact does not exist: {artifact_path}")
    if not artifact_path.is_file():
        raise ValueError(f"Debug APK artifact is not a file: {artifact_path}")
    return [
        f"  - Artifact: `{artifact.as_posix()}`.",
        f"  - Size: `{artifact_path.stat().st_size}` bytes.",
        f"  - SHA256: `{verify_debug_apk_evidence.sha256_file(artifact_path)}`.",
    ]


def update_debug_apk_evidence_text(text: str, root: Path, artifact: Path) -> str:
    lines = text.splitlines()
    for index, line in enumerate(lines):
        if line != "- `flutter build apk --debug`":
            continue
        section_end = index + 1
        while section_end < len(lines):
            current = lines[section_end]
            if current.startswith("- `"):
                break
            if not current.strip():
                break
            section_end += 1

        field_indexes = {
            "artifact": next(
                (i for i in range(index + 1, section_end) if re.match(r"^  - Artifact: `", lines[i])),
                None,
            ),
            "size": next(
                (i for i in range(index + 1, section_end) if re.match(r"^  - Size: `", lines[i])),
                None,
            ),
            "sha256": next(
                (i for i in range(index + 1, section_end) if re.match(r"^  - SHA256: `", lines[i])),
                None,
            ),
        }
        if any(value is None for value in field_indexes.values()):
            continue

        evidence_lines = debug_apk_evidence_lines(root, artifact)
        lines[field_indexes["artifact"]] = evidence_lines[0]  # type: ignore[index]
        lines[field_indexes["size"]] = evidence_lines[1]  # type: ignore[index]
        lines[field_indexes["sha256"]] = evidence_lines[2]  # type: ignore[index]
        return "\n".join(lines).rstrip() + "\n"

    raise ValueError(
        "Missing debug APK evidence section with Artifact, Size, and SHA256 fields.",
    )


def update_debug_apk_evidence_file(root: Path, evidence_path: Path, artifact: Path) -> None:
    original = evidence_path.read_text(encoding="utf-8-sig")
    updated = update_debug_apk_evidence_text(original, root, artifact)
    evidence_path.write_text(updated, encoding="utf-8")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Update release_evidence.md with the current local debug APK artifact, size, and SHA256.",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=mobile_root(),
        help="Path to mobile/maclaw_mobile.",
    )
    parser.add_argument(
        "--evidence",
        type=Path,
        default=None,
        help="Path to release_evidence.md. Defaults to docs/release_evidence.md.",
    )
    parser.add_argument(
        "--artifact",
        type=Path,
        default=verify_debug_apk_evidence.DEFAULT_APK_PATH,
        help="Debug APK artifact path, relative to --root unless absolute.",
    )
    args = parser.parse_args(argv)

    root = args.root.resolve()
    evidence_path = args.evidence or (root / "docs/release_evidence.md")
    try:
        update_debug_apk_evidence_file(root, evidence_path.resolve(), args.artifact)
    except (FileNotFoundError, ValueError) as exc:
        print(f"Debug APK evidence update failed: {exc}", file=sys.stderr)
        return 1
    print(f"Updated debug APK release evidence: {evidence_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
