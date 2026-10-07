"""Run idf.py from Git Bash / MSYS on Windows.

Git Bash forces MSYSTEM=MINGW64 into every process (it cannot be unset from the
shell -- `unset MSYSTEM` and `env -u MSYSTEM` both still leak it), and ESP-IDF's
`tools/idf.py` ends with:

    if 'MSYSTEM' in os.environ:
        print_warning('MSys/Mingw is not supported...')   # and never calls main()

so idf.py prints the warning and exits 0 without doing anything.  The only
reliable fix is to filter os.environ inside Python and then re-exec.

This script does four things before exec'ing idf.py:
  1. drops MSYSTEM / MSYS / MINGW* (otherwise idf.py no-ops);
  2. sets IDF_PATH / IDF_TOOLS_PATH / ESP_IDF_VERSION / IDF_PYTHON_ENV_PATH
     (export.sh/export.bat also set these; export.bat refuses to run from bash);
  3. asks `idf_tools.py export --format key-value` for the toolchain PATH and
     merges it;
  4. drops MSYS-style (`/`-prefixed) PATH entries -- the native toolchain
     cannot resolve them.

Usage:
    python scripts/idf_run.py -B build-unified-echoear \
        -D SDKCONFIG=sdkconfig.echoear-2st build
"""

import os
import subprocess
import sys

# Machine-specific: adjust these three if the IDF install moves.
IDF_PATH = r"C:\esp\v6.0.2\esp-idf"
IDF_TOOLS_PATH = r"C:\Users\ma139\.espressif"
IDF_PYTHON = r"C:\Users\ma139\.espressif\python_env\idf6.0_py3.12_env\Scripts\python.exe"
ESP_IDF_VERSION = "6.0.2"

# Set by `idf_tools.py export`; read below rather than hard-coded.
_TOOLS_EXPORT = os.path.join(IDF_PATH, "tools", "idf_tools.py")


# A dedicated, short temp dir for the native (non-MSYS) toolchain.  Keeping the
# gcc/cc1 response-file temp files off the crowded default %LOCALAPPDATA%\Temp
# reduces intermittent "could not open temporary response file" failures that
# otherwise show up mid-build under parallel compilation.
_BUILD_TMP = r"C:\wb_tmp"


def clean_environ():
    env = {
        key: value
        for key, value in os.environ.items()
        if not (key in ("MSYSTEM", "MSYS") or key.startswith("MINGW"))
    }
    env["IDF_PATH"] = IDF_PATH
    env["IDF_TOOLS_PATH"] = IDF_TOOLS_PATH
    env["ESP_IDF_VERSION"] = ESP_IDF_VERSION
    env["IDF_PYTHON_ENV_PATH"] = os.path.dirname(os.path.dirname(IDF_PYTHON))
    # Point TEMP/TMP/TMPDIR at a short, native-Windows directory.  The native
    # xtensa toolchain and cmake write temp files here; a short path with no
    # MSYS mapping avoids both path-parsing oddities and contention on the
    # default (very busy) Temp folder.
    tmp = _BUILD_TMP
    try:
        os.makedirs(tmp, exist_ok=True)
    except OSError:
        tmp = os.environ.get("TEMP") or os.environ.get("TMP") or r"C:\Users\ma139\AppData\Local\Temp"
    env["TEMP"] = tmp
    env["TMP"] = tmp
    env["TMPDIR"] = tmp
    return env


def exported_vars(env):
    """Ask idf_tools.py for EVERY exported variable (PATH, ESP_ROM_ELF_DIR, ...).

    idf_tools.py emits several KEY=VALUE pairs; an earlier version of this script
    only captured the PATH= line, which silently dropped ESP_ROM_ELF_DIR and
    produced a noisy "ESP_ROM_ELF_DIR environment variable is not defined" gdbinit
    warning during configure.  We now apply each pair so the native toolchain sees
    the same environment `export.bat` would set.

    This call requires a clean env (MSYSTEM stripped) -- otherwise idf_tools.py
    prints "MSys/Mingw is not supported" and exits non-zero.
    """
    proc = subprocess.run(
        [IDF_PYTHON, _TOOLS_EXPORT, "export", "--format", "key-value"],
        cwd=IDF_PATH,
        env=env,
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        sys.stderr.write(proc.stdout + proc.stderr + "\n")
        raise SystemExit("idf_tools.py export failed")
    out = {}
    for line in proc.stdout.splitlines():
        if "=" in line and not line.startswith(" "):
            key, _, value = line.partition("=")
            out[key] = value
    if "PATH" not in out:
        raise SystemExit("idf_tools.py export produced no PATH line")
    return out


def merge_path(env, exported):
    # Apply every exported variable first (e.g. ESP_ROM_ELF_DIR), then fix PATH.
    for key, value in exported.items():
        if key == "PATH":
            continue
        env[key] = value
    extra = exported["PATH"]
    # MSYS-style entries start with a single "/" (e.g. /usr/bin, /mingw64/bin).
    # The native xtensa toolchain and cmake cannot resolve them, and they shadow
    # real tools (a bash `link.exe`, `find.exe`) with the wrong implementations.
    kept = [p for p in os.environ.get("PATH", "").split(os.pathsep) if not p.startswith("/")]
    merged = extra.split(os.pathsep) + kept
    seen = set()
    out = []
    for entry in merged:
        key = entry.rstrip("\\/").lower()
        if not entry or key in seen:
            continue
        seen.add(key)
        out.append(entry)
    env["PATH"] = os.pathsep.join(out)


def _d_value(argv, i):
    """Return the ``X=Y`` payload of a ``-D`` flag given as ``-D`` or ``-DX=Y``.

    idf.py accepts both ``-D FOO=bar`` (two argv elements) and ``-DFOO=bar``
    (one), so normalise both before testing the payload.
    """
    a = argv[i]
    if a == "-D" and i + 1 < len(argv):
        return argv[i + 1]
    if a.startswith("-D") and len(a) > 2:
        return a[2:]
    return None


def derive_profile(argv):
    """Best-effort board profile from the CLI.

    The Component Manager selects ``dependencies.lock.<profile>`` and the early
    ``cmake -P`` requirements pass only reads ``MACLAW_PROFILE`` from the
    ENVIRONMENT (command-line ``-D`` cache entries do not survive that pass --
    see the root CMakeLists). Derive it so a plain
    ``idf_run.py -B build-unified-echoear -D SDKCONFIG=sdkconfig.echoear-2st
    build`` resolves the right lock without the caller remembering the env var.
    Without it the manager falls back to a generic/wrong lock, tries to solve
    from scratch, and prunes managed components -- which is both incorrect and
    trips the bulk-delete guard.

    ``sdkconfig.<profile>`` is authoritative: the build directories are
    abbreviated (``build-unified-echoear``) and do NOT equal the full profile
    name (``echoear-2st``), so the build-dir name must not be trusted.
    """
    for i, a in enumerate(argv):
        if a in ("-P", "--profile") and i + 1 < len(argv):
            return argv[i + 1]
        if a.startswith("--profile="):
            return a.split("=", 1)[1]
    for i, _ in enumerate(argv):
        val = _d_value(argv, i)
        if val and val.startswith("SDKCONFIG=sdkconfig."):
            return val.split("sdkconfig.", 1)[1]
    return None


def main(argv):
    env = clean_environ()
    profile = derive_profile(argv)
    if profile:
        env["MACLAW_PROFILE"] = profile
        already = any(
            (_d_value(argv, i) or "").startswith("MACLAW_PROFILE=")
            for i, _ in enumerate(argv)
        )
        if not already:
            argv = ["-D", "MACLAW_PROFILE=" + profile] + list(argv)
    merge_path(env, exported_vars(env))
    cmd = [IDF_PYTHON, os.path.join(IDF_PATH, "tools", "idf.py")] + list(argv)
    return subprocess.run(cmd, env=env, cwd=os.getcwd()).returncode


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
