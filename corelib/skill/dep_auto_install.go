package skill

import (
	"fmt"
	"log"
	"strings"
)

// AutoInstallMissingDependency detects the missing package from a step error,
// installs it via the appropriate package manager (pip/npm), and returns nil on
// success. Returns a non-nil error if:
//   - the error message does not indicate a missing dependency
//   - the package name cannot be extracted
//   - the installation command fails
//
// skillDir is used as the install directory for npm packages (node_modules
// resolution requires packages to be local to the skill). For pip packages
// it is ignored (pip installs globally or per-user).
//
// This is intended to be called by the Skill Runner when a step fails with
// ErrMissingDependency at runtime — supplementing the pre-execution requirement
// check (which only covers explicitly declared dependencies).
func AutoInstallMissingDependency(errMsg, output, command, skillDir string) error {
	return AutoInstallMissingDependencyWithPython(errMsg, output, command, skillDir, "")
}

// AutoInstallMissingDependencyWithPython is like AutoInstallMissingDependency
// but accepts an explicit pythonPath for pip installations. When pythonPath is
// non-empty, packages are installed into that Python's environment (critical
// for skills using a shared Python runtime where the step runs with a managed
// Python different from the system default). When empty, falls back to
// findPythonExecutable().
func AutoInstallMissingDependencyWithPython(errMsg, output, command, skillDir, pythonPath string) error {
	combined := strings.TrimSpace(output + " " + errMsg)
	if combined == "" {
		return fmt.Errorf("empty error message, cannot detect missing dependency")
	}

	// Verify this is actually a missing dependency error.
	lowerCombined := strings.ToLower(combined)
	if !hasSkillMissingDependencyMarker(lowerCombined) {
		return fmt.Errorf("error does not indicate a missing dependency")
	}

	kind := missingDependencyKindFromMessage(combined)
	name := missingDependencyNameFromMessage(combined)
	if name == "" {
		return fmt.Errorf("cannot extract package name from error: %s", truncateUTF8Bytes(errMsg, 200))
	}

	// Resolve the pip/npm install name (handles import→package mappings like
	// cv2→opencv-python, PIL→Pillow, rapidocr→rapidocr-onnxruntime, etc.)
	installName := missingDependencyInstallName(kind, name)
	if installName == "" {
		installName = name
	}

	log.Printf("[dep-auto-install] detected missing %s package: import=%q install=%q python=%q", kind, name, installName, pythonPath)

	switch kind {
	case "python":
		python := strings.TrimSpace(pythonPath)
		if python == "" {
			python = findPythonExecutable()
		}
		if python == "" {
			return fmt.Errorf("python not found, cannot auto-install %s", installName)
		}
		if err := installPipPkgScoped(python, installName); err != nil {
			return fmt.Errorf("auto-install pip package %s failed: %w", installName, err)
		}
		log.Printf("[dep-auto-install] successfully installed python package %s (python=%s)", installName, python)
		return nil

	case "node":
		if err := installNpmPkgInDir(installName, skillDir); err != nil {
			return fmt.Errorf("auto-install npm package %s failed: %w", installName, err)
		}
		log.Printf("[dep-auto-install] successfully installed node package %s (dir=%s)", installName, skillDir)
		return nil

	default:
		return fmt.Errorf("unsupported dependency kind %q for package %q", kind, name)
	}
}

// MissingDependencyInstallNameFromError extracts the pip/npm install package
// name from an error message. Returns ("", "") if not a missing dependency error.
// Returns (kind, installName) on success where kind is "python" or "node".
func MissingDependencyInstallNameFromError(errMsg string) (kind, installName string) {
	lower := strings.ToLower(errMsg)
	if !hasSkillMissingDependencyMarker(lower) {
		return "", ""
	}
	kind = missingDependencyKindFromMessage(errMsg)
	name := missingDependencyNameFromMessage(errMsg)
	if name == "" {
		return kind, ""
	}
	installName = missingDependencyInstallName(kind, name)
	if installName == "" {
		installName = name
	}
	return kind, installName
}
