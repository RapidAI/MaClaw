from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path


DEFAULT_SCAN_TARGETS = ("lib", "android", "ios", "pubspec.yaml", "pubspec.lock")
SKIP_DIRS = {
    ".dart_tool",
    ".gradle",
    ".idea",
    ".symlinks",
    "build",
    "DerivedData",
    "Pods",
}
TEXT_SUFFIXES = {
    ".dart",
    ".gradle",
    ".java",
    ".kt",
    ".lock",
    ".m",
    ".mm",
    ".plist",
    ".podspec",
    ".swift",
    ".yaml",
    ".yml",
}


@dataclass(frozen=True)
class BoundaryRule:
    name: str
    pattern: re.Pattern[str]
    reason: str


@dataclass(frozen=True)
class BoundaryViolation:
    path: Path
    line: int
    rule: BoundaryRule
    text: str


RULES = (
    BoundaryRule(
        "dart ffi",
        re.compile(r"\bdart:ffi\b"),
        "Flutter Mobile must not load Go corelib through Dart FFI.",
    ),
    BoundaryRule(
        "dynamic library",
        re.compile(r"\bDynamicLibrary\b"),
        "Flutter Mobile must not load a bundled Go corelib dynamic library.",
    ),
    BoundaryRule(
        "gomobile",
        re.compile(r"\bgomobile\b", re.IGNORECASE),
        "Flutter Mobile must not embed Go corelib through gomobile bindings.",
    ),
    BoundaryRule(
        "corelib reference",
        re.compile(r"corelib", re.IGNORECASE),
        "Core MaClaw capabilities must stay behind Hub APIs or digital employees.",
    ),
    BoundaryRule(
        "corelib method channel",
        re.compile(r"\bMethodChannel\b.*\b(corelib|go|native)\b", re.IGNORECASE),
        "Flutter Mobile must not add a native method-channel bridge to corelib.",
    ),
    BoundaryRule(
        "phone-local ssh dependency",
        re.compile(r"\bdartssh2\b", re.IGNORECASE),
        "Flutter Mobile backend SSH must stay behind Hub/GUI agent sessions, not a phone-local SSH client dependency.",
    ),
    BoundaryRule(
        "phone-side ssh credential api",
        re.compile(
            r"\b(?:saveServerPassword|readServerPassword|saveServerPrivateKey|"
            r"readServerPrivateKey|readServerPrivateKeyPassphrase)\b",
        ),
        "Flutter Mobile must not expose phone-side SSH credential save/read APIs; credentials stay on MaClaw GUI/agent.",
    ),
)


def mobile_root() -> Path:
    return Path(__file__).resolve().parents[1]


def iter_text_files(root: Path, targets: tuple[str, ...] = DEFAULT_SCAN_TARGETS):
    for target in targets:
        path = root / target
        if not path.exists():
            continue
        if path.is_file():
            if path.suffix in TEXT_SUFFIXES:
                yield path
            continue
        for child in path.rglob("*"):
            if any(part in SKIP_DIRS for part in child.relative_to(root).parts):
                continue
            if child.is_file() and child.suffix in TEXT_SUFFIXES:
                yield child


def find_violations(root: Path) -> list[BoundaryViolation]:
    violations: list[BoundaryViolation] = []
    for path in sorted(iter_text_files(root)):
        try:
            lines = path.read_text(encoding="utf-8").splitlines()
        except UnicodeDecodeError:
            continue
        for line_number, line in enumerate(lines, start=1):
            for rule in RULES:
                if rule.pattern.search(line):
                    violations.append(
                        BoundaryViolation(
                            path=path.relative_to(root),
                            line=line_number,
                            rule=rule,
                            text=line.strip(),
                        )
                    )
    return violations


def format_result(violations: list[BoundaryViolation]) -> str:
    if not violations:
        return "MaClaw Mobile runtime boundary verified.\n"

    lines = ["MaClaw Mobile runtime boundary violations found:"]
    for violation in violations:
        lines.append(
            f"- {violation.path}:{violation.line}: {violation.rule.name}: "
            f"{violation.text}",
        )
        lines.append(f"  {violation.rule.reason}")
    return "\n".join(lines).rstrip() + "\n"


def write_log(path: Path, text: str, *, force: bool = False) -> None:
    if path.exists() and not force:
        raise FileExistsError(
            f"{path} already exists; pass --force to overwrite runtime-boundary evidence log",
        )
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Verify that MaClaw Mobile does not embed or bridge Go corelib.",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=mobile_root(),
        help="Path to mobile/maclaw_mobile. Defaults to this script's project root.",
    )
    parser.add_argument(
        "--log",
        type=Path,
        help="Optional path to write the runtime-boundary verification result for QA evidence.",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Overwrite an existing runtime-boundary evidence log.",
    )
    args = parser.parse_args(argv)

    root = args.root.resolve()
    violations = find_violations(root)
    output = format_result(violations)
    if args.log:
        try:
            write_log(args.log, output, force=args.force)
        except FileExistsError as exc:
            print(f"Runtime boundary log write failed: {exc}", file=sys.stderr)
            return 1
    if not violations:
        print(output, end="")
        return 0

    print(output, end="", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
