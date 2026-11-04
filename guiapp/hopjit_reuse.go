package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

type hopJITLLMInvoke func(context.Context, []interface{}, bool) (string, error)

type hopJITExecutionIdentity struct {
	PrincipalID   string
	PolicyOwnerID string
	SessionID     string
	Platform      string
}

type guiHopJITCaller struct {
	app      *App
	handler  *IMMessageHandler
	invoke   hopJITLLMInvoke
	identity hopJITExecutionIdentity
}

func (c *guiHopJITCaller) ExecuteStep(ctx context.Context, step hopjit.StepReady) (map[string]any, error) {
	if step.StepType == hopspec.StepAct {
		return c.executeActFree(ctx, step)
	}
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "你在 HopJIT 中执行一个原子步骤。只返回 JSON 对象，键必须严格匹配输出声明，不要输出 Markdown 围栏或解释。"},
		map[string]interface{}{"role": "user", "content": fmt.Sprintf("步骤类型: %s\n当前步骤: %s\n%s", step.StepType, step.Summary, step.Context.Render())},
	}
	text, err := c.call(ctx, messages, true)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stripHopJITCodeFence(text)), &result); err != nil {
		return nil, fmt.Errorf("HopJIT step returned invalid JSON: %w", err)
	}
	return result, nil
}

func (c *guiHopJITCaller) Replan(ctx context.Context, snapshot hopjit.EngineSnapshot) (string, error) {
	history, err := json.Marshal(snapshot.RetryHistory[snapshot.AdaptiveNeeded])
	if err != nil {
		return "", err
	}
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "为 HopJIT 的 adaptive 容器生成替代子步骤。只返回 HopSpec Markdown 步骤，不要围栏。保留原 Outputs 与 check final，不得加入 commit。"},
		map[string]interface{}{"role": "user", "content": fmt.Sprintf("目标: %s\n容器: %s\n失败历史: %s", snapshot.Spec.Goal, snapshot.AdaptiveNeeded, history)},
	}
	return c.call(ctx, messages, false)
}

func (c *guiHopJITCaller) call(ctx context.Context, messages []interface{}, jsonResponse bool) (string, error) {
	if c.invoke != nil {
		return c.invoke(ctx, messages, jsonResponse)
	}
	if c.app == nil {
		return "", errors.New("HopJIT LLM app is unavailable")
	}
	opts := simpleLLMRequestOptions{AwaitResponse: true}
	if jsonResponse {
		opts.ResponseFormat = map[string]interface{}{"type": "json_object"}
	}
	resp, err := doSimpleLLMRequestWithOptions(ctx, c.app.GetMaclawLLMConfig(), messages, nil, 120*time.Second, opts)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (a *App) RunHopSpec(spec string, inputs map[string]any) (hopjit.ReuseResult, error) {
	return a.runHopSpecWithIdentity(hopJITExecutionIdentity{}, spec, inputs)
}

func (a *App) runHopSpecWithIdentity(identity hopJITExecutionIdentity, spec string, inputs map[string]any) (hopjit.ReuseResult, error) {
	store, err := a.openHopJITPersistence()
	if err != nil {
		return hopjit.ReuseResult{}, err
	}
	return runHopSpecWithPersistence(context.Background(), store, spec, inputs, a.newHopJITCallerWithIdentity(identity))
}

func (a *App) ResumeHopSpec(runID string, answers map[string]any) (hopjit.ReuseResult, error) {
	return a.resumeHopSpecWithIdentity(hopJITExecutionIdentity{}, runID, answers)
}

func (a *App) resumeHopSpecWithIdentity(identity hopJITExecutionIdentity, runID string, answers map[string]any) (hopjit.ReuseResult, error) {
	store, err := a.openHopJITPersistence()
	if err != nil {
		return hopjit.ReuseResult{}, err
	}
	return resumeHopSpecWithPersistence(context.Background(), store, runID, answers, a.newHopJITCallerWithIdentity(identity))
}

func (a *App) AbortHopSpec(runID, reason string) error {
	return a.abortHopSpecWithIdentity(hopJITExecutionIdentity{}, runID, reason)
}

func (a *App) abortHopSpecWithIdentity(identity hopJITExecutionIdentity, runID, reason string) error {
	store, err := a.openHopJITPersistence()
	if err != nil {
		return err
	}
	engine := hopjit.NewExecutionEngine(store, hopjit.WithRunOwner(identity.PrincipalID))
	if err := engine.Resume(context.Background(), runID); err != nil {
		return err
	}
	return engine.Abort(context.Background(), reason)
}

func authorizeHopJITSnapshot(snapshot hopjit.EngineSnapshot, identity hopJITExecutionIdentity) error {
	if snapshot.OwnerID != strings.TrimSpace(identity.PrincipalID) {
		return hopjit.ErrRunAccessDenied
	}
	return nil
}

type hopJITPersistenceState struct {
	mu    sync.Mutex
	store *hopjit.SQLitePersistence
	path  string
}

func (s *hopJITPersistenceState) open(path string) (*hopjit.SQLitePersistence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store != nil && s.path == path {
		return s.store, nil
	}
	if s.store != nil {
		_ = s.store.Close()
		s.store = nil
	}
	store, err := hopjit.NewSQLitePersistence(path)
	if err != nil {
		return nil, err
	}
	s.store = store
	s.path = path
	return store, nil
}

func (s *hopJITPersistenceState) close() {
	s.mu.Lock()
	store := s.store
	s.store = nil
	s.path = ""
	s.mu.Unlock()
	if store != nil {
		_ = store.Close()
	}
}

func (a *App) openHopJITPersistence() (*hopjit.SQLitePersistence, error) {
	if a == nil {
		return nil, errors.New("HopJIT app is unavailable")
	}
	return a.hopJITPersistence.open(filepath.Join(a.GetDataDir(), "hopjit", "hopjit.db"))
}

func (a *App) closeHopJITPersistence() {
	if a != nil {
		a.hopJITPersistence.close()
	}
}

func (a *App) newHopJITCaller() *guiHopJITCaller {
	return a.newHopJITCallerWithIdentity(hopJITExecutionIdentity{})
}

func (a *App) newHopJITCallerWithIdentity(identity hopJITExecutionIdentity) *guiHopJITCaller {
	caller := &guiHopJITCaller{app: a, identity: identity}
	if handler := a.ensureLocalIMHandler(); handler != nil {
		caller.handler = handler
	}
	return caller
}

func (c *guiHopJITCaller) HopJITOwnerID() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.identity.PrincipalID)
}

func hopJITRunOwnerID(caller hopjit.ReuseCaller) string {
	if owner, ok := caller.(interface{ HopJITOwnerID() string }); ok {
		return owner.HopJITOwnerID()
	}
	return ""
}

func runHopSpecWithPersistence(ctx context.Context, store hopjit.PersistenceProvider, spec string, inputs map[string]any, caller hopjit.ReuseCaller) (hopjit.ReuseResult, error) {
	engine := hopjit.NewExecutionEngine(store, hopjit.WithRunOwner(hopJITRunOwnerID(caller)))
	if _, err := engine.Init(ctx, spec, inputs); err != nil {
		return hopjit.ReuseResult{}, err
	}
	return hopjit.DriveReuse(ctx, engine, caller)
}

func resumeHopSpecWithPersistence(ctx context.Context, store hopjit.PersistenceProvider, runID string, answers map[string]any, caller hopjit.ReuseCaller) (hopjit.ReuseResult, error) {
	engine := hopjit.NewExecutionEngine(store, hopjit.WithRunOwner(hopJITRunOwnerID(caller)))
	if err := engine.Resume(ctx, runID); err != nil {
		return hopjit.ReuseResult{}, err
	}
	snapshot, err := engine.Snapshot()
	if err != nil {
		return hopjit.ReuseResult{}, err
	}
	if snapshot.Pause != nil {
		if answers == nil {
			return hopjit.ReuseResult{}, errors.New("HopJIT pause answers are required")
		}
		if err := engine.Complete(ctx, snapshot.Pause.StepID, answers); err != nil {
			return hopjit.ReuseResult{}, err
		}
	} else if len(answers) > 0 {
		return hopjit.ReuseResult{}, errors.New("HopJIT run is not waiting for answers")
	}
	return hopjit.DriveReuse(ctx, engine, caller)
}

func stripHopJITCodeFence(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) >= 2 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		return strings.Join(lines[1:len(lines)-1], "\n")
	}
	return trimmed
}
