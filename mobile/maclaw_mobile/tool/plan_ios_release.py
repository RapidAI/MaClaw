from __future__ import annotations

import argparse
import plistlib
import re
import sys
from dataclasses import dataclass
from pathlib import Path

import release_evidence_commands
import signed_artifact_evidence
import verify_ios_wrapper


APPLE_TEAM_ID_RE = re.compile(r"^[A-Z0-9]{10}$")
VALID_EXPORT_METHODS = ("development", "ad-hoc", "enterprise", "app-store")
RUNNER_BUNDLE_ID = "top.mypapers.maclaw.mobile"
SHARE_EXTENSION_BUNDLE_ID = "top.mypapers.maclaw.mobile.ShareExtension"
APP_GROUP = "group.top.mypapers.maclaw.mobile"


@dataclass(frozen=True)
class IOSReleasePlan:
    archive_command: list[str]
    export_command: list[str]
    archive_path: Path
    export_dir: Path
    export_options_path: Path


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def validate_team_id(value: str) -> str:
    normalized = value.strip().upper()
    if APPLE_TEAM_ID_RE.fullmatch(normalized) is None:
        raise argparse.ArgumentTypeError("team id must be a 10-character Apple team identifier")
    return normalized


def release_plan(
    root: Path,
    *,
    team_id: str,
    export_method: str,
    archive_path: Path,
    export_dir: Path,
    export_options_path: Path,
) -> IOSReleasePlan:
    if export_method not in VALID_EXPORT_METHODS:
        raise ValueError(f"unsupported iOS export method: {export_method}")
    workspace = root / "ios" / "Runner.xcworkspace"
    archive_command = [
        "xcodebuild",
        "archive",
        "-workspace",
        str(workspace),
        "-scheme",
        "Runner",
        "-configuration",
        "Release",
        "-archivePath",
        str(archive_path),
        "DEVELOPMENT_TEAM=" + team_id,
    ]
    export_command = [
        "xcodebuild",
        "-exportArchive",
        "-archivePath",
        str(archive_path),
        "-exportPath",
        str(export_dir),
        "-exportOptionsPlist",
        str(export_options_path),
    ]
    return IOSReleasePlan(
        archive_command=archive_command,
        export_command=export_command,
        archive_path=archive_path,
        export_dir=export_dir,
        export_options_path=export_options_path,
    )


def validate_export_options(
    path: Path,
    *,
    team_id: str,
    export_method: str,
) -> list[str]:
    if not path.exists():
        return [f"Missing export options plist: {path}"]
    try:
        with path.open("rb") as handle:
            payload = plistlib.load(handle)
    except (plistlib.InvalidFileException, OSError, ValueError) as exc:
        return [f"Export options plist is not readable: {path}: {exc}"]
    errors: list[str] = []
    if payload.get("teamID") != team_id:
        errors.append(
            f"Export options teamID must match {team_id}: found {payload.get('teamID')!r}",
        )
    if payload.get("method") != export_method:
        errors.append(
            f"Export options method must match {export_method}: found {payload.get('method')!r}",
        )
    return errors


def _format_command(command: list[str]) -> str:
    return " ".join(f'"{arg}"' if " " in arg else arg for arg in command)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Plan a signed MaClaw Mobile iOS archive/TestFlight QA build on macOS.",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=mobile_root(),
        help="Path to mobile/maclaw_mobile. Defaults to this script project root.",
    )
    parser.add_argument("--team-id", required=True, type=validate_team_id)
    parser.add_argument(
        "--export-method",
        choices=VALID_EXPORT_METHODS,
        default="development",
        help="Xcode export method for the signed QA artifact.",
    )
    parser.add_argument(
        "--archive-path",
        type=Path,
        default=Path("build/ios/archive/MaClawMobile.xcarchive"),
    )
    parser.add_argument(
        "--export-dir",
        type=Path,
        default=Path("build/ios/export"),
    )
    parser.add_argument(
        "--export-options",
        type=Path,
        default=Path("ios/ExportOptions.plist"),
        help="Path to the Xcode export options plist. Generate it with tool/setup_ios_export_options.py.",
    )
    parser.add_argument(
        "--provisioning-profiles",
        help=(
            "Optional Runner and Share Extension provisioning profile UUID/name/file "
            "summary used to print paste-ready QA artifact evidence."
        ),
    )
    parser.add_argument(
        "--record-dir",
        type=Path,
        help=(
            "Optional QA records directory; validates local .xcarchive paths "
            "and prints them relative to the QA record location when artifact "
            "evidence is generated. Default examples use docs/qa-builds."
        ),
    )
    args = parser.parse_args(argv)
    root = args.root.resolve()
    if args.record_dir is not None and not args.provisioning_profiles:
        print(
            "iOS release plan cannot generate QA artifact evidence: "
            "--record-dir requires --provisioning-profiles.",
            file=sys.stderr,
        )
        return 1

    wrapper_errors = verify_ios_wrapper.verify_ios_wrapper(root)
    if wrapper_errors:
        print("iOS wrapper is not ready for signed archive planning:", file=sys.stderr)
        for error in wrapper_errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    export_options_path = args.export_options
    export_options_check_path = (
        export_options_path
        if export_options_path.is_absolute()
        else root / export_options_path
    )
    export_options_errors = validate_export_options(
        export_options_check_path,
        team_id=args.team_id,
        export_method=args.export_method,
    )
    if export_options_errors:
        print("iOS export options are not ready for signed archive planning:", file=sys.stderr)
        for error in export_options_errors:
            print(f"- {error}", file=sys.stderr)
        print(
            "- Run `"
            + release_evidence_commands.setup_ios_export_options_command(
                team_id=args.team_id,
                export_method=args.export_method,
            )
            + "` first.",
            file=sys.stderr,
        )
        return 1

    try:
        plan = release_plan(
            root,
            team_id=args.team_id,
            export_method=args.export_method,
            archive_path=args.archive_path,
            export_dir=args.export_dir,
            export_options_path=export_options_path,
        )
    except ValueError as exc:
        print(f"iOS release plan failed: {exc}", file=sys.stderr)
        return 1

    print("MaClaw Mobile iOS wrapper inputs verified.")
    print(f"Runner bundle id: {RUNNER_BUNDLE_ID}")
    print(f"Share Extension bundle id: {SHARE_EXTENSION_BUNDLE_ID}")
    print(f"App group: {APP_GROUP}")
    print(f"Team ID: {args.team_id}")
    print(f"Export method: {args.export_method}")
    print(f"Export options: {plan.export_options_path}")
    print(f"Archive command: {_format_command(plan.archive_command)}")
    print(f"Export command: {_format_command(plan.export_command)}")
    if args.provisioning_profiles:
        record_dir = None
        if args.record_dir is not None:
            record_dir = (
                args.record_dir
                if args.record_dir.is_absolute()
                else root / args.record_dir
            )
        archive_or_build = plan.archive_path
        if record_dir is not None and not archive_or_build.is_absolute():
            archive_or_build = root / archive_or_build
        try:
            evidence_lines = signed_artifact_evidence.ios_evidence_lines(
                archive_or_build=str(archive_or_build),
                team_id=args.team_id,
                provisioning_profiles=args.provisioning_profiles,
                record_dir=record_dir,
            )
        except (FileNotFoundError, ValueError) as exc:
            print(f"iOS QA artifact evidence could not be generated: {exc}", file=sys.stderr)
            return 1
        print("iOS QA artifact evidence:")
        for line in evidence_lines:
            print(line)
    print("Record the .xcarchive path or TestFlight build number, Team ID, Runner and Share Extension provisioning profiles, bundle IDs, app group, and URL scheme evidence in the QA build record.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
