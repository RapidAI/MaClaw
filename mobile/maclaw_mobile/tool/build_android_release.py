from __future__ import annotations

import argparse
import hashlib
import re
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

import release_evidence_commands
import verify_android_release_signing
import signed_artifact_evidence


REQUIRED_KEY_PROPERTIES = ("storeFile", "storePassword", "keyAlias", "keyPassword")
ANDROID_RELEASE_ARTIFACTS = ("apk", "appbundle")
BUILD_NAME_RE = re.compile(r"^\d+(?:\.\d+){1,3}$")
BUILD_NUMBER_RE = re.compile(r"^\d+$")


@dataclass(frozen=True)
class AndroidReleaseBuildPlan:
    command: list[str]
    artifact_path: Path
    key_store_path: Path


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _parse_key_properties(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        separator = "=" if "=" in line else ":" if ":" in line else ""
        if not separator:
            continue
        key, value = line.split(separator, 1)
        values[key.strip()] = value.strip()
    return values


def _resolve_store_file(android_dir: Path, raw_store_file: str) -> Path:
    candidate = Path(raw_store_file)
    if candidate.is_absolute():
        return candidate
    return android_dir / candidate


def validate_key_properties(root: Path) -> tuple[dict[str, str], list[str]]:
    key_properties = root / "android" / "key.properties"
    if not key_properties.exists():
        return {}, [f"Missing Android signing file: {key_properties}"]
    values = _parse_key_properties(key_properties)
    errors = [
        f"android/key.properties missing `{key}`"
        for key in REQUIRED_KEY_PROPERTIES
        if not values.get(key)
    ]
    if values.get("storeFile"):
        store_file = _resolve_store_file(key_properties.parent, values["storeFile"])
        if not store_file.exists():
            errors.append(f"Android signing storeFile does not exist: {store_file}")
        if "debug" in store_file.name.lower():
            errors.append("Android signing storeFile must not be a debug keystore")
    return values, errors


def build_plan(
    root: Path,
    artifact: str,
    build_name: str | None = None,
    build_number: str | None = None,
) -> AndroidReleaseBuildPlan:
    if artifact not in ANDROID_RELEASE_ARTIFACTS:
        raise ValueError(
            "Android --artifact must be one of "
            + ", ".join(ANDROID_RELEASE_ARTIFACTS),
        )
    key_values, errors = validate_key_properties(root)
    if errors:
        raise ValueError("; ".join(errors))
    if not build_name or not build_number:
        raise ValueError(
            "Android signed QA builds require both --build-name and --build-number "
            "so artifacts can be matched to QA records",
        )
    if build_name is not None and BUILD_NAME_RE.fullmatch(build_name.strip()) is None:
        raise ValueError("Android --build-name must be a numeric app version such as 1.0.0")
    if build_number is not None and BUILD_NUMBER_RE.fullmatch(build_number.strip()) is None:
        raise ValueError("Android --build-number must be a numeric build number such as 42")
    command = ["flutter", "build", "apk" if artifact == "apk" else "appbundle", "--release"]
    if build_name:
        command.extend(["--build-name", build_name.strip()])
    if build_number:
        command.extend(["--build-number", build_number.strip()])
    artifact_path = (
        root / "build" / "app" / "outputs" / "flutter-apk" / "app-release.apk"
        if artifact == "apk"
        else root / "build" / "app" / "outputs" / "bundle" / "release" / "app-release.aab"
    )
    store_file = _resolve_store_file(root / "android", key_values["storeFile"])
    return AndroidReleaseBuildPlan(command=command, artifact_path=artifact_path, key_store_path=store_file)


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _executable_command(command: list[str]) -> list[str]:
    executable = shutil.which(command[0])
    if executable is None:
        return command
    return [executable, *command[1:]]


def _qa_evidence_options_error(
    *,
    record_dir: Path | None,
    signing_identity: str | None,
    installer_channel: str | None,
) -> str:
    provided = [
        record_dir is not None,
        bool(signing_identity),
        bool(installer_channel),
    ]
    if any(provided) and not all(provided):
        return (
            "Generating QA artifact evidence requires --record-dir, "
            "--signing-identity, and --installer-channel together."
        )
    return ""


def _is_local_signing_input_error(message: str) -> bool:
    return any(
        marker in message
        for marker in (
            "Missing Android signing file",
            "android/key.properties",
            "Android signing storeFile",
        )
    )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Build a signed MaClaw Mobile Android release artifact with local key.properties.",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=mobile_root(),
        help="Path to mobile/maclaw_mobile. Defaults to this script project root.",
    )
    parser.add_argument(
        "--artifact",
        choices=ANDROID_RELEASE_ARTIFACTS,
        default="apk",
        help="Signed Android artifact type to build.",
    )
    parser.add_argument("--build-name", help="Required Flutter build name/version for QA traceability.")
    parser.add_argument("--build-number", help="Required Flutter build number for QA traceability.")
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Validate signing inputs and print the Flutter build command without running it.",
    )
    parser.add_argument(
        "--record-dir",
        type=Path,
        help=(
            "Optional QA records directory used to print QA artifact evidence "
            "after a successful build; default examples use docs/qa-builds."
        ),
    )
    parser.add_argument(
        "--signing-identity",
        help="Optional non-debug release/internal signing identity for QA artifact evidence.",
    )
    parser.add_argument(
        "--installer-channel",
        help="Optional auditable distribution channel for QA artifact evidence.",
    )
    args = parser.parse_args(argv)
    root = args.root.resolve()

    evidence_error = _qa_evidence_options_error(
        record_dir=args.record_dir,
        signing_identity=args.signing_identity,
        installer_channel=args.installer_channel,
    )
    if evidence_error:
        print(f"Android release build cannot start: {evidence_error}", file=sys.stderr)
        return 1
    if args.record_dir is not None:
        try:
            signed_artifact_evidence.validate_record_dir(args.record_dir)
        except (FileNotFoundError, ValueError) as exc:
            print(f"Android release build cannot start: {exc}", file=sys.stderr)
            return 1

    config_errors = verify_android_release_signing.verify_android_release_signing(root)
    if config_errors:
        print("Android release signing config is not ready:", file=sys.stderr)
        for error in config_errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    try:
        plan = build_plan(
            root,
            artifact=args.artifact,
            build_name=args.build_name,
            build_number=args.build_number,
        )
    except ValueError as exc:
        print(f"Android release build cannot start: {exc}", file=sys.stderr)
        if _is_local_signing_input_error(str(exc)):
            print(
                f"- Run `{release_evidence_commands.setup_android_signing_command()}` "
                "with release signing environment variables first.",
                file=sys.stderr,
            )
        return 1

    print("Android release signing inputs verified.")
    print(f"Signing store: {plan.key_store_path}")
    print(f"Command: {' '.join(plan.command)}")
    if args.dry_run:
        print("Dry run only; no Android artifact was built.")
        return 0

    try:
        subprocess.run(_executable_command(plan.command), cwd=root, check=True)
    except subprocess.CalledProcessError as exc:
        print(
            f"Android release build failed with exit code {exc.returncode}: {' '.join(plan.command)}",
            file=sys.stderr,
        )
        return 1
    if not plan.artifact_path.exists():
        print(f"Expected Android release artifact was not created: {plan.artifact_path}", file=sys.stderr)
        return 1
    print(f"Artifact: {plan.artifact_path}")
    print(f"Size: {plan.artifact_path.stat().st_size} bytes")
    print(f"SHA256: {_sha256_file(plan.artifact_path)}")
    if args.record_dir is not None:
        try:
            evidence_lines = signed_artifact_evidence.android_evidence_lines(
                plan.artifact_path,
                record_dir=args.record_dir,
                version=f"{args.build_name.strip()}+{args.build_number.strip()}",
                signing_identity=args.signing_identity or "",
                installer_channel=args.installer_channel or "",
            )
        except (FileNotFoundError, ValueError) as exc:
            print(f"QA artifact evidence could not be generated: {exc}", file=sys.stderr)
            return 1
        print("QA artifact evidence:")
        for line in evidence_lines:
            print(line)
    record_dir_label = str(args.record_dir or release_evidence_commands.DEFAULT_QA_RECORDS_DIR).replace("\\", "/")
    print("Record the artifact path, SHA256, version/build number, signing identity, and installer channel in the QA build record.")
    print(
        "After filling the signed-build QA record, run "
        f"`python3 tool/validate_qa_build_record.py {record_dir_label}/<record>.md`; "
        "if validation fails, run "
        f"`python3 tool/qa_build_record_report.py {record_dir_label}/<record>.md`."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
