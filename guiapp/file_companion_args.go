package guiapp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const (
	desktopLaunchMain          = "main"
	desktopLaunchFileCompanion = "file-companion"
	fileCompanionOpenArg       = "open-file"
)

// desktopLaunch classifies one process argv. Mode is "main" or "file-companion".
// StartHidden is the Darwin document latch: true only when the filtered user
// args are empty on darwin, so applicationWillFinishLaunching does not order
// the main window front before the launch-file batch hook runs.
type desktopLaunch struct {
	Mode        string
	LockID      string
	StartHidden bool
	Paths       []string
	Explicit    bool
}

// fileCompanionLockID is the companion single-instance id. It appends a suffix
// to the historical main-app id and does not change singleInstanceUniqueID.
func fileCompanionLockID() string {
	return singleInstanceUniqueID() + "-file-companion"
}

func classifyDesktopLaunch(args []string) desktopLaunch {
	return classifyDesktopLaunchForOS(args, goruntime.GOOS)
}

// classifyDesktopLaunchForOS is the shipped argv classifier. goos selects the
// Darwin latch only; file existence checks use the host filesystem so a
// Windows test can still assert StartHidden for darwin.
func classifyDesktopLaunchForOS(args []string, goos string) desktopLaunch {
	user := desktopUserArgs(args)
	filtered := filterDesktopLaunchNoise(user)
	main := desktopLaunch{Mode: desktopLaunchMain, LockID: singleInstanceUniqueID()}
	if len(filtered) == 0 {
		// Finder, Dock, and cold Open With arrive with an empty tail or only
		// a legacy -psn_* serial. They stay on the main lock. The latch hides
		// that process until the post-launch batch hook; it is not used for
		// open-file, autostart, or any non-darwin launch.
		main.StartHidden = goos == "darwin"
		return main
	}
	if filtered[0] == fileCompanionOpenArg {
		return desktopLaunch{
			Mode:     desktopLaunchFileCompanion,
			LockID:   fileCompanionLockID(),
			Paths:    pathsAfterOpenFile(user),
			Explicit: true,
		}
	}
	if desktopArgsAreKnownSubcommand(filtered) {
		return main
	}
	if allExistingNonDirectoryFiles(filtered) {
		return desktopLaunch{
			Mode:   desktopLaunchFileCompanion,
			LockID: fileCompanionLockID(),
			Paths:  append([]string(nil), filtered...),
		}
	}
	return main
}

func desktopUserArgs(args []string) []string {
	if len(args) <= 1 {
		return nil
	}
	return args[1:]
}

// filterDesktopLaunchNoise drops empty strings and legacy Launch Services
// -psn* serials. argv[0] is never included.
func filterDesktopLaunchNoise(user []string) []string {
	if len(user) == 0 {
		return nil
	}
	out := make([]string, 0, len(user))
	for _, arg := range user {
		if arg == "" || strings.HasPrefix(arg, "-psn") {
			continue
		}
		out = append(out, arg)
	}
	return out
}

// pathsAfterOpenFile keeps every non-empty argument after the open-file token.
// A missing or directory path stays in the list so the companion can show an
// error tab instead of dropping the batch. -psn noise before the token is
// ignored; arguments after the token are paths, not launch serials.
func pathsAfterOpenFile(user []string) []string {
	seen := false
	var paths []string
	for _, arg := range user {
		if !seen {
			if arg == "" || strings.HasPrefix(arg, "-psn") {
				continue
			}
			if arg == fileCompanionOpenArg {
				seen = true
			}
			continue
		}
		if arg == "" {
			continue
		}
		paths = append(paths, arg)
	}
	if paths == nil {
		return []string{}
	}
	return paths
}

func desktopArgsAreKnownSubcommand(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "init", "autostart", "remote-smoke", "generate-mobile-pwa-shell", "generate-android-pwa-shell":
			return true
		}
		lower := strings.ToLower(arg)
		if lower == "tui" || lower == "ui" {
			return true
		}
		if strings.HasPrefix(lower, "maclaw://") {
			return true
		}
	}
	return false
}

func allExistingNonDirectoryFiles(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if strings.ContainsRune(path, 0) {
			return false
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

var errFileCompanionNULPath = errors.New("file companion path contains NUL")

// fileCompanionCanonicalPath is the identity used for grants, dedupe, and the
// session id. Abs, then EvalSymlinks; a missing target keeps the absolute
// path. Windows folds the entire path with strings.ToLower, not only the drive.
func fileCompanionCanonicalPath(path string) (string, error) {
	return fileCompanionCanonicalPathForOS(path, goruntime.GOOS)
}

func fileCompanionCanonicalPathForOS(path, goos string) (string, error) {
	if strings.ContainsRune(path, 0) {
		return "", errFileCompanionNULPath
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil || resolved == "" {
		resolved = abs
	}
	if goos == "windows" {
		resolved = strings.ToLower(resolved)
	}
	return resolved, nil
}

// fileCompanionSessionID is fc_ plus the first 16 bytes of SHA-256(canonical),
// hex-encoded (32 characters).
func fileCompanionSessionID(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return "fc_" + hex.EncodeToString(sum[:16])
}
