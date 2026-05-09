package skillmarket

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	marketschema "github.com/RapidAI/CodeClaw/corelib/skillmarket"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

type Service struct {
	repo store.SkillRepository
}

type SkillInput = marketschema.SkillInput

const maxSkillPackageBytes = 1 << 20

func NewService(repo store.SkillRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) EnsureDefaults(ctx context.Context) error {
	count, err := s.repo.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, input := range DefaultSkills() {
		if _, err := s.Create(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]*store.Skill, error) {
	return s.repo.List(ctx)
}

func (s *Service) SearchActive(ctx context.Context, query string) ([]*store.Skill, error) {
	items, err := s.repo.SearchActive(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]*store.Skill, 0, len(items))
	for _, item := range items {
		if isCenterInstallableSkill(item) {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *Service) GetActive(ctx context.Context, id string) (*store.Skill, error) {
	skill, err := s.repo.GetByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if skill.Status != "active" {
		return nil, fmt.Errorf("skill disabled")
	}
	return skill, nil
}

func (s *Service) GetInstallable(ctx context.Context, id string) (*store.Skill, error) {
	skill, err := s.GetActive(ctx, id)
	if err != nil {
		return nil, err
	}
	if !isCenterInstallableSkill(skill) {
		return nil, fmt.Errorf("skill package is not available")
	}
	return skill, nil
}

func isCenterInstallableSkill(skill *store.Skill) bool {
	if skill == nil {
		return false
	}
	return skill.Status == "active" &&
		strings.TrimSpace(skill.PackageContent) != "" &&
		strings.TrimSpace(skill.PackageSHA256) != ""
}

func (s *Service) DownloadPackage(ctx context.Context, id string) (*marketschema.PackageDownload, error) {
	skill, err := s.GetInstallable(ctx, id)
	if err != nil {
		return nil, err
	}
	return &marketschema.PackageDownload{
		SkillID:        skill.ID,
		Version:        skill.Version,
		Format:         marketschema.FirstNonEmpty(skill.PackageFormat, "skill.md"),
		SHA256:         skill.PackageSHA256,
		Size:           skill.PackageSize,
		ContentBase64:  skill.PackageContent,
		ContentType:    "application/json",
		SourceContract: "corelib/skillmarket.PackageDownload.v1",
	}, nil
}

func (s *Service) Create(ctx context.Context, input SkillInput) (*store.Skill, error) {
	skill, err := buildSkill(input, true)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, skill); err != nil {
		return nil, err
	}
	return skill, nil
}

func (s *Service) Update(ctx context.Context, id string, input SkillInput) (*store.Skill, error) {
	input.ID = id
	existing, err := s.repo.GetByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	skill, err := buildSkill(input, false)
	if err != nil {
		return nil, err
	}
	mergeExistingSkillFields(skill, existing)
	if err := s.repo.Update(ctx, skill); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, skill.ID)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, strings.TrimSpace(id))
}

func (s *Service) PublishFromCenter(ctx context.Context, centerID string, input SkillInput) (*store.Skill, error) {
	input.SourceCenterID = strings.TrimSpace(centerID)
	if strings.TrimSpace(input.ID) == "" {
		return nil, fmt.Errorf("id is required")
	}
	if strings.TrimSpace(input.AuthorEmail) == "" {
		return nil, fmt.Errorf("author_email is required")
	}
	if strings.TrimSpace(input.Author) == "" {
		input.Author = input.AuthorEmail
	}
	if strings.TrimSpace(input.Status) == "" {
		input.Status = "active"
	}
	skill, err := buildSkill(input, true)
	if err != nil {
		return nil, err
	}
	if existing, err := s.repo.GetByID(ctx, skill.ID); err == nil && existing != nil {
		if existing.SourceCenterID != "" && existing.SourceCenterID != input.SourceCenterID {
			return nil, fmt.Errorf("skill id already belongs to another center")
		}
		skill.CreatedAt = existing.CreatedAt
		if err := s.repo.Update(ctx, skill); err != nil {
			return nil, err
		}
		return s.repo.GetByID(ctx, skill.ID)
	}
	if err := s.repo.Create(ctx, skill); err != nil {
		return nil, err
	}
	return skill, nil
}

func buildSkill(input SkillInput, create bool) (*store.Skill, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	tagsJSON, err := json.Marshal(input.Tags)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	createdAt := now
	if !create {
		createdAt = time.Time{}
	}
	packageFormat := strings.TrimSpace(input.PackageFormat)
	packageContent := strings.TrimSpace(input.PackageContentBase64)
	var packageSHA string
	var packageSize int64
	if packageContent != "" {
		decoded, err := base64.StdEncoding.DecodeString(packageContent)
		if err != nil {
			return nil, fmt.Errorf("package_content_base64 is invalid: %w", err)
		}
		if len(decoded) == 0 {
			return nil, fmt.Errorf("package_content_base64 is empty")
		}
		if len(decoded) > maxSkillPackageBytes {
			return nil, fmt.Errorf("package_content_base64 exceeds %d bytes", maxSkillPackageBytes)
		}
		if packageFormat == "" {
			packageFormat = "skill.md"
		}
		sum := sha256.Sum256(decoded)
		packageSHA = fmt.Sprintf("%x", sum[:])
		packageSize = int64(len(decoded))
	}

	return &store.Skill{
		ID:             id,
		Name:           name,
		Description:    strings.TrimSpace(input.Description),
		Category:       marketschema.FirstNonEmpty(input.Category, "general"),
		Version:        marketschema.FirstNonEmpty(input.Version, "1.0.0"),
		Tags:           string(tagsJSON),
		RiskLevel:      marketschema.FirstNonEmpty(input.RiskLevel, "low"),
		Status:         marketschema.NormalizeStatus(input.Status),
		Price:          input.Price,
		Author:         strings.TrimSpace(input.Author),
		AuthorEmail:    strings.TrimSpace(input.AuthorEmail),
		SourceCenterID: strings.TrimSpace(input.SourceCenterID),
		PackageFormat:  packageFormat,
		PackageContent: packageContent,
		PackageSHA256:  packageSHA,
		PackageSize:    packageSize,
		CreatedAt:      createdAt,
		UpdatedAt:      now,
	}, nil
}

func mergeExistingSkillFields(next *store.Skill, existing *store.Skill) {
	if next == nil || existing == nil {
		return
	}
	next.AvgRating = existing.AvgRating
	next.DownloadCount = existing.DownloadCount
	if strings.TrimSpace(next.SourceCenterID) == "" {
		next.SourceCenterID = existing.SourceCenterID
	}
	if strings.TrimSpace(next.PackageContent) == "" {
		next.PackageFormat = existing.PackageFormat
		next.PackageContent = existing.PackageContent
		next.PackageSHA256 = existing.PackageSHA256
		next.PackageSize = existing.PackageSize
	}
}

func DefaultSkills() []SkillInput {
	return []SkillInput{
		{
			ID:          "goal-recovery-loop",
			Name:        "Goal recovery loop",
			Description: "Detect stalled goals, push assigned iWorkers, and create recovery tasks when autonomous execution stops.",
			Category:    "operations",
			Version:     "1.0.0",
			Tags:        []string{"goalwatch", "autonomy", "recovery"},
			RiskLevel:   "medium",
			Status:      "active",
			Author:      "iWorkerCloud",
		},
		{
			ID:          "memory-deposition-review",
			Name:        "Memory deposition review",
			Description: "Turn recurring human decisions and exception handling into company, department, and personal memory updates.",
			Category:    "knowledge",
			Version:     "1.0.0",
			Tags:        []string{"memory", "knowledge", "continuity"},
			RiskLevel:   "low",
			Status:      "active",
			Author:      "iWorkerCloud",
		},
		{
			ID:          "a2a-decision-brief",
			Name:        "A2A decision brief",
			Description: "Summarize agent-to-agent deliberation into decision options, risks, owners, and follow-up tasks.",
			Category:    "collaboration",
			Version:     "1.0.0",
			Tags:        []string{"a2a", "decision", "brief"},
			RiskLevel:   "medium",
			Status:      "active",
			Author:      "iWorkerCloud",
		},
	}
}
