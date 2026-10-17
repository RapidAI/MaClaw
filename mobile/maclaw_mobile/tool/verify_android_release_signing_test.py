from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import verify_android_release_signing


VALID_GRADLE = '''
import java.util.Properties

plugins {
    id("com.android.application")
}

val maclawKeystorePropertiesFile = rootProject.file("key.properties")
val maclawKeystoreProperties = Properties()
val maclawReleaseSigningConfigured = maclawKeystorePropertiesFile.exists()

gradle.taskGraph.whenReady {
    val releaseTaskRequested = allTasks.any { task ->
        task.path.endsWith(":app:assembleRelease") || task.path.endsWith(":app:bundleRelease")
    }
}

android {
    signingConfigs {
        if (maclawReleaseSigningConfigured) {
            create("release") {
                keyAlias = maclawKeystoreProperties["keyAlias"] as String
                keyPassword = maclawKeystoreProperties["keyPassword"] as String
                storeFile = file(maclawKeystoreProperties["storeFile"] as String)
                storePassword = maclawKeystoreProperties["storePassword"] as String
            }
        }
    }

    buildTypes {
        release {
            if (maclawReleaseSigningConfigured) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }
}
'''


class VerifyAndroidReleaseSigningTest(unittest.TestCase):
    def _write_fixture(
        self,
        gradle: str,
        gitignore: str,
        key_properties_example: str | None = None,
    ) -> Path:
        root = Path(self.tmp.name) / 'mobile/maclaw_mobile'
        gradle_path = root / 'android/app/build.gradle.kts'
        gradle_path.parent.mkdir(parents=True)
        gradle_path.write_text(gradle, encoding='utf-8')
        if key_properties_example is None:
            key_properties_example = '\n'.join(
                verify_android_release_signing.REQUIRED_KEY_PROPERTIES_EXAMPLE_MARKERS
            )
        if key_properties_example:
            (root / 'android/key.properties.example').write_text(
                key_properties_example,
                encoding='utf-8',
            )
        (Path(self.tmp.name) / '.gitignore').write_text(gitignore, encoding='utf-8')
        return root

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def test_valid_signing_config_passes(self) -> None:
        root = self._write_fixture(
            VALID_GRADLE,
            '\n'.join(verify_android_release_signing.REQUIRED_GITIGNORE_MARKERS),
        )

        self.assertEqual([], verify_android_release_signing.verify_android_release_signing(root))

    def test_rejects_debug_signing_fallback(self) -> None:
        root = self._write_fixture(
            VALID_GRADLE + '\nsigningConfig = signingConfigs.getByName("debug")\n',
            '\n'.join(verify_android_release_signing.REQUIRED_GITIGNORE_MARKERS),
        )

        errors = verify_android_release_signing.verify_android_release_signing(root)

        self.assertTrue(any('debug' in error for error in errors))

    def test_rejects_flutter_template_gradle_comments(self) -> None:
        root = self._write_fixture(
            VALID_GRADLE
            + '\n// TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).\n'
            + '// For more information, see: https://flutter.dev/to/review-gradle-config.\n',
            '\n'.join(verify_android_release_signing.REQUIRED_GITIGNORE_MARKERS),
        )

        errors = verify_android_release_signing.verify_android_release_signing(root)

        self.assertTrue(any('unique Application ID' in error for error in errors))
        self.assertTrue(any('review-gradle-config' in error for error in errors))

    def test_rejects_missing_key_properties_loading(self) -> None:
        root = self._write_fixture(
            VALID_GRADLE.replace('rootProject.file("key.properties")', 'rootProject.file("debug.properties")'),
            '\n'.join(verify_android_release_signing.REQUIRED_GITIGNORE_MARKERS),
        )

        errors = verify_android_release_signing.verify_android_release_signing(root)

        self.assertTrue(any('key.properties' in error for error in errors))

    def test_rejects_missing_gitignore_secret_rules(self) -> None:
        root = self._write_fixture(VALID_GRADLE, 'mobile/maclaw_mobile/docs/qa-builds/*\n')

        errors = verify_android_release_signing.verify_android_release_signing(root)

        self.assertTrue(any('key.properties' in error for error in errors))
        self.assertTrue(any('*.jks' in error for error in errors))

    def test_rejects_missing_key_properties_example(self) -> None:
        root = self._write_fixture(
            VALID_GRADLE,
            '\n'.join(verify_android_release_signing.REQUIRED_GITIGNORE_MARKERS),
            key_properties_example='',
        )

        errors = verify_android_release_signing.verify_android_release_signing(root)

        self.assertTrue(any('key.properties.example' in error for error in errors))

    def test_rejects_unredacted_key_properties_example(self) -> None:
        root = self._write_fixture(
            VALID_GRADLE,
            '\n'.join(verify_android_release_signing.REQUIRED_GITIGNORE_MARKERS),
            key_properties_example=(
                'storeFile=release-signing-key.jks\n'
                'storePassword=real-password\n'
                'keyAlias=<release-key-alias>\n'
                'keyPassword=<release-key-password>\n'
            ),
        )

        errors = verify_android_release_signing.verify_android_release_signing(root)

        self.assertTrue(any('storePassword=<release-keystore-password>' in error for error in errors))

    def test_main_prints_success(self) -> None:
        root = self._write_fixture(
            VALID_GRADLE,
            '\n'.join(verify_android_release_signing.REQUIRED_GITIGNORE_MARKERS),
        )
        output = StringIO()

        with redirect_stdout(output):
            exit_code = verify_android_release_signing.main(['--root', str(root)])

        self.assertEqual(0, exit_code)
        self.assertIn('verified', output.getvalue())


if __name__ == '__main__':
    unittest.main()
