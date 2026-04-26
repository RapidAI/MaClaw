package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	llmEndpointAccessLogsKey         = "llm_endpoint_access_logs_v1"
	llmEndpointAccessLogsVersion     = 1
	llmEndpointAccessLogsKeepEntries = 200
	llmEndpointAccessLogsBodyLimit   = 65535
)

type llmEndpointAccessLogEntry struct {
	ID                string         `json:"id"`
	Email             string         `json:"email,omitempty"`
	ClientIP          string         `json:"client_ip,omitempty"`
	RequestedModel    string         `json:"requested_model,omitempty"`
	AuthorizedModel   string         `json:"authorized_model,omitempty"`
	ProviderID        string         `json:"provider_id,omitempty"`
	StatusCode        int            `json:"status_code"`
	ErrorCode         string         `json:"error_code,omitempty"`
	InputTokens       int64          `json:"input_tokens,omitempty"`
	OutputTokens      int64          `json:"output_tokens,omitempty"`
	TotalTokens       int64          `json:"total_tokens,omitempty"`
	CachedInputTokens int64          `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int64          `json:"cache_write_tokens,omitempty"`
	RequestBytes      int            `json:"request_bytes,omitempty"`
	RequestBody       string         `json:"request_body,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type llmEndpointAccessLogStore struct {
	Version       int                         `json:"version"`
	TotalRequests int64                       `json:"total_requests"`
	IPCounts      map[string]int64            `json:"ip_counts,omitempty"`
	Entries       []llmEndpointAccessLogEntry `json:"entries,omitempty"`
}

type llmEndpointAccessLogSummary struct {
	TotalRequests   int64  `json:"total_requests"`
	UniqueIPCount   int    `json:"unique_ip_count"`
	LatestRequestAt string `json:"latest_request_at,omitempty"`
}

type llmEndpointAccessLogView struct {
	ID                string         `json:"id"`
	Email             string         `json:"email,omitempty"`
	ClientIP          string         `json:"client_ip,omitempty"`
	RequestedModel    string         `json:"requested_model,omitempty"`
	AuthorizedModel   string         `json:"authorized_model,omitempty"`
	ProviderID        string         `json:"provider_id,omitempty"`
	StatusCode        int            `json:"status_code"`
	ErrorCode         string         `json:"error_code,omitempty"`
	InputTokens       int64          `json:"input_tokens,omitempty"`
	OutputTokens      int64          `json:"output_tokens,omitempty"`
	TotalTokens       int64          `json:"total_tokens,omitempty"`
	CachedInputTokens int64          `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int64          `json:"cache_write_tokens,omitempty"`
	RequestBytes      int            `json:"request_bytes,omitempty"`
	RequestBody       string         `json:"request_body,omitempty"`
	CreatedAt         string         `json:"created_at"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type llmEndpointAccessLogResponse struct {
	Summary llmEndpointAccessLogSummary `json:"summary"`
	Logs    []llmEndpointAccessLogView  `json:"logs"`
	Total   int                         `json:"total"`
	Offset  int                         `json:"offset"`
	Limit   int                         `json:"limit"`
}

type llmEndpointAccessLogAccumulator struct {
	mu       sync.Mutex
	once     sync.Once
	pending  map[store.SystemSettingsRepository]*llmEndpointAccessLogStore
	interval time.Duration
}

var globalLLMEndpointAccessLogAccumulator = &llmEndpointAccessLogAccumulator{
	pending:  map[store.SystemSettingsRepository]*llmEndpointAccessLogStore{},
	interval: 5 * time.Second,
}

func enqueueLLMEndpointAccessLog(system store.SystemSettingsRepository, entry llmEndpointAccessLogEntry) {
	if system == nil {
		return
	}
	globalLLMEndpointAccessLogAccumulator.start()
	globalLLMEndpointAccessLogAccumulator.enqueue(system, entry)
}

func (a *llmEndpointAccessLogAccumulator) start() {
	a.once.Do(func() {
		go func() {
			ticker := time.NewTicker(a.interval)
			defer ticker.Stop()
			for range ticker.C {
				a.flush(context.Background())
			}
		}()
	})
}

func (a *llmEndpointAccessLogAccumulator) enqueue(system store.SystemSettingsRepository, entry llmEndpointAccessLogEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	buf := a.pending[system]
	if buf == nil {
		buf = newLLMEndpointAccessLogStore()
		a.pending[system] = buf
	}
	buf.add(entry)
}

func (a *llmEndpointAccessLogAccumulator) flush(ctx context.Context) {
	a.mu.Lock()
	snapshot := a.pending
	a.pending = map[store.SystemSettingsRepository]*llmEndpointAccessLogStore{}
	a.mu.Unlock()
	for system, pending := range snapshot {
		if system == nil || pending == nil || pending.TotalRequests == 0 {
			continue
		}
		if err := flushLLMEndpointAccessLogs(ctx, system, pending); err != nil {
			a.requeue(system, pending)
		}
	}
}

func (a *llmEndpointAccessLogAccumulator) requeue(system store.SystemSettingsRepository, pending *llmEndpointAccessLogStore) {
	if system == nil || pending == nil || pending.TotalRequests == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.pending[system]
	if current == nil {
		current = newLLMEndpointAccessLogStore()
		a.pending[system] = current
	}
	mergeLLMEndpointAccessLogs(current, pending)
}

func (a *llmEndpointAccessLogAccumulator) snapshot(system store.SystemSettingsRepository) *llmEndpointAccessLogStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneLLMEndpointAccessLogStore(a.pending[system])
}

func newLLMEndpointAccessLogStore() *llmEndpointAccessLogStore {
	return &llmEndpointAccessLogStore{Version: llmEndpointAccessLogsVersion, IPCounts: map[string]int64{}, Entries: []llmEndpointAccessLogEntry{}}
}

func cloneLLMEndpointAccessLogStore(src *llmEndpointAccessLogStore) *llmEndpointAccessLogStore {
	if src == nil {
		return nil
	}
	clone := &llmEndpointAccessLogStore{
		Version:       src.Version,
		TotalRequests: src.TotalRequests,
		IPCounts:      map[string]int64{},
		Entries:       append([]llmEndpointAccessLogEntry(nil), src.Entries...),
	}
	for ip, count := range src.IPCounts {
		clone.IPCounts[ip] = count
	}
	return clone
}

func (s *llmEndpointAccessLogStore) add(entry llmEndpointAccessLogEntry) {
	if s == nil {
		return
	}
	if s.Version == 0 {
		s.Version = llmEndpointAccessLogsVersion
	}
	if s.IPCounts == nil {
		s.IPCounts = map[string]int64{}
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	entry.ID = strings.TrimSpace(entry.ID)
	if entry.ID == "" {
		entry.ID = entry.CreatedAt.UTC().Format("20060102T150405.000000000")
	}
	entry.Email = strings.ToLower(strings.TrimSpace(entry.Email))
	entry.ClientIP = strings.TrimSpace(entry.ClientIP)
	entry.RequestedModel = strings.TrimSpace(entry.RequestedModel)
	entry.AuthorizedModel = strings.TrimSpace(entry.AuthorizedModel)
	entry.ProviderID = strings.TrimSpace(entry.ProviderID)
	entry.ErrorCode = strings.TrimSpace(entry.ErrorCode)
	entry.RequestBody = trimLLMEndpointAccessLogBody(entry.RequestBody)
	s.TotalRequests++
	if entry.ClientIP != "" {
		s.IPCounts[entry.ClientIP]++
	}
	s.Entries = append(s.Entries, entry)
	pruneLLMEndpointAccessLogs(s)
}

func pruneLLMEndpointAccessLogs(store *llmEndpointAccessLogStore) {
	if store == nil {
		return
	}
	if len(store.Entries) > llmEndpointAccessLogsKeepEntries {
		store.Entries = append([]llmEndpointAccessLogEntry(nil), store.Entries[len(store.Entries)-llmEndpointAccessLogsKeepEntries:]...)
	}
}

func mergeLLMEndpointAccessLogs(dst, src *llmEndpointAccessLogStore) {
	if dst == nil || src == nil {
		return
	}
	if dst.IPCounts == nil {
		dst.IPCounts = map[string]int64{}
	}
	dst.TotalRequests += src.TotalRequests
	for ip, count := range src.IPCounts {
		dst.IPCounts[ip] += count
	}
	dst.Entries = append(dst.Entries, src.Entries...)
	sort.Slice(dst.Entries, func(i, j int) bool {
		return dst.Entries[i].CreatedAt.Before(dst.Entries[j].CreatedAt)
	})
	pruneLLMEndpointAccessLogs(dst)
}

func loadLLMEndpointAccessLogs(ctx context.Context, system store.SystemSettingsRepository) (*llmEndpointAccessLogStore, error) {
	if system == nil {
		return newLLMEndpointAccessLogStore(), nil
	}
	raw, err := system.Get(ctx, llmEndpointAccessLogsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return newLLMEndpointAccessLogStore(), nil
	}
	var out llmEndpointAccessLogStore
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out.Version == 0 {
		out.Version = llmEndpointAccessLogsVersion
	}
	if out.IPCounts == nil {
		out.IPCounts = map[string]int64{}
	}
	if out.Entries == nil {
		out.Entries = []llmEndpointAccessLogEntry{}
	}
	pruneLLMEndpointAccessLogs(&out)
	return &out, nil
}

func saveLLMEndpointAccessLogs(ctx context.Context, system store.SystemSettingsRepository, logs *llmEndpointAccessLogStore) error {
	if system == nil {
		return nil
	}
	if logs == nil {
		logs = newLLMEndpointAccessLogStore()
	}
	logs.Version = llmEndpointAccessLogsVersion
	if logs.IPCounts == nil {
		logs.IPCounts = map[string]int64{}
	}
	if logs.Entries == nil {
		logs.Entries = []llmEndpointAccessLogEntry{}
	}
	pruneLLMEndpointAccessLogs(logs)
	data, err := json.Marshal(logs)
	if err != nil {
		return err
	}
	return system.Set(ctx, llmEndpointAccessLogsKey, string(data))
}

func flushLLMEndpointAccessLogs(ctx context.Context, system store.SystemSettingsRepository, pending *llmEndpointAccessLogStore) error {
	if pending == nil || pending.TotalRequests == 0 {
		return nil
	}
	stored, err := loadLLMEndpointAccessLogs(ctx, system)
	if err != nil {
		return err
	}
	mergeLLMEndpointAccessLogs(stored, pending)
	return saveLLMEndpointAccessLogs(ctx, system, stored)
}

func currentLLMEndpointAccessLogs(ctx context.Context, system store.SystemSettingsRepository) (*llmEndpointAccessLogStore, error) {
	stored, err := loadLLMEndpointAccessLogs(ctx, system)
	if err != nil {
		return nil, err
	}
	pending := globalLLMEndpointAccessLogAccumulator.snapshot(system)
	if pending != nil {
		mergeLLMEndpointAccessLogs(stored, pending)
	}
	return stored, nil
}

func buildLLMEndpointAccessLogSummary(logs *llmEndpointAccessLogStore) llmEndpointAccessLogSummary {
	summary := llmEndpointAccessLogSummary{}
	if logs == nil {
		return summary
	}
	summary.TotalRequests = logs.TotalRequests
	summary.UniqueIPCount = len(logs.IPCounts)
	if n := len(logs.Entries); n > 0 {
		summary.LatestRequestAt = logs.Entries[n-1].CreatedAt.UTC().Format(time.RFC3339)
	}
	return summary
}

func trimLLMEndpointAccessLogBody(body string) string {
	body = strings.TrimSpace(body)
	if len(body) <= llmEndpointAccessLogsBodyLimit {
		return body
	}
	return body[:llmEndpointAccessLogsBodyLimit] + "\n...[truncated]"
}

func llmEndpointClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	for _, key := range []string{"X-Forwarded-For", "X-Real-IP"} {
		value := strings.TrimSpace(r.Header.Get(key))
		if value == "" {
			continue
		}
		if key == "X-Forwarded-For" && strings.Contains(value, ",") {
			value = strings.TrimSpace(strings.Split(value, ",")[0])
		}
		if value != "" {
			return value
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return strings.TrimSpace(host)
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func llmEndpointRequestedModel(body map[string]any) string {
	if body == nil {
		return ""
	}
	value, ok := body["model"]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func llmEndpointUpstreamHost(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Host)
}

func llmEndpointMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

type llmEndpointAccessLogFilter struct {
	ProviderID   string
	UpstreamHost string
	ClientIP     string
	Email        string
	Query        string
	CachedOnly   bool
	StartAt      time.Time
	EndAt        time.Time
}

func parseLLMEndpointAccessLogFilterTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func parseLLMEndpointAccessLogBool(raw string) bool {
	value := strings.TrimSpace(strings.ToLower(raw))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func filterLLMEndpointAccessLogs(entries []llmEndpointAccessLogEntry, filter llmEndpointAccessLogFilter) []llmEndpointAccessLogEntry {
	providerID := strings.ToLower(strings.TrimSpace(filter.ProviderID))
	upstreamHost := strings.ToLower(strings.TrimSpace(filter.UpstreamHost))
	clientIP := strings.ToLower(strings.TrimSpace(filter.ClientIP))
	email := strings.ToLower(strings.TrimSpace(filter.Email))
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	if providerID == "" && upstreamHost == "" && clientIP == "" && email == "" && query == "" && !filter.CachedOnly && filter.StartAt.IsZero() && filter.EndAt.IsZero() {
		return append([]llmEndpointAccessLogEntry(nil), entries...)
	}
	filtered := make([]llmEndpointAccessLogEntry, 0, len(entries))
	for _, item := range entries {
		if !filter.StartAt.IsZero() && item.CreatedAt.Before(filter.StartAt) {
			continue
		}
		if !filter.EndAt.IsZero() && item.CreatedAt.After(filter.EndAt) {
			continue
		}
		if providerID != "" && !strings.Contains(strings.ToLower(strings.TrimSpace(item.ProviderID)), providerID) {
			continue
		}
		if upstreamHost != "" && !strings.Contains(strings.ToLower(llmEndpointMetadataString(item.Metadata, "upstream_host")), upstreamHost) {
			continue
		}
		if clientIP != "" && !strings.Contains(strings.ToLower(strings.TrimSpace(item.ClientIP)), clientIP) {
			continue
		}
		if email != "" && !strings.Contains(strings.ToLower(strings.TrimSpace(item.Email)), email) {
			continue
		}
		if filter.CachedOnly && item.CachedInputTokens <= 0 && item.CacheWriteTokens <= 0 {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{
				strings.TrimSpace(item.ProviderID),
				llmEndpointMetadataString(item.Metadata, "upstream_host"),
				strings.TrimSpace(item.ClientIP),
				strings.TrimSpace(item.Email),
				strings.TrimSpace(item.RequestedModel),
				strings.TrimSpace(item.AuthorizedModel),
				strings.TrimSpace(item.ErrorCode),
			}, "\n"))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func buildLLMEndpointAccessLogSummaryFromEntries(totalRequests int64, entries []llmEndpointAccessLogEntry) llmEndpointAccessLogSummary {
	summary := llmEndpointAccessLogSummary{TotalRequests: totalRequests}
	if len(entries) == 0 {
		return summary
	}
	uniqueIPs := map[string]struct{}{}
	latest := entries[0].CreatedAt
	for _, entry := range entries {
		if ip := strings.TrimSpace(entry.ClientIP); ip != "" {
			uniqueIPs[ip] = struct{}{}
		}
		if entry.CreatedAt.After(latest) {
			latest = entry.CreatedAt
		}
	}
	summary.UniqueIPCount = len(uniqueIPs)
	if !latest.IsZero() {
		summary.LatestRequestAt = latest.UTC().Format(time.RFC3339)
	}
	return summary
}

func GetLLMEndpointAccessLogsHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logs, err := currentLLMEndpointAccessLogs(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_ENDPOINT_ACCESS_LOG_LOAD_FAILED", err.Error())
			return
		}
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
		if limit <= 0 {
			limit = 50
		}
		if limit > 200 {
			limit = 200
		}
		if offset < 0 {
			offset = 0
		}
		entries := filterLLMEndpointAccessLogs(logs.Entries, llmEndpointAccessLogFilter{
			ProviderID:   r.URL.Query().Get("provider"),
			UpstreamHost: r.URL.Query().Get("upstream_host"),
			ClientIP:     r.URL.Query().Get("client_ip"),
			Email:        r.URL.Query().Get("email"),
			Query:        r.URL.Query().Get("q"),
			CachedOnly:   parseLLMEndpointAccessLogBool(r.URL.Query().Get("cached_only")),
			StartAt:      parseLLMEndpointAccessLogFilterTime(r.URL.Query().Get("start_at")),
			EndAt:        parseLLMEndpointAccessLogFilterTime(r.URL.Query().Get("end_at")),
		})
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].CreatedAt.After(entries[j].CreatedAt)
		})
		total := len(entries)
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}
		views := make([]llmEndpointAccessLogView, 0, end-offset)
		for _, item := range entries[offset:end] {
			views = append(views, llmEndpointAccessLogView{
				ID:                item.ID,
				Email:             item.Email,
				ClientIP:          item.ClientIP,
				RequestedModel:    item.RequestedModel,
				AuthorizedModel:   item.AuthorizedModel,
				ProviderID:        item.ProviderID,
				StatusCode:        item.StatusCode,
				ErrorCode:         item.ErrorCode,
				InputTokens:       item.InputTokens,
				OutputTokens:      item.OutputTokens,
				TotalTokens:       item.TotalTokens,
				CachedInputTokens: item.CachedInputTokens,
				CacheWriteTokens:  item.CacheWriteTokens,
				RequestBytes:      item.RequestBytes,
				RequestBody:       item.RequestBody,
				CreatedAt:         item.CreatedAt.UTC().Format(time.RFC3339),
				Metadata:          item.Metadata,
			})
		}
		writeJSON(w, http.StatusOK, llmEndpointAccessLogResponse{
			Summary: buildLLMEndpointAccessLogSummaryFromEntries(int64(total), entries),
			Logs:    views,
			Total:   total,
			Offset:  offset,
			Limit:   limit,
		})
	}
}
