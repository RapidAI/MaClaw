package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type maclawAppSubmissionRecord struct {
	SubmissionID   string                     `json:"submission_id"`
	SubmittedAt    string                     `json:"submitted_at"`
	Status         string                     `json:"status"`
	Channel        string                     `json:"channel"`
	AppIDs         []string                   `json:"app_ids"`
	AppNames       []string                   `json:"app_names,omitempty"`
	PackageSHA     string                     `json:"package_sha256,omitempty"`
	PackageSize    int                        `json:"package_bytes,omitempty"`
	ReviewedAt     string                     `json:"reviewed_at,omitempty"`
	PublishedAt    string                     `json:"published_at,omitempty"`
	Reviewer       string                     `json:"reviewer,omitempty"`
	RiskLevel      string                     `json:"risk_level,omitempty"`
	ApprovedScopes []string                   `json:"approved_scopes,omitempty"`
	ReviewIssues   []maclawAppReviewIssue     `json:"review_issues,omitempty"`
	Events         []maclawAppSubmissionEvent `json:"events,omitempty"`
	Package        map[string]any             `json:"package"`
	Message        string                     `json:"message"`
}

type maclawAppSubmissionQueue struct {
	Schema      string                      `json:"schema"`
	UpdatedAt   string                      `json:"updated_at"`
	Submissions []maclawAppSubmissionRecord `json:"submissions"`
}

type maclawAppSubmissionSummary struct {
	SubmissionID   string                 `json:"submission_id"`
	SubmittedAt    string                 `json:"submitted_at"`
	Status         string                 `json:"status"`
	Channel        string                 `json:"channel"`
	AppIDs         []string               `json:"app_ids"`
	AppNames       []string               `json:"app_names,omitempty"`
	PackageSHA     string                 `json:"package_sha256,omitempty"`
	PackageSize    int                    `json:"package_bytes,omitempty"`
	ReviewedAt     string                 `json:"reviewed_at,omitempty"`
	PublishedAt    string                 `json:"published_at,omitempty"`
	Reviewer       string                 `json:"reviewer,omitempty"`
	RiskLevel      string                 `json:"risk_level,omitempty"`
	ApprovedScopes []string               `json:"approved_scopes,omitempty"`
	ReviewIssues   []maclawAppReviewIssue `json:"review_issues,omitempty"`
	EventCount     int                    `json:"event_count,omitempty"`
	LastEventAt    string                 `json:"last_event_at,omitempty"`
	Message        string                 `json:"message"`
}

type maclawAppReviewIssue struct {
	Path       string `json:"path,omitempty"`
	Severity   string `json:"severity,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type maclawAppSubmissionEvent struct {
	At           string `json:"at"`
	Status       string `json:"status"`
	Channel      string `json:"channel"`
	SubmissionID string `json:"submission_id"`
	Message      string `json:"message,omitempty"`
	Reviewer     string `json:"reviewer,omitempty"`
}

type maclawAppSubmissionStatusUpdate struct {
	Status         string                 `json:"status"`
	Channel        string                 `json:"channel"`
	Message        string                 `json:"message"`
	SubmissionID   string                 `json:"submission_id"`
	ReviewedAt     string                 `json:"reviewed_at"`
	PublishedAt    string                 `json:"published_at"`
	Reviewer       string                 `json:"reviewer"`
	RiskLevel      string                 `json:"risk_level"`
	ApprovedScopes []string               `json:"approved_scopes"`
	ReviewIssues   []maclawAppReviewIssue `json:"review_issues"`
}

// SubmitMaclawAppPackage stores a maclaw.app.pack.v1 submission in the local
// durable queue. Enterprise Hub upload can later consume the same package JSON.
func (a *App) SubmitMaclawAppPackage(packageJSON string) (map[string]any, error) {
	pkg, appIDs, appNames, err := parseMaclawAppPackage(packageJSON)
	if err != nil {
		return nil, err
	}
	packageSHA, packageSize, err := maclawAppPackageFingerprint(pkg)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	submissionID := "local-review-" + firstMaclawAppID(appIDs) + "-" + shortRandomHex()
	record := maclawAppSubmissionRecord{
		SubmissionID: submissionID,
		SubmittedAt:  now,
		Status:       "submitted",
		Channel:      "local",
		AppIDs:       appIDs,
		AppNames:     appNames,
		PackageSHA:   packageSHA,
		PackageSize:  packageSize,
		Package:      pkg,
		Message:      "queued locally for enterprise market sync",
	}
	record.Events = append(record.Events, record.maclawAppSubmissionEvent(now))
	if err := a.appendMaclawAppSubmission(record); err != nil {
		return nil, err
	}
	return map[string]any{
		"submission_id":  submissionID,
		"submitted_at":   now,
		"status":         record.Status,
		"channel":        record.Channel,
		"app_ids":        append([]string(nil), record.AppIDs...),
		"app_names":      append([]string(nil), record.AppNames...),
		"package_sha256": record.PackageSHA,
		"package_bytes":  record.PackageSize,
		"message":        record.Message,
	}, nil
}

// ListMaclawAppPackageSubmissions returns newest-first submission summaries
// without exposing full package payloads.
func (a *App) ListMaclawAppPackageSubmissions(limit int) ([]maclawAppSubmissionSummary, error) {
	queue, err := a.readMaclawAppSubmissionQueue()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if limit > len(queue.Submissions) {
		limit = len(queue.Submissions)
	}
	summaries := make([]maclawAppSubmissionSummary, 0, limit)
	for _, record := range queue.Submissions[:limit] {
		summaries = append(summaries, record.maclawAppSubmissionSummary())
	}
	return summaries, nil
}

// GetMaclawAppPackageSubmission returns a full queued submission, including the
// maclaw.app.pack.v1 package payload, for sync workers and audit diagnostics.
func (a *App) GetMaclawAppPackageSubmission(submissionID string) (*maclawAppSubmissionRecord, error) {
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return nil, fmt.Errorf("submission_id is required")
	}
	queue, err := a.readMaclawAppSubmissionQueue()
	if err != nil {
		return nil, err
	}
	for _, record := range queue.Submissions {
		if record.SubmissionID != submissionID {
			continue
		}
		cloned := record
		cloned.AppIDs = append([]string(nil), record.AppIDs...)
		cloned.AppNames = append([]string(nil), record.AppNames...)
		cloned.ApprovedScopes = append([]string(nil), record.ApprovedScopes...)
		cloned.ReviewIssues = cloneMaclawAppReviewIssues(record.ReviewIssues)
		cloned.Events = append([]maclawAppSubmissionEvent(nil), record.Events...)
		cloned.Package = cloneMapAny(record.Package)
		if cloned.PackageSHA == "" || cloned.PackageSize == 0 {
			packageSHA, packageSize, _ := maclawAppPackageFingerprint(cloned.Package)
			cloned.PackageSHA = packageSHA
			cloned.PackageSize = packageSize
		}
		return &cloned, nil
	}
	return nil, nil
}

// WithdrawMaclawAppPackageSubmission removes a local pending submission from the
// durable queue. Hub-backed submissions must be withdrawn through the market.
func (a *App) WithdrawMaclawAppPackageSubmission(submissionID string) (bool, error) {
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return false, fmt.Errorf("submission_id is required")
	}
	queue, err := a.readMaclawAppSubmissionQueue()
	if err != nil {
		return false, err
	}
	next := queue.Submissions[:0]
	removed := false
	for _, record := range queue.Submissions {
		if record.SubmissionID == submissionID {
			if record.Channel != "" && record.Channel != "local" {
				return false, fmt.Errorf("submission %s is %s-backed and cannot be removed from the local queue", submissionID, record.Channel)
			}
			removed = true
			continue
		}
		next = append(next, record)
	}
	if !removed {
		return false, nil
	}
	queue.Submissions = next
	queue.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return true, a.writeMaclawAppSubmissionQueue(queue)
}

// UpdateMaclawAppPackageSubmissionStatus updates a queued submission after a
// sync worker or enterprise market review callback reports a new state.
func (a *App) UpdateMaclawAppPackageSubmissionStatus(submissionID string, update maclawAppSubmissionStatusUpdate) (bool, error) {
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return false, fmt.Errorf("submission_id is required")
	}
	status := normalizeMaclawAppSubmissionStatus(update.Status)
	if status == "" {
		return false, fmt.Errorf("invalid maclaw app submission status")
	}
	channel := strings.TrimSpace(update.Channel)
	if channel == "" {
		channel = "hub"
	}
	if channel != "local" && channel != "hub" {
		return false, fmt.Errorf("invalid maclaw app submission channel")
	}
	queue, err := a.readMaclawAppSubmissionQueue()
	if err != nil {
		return false, err
	}
	for i := range queue.Submissions {
		if queue.Submissions[i].SubmissionID != submissionID {
			continue
		}
		if nextID := strings.TrimSpace(update.SubmissionID); nextID != "" {
			for j := range queue.Submissions {
				if j != i && queue.Submissions[j].SubmissionID == nextID {
					return false, fmt.Errorf("submission_id %s already exists", nextID)
				}
			}
			queue.Submissions[i].SubmissionID = nextID
		}
		now := time.Now().UTC().Format(time.RFC3339)
		queue.Submissions[i].Status = status
		queue.Submissions[i].Channel = channel
		queue.Submissions[i].Message = strings.TrimSpace(update.Message)
		queue.Submissions[i].Reviewer = strings.TrimSpace(update.Reviewer)
		queue.Submissions[i].RiskLevel = normalizeMaclawAppRiskLevel(update.RiskLevel)
		queue.Submissions[i].ApprovedScopes = normalizeMaclawAppScopes(update.ApprovedScopes)
		queue.Submissions[i].ReviewIssues = normalizeMaclawAppReviewIssues(update.ReviewIssues)
		if reviewedAt := strings.TrimSpace(update.ReviewedAt); reviewedAt != "" {
			queue.Submissions[i].ReviewedAt = reviewedAt
		} else if status != "submitted" {
			queue.Submissions[i].ReviewedAt = now
		}
		if publishedAt := strings.TrimSpace(update.PublishedAt); publishedAt != "" {
			queue.Submissions[i].PublishedAt = publishedAt
		} else if status == "published" {
			queue.Submissions[i].PublishedAt = now
		}
		queue.Submissions[i].Events = append(queue.Submissions[i].Events, queue.Submissions[i].maclawAppSubmissionEvent(now))
		queue.UpdatedAt = now
		return true, a.writeMaclawAppSubmissionQueue(queue)
	}
	return false, nil
}

func parseMaclawAppPackage(packageJSON string) (map[string]any, []string, []string, error) {
	if strings.TrimSpace(packageJSON) == "" {
		return nil, nil, nil, fmt.Errorf("maclaw app package is empty")
	}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(packageJSON), &pkg); err != nil {
		return nil, nil, nil, fmt.Errorf("decode maclaw app package: %w", err)
	}
	if stringMapValue(pkg, "schema") != "maclaw.app.pack.v1" {
		return nil, nil, nil, fmt.Errorf("maclaw app package schema must be maclaw.app.pack.v1")
	}
	if stringMapValue(pkg, "privateMarker") != "x_maclaw_apps" {
		return nil, nil, nil, fmt.Errorf("maclaw app package privateMarker must be x_maclaw_apps")
	}
	rawApps, ok := pkg["apps"].([]any)
	if !ok || len(rawApps) == 0 {
		return nil, nil, nil, fmt.Errorf("maclaw app package apps must be a non-empty array")
	}
	appIDs := make([]string, 0, len(rawApps))
	appNames := make([]string, 0, len(rawApps))
	seenIDs := make(map[string]struct{}, len(rawApps))
	for i, raw := range rawApps {
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, nil, fmt.Errorf("maclaw app package apps[%d] must be an object", i)
		}
		if stringMapValue(entry, "schema") != "maclaw.app.v1" {
			return nil, nil, nil, fmt.Errorf("maclaw app package apps[%d].schema must be maclaw.app.v1", i)
		}
		if stringMapValue(entry, "privateMarker") != "x_maclaw_apps" {
			return nil, nil, nil, fmt.Errorf("maclaw app package apps[%d].privateMarker must be x_maclaw_apps", i)
		}
		app, ok := entry["app"].(map[string]any)
		if !ok {
			return nil, nil, nil, fmt.Errorf("maclaw app package apps[%d].app must be an object", i)
		}
		appID := strings.TrimSpace(stringMapValue(app, "id"))
		if appID == "" {
			return nil, nil, nil, fmt.Errorf("maclaw app package apps[%d].app.id is required", i)
		}
		if _, ok := seenIDs[appID]; ok {
			return nil, nil, nil, fmt.Errorf("maclaw app package apps[%d].app.id duplicates %q", i, appID)
		}
		seenIDs[appID] = struct{}{}
		appIDs = append(appIDs, appID)
		appNames = append(appNames, stringMapValue(app, "name"))
	}
	return pkg, appIDs, appNames, nil
}

func (a *App) appendMaclawAppSubmission(record maclawAppSubmissionRecord) error {
	queue, err := a.readMaclawAppSubmissionQueue()
	if err != nil {
		return err
	}
	if queue.Schema == "" {
		queue.Schema = "maclaw.app.submissions.v1"
	}
	queue.UpdatedAt = record.SubmittedAt
	queue.Submissions = append([]maclawAppSubmissionRecord{record}, queue.Submissions...)
	if len(queue.Submissions) > 200 {
		queue.Submissions = queue.Submissions[:200]
	}
	return a.writeMaclawAppSubmissionQueue(queue)
}

func (a *App) writeMaclawAppSubmissionQueue(queue maclawAppSubmissionQueue) error {
	path := a.maclawAppSubmissionQueuePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(queue, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (a *App) readMaclawAppSubmissionQueue() (maclawAppSubmissionQueue, error) {
	path := a.maclawAppSubmissionQueuePath()
	queue := maclawAppSubmissionQueue{Schema: "maclaw.app.submissions.v1"}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return queue, nil
	}
	if err != nil {
		return queue, err
	}
	if len(data) == 0 {
		return queue, nil
	}
	if err := json.Unmarshal(data, &queue); err != nil {
		return queue, fmt.Errorf("decode maclaw app submission queue: %w", err)
	}
	if queue.Schema == "" {
		queue.Schema = "maclaw.app.submissions.v1"
	}
	return queue, nil
}

func (a *App) maclawAppSubmissionQueuePath() string {
	return filepath.Join(a.GetDataDir(), "app_market_submissions.json")
}

func firstMaclawAppID(ids []string) string {
	if len(ids) == 0 {
		return "app"
	}
	id := strings.ToLower(ids[0])
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

func shortRandomHex() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func stringMapValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func (record maclawAppSubmissionRecord) maclawAppSubmissionSummary() maclawAppSubmissionSummary {
	packageSHA := record.PackageSHA
	packageSize := record.PackageSize
	if packageSHA == "" || packageSize == 0 {
		computedSHA, computedSize, _ := maclawAppPackageFingerprint(record.Package)
		if packageSHA == "" {
			packageSHA = computedSHA
		}
		if packageSize == 0 {
			packageSize = computedSize
		}
	}
	eventCount := len(record.Events)
	lastEventAt := ""
	if eventCount > 0 {
		lastEventAt = record.Events[eventCount-1].At
	} else if record.SubmittedAt != "" {
		eventCount = 1
		lastEventAt = record.SubmittedAt
	}
	return maclawAppSubmissionSummary{
		SubmissionID:   record.SubmissionID,
		SubmittedAt:    record.SubmittedAt,
		Status:         record.Status,
		Channel:        record.Channel,
		AppIDs:         append([]string(nil), record.AppIDs...),
		AppNames:       append([]string(nil), record.AppNames...),
		PackageSHA:     packageSHA,
		PackageSize:    packageSize,
		ReviewedAt:     record.ReviewedAt,
		PublishedAt:    record.PublishedAt,
		Reviewer:       record.Reviewer,
		RiskLevel:      record.RiskLevel,
		ApprovedScopes: append([]string(nil), record.ApprovedScopes...),
		ReviewIssues:   cloneMaclawAppReviewIssues(record.ReviewIssues),
		EventCount:     eventCount,
		LastEventAt:    lastEventAt,
		Message:        record.Message,
	}
}

func (record maclawAppSubmissionRecord) maclawAppSubmissionEvent(at string) maclawAppSubmissionEvent {
	return maclawAppSubmissionEvent{
		At:           at,
		Status:       record.Status,
		Channel:      record.Channel,
		SubmissionID: record.SubmissionID,
		Message:      record.Message,
		Reviewer:     record.Reviewer,
	}
}

func normalizeMaclawAppRiskLevel(value string) string {
	switch strings.TrimSpace(value) {
	case "low", "medium", "high", "critical":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func normalizeMaclawAppScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(scopes))
	normalized := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		normalized = append(normalized, scope)
	}
	return normalized
}

func normalizeMaclawAppReviewIssues(issues []maclawAppReviewIssue) []maclawAppReviewIssue {
	if len(issues) == 0 {
		return nil
	}
	normalized := make([]maclawAppReviewIssue, 0, len(issues))
	for _, issue := range issues {
		message := strings.TrimSpace(issue.Message)
		if message == "" {
			continue
		}
		severity := strings.TrimSpace(issue.Severity)
		switch severity {
		case "", "info", "warning", "error", "critical":
		default:
			severity = "warning"
		}
		normalized = append(normalized, maclawAppReviewIssue{
			Path:       strings.TrimSpace(issue.Path),
			Severity:   severity,
			Message:    message,
			Suggestion: strings.TrimSpace(issue.Suggestion),
		})
	}
	return normalized
}

func cloneMaclawAppReviewIssues(issues []maclawAppReviewIssue) []maclawAppReviewIssue {
	if len(issues) == 0 {
		return nil
	}
	return append([]maclawAppReviewIssue(nil), issues...)
}

func maclawAppPackageFingerprint(pkg map[string]any) (string, int, error) {
	if pkg == nil {
		return "", 0, nil
	}
	data, err := json.Marshal(pkg)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), len(data), nil
}

func cloneMapAny(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return value
	}
	return cloned
}

func normalizeMaclawAppSubmissionStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "submitted", "review_failed", "approved", "published", "deprecated", "revoked":
		return strings.TrimSpace(status)
	default:
		return ""
	}
}
