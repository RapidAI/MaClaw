package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAdminLogTailLines = 200
	maxAdminLogTailLines     = 2000
	maxAdminLogReadBytes     = 2 << 20
)

type adminLogSource struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Path        string    `json:"path,omitempty"`
	Exists      bool      `json:"exists"`
	SizeBytes   int64     `json:"size_bytes,omitempty"`
	ModifiedAt  time.Time `json:"modified_at,omitempty"`
}

type adminLogLine struct {
	Number int    `json:"number"`
	Level  string `json:"level,omitempty"`
	Text   string `json:"text"`
}

type adminLogReadResponse struct {
	Source    adminLogSource `json:"source"`
	Tail      int            `json:"tail"`
	Level     string         `json:"level,omitempty"`
	Query     string         `json:"q,omitempty"`
	Lines     []adminLogLine `json:"lines"`
	Truncated bool           `json:"truncated"`
}

type adminRecentLogLine struct {
	SourceID   string       `json:"source_id"`
	SourceName string       `json:"source_name"`
	ModifiedAt time.Time    `json:"modified_at,omitempty"`
	Line       adminLogLine `json:"line"`
}

type adminLogSearchRequest struct {
	Sources []string `json:"sources,omitempty"`
	Level   string   `json:"level,omitempty"`
	Query   string   `json:"q,omitempty"`
	Tail    int      `json:"tail,omitempty"`
	Limit   int      `json:"limit,omitempty"`
}

type adminLogSearchResponse struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Sources     []string             `json:"sources"`
	Level       string               `json:"level,omitempty"`
	Query       string               `json:"q,omitempty"`
	Tail        int                  `json:"tail"`
	Limit       int                  `json:"limit"`
	Items       []adminRecentLogLine `json:"items"`
}

type adminLogRotateResponse struct {
	Source      adminLogSource `json:"source"`
	RotatedTo   string         `json:"rotated_to,omitempty"`
	Rotated     bool           `json:"rotated"`
	CreatedNew  bool           `json:"created_new"`
	GeneratedAt time.Time      `json:"generated_at"`
}

func (s *HTTPServer) handleAdminLogSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": redactAdminLogSourcesForAdminAPI(s.svc.DataRoot(), adminLogSources(s.svc.DataRoot()))})
}

func (s *HTTPServer) handleAdminRecentLogErrors(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = parsed
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	includeWarn := false
	if raw := strings.TrimSpace(r.URL.Query().Get("include_warn")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid include_warn"})
			return
		}
		includeWarn = parsed
	}
	items := recentAdminLogErrors(s.svc.DataRoot(), limit, includeWarn)
	_ = s.recordAdminAudit(r.Context(), "admin.logs_recent_errors_read", "log_source", "all", map[string]string{"limit": strconv.Itoa(limit), "include_warn": strconv.FormatBool(includeWarn), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "include_warn": includeWarn})
}
func (s *HTTPServer) handleAdminLogSearch(w http.ResponseWriter, r *http.Request) {
	var in adminLogSearchRequest
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	level := strings.ToLower(strings.TrimSpace(in.Level))
	if level != "" && level != "error" && level != "warn" && level != "info" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid level"})
		return
	}
	query := strings.ToLower(strings.TrimSpace(in.Query))
	if query == "" && level == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q or level is required"})
		return
	}
	tail := in.Tail
	if tail <= 0 {
		tail = maxAdminLogTailLines
	}
	if tail > maxAdminLogTailLines {
		tail = maxAdminLogTailLines
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	items, sourceIDs, err := searchAdminLogLines(s.svc.DataRoot(), in.Sources, tail, limit, level, query)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.logs_search", "log_source", "all", map[string]string{"sources": strings.Join(sourceIDs, ","), "tail": strconv.Itoa(tail), "limit": strconv.Itoa(limit), "level": level, "q": redactShort(query), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, adminLogSearchResponse{GeneratedAt: time.Now().UTC(), Sources: sourceIDs, Level: level, Query: redactShort(query), Tail: tail, Limit: limit, Items: items})
}
func (s *HTTPServer) handleAdminLogDownload(w http.ResponseWriter, r *http.Request) {
	sourceID := strings.TrimSpace(r.PathValue("sourceId"))
	source, ok := findAdminLogSource(s.svc.DataRoot(), sourceID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "log source not found"})
		return
	}
	tail, level, query, ok := parseAdminLogReadFilters(w, r)
	if !ok {
		return
	}
	lines, _, err := readAdminLogLines(source.Path, tail, level, query)
	if err != nil {
		if os.IsNotExist(err) {
			lines = []adminLogLine{}
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
			return
		}
	}
	_ = s.recordAdminAudit(r.Context(), "admin.logs_download", "log_source", source.ID, map[string]string{"tail": strconv.Itoa(tail), "level": level, "q": redactShort(query), "remote_ip": requestClientIP(r)})
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"maclawsrv-%s-log.txt\"", source.ID))
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, formatAdminLogLinesText(source, lines))
}
func (s *HTTPServer) handleAdminLogRotate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if err := requireAdminConfirmation(r, "log rotation"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	sourceID := strings.TrimSpace(r.PathValue("sourceId"))
	source, ok := findAdminLogSource(s.svc.DataRoot(), sourceID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "log source not found"})
		return
	}
	out, err := rotateAdminLogSource(source)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.logs_rotate", "log_source", source.ID, map[string]string{"rotated": strconv.FormatBool(out.Rotated), "rotated_to": filepath.Base(out.RotatedTo), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, redactAdminLogRotateResponseForAdminAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleAdminLogRead(w http.ResponseWriter, r *http.Request) {
	sourceID := strings.TrimSpace(r.PathValue("sourceId"))
	source, ok := findAdminLogSource(s.svc.DataRoot(), sourceID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "log source not found"})
		return
	}
	tail, level, query, ok := parseAdminLogReadFilters(w, r)
	if !ok {
		return
	}
	lines, truncated, err := readAdminLogLines(source.Path, tail, level, query)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, adminLogReadResponse{Source: redactAdminLogSourceForAdminAPI(s.svc.DataRoot(), source), Tail: tail, Level: level, Query: redactShort(query), Lines: []adminLogLine{}})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.logs_read", "log_source", source.ID, map[string]string{"tail": strconv.Itoa(tail), "level": level, "q": redactShort(query), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, adminLogReadResponse{Source: redactAdminLogSourceForAdminAPI(s.svc.DataRoot(), source), Tail: tail, Level: level, Query: redactShort(query), Lines: lines, Truncated: truncated})
}

func redactAdminLogSourceForAdminAPI(dataRoot string, source adminLogSource) adminLogSource {
	source.Path = redactSupportBundleValue(dataRoot, source.Path)
	return source
}

func redactAdminLogSourcesForAdminAPI(dataRoot string, sources []adminLogSource) []adminLogSource {
	out := make([]adminLogSource, 0, len(sources))
	for _, source := range sources {
		out = append(out, redactAdminLogSourceForAdminAPI(dataRoot, source))
	}
	return out
}

func redactAdminLogRotateResponseForAdminAPI(dataRoot string, in adminLogRotateResponse) adminLogRotateResponse {
	in.Source = redactAdminLogSourceForAdminAPI(dataRoot, in.Source)
	in.RotatedTo = redactSupportBundleValue(dataRoot, in.RotatedTo)
	return in
}
func rotateAdminLogSource(source adminLogSource) (adminLogRotateResponse, error) {
	if source.Path == "" {
		return adminLogRotateResponse{}, fmt.Errorf("log source path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(source.Path), 0o700); err != nil {
		return adminLogRotateResponse{}, err
	}
	now := time.Now().UTC()
	out := adminLogRotateResponse{Source: source, GeneratedAt: now}
	if source.Exists {
		rotatedTo := fmt.Sprintf("%s.%s", source.Path, now.Format("20060102T150405Z"))
		for i := 1; ; i++ {
			if _, err := os.Stat(rotatedTo); os.IsNotExist(err) {
				break
			}
			rotatedTo = fmt.Sprintf("%s.%s.%d", source.Path, now.Format("20060102T150405Z"), i)
		}
		if err := os.Rename(source.Path, rotatedTo); err != nil {
			return adminLogRotateResponse{}, err
		}
		out.Rotated = true
		out.RotatedTo = rotatedTo
	}
	f, err := os.OpenFile(source.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return adminLogRotateResponse{}, err
	}
	if err := f.Close(); err != nil {
		return adminLogRotateResponse{}, err
	}
	out.CreatedNew = true
	if refreshed, err := os.Stat(source.Path); err == nil {
		out.Source.Exists = true
		out.Source.SizeBytes = refreshed.Size()
		out.Source.ModifiedAt = refreshed.ModTime().UTC()
	}
	return out, nil
}

func parseAdminLogReadFilters(w http.ResponseWriter, r *http.Request) (int, string, string, bool) {
	tail := defaultAdminLogTailLines
	if value := strings.TrimSpace(r.URL.Query().Get("tail")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tail"})
			return 0, "", "", false
		}
		tail = parsed
		if tail > maxAdminLogTailLines {
			tail = maxAdminLogTailLines
		}
	}
	level := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("level")))
	if level != "" && level != "error" && level != "warn" && level != "info" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid level"})
		return 0, "", "", false
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	return tail, level, query, true
}

func formatAdminLogLinesText(source adminLogSource, lines []adminLogLine) string {
	var b strings.Builder
	b.WriteString("# source: ")
	b.WriteString(source.ID)
	b.WriteString("\n")
	for _, line := range lines {
		b.WriteString(strconv.Itoa(line.Number))
		b.WriteString("	")
		b.WriteString(line.Level)
		b.WriteString("	")
		b.WriteString(line.Text)
		b.WriteString("\n")
	}
	return b.String()
}
func adminLogSources(dataRoot string) []adminLogSource {
	paths := []adminLogSource{
		{ID: "service", Name: "MaClawSrv service", Description: "Process log written by the MaClawSrv logger.", Path: defaultServiceLogPath(dataRoot)},
		{ID: "scheduler", Name: "Scheduler", Description: "Optional scheduler log when configured separately.", Path: getenv("MACLAW_SCHEDULER_LOG_FILE", filepath.Join(dataRoot, "logs", "scheduler.log"))},
	}
	for i := range paths {
		if info, err := os.Stat(paths[i].Path); err == nil && !info.IsDir() {
			paths[i].Exists = true
			paths[i].SizeBytes = info.Size()
			paths[i].ModifiedAt = info.ModTime().UTC()
		}
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i].ID < paths[j].ID })
	return paths
}

func findAdminLogSource(dataRoot, id string) (adminLogSource, bool) {
	if !isSafeID(id) {
		return adminLogSource{}, false
	}
	for _, source := range adminLogSources(dataRoot) {
		if source.ID == id {
			return source, true
		}
	}
	return adminLogSource{}, false
}

func searchAdminLogLines(dataRoot string, sourceIDs []string, tail, limit int, level, query string) ([]adminRecentLogLine, []string, error) {
	available := adminLogSources(dataRoot)
	selected := []adminLogSource{}
	if len(sourceIDs) == 0 {
		selected = available
	} else {
		seen := map[string]bool{}
		for _, raw := range sourceIDs {
			id := strings.TrimSpace(raw)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			source, ok := findAdminLogSource(dataRoot, id)
			if !ok {
				return nil, nil, fmt.Errorf("log source not found: %s", id)
			}
			selected = append(selected, source)
		}
	}
	items := []adminRecentLogLine{}
	resolved := make([]string, 0, len(selected))
	for _, source := range selected {
		resolved = append(resolved, source.ID)
		lines, _, err := readAdminLogLines(source.Path, tail, level, query)
		if err != nil {
			continue
		}
		for _, line := range lines {
			items = append(items, adminRecentLogLine{SourceID: source.ID, SourceName: source.Name, ModifiedAt: source.ModifiedAt, Line: line})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ModifiedAt.Equal(items[j].ModifiedAt) {
			if items[i].SourceID == items[j].SourceID {
				return items[i].Line.Number > items[j].Line.Number
			}
			return items[i].SourceID < items[j].SourceID
		}
		return items[i].ModifiedAt.After(items[j].ModifiedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, resolved, nil
}
func recentAdminLogErrors(dataRoot string, limit int, includeWarn bool) []adminRecentLogLine {
	if limit <= 0 {
		return nil
	}
	items := []adminRecentLogLine{}
	for _, source := range adminLogSources(dataRoot) {
		lines, _, err := readAdminLogLines(source.Path, maxAdminLogTailLines, "", "")
		if err != nil {
			continue
		}
		for _, line := range lines {
			if line.Level != "error" && !(includeWarn && line.Level == "warn") {
				continue
			}
			items = append(items, adminRecentLogLine{SourceID: source.ID, SourceName: source.Name, ModifiedAt: source.ModifiedAt, Line: line})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ModifiedAt.Equal(items[j].ModifiedAt) {
			if items[i].SourceID == items[j].SourceID {
				return items[i].Line.Number > items[j].Line.Number
			}
			return items[i].SourceID < items[j].SourceID
		}
		return items[i].ModifiedAt.After(items[j].ModifiedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}
func readAdminLogLines(path string, tail int, level, query string) ([]adminLogLine, bool, error) {
	data, truncated, err := readLogTailBytes(path, maxAdminLogReadBytes)
	if err != nil {
		return nil, false, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	all := []adminLogLine{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		text := redactLogLine(scanner.Text())
		lineLevel := classifyLogLine(text)
		if level != "" && lineLevel != level {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(text), query) {
			continue
		}
		all = append(all, adminLogLine{Number: lineNo, Level: lineLevel, Text: text})
	}
	if err := scanner.Err(); err != nil {
		return nil, truncated, err
	}
	if len(all) > tail {
		all = all[len(all)-tail:]
	}
	return all, truncated, nil
}

func readLogTailBytes(path string, maxBytes int64) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if info.Size() <= maxBytes {
		data, err := io.ReadAll(f)
		return data, false, err
	}
	if _, err := f.Seek(-maxBytes, io.SeekEnd); err != nil {
		return nil, false, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, false, err
	}
	if idx := bytes.IndexByte(data, '\n'); idx >= 0 && idx+1 < len(data) {
		data = data[idx+1:]
	}
	return data, true, nil
}

func classifyLogLine(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "panic") || strings.Contains(lower, "fatal"):
		return "error"
	case strings.Contains(lower, "warn"):
		return "warn"
	default:
		return "info"
	}
}

func redactLogLine(line string) string {
	line = supportBundleBearerPattern.ReplaceAllString(line, "Bearer <redacted>")
	line = supportBundleJSONSecretPattern.ReplaceAllString(line, `${1}"<redacted>"`)
	line = supportBundleInlineSecretPattern.ReplaceAllString(line, `${1}${2}<redacted>`)
	fields := strings.Fields(line)
	redactNext := false
	for i, field := range fields {
		lower := strings.ToLower(strings.Trim(field, "\"'"))
		if redactNext {
			fields[i] = "<redacted>"
			redactNext = lower == "bearer"
			continue
		}
		if lower == "bearer" || strings.HasPrefix(lower, "bearer:") || strings.HasPrefix(lower, "bearer=") {
			fields[i] = redactLogField(field)
			redactNext = true
			continue
		}
		if isSensitiveLogField(lower) {
			fields[i] = redactLogField(field)
			if strings.HasSuffix(lower, "authorization:") || lower == "authorization" || strings.HasSuffix(lower, "auth:") || lower == "auth" {
				redactNext = true
			}
		}
	}
	return strings.Join(fields, " ")
}

func isSensitiveLogField(lower string) bool {
	for _, key := range adminSecretKeyMarkers() {
		if strings.Contains(lower, key) {
			return true
		}
	}
	return false
}

func redactLogField(field string) string {
	if strings.Contains(field, "=") {
		parts := strings.SplitN(field, "=", 2)
		return parts[0] + "=<redacted>"
	}
	if strings.Contains(field, ":") {
		parts := strings.SplitN(field, ":", 2)
		return parts[0] + ":<redacted>"
	}
	return "<redacted>"
}

func defaultServiceLogPath(dataRoot string) string {
	return getenv("MACLAW_LOG_FILE", filepath.Join(dataRoot, "logs", "maclaw_srv.log"))
}
