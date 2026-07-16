package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

const omniparserYOLOFilename = "omniparser-v2.yolow"
const uiaSidecarExeName = "maclaw-uia-sidecar.exe"

// ComputerUseChecks returns local filesystem/config readiness for Computer Use.
// It does not start sidecars or load YOLO (GUI self-check does that).
func ComputerUseChecks(cfg corelib.AppConfig, baseDir string) []Check {
	baseDir = strings.TrimSpace(baseDir)
	out := make([]Check, 0, 4)

	cuOn := true
	if cfg.ComputerUseEnabled != nil {
		cuOn = *cfg.ComputerUseEnabled
	}
	spOn := true
	if cfg.ScreenParsingEnabled != nil {
		spOn = *cfg.ScreenParsingEnabled
	}

	if !cuOn {
		out = append(out, Check{
			ID:      "computer_use.enabled",
			Status:  StatusSkip,
			Message: "Computer Use disabled in config",
			Hint:    "Enable computer_use_enabled or use Settings → Computer Use",
			Detail:  map[string]any{"computer_use_enabled": false},
		})
		return out
	}

	out = append(out, Check{
		ID:      "computer_use.enabled",
		Status:  StatusOK,
		Message: "Computer Use enabled",
		Detail: map[string]any{
			"computer_use_enabled":  true,
			"screen_parsing_enabled": spOn,
			"platform":              runtime.GOOS,
		},
	})

	// OmniParser weights (optional but recommended for text-only models).
	yoloPath := computerUseYOLOPath(baseDir)
	if !spOn {
		out = append(out, Check{
			ID:      "computer_use.omniparser",
			Status:  StatusSkip,
			Message: "screen parsing disabled; observe will use a11y/OCR only",
			Detail:  map[string]any{"path": yoloPath},
		})
	} else if yoloPath == "" {
		out = append(out, Check{
			ID:      "computer_use.omniparser",
			Status:  StatusWarn,
			Message: "could not resolve models directory for OmniParser weights",
			Hint:    "Ensure MaClaw home is writable; download from Settings → Screen Parsing",
		})
	} else if st, err := os.Stat(yoloPath); err != nil {
		out = append(out, Check{
			ID:      "computer_use.omniparser",
			Status:  StatusWarn,
			Message: "OmniParser YOLO weights missing",
			Hint:    "Settings → Screen Parsing → download (~77MB), or first-run will attempt Hub/GitHub preload",
			Detail:  map[string]any{"path": yoloPath},
		})
	} else {
		out = append(out, Check{
			ID:      "computer_use.omniparser",
			Status:  StatusOK,
			Message: "OmniParser YOLO weights present",
			Detail:  map[string]any{"path": yoloPath, "size": st.Size()},
		})
	}

	// Windows: prebuilt UIA sidecar is preferred (PS fallback always exists).
	if runtime.GOOS == "windows" && baseDir != "" {
		exe := filepath.Join(baseDir, "bin", uiaSidecarExeName)
		if st, err := os.Stat(exe); err != nil {
			out = append(out, Check{
				ID:      "computer_use.uia_sidecar",
				Status:  StatusWarn,
				Message: "prebuilt UIA sidecar not found under MaclawBaseDir/bin",
				Hint:    "Installer ships maclaw-uia-sidecar.exe; or run scripts/build_uia_sidecar.ps1 (csc fallback at runtime)",
				Detail:  map[string]any{"expected": exe},
			})
		} else {
			out = append(out, Check{
				ID:      "computer_use.uia_sidecar",
				Status:  StatusOK,
				Message: "UIA sidecar binary present",
				Detail:  map[string]any{"path": exe, "size": st.Size()},
			})
		}
	}

	if runtime.GOOS == "darwin" {
		out = append(out, Check{
			ID:      "computer_use.macos_permissions",
			Status:  StatusInfo,
			Message: "macOS requires Accessibility (+ Screen Recording for capture) for full Computer Use",
			Hint:    "System Settings → Privacy & Security → Accessibility / Screen Recording; use GUI Settings → Run self-check",
		})
	}

	// Log lifecycle policy (diag/csv prune) — informational for ops.
	keep := 10
	if cfg.ComputerUseLogKeepNewest != nil && *cfg.ComputerUseLogKeepNewest > 0 {
		keep = *cfg.ComputerUseLogKeepNewest
	}
	age := 0
	if cfg.ComputerUseLogMaxAgeDays != nil && *cfg.ComputerUseLogMaxAgeDays > 0 {
		age = *cfg.ComputerUseLogMaxAgeDays
	}
	autoPrune := cfg.ComputerUseLogAutoPrune != nil && *cfg.ComputerUseLogAutoPrune
	out = append(out, Check{
		ID:      "computer_use.log_policy",
		Status:  StatusInfo,
		Message: "Computer Use log prune policy",
		Hint:    "Settings → Computer Use → keep newest / max age / startup auto-prune; BatchDelete for multi-select",
		Detail: map[string]any{
			"keep_newest":  keep,
			"max_age_days": age,
			"auto_prune":   autoPrune,
		},
	})

	return out
}

func computerUseYOLOPath(baseDir string) string {
	// Prefer the doctor-evaluated home (baseDir/models) so reports match that install.
	if strings.TrimSpace(baseDir) != "" {
		return filepath.Join(baseDir, "models", omniparserYOLOFilename)
	}
	if dir := embedding.DefaultModelsDir(); dir != "" {
		return filepath.Join(dir, omniparserYOLOFilename)
	}
	return ""
}
