package skill

// requirement_helpers.go provides the low-level OS interaction functions used
// by the built-in Checkers and Fixers. Separated from checker logic so they
// can be overridden in tests via the exported function variables.

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// --- Overridable function variables for testability ---

var commandExists = defaultCommandExists

func defaultCommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

var envLookup = defaultEnvLookup

func defaultEnvLookup(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

var findPythonExecutable = defaultFindPython

func defaultFindPython() string {
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("python3"); err == nil {
			return "python3"
		}
	}
	if _, err := exec.LookPath("python"); err == nil {
		return "python"
	}
	return ""
}

var checkPipInstalled = defaultCheckPipInstalled

func defaultCheckPipInstalled(python, name string) bool {
	cmd := exec.Command(python, "-m", "pip", "show", name)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	return cmd.Run() == nil
}

var installPipPkg = defaultInstallPipPkg

func defaultInstallPipPkg(python, pkg string) error {
	cmd := exec.Command(python, "-m", "pip", "install", "--quiet", pkg)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pip install %s failed: %v\n%s", pkg, err, strings.TrimSpace(string(out)))
	}
	log.Printf("[requirement] pip install %s success", pkg)
	return nil
}

// checkNpmInstalledInDir checks if an npm package is installed, optionally
// scoped to a specific directory. Checks local (dir or cwd) first, then global.
var checkNpmInstalledInDir = defaultCheckNpmInstalledInDir

func defaultCheckNpmInstalledInDir(name, dir string) bool {
	cmd := exec.Command("npm", "list", name, "--depth=0")
	if dir != "" {
		cmd.Dir = dir
	}
	if cmd.Run() == nil {
		return true
	}
	cmd = exec.Command("npm", "list", "-g", name, "--depth=0")
	return cmd.Run() == nil
}

// installNpmPkgInDir installs an npm package, optionally in a specific
// directory. When dir is non-empty, `npm install` runs with Dir set,
// installing locally to that directory (no elevated permissions needed).
var installNpmPkgInDir = defaultInstallNpmPkgInDir

func defaultInstallNpmPkgInDir(pkg, dir string) error {
	cmd := exec.Command("npm", "install", "--silent", pkg)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("npm install %s failed: %v\n%s", pkg, err, strings.TrimSpace(string(out)))
	}
	log.Printf("[requirement] npm install %s success (dir=%q)", pkg, dir)
	return nil
}
