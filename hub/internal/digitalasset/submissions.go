package digitalasset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/google/uuid"
)

// ErrConflict is returned when a submission is not in a reviewable state.
var ErrConflict = fmt.Errorf("digital asset submission conflict")

// SubmitExperienceInput is a viewer contribution.
type SubmitExperienceInput struct {
	TenantID        string
	LibraryID       string
	SubmitterUserID string
	SubmitterEmail  string
	Kind            string
	Title           string
	Summary         string
	SourceShareID   string
	PackageJSON     []byte
}

// ReviewSubmissionInput is an admin approve/reject.
type ReviewSubmissionInput struct {
	TenantID     string
	SubmissionID string
	LibraryID    string // optional retarget on approve
	Actor        string
	ActorUserID  string
	Note         string
}

// ListContributableLibraries returns active libraries the viewer may submit to.
func (s *Service) ListContributableLibraries(ctx context.Context, tenantID, email, kind string) ([]LibraryView, error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	wantKind, err := normalizeLibraryKind(kind)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(kind) == "" {
		wantKind = ""
	}
	items, _, err := s.Repo.ListLibraries(ctx, store.DigitalAssetLibraryFilter{
		TenantID: tenantID,
		Status:   store.DigitalAssetStatusActive,
		Kind:     wantKind,
		Limit:    200,
	})
	if err != nil {
		return nil, err
	}
	out := make([]LibraryView, 0, len(items))
	for _, lib := range items {
		if lib == nil || !lib.AcceptsSubmissions {
			continue
		}
		ok, aerr := s.ACL.CanAccessLibrary(ctx, lib, email)
		if aerr != nil {
			return nil, aerr
		}
		if !ok {
			continue
		}
		out = append(out, LibraryToView(lib))
	}
	return out, nil
}

// SubmitExperience stores a staged contribution package.
func (s *Service) SubmitExperience(ctx context.Context, in SubmitExperienceInput) (*store.DigitalAssetSubmission, error) {
	if err := s.requireEnabled(ctx, in.TenantID); err != nil {
		return nil, err
	}
	if s.Repo == nil || s.Host == nil {
		return nil, fmt.Errorf("digital assets host is not configured")
	}
	kind := strings.TrimSpace(in.Kind)
	if kind != SubmissionKindBusiness && kind != SubmissionKindCoding {
		return nil, fmt.Errorf("%w: kind must be business_knowledge or coding_experience", ErrInvalid)
	}
	summary := strings.TrimSpace(in.Summary)
	if summary == "" {
		return nil, fmt.Errorf("%w: summary is required", ErrInvalid)
	}
	lib, err := s.GetLibrary(ctx, in.TenantID, in.LibraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil || lib.Status != store.DigitalAssetStatusActive {
		return nil, ErrNotFound
	}
	if !lib.AcceptsSubmissions {
		return nil, fmt.Errorf("%w: library does not accept submissions", ErrForbidden)
	}
	ok, err := s.ACL.CanAccessLibrary(ctx, lib, in.SubmitterEmail)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	if err := assertKindMatchesLibrary(kind, lib.LibraryKind); err != nil {
		return nil, err
	}
	pkg, err := parseAndValidateSubmissionPackage(in.PackageJSON, kind)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	id := "das_" + uuid.NewString()
	dir := filepath.Join(s.Host.Root(), in.TenantID, "submissions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	pkgPath := filepath.Join(dir, id+".json")
	if err := os.WriteFile(pkgPath, in.PackageJSON, 0o644); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(in.PackageJSON)
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = strings.TrimSpace(pkg.Manifest.Title)
	}
	if title == "" {
		title = "Untitled contribution"
	}
	row := &store.DigitalAssetSubmission{
		ID:              id,
		TenantID:        in.TenantID,
		LibraryID:       lib.ID,
		SubmitterUserID: strings.TrimSpace(in.SubmitterUserID),
		SubmitterEmail:  strings.TrimSpace(in.SubmitterEmail),
		Kind:            kind,
		Status:          store.DigitalAssetSubmissionSubmitted,
		Title:           title,
		Summary:         summary,
		PackageRef:      pkgPath,
		PackageSHA256:   hex.EncodeToString(sum[:]),
		PackageBytes:    int64(len(in.PackageJSON)),
		ItemCount:       len(pkg.Sources),
		SourceShareID:   strings.TrimSpace(in.SourceShareID),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.Repo.CreateSubmission(ctx, row); err != nil {
		_ = os.Remove(pkgPath)
		return nil, err
	}
	return row, nil
}

// ListSubmissions lists contributions. SubmitterUserID scopes to "mine".
func (s *Service) ListSubmissions(ctx context.Context, filter store.DigitalAssetSubmissionFilter) ([]*store.DigitalAssetSubmission, int, error) {
	if err := s.requireEnabled(ctx, filter.TenantID); err != nil {
		return nil, 0, err
	}
	return s.Repo.ListSubmissions(ctx, filter)
}

// GetSubmission returns one contribution.
func (s *Service) GetSubmission(ctx context.Context, tenantID, submissionID string) (*store.DigitalAssetSubmission, error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	row, err := s.Repo.GetSubmission(ctx, tenantID, submissionID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNotFound
	}
	return row, nil
}

// WithdrawSubmission lets the submitter cancel a pending contribution.
func (s *Service) WithdrawSubmission(ctx context.Context, tenantID, submissionID, userID string) (*store.DigitalAssetSubmission, error) {
	row, err := s.GetSubmission(ctx, tenantID, submissionID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(row.SubmitterUserID) != strings.TrimSpace(userID) {
		return nil, ErrForbidden
	}
	if row.Status != store.DigitalAssetSubmissionSubmitted {
		return nil, fmt.Errorf("%w: only submitted contributions can be withdrawn", ErrConflict)
	}
	row.Status = store.DigitalAssetSubmissionWithdrawn
	row.UpdatedAt = time.Now().UTC()
	if err := s.Repo.UpdateSubmission(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// RejectSubmission records an admin rejection.
func (s *Service) RejectSubmission(ctx context.Context, in ReviewSubmissionInput) (*store.DigitalAssetSubmission, error) {
	row, err := s.GetSubmission(ctx, in.TenantID, in.SubmissionID)
	if err != nil {
		return nil, err
	}
	if row.Status != store.DigitalAssetSubmissionSubmitted && row.Status != store.DigitalAssetSubmissionImportFailed {
		return nil, fmt.Errorf("%w: submission is not pending review", ErrConflict)
	}
	note := strings.TrimSpace(in.Note)
	if note == "" {
		return nil, fmt.Errorf("%w: review_note is required", ErrInvalid)
	}
	now := time.Now().UTC()
	row.Status = store.DigitalAssetSubmissionRejected
	row.ReviewerUserID = strings.TrimSpace(in.ActorUserID)
	if row.ReviewerUserID == "" {
		row.ReviewerUserID = strings.TrimSpace(in.Actor)
	}
	row.ReviewNote = note
	row.ReviewedAt = &now
	row.UpdatedAt = now
	if err := s.Repo.UpdateSubmission(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// ApproveSubmission imports the staged package into the target library.
func (s *Service) ApproveSubmission(ctx context.Context, in ReviewSubmissionInput) (*store.DigitalAssetSubmission, error) {
	row, err := s.GetSubmission(ctx, in.TenantID, in.SubmissionID)
	if err != nil {
		return nil, err
	}
	if row.Status != store.DigitalAssetSubmissionSubmitted && row.Status != store.DigitalAssetSubmissionImportFailed {
		return nil, fmt.Errorf("%w: submission is not pending review", ErrConflict)
	}
	libraryID := strings.TrimSpace(in.LibraryID)
	if libraryID == "" {
		libraryID = row.LibraryID
	}
	lib, err := s.GetLibrary(ctx, in.TenantID, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil || lib.Status != store.DigitalAssetStatusActive {
		return nil, ErrNotFound
	}
	if err := assertKindMatchesLibrary(row.Kind, lib.LibraryKind); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(row.PackageRef)
	if err != nil {
		return nil, fmt.Errorf("package missing: %w", err)
	}
	pkg, err := parseAndValidateSubmissionPackage(raw, row.Kind)
	if err != nil {
		return nil, err
	}

	s.importStartMu.Lock()
	s.reclaimStaleImportJobs(ctx, in.TenantID)
	if n, err := s.Repo.CountRunningJobs(ctx, in.TenantID); err == nil && n > 0 {
		s.importStartMu.Unlock()
		return nil, fmt.Errorf("tenant already has a running import job")
	}
	now := time.Now().UTC()
	progress, _ := json.Marshal(map[string]any{
		"submission_id": row.ID, "phase": "importing", "percent": 15,
		"message": "importing experience submission",
	})
	job := &store.DigitalAssetImportJob{
		ID: "daij_" + uuid.NewString(), TenantID: in.TenantID, LibraryID: libraryID,
		Kind: "experience_submission", Status: "running", ProgressJSON: string(progress),
		CreatedBy: in.Actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Repo.CreateJob(ctx, job); err != nil {
		s.importStartMu.Unlock()
		return nil, err
	}
	s.importStartMu.Unlock()

	prefix := "sub_" + row.ID + "_"
	domain := "business"
	if row.Kind == SubmissionKindCoding {
		domain = "technical"
	}
	err = s.Host.WithLibraryWrite(ctx, in.TenantID, libraryID, func(st *knowledge.SQLiteStore) error {
		ps := make([]knowledge.PackageSource, 0, len(pkg.Sources))
		for _, src := range pkg.Sources {
			id := strings.TrimSpace(src.ID)
			if id != "" && !strings.HasPrefix(id, prefix) {
				id = prefix + id
			} else if id == "" {
				id = prefix + uuid.NewString()
			}
			labels := append([]string{}, src.Labels...)
			labels = append(labels,
				"enterprise_import_kind=experience_submission",
				"submission_id="+row.ID,
				"submitted_by="+row.SubmitterEmail,
				"experience_domain="+domain,
			)
			if cls := experienceClassFromLabels(src.Labels, src.Kind); cls != "" {
				labels = append(labels, "experience_class="+cls)
			}
			topicHint := src.TopicHint
			if row.Kind == SubmissionKindCoding {
				topicHint = markCodingExperienceVerified(topicHint)
			}
			ps = append(ps, knowledge.PackageSource{
				ID: id, Kind: src.Kind, URI: src.URI, CanonicalURI: src.CanonicalURI,
				Title: src.Title, TopicHint: firstNonEmpty(topicHint, pkg.Manifest.Title),
				Labels: labels, Content: src.Content, ContentTruncated: src.ContentTruncated,
			})
		}
		res := knowledge.ImportPackageSources(ctx, st, ps, knowledge.PackageImportOptions{
			TenantID:  in.TenantID,
			TopicHint: firstNonEmpty(row.Title, pkg.Manifest.Title),
			RootPath:  "submission://" + row.ID,
		})
		if res.Failed > 0 && res.Imported == 0 {
			return fmt.Errorf("import failed: %v", res.Warnings)
		}
		lib.SourceCount += int64(res.Imported)
		lib.UpdatedAt = time.Now().UTC()
		lib.UpdatedBy = in.Actor
		return s.advanceContentAfterImportLocked(ctx, st, lib, "upsert_sources", in.Actor)
	})
	if err != nil {
		s.failJob(job, err)
		row.Status = store.DigitalAssetSubmissionImportFailed
		row.ImportJobID = job.ID
		row.ReviewNote = strings.TrimSpace(in.Note)
		row.UpdatedAt = time.Now().UTC()
		_ = s.Repo.UpdateSubmission(ctx, row)
		return row, err
	}
	job.Status = "succeeded"
	job.ProgressJSON = string(mustJSON(map[string]any{
		"submission_id": row.ID, "phase": "done", "percent": 100, "message": "import completed",
	}))
	job.UpdatedAt = time.Now().UTC()
	_ = s.Repo.UpdateJob(ctx, job)

	reviewed := time.Now().UTC()
	row.Status = store.DigitalAssetSubmissionApproved
	row.LibraryID = libraryID
	row.ImportJobID = job.ID
	row.ReviewerUserID = strings.TrimSpace(in.ActorUserID)
	if row.ReviewerUserID == "" {
		row.ReviewerUserID = strings.TrimSpace(in.Actor)
	}
	row.ReviewNote = strings.TrimSpace(in.Note)
	row.ReviewedAt = &reviewed
	row.UpdatedAt = reviewed
	if err := s.Repo.UpdateSubmission(ctx, row); err != nil {
		return row, err
	}
	return row, nil
}

// SubmissionPreviewTitles reads package titles for admin review.
func (s *Service) SubmissionPreviewTitles(row *store.DigitalAssetSubmission, limit int) []string {
	if row == nil || strings.TrimSpace(row.PackageRef) == "" {
		return nil
	}
	if limit <= 0 || limit > 40 {
		limit = 12
	}
	raw, err := os.ReadFile(row.PackageRef)
	if err != nil {
		return nil
	}
	var pkg sharePackageFile
	if json.Unmarshal(raw, &pkg) != nil {
		return nil
	}
	out := make([]string, 0, limit)
	for _, src := range pkg.Sources {
		title := strings.TrimSpace(src.Title)
		if title == "" {
			title = strings.TrimSpace(src.ID)
		}
		if title == "" {
			continue
		}
		out = append(out, title)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// SubmissionToView converts a store row to API JSON.
func SubmissionToView(row *store.DigitalAssetSubmission, titles []string) SubmissionView {
	if row == nil {
		return SubmissionView{}
	}
	return SubmissionView{
		ID: row.ID, TenantID: row.TenantID, LibraryID: row.LibraryID,
		SubmitterUserID: row.SubmitterUserID, SubmitterEmail: row.SubmitterEmail,
		Kind: row.Kind, Status: row.Status, Title: row.Title, Summary: row.Summary,
		ItemCount: row.ItemCount, PackageBytes: row.PackageBytes,
		ReviewNote: row.ReviewNote, ReviewedAt: FormatTime(derefTime(row.ReviewedAt)),
		ImportJobID: row.ImportJobID,
		CreatedAt:   FormatTime(row.CreatedAt), UpdatedAt: FormatTime(row.UpdatedAt),
		PreviewTitles: titles,
	}
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func assertKindMatchesLibrary(submissionKind, libraryKind string) error {
	libraryKind, err := normalizeLibraryKind(libraryKind)
	if err != nil {
		return err
	}
	switch submissionKind {
	case SubmissionKindBusiness:
		if libraryKind != LibraryKindBusiness {
			return fmt.Errorf("%w: business_knowledge can only target a business library", ErrInvalid)
		}
	case SubmissionKindCoding:
		if libraryKind != LibraryKindTechnical {
			return fmt.Errorf("%w: coding_experience can only target a technical library", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unsupported submission kind", ErrInvalid)
	}
	return nil
}

func parseAndValidateSubmissionPackage(raw []byte, kind string) (*sharePackageFile, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: package is required", ErrInvalid)
	}
	if len(raw) > MaxSubmissionPackageBytes {
		return nil, fmt.Errorf("%w: package exceeds %d bytes", ErrInvalid, MaxSubmissionPackageBytes)
	}
	var pkg sharePackageFile
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil, fmt.Errorf("%w: invalid package json: %v", ErrInvalid, err)
	}
	if strings.TrimSpace(pkg.Manifest.Format) != "maclaw.knowledge.package" {
		return nil, fmt.Errorf("%w: unsupported package format", ErrInvalid)
	}
	if len(pkg.Sources) == 0 {
		return nil, fmt.Errorf("%w: package has no sources", ErrInvalid)
	}
	if len(pkg.Sources) > MaxSubmissionItems {
		return nil, fmt.Errorf("%w: package exceeds %d items", ErrInvalid, MaxSubmissionItems)
	}
	for i, src := range pkg.Sources {
		title := strings.TrimSpace(src.Title)
		content := strings.TrimSpace(src.Content)
		if title == "" || content == "" {
			return nil, fmt.Errorf("%w: source %d needs title and content", ErrInvalid, i)
		}
		if isExcludedBusinessSource(src) {
			return nil, fmt.Errorf("%w: source %q is session, local-only, or enterprise content", ErrInvalid, title)
		}
		if kind == SubmissionKindCoding && !isCodingExperienceSource(src) {
			return nil, fmt.Errorf("%w: source %q is not a coding experience", ErrInvalid, title)
		}
		if kind == SubmissionKindCoding {
			meta := parseCodingTopicHint(src.TopicHint)
			if strings.TrimSpace(meta.TriggerCondition) == "" {
				return nil, fmt.Errorf("%w: coding experience %q needs trigger_condition", ErrInvalid, title)
			}
		}
	}
	return &pkg, nil
}

func isExcludedBusinessSource(src struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	URI              string   `json:"uri"`
	CanonicalURI     string   `json:"canonical_uri"`
	Title            string   `json:"title"`
	TopicHint        string   `json:"topic_hint"`
	Labels           []string `json:"labels"`
	Content          string   `json:"content"`
	ContentTruncated bool     `json:"content_truncated"`
}) bool {
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
		if l == "save_scope:session" || l == "save_scope:local_only" ||
			strings.HasPrefix(l, "enterprise_import_kind=") || l == "save_scope=session" || l == "save_scope=local_only" {
			return true
		}
	}
	return false
}

func isCodingExperienceSource(src struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	URI              string   `json:"uri"`
	CanonicalURI     string   `json:"canonical_uri"`
	Title            string   `json:"title"`
	TopicHint        string   `json:"topic_hint"`
	Labels           []string `json:"labels"`
	Content          string   `json:"content"`
	ContentTruncated bool     `json:"content_truncated"`
}) bool {
	if strings.EqualFold(strings.TrimSpace(src.Kind), "coding_experience") {
		return true
	}
	for _, label := range src.Labels {
		if strings.EqualFold(strings.TrimSpace(label), "coding_experience") {
			return true
		}
	}
	return parseCodingTopicHint(src.TopicHint).TriggerCondition != ""
}

func experienceClassFromLabels(labels []string, kind string) string {
	for _, label := range labels {
		l := strings.TrimSpace(label)
		if strings.HasPrefix(l, "experience_class=") {
			return strings.TrimSpace(strings.TrimPrefix(l, "experience_class="))
		}
		if strings.HasPrefix(l, "category:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "category:"))
		}
	}
	if strings.EqualFold(kind, "coding_experience") {
		return "pattern"
	}
	return ""
}

func parseCodingTopicHint(raw string) knowledge.CodingExperienceMetadata {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return knowledge.CodingExperienceMetadata{}
	}
	var meta knowledge.CodingExperienceMetadata
	if json.Unmarshal([]byte(raw), &meta) != nil {
		return knowledge.CodingExperienceMetadata{}
	}
	return meta
}

func markCodingExperienceVerified(topicHint string) string {
	meta := parseCodingTopicHint(topicHint)
	if strings.TrimSpace(topicHint) == "" || !strings.HasPrefix(strings.TrimSpace(topicHint), "{") {
		return topicHint
	}
	meta.Status = knowledge.CodingStatusVerified
	meta.Confidence = knowledge.CodingConfidenceInitial
	meta.RecallCount = 0
	meta.SuccessCount = 0
	meta.FailureCount = 0
	b, err := json.Marshal(meta)
	if err != nil {
		return topicHint
	}
	return string(b)
}
