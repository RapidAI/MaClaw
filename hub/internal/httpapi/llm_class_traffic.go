package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	llmClassTrafficKey        = "llm_class_traffic_v1"
	llmClassTrafficVersion    = 1
	llmClassTrafficKeepDays   = 40
	llmClassTrafficMaxSamples = 20
)

var llmClassTrafficMu sync.Mutex

type llmClassTrafficStore struct {
	Version int                            `json:"version"`
	Days    map[string]*llmClassTrafficDay `json:"days,omitempty"`
}

type llmClassTrafficDay struct {
	Groups map[string]*llmClassTrafficGroup `json:"groups,omitempty"`
}

type llmClassTrafficGroup struct {
	Totals  llmUsageCounters            `json:"totals"`
	Classes map[string]llmUsageCounters `json:"classes,omitempty"`
	Sources map[string]int64            `json:"sources,omitempty"`
	Samples []llmClassTrafficSample     `json:"samples,omitempty"`
}

type llmClassTrafficSample struct {
	At      time.Time `json:"at"`
	Class   string    `json:"class"`
	Source  string    `json:"source"`
	Preview string    `json:"preview,omitempty"`
}

type llmClassTrafficRow struct {
	Class        string `json:"class"`
	Requests     int64  `json:"requests"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
}

type llmClassTrafficResponse struct {
	ServiceGroupID string                  `json:"service_group_id"`
	Window         string                  `json:"window"`
	Rows           []llmClassTrafficRow    `json:"rows"`
	Sources        map[string]int64        `json:"sources,omitempty"`
	Samples        []llmClassTrafficSample `json:"samples,omitempty"`
	GeneratedAt    time.Time               `json:"generated_at"`
}

func recordLLMClassTraffic(system store.SystemSettingsRepository, serviceGroupIDs []string, meta llmservice.OfficialForwardMeta, usage corelib.TokenUsageStat, preview string) {
	if system == nil {
		return
	}
	ids := normalizeUsageStringSlice(serviceGroupIDs)
	if len(ids) == 0 {
		return
	}
	class := strings.TrimSpace(meta.WorkloadClass)
	if class == "" {
		class = llmpool.WorkloadUnclassified
	}
	classSource := strings.TrimSpace(meta.ClassSource)
	if classSource == "" {
		if class == llmpool.WorkloadUnclassified {
			classSource = llmpool.ClassSourceNone
		} else if class == llmpool.WorkloadFallbackBalanced {
			classSource = llmpool.ClassSourceFallback
		} else {
			classSource = llmpool.ClassSourceHint
		}
	}
	llmClassTrafficMu.Lock()
	defer llmClassTrafficMu.Unlock()
	storeData := loadLLMClassTrafficLocked(context.Background(), system)
	now := time.Now()
	day := storeData.ensureDay(now.Format("2006-01-02"))
	for _, groupID := range ids {
		entry := day.ensureGroup(groupID)
		addUsageCounters(&entry.Totals, usage, 0)
		counters := entry.Classes[class]
		addUsageCounters(&counters, usage, 0)
		if entry.Classes == nil {
			entry.Classes = map[string]llmUsageCounters{}
		}
		entry.Classes[class] = counters
		if entry.Sources == nil {
			entry.Sources = map[string]int64{}
		}
		entry.Sources[classSource]++
		if classSource != llmpool.ClassSourceHint && strings.TrimSpace(preview) != "" {
			entry.Samples = append([]llmClassTrafficSample{{
				At:      now.UTC(),
				Class:   class,
				Source:  classSource,
				Preview: truncateRunes(preview, 200),
			}}, entry.Samples...)
			if len(entry.Samples) > llmClassTrafficMaxSamples {
				entry.Samples = entry.Samples[:llmClassTrafficMaxSamples]
			}
		}
	}
	storeData.prune(now)
	_ = saveLLMClassTrafficLocked(context.Background(), system, storeData)
}

func loadLLMClassTrafficLocked(ctx context.Context, system store.SystemSettingsRepository) *llmClassTrafficStore {
	data := &llmClassTrafficStore{Version: llmClassTrafficVersion, Days: map[string]*llmClassTrafficDay{}}
	raw, err := system.Get(ctx, llmClassTrafficKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return data
	}
	if json.Unmarshal([]byte(raw), data) != nil {
		return &llmClassTrafficStore{Version: llmClassTrafficVersion, Days: map[string]*llmClassTrafficDay{}}
	}
	if data.Days == nil {
		data.Days = map[string]*llmClassTrafficDay{}
	}
	return data
}

func saveLLMClassTrafficLocked(ctx context.Context, system store.SystemSettingsRepository, data *llmClassTrafficStore) error {
	if data == nil {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return system.Set(ctx, llmClassTrafficKey, string(raw))
}

func (s *llmClassTrafficStore) ensureDay(day string) *llmClassTrafficDay {
	if s.Days == nil {
		s.Days = map[string]*llmClassTrafficDay{}
	}
	entry := s.Days[day]
	if entry == nil {
		entry = &llmClassTrafficDay{}
		s.Days[day] = entry
	}
	return entry
}

func (d *llmClassTrafficDay) ensureGroup(id string) *llmClassTrafficGroup {
	if d.Groups == nil {
		d.Groups = map[string]*llmClassTrafficGroup{}
	}
	entry := d.Groups[id]
	if entry == nil {
		entry = &llmClassTrafficGroup{Classes: map[string]llmUsageCounters{}, Sources: map[string]int64{}}
		d.Groups[id] = entry
	}
	return entry
}

func (s *llmClassTrafficStore) prune(now time.Time) {
	if s == nil {
		return
	}
	cutoff := now.AddDate(0, 0, -llmClassTrafficKeepDays).Format("2006-01-02")
	for day := range s.Days {
		if day < cutoff {
			delete(s.Days, day)
		}
	}
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

func GetLLMClassTrafficHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		groupID := strings.TrimSpace(r.URL.Query().Get("service_group_id"))
		if groupID == "" {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_GROUP_REQUIRED", "service_group_id is required")
			return
		}
		window := strings.TrimSpace(r.URL.Query().Get("window"))
		if window == "" {
			window = "24h"
		}
		days := 1
		switch window {
		case "7d":
			days = 7
		case "30d":
			days = 30
		case "24h":
			days = 1
		default:
			writeError(w, http.StatusBadRequest, "LLM_TRAFFIC_WINDOW_INVALID", "window must be 24h, 7d, or 30d")
			return
		}
		llmClassTrafficMu.Lock()
		storeData := loadLLMClassTrafficLocked(r.Context(), system)
		llmClassTrafficMu.Unlock()
		now := time.Now()
		totals := map[string]llmUsageCounters{}
		sources := map[string]int64{}
		var samples []llmClassTrafficSample
		groupTotal := llmUsageCounters{}
		for i := 0; i < days; i++ {
			dayKey := now.AddDate(0, 0, -i).Format("2006-01-02")
			day := storeData.Days[dayKey]
			if day == nil || day.Groups[groupID] == nil {
				continue
			}
			entry := day.Groups[groupID]
			addUsageCountersFromTotals(&groupTotal, entry.Totals)
			for class, counters := range entry.Classes {
				dst := totals[class]
				addUsageCountersFromTotals(&dst, counters)
				totals[class] = dst
			}
			for source, count := range entry.Sources {
				sources[source] += count
			}
			samples = append(samples, entry.Samples...)
		}
		rows := make([]llmClassTrafficRow, 0, len(llmpool.FrozenWorkloadClasses)+2)
		appendRow := func(class string) {
			counters := totals[class]
			rows = append(rows, llmClassTrafficRow{
				Class:        class,
				Requests:     counters.Requests,
				InputTokens:  counters.InputTokens,
				OutputTokens: counters.OutputTokens,
				TotalTokens:  counters.TotalTokens,
			})
		}
		for _, class := range llmpool.FrozenWorkloadClasses {
			appendRow(class)
		}
		appendRow(llmpool.WorkloadFallbackBalanced)
		appendRow(llmpool.WorkloadUnclassified)
		rows = append(rows, llmClassTrafficRow{
			Class:        "total",
			Requests:     groupTotal.Requests,
			InputTokens:  groupTotal.InputTokens,
			OutputTokens: groupTotal.OutputTokens,
			TotalTokens:  groupTotal.TotalTokens,
		})
		sort.SliceStable(samples, func(i, j int) bool { return samples[i].At.After(samples[j].At) })
		if len(samples) > llmClassTrafficMaxSamples {
			samples = samples[:llmClassTrafficMaxSamples]
		}
		writeJSON(w, http.StatusOK, llmClassTrafficResponse{
			ServiceGroupID: groupID,
			Window:         window,
			Rows:           rows,
			Sources:        sources,
			Samples:        samples,
			GeneratedAt:    time.Now().UTC(),
		})
	}
}

type llmClassifyPreviewRequest struct {
	Headers map[string]string `json:"headers"`
	Body    map[string]any    `json:"body"`
}

func PostLLMClassifyPreviewHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		groupID := strings.TrimSpace(r.URL.Query().Get("service_group_id"))
		if groupID == "" {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_GROUP_REQUIRED", "service_group_id is required")
			return
		}
		var req llmClassifyPreviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		reg, err := loadCachedLLMServiceRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		group := reg.FindModelServiceGroup(groupID)
		if group == nil {
			writeError(w, http.StatusNotFound, "LLM_SERVICE_GROUP_NOT_FOUND", "unknown service group")
			return
		}
		header := http.Header{}
		for key, value := range req.Headers {
			header.Set(key, value)
		}
		pool := group.ToPoolGroup()
		dec := llmpool.ClassifyAndRoute(header, req.Body, &pool)
		dec.BindRequestedGroup(groupID)
		writeJSON(w, http.StatusOK, dec)
	}
}
