package main

// remote_coding_subagent_spawn.go — Codex-style nested subagents for pure remote coding.
//
// Mirrors local spawn_coding_agent but keeps all SSH work on the shared remote
// session. Children cannot spawn further (nest depth hard cap).

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

// Remote SSH tool surface by nested role. Worker allows the full remote set
// (except spawn, which is gated by canSpawnRemoteCodingAgent).
var remoteCodingSpawnRoleTools = map[codingSubAgentRole]map[string]bool{
	// todo_write is root/worker-only (requirement breakdown for implement turns).
	codingRoleExplorer: {
		"ssh_read_file": true, "ssh_list_dir": true, "ssh_bash": true,
		codeNavigationToolName: true, reportLocalizationToolName: true,
		"web_search": true, "web_fetch": true, "current_datetime": true,
		"coding_knowledge_search": true, "knowledge_search": true,
	},
	codingRoleReviewer: {
		"ssh_read_file": true, "ssh_list_dir": true, "ssh_bash": true, "ssh_check_task": true,
		codeNavigationToolName: true, reportLocalizationToolName: true,
		"web_search": true, "web_fetch": true, "current_datetime": true,
		"coding_knowledge_search": true, "knowledge_search": true,
	},
}

func (r *RemoteCodingSubAgent) canSpawnRemoteCodingAgent() bool {
	if r == nil {
		return false
	}
	if r.nestDepth >= codingSubAgentMaxNestDepth {
		return false
	}
	role := r.role
	if role == "" {
		role = codingRoleWorker
	}
	return role == codingRoleWorker
}

func (r *RemoteCodingSubAgent) remoteToolAllowedForRole(name string) bool {
	if r == nil {
		return true
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == codingSubAgentSpawnToolName {
		return r.canSpawnRemoteCodingAgent()
	}
	role := r.role
	if role == "" || role == codingRoleWorker {
		return true
	}
	allowed, ok := remoteCodingSpawnRoleTools[role]
	if !ok || allowed == nil {
		return true
	}
	return allowed[name]
}

func filterRemoteCodingToolsForRole(tools []map[string]interface{}, agent *RemoteCodingSubAgent) []map[string]interface{} {
	if agent == nil || len(tools) == 0 {
		return tools
	}
	role := agent.role
	if role == "" || role == codingRoleWorker {
		if agent.canSpawnRemoteCodingAgent() {
			return tools
		}
		out := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			fn, _ := t["function"].(map[string]interface{})
			name, _ := fn["name"].(string)
			if name == codingSubAgentSpawnToolName {
				continue
			}
			out = append(out, t)
		}
		return out
	}
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		fn, _ := t["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if agent.remoteToolAllowedForRole(name) {
			out = append(out, t)
		}
	}
	return out
}

func (c *remoteCodingCallbacks) executeSpawnRemoteCodingAgent(args map[string]interface{}) string {
	if c == nil || c.agent == nil {
		return "remote coding subagent is unavailable"
	}
	parent := c.agent
	if !parent.canSpawnRemoteCodingAgent() {
		return fmt.Sprintf("%s unavailable: only remote pure-coding root can spawn (depth=%d role=%q)",
			codingSubAgentSpawnToolName, parent.nestDepth, parent.role)
	}
	specs, err := parseCodingSpawnSpecs(args)
	if err != nil {
		return "spawn_coding_agent: " + err.Error()
	}
	if parent.loopCtx != nil && parent.loopCtx.IsCancelled() {
		return "remote coding subagent cancelled before spawn"
	}

	var progressMu sync.Mutex
	progress := func(msg string) {
		progressMu.Lock()
		defer progressMu.Unlock()
		emitCodingSubAgentProgress(parent.onProgress, msg)
	}
	childProgress := func(text string) {
		if parent.onProgress == nil {
			return
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		parent.onProgress(text)
	}

	parallel := shouldParallelizeCodingSpawn(specs)
	mode := "sequential"
	if parallel {
		mode = "parallel"
	}
	progress(fmt.Sprintf("spawn_coding_agent(remote): launching %d nested agent(s) (%s)", len(specs), mode))

	type spawnOutcome struct {
		idx    int
		spec   codingSpawnSpec
		result *RemoteCodingSubAgentResult
	}
	outcomes := make([]spawnOutcome, len(specs))
	runOne := func(i int, spec codingSpawnSpec) {
		if parent.loopCtx != nil && parent.loopCtx.IsCancelled() {
			outcomes[i] = spawnOutcome{
				idx:  i,
				spec: spec,
				result: &RemoteCodingSubAgentResult{
					Status: "cancelled",
					Error:  "remote coding subagent cancelled before nested agent start",
				},
			}
			return
		}
		progress(fmt.Sprintf("nested remote agent [%d/%d] role=%s starting", i+1, len(specs), spec.Role))
		res := parent.runNestedRemoteCodingAgent(spec, c, childProgress)
		outcomes[i] = spawnOutcome{idx: i, spec: spec, result: res}
		status := "unknown"
		if res != nil {
			status = res.Status
		}
		progress(fmt.Sprintf("nested remote agent [%d/%d] role=%s finished status=%s", i+1, len(specs), spec.Role, status))
	}

	if parallel {
		var wg sync.WaitGroup
		for i, spec := range specs {
			wg.Add(1)
			go func(i int, spec codingSpawnSpec) {
				defer wg.Done()
				runOne(i, spec)
			}(i, spec)
		}
		wg.Wait()
	} else {
		for i, spec := range specs {
			runOne(i, spec)
		}
	}

	var b strings.Builder
	passed := 0
	for _, o := range outcomes {
		res := o.result
		if res != nil && res.Status == "success" {
			passed++
		}
	}
	failed := len(outcomes) - passed
	// Prefix with 错误 so remoteCodingToolOutcome marks tool failure when any child fails.
	if failed > 0 {
		b.WriteString(fmt.Sprintf("错误: spawn_coding_agent(remote) 有子代理失败 passed=%d failed=%d mode=%s\n", passed, failed, mode))
	} else {
		b.WriteString(fmt.Sprintf("spawn_coding_agent(remote) completed: %d agent(s) mode=%s\n", len(outcomes), mode))
	}
	for _, o := range outcomes {
		res := o.result
		if res == nil {
			b.WriteString(fmt.Sprintf("\n### agent[%d] role=%s\nstatus=failed\nerror=nil result\n", o.idx, o.spec.Role))
			continue
		}
		b.WriteString(fmt.Sprintf("\n### agent[%d] role=%s task=%q\n", o.idx, o.spec.Role, truncateRunesForSubAgent(o.spec.Task, 120)))
		b.WriteString(fmt.Sprintf("status=%s iterations=%d tools=%d\n", res.Status, res.Iterations, res.ToolCalls))
		if res.Summary != "" {
			b.WriteString("summary:\n")
			b.WriteString(truncateRunesForSubAgent(res.Summary, 4000))
			b.WriteString("\n")
		}
		if res.Error != "" {
			b.WriteString("error: ")
			b.WriteString(compactSubAgentErrorSummary(res.Error))
			b.WriteString("\n")
		}
		if len(res.FilesModified) > 0 {
			b.WriteString("files_modified: ")
			b.WriteString(strings.Join(res.FilesModified, ", "))
			b.WriteString("\n")
		}
		if len(res.FilesCreated) > 0 {
			b.WriteString("files_created: ")
			b.WriteString(strings.Join(res.FilesCreated, ", "))
			b.WriteString("\n")
		}
		c.mergeRemoteSpawnedFileAudit(res.FilesModified, res.FilesCreated)
	}
	b.WriteString(fmt.Sprintf("\npassed=%d failed=%d\n", passed, failed))
	return strings.TrimSpace(b.String())
}

func (c *remoteCodingCallbacks) mergeRemoteSpawnedFileAudit(modified, created []string) {
	if c == nil {
		return
	}
	seenMod := map[string]bool{}
	for _, p := range c.filesModified {
		seenMod[p] = true
	}
	seenCreate := map[string]bool{}
	for _, p := range c.filesCreated {
		seenCreate[p] = true
	}
	createdSet := map[string]bool{}
	for _, p := range created {
		p = strings.TrimSpace(p)
		if p != "" {
			createdSet[p] = true
		}
	}
	for _, p := range modified {
		p = strings.TrimSpace(p)
		if p == "" || seenMod[p] {
			continue
		}
		isCreated := createdSet[p]
		c.trackRemoteFileChanged(p, isCreated)
		seenMod[p] = true
		if isCreated {
			seenCreate[p] = true
		}
	}
	for p := range createdSet {
		if seenCreate[p] || seenMod[p] {
			continue
		}
		c.trackRemoteFileChanged(p, true)
		seenCreate[p] = true
		seenMod[p] = true
	}
}

func (parent *RemoteCodingSubAgent) runNestedRemoteCodingAgent(spec codingSpawnSpec, parentCB *remoteCodingCallbacks, onProgress func(string)) *RemoteCodingSubAgentResult {
	if parent == nil {
		return &RemoteCodingSubAgentResult{Status: "failed", Error: "parent remote coding subagent is nil"}
	}
	child := NewRemoteCodingSubAgent(
		parent.handler,
		parent.cfg,
		parent.httpClient,
		parent.sessionID,
		parent.workDir,
		parent.projectDir,
		parent.loopCtx,
	)
	child.nestDepth = parent.nestDepth + 1
	child.role = spec.Role
	child.codingKB = parent.codingKB
	child.generalKB = parent.generalKB
	// Reuse parent preview session so nested file edits still stream to the same panel.
	child.sourcePreviewEnabled = parent.sourcePreviewEnabled
	child.sourcePreviewSessionID = parent.sourcePreviewSessionID
	// Share high-risk approval state (mutex-protected).
	child.highRiskApproval = parent.highRiskApproval
	child.highRiskApprovalExplicit = true

	if onProgress == nil {
		onProgress = parent.onProgress
	}
	child.SetCallbacks(nil, onProgress)

	taskCtx := codingSpawnRolePromptHint(spec.Role)
	if parentCB != nil && strings.TrimSpace(parentCB.taskContext) != "" {
		taskCtx += "\n\n## Parent remote task context\n" + truncateRunesForSubAgent(parentCB.taskContext, 1500)
	}
	if parentCB != nil && strings.TrimSpace(parentCB.task) != "" {
		taskCtx += "\n\n## Parent task\n" + truncateRunesForSubAgent(parentCB.task, 800)
	}
	if spec.Context != "" {
		taskCtx += "\n\n## Spawn context\n" + truncateRunesForSubAgent(spec.Context, 2000)
	}
	taskDesc := fmt.Sprintf("[%s] %s", spec.Role, spec.Task)

	log.Printf("[remote-coding-spawn] start role=%s depth=%d task=%q session=%s project=%s",
		spec.Role, child.nestDepth, truncateRunesForSubAgent(spec.Task, 80), parent.sessionID, parent.projectDir)
	result := child.ExecuteTask(taskDesc, taskCtx)
	if result == nil {
		return &RemoteCodingSubAgentResult{Status: "failed", Error: "nested remote coding agent returned nil"}
	}
	log.Printf("[remote-coding-spawn] done role=%s status=%s iters=%d tools=%d err=%q",
		spec.Role, result.Status, result.Iterations, result.ToolCalls, compactSubAgentErrorSummary(result.Error))
	return result
}
