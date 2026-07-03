from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

import run_release_gates


def assert_in_order(test: unittest.TestCase, text: str, expected_parts: list[str]) -> None:
    cursor = -1
    for part in expected_parts:
        index = text.find(part, cursor + 1)
        test.assertNotEqual(
            -1,
            index,
            f"{part!r} should appear after index {cursor}",
        )
        cursor = index


class RunReleaseGatesTest(unittest.TestCase):
    def test_release_gates_match_documented_order(self) -> None:
        gates = run_release_gates.release_gates()

        self.assertEqual(
            [
                "Go mobile API",
                "Go mobile HubCenter discovery",
                "Go mobile GUI and digital employee",
                "Platform configuration tests",
                "QA record validator tests",
                "QA build record scaffold tests",
                "QA records directory validator tests",
                "QA build record report tests",
                "QA release evidence link helper tests",
                "QA preflight helper tests",
                "Android signing setup helper tests",
                "Release status report helper tests",
                "Release handoff helper tests",
                "QA records directory validation",
                "Runtime boundary verifier tests",
                "Release gate runner tests",
                "Debug APK evidence verifier tests",
                "Debug APK evidence updater tests",
                "Signed artifact evidence helper tests",
                "Manual release gate parity tests",
                "Final release evidence verifier tests",
                "Android release signing verifier tests",
                "Android release build helper tests",
                "iOS wrapper verifier tests",
                "iOS release plan helper tests",
                "iOS export options setup helper tests",
                "Release documentation tests",
                "Generate Flutter native wrappers",
                "Apply MaClaw native wrapper configuration",
                "Android release signing verification",
                "iOS wrapper verification",
                "Runtime boundary verification",
                "Flutter pub get",
                "Flutter analyze",
                "Flutter tests",
                "Android debug APK",
            ],
            [gate.name for gate in gates],
        )

    def test_go_gates_run_from_repo_root_and_flutter_gates_from_mobile_root(self) -> None:
        gates = run_release_gates.release_gates()
        root = run_release_gates.repo_root()
        mobile = run_release_gates.mobile_root()

        self.assertEqual(root, gates[0].cwd)
        self.assertEqual(root, gates[1].cwd)
        self.assertEqual(root, gates[2].cwd)
        for gate in gates[3:]:
            self.assertEqual(mobile, gate.cwd)

    def test_commands_include_release_critical_checks(self) -> None:
        commands = run_release_gates.documented_commands()

        for expected in [
            'go test ./hub/internal/httpapi -run "TestMobile.*" -count=1',
            'go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)" -count=1',
            'go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown" -count=1',
            "python3 -m unittest tool/configure_platforms_test.py",
            "python3 -m unittest tool/validate_qa_build_record_test.py",
            "python3 -m unittest tool/create_qa_build_record_test.py",
            "python3 -m unittest tool/validate_qa_build_records_dir_test.py",
            "python3 -m unittest tool/qa_build_record_report_test.py",
            "python3 -m unittest tool/qa_release_evidence_links_test.py",
            "python3 -m unittest tool/qa_preflight_test.py",
            "python3 -m unittest tool/setup_android_signing_test.py",
            "python3 -m unittest tool/release_status_report_test.py",
            "python3 -m unittest tool/release_handoff_test.py",
            "python3 tool/validate_qa_build_records_dir.py docs/qa-builds",
            "python3 -m unittest tool/verify_runtime_boundary_test.py",
            "python3 -m unittest tool/run_release_gates_test.py",
            "python3 -m unittest tool/verify_debug_apk_evidence_test.py",
            "python3 -m unittest tool/update_debug_apk_evidence_test.py",
            "python3 -m unittest tool/signed_artifact_evidence_test.py",
            "python3 -m unittest tool/verify_manual_release_gates_test.py",
            "python3 -m unittest tool/verify_final_release_evidence_test.py",
            "python3 -m unittest tool/verify_android_release_signing_test.py",
            "python3 -m unittest tool/build_android_release_test.py",
            "python3 -m unittest tool/verify_ios_wrapper_test.py",
            "python3 -m unittest tool/plan_ios_release_test.py",
            "python3 -m unittest tool/setup_ios_export_options_test.py",
            "flutter test test/release_docs_test.dart --concurrency=1 --reporter compact",
            "flutter create --platforms android,ios .",
            "python3 tool/configure_platforms.py",
            "python3 tool/verify_android_release_signing.py",
            "python3 tool/verify_ios_wrapper.py",
            "python3 tool/verify_runtime_boundary.py",
            "flutter pub get",
            "flutter analyze",
            "flutter test --concurrency=1",
            "flutter build apk --debug",
        ]:
            self.assertTrue(
                any(expected in command for command in commands),
                f"missing command containing {expected!r}",
            )

    def test_dry_run_prints_gate_sequence_without_running_commands(self) -> None:
        output = StringIO()

        with redirect_stdout(output):
            self.assertEqual(0, run_release_gates.main(["--dry-run"]))

        text = output.getvalue()
        self.assertIn("Go mobile API", text)
        self.assertIn("Android debug APK", text)
        self.assertIn("flutter build apk --debug", text)

    def test_dry_run_prints_numbered_gate_sequence(self) -> None:
        output = StringIO()

        with redirect_stdout(output):
            self.assertEqual(0, run_release_gates.main(["--dry-run"]))

        lines = [line for line in output.getvalue().splitlines() if line.strip()]
        gate_count = len(run_release_gates.release_gates())
        self.assertEqual(gate_count, len(lines))
        self.assertTrue(lines[0].startswith(f"[01/{gate_count}] Go mobile API:"))
        self.assertTrue(
            lines[-1].startswith(f"[{gate_count:02d}/{gate_count}] Android debug APK:")
        )

    def test_dry_run_can_write_log_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            log_path = Path(tmp) / "release-gates.log"
            output = StringIO()

            with redirect_stdout(output):
                self.assertEqual(0, run_release_gates.main(["--dry-run", "--log", str(log_path)]))

            text = log_path.read_text(encoding="utf-8")
            self.assertIn("[01/", text)
            self.assertIn("Go mobile API", text)
            self.assertIn("Android debug APK", text)

    def test_dry_run_refuses_to_overwrite_log_without_force(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            log_path = Path(tmp) / "release-gates.log"
            log_path.write_text("existing release gate evidence", encoding="utf-8")
            output = StringIO()
            error = StringIO()

            with redirect_stdout(output), redirect_stderr(error):
                self.assertEqual(
                    1,
                    run_release_gates.main(["--dry-run", "--log", str(log_path)]),
                )

            self.assertEqual(
                "existing release gate evidence",
                log_path.read_text(encoding="utf-8"),
            )
            self.assertIn("pass --force to overwrite", error.getvalue())

            with redirect_stdout(StringIO()):
                self.assertEqual(
                    0,
                    run_release_gates.main(
                        ["--dry-run", "--log", str(log_path), "--force"],
                    ),
                )

            self.assertIn("Go mobile API", log_path.read_text(encoding="utf-8"))

    def test_run_can_write_log_file_with_command_output(self) -> None:
        gate = run_release_gates.ReleaseGate(
            "Stub gate",
            Path.cwd(),
            ["stub-command"],
        )
        completed = run_release_gates.subprocess.CompletedProcess(
            args=["stub-command"],
            returncode=0,
            stdout="stub stdout\n",
            stderr="stub stderr\n",
        )
        with tempfile.TemporaryDirectory() as tmp:
            log_path = Path(tmp) / "nested" / "release-gates.log"
            output = StringIO()
            error = StringIO()
            with patch("run_release_gates.release_gates", return_value=[gate]):
                with patch("run_release_gates.subprocess.run", return_value=completed):
                    with redirect_stdout(output), redirect_stderr(error):
                        self.assertEqual(0, run_release_gates.main(["--log", str(log_path)]))

            text = log_path.read_text(encoding="utf-8")
            self.assertIn("==> [01/1] Stub gate:", text)
            self.assertIn("stub stdout", text)
            self.assertIn("stub stderr", text)
            self.assertIn("All MaClaw Mobile automated release gates passed.", text)

    def test_executable_command_resolves_windows_batch_shims(self) -> None:
        with patch("run_release_gates.shutil.which", return_value=r"D:\flutter\bin\flutter.BAT"):
            self.assertEqual(
                [r"D:\flutter\bin\flutter.BAT", "test", "--concurrency=1"],
                run_release_gates.executable_command(
                    ["flutter", "test", "--concurrency=1"]
                ),
            )

    def test_executable_command_keeps_unknown_commands_readable(self) -> None:
        with patch("run_release_gates.shutil.which", return_value=None):
            self.assertEqual(
                ["flutter", "test"],
                run_release_gates.executable_command(["flutter", "test"]),
            )

    def test_ci_workflow_covers_release_gate_commands_and_artifact_upload(self) -> None:
        workflow = (
            run_release_gates.repo_root() / ".github" / "workflows" / "maclaw-mobile.yml"
        ).read_text(encoding="utf-8")

        for expected in [
            *run_release_gates.documented_commands(),
            "actions/upload-artifact@v4",
            "maclaw-mobile-debug-apk",
            "mobile/maclaw_mobile/build/app/outputs/flutter-apk/app-debug.apk",
        ]:
            self.assertIn(expected, workflow)

    def test_ci_workflow_runs_release_gate_commands_in_runner_order(self) -> None:
        workflow = (
            run_release_gates.repo_root() / ".github" / "workflows" / "maclaw-mobile.yml"
        ).read_text(encoding="utf-8")

        assert_in_order(self, workflow, run_release_gates.documented_commands())

    def test_release_docs_run_gate_commands_in_runner_order(self) -> None:
        mobile = run_release_gates.mobile_root()
        checklist = (mobile / "docs" / "release_checklist.md").read_text(encoding="utf-8")
        evidence = (mobile / "docs" / "release_evidence.md").read_text(encoding="utf-8")
        commands = run_release_gates.documented_commands()

        assert_in_order(self, checklist, commands)
        assert_in_order(self, evidence, commands)


if __name__ == "__main__":
    unittest.main()
