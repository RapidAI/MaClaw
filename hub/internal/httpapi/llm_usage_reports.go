package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	llmUsageReportsKey      = "llm_usage_reports_v1"
	llmUsageReportsVersion  = 1
	llmUsageReportsKeepDays = 366
)

type llmUsageReportsStore struct {
	Version int                           `json:"version"`
	Days    map[string]*llmUsageReportDay `json:"days,omitempty"`
}

type llmUsageReportDay struct {
	Totals    llmUsageCounters                `json:"totals"`
	Users     map[string]*llmUsageReportEntry `json:"users,omitempty"`
	Groups    map[string]*llmUsageReportEntry `json:"groups,omitempty"`
	Providers map[string]*llmUsageReportEntry `json:"providers,omitempty"`
}

type llmUsageReportEntry struct {
	Totals llmUsageCounters   `json:"totals"`
	Hours  []llmUsageCounters `json:"hours,omitempty"`
}

type llmUsageCounters struct {
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	TotalTokens       int64   `json:"total_tokens"`
	CachedInputTokens int64   `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int64   `json:"cache_write_tokens,omitempty"`
	InputCostRMB      float64 `json:"input_cost_rmb,omitempty"`
	OutputCostRMB     float64 `json:"output_cost_rmb,omitempty"`
	TotalCostRMB      float64 `json:"total_cost_rmb,omitempty"`
	Requests          int64   `json:"requests"`
	CachedRequests    int64   `json:"cached_requests,omitempty"`
	Credits           float64 `json:"credits,omitempty"`
}

type llmUsageReportEntityOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type llmUsageReportRow struct {
	ID                string             `json:"id"`
	Name              string             `json:"name"`
	InputTokens       int64              `json:"input_tokens"`
	OutputTokens      int64              `json:"output_tokens"`
	TotalTokens       int64              `json:"total_tokens"`
	CachedInputTokens int64              `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int64              `json:"cache_write_tokens,omitempty"`
	InputCostRMB      float64            `json:"input_cost_rmb,omitempty"`
	OutputCostRMB     float64            `json:"output_cost_rmb,omitempty"`
	TotalCostRMB      float64            `json:"total_cost_rmb,omitempty"`
	Requests          int64              `json:"requests"`
	CachedRequests    int64              `json:"cached_requests,omitempty"`
	Credits           float64            `json:"credits"`
	Hours             []llmUsageCounters `json:"hours,omitempty"`
}

type llmUsageReportResponse struct {
	Scope           string                       `json:"scope"`
	Period          string                       `json:"period"`
	Date            string                       `json:"date,omitempty"`
	Month           string                       `json:"month,omitempty"`
	SelectedEntity  string                       `json:"selected_entity,omitempty"`
	Summary         llmUsageCounters             `json:"summary"`
	Trend           []llmUsageCounters           `json:"trend,omitempty"`
	Rows            []llmUsageReportRow          `json:"rows"`
	Entities        []llmUsageReportEntityOption `json:"entities,omitempty"`
	AvailableGroups []llmUsageReportEntityOption `json:"available_groups,omitempty"`
	GeneratedAt     time.Time                    `json:"generated_at"`
}

func (s *llmUsageReportsStore) ensureDay(day string) *llmUsageReportDay {
	if s.Days == nil {
		s.Days = map[string]*llmUsageReportDay{}
	}
	entry := s.Days[day]
	if entry == nil {
		entry = &llmUsageReportDay{}
		s.Days[day] = entry
	}
	return entry
}

func (d *llmUsageReportDay) ensureUser(email string) *llmUsageReportEntry {
	if d.Users == nil {
		d.Users = map[string]*llmUsageReportEntry{}
	}
	entry := d.Users[email]
	if entry == nil {
		entry = &llmUsageReportEntry{}
		d.Users[email] = entry
	}
	ensureHourlyCounters(entry)
	return entry
}

func (d *llmUsageReportDay) ensureGroup(groupID string) *llmUsageReportEntry {
	if d.Groups == nil {
		d.Groups = map[string]*llmUsageReportEntry{}
	}
	entry := d.Groups[groupID]
	if entry == nil {
		entry = &llmUsageReportEntry{}
		d.Groups[groupID] = entry
	}
	ensureHourlyCounters(entry)
	return entry
}

func (d *llmUsageReportDay) ensureProvider(providerID string) *llmUsageReportEntry {
	if d.Providers == nil {
		d.Providers = map[string]*llmUsageReportEntry{}
	}
	entry := d.Providers[providerID]
	if entry == nil {
		entry = &llmUsageReportEntry{}
		d.Providers[providerID] = entry
	}
	ensureHourlyCounters(entry)
	return entry
}

func ensureHourlyCounters(entry *llmUsageReportEntry) {
	if entry == nil {
		return
	}
	if len(entry.Hours) == 24 {
		return
	}
	hours := make([]llmUsageCounters, 24)
	copy(hours, entry.Hours)
	entry.Hours = hours
}

func addUsageCounters(dst *llmUsageCounters, usage corelib.TokenUsageStat, credits float64) {
	if dst == nil {
		return
	}
	requests := usage.Requests
	if requests <= 0 {
		requests = 1
	}
	dst.InputTokens += usage.InputTokens
	dst.OutputTokens += usage.OutputTokens
	dst.TotalTokens += usage.TotalTokens
	dst.CachedInputTokens += usage.CachedInputTokens
	dst.CacheWriteTokens += usage.CacheWriteTokens
	dst.InputCostRMB += usage.InputCostRMB
	dst.OutputCostRMB += usage.OutputCostRMB
	dst.TotalCostRMB += usage.TotalCostRMB
	dst.Requests += requests
	dst.CachedRequests += usage.CachedRequests
	dst.Credits += credits
}

func addUsageCountersFromTotals(dst *llmUsageCounters, src llmUsageCounters) {
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.TotalTokens += src.TotalTokens
	dst.CachedInputTokens += src.CachedInputTokens
	dst.CacheWriteTokens += src.CacheWriteTokens
	dst.InputCostRMB += src.InputCostRMB
	dst.OutputCostRMB += src.OutputCostRMB
	dst.TotalCostRMB += src.TotalCostRMB
	dst.Requests += src.Requests
	dst.CachedRequests += src.CachedRequests
	dst.Credits += src.Credits
}

func cloneUsageCountersSlice(items []llmUsageCounters) []llmUsageCounters {
	if len(items) == 0 {
		return nil
	}
	out := make([]llmUsageCounters, len(items))
	copy(out, items)
	return out
}

func (s *llmUsageReportsStore) addUsage(ts time.Time, email string, userGroupIDs []string, usage corelib.TokenUsageStat, credits float64, providerIDs ...string) {
	if s == nil {
		return
	}
	dayKey := ts.Format("2006-01-02")
	hour := ts.Hour()
	day := s.ensureDay(dayKey)
	addUsageCounters(&day.Totals, usage, credits)
	email = strings.ToLower(strings.TrimSpace(email))
	if email != "" {
		entry := day.ensureUser(email)
		addUsageCounters(&entry.Totals, usage, credits)
		addUsageCounters(&entry.Hours[hour], usage, credits)
	}
	for _, groupID := range normalizeUsageStringSlice(userGroupIDs) {
		entry := day.ensureGroup(groupID)
		addUsageCounters(&entry.Totals, usage, credits)
		addUsageCounters(&entry.Hours[hour], usage, credits)
	}
	providerID := ""
	if len(providerIDs) > 0 {
		providerID = strings.TrimSpace(providerIDs[0])
	}
	if providerID != "" {
		entry := day.ensureProvider(providerID)
		addUsageCounters(&entry.Totals, usage, credits)
		addUsageCounters(&entry.Hours[hour], usage, credits)
	}
}

func mergeLLMUsageReports(dst *llmUsageReportsStore, src *llmUsageReportsStore) {
	if dst == nil || src == nil {
		return
	}
	for dayKey, srcDay := range src.Days {
		if srcDay == nil {
			continue
		}
		dstDay := dst.ensureDay(dayKey)
		addUsageCountersFromTotals(&dstDay.Totals, srcDay.Totals)
		for email, srcEntry := range srcDay.Users {
			if srcEntry == nil {
				continue
			}
			dstEntry := dstDay.ensureUser(email)
			addUsageCountersFromTotals(&dstEntry.Totals, srcEntry.Totals)
			for i := 0; i < len(srcEntry.Hours) && i < 24; i++ {
				addUsageCountersFromTotals(&dstEntry.Hours[i], srcEntry.Hours[i])
			}
		}
		for groupID, srcEntry := range srcDay.Groups {
			if srcEntry == nil {
				continue
			}
			dstEntry := dstDay.ensureGroup(groupID)
			addUsageCountersFromTotals(&dstEntry.Totals, srcEntry.Totals)
			for i := 0; i < len(srcEntry.Hours) && i < 24; i++ {
				addUsageCountersFromTotals(&dstEntry.Hours[i], srcEntry.Hours[i])
			}
		}
		for providerID, srcEntry := range srcDay.Providers {
			if srcEntry == nil {
				continue
			}
			dstEntry := dstDay.ensureProvider(providerID)
			addUsageCountersFromTotals(&dstEntry.Totals, srcEntry.Totals)
			for i := 0; i < len(srcEntry.Hours) && i < 24; i++ {
				addUsageCountersFromTotals(&dstEntry.Hours[i], srcEntry.Hours[i])
			}
		}
	}
	pruneLLMUsageReports(dst, time.Now())
}

func pruneLLMUsageReports(rep *llmUsageReportsStore, now time.Time) {
	if rep == nil || len(rep.Days) == 0 {
		return
	}
	cutoff := now.AddDate(0, 0, -llmUsageReportsKeepDays).Format("2006-01-02")
	for dayKey := range rep.Days {
		if dayKey < cutoff {
			delete(rep.Days, dayKey)
		}
	}
}

func loadLLMUsageReports(ctx context.Context, system store.SystemSettingsRepository) (*llmUsageReportsStore, error) {
	if system == nil {
		return &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}, nil
	}
	raw, err := system.Get(ctx, llmUsageReportsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}, nil
	}
	var rep llmUsageReportsStore
	if err := json.Unmarshal([]byte(raw), &rep); err != nil {
		return nil, err
	}
	if rep.Days == nil {
		rep.Days = map[string]*llmUsageReportDay{}
	}
	if rep.Version == 0 {
		rep.Version = llmUsageReportsVersion
	}
	return &rep, nil
}

func llmUsageTotalsForUser(ctx context.Context, system store.SystemSettingsRepository, email string) (llmUsageCounters, error) {
	rep, err := loadLLMUsageReports(ctx, system)
	if err != nil {
		return llmUsageCounters{}, err
	}
	var totals llmUsageCounters
	email = strings.ToLower(strings.TrimSpace(email))
	if rep == nil || email == "" {
		return totals, nil
	}
	for _, day := range rep.Days {
		if day == nil || day.Users == nil {
			continue
		}
		if entry := day.Users[email]; entry != nil {
			addUsageCountersFromTotals(&totals, entry.Totals)
		}
	}
	return totals, nil
}

func saveLLMUsageReports(ctx context.Context, system store.SystemSettingsRepository, rep *llmUsageReportsStore) error {
	if system == nil {
		return nil
	}
	if rep == nil {
		rep = &llmUsageReportsStore{}
	}
	rep.Version = llmUsageReportsVersion
	if rep.Days == nil {
		rep.Days = map[string]*llmUsageReportDay{}
	}
	pruneLLMUsageReports(rep, time.Now())
	data, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	return system.Set(ctx, llmUsageReportsKey, string(data))
}

func flushLLMUsageReports(ctx context.Context, system store.SystemSettingsRepository, pending *llmUsageReportsStore) error {
	if pending == nil || len(pending.Days) == 0 {
		return nil
	}
	rep, err := loadLLMUsageReports(ctx, system)
	if err != nil {
		return err
	}
	mergeLLMUsageReports(rep, pending)
	return saveLLMUsageReports(ctx, system, rep)
}

func flattenSecurityGroups(node *security.GroupTreeNode, path string, out *[]llmUsageReportEntityOption) {
	if node == nil {
		return
	}
	name := node.Name
	if path != "" {
		name = path + " / " + node.Name
	}
	*out = append(*out, llmUsageReportEntityOption{ID: node.ID, Name: name})
	for _, child := range node.Children {
		flattenSecurityGroups(child, name, out)
	}
}

func groupNameMap(ctx context.Context, securitySvc *security.SecurityService) map[string]string {
	out := map[string]string{}
	if securitySvc == nil {
		return out
	}
	tree, err := securitySvc.GetGroupTree(ctx)
	if err != nil || tree == nil {
		return out
	}
	items := make([]llmUsageReportEntityOption, 0)
	flattenSecurityGroups(tree, "", &items)
	for _, item := range items {
		out[item.ID] = item.Name
	}
	return out
}

func listAvailableGroups(ctx context.Context, securitySvc *security.SecurityService) []llmUsageReportEntityOption {
	if securitySvc == nil {
		return nil
	}
	tree, err := securitySvc.GetGroupTree(ctx)
	if err != nil || tree == nil {
		return nil
	}
	items := make([]llmUsageReportEntityOption, 0)
	flattenSecurityGroups(tree, "", &items)
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items
}

func providerNameMap(ctx context.Context, system store.SystemSettingsRepository) map[string]string {
	out := map[string]string{}
	if system == nil {
		return out
	}
	reg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil || reg == nil {
		return out
	}
	for _, provider := range reg.Providers {
		id := strings.TrimSpace(provider.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = id
		}
		out[id] = name
	}
	return out
}

func normalizeUsageScope(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "group":
		return "group"
	case "provider", "llm_provider", "llm-provider":
		return "provider"
	}
	return "user"
}

func normalizeUsagePeriod(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), "monthly") || strings.EqualFold(strings.TrimSpace(v), "month") {
		return "monthly"
	}
	return "daily"
}

func parseUsageDay(v string, now time.Time) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return now.Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return now.Format("2006-01-02")
	}
	return v
}

func parseUsageMonth(v string, now time.Time) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return now.Format("2006-01")
	}
	if _, err := time.Parse("2006-01", v); err != nil {
		return now.Format("2006-01")
	}
	return v
}

func buildLLMUsageReportResponse(ctx context.Context, rep *llmUsageReportsStore, securitySvc *security.SecurityService, scope, period, dayKey, monthKey, entity string, now time.Time, providerNameMaps ...map[string]string) llmUsageReportResponse {
	resp := llmUsageReportResponse{
		Scope:           scope,
		Period:          period,
		SelectedEntity:  strings.TrimSpace(entity),
		Rows:            []llmUsageReportRow{},
		Trend:           []llmUsageCounters{},
		AvailableGroups: listAvailableGroups(ctx, securitySvc),
		GeneratedAt:     now,
	}
	groupNames := groupNameMap(ctx, securitySvc)
	providerNames := map[string]string{}
	if len(providerNameMaps) > 0 && providerNameMaps[0] != nil {
		providerNames = providerNameMaps[0]
	}
	entityOptions := map[string]string{}
	if rep == nil {
		return resp
	}
	addRow := func(id string, totals llmUsageCounters, hours []llmUsageCounters) {
		if strings.TrimSpace(id) == "" {
			return
		}
		name := id
		if scope == "group" {
			if display := groupNames[id]; display != "" {
				name = display
			}
		} else if scope == "provider" {
			if display := providerNames[id]; display != "" {
				name = display
			}
		}
		resp.Rows = append(resp.Rows, llmUsageReportRow{
			ID:                id,
			Name:              name,
			InputTokens:       totals.InputTokens,
			OutputTokens:      totals.OutputTokens,
			TotalTokens:       totals.TotalTokens,
			CachedInputTokens: totals.CachedInputTokens,
			CacheWriteTokens:  totals.CacheWriteTokens,
			InputCostRMB:      totals.InputCostRMB,
			OutputCostRMB:     totals.OutputCostRMB,
			TotalCostRMB:      totals.TotalCostRMB,
			Requests:          totals.Requests,
			CachedRequests:    totals.CachedRequests,
			Credits:           totals.Credits,
			Hours:             cloneUsageCountersSlice(hours),
		})
		entityOptions[id] = name
	}
	if period == "daily" {
		resp.Date = dayKey
		day := rep.Days[dayKey]
		if day == nil {
			return resp
		}
		if entity != "" {
			if scope == "group" {
				if entry := day.Groups[entity]; entry != nil {
					resp.Summary = entry.Totals
					resp.Trend = cloneUsageCountersSlice(entry.Hours)
					addRow(entity, entry.Totals, entry.Hours)
				}
			} else if scope == "provider" {
				if entry := day.Providers[entity]; entry != nil {
					resp.Summary = entry.Totals
					resp.Trend = cloneUsageCountersSlice(entry.Hours)
					addRow(entity, entry.Totals, entry.Hours)
				}
			} else if entry := day.Users[strings.ToLower(entity)]; entry != nil {
				resp.Summary = entry.Totals
				resp.Trend = cloneUsageCountersSlice(entry.Hours)
				addRow(strings.ToLower(entity), entry.Totals, entry.Hours)
			}
		} else {
			if scope != "provider" {
				resp.Summary = day.Totals
			}
			resp.Trend = make([]llmUsageCounters, 24)
			if scope == "group" {
				for id, entry := range day.Groups {
					if entry == nil {
						continue
					}
					addRow(id, entry.Totals, entry.Hours)
					for i := 0; i < len(entry.Hours) && i < 24; i++ {
						addUsageCountersFromTotals(&resp.Trend[i], entry.Hours[i])
					}
				}
			} else if scope == "provider" {
				for id, entry := range day.Providers {
					if entry == nil {
						continue
					}
					addUsageCountersFromTotals(&resp.Summary, entry.Totals)
					addRow(id, entry.Totals, entry.Hours)
					for i := 0; i < len(entry.Hours) && i < 24; i++ {
						addUsageCountersFromTotals(&resp.Trend[i], entry.Hours[i])
					}
				}
			} else {
				for id, entry := range day.Users {
					if entry == nil {
						continue
					}
					addRow(id, entry.Totals, entry.Hours)
					for i := 0; i < len(entry.Hours) && i < 24; i++ {
						addUsageCountersFromTotals(&resp.Trend[i], entry.Hours[i])
					}
				}
			}
		}
	} else {
		resp.Month = monthKey
		monthly := map[string]llmUsageCounters{}
		for date, day := range rep.Days {
			if day == nil || !strings.HasPrefix(date, monthKey+"-") {
				continue
			}
			if entity == "" && scope != "provider" {
				addUsageCountersFromTotals(&resp.Summary, day.Totals)
			}
			if scope == "group" {
				for id, entry := range day.Groups {
					if entry == nil {
						continue
					}
					curr := monthly[id]
					addUsageCountersFromTotals(&curr, entry.Totals)
					monthly[id] = curr
				}
			} else if scope == "provider" {
				for id, entry := range day.Providers {
					if entry == nil {
						continue
					}
					if entity == "" {
						addUsageCountersFromTotals(&resp.Summary, entry.Totals)
					}
					curr := monthly[id]
					addUsageCountersFromTotals(&curr, entry.Totals)
					monthly[id] = curr
				}
			} else {
				for id, entry := range day.Users {
					if entry == nil {
						continue
					}
					curr := monthly[id]
					addUsageCountersFromTotals(&curr, entry.Totals)
					monthly[id] = curr
				}
			}
		}
		if entity != "" {
			if totals, ok := monthly[entity]; ok {
				resp.Summary = totals
				addRow(entity, totals, nil)
			}
		} else {
			for id, totals := range monthly {
				addRow(id, totals, nil)
			}
		}
	}
	sort.Slice(resp.Rows, func(i, j int) bool {
		if resp.Rows[i].TotalTokens == resp.Rows[j].TotalTokens {
			return strings.ToLower(resp.Rows[i].Name) < strings.ToLower(resp.Rows[j].Name)
		}
		return resp.Rows[i].TotalTokens > resp.Rows[j].TotalTokens
	})
	resp.Entities = make([]llmUsageReportEntityOption, 0, len(entityOptions))
	for id, name := range entityOptions {
		resp.Entities = append(resp.Entities, llmUsageReportEntityOption{ID: id, Name: name})
	}
	sort.Slice(resp.Entities, func(i, j int) bool {
		return strings.ToLower(resp.Entities[i].Name) < strings.ToLower(resp.Entities[j].Name)
	})
	return resp
}

func GetLLMUsageReportHandler(system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		rep, err := loadLLMUsageReports(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_USAGE_REPORT_LOAD_FAILED", err.Error())
			return
		}
		now := time.Now()
		scope := normalizeUsageScope(r.URL.Query().Get("scope"))
		period := normalizeUsagePeriod(r.URL.Query().Get("period"))
		dayKey := parseUsageDay(r.URL.Query().Get("date"), now)
		monthKey := parseUsageMonth(r.URL.Query().Get("month"), now)
		entity := strings.TrimSpace(r.URL.Query().Get("entity"))
		if scope == "user" {
			entity = strings.ToLower(entity)
		}
		resp := buildLLMUsageReportResponse(r.Context(), rep, securitySvc, scope, period, dayKey, monthKey, entity, now, providerNameMap(r.Context(), system))
		writeJSON(w, http.StatusOK, resp)
	}
}

func formatUsageReportError(scope, period string) error {
	return fmt.Errorf("invalid usage report request: scope=%s period=%s", scope, period)
}
