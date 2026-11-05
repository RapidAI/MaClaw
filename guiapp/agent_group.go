package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentgroup"
)

// AgentGroupBot is one selectable teammate for an auto-teaming run.
type AgentGroupBot struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// AgentGroupRun is the Wails projection of one auto-teaming execution.
type AgentGroupRun struct {
	ID          string              `json:"id"`
	Goal        string              `json:"goal"`
	RouterBotID string              `json:"router_bot_id,omitempty"`
	PageID      string              `json:"page_id,omitempty"`
	Status      string              `json:"status"`
	SubTasks    []agentgroup.SubTask `json:"sub_tasks"`
	Summary     string              `json:"summary,omitempty"`
	Progress    []string            `json:"progress,omitempty"`
	CreatedAt   string              `json:"created_at"`
	UpdatedAt   string              `json:"updated_at"`
}

var (
	agentGroupMu   sync.Mutex
	agentGroupOrch *agentgroup.Orchestrator
)

// agentGroupDispatchFunc is overridable in tests (Hub-less).
var agentGroupDispatchFunc = func(a *App, ctx context.Context, bot agentgroup.Bot, task agentgroup.SubTask) (string, error) {
	return a.dispatchAgentGroupTask(ctx, bot, task)
}

func (a *App) agentGroupDataDir() string {
	if a == nil {
		return ""
	}
	if d := strings.TrimSpace(a.GetDataDir()); d != "" {
		return d
	}
	if strings.TrimSpace(a.testHomeDir) != "" {
		return a.testHomeDir
	}
	return ""
}

func (a *App) agentGroupOrchestrator() *agentgroup.Orchestrator {
	agentGroupMu.Lock()
	defer agentGroupMu.Unlock()
	if agentGroupOrch != nil {
		return agentGroupOrch
	}
	orch := agentgroup.NewOrchestrator(func(ctx context.Context, bot agentgroup.Bot, task agentgroup.SubTask) (string, error) {
		return agentGroupDispatchFunc(a, ctx, bot, task)
	})
	if dir := a.agentGroupDataDir(); dir != "" {
		orch.SetPersist(agentgroup.FilePersist(dir))
		if runs, err := agentgroup.LoadRunsFile(dir); err == nil && len(runs) > 0 {
			orch.LoadRuns(runs)
		}
	}
	// Optional LLM planner with heuristic fallback.
	orch.SetPlanner(agentgroup.PlannerWithFallback(a.agentGroupLLMPlanner(), agentgroup.DefaultPlan))
	agentGroupOrch = orch
	return agentGroupOrch
}

func (a *App) agentGroupLLMPlanner() agentgroup.Planner {
	return func(goal string, bots []agentgroup.Bot) []agentgroup.SubTask {
		tasks, err := a.planAgentGroupWithLLM(goal, bots)
		if err != nil || len(tasks) == 0 {
			return nil
		}
		return tasks
	}
}

func (a *App) planAgentGroupWithLLM(goal string, bots []agentgroup.Bot) ([]agentgroup.SubTask, error) {
	if a == nil {
		return nil, fmt.Errorf("app nil")
	}
	if !a.isMaclawLLMConfigured() {
		return nil, fmt.Errorf("LLM not configured")
	}
	caller := &guiLLMCaller{cfg: a.GetMaclawLLMConfig()}
	roster, _ := json.Marshal(bots)
	prompt := fmt.Sprintf(`Split the user goal into 1-6 parallel sub-tasks for desktop worker bots.
Return ONLY a JSON array of objects: [{"description":"...","need_caps":["search"|"coding"|"browser"|"writing"|"vision"]}].
Goal: %s
Bots: %s`, goal, string(roster))
	raw, err := caller.CallLLM(prompt, 0.2, 20*time.Second)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	text := strings.TrimSpace(string(raw))
	if i := strings.Index(text, "["); i >= 0 {
		if j := strings.LastIndex(text, "]"); j > i {
			text = text[i : j+1]
		}
	}
	var parsed []struct {
		Description string   `json:"description"`
		NeedCaps    []string `json:"need_caps"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, err
	}
	out := make([]agentgroup.SubTask, 0, len(parsed))
	for i, p := range parsed {
		desc := strings.TrimSpace(p.Description)
		if desc == "" {
			continue
		}
		out = append(out, agentgroup.SubTask{
			ID:          fmt.Sprintf("st-%d", i+1),
			Description: desc,
			NeedCaps:    append([]string(nil), p.NeedCaps...),
			Status:      "pending",
		})
	}
	return out, nil
}

// dispatchAgentGroupTask posts a Hub delegate order and waits for the assignment report.
func (a *App) dispatchAgentGroupTask(ctx context.Context, bot agentgroup.Bot, task agentgroup.SubTask) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app nil")
	}
	from := "main"
	order := strings.TrimSpace(task.Description)
	if order == "" {
		return "", fmt.Errorf("empty sub-task")
	}
	note, err := a.postOneDesktopBotCross(from, "delegate", bot.ID, order, order, "")
	if err != nil {
		return "", fmt.Errorf("hub delegate to %s: %w", bot.ID, err)
	}
	deadline := time.Now().Add(90 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	noteID := strings.TrimSpace(note.ID)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timeout waiting for bot %s report (note %s)", bot.ID, noteID)
		}
		items, listErr := a.ListBotAssignments()
		if listErr == nil {
			for _, asg := range items {
				if strings.TrimSpace(asg.TargetBot) != bot.ID {
					continue
				}
				// Prefer matching ReplyTo / note id when Hub links them.
				linked := noteID == "" || strings.TrimSpace(asg.ID) == noteID || strings.Contains(asg.Task, order) || strings.TrimSpace(asg.ParentID) == noteID
				if !linked && strings.TrimSpace(asg.Task) != order {
					continue
				}
				state := strings.ToLower(strings.TrimSpace(asg.State))
				switch state {
				case "completed", "done":
					if strings.TrimSpace(asg.LastReport) != "" {
						return asg.LastReport, nil
					}
					return "completed", nil
				case "failed":
					if strings.TrimSpace(asg.LastReport) != "" {
						return "", fmt.Errorf("%s", asg.LastReport)
					}
					return "", fmt.Errorf("bot %s failed assignment %s", bot.ID, asg.ID)
				case "blocked", "waiting_human", "needs_login":
					msg := asg.LastReport
					if msg == "" {
						msg = asg.OpenQuestion
					}
					if msg == "" {
						msg = state
					}
					return msg, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

func projectAgentGroupRun(r *agentgroup.Run) AgentGroupRun {
	if r == nil {
		return AgentGroupRun{}
	}
	return AgentGroupRun{
		ID:          r.ID,
		Goal:        r.Goal,
		RouterBotID: r.RouterBotID,
		PageID:      r.PageID,
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
// dispatches via Hub/router paths, persists the run, and links a shared page.
func (a *App) StartAgentGroupRun(goal, routerBotID string, bots []AgentGroupBot) (*AgentGroupRun, error) {
	if strings.TrimSpace(goal) == "" {
		return nil, fmt.Errorf("goal is required")
	}
	if len(bots) == 0 {
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	run, err := a.agentGroupOrchestrator().Start(ctx, goal, routerBotID, toAgentGroupBots(bots))
	if err != nil {
		return nil, err
	}
	// Link a shared output page for this run (group_id = run id).
	if page, pageErr := a.CreateSharedPage(fmt.Sprintf("Agent Group: %s", collapseAgentGroupTitle(goal)), run.ID, "router"); pageErr == nil && page != nil {
		a.agentGroupOrchestrator().SetPageID(run.ID, page.ID)
		_, _ = a.AppendSharedPageSection(page.ID, "Goal", goal, "router")
		if run.Summary != "" {
			_, _ = a.AppendSharedPageSection(page.ID, "Summary", run.Summary, "router")
		}
		if updated, ok := a.agentGroupOrchestrator().Get(run.ID); ok {
			run = updated
		}
	}
	out := projectAgentGroupRun(run)
	return &out, nil
}

func collapseAgentGroupTitle(goal string) string {
	goal = strings.Join(strings.Fields(strings.TrimSpace(goal)), " ")
	r := []rune(goal)
	if len(r) > 48 {
		return string(r[:45]) + "..."
	}
	return goal
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
