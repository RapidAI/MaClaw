from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class ReleaseGate:
    name: str
    cwd: Path
    command: list[str]


def repo_root() -> Path:
    return Path(__file__).resolve().parents[3]


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def release_gates() -> list[ReleaseGate]:
    root = repo_root()
    mobile = mobile_root()
    return [
        ReleaseGate(
            "Go mobile API",
            root,
            ["go", "test", "./hub/internal/httpapi", "-run", "TestMobile.*", "-count=1"],
        ),
        ReleaseGate(
            "Go mobile HubCenter discovery",
            root,
            [
                "go",
                "test",
                "./hubcenter/internal/httpapi",
                "-run",
                "TestMobile(ServiceRedemption|DesktopQRSession)",
                "-count=1",
            ],
        ),
        ReleaseGate(
            "Go mobile GUI and digital employee",
            root,
            [
                "go",
                "test",
                "./gui",
                "-run",
                "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown",
                "-count=1",
            ],
        ),
        ReleaseGate(
            "Platform configuration tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/configure_platforms_test.py"],
        ),
        ReleaseGate(
            "QA record validator tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/validate_qa_build_record_test.py"],
        ),
        ReleaseGate(
            "QA build record scaffold tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/create_qa_build_record_test.py"],
        ),
        ReleaseGate(
            "QA records directory validator tests",
            mobile,
            [
                sys.executable,
                "-m",
                "unittest",
                "tool/validate_qa_build_records_dir_test.py",
            ],
        ),
        ReleaseGate(
            "QA build record report tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/qa_build_record_report_test.py"],
        ),
        ReleaseGate(
            "QA release evidence link helper tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/qa_release_evidence_links_test.py"],
        ),
        ReleaseGate(
            "QA preflight helper tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/qa_preflight_test.py"],
        ),
        ReleaseGate(
            "Android signing setup helper tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/setup_android_signing_test.py"],
        ),
        ReleaseGate(
            "Release status report helper tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/release_status_report_test.py"],
        ),
        ReleaseGate(
            "Release handoff helper tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/release_handoff_test.py"],
        ),
        ReleaseGate(
            "QA records directory validation",
            mobile,
            [sys.executable, "tool/validate_qa_build_records_dir.py", "docs/qa-builds"],
        ),
        ReleaseGate(
            "Runtime boundary verifier tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/verify_runtime_boundary_test.py"],
        ),
        ReleaseGate(
            "Release gate runner tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/run_release_gates_test.py"],
        ),
        ReleaseGate(
            "Debug APK evidence verifier tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/verify_debug_apk_evidence_test.py"],
        ),
        ReleaseGate(
            "Debug APK evidence updater tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/update_debug_apk_evidence_test.py"],
        ),
        ReleaseGate(
            "Signed artifact evidence helper tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/signed_artifact_evidence_test.py"],
        ),
        ReleaseGate(
            "Manual release gate parity tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/verify_manual_release_gates_test.py"],
        ),
        ReleaseGate(
            "Final release evidence verifier tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/verify_final_release_evidence_test.py"],
        ),
        ReleaseGate(
            "Android release signing verifier tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/verify_android_release_signing_test.py"],
        ),
        ReleaseGate(
            "Android release build helper tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/build_android_release_test.py"],
        ),
        ReleaseGate(
            "iOS wrapper verifier tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/verify_ios_wrapper_test.py"],
        ),
        ReleaseGate(
            "iOS release plan helper tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/plan_ios_release_test.py"],
        ),
        ReleaseGate(
            "iOS export options setup helper tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/setup_ios_export_options_test.py"],
        ),
        ReleaseGate(
            "Release documentation tests",
            mobile,
            [
                "flutter",
                "test",
                "test/release_docs_test.dart",
                "--concurrency=1",
                "--reporter",
                "compact",
            ],
        ),
        ReleaseGate(
            "Generate Flutter native wrappers",
            mobile,
            ["flutter", "create", "--platforms", "android,ios", "."],
        ),
        ReleaseGate(
            "Apply MaClaw native wrapper configuration",
            mobile,
            [sys.executable, "tool/configure_platforms.py"],
        ),
        ReleaseGate(
            "Android release signing verification",
            mobile,
            [sys.executable, "tool/verify_android_release_signing.py"],
        ),
        ReleaseGate(
            "iOS wrapper verification",
            mobile,
            [sys.executable, "tool/verify_ios_wrapper.py"],
        ),
        ReleaseGate(
            "Runtime boundary verification",
            mobile,
            [sys.executable, "tool/verify_runtime_boundary.py"],
        ),
        ReleaseGate("Flutter pub get", mobile, ["flutter", "pub", "get"]),
        ReleaseGate("Flutter analyze", mobile, ["flutter", "analyze"]),
        ReleaseGate("Flutter tests", mobile, ["flutter", "test", "--concurrency=1"]),
        ReleaseGate("Android debug APK", mobile, ["flutter", "build", "apk", "--debug"]),
    ]


def _quote_for_docs(arg: str) -> str:
    if any(char in arg for char in (" ", "|", "(", ")", "*")):
        escaped = arg.replace('"', '\\"')
        return f'"{escaped}"'
    return arg


def documented_command(gate: ReleaseGate, python_command: str = "python3") -> str:
    command = [
        python_command if index == 0 and arg == sys.executable else arg
        for index, arg in enumerate(gate.command)
    ]
    return " ".join(_quote_for_docs(arg) for arg in command)


def documented_commands(python_command: str = "python3") -> list[str]:
    return [
        documented_command(gate, python_command=python_command)
        for gate in release_gates()
    ]


def format_gate(gate: ReleaseGate, index: int | None = None, total: int | None = None) -> str:
    prefix = ""
    if index is not None and total is not None:
        width = max(2, len(str(total)))
        prefix = f"[{index:0{width}d}/{total}] "
    return f"{prefix}{gate.name}: ({gate.cwd}) {' '.join(gate.command)}"


def executable_command(command: list[str]) -> list[str]:
    if not command:
        return command
    executable = shutil.which(command[0])
    if executable is None:
        return command
    return [executable, *command[1:]]


def run_gate(gate: ReleaseGate, index: int | None = None, total: int | None = None) -> None:
    print(f"==> {format_gate(gate, index=index, total=total)}", flush=True)
    subprocess.run(executable_command(gate.command), cwd=gate.cwd, check=True)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Run the MaClaw Mobile automated release gates in order.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print the gate sequence without running commands.",
    )
    args = parser.parse_args(argv)

    gates = release_gates()
    if args.dry_run:
        for index, gate in enumerate(gates, start=1):
            print(format_gate(gate, index=index, total=len(gates)))
        return 0

    for index, gate in enumerate(gates, start=1):
        run_gate(gate, index=index, total=len(gates))
    print("All MaClaw Mobile automated release gates passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
