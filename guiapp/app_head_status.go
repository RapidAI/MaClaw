package guiapp

// app_head_status.go — one-line status of the local intent / tool /
// interrupt heads, logged on every head landing and after bug-report
// diagnostics are cleared (the startup attach lines are gone by then).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// pinnedHeadAssetID is "<file>@<first 12 hex of the pinned sha256>", the ID
// format LocalHead / ToolHead use, or "" when name is not pinned.
func pinnedHeadAssetID(name string) string {
	for _, a := range embedding.ReleaseAssets() {
		if a.Filename == name && len(a.SHA256) >= 12 {
			return name + "@" + strings.ToLower(a.SHA256[:12])
		}
	}
	return ""
}

func fileSHA12(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// HeadStatusLines describes each pinned head: file on disk, pinned id and
// whether it is live. Pure reads; safe from any goroutine.
func (a *App) HeadStatusLines() []string {
	if a == nil {
		return nil
	}
	dir := embedding.DefaultModelsDir()
	var out []string
	for _, as := range embedding.ReleaseAssets() {
		if !embedding.IsHeadKind(as.Kind) {
			continue
		}
		p := filepath.Join(dir, as.Subdir, as.Filename)
		disk := "missing"
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			disk = fmt.Sprintf("present %dB", fi.Size())
			if _, err := os.Stat(p + ".verified"); err == nil {
				disk += " verified"
			}
		}
		attached := "n/a"
		switch as.Kind {
		case "intent_head":
			attached = "no"
			if h, _ := a.localL3Runtime().Active(); h != nil {
				attached = "yes " + h.ID
			}
		case "tool_head":
			attached = "no"
			if a.toolRouter != nil {
				if h := a.toolRouter.ToolHead(); h != nil {
					attached = "yes " + h.ID
				}
			}
		case "interrupt_head":
			attached = "no"
			if emb := a.activeInterruptEmbedder(); emb != nil {
				if m := interruptRelevanceModelFor(emb); m != nil && m != builtinQwen3InterruptRelevance {
					sha := ""
					if strings.HasPrefix(disk, "present") && as.Size < 64<<20 {
						sha = "@" + fileSHA12(p)
					}
					attached = "yes " + as.Filename + sha
				}
			}
		}
		out = append(out, fmt.Sprintf("%s pinned=%s disk=%s attached=%s", as.Kind, pinnedHeadAssetID(as.Filename), disk, attached))
	}
	return out
}

// logHeadStatus logs HeadStatusLines plus the local L3 description.
func (a *App) logHeadStatus(reason string) {
	if a == nil {
		return
	}
	for _, l := range a.HeadStatusLines() {
		log.Printf("[heads] %s: %s", reason, l)
	}
	log.Printf("[heads] %s: intent %s", reason, a.localL3Runtime().Describe())
}
