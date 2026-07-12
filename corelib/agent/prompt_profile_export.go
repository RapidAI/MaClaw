package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// PromptProfileExportSchemaVersion bumps when export shape changes incompatibly.
const PromptProfileExportSchemaVersion = 1

// PromptProfileExport is a portable, mergeable snapshot of adaptive-prompt stats
// for multi-instance ops (CLI export → scp/rsync → merge on hub/ops host).
// It intentionally avoids user/tenant labels (process-level counters only).
type PromptProfileExport struct {
	SchemaVersion int    `json:"schema_version"`
	ExportedAt    string `json:"exported_at"`
	Host          string `json:"host,omitempty"`
	// InstanceID is an optional operator tag (hostname-pid or free-form).
	InstanceID string `json:"instance_id,omitempty"`
	// SourcePath is set when the export was loaded from disk.
	SourcePath string `json:"source_path,omitempty"`
	// Stats is the process (+ durable) adaptive prompt counters.
	Stats PromptProfileStats `json:"stats"`
	// Summary is the one-line FormatLine at export time.
	Summary string `json:"summary,omitempty"`
	// Env captures operator overrides present at export time.
	Env PromptProfileExportEnv `json:"env,omitempty"`
}

// PromptProfileExportEnv records env knobs that affect adaptive prompt behavior.
type PromptProfileExportEnv struct {
	PromptProfile string `json:"prompt_profile,omitempty"` // raw MACLAW_PROMPT_PROFILE
	LightRetry    string `json:"light_retry,omitempty"`    // raw MACLAW_PROMPT_LIGHT_RETRY
	ForcedProfile string `json:"forced_profile,omitempty"` // light|full when override active
	LightRetryOn  bool   `json:"light_retry_on"`
	ABPercent     int    `json:"ab_percent,omitempty"` // MACLAW_PROMPT_AB_PERCENT
}

// PromptProfileMergeResult is the aggregate of several exports.
type PromptProfileMergeResult struct {
	SchemaVersion int                   `json:"schema_version"`
	MergedAt      string                `json:"merged_at"`
	SourceCount   int                   `json:"source_count"`
	Hosts         []string              `json:"hosts,omitempty"`
	Sources       []string              `json:"sources,omitempty"`
	Stats         PromptProfileStats    `json:"stats"`
	Summary       string                `json:"summary,omitempty"`
	Instances     []PromptProfileExport `json:"instances,omitempty"`
}

// AdaptivePromptHeartbeatStat returns a compact corelib snapshot for Hub
// machine.heartbeat. Nil when no adaptive-prompt activity has been recorded.
func AdaptivePromptHeartbeatStat() *corelib.AdaptivePromptStat {
	st := GetPromptProfileStats()
	out := corelib.AdaptivePromptStat{
		LightTurns:      st.LightTurns,
		FullTurns:       st.FullTurns,
		LightPercent:    st.LightPercent,
		EstTokensSaved:  st.EstTokensSaved,
		LightToolDenies: st.LightToolDenies,
		LightUpgrades:   st.LightUpgrades,
		AbEligibleLight: st.AbEligibleLight,
		AbSampleFull:    st.AbSampleFull,
		LastProfile:     st.LastProfile,
		LastTask:        st.LastTask,
		LastDeniedTool:  st.LastDeniedTool,
		Summary:         st.FormatLine(),
	}
	if out.Empty() {
		return nil
	}
	return &out
}

// BuildPromptProfileExport captures the current process stats as an export.
func BuildPromptProfileExport() PromptProfileExport {
	// Flush debounced durable write so export matches in-memory counters.
	_ = FlushPromptProfileStats()
	st := GetPromptProfileStats()
	host, _ := os.Hostname()
	exp := PromptProfileExport{
		SchemaVersion: PromptProfileExportSchemaVersion,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		Host:          host,
		InstanceID:    fmt.Sprintf("%s-%d", host, os.Getpid()),
		Stats:         st,
		Summary:       st.FormatLine(),
		Env: PromptProfileExportEnv{
			PromptProfile: strings.TrimSpace(os.Getenv(PromptProfileEnvKey)),
			LightRetry:    strings.TrimSpace(os.Getenv(PromptLightRetryEnvKey)),
			LightRetryOn:  LightToolRetryEnabled(),
			ABPercent:     PromptABSamplePercent(),
		},
	}
	if forced, ok := EnvPromptProfileOverride(); ok {
		exp.Env.ForcedProfile = string(forced)
	}
	return exp
}

// PromptProfileExportDir returns the default directory for written export files.
func PromptProfileExportDir() string {
	return filepath.Join(maclawpath.DataDir(), "stats", "exports")
}

// DefaultPromptProfileExportPath returns a timestamped path under ExportDir.
func DefaultPromptProfileExportPath() string {
	ts := time.Now().UTC().Format("20060102T150405Z")
	host, _ := os.Hostname()
	if host == "" {
		host = "host"
	}
	// Sanitize host for filesystem.
	host = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, host)
	return filepath.Join(PromptProfileExportDir(), fmt.Sprintf("prompt_profile_%s_%s.json", host, ts))
}

// WritePromptProfileExport writes exp to path (creates parent dirs).
func WritePromptProfileExport(path string, exp PromptProfileExport) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("export path is empty")
	}
	if exp.SchemaVersion == 0 {
		exp.SchemaVersion = PromptProfileExportSchemaVersion
	}
	if exp.ExportedAt == "" {
		exp.ExportedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadPromptProfileExport reads a single export JSON file.
func LoadPromptProfileExport(path string) (PromptProfileExport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PromptProfileExport{}, err
	}
	var exp PromptProfileExport
	if err := json.Unmarshal(data, &exp); err != nil {
		return PromptProfileExport{}, err
	}
	// Accept raw stats-only files (durable prompt_profile.json) as degenerate exports.
	if exp.SchemaVersion == 0 && exp.ExportedAt == "" && exp.Stats.LightTurns == 0 && exp.Stats.FullTurns == 0 {
		var st PromptProfileStats
		if err2 := json.Unmarshal(data, &st); err2 == nil && (st.LightTurns > 0 || st.FullTurns > 0 || st.EstTokensSaved > 0) {
			exp = PromptProfileExport{
				SchemaVersion: PromptProfileExportSchemaVersion,
				Stats:         st,
				Summary:       st.FormatLine(),
			}
		}
	}
	if exp.SchemaVersion == 0 {
		exp.SchemaVersion = PromptProfileExportSchemaVersion
	}
	exp.SourcePath = path
	return exp, nil
}

// MergePromptProfileExports sums counters across exports for fleet-level views.
// last_* fields come from the export with the newest ExportedAt (or last in list).
func MergePromptProfileExports(exports []PromptProfileExport) PromptProfileMergeResult {
	out := PromptProfileMergeResult{
		SchemaVersion: PromptProfileExportSchemaVersion,
		MergedAt:      time.Now().UTC().Format(time.RFC3339),
		SourceCount:   len(exports),
		Instances:     append([]PromptProfileExport(nil), exports...),
	}
	if len(exports) == 0 {
		return out
	}
	hostSet := map[string]struct{}{}
	var newestAt time.Time
	var newest PromptProfileExport
	for i, exp := range exports {
		st := exp.Stats
		out.Stats.LightTurns += st.LightTurns
		out.Stats.FullTurns += st.FullTurns
		out.Stats.EstTokensSaved += st.EstTokensSaved
		out.Stats.LightToolDenies += st.LightToolDenies
		out.Stats.LightUpgrades += st.LightUpgrades
		out.Stats.ByTask = mergeInt64Maps(out.Stats.ByTask, st.ByTask)
		out.Stats.ByDeniedTool = mergeInt64Maps(out.Stats.ByDeniedTool, st.ByDeniedTool)
		if h := strings.TrimSpace(exp.Host); h != "" {
			hostSet[h] = struct{}{}
		}
		if p := strings.TrimSpace(exp.SourcePath); p != "" {
			out.Sources = append(out.Sources, p)
		}
		at := parseExportTime(exp.ExportedAt)
		if i == 0 || at.After(newestAt) {
			newestAt = at
			newest = exp
		}
	}
	for h := range hostSet {
		out.Hosts = append(out.Hosts, h)
	}
	sort.Strings(out.Hosts)
	if total := out.Stats.LightTurns + out.Stats.FullTurns; total > 0 {
		out.Stats.LightPercent = int((out.Stats.LightTurns * 100) / total)
	}
	// Carry "last" fields from newest export for operator context.
	out.Stats.LastProfile = newest.Stats.LastProfile
	out.Stats.LastAt = newest.Stats.LastAt
	out.Stats.LastTask = newest.Stats.LastTask
	out.Stats.LastReason = newest.Stats.LastReason
	out.Stats.LastDeniedTool = newest.Stats.LastDeniedTool
	out.Stats.LastUpgradeReason = newest.Stats.LastUpgradeReason
	out.Stats.LastFullTokens = newest.Stats.LastFullTokens
	out.Stats.LastLightTokens = newest.Stats.LastLightTokens
	out.Stats.LastSavedTokens = newest.Stats.LastSavedTokens
	out.Summary = out.Stats.FormatLine()
	return out
}

func mergeInt64Maps(dst, src map[string]int64) map[string]int64 {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]int64, len(src))
	}
	for k, v := range src {
		dst[k] += v
	}
	return dst
}

func parseExportTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return time.Time{}
}
