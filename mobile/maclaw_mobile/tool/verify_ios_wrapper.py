from __future__ import annotations

import argparse
import plistlib
import sys
from pathlib import Path


IOS_BUNDLE_ID = 'top.mypapers.maclaw.mobile'
IOS_TEST_BUNDLE_ID = f'{IOS_BUNDLE_ID}.RunnerTests'
IOS_APP_GROUP_ID = f'group.{IOS_BUNDLE_ID}'
IOS_SHARE_EXTENSION_NAME = 'ShareExtension'
IOS_RUNNER_TEST_MARKERS = (
    'testMaClawMobileBundleConfiguration',
    f'"{IOS_TEST_BUNDLE_ID}"',
)

IOS_USAGE_DESCRIPTIONS = {
    'NSCameraUsageDescription': '\u7528\u4e8e\u62cd\u7167\u63d0\u95ee\u548c\u5bfc\u5165\u56fe\u7247\u6587\u6863\u3002',
    'NSMicrophoneUsageDescription': '\u7528\u4e8e\u8bed\u97f3\u63d0\u95ee\u3002',
    'NSSpeechRecognitionUsageDescription': '\u7528\u4e8e\u5c06\u8bed\u97f3\u63d0\u95ee\u8f6c\u6210\u6587\u5b57\u3002',
    'NSPhotoLibraryUsageDescription': '\u7528\u4e8e\u4ece\u76f8\u518c\u5bfc\u5165\u56fe\u7247\u6216\u622a\u56fe\u3002',
    'NSLocalNetworkUsageDescription': '\u7528\u4e8e\u8fde\u63a5\u672c\u5730\u6216\u5185\u7f51\u670d\u52a1\u5668\u8fdb\u884c SSH \u5e94\u6025\u7ef4\u62a4\u3002',
}


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def read_plist(path: Path) -> dict[str, object]:
    with path.open('rb') as fh:
        return plistlib.load(fh)


def _url_schemes(plist: dict[str, object]) -> set[str]:
    schemes: set[str] = set()
    for item in plist.get('CFBundleURLTypes', []):
        if isinstance(item, dict):
            for scheme in item.get('CFBundleURLSchemes', []):
                if isinstance(scheme, str):
                    schemes.add(scheme)
    return schemes


def _app_groups(path: Path) -> list[str]:
    entitlements = read_plist(path)
    groups = entitlements.get('com.apple.security.application-groups', [])
    return [group for group in groups if isinstance(group, str)]


def verify_ios_wrapper(root: Path) -> list[str]:
    ios = root / 'ios'
    runner_info_path = ios / 'Runner/Info.plist'
    runner_entitlements_path = ios / 'Runner/Runner.entitlements'
    share_info_path = ios / f'{IOS_SHARE_EXTENSION_NAME}/Info.plist'
    share_entitlements_path = ios / f'{IOS_SHARE_EXTENSION_NAME}/{IOS_SHARE_EXTENSION_NAME}.entitlements'
    share_controller_path = ios / f'{IOS_SHARE_EXTENSION_NAME}/ShareViewController.swift'
    runner_tests_path = ios / 'RunnerTests/RunnerTests.swift'
    project_path = ios / 'Runner.xcodeproj/project.pbxproj'

    required_files = [
        runner_info_path,
        runner_entitlements_path,
        share_info_path,
        share_entitlements_path,
        share_controller_path,
        runner_tests_path,
        project_path,
    ]
    missing = [path for path in required_files if not path.exists()]
    if missing:
        return [f'Missing iOS wrapper file: {path}' for path in missing]

    errors: list[str] = []
    runner_info = read_plist(runner_info_path)
    for key, expected in IOS_USAGE_DESCRIPTIONS.items():
        if runner_info.get(key) != expected:
            errors.append(f'iOS Runner Info.plist `{key}` must contain the readable MaClaw usage description.')
    if runner_info.get('CFBundleDisplayName') != 'MaClaw Mobile':
        errors.append('iOS Runner Info.plist CFBundleDisplayName must be MaClaw Mobile.')
    if runner_info.get('CFBundleName') != 'MaClaw Mobile':
        errors.append('iOS Runner Info.plist CFBundleName must be MaClaw Mobile, not the Flutter template name.')
    if runner_info.get('AppGroupId') != '$(CUSTOM_GROUP_ID)':
        errors.append('iOS Runner Info.plist must expose AppGroupId as $(CUSTOM_GROUP_ID).')
    schemes = _url_schemes(runner_info)
    for scheme in {'maclaw', 'ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER)'}:
        if scheme not in schemes:
            errors.append(f'iOS Runner Info.plist missing URL scheme `{scheme}`.')

    for path, label in [
        (runner_entitlements_path, 'Runner'),
        (share_entitlements_path, IOS_SHARE_EXTENSION_NAME),
    ]:
        if '$(CUSTOM_GROUP_ID)' not in _app_groups(path):
            errors.append(f'iOS {label} entitlements must include $(CUSTOM_GROUP_ID).')

    share_info = read_plist(share_info_path)
    if share_info.get('AppGroupId') != '$(CUSTOM_GROUP_ID)':
        errors.append('iOS Share Extension Info.plist must expose AppGroupId as $(CUSTOM_GROUP_ID).')
    extension = share_info.get('NSExtension', {})
    if not isinstance(extension, dict):
        errors.append('iOS Share Extension Info.plist missing NSExtension dictionary.')
        extension = {}
    if extension.get('NSExtensionPointIdentifier') != 'com.apple.share-services':
        errors.append('iOS Share Extension must use com.apple.share-services.')
    if extension.get('NSExtensionPrincipalClass') != '$(PRODUCT_MODULE_NAME).ShareViewController':
        errors.append('iOS Share Extension must use ShareViewController principal class.')
    attrs = extension.get('NSExtensionAttributes', {})
    if not isinstance(attrs, dict):
        attrs = {}
    activation = attrs.get('NSExtensionActivationRule', {})
    if not isinstance(activation, dict):
        activation = {}
    expected_activation = {
        'NSExtensionActivationSupportsText': True,
        'NSExtensionActivationSupportsWebURLWithMaxCount': 1,
        'NSExtensionActivationSupportsFileWithMaxCount': 10,
        'NSExtensionActivationSupportsImageWithMaxCount': 20,
    }
    for key, expected in expected_activation.items():
        if activation.get(key) != expected:
            errors.append(f'iOS Share Extension activation rule `{key}` must be {expected!r}.')
    if attrs.get('PHSupportedMediaTypes') != ['Image']:
        errors.append('iOS Share Extension PHSupportedMediaTypes must be Image.')
    if 'RSIShareViewController' not in share_controller_path.read_text(encoding='utf-8'):
        errors.append('iOS ShareViewController must extend receive_sharing_intent RSIShareViewController.')
    runner_tests = runner_tests_path.read_text(encoding='utf-8', errors='ignore')
    for marker in IOS_RUNNER_TEST_MARKERS:
        if marker not in runner_tests:
            errors.append('iOS RunnerTests must contain the MaClaw bundle configuration smoke test.')
            break

    project = project_path.read_text(encoding='utf-8', errors='ignore')
    for marker in [
        f'PRODUCT_BUNDLE_IDENTIFIER = {IOS_BUNDLE_ID};',
        f'PRODUCT_BUNDLE_IDENTIFIER = {IOS_TEST_BUNDLE_ID};',
        f'CUSTOM_GROUP_ID = {IOS_APP_GROUP_ID};',
        'MaClaw Mobile Share Extension files are generated by tool/configure_platforms.py',
    ]:
        if marker not in project:
            errors.append(f'iOS Xcode project missing `{marker}`.')
    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description='Verify MaClaw Mobile iOS wrapper, Share Extension, and app-group wiring.',
    )
    parser.add_argument(
        '--root',
        type=Path,
        default=mobile_root(),
        help='Path to mobile/maclaw_mobile. Defaults to this script project root.',
    )
    args = parser.parse_args(argv)

    errors = verify_ios_wrapper(args.root.resolve())
    if not errors:
        print('MaClaw Mobile iOS wrapper verified.')
        return 0

    print('MaClaw Mobile iOS wrapper violations found:')
    for error in errors:
        print(f'- {error}')
    return 1


if __name__ == '__main__':
    sys.exit(main())
