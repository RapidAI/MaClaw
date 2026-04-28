package skillmarket

import (
	"context"
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
	return s.repo.SearchActive(ctx, query)
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
	skill, err := buildSkill(input, false)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, skill); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, skill.ID)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, strings.TrimSpace(id))
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
	return &store.Skill{
		ID:          id,
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		Category:    marketschema.FirstNonEmpty(input.Category, "general"),
		Version:     marketschema.FirstNonEmpty(input.Version, "1.0.0"),
		Tags:        string(tagsJSON),
		RiskLevel:   marketschema.FirstNonEmpty(input.RiskLevel, "low"),
		Status:      marketschema.NormalizeStatus(input.Status),
		Price:       input.Price,
		Author:      strings.TrimSpace(input.Author),
		CreatedAt:   createdAt,
		UpdatedAt:   now,
	}, nil
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
