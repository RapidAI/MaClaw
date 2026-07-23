#!/usr/bin/env python3
"""Apply MaClaw Mobile native settings after `flutter create`.

The repository keeps Android/iOS wrappers deterministic. Run this script after
`flutter create --platforms android,ios .` to restore the MaClaw package ids,
permissions, share entry points, URL schemes, and iOS Share Extension files.
"""

from __future__ import annotations

import plistlib
import re
import xml.etree.ElementTree as ET
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
ANDROID_NS = "http://schemas.android.com/apk/res/android"
ET.register_namespace("android", ANDROID_NS)

IOS_BUNDLE_ID = "top.mypapers.maclaw.mobile"
IOS_TEST_BUNDLE_ID = f"{IOS_BUNDLE_ID}.RunnerTests"
IOS_APP_GROUP_ID = f"group.{IOS_BUNDLE_ID}"
IOS_SHARE_EXTENSION_NAME = "ShareExtension"
ANDROID_PACKAGE_ID = IOS_BUNDLE_ID

IOS_USAGE_DESCRIPTIONS = {
    "NSCameraUsageDescription": "\u7528\u4e8e\u62cd\u7167\u63d0\u95ee\u548c\u5bfc\u5165\u56fe\u7247\u6587\u6863\u3002",
    "NSMicrophoneUsageDescription": "\u7528\u4e8e\u8bed\u97f3\u63d0\u95ee\u4e0e\u4f1a\u8bae\u5f55\u97f3\uff0c\u4f1a\u8bae\u5f55\u97f3\u53ef\u5728\u8bbe\u5907\u9501\u5c4f\u6216\u5207\u6362\u5e94\u7528\u540e\u7ee7\u7eed\u3002",
    "NSSpeechRecognitionUsageDescription": "\u7528\u4e8e\u5c06\u8bed\u97f3\u63d0\u95ee\u8f6c\u6210\u6587\u5b57\u3002",
    "NSPhotoLibraryUsageDescription": "\u7528\u4e8e\u4ece\u76f8\u518c\u5bfc\u5165\u56fe\u7247\u6216\u622a\u56fe\u3002",
    "NSLocalNetworkUsageDescription": "\u7528\u4e8e\u53d1\u73b0 MaClaw \u5b98\u65b9 Hub \u5e76\u540c\u6b65 GUI/agent \u7ba1\u7406\u7684\u540e\u53f0 SSH \u4f1a\u8bdd\u72b6\u6001\u3002",
}

IOS_BACKGROUND_MODES = ("audio",)

IOS_CORRUPT_USAGE_MARKERS = [
    "?/string>",
    "\u9422",
    "\u95bb",
    "\ufffd",
]

IOS_RUNNER_TESTS_SWIFT = """import Flutter
import UIKit
import XCTest

class RunnerTests: XCTestCase {

  func testMaClawMobileBundleConfiguration() {
    let bundle = Bundle(for: RunnerTests.self)

    XCTAssertEqual(bundle.bundleIdentifier, "top.mypapers.maclaw.mobile.RunnerTests")
  }

}
"""

ANDROID_PERMISSIONS = [
    ("android.permission.INTERNET", {}),
    ("android.permission.CAMERA", {}),
    ("android.permission.RECORD_AUDIO", {}),
    ("android.permission.POST_NOTIFICATIONS", {}),
    ("android.permission.READ_MEDIA_IMAGES", {}),
    ("android.permission.READ_MEDIA_VIDEO", {}),
    ("android.permission.READ_EXTERNAL_STORAGE", {"maxSdkVersion": "32"}),
]

SHARE_MIME_TYPES = [
    "text/plain",
    "image/*",
    "application/pdf",
    "application/msword",
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    "application/vnd.ms-excel",
    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    "text/csv",
]

ANDROID_RELEASE_SIGNING_BLOCK = """
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
""".rstrip()

FLUTTER_TEMPLATE_WIDGET_TEST_MARKERS = [
    "Counter increments smoke test",
    "await tester.pumpWidget(const MyApp())",
]


def android_attr(name: str) -> str:
    return f"{{{ANDROID_NS}}}{name}"


def ensure_permission(manifest: ET.Element, name: str, **attrs: str) -> None:
    for item in manifest.findall("uses-permission"):
        if item.get(android_attr("name")) == name:
            for key, value in attrs.items():
                item.set(android_attr(key), value)
            return
    node = ET.SubElement(manifest, "uses-permission")
    node.set(android_attr("name"), name)
    for key, value in attrs.items():
        node.set(android_attr(key), value)


def intent_filter_signature(filter_node: ET.Element) -> tuple[str, ...]:
    values: list[str] = []
    for child in filter_node:
        values.append(
            "|".join(
                [
                    child.tag,
                    child.get(android_attr("name"), ""),
                    child.get(android_attr("mimeType"), ""),
                    child.get(android_attr("scheme"), ""),
                ]
            )
        )
    return tuple(sorted(values))


def build_share_intent_filter(action: str, mime_type: str) -> ET.Element:
    node = ET.Element("intent-filter")
    action_node = ET.SubElement(node, "action")
    action_node.set(android_attr("name"), action)
    category = ET.SubElement(node, "category")
    category.set(android_attr("name"), "android.intent.category.DEFAULT")
    data = ET.SubElement(node, "data")
    data.set(android_attr("mimeType"), mime_type)
    return node


def build_deep_link_filter() -> ET.Element:
    node = ET.Element("intent-filter")
    action = ET.SubElement(node, "action")
    action.set(android_attr("name"), "android.intent.action.VIEW")
    category_default = ET.SubElement(node, "category")
    category_default.set(android_attr("name"), "android.intent.category.DEFAULT")
    category_browsable = ET.SubElement(node, "category")
    category_browsable.set(android_attr("name"), "android.intent.category.BROWSABLE")
    data = ET.SubElement(node, "data")
    data.set(android_attr("scheme"), "maclaw")
    return node


def configure_android() -> None:
    configure_android_root_gradle()
    configure_android_gradle()
    configure_android_gradle_properties()
    configure_android_variant_manifests()
    configure_android_main_activity()
    manifest_path = ROOT / "android/app/src/main/AndroidManifest.xml"
    if not manifest_path.exists():
        return
    tree = ET.parse(manifest_path)
    manifest = tree.getroot()
    for name, attrs in ANDROID_PERMISSIONS:
        ensure_permission(manifest, name, **attrs)

    application = manifest.find("application")
    if application is None:
        return
    application.set(android_attr("label"), "MaClaw Mobile")
    activity = application.find("activity")
    if activity is None:
        return
    activity.set(android_attr("exported"), "true")

    existing = {
        intent_filter_signature(item) for item in activity.findall("intent-filter")
    }
    filters = [build_deep_link_filter()]
    for action in [
        "android.intent.action.SEND",
        "android.intent.action.SEND_MULTIPLE",
    ]:
        for mime_type in SHARE_MIME_TYPES:
            filters.append(build_share_intent_filter(action, mime_type))
    for filter_node in filters:
        signature = intent_filter_signature(filter_node)
        if signature not in existing:
            activity.append(filter_node)
            existing.add(signature)
    ET.indent(tree, space="    ")
    tree.write(manifest_path, encoding="utf-8", xml_declaration=True)


ANDROID_TEMPLATE_INTERNET_PERMISSION_COMMENT = """    <!-- The INTERNET permission is required for development. Specifically,
         the Flutter tool needs it to communicate with the running application
         to allow setting breakpoints, to provide hot reload, etc.
    -->
"""


def configure_android_variant_manifests() -> None:
    replacements = {
        ROOT / "android/app/src/debug/AndroidManifest.xml": (
            "    <!-- Development builds need network access for MaClaw service smoke checks. -->\n"
        ),
        ROOT / "android/app/src/profile/AndroidManifest.xml": (
            "    <!-- Profile builds need network access for MaClaw service smoke checks. -->\n"
        ),
    }
    for path, replacement in replacements.items():
        if not path.exists():
            continue
        text = path.read_text(encoding="utf-8")
        text = text.replace(ANDROID_TEMPLATE_INTERNET_PERMISSION_COMMENT, replacement)
        path.write_text(text, encoding="utf-8")



def configure_android_root_gradle() -> None:
    gradle_path = ROOT / "android/build.gradle.kts"
    if not gradle_path.exists():
        return
    text = gradle_path.read_text(encoding="utf-8")
    marker = "compilerOptions.jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)"
    if marker in text:
        return
    block = """
subprojects {
    plugins.withId("com.android.application") {
        extensions.configure<com.android.build.gradle.AppExtension>("android") {
            compileOptions {
                sourceCompatibility = JavaVersion.VERSION_17
                targetCompatibility = JavaVersion.VERSION_17
            }
        }
    }
    plugins.withId("com.android.library") {
        extensions.configure<com.android.build.gradle.LibraryExtension>("android") {
            compileOptions {
                sourceCompatibility = JavaVersion.VERSION_17
                targetCompatibility = JavaVersion.VERSION_17
            }
        }
    }
    tasks.withType<org.jetbrains.kotlin.gradle.tasks.KotlinCompile>().configureEach {
        compilerOptions.jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
    }
}

""".lstrip()
    text = re.sub(r"(?m)^subprojects \{\r?\n    val newSubprojectBuildDir", block + "subprojects {\n    val newSubprojectBuildDir", text, count=1)
    gradle_path.write_text(text, encoding="utf-8")


def configure_android_gradle_properties() -> None:
    properties_path = ROOT / "android/gradle.properties"
    if not properties_path.exists():
        return
    text = properties_path.read_text(encoding="utf-8")
    for line in [
        "kotlin.incremental=false",
        "kotlin.jvm.target.validation.mode=ignore",
    ]:
        if line not in text:
            text = text.rstrip() + f"\n{line}\n"
    properties_path.write_text(text, encoding="utf-8")

def configure_android_gradle() -> None:
    gradle_path = ROOT / "android/app/build.gradle.kts"
    if not gradle_path.exists():
        return
    text = gradle_path.read_text(encoding="utf-8")
    text = text.replace(
        "    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.\n",
        "    // Keep the MaClaw Mobile wrapper on the standard plugin order.\n",
    )
    text = text.replace(
        'namespace = "com.example.maclaw_mobile"',
        f'namespace = "{ANDROID_PACKAGE_ID}"',
    )
    text = text.replace(
        'applicationId = "com.example.maclaw_mobile"',
        f'applicationId = "{ANDROID_PACKAGE_ID}"',
    )
    for marker in [
        "        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).\n",
        "        // You can update the following values to match your application needs.\n",
        "        // For more information, see: https://flutter.dev/to/review-gradle-config.\n",
    ]:
        text = text.replace(marker, "")
    if "isCoreLibraryDesugaringEnabled = true" not in text:
        text = text.replace(
            "targetCompatibility = JavaVersion.VERSION_17",
            "targetCompatibility = JavaVersion.VERSION_17\n        isCoreLibraryDesugaringEnabled = true",
            1,
        )
    if "coreLibraryDesugaring(" not in text:
        text = text.rstrip() + (
            "\n\ndependencies {\n"
            "    coreLibraryDesugaring(\"com.android.tools:desugar_jdk_libs:2.1.5\")\n"
            "}\n"
        )
    text = configure_android_release_signing(text)
    gradle_path.write_text(text, encoding="utf-8")


def configure_android_release_signing(text: str) -> str:
    if "import java.util.Properties" not in text:
        text = "import java.util.Properties\n\n" + text
    signing_setup = """
val maclawKeystorePropertiesFile = rootProject.file("key.properties")
val maclawKeystoreProperties = Properties()
val maclawReleaseSigningConfigured = maclawKeystorePropertiesFile.exists()
if (maclawReleaseSigningConfigured) {
    maclawKeystorePropertiesFile.inputStream().use { maclawKeystoreProperties.load(it) }
}

gradle.taskGraph.whenReady {
    val releaseTaskRequested = allTasks.any { task ->
        task.path.endsWith(":app:assembleRelease") || task.path.endsWith(":app:bundleRelease")
    }
    if (releaseTaskRequested && !maclawReleaseSigningConfigured) {
        throw GradleException(
            "MaClaw Mobile release signing requires android/key.properties with storeFile, storePassword, keyAlias, and keyPassword."
        )
    }
}

""".lstrip()
    text = re.sub(
        r"(?ms)^val maclawKeystorePropertiesFile = rootProject\.file\(\"key\.properties\"\).*?^}\n\nplugins \{",
        "plugins {",
        text,
        count=1,
    )
    if "val maclawKeystorePropertiesFile = rootProject.file(\"key.properties\")" not in text:
        text = re.sub(
            r"(?ms)^(plugins \{.*?^}\n)",
            r"\1\n" + signing_setup,
            text,
            count=1,
        )
    if "val maclawKeystorePropertiesFile = rootProject.file(\"key.properties\")" not in text:
        text = text.replace("\nandroid {\n", "\n" + signing_setup + "android {\n", 1)
    if "signingConfigs {" not in text:
        text = re.sub(
            r"(?ms)^    buildTypes \{.*?^    \}\n",
            ANDROID_RELEASE_SIGNING_BLOCK + "\n",
            text,
            count=1,
        )
    else:
        text = re.sub(
            r"(?ms)^    signingConfigs \{.*?^    buildTypes \{.*?^    \}\n",
            ANDROID_RELEASE_SIGNING_BLOCK + "\n",
            text,
            count=1,
        )
    text = text.replace(
        "            // TODO: Add your own signing config for the release build.\n"
        "            // Signing with the debug keys for now, so `flutter run --release` works.\n"
        "            signingConfig = signingConfigs.getByName(\"debug\")\n",
        "",
    )
    return text



def configure_android_main_activity() -> None:
    kotlin_root = ROOT / "android/app/src/main/kotlin"
    if not kotlin_root.exists():
        return
    package_dir = kotlin_root / Path(ANDROID_PACKAGE_ID.replace(".", "/"))
    package_dir.mkdir(parents=True, exist_ok=True)
    target = package_dir / "MainActivity.kt"
    candidates = [
        kotlin_root / "com/example/maclaw_mobile/MainActivity.kt",
        kotlin_root / "top/mypapers/maclaw/maclaw_mobile/MainActivity.kt",
        target,
    ]
    source = next((item for item in candidates if item.exists()), None)
    if source is None:
        return
    body = source.read_text(encoding="utf-8")
    body = re.sub(
        r"^package\s+[A-Za-z0-9_.]+",
        f"package {ANDROID_PACKAGE_ID}",
        body,
        count=1,
        flags=re.MULTILINE,
    )
    target.write_text(body, encoding="utf-8")
    for candidate in candidates:
        if candidate != target and candidate.exists():
            candidate.unlink()
            prune_empty_dirs(candidate.parent, kotlin_root)


def prune_empty_dirs(path: Path, stop: Path) -> None:
    current = path
    stop = stop.resolve()
    while current.resolve() != stop and current.exists():
        try:
            current.rmdir()
        except OSError:
            break
        current = current.parent

def configure_ios() -> None:
    plist_path = ROOT / "ios/Runner/Info.plist"
    if not plist_path.exists():
        return
    with plist_path.open("rb") as fh:
        plist = plistlib.load(fh)
    plist["AppGroupId"] = "$(CUSTOM_GROUP_ID)"
    plist["CFBundleDisplayName"] = "MaClaw Mobile"
    plist["CFBundleName"] = "MaClaw Mobile"
    apply_ios_usage_descriptions(plist)
    apply_ios_background_modes(plist)
    url_types = plist.setdefault("CFBundleURLTypes", [])
    if not any("maclaw" in item.get("CFBundleURLSchemes", []) for item in url_types):
        url_types.append(
            {
                "CFBundleURLName": IOS_BUNDLE_ID,
                "CFBundleURLSchemes": ["maclaw"],
            }
        )
    share_scheme = "ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER)"
    if not any(share_scheme in item.get("CFBundleURLSchemes", []) for item in url_types):
        url_types.append(
            {
                "CFBundleTypeRole": "Editor",
                "CFBundleURLName": "MaClaw Mobile Share",
                "CFBundleURLSchemes": [share_scheme],
            }
        )
    with plist_path.open("wb") as fh:
        plistlib.dump(plist, fh)
    configure_ios_entitlements()
    configure_ios_share_extension()
    configure_ios_runner_tests()
    configure_ios_project_settings()


def apply_ios_usage_descriptions(plist: dict[str, object]) -> None:
    for key, value in IOS_USAGE_DESCRIPTIONS.items():
        if key not in plist or is_corrupt_ios_usage_description(plist[key]):
            plist[key] = value


def apply_ios_background_modes(plist: dict[str, object]) -> None:
    """Preserve existing modes and ensure background recording survives regen."""
    current = plist.get("UIBackgroundModes", [])
    modes = current if isinstance(current, list) else []
    normalized = [mode for mode in modes if isinstance(mode, str) and mode.strip()]
    for mode in IOS_BACKGROUND_MODES:
        if mode not in normalized:
            normalized.append(mode)
    plist["UIBackgroundModes"] = normalized


def is_corrupt_ios_usage_description(value: object) -> bool:
    if not isinstance(value, str):
        return True
    text = value.strip()
    if not text:
        return True
    return any(marker in text for marker in IOS_CORRUPT_USAGE_MARKERS)


def entitlement_payload() -> dict[str, object]:
    return {
        "com.apple.security.application-groups": ["$(CUSTOM_GROUP_ID)"],
    }


def configure_ios_entitlements() -> None:
    runner_dir = ROOT / "ios/Runner"
    if not runner_dir.exists():
        return
    entitlements_path = runner_dir / "Runner.entitlements"
    with entitlements_path.open("wb") as fh:
        plistlib.dump(entitlement_payload(), fh)


def configure_ios_runner_tests() -> None:
    tests_dir = ROOT / "ios/RunnerTests"
    if not tests_dir.exists():
        return
    (tests_dir / "RunnerTests.swift").write_text(
        IOS_RUNNER_TESTS_SWIFT,
        encoding="utf-8",
    )


def configure_ios_share_extension() -> None:
    ios_dir = ROOT / "ios"
    if not ios_dir.exists():
        return
    share_dir = ios_dir / IOS_SHARE_EXTENSION_NAME
    share_dir.mkdir(parents=True, exist_ok=True)
    write_ios_share_info_plist(share_dir / "Info.plist")
    write_ios_share_entitlements(share_dir / f"{IOS_SHARE_EXTENSION_NAME}.entitlements")
    write_ios_share_controller(share_dir / "ShareViewController.swift")
    patch_ios_project_hint(ios_dir / "Runner.xcodeproj/project.pbxproj")


def write_ios_share_info_plist(path: Path) -> None:
    plist = {
        "AppGroupId": "$(CUSTOM_GROUP_ID)",
        "CFBundleDevelopmentRegion": "$(DEVELOPMENT_LANGUAGE)",
        "CFBundleDisplayName": "MaClaw",
        "CFBundleExecutable": "$(EXECUTABLE_NAME)",
        "CFBundleIdentifier": "$(PRODUCT_BUNDLE_IDENTIFIER)",
        "CFBundleInfoDictionaryVersion": "6.0",
        "CFBundleName": "$(PRODUCT_NAME)",
        "CFBundlePackageType": "$(PRODUCT_BUNDLE_PACKAGE_TYPE)",
        "CFBundleShortVersionString": "$(FLUTTER_BUILD_NAME)",
        "CFBundleVersion": "$(FLUTTER_BUILD_NUMBER)",
        "NSExtension": {
            "NSExtensionAttributes": {
                "PHSupportedMediaTypes": ["Image"],
                "NSExtensionActivationRule": {
                    "NSExtensionActivationSupportsFileWithMaxCount": 10,
                    "NSExtensionActivationSupportsImageWithMaxCount": 20,
                    "NSExtensionActivationSupportsText": True,
                    "NSExtensionActivationSupportsWebURLWithMaxCount": 1,
                },
            },
            "NSExtensionPointIdentifier": "com.apple.share-services",
            "NSExtensionPrincipalClass": "$(PRODUCT_MODULE_NAME).ShareViewController",
        },
    }
    with path.open("wb") as fh:
        plistlib.dump(plist, fh)


def write_ios_share_entitlements(path: Path) -> None:
    with path.open("wb") as fh:
        plistlib.dump(entitlement_payload(), fh)


def write_ios_share_controller(path: Path) -> None:
    path.write_text(
        """import receive_sharing_intent

class ShareViewController: RSIShareViewController {
    override func shouldAutoRedirect() -> Bool {
        return true
    }
}
""",
        encoding="utf-8",
    )


def configure_ios_project_settings() -> None:
    project_path = ROOT / "ios/Runner.xcodeproj/project.pbxproj"
    if not project_path.exists():
        return
    text = project_path.read_text(encoding="utf-8", errors="ignore")
    text = text.replace(
        "PRODUCT_BUNDLE_IDENTIFIER = com.example.maclawMobile;",
        f"PRODUCT_BUNDLE_IDENTIFIER = {IOS_BUNDLE_ID};",
    )
    text = re.sub(
        r"PRODUCT_BUNDLE_IDENTIFIER = com\.example\.maclawMobile\.RunnerTests;",
        f"PRODUCT_BUNDLE_IDENTIFIER = {IOS_TEST_BUNDLE_ID};",
        text,
    )
    text = add_build_setting_if_missing(
        text,
        "CUSTOM_GROUP_ID",
        IOS_APP_GROUP_ID,
    )
    project_path.write_text(text, encoding="utf-8")


def add_build_setting_if_missing(text: str, key: str, value: str) -> str:
    if f"{key} =" in text:
        return text
    return text.replace(
        "PRODUCT_BUNDLE_IDENTIFIER =",
        f"{key} = {value};\n\t\t\t\tPRODUCT_BUNDLE_IDENTIFIER =",
    )


def patch_ios_project_hint(project_path: Path) -> None:
    """Leave a machine-readable note when the generated Xcode project exists.

    The official Apple Team ID, signing profiles, and the Swift package product
    are selected in Xcode. The generated files are the canonical contents for
    the Share Extension target and keep CI regeneration deterministic.
    """
    if not project_path.exists():
        return
    marker = "MaClaw Mobile Share Extension files are generated by tool/configure_platforms.py"
    text = project_path.read_text(encoding="utf-8", errors="ignore")
    if marker in text:
        return
    project_path.write_text(text + f"\n/* {marker} */\n", encoding="utf-8")


def remove_flutter_template_widget_test() -> None:
    widget_test = ROOT / "test/widget_test.dart"
    if not widget_test.exists():
        return
    text = widget_test.read_text(encoding="utf-8", errors="ignore")
    if all(marker in text for marker in FLUTTER_TEMPLATE_WIDGET_TEST_MARKERS):
        widget_test.unlink()


def main() -> None:
    configure_android()
    configure_ios()
    remove_flutter_template_widget_test()


if __name__ == "__main__":
    main()
