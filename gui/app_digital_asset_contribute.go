package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

type DigitalAssetContributeRequest struct {
	HubURL          string   `json:"hub_url"`
	HubToken        string   `json:"hub_token"`
	LibraryID       string   `json:"library_id"`
	Title           string   `json:"title"`
	Summary         string   `json:"summary"`
	SourceIDs       []string `json:"source_ids"`
	RedactSensitive bool     `json:"redact_sensitive"`
	ExperienceIDs   []string `json:"experience_ids"`
}

type DigitalAssetSubmissionView struct {
	ID            string   `json:"id"`
	LibraryID     string   `json:"library_id"`
	Kind          string   `json:"kind"`
	Status        string   `json:"status"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	ItemCount     int      `json:"item_count"`
	ReviewNote    string   `json:"review_note"`
	ReviewedAt    string   `json:"reviewed_at"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
	PreviewTitles []string `json:"preview_titles"`
}

type DigitalAssetContributableLibrary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	LibraryKind string `json:"library_kind"`
}

func (a *App) DigitalAssetListContributableLibraries(kind string) ([]DigitalAssetContributableLibrary, error) {
	hubURL, token, err := a.digitalAssetHubAuth("", "")
	if err != nil {
		return nil, err
	}
	q := ""
	if strings.TrimSpace(kind) != "" {
		q = "?kind=" + url.QueryEscape(strings.TrimSpace(kind))
	}
	var payload struct {
		Items []DigitalAssetContributableLibrary `json:"items"`
	}
	if err := a.digitalAssetHubJSON(http.MethodGet, hubURL+"/api/digital-assets/libraries/contributable"+q, token, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (a *App) DigitalAssetListMySubmissions() ([]DigitalAssetSubmissionView, error) {
	hubURL, token, err := a.digitalAssetHubAuth("", "")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Items []DigitalAssetSubmissionView `json:"items"`
	}
	if err := a.digitalAssetHubJSON(http.MethodGet, hubURL+"/api/digital-assets/submissions", token, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (a *App) DigitalAssetWithdrawSubmission(id string) (DigitalAssetSubmissionView, error) {
	hubURL, token, err := a.digitalAssetHubAuth("", "")
	if err != nil {
		return DigitalAssetSubmissionView{}, err
	}
	var view DigitalAssetSubmissionView
	if err := a.digitalAssetHubJSON(http.MethodPost, hubURL+"/api/digital-assets/submissions/"+url.PathEscape(strings.TrimSpace(id))+"/withdraw", token, []byte("{}"), &view); err != nil {
		return DigitalAssetSubmissionView{}, err
	}
	return view, nil
}

func (a *App) KnowledgeContributeToOrg(req DigitalAssetContributeRequest) (DigitalAssetSubmissionView, error) {
	if strings.TrimSpace(req.Summary) == "" {
		return DigitalAssetSubmissionView{}, fmt.Errorf("summary is required")
	}
	if strings.TrimSpace(req.LibraryID) == "" {
		return DigitalAssetSubmissionView{}, fmt.Errorf("library_id is required")
	}
	hubURL, token, err := a.digitalAssetHubAuth(req.HubURL, req.HubToken)
	if err != nil {
		return DigitalAssetSubmissionView{}, err
	}
	store, err := a.openKnowledgeStore()
	if err != nil {
		return DigitalAssetSubmissionView{}, err
	}
	defer store.Close()
	sources, err := store.ListSources(a.knowledgeContext(), knowledge.ListSourcesOptions{
		SourceIDs: compactKnowledgeSourceIDStrings(req.SourceIDs),
		Limit:     5000,
		Status:    "active",
	})
	if err != nil {
		return DigitalAssetSubmissionView{}, err
	}
	filtered := make([]knowledge.Source, 0, len(sources))
	for _, src := range sources {
		if isExcludedPersonalContributeSource(src) {
			continue
		}
		filtered = append(filtered, src)
	}
	if len(filtered) == 0 {
		return DigitalAssetSubmissionView{}, fmt.Errorf("no eligible personal knowledge sources to contribute")
	}
	cfg, _ := a.LoadConfig()
	pkg, _, err := buildGUIKnowledgePackage(a.knowledgeContext(), store, cfg, strings.TrimSpace(req.Title), strings.TrimSpace(req.Summary), filtered, req.RedactSensitive)
	if err != nil {
		return DigitalAssetSubmissionView{}, err
	}
	return a.postDigitalAssetSubmission(hubURL, token, req.LibraryID, "business_knowledge", req.Title, req.Summary, pkg)
}

func (a *App) CodingKnowledgeContributeToOrg(req DigitalAssetContributeRequest) (DigitalAssetSubmissionView, error) {
	if strings.TrimSpace(req.Summary) == "" {
		return DigitalAssetSubmissionView{}, fmt.Errorf("summary is required")
	}
	if strings.TrimSpace(req.LibraryID) == "" {
		return DigitalAssetSubmissionView{}, fmt.Errorf("library_id is required")
	}
	hubURL, token, err := a.digitalAssetHubAuth(req.HubURL, req.HubToken)
	if err != nil {
		return DigitalAssetSubmissionView{}, err
	}
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return DigitalAssetSubmissionView{}, fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	experiences, err := store.ListExperiences(ctx, knowledge.CodingListFilter{Limit: 10000})
	if err != nil {
		return DigitalAssetSubmissionView{}, err
	}
	want := map[string]struct{}{}
	for _, id := range req.ExperienceIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			want[id] = struct{}{}
		}
	}
	items := make([]guiKnowledgePackageSource, 0)
	for _, exp := range experiences {
		if exp.Status != knowledge.CodingStatusActive && exp.Status != knowledge.CodingStatusVerified {
			continue
		}
		if len(want) > 0 {
			if _, ok := want[exp.ID]; !ok {
				continue
			}
		}
		if exp.Content == "" {
			nodes, nerr := store.Inner().ListNodesBySource(ctx, exp.ID, 1)
			if nerr == nil && len(nodes) > 0 {
				exp.Content = nodes[0].Text
			}
		}
		if strings.TrimSpace(exp.Title) == "" || strings.TrimSpace(exp.Content) == "" || strings.TrimSpace(exp.TriggerCondition) == "" {
			continue
		}
		sanitized := knowledge.SanitizeExperienceForExport(exp)
		sanitized.ProjectPath = orgProjectLeaf(exp.ProjectPath)
		if sanitized.Scope == knowledge.CodingScopeProject && sanitized.ProjectPath == "" {
			sanitized.Scope = knowledge.CodingScopeUniversal
		}
		hint, _ := json.Marshal(map[string]any{
			"category":          sanitized.Category,
			"scope":             sanitized.Scope,
			"language":          sanitized.Language,
			"frameworks":        sanitized.Frameworks,
			"trigger_condition": sanitized.TriggerCondition,
			"code_snippet":      sanitized.CodeSnippet,
			"failed_attempts":   sanitized.FailedAttempts,
			"contraindications": sanitized.Contraindications,
			"status":            knowledge.CodingStatusCandidate,
		})
		class := sanitized.Category
		if class == "" {
			class = "pattern"
		}
		items = append(items, guiKnowledgePackageSource{
			Kind:      "coding_experience",
			Title:     sanitized.Title,
			TopicHint: string(hint),
			Labels:    []string{"coding_experience", "experience_class=" + class},
			Content:   sanitized.Content,
		})
	}
	if len(items) == 0 {
		return DigitalAssetSubmissionView{}, fmt.Errorf("no active or verified coding experiences to contribute")
	}
	now := time.Now().UTC()
	pkg := guiKnowledgePackage{
		Manifest: guiKnowledgePackageManifest{
			Format:      "maclaw.knowledge.package",
			Version:     1,
			PackageID:   fmt.Sprintf("kxp_%s", now.Format("20060102T150405Z")),
			Title:       strings.TrimSpace(req.Title),
			Description: strings.TrimSpace(req.Summary),
			CreatedAt:   now.Format(time.RFC3339),
			SourceCount: len(items),
			Editable:    true,
		},
		Sources: items,
	}
	return a.postDigitalAssetSubmission(hubURL, token, req.LibraryID, "coding_experience", req.Title, req.Summary, pkg)
}

func (a *App) postDigitalAssetSubmission(hubURL, token, libraryID, kind, title, summary string, pkg guiKnowledgePackage) (DigitalAssetSubmissionView, error) {
	pkgJSON, err := json.Marshal(pkg)
	if err != nil {
		return DigitalAssetSubmissionView{}, err
	}
	body, err := json.Marshal(map[string]any{
		"library_id": libraryID,
		"kind":       kind,
		"title":      strings.TrimSpace(title),
		"summary":    strings.TrimSpace(summary),
		"package":    json.RawMessage(pkgJSON),
	})
	if err != nil {
		return DigitalAssetSubmissionView{}, err
	}
	var view DigitalAssetSubmissionView
	if err := a.digitalAssetHubJSON(http.MethodPost, hubURL+"/api/digital-assets/submissions", token, body, &view); err != nil {
		return DigitalAssetSubmissionView{}, err
	}
	return view, nil
}

func (a *App) digitalAssetHubAuth(hubURL, token string) (string, string, error) {
	cfg, _ := a.LoadConfig()
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if hubURL == "" {
		hubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	}
	if hubURL == "" {
		return "", "", fmt.Errorf("hub_url is required")
	}
	if parsed, err := url.Parse(hubURL); err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("hub_url must be an absolute URL")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		token = strings.TrimSpace(cfg.RemoteViewerToken)
	}
	if token == "" {
		return "", "", fmt.Errorf("hub token is required")
	}
	return hubURL, token, nil
}

func (a *App) digitalAssetHubJSON(method, rawURL, token string, body []byte, dest any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", knowledgeShareBearerToken(token))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hubHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hub returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if dest == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, dest)
}

func isExcludedPersonalContributeSource(src knowledge.Source) bool {
	id := strings.ToLower(strings.TrimSpace(src.ID))
	uri := strings.ToLower(strings.TrimSpace(src.URI))
	if strings.HasPrefix(id, "dal_") || strings.HasPrefix(id, "sub_") {
		return true
	}
	if strings.HasPrefix(uri, "enterprise://") || strings.HasPrefix(uri, "submission://") {
		return true
	}
	for _, label := range src.Labels {
		l := strings.ToLower(strings.TrimSpace(label))
		if l == "save_scope:session" || l == "save_scope:local_only" || strings.HasPrefix(l, "enterprise_import_kind=") {
			return true
		}
	}
	return false
}

func orgProjectLeaf(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return ""
	}
	return path.Base(p)
}
