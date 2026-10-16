from __future__ import annotations

import argparse
import plistlib
import sys
from pathlib import Path

import configure_platforms


IOS_BUNDLE_ID = 'top.mypapers.maclaw.mobile'
IOS_TEST_BUNDLE_ID = f'{IOS_BUNDLE_ID}.RunnerTests'
IOS_APP_GROUP_ID = f'group.{IOS_BUNDLE_ID}'
IOS_SHARE_EXTENSION_NAME = 'ShareExtension'
IOS_RUNNER_TEST_MARKERS = (
    'testMaClawMobileBundleConfiguration',
    f'"{IOS_TEST_BUNDLE_ID}"',
)

# Keep this aligned with tool/configure_platforms.py. CI runs configure, then this verifier.
IOS_USAGE_DESCRIPTIONS = configure_platforms.IOS_USAGE_DESCRIPTIONS


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
    podfile_path = ios / 'Podfile'

    required_files = [
        runner_info_path,
        runner_entitlements_path,
        share_info_path,
        share_entitlements_path,
        share_controller_path,
        runner_tests_path,
        project_path,
        podfile_path,
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
        'name = ShareExtension;',
        'productType = "com.apple.product-type.app-extension";',
        f'PRODUCT_BUNDLE_IDENTIFIER = {IOS_BUNDLE_ID}.ShareExtension;',
        'D1A000000000000000000304 /* Embed App Extensions */',
        'dstSubfolderSpec = 13;',
    ]:
        if marker not in project:
            errors.append(f'iOS Xcode project missing `{marker}`.')
    podfile = podfile_path.read_text(encoding='utf-8', errors='ignore')
    if "target 'ShareExtension' do" not in podfile:
        errors.append("iOS Podfile missing the ShareExtension target.")
    if "pod 'receive_sharing_intent'" not in podfile:
        errors.append('iOS Podfile must link receive_sharing_intent for ShareExtension.')
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
