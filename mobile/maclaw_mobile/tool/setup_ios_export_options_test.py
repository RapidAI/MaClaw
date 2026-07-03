from __future__ import annotations

import plistlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import setup_ios_export_options


class SetupIOSExportOptionsTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def root(self) -> Path:
        root = Path(self.tmp.name)
        (root / "ios").mkdir(exist_ok=True)
        return root

    def test_export_options_payload_normalizes_team_id(self) -> None:
        payload = setup_ios_export_options.export_options_payload(
            "abcd123456",
            "development",
        )

        self.assertEqual("ABCD123456", payload["teamID"])
        self.assertEqual("development", payload["method"])
        self.assertEqual("automatic", payload["signingStyle"])

    def test_export_options_payload_rejects_unknown_method(self) -> None:
        with self.assertRaises(ValueError):
            setup_ios_export_options.export_options_payload("ABCD123456", "invalid")

    def test_write_export_options_creates_plist(self) -> None:
        target = setup_ios_export_options.write_export_options(
            self.root(),
            team_id="ABCD123456",
            export_method="ad-hoc",
        )

        with target.open("rb") as handle:
            payload = plistlib.load(handle)
        self.assertEqual("ad-hoc", payload["method"])
        self.assertEqual("ABCD123456", payload["teamID"])

    def test_write_export_options_refuses_overwrite_without_force(self) -> None:
        root = self.root()
        setup_ios_export_options.write_export_options(
            root,
            team_id="ABCD123456",
            export_method="development",
        )

        with self.assertRaises(FileExistsError):
            setup_ios_export_options.write_export_options(
                root,
                team_id="ABCD123456",
                export_method="app-store",
            )

        setup_ios_export_options.write_export_options(
            root,
            team_id="ABCD123456",
            export_method="app-store",
            force=True,
        )
        with (root / "ios" / "ExportOptions.plist").open("rb") as handle:
            self.assertEqual("app-store", plistlib.load(handle)["method"])

    def test_main_writes_export_options(self) -> None:
        stdout = StringIO()

        with redirect_stdout(stdout):
            exit_code = setup_ios_export_options.main(
                [
                    "--root",
                    str(self.root()),
                    "--team-id",
                    "ABCD123456",
                    "--export-method",
                    "enterprise",
                ],
            )

        self.assertEqual(0, exit_code)
        self.assertIn("Wrote local iOS export options", stdout.getvalue())
        self.assertIn(
            "python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method enterprise",
            stdout.getvalue(),
        )

    def test_main_rejects_invalid_team_id_without_traceback(self) -> None:
        stderr = StringIO()

        with redirect_stderr(stderr):
            with self.assertRaises(SystemExit) as raised:
                setup_ios_export_options.main(
                    ["--root", str(self.root()), "--team-id", "too-short"],
                )

        self.assertEqual(2, raised.exception.code)
        self.assertIn("team id", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
