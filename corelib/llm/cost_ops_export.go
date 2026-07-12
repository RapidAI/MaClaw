package llm

// Portable cost-ops export/merge for offline fleet rollups (GUI/TUI/CLI).

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

const CostOpsExportSchemaVersion = 1

// CostOpsExport is a portable snapshot of cost-route + daily fleet stats.
type CostOpsExport struct {
	SchemaVersion int               `json:"schema_version"`
	ExportedAt    string            `json:"exported_at"`
	Host          string            `json:"host,omitempty"`
	InstanceID    string            `json:"instance_id,omitempty"`
	CostRoute     CostRouteStats    `json:"cost_route"`
	DailyFleet    CostDailyFleetView `json:"daily_fleet"`
	Heartbeat     *corelib.CostOpsStat `json:"heartbeat,omitempty"`
	Summary       string            `json:"summary,omitempty"`
	SourcePath    string            `json:"source_path,omitempty"`
	Env           CostOpsExportEnv  `json:"env,omitempty"`
}

// CostOpsExportEnv captures routing env at export time.
type CostOpsExportEnv struct {
	CostRouteMode string `json:"cost_route_mode,omitempty"`
}

// CostOpsMergeResult is a multi-export fleet rollup.
type CostOpsMergeResult struct {
	SchemaVersion int              `json:"schema_version"`
	MergedAt      string           `json:"merged_at"`
	SourceCount   int              `json:"source_count"`
	RouteDecisions int64           `json:"route_decisions"`
	RouteApplied  int64            `json:"route_applied"`
	RouteShadow   int64            `json:"route_shadow"`
	ByTier        map[string]int64 `json:"by_tier,omitempty"`
	DailyCostUSD  float64          `json:"daily_cost_usd"`
	DailyCalls    int              `json:"daily_calls"`
	DailyInstances int             `json:"daily_instances"`
	Summary       string           `json:"summary,omitempty"`
	Hosts         []string         `json:"hosts,omitempty"`
	Instances     []CostOpsExport  `json:"instances,omitempty"`
}

// BuildCostOpsExport captures current process + durable cost stats.
func BuildCostOpsExport() CostOpsExport {
	// Flush debounced writers so export matches in-memory counters.
	_ = FlushCostRouteStats()
	_ = FlushDailyCostPersist()
	host, _ := os.Hostname()
	if host == "" {
		host = "host"
	}
	route := LoadCostRouteStats()
	fleet := LoadCostDailyFleet()
	hb := CostOpsHeartbeatStat()
	parts := make([]string, 0, 2)
	if line := FormatCostRouteStatsLine(); line != "" {
		parts = append(parts, line)
	}
	if line := FormatCostDailyFleetLine(); line != "" {
		parts = append(parts, line)
	}
	return CostOpsExport{
		SchemaVersion: CostOpsExportSchemaVersion,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		Host:          host,
		InstanceID:    fmt.Sprintf("%s-%d", host, os.Getpid()),
		CostRoute:     route,
		DailyFleet:    fleet,
		Heartbeat:     hb,
		Summary:       strings.Join(parts, " | "),
		Env: CostOpsExportEnv{
			CostRouteMode: string(ResolveCostRouteMode()),
		},
	}
}

// CostOpsExportDir is the default directory for written export files.
func CostOpsExportDir() string {
	return filepath.Join(maclawpath.DataDir(), "stats", "exports")
}

// DefaultCostOpsExportPath returns a timestamped path under ExportDir.
func DefaultCostOpsExportPath() string {
	ts := time.Now().UTC().Format("20060102T150405Z")
	host, _ := os.Hostname()
	if host == "" {
		host = "host"
	}
	host = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, host)
	return filepath.Join(CostOpsExportDir(), fmt.Sprintf("cost_ops_%s_%s.json", host, ts))
}

// WriteCostOpsExport writes exp to path (creates parent dirs).
func WriteCostOpsExport(path string, exp CostOpsExport) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("export path is empty")
	}
	if exp.SchemaVersion == 0 {
		exp.SchemaVersion = CostOpsExportSchemaVersion
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

// LoadCostOpsExport reads a single export JSON file.
func LoadCostOpsExport(path string) (CostOpsExport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CostOpsExport{}, err
	}
	var exp CostOpsExport
	if err := json.Unmarshal(data, &exp); err != nil {
		return CostOpsExport{}, err
	}
	if exp.SchemaVersion == 0 {
		exp.SchemaVersion = CostOpsExportSchemaVersion
	}
	exp.SourcePath = path
	return exp, nil
}

// MergeCostOpsExports sums route counters and daily fleet spend across exports.
func MergeCostOpsExports(exports []CostOpsExport) CostOpsMergeResult {
	out := CostOpsMergeResult{
		SchemaVersion: CostOpsExportSchemaVersion,
		MergedAt:      time.Now().UTC().Format(time.RFC3339),
		SourceCount:   len(exports),
		ByTier:        make(map[string]int64),
		Instances:     append([]CostOpsExport(nil), exports...),
	}
	if len(exports) == 0 {
		out.ByTier = nil
		return out
	}
	hostSet := map[string]struct{}{}
	for _, e := range exports {
		out.RouteDecisions += e.CostRoute.Decisions
		out.RouteApplied += e.CostRoute.Applied
		out.RouteShadow += e.CostRoute.Shadow
		for k, v := range e.CostRoute.ByTier {
			out.ByTier[k] += v
		}
		// Prefer heartbeat daily when present (compact), else fleet view.
		if e.Heartbeat != nil && (e.Heartbeat.DailyCalls > 0 || e.Heartbeat.DailyCostUSD > 0) {
			out.DailyCostUSD += e.Heartbeat.DailyCostUSD
			out.DailyCalls += e.Heartbeat.DailyCalls
			out.DailyInstances += e.Heartbeat.DailyInstances
		} else {
			out.DailyCostUSD += e.DailyFleet.CostUSD
			out.DailyCalls += e.DailyFleet.Calls
			out.DailyInstances += e.DailyFleet.Instances
		}
		if h := strings.TrimSpace(e.Host); h != "" {
			hostSet[h] = struct{}{}
		}
	}
	if len(out.ByTier) == 0 {
		out.ByTier = nil
	}
	for h := range hostSet {
		out.Hosts = append(out.Hosts, h)
	}
	sort.Strings(out.Hosts)
	parts := make([]string, 0, 2)
	if out.RouteDecisions > 0 {
		parts = append(parts, fmt.Sprintf("cost-route decisions=%d applied=%d shadow=%d",
			out.RouteDecisions, out.RouteApplied, out.RouteShadow))
	}
	if out.DailyCalls > 0 || out.DailyCostUSD > 0 {
		parts = append(parts, fmt.Sprintf("llm-cost today=$%.4f calls=%d instances=%d",
			out.DailyCostUSD, out.DailyCalls, out.DailyInstances))
	}
	out.Summary = strings.Join(parts, " | ")
	return out
}
