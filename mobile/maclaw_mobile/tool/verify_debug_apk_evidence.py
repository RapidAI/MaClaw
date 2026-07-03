from __future__ import annotations

import argparse
import hashlib
import re
import sys
from dataclasses import dataclass
from pathlib import Path


DEFAULT_APK_PATH = Path("build/app/outputs/flutter-apk/app-debug.apk")


@dataclass(frozen=True)
class DebugApkEvidence:
    artifact: Path
    size: int
    sha256: str


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def parse_debug_apk_evidence(text: str) -> DebugApkEvidence:
    sections = _debug_apk_sections(text)
    if not sections:
        raise ValueError("Missing `flutter build apk --debug` evidence section.")
    body = next(
        (
            section
            for section in sections
            if "  - Artifact:" in section
            and "  - Size:" in section
            and "  - SHA256:" in section
        ),
        sections[0],
    )

    artifact = _required_group(
        re.search(r"(?m)^  - Artifact: `(?P<value>[^`]+)`\.?$", body),
        "Artifact",
    )
    size_text = _required_group(
        re.search(r"(?m)^  - Size: `(?P<value>[0-9]+)` bytes\.?$", body),
        "Size",
    )
    sha256 = _required_group(
        re.search(r"(?m)^  - SHA256: `(?P<value>[0-9A-Fa-f]{64})`\.?$", body),
        "SHA256",
    )
    return DebugApkEvidence(
        artifact=Path(artifact),
        size=int(size_text),
        sha256=sha256.upper(),
    )


def _debug_apk_sections(text: str) -> list[str]:
    lines = text.splitlines()
    sections: list[str] = []
    for index, line in enumerate(lines):
        if line != "- `flutter build apk --debug`":
            continue
        section_lines: list[str] = []
        for item in lines[index + 1 :]:
            if item.startswith("- `"):
                break
            if item.startswith("  "):
                section_lines.append(item)
                continue
            if not item.strip():
                break
            break
        sections.append("\n".join(section_lines))
    return sections


def _required_group(match: re.Match[str] | None, field: str) -> str:
    if match is None:
        raise ValueError(f"Missing debug APK evidence field: {field}.")
    return match.group("value")


def resolve_artifact_path(root: Path, artifact: Path) -> Path:
    if artifact.is_absolute():
        return artifact
    return root / artifact


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest().upper()


def verify_debug_apk_evidence(root: Path, evidence_path: Path) -> list[str]:
    evidence = parse_debug_apk_evidence(evidence_path.read_text(encoding="utf-8-sig"))
    artifact_path = resolve_artifact_path(root, evidence.artifact)
    errors: list[str] = []

    if not artifact_path.exists():
        errors.append(f"Debug APK artifact does not exist: {artifact_path}")
        return errors
    if not artifact_path.is_file():
        errors.append(f"Debug APK artifact is not a file: {artifact_path}")
        return errors

    actual_size = artifact_path.stat().st_size
    if actual_size != evidence.size:
        errors.append(
            f"Debug APK size mismatch: evidence={evidence.size} actual={actual_size}"
        )

    actual_sha = sha256_file(artifact_path)
    if actual_sha != evidence.sha256:
        errors.append(
            f"Debug APK SHA256 mismatch: evidence={evidence.sha256} actual={actual_sha}"
        )

    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Verify release_evidence.md matches the current local debug APK.",
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
    args = parser.parse_args(argv)

    root = args.root.resolve()
    evidence_path = args.evidence or (root / "docs/release_evidence.md")
    errors = verify_debug_apk_evidence(root, evidence_path.resolve())
    if not errors:
        print("Debug APK release evidence verified.")
        return 0

    print("Debug APK release evidence validation failed:", file=sys.stderr)
    for error in errors:
        print(f"- {error}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
