from __future__ import annotations

import os
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

import setup_android_signing


class SetupAndroidSigningTest(unittest.TestCase):
    def make_root(self) -> Path:
        root = Path(self.tmp.name)
        (root / "android").mkdir()
        return root

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def env(self, store_file: str = "release.jks") -> dict[str, str]:
        return {
            setup_android_signing.ENV_STORE_FILE: store_file,
            setup_android_signing.ENV_STORE_PASSWORD: "store-secret",
            setup_android_signing.ENV_KEY_ALIAS: "maclaw-mobile",
            setup_android_signing.ENV_KEY_PASSWORD: "key-secret",
        }

    def test_config_from_env_requires_all_values(self) -> None:
        config, errors = setup_android_signing.config_from_env({})

        self.assertIsNone(config)
        self.assertEqual(4, len(errors))
        self.assertTrue(any(setup_android_signing.ENV_STORE_FILE in error for error in errors))

    def test_config_from_env_rejects_documented_placeholders(self) -> None:
        env = self.env("<release-signing-key.jks>")
        env[setup_android_signing.ENV_STORE_PASSWORD] = "<release-keystore-password>"
        env[setup_android_signing.ENV_KEY_ALIAS] = "<release-key-alias>"
        env[setup_android_signing.ENV_KEY_PASSWORD] = "<release-key-password>"

        config, errors = setup_android_signing.config_from_env(env)

        self.assertIsNone(config)
        self.assertEqual(4, len(errors))
        self.assertTrue(all("placeholder" in error for error in errors))
        self.assertTrue(
            any(setup_android_signing.ENV_STORE_PASSWORD in error for error in errors),
        )

    def test_validate_config_requires_existing_non_debug_store(self) -> None:
        root = self.make_root()
        config, errors = setup_android_signing.config_from_env(self.env("debug.keystore"))

        self.assertEqual([], errors)
        assert config is not None
        validation_errors = setup_android_signing.validate_config(root, config)

        self.assertTrue(any("does not exist" in error for error in validation_errors))
        self.assertTrue(any("debug keystore" in error for error in validation_errors))

    def test_write_key_properties_from_env_config(self) -> None:
        root = self.make_root()
        (root / "android" / "release.jks").write_text("keystore", encoding="utf-8")
        config, errors = setup_android_signing.config_from_env(self.env())

        self.assertEqual([], errors)
        assert config is not None
        self.assertEqual([], setup_android_signing.validate_config(root, config))
        target = setup_android_signing.write_key_properties(root, config)

        self.assertEqual(root / "android" / "key.properties", target)
        text = target.read_text(encoding="utf-8")
        self.assertIn("storeFile=release.jks", text)
        self.assertIn("keyAlias=maclaw-mobile", text)
        self.assertIn("storePassword=store-secret", text)

    def test_write_key_properties_refuses_overwrite_without_force(self) -> None:
        root = self.make_root()
        (root / "android" / "release.jks").write_text("keystore", encoding="utf-8")
        (root / "android" / "key.properties").write_text("existing", encoding="utf-8")
        config, _ = setup_android_signing.config_from_env(self.env())
        assert config is not None

        with self.assertRaises(FileExistsError):
            setup_android_signing.write_key_properties(root, config)

        setup_android_signing.write_key_properties(root, config, force=True)
        self.assertIn(
            "storeFile=release.jks",
            (root / "android" / "key.properties").read_text(encoding="utf-8"),
        )

    def test_main_writes_config_from_environment(self) -> None:
        root = self.make_root()
        (root / "android" / "release.jks").write_text("keystore", encoding="utf-8")
        stdout = StringIO()

        with patch.dict(os.environ, self.env(), clear=True), redirect_stdout(stdout):
            exit_code = setup_android_signing.main(["--root", str(root)])

        self.assertEqual(0, exit_code)
        self.assertIn("Wrote local Android signing config", stdout.getvalue())
        self.assertIn("Complete iOS ExportOptions if this QA scope includes iOS", stdout.getvalue())
        self.assertIn("python3 tool/qa_preflight.py", stdout.getvalue())
        self.assertIn("do not commit android/key.properties", stdout.getvalue())

    def test_main_reports_missing_environment_without_traceback(self) -> None:
        root = self.make_root()
        stderr = StringIO()

        with patch.dict(os.environ, {}, clear=True), redirect_stderr(stderr):
            exit_code = setup_android_signing.main(["--root", str(root)])

        self.assertEqual(1, exit_code)
        self.assertIn("Android signing setup cannot continue", stderr.getvalue())
        self.assertIn(setup_android_signing.ENV_KEY_PASSWORD, stderr.getvalue())

    def test_main_rejects_placeholder_environment_without_writing_config(self) -> None:
        root = self.make_root()
        (root / "android" / "release.jks").write_text("keystore", encoding="utf-8")
        env = self.env()
        env[setup_android_signing.ENV_KEY_PASSWORD] = "<release-key-password>"
        stderr = StringIO()

        with patch.dict(os.environ, env, clear=True), redirect_stderr(stderr):
            exit_code = setup_android_signing.main(["--root", str(root)])

        self.assertEqual(1, exit_code)
        self.assertIn("placeholder value", stderr.getvalue())
        self.assertFalse((root / "android" / "key.properties").exists())


if __name__ == "__main__":
    unittest.main()
