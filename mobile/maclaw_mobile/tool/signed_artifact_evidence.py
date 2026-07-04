from __future__ import annotations

import argparse
import hashlib
import os
import re
import sys
from pathlib import Path


ANDROID_ARTIFACT_SUFFIXES = (".apk", ".aab")
ANDROID_ARTIFACT_MARKERS = ("release", "signed", "internal")
APPLE_TEAM_ID_RE = re.compile(r"^[A-Z0-9]{10}$")
VERSION_BUILD_RE = re.compile(r"^\d+(?:\.\d+){1,3}\+\d+$")
TESTFLIGHT_BUILD_RE = re.compile(r"(?i)\btestflight\s+build\s+\d+\b")
IOS_PROFILE_REFERENCE_RE = re.compile(
    r"(?i)(\buuid\s+[a-z0-9][a-z0-9-]{5,}\b|\.mobileprovision\b|\bprofile name\s+[^;,\n]{4,})"
)
PLACEHOLDER_VALUES = {
    "",
    "ok",
    "yes",
    "done",
    "n/a",
    "na",
    "todo",
    "tbd",
    "<apple_team_id>",
    "<runner profile; share extension profile>",
    "<xcode archive path or testflight build number>",
}


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


def is_version_build(value: str) -> bool:
    normalized = value.strip()
    return (
        len(normalized) >= 5
        and normalized.lower() not in PLACEHOLDER_VALUES
        and VERSION_BUILD_RE.fullmatch(normalized) is not None
    )


def is_android_signing_identity(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        len(normalized) >= 6
        and normalized not in PLACEHOLDER_VALUES
        and "debug" not in normalized
        and any(
            marker in normalized
            for marker in (
                "release",
                "signed",
                "internal",
                "upload",
                "keystore",
                "certificate",
                "cert",
                "play app signing",
            )
        )
        and any(
            marker in normalized
            for marker in (
                "alias",
                "sha-1",
                "sha1",
                "sha-256",
                "sha256",
                "fingerprint",
                "upload key",
                "upload certificate",
                "certificate id",
                "cert id",
            )
        )
    )


def is_installer_channel(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        len(normalized) >= 6
        and normalized not in PLACEHOLDER_VALUES
        and "debug" not in normalized
        and any(
            marker in normalized
            for marker in (
                "internal app sharing",
                "play internal",
                "internal testing",
                "closed testing",
                "open testing",
                "play console",
                "mdm",
                "enterprise",
                "firebase app distribution",
            )
        )
    )


def is_trackable_ios_archive(value: str) -> bool:
    normalized = value.strip().lower()
    if len(normalized) < 6 or normalized in PLACEHOLDER_VALUES:
        return False
    return normalized.endswith(".xcarchive") or TESTFLIGHT_BUILD_RE.search(value) is not None


def is_local_ios_archive_path(value: str) -> bool:
    return value.strip().lower().endswith(".xcarchive")


def is_trackable_ios_profiles(value: str) -> bool:
    normalized = value.strip().lower()
    return (
        len(normalized) >= 12
        and normalized not in PLACEHOLDER_VALUES
        and "runner" in normalized
        and ("share extension" in normalized or "shareextension" in normalized)
        and IOS_PROFILE_REFERENCE_RE.search(value) is not None
    )


def display_path(path: Path, *, record_dir: Path | None = None) -> str:
    if record_dir is None:
        return str(path)
    return Path(os.path.relpath(path.resolve(), record_dir.resolve())).as_posix()


def validate_record_dir(record_dir: Path | None) -> Path | None:
    if record_dir is None:
        return None
    record_dir = record_dir.resolve()
    if not record_dir.exists():
        raise FileNotFoundError(f"QA record directory does not exist: {record_dir}")
    if not record_dir.is_dir():
        raise ValueError(f"QA record path is not a directory: {record_dir}")
    return record_dir


def android_evidence_lines(
    artifact: Path,
    *,
    record_dir: Path | None = None,
    version: str = "",
    signing_identity: str = "",
    installer_channel: str = "",
) -> list[str]:
    artifact = artifact.resolve()
    record_dir = validate_record_dir(record_dir)
    if not artifact.exists():
        raise FileNotFoundError(f"Android signed artifact does not exist: {artifact}")
    if not artifact.is_file():
        raise ValueError(f"Android signed artifact is not a file: {artifact}")

    artifact_label = display_path(artifact, record_dir=record_dir)
    if not is_trackable_android_artifact(artifact_label):
        raise ValueError(
            "Artifact path must point to a signed/release/internal .apk or .aab file and must not contain debug.",
        )
    version = version.strip()
    if not version:
        raise ValueError(
            "Version/build number is required for Android signed artifact evidence.",
        )
    if not is_version_build(version):
        raise ValueError("Version/build number must use app-version+build, for example 1.0.0+42.")
    signing_identity = signing_identity.strip()
    if not signing_identity:
        raise ValueError(
            "Signing identity is required for Android signed artifact evidence.",
        )
    if not is_android_signing_identity(signing_identity):
        raise ValueError(
            "Signing identity must identify a non-debug release/internal signing identity.",
        )
    installer_channel = installer_channel.strip()
    if not installer_channel:
        raise ValueError(
            "Installer channel is required for Android signed artifact evidence.",
        )
    if not is_installer_channel(installer_channel):
        raise ValueError(
            "Installer channel must identify a non-debug auditable distribution channel.",
        )

    lines = [
        f"Artifact path: {artifact_label}",
        f"SHA256: {sha256_file(artifact).upper()}",
        f"Artifact size bytes: {artifact.stat().st_size}",
    ]
    lines.append(f"Version/build number: {version}")
    lines.append(f"Signing identity: {signing_identity}")
    lines.append(f"Installer channel: {installer_channel}")
    return lines


def ios_evidence_lines(
    *,
    archive_or_build: str,
    team_id: str = "",
    provisioning_profiles: str = "",
    record_dir: Path | None = None,
) -> list[str]:
    archive_or_build = archive_or_build.strip()
    team_id = team_id.strip().upper()
    provisioning_profiles = provisioning_profiles.strip()
    if not is_trackable_ios_archive(archive_or_build):
        raise ValueError(
            "Archive/TestFlight build must identify an .xcarchive or explicit TestFlight build number.",
        )
    if APPLE_TEAM_ID_RE.fullmatch(team_id) is None:
        raise ValueError("Team ID must be a 10-character Apple team identifier.")
    if not is_trackable_ios_profiles(provisioning_profiles):
        raise ValueError(
            "Provisioning profiles must mention Runner, Share Extension, and a trackable profile UUID/file/name.",
        )
    archive_label = archive_or_build.replace(chr(92), chr(47))
    if record_dir is not None and is_local_ios_archive_path(archive_or_build):
        archive_path = Path(archive_or_build).resolve()
        if not archive_path.exists():
            raise FileNotFoundError(f"iOS archive does not exist: {archive_path}")
        if not archive_path.is_dir():
            raise ValueError(f"iOS archive is not a directory: {archive_path}")
        record_dir = validate_record_dir(record_dir)
        archive_label = display_path(archive_path, record_dir=record_dir)
    elif record_dir is not None:
        validate_record_dir(record_dir)
    lines = [f"Archive/TestFlight build: {archive_label}"]
    lines.append(f"Team ID: {team_id}")
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
        help=(
            "Optional QA records directory; prints Artifact path relative to it. "
            "Default examples use docs/qa-builds."
        ),
    )
    android.add_argument("--version", required=True)
    android.add_argument("--signing-identity", required=True)
    android.add_argument("--installer-channel", required=True)

    ios = subparsers.add_parser("ios", help="Generate iOS archive/TestFlight evidence.")
    ios.add_argument("--archive-or-build", required=True)
    ios.add_argument("--team-id", required=True)
    ios.add_argument("--provisioning-profiles", required=True)
    ios.add_argument(
        "--record-dir",
        type=Path,
        default=None,
        help=(
            "Optional QA records directory; validates local .xcarchive paths "
            "and prints them relative to it. Default examples use docs/qa-builds."
        ),
    )

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
                record_dir=args.record_dir,
            )
    except (FileNotFoundError, ValueError) as exc:
        print(f"Signed artifact evidence generation failed: {exc}", file=sys.stderr)
        return 1

    print("\n".join(lines))
    return 0


if __name__ == "__main__":
    sys.exit(main())
