from __future__ import annotations

import argparse
import sys
from pathlib import Path


REQUIRED_GRADLE_MARKERS = (
    'import java.util.Properties',
    'rootProject.file("key.properties")',
    'maclawReleaseSigningConfigured',
    'create("release")',
    'signingConfig = signingConfigs.getByName("release")',
    ':app:assembleRelease',
    ':app:bundleRelease',
    'storeFile',
    'storePassword',
    'keyAlias',
    'keyPassword',
)

FORBIDDEN_GRADLE_MARKERS = (
    'signingConfig = signingConfigs.getByName("debug")',
    'Signing with the debug keys',
    'Specify your own unique Application ID',
    'flutter.dev/to/review-gradle-config',
    'You can update the following values to match your application needs',
)

REQUIRED_GITIGNORE_MARKERS = (
    'mobile/maclaw_mobile/android/key.properties',
    'mobile/maclaw_mobile/android/*.jks',
    'mobile/maclaw_mobile/android/*.keystore',
)

REQUIRED_KEY_PROPERTIES_EXAMPLE_MARKERS = (
    'storeFile=release-signing-key.jks',
    'storePassword=<release-keystore-password>',
    'keyAlias=<release-key-alias>',
    'keyPassword=<release-key-password>',
)

FORBIDDEN_KEY_PROPERTIES_EXAMPLE_MARKERS = ('-----BEGIN',)


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def repo_root(root: Path) -> Path:
    return root.resolve().parents[1]


def verify_android_release_signing(root: Path) -> list[str]:
    errors: list[str] = []
    gradle_path = root / 'android/app/build.gradle.kts'
    example_path = root / 'android/key.properties.example'
    gitignore_path = repo_root(root) / '.gitignore'

    if not gradle_path.exists():
        return [f'Missing Android Gradle file: {gradle_path}']
    gradle = gradle_path.read_text(encoding='utf-8')
    for marker in REQUIRED_GRADLE_MARKERS:
        if marker not in gradle:
            errors.append(f'Android release signing Gradle config missing `{marker}`.')
    for marker in FORBIDDEN_GRADLE_MARKERS:
        if marker in gradle:
            errors.append(f'Android release signing Gradle config must not contain `{marker}`.')

    if not example_path.exists():
        errors.append(f'Missing Android signing template: {example_path}')
    else:
        example = example_path.read_text(encoding='utf-8')
        for marker in REQUIRED_KEY_PROPERTIES_EXAMPLE_MARKERS:
            if marker not in example:
                errors.append(f'Android signing template missing `{marker}`.')
        for marker in FORBIDDEN_KEY_PROPERTIES_EXAMPLE_MARKERS:
            if marker in example:
                errors.append(f'Android signing template must keep `{marker}` redacted.')

    if not gitignore_path.exists():
        errors.append(f'Missing repository .gitignore: {gitignore_path}')
        return errors
    gitignore = gitignore_path.read_text(encoding='utf-8')
    for marker in REQUIRED_GITIGNORE_MARKERS:
        if marker not in gitignore:
            errors.append(f'Repository .gitignore must ignore `{marker}`.')

    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description='Verify MaClaw Mobile Android release signing is not using debug keys.',
    )
    parser.add_argument(
        '--root',
        type=Path,
        default=mobile_root(),
        help='Path to mobile/maclaw_mobile. Defaults to this script project root.',
    )
    args = parser.parse_args(argv)

    errors = verify_android_release_signing(args.root.resolve())
    if not errors:
        print('MaClaw Mobile Android release signing config verified.')
        return 0

    print('MaClaw Mobile Android release signing violations found:')
    for error in errors:
        print(f'- {error}')
    return 1


if __name__ == '__main__':
    sys.exit(main())
