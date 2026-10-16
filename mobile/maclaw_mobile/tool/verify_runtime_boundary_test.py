from __future__ import annotations

import tempfile
import sys
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import verify_runtime_boundary


class VerifyRuntimeBoundaryTest(unittest.TestCase):
    def test_current_mobile_runtime_has_no_corelib_bridge(self) -> None:
        self.assertEqual([], verify_runtime_boundary.find_violations(
            verify_runtime_boundary.mobile_root()
        ))

    def test_flags_dart_ffi_corelib_bridge(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            lib = root / "lib"
            lib.mkdir()
            (lib / "bridge.dart").write_text(
                "import 'dart:ffi';\n"
                "final lib = DynamicLibrary.open('libcorelib.so');\n",
                encoding="utf-8",
            )

            violations = verify_runtime_boundary.find_violations(root)

        self.assertGreaterEqual(len(violations), 3)
        self.assertIn("dart ffi", {violation.rule.name for violation in violations})
        self.assertIn(
            "dynamic library", {violation.rule.name for violation in violations}
        )
        self.assertIn(
            "corelib reference", {violation.rule.name for violation in violations}
        )

    def test_flags_phone_local_ssh_dependency_in_lockfile(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "lib").mkdir()
            (root / "lib" / "main.dart").write_text("void main() {}\n", encoding="utf-8")
            (root / "pubspec.lock").write_text(
                "packages:\n  dartssh2:\n    version: 2.21.0\n",
                encoding="utf-8",
            )

            violations = verify_runtime_boundary.find_violations(root)

        self.assertEqual(1, len(violations))
        self.assertEqual("phone-local ssh dependency", violations[0].rule.name)
        self.assertEqual(Path("pubspec.lock"), violations[0].path)

    def test_flags_terminal_emulator_dependency_in_pubspec(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "lib").mkdir()
            (root / "lib" / "main.dart").write_text("void main() {}\n", encoding="utf-8")
            (root / "pubspec.yaml").write_text(
                "dependencies:\n  xterm: ^4.0.0\n",
                encoding="utf-8",
            )

            violations = verify_runtime_boundary.find_violations(root)

        self.assertEqual(1, len(violations))
        self.assertEqual("terminal emulator dependency", violations[0].rule.name)
        self.assertEqual(Path("pubspec.yaml"), violations[0].path)

    def test_flags_terminal_emulator_import_in_runtime_source(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            lib = root / "lib" / "features" / "servers"
            lib.mkdir(parents=True)
            (lib / "servers_screen.dart").write_text(
                "import 'package:xterm/xterm.dart';\n"
                "final terminal = Terminal(maxLines: 1000);\n",
                encoding="utf-8",
            )

            violations = verify_runtime_boundary.find_violations(root)

        self.assertGreaterEqual(len(violations), 1)
        self.assertEqual(
            {"terminal emulator dependency"},
            {violation.rule.name for violation in violations},
        )

    def test_flags_phone_side_ssh_credential_save_read_api(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            lib = root / "lib" / "core" / "storage"
            lib.mkdir(parents=True)
            (lib / "secure_vault.dart").write_text(
                "class SecureVault {\n"
                "  Future<void> saveServerPassword(String id, String password) async {}\n"
                "  Future<String?> readServerPrivateKey(String id) async => null;\n"
                "}\n",
                encoding="utf-8",
            )

            violations = verify_runtime_boundary.find_violations(root)

        self.assertEqual(2, len(violations))
        self.assertEqual(
            {"phone-side ssh credential api"},
            {violation.rule.name for violation in violations},
        )
        self.assertTrue(
            all(
                violation.path == Path("lib/core/storage/secure_vault.dart")
                for violation in violations
            )
        )

    def test_flags_mobile_only_official_service_surface_regressions(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            lib = root / "lib" / "features" / "auth"
            lib.mkdir(parents=True)
            (lib / "login_screen.dart").write_text(
                "final hubUrl = TextEditingController();\n"
                "final redemptionCode = TextEditingController();\n"
                "TextField(decoration: InputDecoration(labelText: 'Provider base URL'));\n"
                "TextField(decoration: InputDecoration(hintText: 'API key'));\n",
                encoding="utf-8",
            )

            violations = verify_runtime_boundary.find_violations(root)

        self.assertEqual(
            {
                "custom hub configuration surface",
                "redemption-code login surface",
                "arbitrary third-party llm settings surface",
            },
            {violation.rule.name for violation in violations},
        )
    def test_ignores_docs_and_tests_mentions(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            docs = root / "docs"
            tests = root / "test"
            docs.mkdir()
            tests.mkdir()
            (docs / "release.md").write_text(
                "MaClaw Mobile does not embed Go corelib.\n",
                encoding="utf-8",
            )
            (tests / "boundary_test.dart").write_text(
                "expect(doc, contains('corelib'));\n",
                encoding="utf-8",
            )

            self.assertEqual([], verify_runtime_boundary.find_violations(root))

    def test_help_describes_official_service_runtime_boundary(self) -> None:
        stdout = StringIO()

        with self.assertRaises(SystemExit) as raised:
            with redirect_stdout(stdout):
                verify_runtime_boundary.main(["--help"])

        self.assertEqual(0, raised.exception.code)
        help_text = " ".join(stdout.getvalue().split())
        self.assertIn("official-service runtime boundary", help_text)
        self.assertIn("phone-local SSH", help_text)
        self.assertIn("terminal emulator", help_text)
        self.assertIn("phone-side SSH credential APIs", help_text)
        self.assertIn("custom Hub URL", help_text)
        self.assertIn("redemption-code login", help_text)
        self.assertIn("third-party LLM provider/base URL/API-key fields", help_text)
    def test_main_can_write_success_log(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "lib").mkdir()
            (root / "lib" / "main.dart").write_text(
                "void main() {}\n",
                encoding="utf-8",
            )
            log_path = root / "logs" / "runtime-boundary.log"
            stdout = StringIO()

            with redirect_stdout(stdout):
                exit_code = verify_runtime_boundary.main(
                    ["--root", str(root), "--log", str(log_path)],
                )

            self.assertEqual(0, exit_code)
            self.assertIn("runtime boundary verified", stdout.getvalue())
            self.assertIn(
                "MaClaw Mobile runtime boundary verified.",
                log_path.read_text(encoding="utf-8"),
            )

    def test_main_refuses_to_overwrite_success_log_without_force(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "lib").mkdir()
            (root / "lib" / "main.dart").write_text(
                "void main() {}\n",
                encoding="utf-8",
            )
            log_path = root / "runtime-boundary.log"
            log_path.write_text("existing runtime evidence", encoding="utf-8")
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = verify_runtime_boundary.main(
                    ["--root", str(root), "--log", str(log_path)],
                )

            self.assertEqual(1, exit_code)
            self.assertEqual("existing runtime evidence", log_path.read_text(encoding="utf-8"))
            self.assertIn("pass --force to overwrite", stderr.getvalue())

            with redirect_stdout(StringIO()):
                exit_code = verify_runtime_boundary.main(
                    ["--root", str(root), "--log", str(log_path), "--force"],
                )

            self.assertEqual(0, exit_code)
            self.assertIn("runtime boundary verified", log_path.read_text(encoding="utf-8"))

    def test_main_can_write_violation_log(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "lib").mkdir()
            (root / "lib" / "bridge.dart").write_text(
                "import 'dart:ffi';\n",
                encoding="utf-8",
            )
            log_path = root / "logs" / "runtime-boundary.log"
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = verify_runtime_boundary.main(
                    ["--root", str(root), "--log", str(log_path)],
                )

            self.assertEqual(1, exit_code)
            self.assertIn("violations found", stderr.getvalue())
            text = log_path.read_text(encoding="utf-8")
            self.assertIn("violations found", text)
            self.assertIn("dart ffi", text)


if __name__ == "__main__":
    unittest.main()
