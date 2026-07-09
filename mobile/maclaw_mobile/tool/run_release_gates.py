from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

from release_evidence_commands import AUTOMATED_RELEASE_GATE_SUCCESS_LINE


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
                "TestMobile(ServiceRedemption|DesktopQRSession)|TestSameURLOriginHandlesDefaultPorts",
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
                "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown|TestResolveMobileBackendSSHHost|TestMobileServerProfilesFromSSHHosts|TestProcessMobileBackendSSHSession",
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
            "Release evidence command helper tests",
            mobile,
            [sys.executable, "-m", "unittest", "tool/release_evidence_commands_test.py"],
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
            "Manual release gate verification",
            mobile,
            [sys.executable, "tool/verify_manual_release_gates.py"],
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


def _append_output(log_lines: list[str], output: str) -> None:
    if output:
        log_lines.append(normalize_gate_output(output).rstrip())


def normalize_gate_output(output: str) -> str:
    return output.replace("鈭?Built", "[OK] Built").replace("√ Built", "[OK] Built")


def _print_and_log(line: str, log_lines: list[str]) -> None:
    print(line, flush=True)
    log_lines.append(line)


def _write_log(path: Path, log_lines: list[str], *, force: bool = False) -> None:
    if path.exists() and not force:
        raise FileExistsError(
            f"{path} already exists; pass --force to overwrite release-gates evidence log",
        )
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\n".join(log_lines).rstrip() + "\n", encoding="utf-8")


def run_gate(
    gate: ReleaseGate,
    index: int | None = None,
    total: int | None = None,
    log_lines: list[str] | None = None,
) -> None:
    log_lines = log_lines if log_lines is not None else []
    _print_and_log(f"==> {format_gate(gate, index=index, total=total)}", log_lines)
    completed = subprocess.run(
        executable_command(gate.command),
        cwd=gate.cwd,
        check=False,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if completed.stdout:
        completed_stdout = normalize_gate_output(completed.stdout)
        print(completed_stdout, end="")
        _append_output(log_lines, completed.stdout)
    if completed.stderr:
        completed_stderr = normalize_gate_output(completed.stderr)
        print(completed_stderr, end="", file=sys.stderr)
        _append_output(log_lines, completed.stderr)
    if completed.returncode != 0:
        raise subprocess.CalledProcessError(
            completed.returncode,
            completed.args,
            output=completed.stdout,
            stderr=completed.stderr,
        )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Run the MaClaw Mobile automated release gates in order.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print the gate sequence without running commands.",
    )
    parser.add_argument(
        "--log",
        type=Path,
        help="Optional path to write gate sequence and command output for QA evidence.",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Overwrite an existing release-gates evidence log.",
    )
    args = parser.parse_args(argv)

    gates = release_gates()
    log_lines: list[str] = []
    if args.dry_run:
        for index, gate in enumerate(gates, start=1):
            _print_and_log(format_gate(gate, index=index, total=len(gates)), log_lines)
        if args.log:
            try:
                _write_log(args.log, log_lines, force=args.force)
            except FileExistsError as exc:
                print(f"Release gates log write failed: {exc}", file=sys.stderr)
                return 1
        return 0

    exit_code = 0
    try:
        for index, gate in enumerate(gates, start=1):
            run_gate(gate, index=index, total=len(gates), log_lines=log_lines)
        _print_and_log(AUTOMATED_RELEASE_GATE_SUCCESS_LINE, log_lines)
    except subprocess.CalledProcessError as exc:
        exit_code = exc.returncode or 1

    if args.log:
        try:
            _write_log(args.log, log_lines, force=args.force)
        except FileExistsError as exc:
            print(f"Release gates log write failed: {exc}", file=sys.stderr)
            return 1
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
