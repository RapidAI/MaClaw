package workermemory

import (
	"context"
	"fmt"
	"strings"
	"time"

	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

// BootstrapSeedInput carries the minimal organization context needed to give a
// newly purchased iWorkerCenter a usable company, department, and personal memory base.
type BootstrapSeedInput struct {
	CompanyName        string
	BusinessSummary    string
	Priority           string
	VirtualDepartments []string
	InitialWorkers     []BootstrapWorkerSeed
	RecurringTasks     []string
}

type BootstrapWorkerSeed struct {
	ID   string
	Name string
	Role string
}

type BootstrapSeededMemory struct {
	ID      string
	Scope   string
	OwnerID string
	Title   string
}

// SeedBootstrapMemories writes durable Center-owned bootstrap memories through corelib.
// iWorker desktop clients may cache these later, but the canonical copy stays here.
func (h *Handler) SeedBootstrapMemories(ctx context.Context, tenantID string, input BootstrapSeedInput) ([]BootstrapSeededMemory, error) {
	if h == nil || h.store == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	now := time.Now()
	seeded := []BootstrapSeededMemory{}
	write := func(scope, ownerID, title, content string, tags []string) error {
		if strings.TrimSpace(content) == "" {
			return nil
		}
		entry := corememory.Entry{
			Title:      title,
			Content:    content,
			Category:   corememory.CategoryProjectKnowledge,
			Tags:       append([]string{"enterprise_bootstrap", "scope:" + scope}, tags...),
			CreatedAt:  now,
			UpdatedAt:  now,
			SourceType: "iworkercenter.bootstrap",
			OwnerID:    ownerID,
		}
		if err := h.store.SaveForUser(entry, ownerID); err != nil {
			return err
		}
		matches := h.store.Search(entry.Category, content, 0)
		id := entry.ID
		for _, match := range matches {
			if match.OwnerID == ownerID {
				id = match.ID
				break
			}
		}
		seeded = append(seeded, BootstrapSeededMemory{ID: id, Scope: scope, OwnerID: ownerID, Title: title})
		return nil
	}

	companyContent := strings.Join(nonEmptyLines(
		"Company: "+strings.TrimSpace(input.CompanyName),
		"Business summary: "+strings.TrimSpace(input.BusinessSummary),
		"First operating priority: "+strings.TrimSpace(input.Priority),
		"Operating principle: iWorkerCenter is the organization runtime; humans provide judgment and tools, but durable execution memory stays in Center.",
	), "\n")
	if err := write(ScopeCompany, companyOwnerID(tenantID), "Enterprise bootstrap company memory", companyContent, []string{"company_profile"}); err != nil {
		return seeded, err
	}

	for _, department := range input.VirtualDepartments {
		department = strings.TrimSpace(department)
		if department == "" {
			continue
		}
		content := fmt.Sprintf("Virtual department/domain: %s. This is an AI-native capability and memory boundary, not a human middle-management layer. It should execute goals, reuse department memory, and escalate only boundary decisions.", department)
		if err := write(ScopeDepartment, departmentOwnerID(tenantID, department), "Bootstrap department memory: "+department, content, []string{"department_profile", "department:" + department}); err != nil {
			return seeded, err
		}
	}

	for _, worker := range input.InitialWorkers {
		workerID := strings.TrimSpace(worker.ID)
		workerName := strings.TrimSpace(worker.Name)
		if workerID == "" || workerName == "" {
			continue
		}
		content := fmt.Sprintf("iWorker identity: %s. Role/domain: %s. This iWorker's durable memory belongs to iWorkerCenter; local desktop state is only a cache/body. Initial recurring task context: %s.", workerName, strings.TrimSpace(worker.Role), strings.Join(input.RecurringTasks, ", "))
		if err := write(ScopePersonal, personalOwnerID(tenantID, workerID), "Bootstrap personal memory: "+workerName, content, []string{"personal_profile", "worker:" + workerID}); err != nil {
			return seeded, err
		}
	}

	select {
	case <-ctx.Done():
		return seeded, ctx.Err()
	default:
	}
	return seeded, h.store.Flush()
}

func nonEmptyLines(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
