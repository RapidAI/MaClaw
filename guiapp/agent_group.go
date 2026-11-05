package guiapp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentgroup"
)

// AgentGroupBot is the Wails view of one teammate candidate.
type AgentGroupBot struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// AgentGroupRun is the Wails view of one auto-teaming run.
type AgentGroupRun struct {
	ID          string                 `json:"id"`
	Goal        string                 `json:"goal"`
	RouterBotID string                 `json:"router_bot_id,omitempty"`
	Status      string                 `json:"status"`
	SubTasks    []agentgroup.SubTask   `json:"sub_tasks"`
	Summary     string                 `json:"summary,omitempty"`
	Progress    []string               `json:"progress,omitempty"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
}

var (
	agentGroupMu   sync.Mutex
	agentGroupOrch *agentgroup.Orchestrator
)

func (a *App) agentGroupOrchestrator() *agentgroup.Orchestrator {
	agentGroupMu.Lock()
	defer agentGroupMu.Unlock()
	if agentGroupOrch != nil {
		return agentGroupOrch
	}
	// Default worker records a collapsed stub result so GUI demos work offline.
	// Real desktop dispatch can replace this later via SetAgentGroupWorker.
	agentGroupOrch = agentgroup.NewOrchestrator(func(ctx context.Context, bot agentgroup.Bot, task agentgroup.SubTask) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
		return fmt.Sprintf("已处理：%s", strings.TrimSpace(task.Description)), nil
	})
	return agentGroupOrch
}

func projectAgentGroupRun(r *agentgroup.Run) AgentGroupRun {
	if r == nil {
		return AgentGroupRun{}
	}
	return AgentGroupRun{
		ID:          r.ID,
		Goal:        r.Goal,
		RouterBotID: r.RouterBotID,
		Status:      r.Status,
		SubTasks:    append([]agentgroup.SubTask(nil), r.SubTasks...),
		Summary:     r.Summary,
		Progress:    append([]string(nil), r.Progress...),
		CreatedAt:   r.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   r.UpdatedAt.Format(time.RFC3339),
	}
}

func toAgentGroupBots(in []AgentGroupBot) []agentgroup.Bot {
	out := make([]agentgroup.Bot, 0, len(in))
	for _, b := range in {
		role := agentgroup.Role(strings.ToLower(strings.TrimSpace(b.Role)))
		if role == "" {
			role = agentgroup.RoleWorker
		}
		out = append(out, agentgroup.Bot{
			ID:           strings.TrimSpace(b.ID),
			Name:         strings.TrimSpace(b.Name),
			Role:         role,
			Capabilities: append([]string(nil), b.Capabilities...),
		})
	}
	return out
}

// StartAgentGroupRun plans goal into sub-tasks, assigns workers by capability,
// runs them in parallel, and returns one humanized summary plus collapsed progress.
func (a *App) StartAgentGroupRun(goal, routerBotID string, bots []AgentGroupBot) (*AgentGroupRun, error) {
	if strings.TrimSpace(goal) == "" {
		return nil, fmt.Errorf("goal is required")
	}
	if len(bots) == 0 {
		// Fall back to Hub-registered local bots when the UI omits the roster.
		if local, err := a.ListDesktopLocalBots(); err == nil && len(local) > 0 {
			for _, lb := range local {
				bots = append(bots, AgentGroupBot{
					ID: lb.ID, Name: lb.Name, Role: string(agentgroup.RoleWorker), Capabilities: lb.Capabilities,
				})
			}
		}
	}
	if len(bots) == 0 {
		return nil, fmt.Errorf("at least one bot is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run, err := a.agentGroupOrchestrator().Start(ctx, goal, routerBotID, toAgentGroupBots(bots))
	if err != nil {
		return nil, err
	}
	out := projectAgentGroupRun(run)
	return &out, nil
}

// GetAgentGroupRun returns one auto-teaming run.
func (a *App) GetAgentGroupRun(id string) (*AgentGroupRun, error) {
	run, ok := a.agentGroupOrchestrator().Get(id)
	if !ok {
		return nil, fmt.Errorf("agent group run not found")
	}
	out := projectAgentGroupRun(run)
	return &out, nil
}

// ListAgentGroupRuns lists recent auto-teaming runs (newest first).
func (a *App) ListAgentGroupRuns() ([]AgentGroupRun, error) {
	runs := a.agentGroupOrchestrator().List()
	out := make([]AgentGroupRun, 0, len(runs))
	for i := range runs {
		out = append(out, projectAgentGroupRun(&runs[i]))
	}
	return out, nil
}
