from __future__ import annotations

import argparse
import plistlib
import sys
from pathlib import Path

import plan_ios_release


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def export_options_payload(team_id: str, export_method: str) -> dict[str, object]:
    if export_method not in plan_ios_release.VALID_EXPORT_METHODS:
        raise ValueError(f"unsupported iOS export method: {export_method}")
    return {
        "method": export_method,
        "signingStyle": "automatic",
        "stripSwiftSymbols": True,
        "teamID": plan_ios_release.validate_team_id(team_id),
    }


def write_export_options(
    root: Path,
    *,
    team_id: str,
    export_method: str,
    force: bool = False,
) -> Path:
    target = root / "ios" / "ExportOptions.plist"
    if target.exists() and not force:
        raise FileExistsError(
            f"{target} already exists; pass --force to overwrite local export options",
        )
    target.parent.mkdir(parents=True, exist_ok=True)
    with target.open("wb") as handle:
        plistlib.dump(export_options_payload(team_id, export_method), handle)
    return target


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Create local ios/ExportOptions.plist for MaClaw Mobile iOS archive export.",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=mobile_root(),
        help="Path to mobile/maclaw_mobile. Defaults to this script project root.",
    )
    parser.add_argument("--team-id", required=True, type=plan_ios_release.validate_team_id)
    parser.add_argument(
        "--export-method",
        choices=plan_ios_release.VALID_EXPORT_METHODS,
        default="development",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Overwrite an existing local ios/ExportOptions.plist file.",
    )
    args = parser.parse_args(argv)

    try:
        target = write_export_options(
            args.root.resolve(),
            team_id=args.team_id,
            export_method=args.export_method,
            force=args.force,
        )
    except (FileExistsError, ValueError, argparse.ArgumentTypeError) as exc:
        print(f"iOS export options setup failed: {exc}", file=sys.stderr)
        return 1

    payload = export_options_payload(args.team_id, args.export_method)
    print(f"Wrote local iOS export options: {target}")
    print(
        "Next for iOS-only QA: run "
        "`python3 tool/qa_preflight.py --scope ios "
        f"--team-id {payload['teamID']} --export-method {payload['method']} "
        "--log docs/qa-builds/preflight-ios-<version+build>.log`."
    )
    print(
        "For Android/iOS QA, ensure Android signing is configured, then run "
        "`python3 tool/qa_preflight.py --scope android-ios "
        f"--team-id {payload['teamID']} --export-method {payload['method']} "
        "--log docs/qa-builds/preflight-<version+build>.log`."
    )
    print(
        "Keep ios/ExportOptions.plist local and do not add placeholder "
        "signing/export files."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
