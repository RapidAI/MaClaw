package skill

// requirement_command_fixer.go provides automatic installation for known system
// commands (ffmpeg, pandoc, tesseract, graphviz, etc.) using platform-native
// package managers.
//
// Design:
//   - Only attempts installation for commands in the knownCommandInstallRecipes map
//   - Unknown commands return an error (no blind install attempts)
//   - Platform detection uses runtime.GOOS + distro detection for Linux
//   - 60-second timeout per install command
//   - All install commands are non-interactive (--yes / -y / --silent flags)
//   - Failure messages include the specific install command that failed,
//     giving both users and LLM self-repair clear remediation paths

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// CommandFixer attempts to install known system commands using the platform's
// package manager. Registered into DefaultRegistry alongside PipFixer/NpmFixer.
type CommandFixer struct{}

func (f *CommandFixer) Type() string { return "command" }

func (f *CommandFixer) Fix(req Requirement) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return fmt.Errorf("empty command name")
	}

	recipe, ok := knownCommandInstallRecipes[strings.ToLower(name)]
	if !ok {
		// Not a known installable command. Return nil so the violation passes
		// through to re-check with its original message intact. The re-check will
		// find the command still missing and preserve the clean error message.
		return nil
	}

	installCmd := recipe.commandForPlatform()
	if installCmd == "" {
		// Known command but no install method for this platform (e.g., no winget,
		// no brew, or no sudo). Return an error so formatFixFailureMessage
		// generates a message with platformInstallHint (which includes ManualHint).
		return fmt.Errorf("no automatic installer available on this system")
	}

	log.Printf("[requirement-command-fixer] installing %q via: %s", name, installCmd)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	parts := strings.Fields(installCmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty install command for %q", name)
	}
	cmd := coretool.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		if len(outStr) > 300 {
			outStr = outStr[:300] + "...[truncated]"
		}
		return fmt.Errorf("install command failed: %s\n%s\nManual fix: %s", installCmd, outStr, recipe.ManualHint)
	}

	// Verify the command is now available
	if _, lookErr := exec.LookPath(name); lookErr != nil {
		return fmt.Errorf("install command succeeded but %q still not found on PATH; you may need to restart your terminal", name)
	}

	log.Printf("[requirement-command-fixer] successfully installed %q", name)
	return nil
}

// commandInstallRecipe describes how to install a system command on each platform.
type commandInstallRecipe struct {
	Windows     string // winget/choco command
	MacOS       string // brew command
	LinuxApt    string // apt command (Debian/Ubuntu)
	LinuxDnf    string // dnf command (Fedora/RHEL)
	LinuxPacman string // pacman command (Arch)
	ManualHint  string // human-readable install instruction
}

// commandForPlatform returns the appropriate install command for the current OS.
func (r commandInstallRecipe) commandForPlatform() string {
	switch runtime.GOOS {
	case "windows":
		if r.Windows != "" && wingetAvailable() {
			return r.Windows
		}
		return ""
	case "darwin":
		if r.MacOS != "" && brewAvailable() {
			return r.MacOS
		}
		return ""
	case "linux":
		// On Linux, all install commands require sudo. Check if sudo is available
		// and can run non-interactively (NOPASSWD or cached credentials).
		// If sudo requires a password prompt, skip auto-install to avoid blocking.
		if !sudoNonInteractiveAvailable() {
			return ""
		}
		distro := detectLinuxDistro()
		switch distro {
		case "debian", "ubuntu":
			if r.LinuxApt != "" && aptAvailable() {
				return r.LinuxApt
			}
		case "fedora", "rhel", "centos":
			if r.LinuxDnf != "" && dnfAvailable() {
				return r.LinuxDnf
			}
		case "arch", "manjaro":
			if r.LinuxPacman != "" && pacmanAvailable() {
				return r.LinuxPacman
			}
		default:
			// Try apt first (most common), then dnf
			if r.LinuxApt != "" && aptAvailable() {
				return r.LinuxApt
			}
			if r.LinuxDnf != "" && dnfAvailable() {
				return r.LinuxDnf
			}
		}
		return ""
	}
	return ""
}

// knownCommandInstallRecipes maps command names to their platform-specific
// install recipes. Only commonly-needed commands for skill execution are included.
var knownCommandInstallRecipes = map[string]commandInstallRecipe{
	"ffmpeg": {
		Windows:     "winget install --id Gyan.FFmpeg --accept-source-agreements --accept-package-agreements -e",
		MacOS:       "brew install ffmpeg",
		LinuxApt:    "sudo apt-get install -y ffmpeg",
		LinuxDnf:    "sudo dnf install -y ffmpeg",
		LinuxPacman: "sudo pacman -S --noconfirm ffmpeg",
		ManualHint:  "Install ffmpeg from https://ffmpeg.org/download.html",
	},
	"pandoc": {
		Windows:     "winget install --id JohnMacFarlane.Pandoc --accept-source-agreements --accept-package-agreements -e",
		MacOS:       "brew install pandoc",
		LinuxApt:    "sudo apt-get install -y pandoc",
		LinuxDnf:    "sudo dnf install -y pandoc",
		LinuxPacman: "sudo pacman -S --noconfirm pandoc",
		ManualHint:  "Install pandoc from https://pandoc.org/installing.html",
	},
	"tesseract": {
		Windows:     "winget install --id UB-Mannheim.TesseractOCR --accept-source-agreements --accept-package-agreements -e",
		MacOS:       "brew install tesseract",
		LinuxApt:    "sudo apt-get install -y tesseract-ocr",
		LinuxDnf:    "sudo dnf install -y tesseract",
		LinuxPacman: "sudo pacman -S --noconfirm tesseract",
		ManualHint:  "Install tesseract from https://github.com/tesseract-ocr/tesseract",
	},
	"dot": {
		Windows:     "winget install --id Graphviz.Graphviz --accept-source-agreements --accept-package-agreements -e",
		MacOS:       "brew install graphviz",
		LinuxApt:    "sudo apt-get install -y graphviz",
		LinuxDnf:    "sudo dnf install -y graphviz",
		LinuxPacman: "sudo pacman -S --noconfirm graphviz",
		ManualHint:  "Install graphviz from https://graphviz.org/download/",
	},
	"graphviz": {
		Windows:     "winget install --id Graphviz.Graphviz --accept-source-agreements --accept-package-agreements -e",
		MacOS:       "brew install graphviz",
		LinuxApt:    "sudo apt-get install -y graphviz",
		LinuxDnf:    "sudo dnf install -y graphviz",
		LinuxPacman: "sudo pacman -S --noconfirm graphviz",
		ManualHint:  "Install graphviz from https://graphviz.org/download/",
	},
	"magick": {
		Windows:     "winget install --id ImageMagick.ImageMagick --accept-source-agreements --accept-package-agreements -e",
		MacOS:       "brew install imagemagick",
		LinuxApt:    "sudo apt-get install -y imagemagick",
		LinuxDnf:    "sudo dnf install -y ImageMagick",
		LinuxPacman: "sudo pacman -S --noconfirm imagemagick",
		ManualHint:  "Install ImageMagick from https://imagemagick.org/script/download.php",
	},
	"convert": {
		Windows:     "winget install --id ImageMagick.ImageMagick --accept-source-agreements --accept-package-agreements -e",
		MacOS:       "brew install imagemagick",
		LinuxApt:    "sudo apt-get install -y imagemagick",
		LinuxDnf:    "sudo dnf install -y ImageMagick",
		LinuxPacman: "sudo pacman -S --noconfirm imagemagick",
		ManualHint:  "Install ImageMagick from https://imagemagick.org/script/download.php",
	},
	"wkhtmltopdf": {
		Windows:    "winget install --id wkhtmltopdf.wkhtmltopdf --accept-source-agreements --accept-package-agreements -e",
		MacOS:      "brew install --cask wkhtmltopdf",
		LinuxApt:   "sudo apt-get install -y wkhtmltopdf",
		LinuxDnf:   "sudo dnf install -y wkhtmltopdf",
		ManualHint: "Install wkhtmltopdf from https://wkhtmltopdf.org/downloads.html",
	},
	"git": {
		Windows:     "winget install --id Git.Git --accept-source-agreements --accept-package-agreements -e",
		MacOS:       "brew install git",
		LinuxApt:    "sudo apt-get install -y git",
		LinuxDnf:    "sudo dnf install -y git",
		LinuxPacman: "sudo pacman -S --noconfirm git",
		ManualHint:  "Install git from https://git-scm.com/downloads",
	},
	"curl": {
		Windows:     "winget install --id cURL.cURL --accept-source-agreements --accept-package-agreements -e",
		MacOS:       "brew install curl",
		LinuxApt:    "sudo apt-get install -y curl",
		LinuxDnf:    "sudo dnf install -y curl",
		LinuxPacman: "sudo pacman -S --noconfirm curl",
		ManualHint:  "Install curl from https://curl.se/download.html",
	},
	"jq": {
		Windows:     "winget install --id jqlang.jq --accept-source-agreements --accept-package-agreements -e",
		MacOS:       "brew install jq",
		LinuxApt:    "sudo apt-get install -y jq",
		LinuxDnf:    "sudo dnf install -y jq",
		LinuxPacman: "sudo pacman -S --noconfirm jq",
		ManualHint:  "Install jq from https://jqlang.github.io/jq/download/",
	},
	"7z": {
		Windows:     "winget install --id 7zip.7zip --accept-source-agreements --accept-package-agreements -e",
		MacOS:       "brew install p7zip",
		LinuxApt:    "sudo apt-get install -y p7zip-full",
		LinuxDnf:    "sudo dnf install -y p7zip",
		LinuxPacman: "sudo pacman -S --noconfirm p7zip",
		ManualHint:  "Install 7-Zip from https://7-zip.org/",
	},
}

// --- Platform detection helpers ---

var wingetAvailableCache *bool

func wingetAvailable() bool {
	if wingetAvailableCache != nil {
		return *wingetAvailableCache
	}
	_, err := exec.LookPath("winget")
	result := err == nil
	wingetAvailableCache = &result
	return result
}

func brewAvailable() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}

func aptAvailable() bool {
	_, err := exec.LookPath("apt-get")
	return err == nil
}

func dnfAvailable() bool {
	_, err := exec.LookPath("dnf")
	return err == nil
}

func pacmanAvailable() bool {
	_, err := exec.LookPath("pacman")
	return err == nil
}

// sudoNonInteractiveAvailable returns true if `sudo -n true` succeeds,
// meaning sudo can run without prompting for a password (either NOPASSWD
// is configured or a recent sudo credential cache exists).
// Returns false if sudo is not available or requires a password prompt.
func sudoNonInteractiveAvailable() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return false
	}
	// sudo -n = non-interactive; exits 1 if password would be required.
	cmd := exec.Command("sudo", "-n", "true")
	return cmd.Run() == nil
}

// detectLinuxDistro returns a lowercase distro identifier by parsing /etc/os-release.
func detectLinuxDistro() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	content := strings.ToLower(string(data))
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "id=") {
			id := strings.TrimPrefix(line, "id=")
			id = strings.Trim(id, `"'`)
			return id
		}
	}
	// Fallback: check ID_LIKE
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "id_like=") {
			idLike := strings.TrimPrefix(line, "id_like=")
			idLike = strings.Trim(idLike, `"'`)
			if strings.Contains(idLike, "debian") || strings.Contains(idLike, "ubuntu") {
				return "debian"
			}
			if strings.Contains(idLike, "fedora") || strings.Contains(idLike, "rhel") {
				return "fedora"
			}
			if strings.Contains(idLike, "arch") {
				return "arch"
			}
		}
	}
	return ""
}
