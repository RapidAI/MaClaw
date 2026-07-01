#!/usr/bin/env python3
"""Apply MaClaw Mobile native settings after `flutter create`.

The repository intentionally keeps the Flutter project lightweight. CI and
developers can regenerate Android/iOS wrappers, then run this script to restore
the mobile-specific permissions and sharing entry points.
"""

from __future__ import annotations

import plistlib
import xml.etree.ElementTree as ET
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
ANDROID_NS = "http://schemas.android.com/apk/res/android"
ET.register_namespace("android", ANDROID_NS)

IOS_BUNDLE_ID = "top.mypapers.maclaw.mobile"
IOS_APP_GROUP_ID = f"group.{IOS_BUNDLE_ID}"
IOS_SHARE_EXTENSION_NAME = "ShareExtension"


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
    manifest_path = ROOT / "android/app/src/main/AndroidManifest.xml"
    if not manifest_path.exists():
        return
    tree = ET.parse(manifest_path)
    manifest = tree.getroot()
    ensure_permission(manifest, "android.permission.INTERNET")
    ensure_permission(manifest, "android.permission.CAMERA")
    ensure_permission(manifest, "android.permission.RECORD_AUDIO")
    ensure_permission(manifest, "android.permission.READ_MEDIA_IMAGES")
    ensure_permission(manifest, "android.permission.READ_MEDIA_VIDEO")
    ensure_permission(
        manifest,
        "android.permission.READ_EXTERNAL_STORAGE",
        maxSdkVersion="32",
    )

    application = manifest.find("application")
    if application is None:
        return
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
        for mime_type in [
            "text/plain",
            "image/*",
            "application/pdf",
            "application/msword",
            "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
            "application/vnd.ms-excel",
            "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
            "text/csv",
        ]:
            filters.append(build_share_intent_filter(action, mime_type))
    for filter_node in filters:
        signature = intent_filter_signature(filter_node)
        if signature not in existing:
            activity.append(filter_node)
            existing.add(signature)
    tree.write(manifest_path, encoding="utf-8", xml_declaration=True)


def configure_ios() -> None:
    plist_path = ROOT / "ios/Runner/Info.plist"
    if not plist_path.exists():
        return
    with plist_path.open("rb") as fh:
        plist = plistlib.load(fh)
    plist["AppGroupId"] = "$(CUSTOM_GROUP_ID)"
    plist.setdefault("NSCameraUsageDescription", "用于拍照提问和导入图片文档。")
    plist.setdefault("NSMicrophoneUsageDescription", "用于语音提问。")
    plist.setdefault("NSSpeechRecognitionUsageDescription", "用于将语音提问转成文字。")
    plist.setdefault("NSPhotoLibraryUsageDescription", "用于从相册导入图片或截图。")
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


def main() -> None:
    configure_android()
    configure_ios()


if __name__ == "__main__":
    main()
