from __future__ import annotations

import argparse
import hashlib
import os
import sys
from pathlib import Path


ANDROID_ARTIFACT_SUFFIXES = (".apk", ".aab")
ANDROID_ARTIFACT_MARKERS = ("release", "signed", "internal")


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def is_trackable_android_artifact(path_text: str) -> bool:
    normalized = path_text.strip().lower()
    return (
        len(normalized) >= 6
        and "debug" not in normalized
        and normalized.endswith(ANDROID_ARTIFACT_SUFFIXES)
        and any(marker in normalized for marker in ANDROID_ARTIFACT_MARKERS)
    )


def display_path(path: Path, *, record_dir: Path | None = None) -> str:
    if record_dir is None:
        return str(path)
    return os.path.relpath(path.resolve(), record_dir.resolve())


def android_evidence_lines(
    artifact: Path,
    *,
    record_dir: Path | None = None,
    version: str = "",
    signing_identity: str = "",
    installer_channel: str = "",
) -> list[str]:
    artifact = artifact.resolve()
    if not artifact.exists():
        raise FileNotFoundError(f"Android signed artifact does not exist: {artifact}")
    if not artifact.is_file():
        raise ValueError(f"Android signed artifact is not a file: {artifact}")

    artifact_label = display_path(artifact, record_dir=record_dir)
    if not is_trackable_android_artifact(artifact_label):
        raise ValueError(
            "Artifact path must point to a signed/release/internal .apk or .aab file and must not contain debug.",
        )

    lines = [
        f"Artifact path: {artifact_label}",
        f"SHA256: {sha256_file(artifact).upper()}",
        f"Artifact size bytes: {artifact.stat().st_size}",
    ]
    if version:
        lines.append(f"Version/build number: {version}")
    if signing_identity:
        lines.append(f"Signing identity: {signing_identity}")
    if installer_channel:
        lines.append(f"Installer channel: {installer_channel}")
    return lines


def ios_evidence_lines(
    *,
    archive_or_build: str,
    team_id: str = "",
    provisioning_profiles: str = "",
) -> list[str]:
    lines = [f"Archive/TestFlight build: {archive_or_build}"]
    if team_id:
        lines.append(f"Team ID: {team_id.upper()}")
    if provisioning_profiles:
        lines.append(f"Provisioning profiles: {provisioning_profiles}")
    return lines


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Generate paste-ready signed-build QA artifact evidence fields.",
    )
    subparsers = parser.add_subparsers(dest="platform", required=True)

    android = subparsers.add_parser("android", help="Generate Android signed APK/AAB evidence.")
    android.add_argument("artifact", type=Path)
    android.add_argument(
        "--record-dir",
        type=Path,
        default=None,
        help="Optional docs/qa-builds directory; prints Artifact path relative to it.",
    )
    android.add_argument("--version", default="")
    android.add_argument("--signing-identity", default="")
    android.add_argument("--installer-channel", default="")

    ios = subparsers.add_parser("ios", help="Generate iOS archive/TestFlight evidence.")
    ios.add_argument("--archive-or-build", required=True)
    ios.add_argument("--team-id", default="")
    ios.add_argument("--provisioning-profiles", default="")

    args = parser.parse_args(argv)
    try:
        if args.platform == "android":
            lines = android_evidence_lines(
                args.artifact,
                record_dir=args.record_dir,
                version=args.version,
                signing_identity=args.signing_identity,
                installer_channel=args.installer_channel,
            )
        else:
            lines = ios_evidence_lines(
                archive_or_build=args.archive_or_build,
                team_id=args.team_id,
                provisioning_profiles=args.provisioning_profiles,
            )
    except (FileNotFoundError, ValueError) as exc:
        print(f"Signed artifact evidence generation failed: {exc}", file=sys.stderr)
        return 1

    print("\n".join(lines))
    return 0


if __name__ == "__main__":
    sys.exit(main())
