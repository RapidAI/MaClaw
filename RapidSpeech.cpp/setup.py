#!/usr/bin/env python3
import glob
import os
import platform
import re
import shutil
import sys
from pathlib import Path
from setuptools.command.build_ext import build_ext
import setuptools

def is_macos():
    return platform.system() == "Darwin"


def is_windows():
    return platform.system() == "Windows"


def is_linux():
    return platform.system() == "Linux"


def is_arm64():
    return platform.machine() in ["arm64", "aarch64"]


def is_x86():
    return platform.machine() in ["i386", "i686", "x86_64"]

def is_for_pypi():
    ans = os.environ.get("RAPIDSPEECH_IS_FOR_PYPI", None)
    return ans is not None

def read_long_description():
    with open("README.md", encoding="utf8") as f:
        readme = f.read()
    return readme

def need_split_package():
    ans = os.environ.get("RAPIDSPEECH_SPLIT_PYTHON_PACKAGE", None)
    return ans is not None

def get_binaries():
    binaries = [
        "rs-asr-offline",
        "rapidspeech"
    ]

    if is_windows():
        binaries += [
            "librapidspeech.dll",
        ]

    return binaries


def cmake_extension(name, *args, **kwargs) -> setuptools.Extension:
    kwargs["language"] = "c++"
    sources = []
    return setuptools.Extension(name, sources, *args, **kwargs)

class BuildExtension(build_ext):
    def build_extension(self, ext: setuptools.extension.Extension):
        # build/temp.linux-x86_64-3.8
        os.makedirs(self.build_temp, exist_ok=True)

        # build/lib.linux-x86_64-3.8
        os.makedirs(self.build_lib, exist_ok=True)

        out_bin_dir = Path(self.build_lib) / "rapidspeech"
        install_dir = Path(self.build_lib).resolve() / "rapidspeech"

        rapidspeech_dir = Path(__file__).parent.resolve()

        cmake_args = os.environ.get("RAPIDSPEECH_CMAKE_ARGS", "")
        make_args = os.environ.get("RAPIDSPEECH_MAKE_ARGS", "")
        system_make_args = os.environ.get("MAKEFLAGS", "")

        if cmake_args == "":
            cmake_args = "-DCMAKE_BUILD_TYPE=Release"

        extra_cmake_args = ""
        if not need_split_package():
            extra_cmake_args += f" -DCMAKE_INSTALL_PREFIX={install_dir} "
        extra_cmake_args += " -DRS_ENABLE_PYTHON=ON "


        if "PYTHON_EXECUTABLE" not in cmake_args:
            print(f"Setting PYTHON_EXECUTABLE to {sys.executable}")
            cmake_args += f" -DPYTHON_EXECUTABLE={sys.executable}"
        else:
            extra_cmake_args += " -DPYTHON_EXECUTABLE=$(which python3)"

        # putting `cmake_args` from env variable ${rapidspeech_CMAKE_ARGS} last,
        # so they can onverride the "defaults" stored in `extra_cmake_args`
        cmake_args = extra_cmake_args + cmake_args

        if is_windows():
            if not need_split_package():
                build_cmd = f"""
             cmake {cmake_args} -B {self.build_temp} -S {rapidspeech_dir}
             cmake --build {self.build_temp} --target install --config Release -- -m:2
                """
            else:
                build_cmd = f"""
             cmake {cmake_args} -B {self.build_temp} -S {rapidspeech_dir}
             cmake --build {self.build_temp} --target rapidspeech --config Release -- -m:2
                """

            print(f"build command is:\n{build_cmd}")
            ret = os.system(
                f"cmake {cmake_args} -B {self.build_temp} -S {rapidspeech_dir}"
            )
            if ret != 0:
                raise Exception("Failed to configure sherpa")

            if not need_split_package():
                ret = os.system(
                    f"cmake --build {self.build_temp} --target install --config Release -- -m:2"  # noqa
                )
            else:
                ret = os.system(
                    f"cmake --build {self.build_temp} --target rapidspeech --config Release -- -m:2"  # noqa
                )
            if ret != 0:
                raise Exception("Failed to build and install sherpa")
        else:
            if make_args == "" and system_make_args == "":
                print("for fast compilation, run:")
                print('export RAPIDSPEECH_MAKE_ARGS="-j"; python setup.py install')
                print('Setting make_args to "-j4"')
                make_args = "-j4"

            if "-G Ninja" in cmake_args:
                if not need_split_package():
                    build_cmd = f"""
                        cd {self.build_temp}
                        cmake {cmake_args} {rapidspeech_dir}
                        ninja {make_args} install
                    """
                else:
                    build_cmd = f"""
                        cd {self.build_temp}
                        cmake {cmake_args} {rapidspeech_dir}
                        ninja {make_args} rapidspeech
                    """
            else:
                if not need_split_package():
                    build_cmd = f"""
                        cd {self.build_temp}

                        cmake {cmake_args} {rapidspeech_dir}

                        make {make_args} install/strip
                    """
                else:
                    build_cmd = f"""
                        cd {self.build_temp}

                        cmake {cmake_args} {rapidspeech_dir}

                        make {make_args} rapidspeech
                    """
            print(f"build command is:\n{build_cmd}")

            ret = os.system(build_cmd)
            if ret != 0:
                raise Exception(
                    "\nBuild rapidspeech failed. Please check the error message.\n"
                    "You can ask for help by creating an issue on GitHub.\n"
                    "\nClick:\n\thttps://github.com/RapidAI/RapidSpeech.cpp/issues/new\n"  # noqa
                )


        dst = os.path.join(f"{self.build_lib}")
        os.system(f"mkdir {dst}")
        os.system(f"dir {dst}")

        ext = "pyd" if sys.platform.startswith("win") else "so"
        pattern = os.path.join(self.build_temp, "**", f"rapidspeech.*.{ext}")
        matches = glob.glob(pattern, recursive=True)
        print("matches", list(matches))

        for f in matches:
            print('@'*100)
            print(f, os.path.join(f"{self.build_temp}"))
            shutil.copy(f"{f}", dst)
            os.system(f"dir {dst}")



try:
    from wheel.bdist_wheel import bdist_wheel as _bdist_wheel

    class bdist_wheel(_bdist_wheel):
        def finalize_options(self):
            _bdist_wheel.finalize_options(self)
            # In this case, the generated wheel has a name in the form
            # sherpa-xxx-pyxx-none-any.whl
            if is_for_pypi() and not is_macos():
                self.root_is_pure = True
            else:
                # The generated wheel has a name ending with
                # -linux_x86_64.whl
                self.root_is_pure = False

except ImportError:
    bdist_wheel = None

package_name = "rapidspeech"


def get_binaries_to_install():
    if need_split_package():
        return None

    cmake_args = os.environ.get("RAPIDSPEECH_CMAKE_ARGS", "")

    bin_dir = Path("build")
    bin_dir.mkdir(parents=True, exist_ok=True)
    suffix = ".exe" if is_windows() else ""

    binaries = get_binaries()

    exe = []
    for f in binaries:
        suffix = "" if (".dll" in f or ".lib" in f) else suffix
        t = bin_dir / (f + suffix)
        exe.append(str(t))
    return exe


setuptools.setup(
    name=package_name,
    python_requires=">=3.7",
    version="v1.0",
    author="lovemefan",
    author_email="lovemefan@outlook.com",
    packages=["rapidspeech"],
    data_files=(
        [
            (
                ("Scripts", get_binaries_to_install())
                if is_windows()
                else ("bin", get_binaries_to_install())
            )
        ]
        if get_binaries_to_install()
        else None
    ),
    url="https://github.com/RapidAI/RapidSpeech.cpp",
    long_description=read_long_description(),
    long_description_content_type="text/markdown",
    ext_modules=[cmake_extension("rapidspeech")],
    cmdclass={"build_ext": BuildExtension, "bdist_wheel": bdist_wheel},
    zip_safe=False,
    classifiers=[
        "Programming Language :: C++",
        "Programming Language :: Python",
        "Topic :: Scientific/Engineering :: Artificial Intelligence",
    ],
    license="Apache licensed, as found in the LICENSE file",
)
