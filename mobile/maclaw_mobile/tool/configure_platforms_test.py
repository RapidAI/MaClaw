import plistlib
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import configure_platforms


class ConfigurePlatformsTest(unittest.TestCase):
    def test_ios_usage_descriptions_are_readable_chinese(self) -> None:
        plist: dict[str, object] = {}

        configure_platforms.apply_ios_usage_descriptions(plist)

        self.assertEqual(
            plist["NSCameraUsageDescription"],
            "\u7528\u4e8e\u62cd\u7167\u63d0\u95ee\u548c\u5bfc\u5165\u56fe\u7247\u6587\u6863\u3002",
        )
        self.assertEqual(
            plist["NSMicrophoneUsageDescription"],
            "\u7528\u4e8e\u8bed\u97f3\u63d0\u95ee\u3002",
        )
        self.assertEqual(
            plist["NSSpeechRecognitionUsageDescription"],
            "\u7528\u4e8e\u5c06\u8bed\u97f3\u63d0\u95ee\u8f6c\u6210\u6587\u5b57\u3002",
        )
        self.assertEqual(
            plist["NSPhotoLibraryUsageDescription"],
            "\u7528\u4e8e\u4ece\u76f8\u518c\u5bfc\u5165\u56fe\u7247\u6216\u622a\u56fe\u3002",
        )
        self.assertEqual(
            plist["NSLocalNetworkUsageDescription"],
            "\u7528\u4e8e\u8fde\u63a5\u672c\u5730\u6216\u5185\u7f51\u670d\u52a1\u5668\u8fdb\u884c SSH \u5e94\u6025\u7ef4\u62a4\u3002",
        )

    def test_ios_usage_descriptions_keep_existing_values(self) -> None:
        plist: dict[str, object] = {
            "NSCameraUsageDescription": "Custom camera reason.",
        }

        configure_platforms.apply_ios_usage_descriptions(plist)

        self.assertEqual(
            plist["NSCameraUsageDescription"],
            "Custom camera reason.",
        )

    def test_ios_usage_descriptions_repair_corrupted_values(self) -> None:
        plist: dict[str, object] = {
            "NSCameraUsageDescription": "\u9422\u3124\u7c32?/string>",
            "NSMicrophoneUsageDescription": "",
            "NSPhotoLibraryUsageDescription": object(),
        }

        configure_platforms.apply_ios_usage_descriptions(plist)

        self.assertEqual(
            plist["NSCameraUsageDescription"],
            "\u7528\u4e8e\u62cd\u7167\u63d0\u95ee\u548c\u5bfc\u5165\u56fe\u7247\u6587\u6863\u3002",
        )
        self.assertEqual(
            plist["NSMicrophoneUsageDescription"],
            "\u7528\u4e8e\u8bed\u97f3\u63d0\u95ee\u3002",
        )
        self.assertEqual(
            plist["NSPhotoLibraryUsageDescription"],
            "\u7528\u4e8e\u4ece\u76f8\u518c\u5bfc\u5165\u56fe\u7247\u6216\u622a\u56fe\u3002",
        )

    def test_configure_ios_sets_readable_runner_bundle_names(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            runner = root / "ios/Runner"
            runner.mkdir(parents=True)
            info = runner / "Info.plist"
            with info.open("wb") as fh:
                plistlib.dump(
                    {
                        "CFBundleDisplayName": "maclaw_mobile",
                        "CFBundleName": "maclaw_mobile",
                    },
                    fh,
                )
            old_root = configure_platforms.ROOT
            configure_platforms.ROOT = root
            try:
                configure_platforms.configure_ios()
            finally:
                configure_platforms.ROOT = old_root

            plist = plistlib.loads(info.read_bytes())
            self.assertEqual(plist["CFBundleDisplayName"], "MaClaw Mobile")
            self.assertEqual(plist["CFBundleName"], "MaClaw Mobile")

    def test_android_gradle_uses_official_package_id(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            gradle = root / "android/app/build.gradle.kts"
            gradle.parent.mkdir(parents=True)
            gradle.write_text(
                'android {\n'
                '    namespace = "com.example.maclaw_mobile"\n'
                '    compileOptions {\n'
                '        sourceCompatibility = JavaVersion.VERSION_17\n'
                '        targetCompatibility = JavaVersion.VERSION_17\n'
                '    }\n'
                '    defaultConfig {\n'
                '        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).\n'
                '        applicationId = "com.example.maclaw_mobile"\n'
                '        // You can update the following values to match your application needs.\n'
                '        // For more information, see: https://flutter.dev/to/review-gradle-config.\n'
                '    }\n'
                '    buildTypes {\n'
                '        release {\n'
                '            // TODO: Add your own signing config for the release build.\n'
                '            // Signing with the debug keys for now, so `flutter run --release` works.\n'
                '            signingConfig = signingConfigs.getByName("debug")\n'
                '        }\n'
                '    }\n'
                '}\n',
                encoding="utf-8",
            )
            old_root = configure_platforms.ROOT
            configure_platforms.ROOT = root
            try:
                configure_platforms.configure_android_gradle()
            finally:
                configure_platforms.ROOT = old_root

            text = gradle.read_text(encoding="utf-8")
            self.assertIn('namespace = "top.mypapers.maclaw.mobile"', text)
            self.assertIn('applicationId = "top.mypapers.maclaw.mobile"', text)
            self.assertIn("isCoreLibraryDesugaringEnabled = true", text)
            self.assertIn(
                'coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.1.5")',
                text,
            )
            self.assertIn("val maclawKeystorePropertiesFile", text)
            self.assertIn('rootProject.file("key.properties")', text)
            self.assertIn('create("release")', text)
            self.assertIn("maclawReleaseSigningConfigured", text)
            self.assertIn("assembleRelease", text)
            self.assertIn("bundleRelease", text)
            self.assertNotIn('signingConfigs.getByName("debug")', text)
            self.assertNotIn("com.example.maclaw_mobile", text)
            self.assertNotIn("Specify your own unique Application ID", text)
            self.assertNotIn("review-gradle-config", text)

    def test_android_release_signing_template_is_idempotent(self) -> None:
        source = (
            "import java.util.Properties\n\n"
            "val maclawKeystorePropertiesFile = rootProject.file(\"key.properties\")\n"
            "val maclawKeystoreProperties = Properties()\n"
            "val maclawReleaseSigningConfigured = maclawKeystorePropertiesFile.exists()\n"
            "if (maclawReleaseSigningConfigured) {\n"
            "    maclawKeystorePropertiesFile.inputStream().use { maclawKeystoreProperties.load(it) }\n"
            "}\n\n"
            "gradle.taskGraph.whenReady {\n"
            "    val releaseTaskRequested = allTasks.any { task ->\n"
            "        task.path.endsWith(\":app:assembleRelease\") || task.path.endsWith(\":app:bundleRelease\")\n"
            "    }\n"
            "    if (releaseTaskRequested && !maclawReleaseSigningConfigured) {\n"
            "        throw GradleException(\n"
            "            \"MaClaw Mobile release signing requires android/key.properties with storeFile, storePassword, keyAlias, and keyPassword.\"\n"
            "        )\n"
            "    }\n"
            "}\n\n"
            "plugins { id(\"com.android.application\") }\n\n"
            "android {\n"
            "    namespace = \"top.mypapers.maclaw.mobile\"\n"
            "    signingConfigs {\n"
            "        if (maclawReleaseSigningConfigured) {\n"
            "            create(\"release\") {\n"
            "                keyAlias = maclawKeystoreProperties[\"keyAlias\"] as String\n"
            "                keyPassword = maclawKeystoreProperties[\"keyPassword\"] as String\n"
            "                storeFile = file(maclawKeystoreProperties[\"storeFile\"] as String)\n"
            "                storePassword = maclawKeystoreProperties[\"storePassword\"] as String\n"
            "            }\n"
            "        }\n"
            "    }\n\n"
            "    buildTypes {\n"
            "        release {\n"
            "            if (maclawReleaseSigningConfigured) {\n"
            "                signingConfig = signingConfigs.getByName(\"release\")\n"
            "            }\n"
            "        }\n"
            "    }\n"
            "}\n"
        )

        configured = configure_platforms.configure_android_release_signing(source)

        self.assertEqual(1, configured.count("val maclawKeystorePropertiesFile"))
        self.assertEqual(1, configured.count("signingConfigs {"))
        self.assertEqual(1, configured.count("buildTypes {"))

    def test_android_root_gradle_sets_plugin_jvm_targets(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            gradle = root / "android/build.gradle.kts"
            gradle.parent.mkdir(parents=True)
            gradle.write_text(
                "allprojects { repositories { google(); mavenCentral() } }\n\n"
                "subprojects {\n"
                "    val newSubprojectBuildDir: Directory = newBuildDir.dir(project.name)\n"
                "}\n",
                encoding="utf-8",
            )
            old_root = configure_platforms.ROOT
            configure_platforms.ROOT = root
            try:
                configure_platforms.configure_android_root_gradle()
            finally:
                configure_platforms.ROOT = old_root

            text = gradle.read_text(encoding="utf-8")
            self.assertIn("com.android.build.gradle.AppExtension", text)
            self.assertIn("com.android.build.gradle.LibraryExtension", text)
            self.assertIn("JvmTarget.JVM_17", text)

    def test_android_gradle_properties_disable_kotlin_incremental_cache(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            props = root / "android/gradle.properties"
            props.parent.mkdir(parents=True)
            props.write_text("org.gradle.jvmargs=-Xmx8G\n", encoding="utf-8")
            old_root = configure_platforms.ROOT
            configure_platforms.ROOT = root
            try:
                configure_platforms.configure_android_gradle_properties()
            finally:
                configure_platforms.ROOT = old_root

            text = props.read_text(encoding="utf-8")
            self.assertIn("kotlin.incremental=false", text)
            self.assertIn("kotlin.jvm.target.validation.mode=ignore", text)

    def test_android_manifest_gets_permissions_deep_link_and_share_entries(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            manifest = root / "android/app/src/main/AndroidManifest.xml"
            manifest.parent.mkdir(parents=True)
            manifest.write_text(
                '<?xml version="1.0" encoding="utf-8"?>\n'
                '<manifest xmlns:android="http://schemas.android.com/apk/res/android">\n'
                '  <application android:label="maclaw_mobile">\n'
                '    <activity android:name=".MainActivity" android:exported="false" />\n'
                "  </application>\n"
                "</manifest>\n",
                encoding="utf-8",
            )
            old_root = configure_platforms.ROOT
            configure_platforms.ROOT = root
            try:
                configure_platforms.configure_android()
            finally:
                configure_platforms.ROOT = old_root

            text = manifest.read_text(encoding="utf-8")
            self.assertIn('android:label="MaClaw Mobile"', text)
            self.assertIn('android:exported="true"', text)
            self.assertIn('android:name="android.permission.INTERNET"', text)
            self.assertIn('android:name="android.permission.CAMERA"', text)
            self.assertIn('android:name="android.permission.RECORD_AUDIO"', text)
            self.assertIn('android:name="android.permission.POST_NOTIFICATIONS"', text)
            self.assertIn('android:scheme="maclaw"', text)
            self.assertIn('android:name="android.intent.action.SEND"', text)
            self.assertIn('android:name="android.intent.action.SEND_MULTIPLE"', text)
            self.assertIn('android:mimeType="application/pdf"', text)
            self.assertIn(
                'android:mimeType="application/vnd.openxmlformats-officedocument.wordprocessingml.document"',
                text,
            )
            self.assertIn(
                'android:mimeType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"',
                text,
            )

    def test_android_main_activity_uses_official_package_id(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            activity = root / "android/app/src/main/kotlin/com/example/maclaw_mobile/MainActivity.kt"
            activity.parent.mkdir(parents=True)
            activity.write_text(
                "package com.example.maclaw_mobile\n\n"
                "import io.flutter.embedding.android.FlutterActivity\n\n"
                "class MainActivity : FlutterActivity()\n",
                encoding="utf-8",
            )
            old_root = configure_platforms.ROOT
            configure_platforms.ROOT = root
            try:
                configure_platforms.configure_android_main_activity()
            finally:
                configure_platforms.ROOT = old_root

            target = root / "android/app/src/main/kotlin/top/mypapers/maclaw/mobile/MainActivity.kt"
            self.assertTrue(target.exists())
            self.assertFalse(activity.exists())
            self.assertIn(
                "package top.mypapers.maclaw.mobile",
                target.read_text(encoding="utf-8"),
            )

    def test_removes_generated_flutter_template_widget_test(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            widget_test = root / "test/widget_test.dart"
            widget_test.parent.mkdir(parents=True)
            widget_test.write_text(
                "import 'package:flutter_test/flutter_test.dart';\n"
                "import 'package:maclaw_mobile/main.dart';\n\n"
                "void main() {\n"
                "  testWidgets('Counter increments smoke test', (tester) async {\n"
                "    await tester.pumpWidget(const MyApp());\n"
                "  });\n"
                "}\n",
                encoding="utf-8",
            )
            old_root = configure_platforms.ROOT
            configure_platforms.ROOT = root
            try:
                configure_platforms.remove_flutter_template_widget_test()
            finally:
                configure_platforms.ROOT = old_root

            self.assertFalse(widget_test.exists())

    def test_keeps_custom_widget_test(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            widget_test = root / "test/widget_test.dart"
            widget_test.parent.mkdir(parents=True)
            widget_test.write_text(
                "import 'package:flutter_test/flutter_test.dart';\n\n"
                "void main() {\n"
                "  testWidgets('MaClaw shell smoke', (tester) async {});\n"
                "}\n",
                encoding="utf-8",
            )
            old_root = configure_platforms.ROOT
            configure_platforms.ROOT = root
            try:
                configure_platforms.remove_flutter_template_widget_test()
            finally:
                configure_platforms.ROOT = old_root

            self.assertTrue(widget_test.exists())

    def test_ios_project_uses_official_bundle_id_and_app_group(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            project = root / "ios/Runner.xcodeproj/project.pbxproj"
            project.parent.mkdir(parents=True)
            project.write_text(
                "PRODUCT_BUNDLE_IDENTIFIER = com.example.maclawMobile;\n"
                "PRODUCT_BUNDLE_IDENTIFIER = com.example.maclawMobile.RunnerTests;\n",
                encoding="utf-8",
            )
            old_root = configure_platforms.ROOT
            configure_platforms.ROOT = root
            try:
                configure_platforms.configure_ios_project_settings()
            finally:
                configure_platforms.ROOT = old_root

            text = project.read_text(encoding="utf-8")
            self.assertIn("PRODUCT_BUNDLE_IDENTIFIER = top.mypapers.maclaw.mobile;", text)
            self.assertIn(
                "PRODUCT_BUNDLE_IDENTIFIER = top.mypapers.maclaw.mobile.RunnerTests;",
                text,
            )
            self.assertIn("CUSTOM_GROUP_ID = group.top.mypapers.maclaw.mobile;", text)

    def test_ios_entitlement_payload_uses_app_group_variable(self) -> None:
        payload = configure_platforms.entitlement_payload()

        self.assertEqual(
            payload["com.apple.security.application-groups"],
            ["$(CUSTOM_GROUP_ID)"],
        )

    def test_ios_runner_tests_are_generated(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tests_dir = root / "ios/RunnerTests"
            tests_dir.mkdir(parents=True)
            (tests_dir / "RunnerTests.swift").write_text(
                "func testExample() {}\n",
                encoding="utf-8",
            )
            old_root = configure_platforms.ROOT
            configure_platforms.ROOT = root
            try:
                configure_platforms.configure_ios_runner_tests()
            finally:
                configure_platforms.ROOT = old_root

            text = (tests_dir / "RunnerTests.swift").read_text(encoding="utf-8")
            self.assertIn("testMaClawMobileBundleConfiguration", text)
            self.assertIn(
                "top.mypapers.maclaw.mobile.RunnerTests",
                text,
            )
            self.assertNotIn("testExample", text)

    def test_ios_share_extension_files_are_generated(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            project = root / "ios/Runner.xcodeproj/project.pbxproj"
            project.parent.mkdir(parents=True)
            project.write_text("", encoding="utf-8")
            old_root = configure_platforms.ROOT
            configure_platforms.ROOT = root
            try:
                configure_platforms.configure_ios_share_extension()
            finally:
                configure_platforms.ROOT = old_root

            share_dir = root / "ios/ShareExtension"
            info = plistlib.loads((share_dir / "Info.plist").read_bytes())
            entitlements = plistlib.loads(
                (share_dir / "ShareExtension.entitlements").read_bytes()
            )
            controller = (share_dir / "ShareViewController.swift").read_text(
                encoding="utf-8"
            )
            self.assertEqual(info["AppGroupId"], "$(CUSTOM_GROUP_ID)")
            extension = info["NSExtension"]
            self.assertEqual(
                extension["NSExtensionPointIdentifier"],
                "com.apple.share-services",
            )
            activation = extension["NSExtensionAttributes"][
                "NSExtensionActivationRule"
            ]
            self.assertEqual(
                extension["NSExtensionAttributes"]["PHSupportedMediaTypes"],
                ["Image"],
            )
            self.assertTrue(activation["NSExtensionActivationSupportsText"])
            self.assertEqual(
                activation["NSExtensionActivationSupportsWebURLWithMaxCount"],
                1,
            )
            self.assertEqual(
                activation["NSExtensionActivationSupportsImageWithMaxCount"],
                20,
            )
            self.assertEqual(
                activation["NSExtensionActivationSupportsFileWithMaxCount"],
                10,
            )
            self.assertEqual(
                entitlements["com.apple.security.application-groups"],
                ["$(CUSTOM_GROUP_ID)"],
            )
            self.assertIn("RSIShareViewController", controller)
            self.assertIn(
                "MaClaw Mobile Share Extension files are generated",
                project.read_text(encoding="utf-8"),
            )


if __name__ == "__main__":
    unittest.main()
