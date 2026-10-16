package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestCreateTaskUnifiedZeroValueMatchesCreateTask(t *testing.T) {
	app := newProjectSearchTestApp(t)

	legacy := app.CreateTask("帮我整理周报", "")
	if legacy.ProjectPath == "" {
		t.Fatal("CreateTask returned empty project path")
	}
	unified, err := app.CreateTaskUnified(TaskCreateOptions{Name: "帮我整理周报"})
	if err != nil {
		t.Fatalf("CreateTaskUnified: %v", err)
	}
	if unified.ProjectPath == "" {
		t.Fatal("CreateTaskUnified returned empty path")
	}

	legacyRec := app.memoryStore.ProjectIndex().Get(legacy.ProjectPath)
	unifiedRec := app.memoryStore.ProjectIndex().Get(unified.ProjectPath)
	if legacyRec == nil || unifiedRec == nil {
		t.Fatalf("missing records legacy=%v unified=%v", legacyRec != nil, unifiedRec != nil)
	}
	if legacyRec.Name != unifiedRec.Name {
		t.Fatalf("record name legacy=%q unified=%q", legacyRec.Name, unifiedRec.Name)
	}
	// Tags embed the unique project path; compare the sets with path tags removed.
	stripPath := func(tags []string) []string {
		out := make([]string, 0, len(tags))
		for _, tag := range tags {
			if strings.Contains(tag, string(filepath.Separator)) {
				continue
			}
			out = append(out, tag)
		}
		return out
	}
	if strings.Join(stripPath(legacyRec.Tags), ",") != strings.Join(stripPath(unifiedRec.Tags), ",") {
		t.Fatalf("record tags legacy=%#v unified=%#v", legacyRec.Tags, unifiedRec.Tags)
	}
	for _, tag := range []string{taskManagementTag, taskUserCreatedTag} {
		if !projectRecordHasTagLike(unifiedRec.Tags, tag) {
			t.Fatalf("unified tags = %#v, want %q", unifiedRec.Tags, tag)
		}
	}
	if projectRecordHasTagLike(unifiedRec.Tags, taskCodingDevTag) {
		t.Fatalf("unified tags = %#v, did not expect coding_dev", unifiedRec.Tags)
	}
}

func TestCreateTaskUnifiedKeepsShortExplicitName(t *testing.T) {
	app := newProjectSearchTestApp(t)

	got, err := app.CreateTaskUnified(TaskCreateOptions{Name: "继续"})
	if err != nil {
		t.Fatalf("CreateTaskUnified: %v", err)
	}
	if got.ProjectPath == "" {
		t.Fatal("short explicit name returned empty path")
	}
	rec := app.memoryStore.ProjectIndex().Get(got.ProjectPath)
	if rec == nil || rec.Name != "继续" {
		t.Fatalf("record = %#v, want name 继续", rec)
	}
	if _, err := app.CreateTaskUnified(TaskCreateOptions{Name: "   "}); err == nil {
		t.Fatal("blank name should fail")
	}

	remote, err := app.CreateTaskUnified(TaskCreateOptions{
		Name: "ok",
		Mode: "remote_coding_dev",
		Remote: &RemoteTarget{
			Host:    "10.0.0.8",
			User:    "ubuntu",
			WorkDir: "/srv/app",
			Port:    22,
		},
	})
	if err != nil {
		t.Fatalf("remote CreateTaskUnified: %v", err)
	}
	if remote.ProjectPath == "" {
		t.Fatal("remote short name returned empty path")
	}
	remoteRec := app.memoryStore.ProjectIndex().Get(remote.ProjectPath)
	if remoteRec == nil || remoteRec.Name != "ok" {
		t.Fatalf("remote record = %#v, want name ok", remoteRec)
	}
}

func TestCreateTaskUnifiedValidation(t *testing.T) {
	app := newProjectSearchTestApp(t)

	if _, err := app.CreateTaskUnified(TaskCreateOptions{}); err == nil {
		t.Fatal("empty name should fail")
	}
	if _, err := app.CreateTaskUnified(TaskCreateOptions{Name: "x", Mode: "bogus"}); err == nil {
		t.Fatal("unknown mode should fail")
	}
	if _, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:               "x",
		ExpertID:           "expert-paper",
		WorkflowTemplateID: "coding",
	}); err == nil {
		t.Fatal("expert + workflow template should fail")
	}
	if _, err := app.CreateTaskUnified(TaskCreateOptions{Name: "x", Mode: "remote_coding_dev"}); err == nil {
		t.Fatal("remote mode without Remote should fail")
	}
	for _, remote := range []*RemoteTarget{
		{User: "ubuntu", WorkDir: "/srv/app"},
		{Host: "10.0.0.8", WorkDir: "/srv/app"},
		{Host: "10.0.0.8", User: "ubuntu"},
	} {
		if _, err := app.CreateTaskUnified(TaskCreateOptions{Name: "x", Mode: "remote_coding_dev", Remote: remote}); err == nil {
			t.Fatalf("remote %+v should fail validation", remote)
		}
	}
	if _, err := app.CreateTaskUnified(TaskCreateOptions{Name: "x", Mode: "cloud"}); err == nil {
		t.Fatal("cloud mode without workspace id should fail")
	}
}

func TestCreateTaskUnifiedExpertBranch(t *testing.T) {
	app := newProjectSearchTestApp(t)

	path, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:       "review my paper",
		ExpertID:   "expert-paper",
		ExpertName: "Paper reviewer",
	})
	if err != nil {
		t.Fatalf("CreateTaskUnified: %v", err)
	}
	rec := app.memoryStore.ProjectIndex().Get(path.ProjectPath)
	if rec == nil {
		t.Fatalf("missing expert task record %q", path.ProjectPath)
	}
	if !projectRecordHasTagLike(rec.Tags, taskSourceExpertPrefix+"expert-paper") {
		t.Fatalf("expert task tags = %#v, want source tag", rec.Tags)
	}
}

func TestCreateTaskUnifiedExpertBranchStoresWorkingDir(t *testing.T) {
	app := newProjectSearchTestApp(t)
	dir := t.TempDir()

	path, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:       "write the paper",
		ExpertID:   "expert-paper",
		ExpertName: "Paper reviewer",
		WorkingDir: dir,
	})
	if err != nil {
		t.Fatalf("CreateTaskUnified: %v", err)
	}
	if got := app.recentTaskWorkingDir(path.ProjectPath); got != normalizeRecentTaskWorkingDir(dir) {
		t.Fatalf("working dir = %q, want %q", got, normalizeRecentTaskWorkingDir(dir))
	}

	other := t.TempDir()
	again, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:       "write the paper again",
		ExpertID:   "expert-paper",
		ExpertName: "Paper reviewer",
		WorkingDir: other,
	})
	if err != nil {
		t.Fatalf("second CreateTaskUnified: %v", err)
	}
	if again.ProjectPath != path.ProjectPath {
		t.Fatalf("expert task path = %q, want the existing record %q", again.ProjectPath, path.ProjectPath)
	}
	if got := app.recentTaskWorkingDir(path.ProjectPath); got != normalizeRecentTaskWorkingDir(other) {
		t.Fatalf("updated working dir = %q, want %q", got, normalizeRecentTaskWorkingDir(other))
	}
}

func TestCreateTaskUnifiedLatexExpertStartsAFreshPaper(t *testing.T) {
	app := newProjectSearchTestApp(t)

	first, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:       "write the first paper",
		ExpertID:   builtinLatexExpertID,
		ExpertName: "LaTeX",
	})
	if err != nil {
		t.Fatalf("first CreateTaskUnified: %v", err)
	}
	if strings.TrimSpace(first.ProjectPath) == "" {
		t.Fatal("first latex task path is empty")
	}
	persist := app.ensureProjectTabSessionPersist()
	if err := persist.SaveSession(&TabSessionData{
		TabID:        expertTabSessionID(builtinLatexExpertID),
		ProjectPath:  first.ProjectPath,
		Conversation: []interface{}{map[string]interface{}{"role": "user", "content": "old paper"}},
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed expert transcript: %v", err)
	}

	second, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:       "write another paper",
		ExpertID:   builtinLatexExpertID,
		ExpertName: "LaTeX",
	})
	if err != nil {
		t.Fatalf("second CreateTaskUnified: %v", err)
	}
	if second.ProjectPath == "" || second.ProjectPath == first.ProjectPath {
		t.Fatalf("second latex task = %q, want a new directory (first %q)", second.ProjectPath, first.ProjectPath)
	}
	session, err := persist.LoadSession(expertTabSessionID(builtinLatexExpertID))
	if err != nil {
		t.Fatalf("load expert transcript: %v", err)
	}
	if session == nil || len(session.Conversation) != 0 || session.ConversationClearedAt == 0 {
		t.Fatalf("expert transcript was not cleared: %+v", session)
	}
	if strings.TrimSpace(session.ProjectPath) != "" {
		t.Fatalf("cleared expert session still claims project %q", session.ProjectPath)
	}
	archived, err := persist.LoadLatestSessionForProject(first.ProjectPath)
	if err != nil {
		t.Fatalf("load archived paper transcript: %v", err)
	}
	if archived == nil || !latexSessionHasUserTurn(archived.Conversation) {
		t.Fatalf("previous paper lost its transcript: %+v", archived)
	}
	resumed := app.CreateExpertTask(builtinLatexExpertID, "LaTeX")
	if resumed.ProjectPath != second.ProjectPath {
		t.Fatalf("resume path = %q, want newest paper %q", resumed.ProjectPath, second.ProjectPath)
	}
	pi := app.memoryStore.ProjectIndex()
	indexed := pi.ListAllMatching(func(candidate memory.ProjectRecord) bool {
		return projectRecordHasTag(candidate, taskSourceExpertPrefix+builtinLatexExpertID)
	})
	if len(indexed) != 2 {
		t.Fatalf("index has %d latex expert tasks, want 2", len(indexed))
	}
	listed := 0
	var listedDump []string
	for _, item := range app.ListTasks(50) {
		listedDump = append(listedDump, item.ProjectPath+" ["+strings.Join(item.Tags, ",")+"]")
		if projectRecordHasTagLike(item.Tags, taskSourceExpertPrefix+builtinLatexExpertID) {
			listed++
		}
	}
	if listed != 2 {
		t.Fatalf("latex expert tasks listed %d times, want both papers; rows=%s", listed, strings.Join(listedDump, " || "))
	}
}

func TestAbandonUnopenedFreshLatexTaskRestoresThePreviousPaper(t *testing.T) {
	app := newProjectSearchTestApp(t)
	first, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:       "write the first paper",
		ExpertID:   builtinLatexExpertID,
		ExpertName: "LaTeX",
	})
	if err != nil {
		t.Fatalf("first CreateTaskUnified: %v", err)
	}
	persist := app.ensureProjectTabSessionPersist()
	if err := persist.SaveSession(&TabSessionData{
		TabID:        expertTabSessionID(builtinLatexExpertID),
		ProjectPath:  first.ProjectPath,
		Conversation: []interface{}{map[string]interface{}{"role": "user", "content": "old paper"}},
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	second, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:       "write another paper",
		ExpertID:   builtinLatexExpertID,
		ExpertName: "LaTeX",
	})
	if err != nil {
		t.Fatalf("second CreateTaskUnified: %v", err)
	}
	if err := app.AbandonUnopenedFreshLatexTask(second.ProjectPath); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if err := app.AbandonUnopenedFreshLatexTask(first.ProjectPath); err == nil {
		t.Fatal("abandon accepted the paper that is no longer the unopened row")
	}
	session, err := persist.LoadSession(expertTabSessionID(builtinLatexExpertID))
	if err != nil {
		t.Fatal(err)
	}
	if session == nil || !latexSessionHasUserTurn(session.Conversation) || strings.TrimSpace(session.ProjectPath) != "" {
		t.Fatalf("expert transcript was not restored: %+v", session)
	}
	resumed := app.CreateExpertTask(builtinLatexExpertID, "LaTeX")
	if resumed.ProjectPath != first.ProjectPath {
		t.Fatalf("resume path = %q, want the original paper %q", resumed.ProjectPath, first.ProjectPath)
	}
	if _, err := os.Stat(second.ProjectPath); !os.IsNotExist(err) {
		t.Fatalf("abandoned paper directory still present: %v", err)
	}
}

func TestCreateTaskUnifiedWorkflowBranch(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.workflowV2 = buildWorkflowV2State(v2.NewMemoryStore())
	app.workflowV2.machine.SetAllowTempTestPaths(true)

	path, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:               "做一个贪吃蛇游戏",
		WorkflowTemplateID: "coding",
	})
	if err != nil {
		t.Fatalf("CreateTaskUnified: %v", err)
	}
	ownerID := projectSessionOwnerID(path.ProjectPath)
	state := app.workflowV2.machine.GetActive(ownerID)
	if state == nil {
		t.Fatalf("no active workflow for owner %q", ownerID)
	}
	if state.Type != "coding" {
		t.Fatalf("workflow type = %q, want coding", state.Type)
	}
	if state.ProjectPath != path.ProjectPath {
		t.Fatalf("workflow project = %q, want task path %q", state.ProjectPath, path.ProjectPath)
	}
	if state.Summary != "做一个贪吃蛇游戏" {
		t.Fatalf("workflow summary = %q", state.Summary)
	}

	if _, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:               "x",
		WorkflowTemplateID: "no_such_template",
	}); err == nil {
		t.Fatal("unknown workflow template should fail")
	}
}

func TestCreateTaskUnifiedWorkflowCloudRoutesToCloudBranch(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.workflowV2 = buildWorkflowV2State(v2.NewMemoryStore())
	app.workflowV2.machine.SetAllowTempTestPaths(true)

	// Workflow + cloud workspace must create the record via the cloud branch
	// (CreateTaskWithMode does not understand mode "cloud"). A malformed
	// workspace id makes the cloud branch fail fast — proving the routing
	// without needing real cloud infrastructure.
	_, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:               "quarterly review",
		Mode:               "cloud",
		CloudWorkspaceID:   "not-a-valid-cache-id!",
		WorkflowTemplateID: "coding",
	})
	if err == nil {
		t.Fatal("malformed cloud workspace id should fail")
	}
	if !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("error %q should come from the cloud workspace path", err.Error())
	}
}

func TestCreateTaskUnifiedWorkflowParams(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.workflowV2 = buildWorkflowV2State(v2.NewMemoryStore())
	app.workflowV2.machine.SetAllowTempTestPaths(true)

	// Complete params: SubmitForm succeeds and the first phase's FormData is
	// prefilled, so the next user message runs the phase directly.
	full, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:               "做一个贪吃蛇游戏",
		WorkflowTemplateID: "coding",
		Params:             map[string]string{"project_name": "snake", "description": "经典贪吃蛇游戏"},
	})
	if err != nil {
		t.Fatalf("CreateTaskUnified with params: %v", err)
	}
	state := app.workflowV2.machine.GetActive(projectSessionOwnerID(full.ProjectPath))
	if state == nil || state.ActivePhase() == nil {
		t.Fatalf("no active workflow/phase for %q", full)
	}
	if state.ActivePhase().FormData == nil {
		t.Fatal("FormData should be prefilled from Params")
	}
	if got := state.ActivePhase().FormData["project_name"]; got != "snake" {
		t.Fatalf("FormData[project_name] = %v, want snake", got)
	}

	// Incomplete params: SubmitForm fails and is only logged; the workflow must
	// stay active with FormData nil so the AG UI form is shown on the first
	// message instead of the workflow deadlocking.
	partial, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:               "再做一个俄罗斯方块",
		WorkflowTemplateID: "coding",
		Params:             map[string]string{"project_name": "tetris"},
	})
	if err != nil {
		t.Fatalf("CreateTaskUnified with partial params should still succeed: %v", err)
	}
	partialState := app.workflowV2.machine.GetActive(projectSessionOwnerID(partial.ProjectPath))
	if partialState == nil {
		t.Fatal("workflow must remain active after partial params")
	}
	if partialState.ActivePhase().FormData != nil {
		t.Fatal("FormData should stay nil while required slots are missing")
	}
}

func TestCreateTaskUnifiedWorkflowUnknownTemplateLeavesNoRecord(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.workflowV2 = buildWorkflowV2State(v2.NewMemoryStore())
	app.workflowV2.machine.SetAllowTempTestPaths(true)
	app.ensureMemoryStore()

	count := func() int {
		return len(app.memoryStore.ProjectIndex().ListAllMatching(func(candidate memory.ProjectRecord) bool {
			return projectRecordHasTag(candidate, taskManagementTag)
		}))
	}
	before := count()
	if _, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:               "x",
		WorkflowTemplateID: "no_such_template",
	}); err == nil {
		t.Fatal("unknown workflow template should fail")
	}
	if after := count(); after != before {
		t.Fatalf("unknown template must not create an orphan task record: before=%d after=%d", before, after)
	}
}

func TestCreateTaskUnifiedWorkflowStartFailureStillReturnsPath(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.workflowV2 = buildWorkflowV2State(v2.NewMemoryStore())
	// Deliberately NOT calling SetAllowTempTestPaths: the test app's task
	// paths look like temp paths, so machine.Create fails after the record
	// was created — the exact partial-failure contract the GUI relies on
	// (Wails would discard the path if it traveled as the error return).
	res, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:               "工作流启动失败也要能打开任务",
		WorkflowTemplateID: "coding",
	})
	if err != nil {
		t.Fatalf("partial failure must not surface as err: %v", err)
	}
	if strings.TrimSpace(res.ProjectPath) == "" {
		t.Fatal("partial failure must still return the created task path")
	}
	if strings.TrimSpace(res.Warning) == "" {
		t.Fatal("partial failure must carry the degradation as Warning")
	}
	if rec := app.memoryStore.ProjectIndex().Get(res.ProjectPath); rec == nil {
		t.Fatalf("task record %q must exist after partial failure", res.ProjectPath)
	}
	if state := app.workflowV2.machine.GetActive(projectSessionOwnerID(res.ProjectPath)); state != nil {
		t.Fatal("workflow must not be active when its start failed")
	}
}

func TestListWorkflowTemplateSummaries(t *testing.T) {
	app := newProjectSearchTestApp(t)

	summaries := app.ListWorkflowTemplateSummaries()
	if len(summaries) == 0 {
		t.Fatal("no workflow template summaries")
	}
	if len(summaries) < 30 {
		t.Fatalf("only %d summaries, want the full builtin registry (~35)", len(summaries))
	}
	byID := make(map[string]WorkflowTemplateSummary, len(summaries))
	for _, summary := range summaries {
		if summary.ID == "" || summary.Title == "" || summary.Category == "" {
			t.Fatalf("summary missing fields: %+v", summary)
		}
		if summary.PhaseCount <= 0 {
			t.Fatalf("summary %q has no phases", summary.ID)
		}
		if !summary.RequiresWorkingDir {
			t.Fatalf("summary %q RequiresWorkingDir = false, want true (unset = required)", summary.ID)
		}
		byID[summary.ID] = summary
	}

	coding := byID["coding"]
	if coding.ID == "" {
		t.Fatal("missing coding template summary")
	}
	if coding.PhaseCount != 5 {
		t.Fatalf("coding phase count = %d, want 5", coding.PhaseCount)
	}
	if !coding.HasParamSlots {
		t.Fatal("coding template should have param slots (requirements form)")
	}

	// SemanticOnly templates must carry the flag so the frontend can filter them.
	for _, id := range []string{"maintenance", "bid_review", "changjiang_scholar_review", "nsfc_youth_review"} {
		summary, ok := byID[id]
		if !ok {
			t.Fatalf("missing summary for semantic-only template %q", id)
		}
		if !summary.SemanticOnly {
			t.Fatalf("summary %q SemanticOnly = false, want true", id)
		}
	}
	if byID["coding"].SemanticOnly {
		t.Fatal("coding template should not be SemanticOnly")
	}
}

func TestConversationEntriesFromTabSnapshotKeepsModelTurns(t *testing.T) {
	entries := conversationEntriesFromTabSnapshot([]interface{}{
		map[string]interface{}{"role": "User", "content": "  old paper  "},
		map[string]interface{}{"role": "assistant", "content": "compiled"},
		map[string]interface{}{"role": "tool", "content": "", "tool_name": "pdflatex", "tool_calls": []interface{}{map[string]interface{}{"name": "pdflatex"}}},
		map[string]interface{}{"role": "note", "content": "skip"},
		map[string]interface{}{"role": "user", "content": "   "},
	})
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want user, assistant, and the tool turn", len(entries))
	}
	if entries[0].Role != "user" || entries[0].Content != "  old paper  " {
		t.Fatalf("user entry = %#v", entries[0])
	}
	if entries[2].Role != "tool" || entries[2].ToolName != "pdflatex" || entries[2].ToolCalls == nil {
		t.Fatalf("tool entry = %#v", entries[2])
	}
}
