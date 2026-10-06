"""Reuse the real ESP-IDF compile flags to syntax-check edited files.

The project only builds with a MACLAW_PROFILE, and a full idf.py build is slow.
This reuses the recorded per-file command from compile_commands.json so the
xtensa front end sees exactly the real include paths and defines, then runs
-fsyntax-only (no codegen, no link).

Two traps this handles:
  * shlex.split must use posix=True, otherwise -DIDF_VER="v6.0.2" is split
    wrong and every system header appears to be missing.
  * with posix=True the compiler path's backslashes are eaten, so the compiler
    is invoked by its absolute path separately from the split flag list.
"""

import json
import os
import shlex
import subprocess
import sys

DB = "build-unified-echoear/compile_commands.json"

# A file added since the last configure has no recorded command.  Every source
# in the same component is compiled with identical flags, so a sibling's entry
# is enough for -fsyntax-only: swap in the new source and let the front end
# check it against the real include paths.
FALLBACK_ENTRY = "gateway_dispatcher.c"


def flags_for(entry):
    # The compiler path is taken from the raw string: shlex with posix=True
    # eats the backslashes in "C:\...\xtensa-esp32s3-elf-gcc.exe".
    cc = entry["command"].split()[0]
    parts = shlex.split(entry["command"], posix=True)
    out = []
    i = 1
    while i < len(parts):
        part = parts[i]
        if part == "-o":          # drop the "-o <object>" pair
            i += 2
            continue
        # Drop the original source file.  Comparing against entry["file"] is
        # not enough: posix splitting already removed its backslashes, so the
        # reliable test is "ends with .c" (no flag argument does).
        if part.endswith(".c"):
            i += 1
            continue
        out.append(part)
        i += 1
    return cc, out


def main(targets):
    with open(DB, encoding="utf-8") as handle:
        db = json.load(handle)
    by_file = {}
    for entry in db:
        key = entry["file"].replace("\\", "/").rsplit("/", 1)[-1]
        by_file.setdefault(key, entry)

    failures = []
    for target in targets:
        key = target.replace("\\", "/").rsplit("/", 1)[-1]
        entry = by_file.get(key)
        borrowed = False
        if entry is None:
            entry = by_file.get(FALLBACK_ENTRY)
            borrowed = entry is not None
        if entry is None:
            failures.append(f"{target}: no compile_commands entry")
            continue
        cc, flags = flags_for(entry)
        # Absolute path: the compiler runs from the build directory.
        absolute = os.path.abspath(target)
        cmd = [cc] + flags + ["-fsyntax-only", absolute]
        proc = subprocess.run(cmd, cwd=entry["directory"],
                              capture_output=True, text=True)
        if proc.returncode != 0:
            failures.append(f"{target}: exit {proc.returncode}\n{proc.stdout}{proc.stderr}")
        else:
            note = f" (borrowed flags from {FALLBACK_ENTRY})" if borrowed else ""
            print(f"OK   {target}{note}")
    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
