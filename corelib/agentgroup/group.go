// Package agentgroup implements Agent Groups auto-teaming: a router plans a
// user task into sub-tasks, picks worker bots by capability, runs them in
// parallel, then aggregates a single humanized reply. Workers and watchers
// never talk to the user; only the router-facing summary does.
package agentgroup

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Role is the bot's place in the desktop architecture.
type Role string

const (
	RoleRouter  Role = "router"
	RoleWorker  Role = "worker"
	RoleWatcher Role = "watcher"
)

// Bot is one selectable teammate.
type Bot struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         Role     `json:"role"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// SubTask is one planned unit of work.
type SubTask struct {
	ID           string   `json:"id"`
	Description  string   `json:"description"`
	NeedCaps     []string `json:"need_caps,omitempty"`
	AssignedBot  string   `json:"assigned_bot,omitempty"`
	AssignedName string   `json:"assigned_name,omitempty"`
	Status       string   `json:"status"`
	Result       string   `json:"result,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// Run is one auto-teaming execution.
type Run struct {
	ID          string    `json:"id"`
	Goal        string    `json:"goal"`
	RouterBotID string    `json:"router_bot_id,omitempty"`
	PageID      string    `json:"page_id,omitempty"`
	Status      string    `json:"status"`
	SubTasks    []SubTask `json:"sub_tasks"`
	Summary     string    `json:"summary,omitempty"`
	Progress    []string  `json:"progress,omitempty"` // collapsed one-line per bot
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WorkerFunc executes one assigned sub-task and returns a short result text.
type WorkerFunc func(ctx context.Context, bot Bot, task SubTask) (string, error)

// Planner turns a goal into sub-tasks. The default heuristic planner needs no LLM.
type Planner func(goal string, bots []Bot) []SubTask

// PersistFunc writes the current run set (newest-first) after mutations.
type PersistFunc func(runs []Run) error

// Orchestrator plans, assigns, runs, and aggregates.
type Orchestrator struct {
	mu      sync.Mutex
	runs    map[string]*Run
	planner Planner
	worker  WorkerFunc
	persist PersistFunc
	now     func() time.Time
	newID   func() string
}

// NewOrchestrator builds an in-memory orchestrator. worker may be nil for plan-only tests.
func NewOrchestrator(worker WorkerFunc) *Orchestrator {
	return &Orchestrator{
		runs:    map[string]*Run{},
		planner: DefaultPlan,
		worker:  worker,
		now:     time.Now,
		newID: func() string {
			return fmt.Sprintf("ag-%d", time.Now().UnixNano())
		},
	}
}

// SetPlanner replaces the default heuristic planner.
func (o *Orchestrator) SetPlanner(p Planner) {
	if o == nil || p == nil {
		return
	}
	o.planner = p
}

// SetWorker replaces the worker used for sub-task execution.
func (o *Orchestrator) SetWorker(w WorkerFunc) {
	if o == nil {
		return
	}
	o.worker = w
}

// SetPersist installs a durable sink called after each run mutation.
func (o *Orchestrator) SetPersist(p PersistFunc) {
	if o == nil {
		return
	}
	o.persist = p
}

// LoadRuns seeds in-memory history from durable storage (id collisions keep existing).
func (o *Orchestrator) LoadRuns(runs []Run) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := range runs {
		r := runs[i]
		if strings.TrimSpace(r.ID) == "" {
			continue
		}
		if _, ok := o.runs[r.ID]; ok {
			continue
		}
		cp := r
		o.runs[r.ID] = &cp
	}
}

func (o *Orchestrator) persistLocked() {
	if o == nil || o.persist == nil {
		return
	}
	out := make([]Run, 0, len(o.runs))
	for _, r := range o.runs {
		out = append(out, *cloneRun(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	_ = o.persist(out)
}

// DefaultPlan splits a goal on numbered lines / semicolons / "然后|并且|and then".
// Each piece gets NeedCaps inferred from simple keyword hints.
func DefaultPlan(goal string, _ []Bot) []SubTask {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return nil
	}
	parts := splitGoal(goal)
	out := make([]SubTask, 0, len(parts))
	for i, p := range parts {
		out = append(out, SubTask{
			ID:          fmt.Sprintf("st-%d", i+1),
			Description: p,
			NeedCaps:    inferCaps(p),
			Status:      "pending",
		})
	}
	return out
}

func splitGoal(goal string) []string {
	replacer := strings.NewReplacer("；", ";", "\n", ";", "然后", ";", "并且", ";", " and then ", ";", " then ", ";")
	raw := replacer.Replace(goal)
	var parts []string
	for _, p := range strings.Split(raw, ";") {
		p = strings.TrimSpace(p)
		p = strings.TrimLeft(p, "0123456789.、)） ")
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return []string{goal}
	}
	return parts
}

func inferCaps(desc string) []string {
	lower := strings.ToLower(desc)
	var caps []string
	add := func(c string) {
		for _, x := range caps {
			if x == c {
				return
			}
		}
		caps = append(caps, c)
	}
	switch {
	case strings.Contains(lower, "search") || strings.Contains(desc, "搜索") || strings.Contains(desc, "查"):
		add("search")
	case strings.Contains(lower, "code") || strings.Contains(desc, "代码") || strings.Contains(desc, "编程"):
		add("coding")
	case strings.Contains(lower, "browser") || strings.Contains(desc, "浏览") || strings.Contains(desc, "网页"):
		add("browser")
	case strings.Contains(lower, "write") || strings.Contains(desc, "写") || strings.Contains(desc, "文档"):
		add("writing")
	case strings.Contains(lower, "image") || strings.Contains(desc, "图") || strings.Contains(desc, "截图"):
		add("vision")
	}
	return caps
}

// PickBot chooses the best worker for needCaps. Routers and watchers are never assigned work.
func PickBot(bots []Bot, needCaps []string, used map[string]int) (Bot, bool) {
	type scored struct {
		bot   Bot
		score int
	}
	var cands []scored
	for _, b := range bots {
		role := Role(strings.ToLower(string(b.Role)))
		if role == "" {
			role = RoleWorker
		}
		if role == RoleRouter || role == RoleWatcher {
			continue
		}
		score := 0
		capSet := map[string]bool{}
		for _, c := range b.Capabilities {
			capSet[strings.ToLower(strings.TrimSpace(c))] = true
		}
		for _, need := range needCaps {
			if capSet[strings.ToLower(strings.TrimSpace(need))] {
				score += 10
			}
		}
		if len(needCaps) == 0 {
			score = 1
		}
		if score == 0 && len(needCaps) > 0 {
			continue
		}
		score -= used[b.ID] // prefer less-loaded bots
		cands = append(cands, scored{bot: b, score: score})
	}
	if len(cands) == 0 {
		// Fall back to any worker even without cap match.
		for _, b := range bots {
			role := Role(strings.ToLower(string(b.Role)))
			if role == RoleRouter || role == RoleWatcher {
				continue
			}
			return b, true
		}
		return Bot{}, false
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].bot.ID < cands[j].bot.ID
	})
	return cands[0].bot, true
}

// Start plans, assigns workers, and (when a WorkerFunc is set) runs them in parallel.
func (o *Orchestrator) Start(ctx context.Context, goal string, routerBotID string, bots []Bot) (*Run, error) {
	if o == nil {
		return nil, fmt.Errorf("orchestrator is nil")
	}
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return nil, fmt.Errorf("goal is required")
	}
	now := o.now()
	run := &Run{
		ID:          o.newID(),
		Goal:        goal,
		RouterBotID: strings.TrimSpace(routerBotID),
		Status:      "planning",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	tasks := o.planner(goal, bots)
	if len(tasks) == 0 {
		return nil, fmt.Errorf("planner produced no sub-tasks")
	}
	used := map[string]int{}
	botByID := map[string]Bot{}
	for _, b := range bots {
		botByID[b.ID] = b
	}
	for i := range tasks {
		bot, ok := PickBot(bots, tasks[i].NeedCaps, used)
		if !ok {
			tasks[i].Status = "unassigned"
			tasks[i].Error = "no worker bot available"
			continue
		}
		tasks[i].AssignedBot = bot.ID
		tasks[i].AssignedName = bot.Name
		tasks[i].Status = "assigned"
		used[bot.ID]++
	}
	run.SubTasks = tasks
	run.Status = "running"
	o.mu.Lock()
	o.runs[run.ID] = run
	o.persistLocked()
	o.mu.Unlock()

	if o.worker == nil {
		o.finalize(run, botByID)
		return cloneRun(run), nil
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range run.SubTasks {
		if run.SubTasks[i].AssignedBot == "" {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			task := run.SubTasks[idx]
			bot := botByID[task.AssignedBot]
			mu.Lock()
			run.SubTasks[idx].Status = "running"
			run.UpdatedAt = o.now()
			mu.Unlock()
			result, err := o.worker(ctx, bot, task)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				run.SubTasks[idx].Status = "failed"
				run.SubTasks[idx].Error = err.Error()
			} else {
				run.SubTasks[idx].Status = "done"
				run.SubTasks[idx].Result = strings.TrimSpace(result)
			}
			run.UpdatedAt = o.now()
		}(i)
	}
	wg.Wait()
	o.finalize(run, botByID)
	return cloneRun(run), nil
}

func (o *Orchestrator) finalize(run *Run, botByID map[string]Bot) {
	progress := make([]string, 0, len(run.SubTasks))
	var parts []string
	failed := 0
	done := 0
	for _, t := range run.SubTasks {
		name := t.AssignedName
		if name == "" {
			name = t.AssignedBot
		}
		if name == "" {
			name = "unassigned"
		}
		line := fmt.Sprintf("%s: %s — %s", name, t.Status, collapseOneLine(t.Description))
		if t.Result != "" {
			line += " → " + collapseOneLine(t.Result)
		}
		if t.Error != "" {
			line += " (" + collapseOneLine(t.Error) + ")"
		}
		progress = append(progress, line)
		switch t.Status {
		case "done":
			done++
			if t.Result != "" {
				parts = append(parts, fmt.Sprintf("%s完成了「%s」：%s", name, collapseOneLine(t.Description), collapseOneLine(t.Result)))
			} else {
				parts = append(parts, fmt.Sprintf("%s完成了「%s」。", name, collapseOneLine(t.Description)))
			}
		case "failed", "unassigned":
			failed++
			parts = append(parts, fmt.Sprintf("%s未能完成「%s」。", name, collapseOneLine(t.Description)))
		default:
			// plan-only / assigned without worker
			parts = append(parts, fmt.Sprintf("已分配「%s」给 %s。", collapseOneLine(t.Description), name))
		}
		_ = botByID
	}
	summary := "团队协作结果：" + strings.Join(parts, " ")
	if failed == 0 && done == len(run.SubTasks) {
		run.Status = "completed"
	} else if done == 0 && failed > 0 {
		run.Status = "failed"
	} else if o.worker == nil {
		run.Status = "planned"
	} else {
		run.Status = "completed_with_errors"
	}
	run.Summary = summary
	run.Progress = progress
	run.UpdatedAt = o.now()
	o.mu.Lock()
	o.runs[run.ID] = run
	o.persistLocked()
	o.mu.Unlock()
}

// SetPageID attaches a shared page id to a run (agentgroup ↔ sharedpage link).
func (o *Orchestrator) SetPageID(runID, pageID string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	r, ok := o.runs[strings.TrimSpace(runID)]
	if !ok || r == nil {
		return
	}
	r.PageID = strings.TrimSpace(pageID)
	r.UpdatedAt = o.now()
	o.persistLocked()
}

func collapseOneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > 120 {
		r := []rune(s)
		return string(r[:117]) + "..."
	}
	return s
}

// Get returns a run by id.
func (o *Orchestrator) Get(id string) (*Run, bool) {
	if o == nil {
		return nil, false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	r, ok := o.runs[strings.TrimSpace(id)]
	if !ok {
		return nil, false
	}
	return cloneRun(r), true
}

// List returns runs newest-first.
func (o *Orchestrator) List() []Run {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Run, 0, len(o.runs))
	for _, r := range o.runs {
		out = append(out, *cloneRun(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func cloneRun(r *Run) *Run {
	if r == nil {
		return nil
	}
	cp := *r
	cp.SubTasks = append([]SubTask(nil), r.SubTasks...)
	cp.Progress = append([]string(nil), r.Progress...)
	return &cp
}
