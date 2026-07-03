from __future__ import annotations

import argparse
import os
import sys
from dataclasses import dataclass
from pathlib import Path


ENV_STORE_FILE = "MACLAW_ANDROID_STORE_FILE"
ENV_STORE_PASSWORD = "MACLAW_ANDROID_STORE_PASSWORD"
ENV_KEY_ALIAS = "MACLAW_ANDROID_KEY_ALIAS"
ENV_KEY_PASSWORD = "MACLAW_ANDROID_KEY_PASSWORD"
REQUIRED_ENV = (
    ENV_STORE_FILE,
    ENV_STORE_PASSWORD,
    ENV_KEY_ALIAS,
    ENV_KEY_PASSWORD,
)


@dataclass(frozen=True)
class AndroidSigningConfig:
    store_file: str
    store_password: str
    key_alias: str
    key_password: str


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _resolve_store_file(root: Path, store_file: str) -> Path:
    path = Path(store_file)
    if path.is_absolute():
        return path
    return root / "android" / path


def config_from_env(env: dict[str, str]) -> tuple[AndroidSigningConfig | None, list[str]]:
    missing = [name for name in REQUIRED_ENV if not env.get(name, "").strip()]
    if missing:
        return None, [f"Missing environment variable `{name}`" for name in missing]
    return (
        AndroidSigningConfig(
            store_file=env[ENV_STORE_FILE].strip(),
            store_password=env[ENV_STORE_PASSWORD].strip(),
            key_alias=env[ENV_KEY_ALIAS].strip(),
            key_password=env[ENV_KEY_PASSWORD].strip(),
        ),
        [],
    )


def validate_config(root: Path, config: AndroidSigningConfig) -> list[str]:
    store_path = _resolve_store_file(root, config.store_file)
    errors: list[str] = []
    if not store_path.exists():
        errors.append(f"Android signing storeFile does not exist: {store_path}")
    if "debug" in store_path.name.lower():
        errors.append("Android signing storeFile must not be a debug keystore")
    if "\n" in config.store_password or "\n" in config.key_password:
        errors.append("Android signing passwords must be single-line values")
    return errors


def write_key_properties(root: Path, config: AndroidSigningConfig, force: bool = False) -> Path:
    target = root / "android" / "key.properties"
    if target.exists() and not force:
        raise FileExistsError(
            f"{target} already exists; pass --force to overwrite local signing config",
        )
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(
        "\n".join(
            [
                f"storeFile={config.store_file}",
                f"storePassword={config.store_password}",
                f"keyAlias={config.key_alias}",
                f"keyPassword={config.key_password}",
                "",
            ],
        ),
        encoding="utf-8",
    )
    return target


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Create local android/key.properties from MaClaw Android signing "
            "environment variables. Secrets are read from the environment, not "
            "from command-line arguments."
        ),
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=mobile_root(),
        help="Path to mobile/maclaw_mobile. Defaults to this script project root.",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Overwrite an existing local android/key.properties file.",
    )
    args = parser.parse_args(argv)

    root = args.root.resolve()
    config, errors = config_from_env(os.environ)
    if config is not None:
        errors.extend(validate_config(root, config))
    if errors:
        print("Android signing setup cannot continue:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        print(
            "Set MACLAW_ANDROID_STORE_FILE, MACLAW_ANDROID_STORE_PASSWORD, "
            "MACLAW_ANDROID_KEY_ALIAS, and MACLAW_ANDROID_KEY_PASSWORD, then retry.",
            file=sys.stderr,
        )
        return 1

    assert config is not None
    try:
        target = write_key_properties(root, config, force=args.force)
    except FileExistsError as exc:
        print(str(exc), file=sys.stderr)
        return 1

    print(f"Wrote local Android signing config: {target}")
    print("Run `python3 tool/qa_preflight.py` next; do not commit android/key.properties.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
