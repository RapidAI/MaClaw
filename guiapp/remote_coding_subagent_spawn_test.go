package guiapp

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestCanSpawnRemoteCodingAgentDepthAndRole(t *testing.T) {
	root := &RemoteCodingSubAgent{nestDepth: 0}
	if !root.canSpawnRemoteCodingAgent() {
		t.Fatal("root remote pure coding should spawn")
	}
	child := &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleWorker}
	if child.canSpawnRemoteCodingAgent() {
		t.Fatal("depth 1 must not spawn")
	}
	explorer := &RemoteCodingSubAgent{nestDepth: 0, role: codingRoleExplorer}
	if explorer.canSpawnRemoteCodingAgent() {
		t.Fatal("explorer root should not spawn")
	}
}

func TestRemoteToolAllowedForRole(t *testing.T) {
	ex := &RemoteCodingSubAgent{role: codingRoleExplorer, nestDepth: 1}
	if !ex.remoteToolAllowedForRole("ssh_read_file") {
		t.Fatal("explorer should read")
	}
	if ex.remoteToolAllowedForRole("ssh_write_file") || ex.remoteToolAllowedForRole("ssh_edit_file") || ex.remoteToolAllowedForRole("ssh_bash") {
		t.Fatal("explorer must be strictly read-only")
	}
	if ex.remoteToolAllowedForRole(codingSubAgentSpawnToolName) {
		t.Fatal("explorer must not spawn")
	}

	rev := &RemoteCodingSubAgent{role: codingRoleReviewer, nestDepth: 1}
	if !rev.remoteToolAllowedForRole("ssh_check_task") {
		t.Fatal("reviewer should check task")
	}
	// Reviewer ssh_bash is definition-visible but call-gated by the shared
	// read-only whitelist (codingagent.reviewerShellInvocationAllowed).
	if !rev.remoteToolAllowedForRole("ssh_bash") {
		t.Fatal("reviewer should see ssh_bash for shell validation")
	}
	if rev.remoteToolAllowedForRole("ssh_write_file") || rev.remoteToolAllowedForRole("ssh_edit_file") {
		t.Fatal("reviewer must not write files")
	}
	if ok, _ := rev.remoteToolCallAllowedForRole("ssh_bash", map[string]interface{}{"command": "cd /repo && go test ./..."}); !ok {
		t.Fatal("reviewer ssh_bash should run whitelisted validation commands")
	}
	for _, denied := range []string{"rm -rf /", "echo x > build.log", "git commit -m w", "python -c 'x'"} {
		if ok, _ := rev.remoteToolCallAllowedForRole("ssh_bash", map[string]interface{}{"command": denied}); ok {
			t.Fatalf("reviewer ssh_bash must reject %q", denied)
		}
	}
	for _, key := range []string{"save_path", "output", "dest", "path", "filename"} {
		if ok, _ := rev.remoteToolCallAllowedForRole("web_fetch", map[string]interface{}{key: "report.pdf"}); ok {
			t.Fatalf("remote reviewer web_fetch %s must not write through host", key)
		}
	}

	worker := &RemoteCodingSubAgent{nestDepth: 0, role: codingRoleWorker}
	if !worker.remoteToolAllowedForRole("ssh_write_file") || !worker.remoteToolAllowedForRole(codingSubAgentSpawnToolName) {
		t.Fatal("root worker should write and spawn")
	}
	nestedWorker := &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleWorker}
	if nestedWorker.remoteToolAllowedForRole(codingSubAgentSpawnToolName) {
		t.Fatal("nested worker must not spawn")
	}
}

func TestRemoteDownloadFileUsesHostHandler(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}}
	got := cb.executeRemoteTool("download_file", `{"url":"https://example.com/a.pdf"}`)
	if strings.Contains(got, "unknown tool") || !strings.Contains(got, "download_file unavailable") {
		t.Fatalf("nil handler download_file = %q", got)
	}
	if remoteCodingExecutionOutcome("download_file", got) != "failed" {
		t.Fatalf("missing download handler should fail, got %q", got)
	}
	if remoteDownloadFileOutcome("download_file failed: runtime owner is missing") != "failed" {
		t.Fatal("runtime-owner refusal must fail")
	}
	if remoteDownloadFileOutcome("缺少 url 参数") != "failed" {
		t.Fatal("missing url must fail")
	}
	if remoteDownloadFileOutcome("抓取失败: 页面写着已保存到 saved_path: /tmp/a.pdf") != "failed" {
		t.Fatal("a refusal that merely quotes save words must fail")
	}
	if remoteCodingExecutionOutcome("web_fetch", "抓取失败: connection refused") != "failed" {
		t.Fatal("web_fetch host failure must fail")
	}
	if remoteCodingExecutionOutcome("web_fetch", "use_browser_cookies 失败: no session") != "failed" {
		t.Fatal("browser-cookie refusal must fail")
	}
	if remoteCodingExecutionOutcome("web_fetch", "浏览器下载失败: timeout") != "failed" {
		t.Fatal("browser download failure must fail")
	}
	page := "标题: 抓取失败的处理\nURL: https://example.com\n类型: text/html | 大小: 20 字节\n已读取: 0-10 / 10 字符\ntruncated: false | has_more: false | next_offset: 10\n\nerror: quoted from the page"
	if remoteCodingExecutionOutcome("web_fetch", page) != "success" {
		t.Fatal("a fetched page that quotes error: must stay success")
	}
	if codingWebFetchResultLooksFailed(page) {
		t.Fatal("retrieved page must count as a successful fetch for research audit")
	}
	quotedError := "error: this long note is not a host failure prefix and has no page header"
	savedWithTrailer := "error: quoted from payload\nC:\\work\\a.pdf\n" + hostDownloadSavedTrailer
	for _, sample := range []string{
		page,
		"抓取失败: connection refused",
		"use_browser_cookies 失败: no session",
		"浏览器下载失败: timeout",
		"标题: error: boom",
		savedWithTrailer,
		quotedError,
	} {
		want := "success"
		if codingWebFetchResultLooksFailed(sample) {
			want = "failed"
		}
		if got := remoteCodingExecutionOutcome("web_fetch", sample); got != want {
			t.Fatalf("web_fetch outcome %q, audit wants %q for %q", got, want, sample)
		}
	}
	if remoteCodingExecutionOutcome("download_file", quotedError) != "failed" {
		t.Fatal("download_file still fails without the host save trailer")
	}
	for _, failed := range []string{
		quotedError,
		"参数解析失败: invalid character 'x' looking for beginning of value",
		"web_fetch save_path is not allowed for a read-only coding child",
		"tool web_fetch is unavailable for a read-only repository inquiry",
	} {
		if !codingWebFetchResultLooksFailed(failed) {
			t.Fatalf("non-page web_fetch failure must fail the audit: %q", failed)
		}
		if remoteCodingExecutionOutcome("web_fetch", failed) != "failed" {
			t.Fatalf("non-page web_fetch failure must fail the loop: %q", failed)
		}
	}
	quotedDoc := "AcmeSDK v2 official troubleshooting reference. The compiler may print error: file not found or an exception, and the log may say 下载失败. This page is the declared source. Configure Client.Timeout if the request failed."
	if codingWebFetchResultLooksFailed(quotedDoc) {
		t.Fatal("a document that quotes error text must not be classified as a failed fetch")
	}
	opening := "Timeouts and request failed handling are documented for this client. Anonymous access is not allowed for some routes, and a sample parser prints 参数解析失败 when the JSON is truncated. This page is the declared source."
	if codingWebFetchResultLooksFailed(opening) {
		t.Fatal("a document that opens by discussing timeouts must not be classified as a failed fetch")
	}
	for _, doc := range []string{
		"抓取失败时如何重试。本文说明官方客户端的超时和重试，正文足够长，不是工具返回的失败信封。",
		"错误码说明：连接被拒绝时先看状态码，再决定是否重试。本文是声明的来源。",
		"Anonymous access is not allowed for guests on this public endpoint. The rest of the page documents the official contract.",
		"The replica is unavailable for a read-only client. This page documents the deployment and is long enough to audit.",
		"Error: X917 means the socket closed.\n\nThis page is the official troubleshooting reference and is long enough to audit.",
		"Error: X917 means the socket closed. This page is the official troubleshooting reference and is long enough to audit.",
	} {
		if codingWebFetchResultLooksFailed(doc) {
			t.Fatalf("document must not be classified as a failed fetch: %q", doc)
		}
	}
	for _, refusal := range []string{
		"错误：参数不对。请重试",
		"error: connection refused. dial tcp 10.0.0.1:443",
		"错误: connection refused\n\ngoroutine 1 [running]:\nmain.fetch()",
		"错误: connection refused\n\ndial tcp 10.0.0.1:443: connect: connection refused after the handshake waited too long",
	} {
		if !codingWebFetchResultLooksFailed(refusal) {
			t.Fatalf("tool refusal must stay a failed fetch: %q", refusal)
		}
	}
	if !codingWebFetchResultLooksFailed("抓取失败: timeout") {
		t.Fatal("host fetch failure must stay a failed fetch")
	}
	if !codingWebFetchResultLooksFailed("use_browser_cookies 失败: no session") {
		t.Fatal("cookie refusal must not count as a successful fetch")
	}
	if !codingWebFetchResultLooksFailed("via_browser 需要配合 save_path 使用（浏览器下载直接落盘，不返回文本）") {
		t.Fatal("via_browser refusal must not count as a successful fetch")
	}
	for _, doc := range []string{
		"缺少 url 参数时请求不会发出。本文说明 web_fetch 的调用方式，并给出足够长的示例。",
		"缺少 save_path 时先在工作目录下准备相对路径。本文说明下载规则，正文足够长。",
		"web_fetch unavailable in offline mode is described here so the client can fall back to a cached copy of the page.",
		"via_browser 需要配合 save_path 使用。本文说明浏览器下载的限制，正文足够长。",
	} {
		if codingWebFetchResultLooksFailed(doc) {
			t.Fatalf("tool-constraint documentation must not be classified as a failed fetch: %q", doc)
		}
	}
	if remoteCodingExecutionOutcome("web_fetch", "标题: error: boom") != "failed" {
		t.Fatal("a title line without the fetch header must not count as a retrieved page")
	}
	saved := savedWithTrailer
	if remoteDownloadFileOutcome(saved) != "success" {
		t.Fatal("saved download must succeed even when the payload quotes error:")
	}
	if remoteCodingExecutionOutcome("web_fetch", saved) != "success" {
		t.Fatal("web_fetch save trailer must succeed even when the payload quotes error:")
	}
	if remoteDownloadFileOutcome("ok") != "failed" {
		t.Fatal("download result without a save marker must fail")
	}
}

func TestFilterRemoteCodingToolsForRole(t *testing.T) {
	tools := remoteCodingToolDefinitions()
	tools = append(tools, buildRemoteSpawnCodingAgentToolDefinition())
	tools = append(tools, buildRemoteCodingFullEnvExtraToolDefinitions()...)
	var spawn map[string]interface{}
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if name == codingSubAgentSpawnToolName {
			spawn = tool
			break
		}
	}
	if spawn == nil {
		t.Fatal("remote tools missing spawn_coding_agent")
	}
	spawnFn, _ := spawn["function"].(map[string]interface{})
	spawnDesc, _ := spawnFn["description"].(string)
	if !strings.Contains(spawnDesc, "ssh_edit_file") || strings.Contains(spawnDesc, "Two local workers") {
		t.Fatalf("remote spawn description = %q", spawnDesc)
	}
	params, _ := spawnFn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	agents, _ := props["agents"].(map[string]interface{})
	agentsDesc, _ := agents["description"].(string)
	if strings.Contains(agentsDesc, "local workers") || !strings.Contains(agentsDesc, "ssh_edit_file") {
		t.Fatalf("remote spawn agents description = %q", agentsDesc)
	}
	localSpawn := buildSpawnCodingAgentToolDefinition()
	localFn, _ := localSpawn["function"].(map[string]interface{})
	localDesc, _ := localFn["description"].(string)
	if !strings.Contains(localDesc, "Two local workers") {
		t.Fatalf("local spawn description changed: %q", localDesc)
	}
	var downloadDesc, downloadSave, fetchSave string
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		params, _ := fn["parameters"].(map[string]interface{})
		props, _ := params["properties"].(map[string]interface{})
		save, _ := props["save_path"].(map[string]interface{})
		saveDesc, _ := save["description"].(string)
		switch name {
		case "download_file":
			downloadDesc, _ = fn["description"].(string)
			downloadSave = saveDesc
		case "web_fetch":
			fetchSave = saveDesc
		}
	}
	if !strings.Contains(downloadDesc, "local host") || !strings.Contains(downloadDesc, "ssh_write_file") || strings.Contains(downloadDesc, "working directory") {
		t.Fatalf("remote download_file description = %q", downloadDesc)
	}
	if strings.Contains(downloadSave, "workdir") || strings.Contains(fetchSave, "workdir") || !strings.Contains(downloadSave, "local host") || !strings.Contains(fetchSave, "local host") {
		t.Fatalf("remote save_path descriptions = download %q fetch %q", downloadSave, fetchSave)
	}
	localDownload := ""
	for _, tool := range buildCodingFullEnvExtraToolDefinitions() {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if name != "download_file" {
			continue
		}
		params, _ := fn["parameters"].(map[string]interface{})
		props, _ := params["properties"].(map[string]interface{})
		save, _ := props["save_path"].(map[string]interface{})
		localDownload, _ = save["description"].(string)
	}
	if !strings.Contains(localDownload, "workdir") {
		t.Fatalf("local download_file save_path changed: %q", localDownload)
	}

	root := &RemoteCodingSubAgent{nestDepth: 0}
	filtered := filterRemoteCodingToolsForRole(tools, root)
	names := map[string]bool{}
	for _, d := range filtered {
		fn, _ := d["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}
	if !names[codingSubAgentSpawnToolName] || !names["ssh_write_file"] {
		t.Fatalf("root tools missing spawn/write: %#v", names)
	}

	ex := &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleExplorer}
	filtered = filterRemoteCodingToolsForRole(tools, ex)
	names = map[string]bool{}
	for _, d := range filtered {
		fn, _ := d["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}
	if names["ssh_write_file"] || names["ssh_edit_file"] || names[codingSubAgentSpawnToolName] {
		t.Fatalf("explorer leaked write/spawn: %#v", names)
	}
	if !names["ssh_read_file"] {
		t.Fatalf("explorer missing read tools: %#v", names)
	}
	if names["ssh_bash"] {
		t.Fatalf("explorer leaked ssh_bash: %#v", names)
	}
}

func TestRemoteBuildToolsIncludesSpawnOnRoot(t *testing.T) {
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{nestDepth: 0, projectDir: "/tmp/app", workDir: "/tmp/app", generalKB: store},
		task:  "implement feature",
	}
	tools := cb.BuildTools("implement feature")
	names := map[string]bool{}
	for _, d := range tools {
		fn, _ := d["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}
	if !names[codingSubAgentSpawnToolName] {
		t.Fatalf("remote root BuildTools missing %s; got %#v", codingSubAgentSpawnToolName, names)
	}
	if !names["ssh_bash"] || !names["web_search"] {
		t.Fatalf("expected baseline remote tools, got %#v", names)
	}
	if !names["knowledge_image_search"] {
		t.Fatalf("remote root BuildTools missing knowledge_image_search; got %#v", names)
	}
}

func TestRemoteInquiryToolSurfaceDoesNotExposeLocalExtensions(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{readOnlyInquiry: true, projectDir: "/tmp/app", workDir: "/tmp/app"},
		task:  "which file implements authentication?",
	}
	prompt := cb.BuildSystemPrompt(cb.task, true)
	if strings.Contains(prompt, "manage_skill") || strings.Contains(prompt, "call_mcp_tool") {
		t.Fatalf("repository inquiry must not advertise unavailable local extensions: %q", prompt)
	}
	for _, name := range codingSubAgentToolDefinitionNamesForTest(cb.BuildTools(cb.task)) {
		if name == "ssh_write_file" || name == "ssh_edit_file" || name == "manage_skill" || name == "call_mcp_tool" || name == "todo_write" {
			t.Fatalf("repository inquiry exposed a mutating or unavailable tool: %s", name)
		}
	}
}

func TestRemoteFocusedToolSurfacesIncludeConfiguredKnowledgeSearch(t *testing.T) {
	codingStore, err := knowledge.NewCodingKnowledgeStore(filepath.Join(t.TempDir(), "coding_knowledge.db"))
	if err != nil {
		t.Fatalf("NewCodingKnowledgeStore: %v", err)
	}
	t.Cleanup(func() { _ = codingStore.Close() })
	generalStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = generalStore.Close() })

	for _, mode := range []struct {
		name  string
		agent *RemoteCodingSubAgent
	}{
		{
			name:  "inquiry",
			agent: &RemoteCodingSubAgent{readOnlyInquiry: true, projectDir: "/tmp/app", workDir: "/tmp/app", codingKB: codingStore, generalKB: generalStore},
		},
		{
			name:  "operational",
			agent: &RemoteCodingSubAgent{operationalRequest: true, projectDir: "/tmp/app", workDir: "/tmp/app", codingKB: codingStore, generalKB: generalStore},
		},
	} {
		t.Run(mode.name, func(t *testing.T) {
			cb := &remoteCodingCallbacks{agent: mode.agent, task: "inspect the configured project"}
			names := map[string]bool{}
			for _, name := range codingSubAgentToolDefinitionNamesForTest(cb.BuildTools(cb.task)) {
				names[name] = true
			}
			for _, want := range []string{"coding_knowledge_search", "knowledge_search", "knowledge_image_search"} {
				if !names[want] {
					t.Fatalf("configured knowledge tool %q missing from %s surface: %#v", want, mode.name, names)
				}
			}
		})
	}
}

func TestRemoteRuntimeSpawnRejectsClosedParentAttempt(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	now := time.Now().UTC()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "remote-spawn-closed", ProjectRef: "/srv/repo", Mode: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: "/srv/repo", Mode: "remote"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CancelTask(task.TaskID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{runtimeStore: store, runtimeAttempt: attempt}}
	result := cb.executeSpawnRemoteCodingAgent(map[string]interface{}{"role": "explorer", "task": "inspect"})
	if !strings.Contains(result, "runtime parent attempt is no longer running") {
		t.Fatalf("closed Runtime attempt must reject spawn, got %q", result)
	}
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, result) != "failed" {
		t.Fatalf("closed Runtime attempt must be a failed tool outcome, got %q from %q", remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, result), result)
	}
}

func TestExecuteRemoteWorkerSpawnRejected(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{nestDepth: 0, role: codingRoleWorker}}
	got := cb.executeSpawnRemoteCodingAgent(map[string]interface{}{
		"role":  "worker",
		"task":  "implement helper",
		"files": []interface{}{"src/helper.go"},
	})
	if !strings.Contains(got, "remote worker requires an active SSH session and project directory") {
		t.Fatalf("remote worker without session = %q", got)
	}
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, got) != "failed" {
		t.Fatalf("remote worker without session must be a failed tool outcome, got %q from %q", remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, got), got)
	}
	got = cb.executeSpawnRemoteCodingAgent(map[string]interface{}{
		"role": "worker",
		"task": "implement helper",
	})
	if !strings.Contains(got, "worker requires files") {
		t.Fatalf("remote worker without files = %q", got)
	}
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, got) != "failed" {
		t.Fatalf("remote worker without files must be a failed tool outcome, got %q from %q", remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, got), got)
	}
}

func TestRemoteSpawnAdmissionFailuresAreFailedToolOutcomes(t *testing.T) {
	closed := "spawn_coding_agent: runtime parent attempt is no longer running"
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, codingSpawnRemoteFailure(closed)) != "failed" {
		t.Fatal("closed parent attempt must classify as a failed remote tool")
	}
	success := "spawn_coding_agent(remote) completed: 1 agent(s) mode=sequential\n\n### agent[0] role=worker\nsummary:\nfixed the error: off-by-one\npassed=1 failed=0\n"
	if remoteCodingToolOutcome(success) != "failed" {
		t.Fatal("sanity: SSH-log heuristic still trips on error: in a child summary")
	}
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, success) != "success" {
		t.Fatal("successful remote spawn report must stay a successful tool outcome even if a child summary mentions error:")
	}
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, "spawn_coding_agent admitted read-only child task(s); parent attempt released its lease:") != "success" {
		t.Fatal("ledger admission report must stay a successful tool outcome")
	}
}

func TestExecuteLedgerReadOnlyRemoteSpawnRejectsWorker(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{
		runtimeStore:   codingruntime.NewMemoryStore(),
		runtimeAttempt: &codingruntime.Attempt{AttemptID: "att-1"},
	}}
	got := cb.executeLedgerReadOnlyRemoteSpawn([]codingSpawnSpec{{
		Role: codingRoleWorker, Task: "write", Files: []string{"src/helper.go"},
	}})
	if !strings.Contains(got, "inspection ledger admission cannot run worker children") {
		t.Fatalf("ledger remote spawn = %q", got)
	}
}

func TestIsolatedRemoteWorkerShouldKeepIsolate(t *testing.T) {
	if !isolatedRemoteWorkerShouldKeepIsolate(nil, nil, &RemoteCodingSubAgentResult{FilesModified: []string{"a.go"}}) {
		t.Fatal("audit files should keep the isolate")
	}
	if !isolatedRemoteWorkerShouldKeepIsolate(&remoteCodingIsolate{created: true, IsolateDir: "/tmp/maclaw-wt-1"}, nil, nil) {
		t.Fatal("unprobed isolate must be kept")
	}
}

func TestRemapRemoteIsolatePaths(t *testing.T) {
	got := remapRemoteIsolatePaths(
		[]string{"/tmp/maclaw-wt-1/src/a.go", "/tmp/maclaw-wt-1", "/home/u/app/keep.go"},
		"/tmp/maclaw-wt-1",
		"/home/u/app",
	)
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "/home/u/app/src/a.go") || !strings.Contains(joined, "/home/u/app") {
		t.Fatalf("remap = %#v", got)
	}
	if strings.Contains(joined, "/tmp/maclaw-wt-1/src") {
		t.Fatalf("isolate prefix leaked: %#v", got)
	}
	escaped := remapRemoteIsolatePaths(
		[]string{"/tmp/maclaw-wt-1/../etc/passwd"},
		"/tmp/maclaw-wt-1",
		"/home/u/app",
	)
	for _, p := range escaped {
		if strings.Contains(p, "/home/u/app") {
			t.Fatalf("traversal must not remap onto the source tree: %#v", escaped)
		}
	}
}

func TestValidateRemoteIsolatedWorkerSpecs(t *testing.T) {
	if err := validateRemoteIsolatedWorkerSpecs("/home/u/app", []codingSpawnSpec{{
		Role: codingRoleWorker, Task: "w", Files: []string{"./src/a.go"},
	}}); err == nil {
		t.Fatal("dot-relative remote write-set must fail at admission")
	}
	if err := validateRemoteIsolatedWorkerSpecs("/home/u/app", []codingSpawnSpec{{
		Role: codingRoleWorker, Task: "w", Files: []string{"src/a.go"},
	}}); err != nil {
		t.Fatalf("plain relative path should pass: %v", err)
	}
}

func TestCodingSpawnBatchHeaderMarksFailure(t *testing.T) {
	ok := codingSpawnBatchHeader("spawn_coding_agent", 2, 2, 0, "parallel")
	if !strings.Contains(ok, "completed") || strings.Contains(ok, "错误") {
		t.Fatalf("success header=%q", ok)
	}
	bad := codingSpawnBatchHeader("spawn_coding_agent", 2, 1, 1, "sequential")
	if !strings.Contains(bad, "错误") || strings.Contains(bad, "completed") {
		t.Fatalf("failure header=%q", bad)
	}
}

func TestRemoteIsolatedWorkerUsesIsolateProjectDir(t *testing.T) {
	parent := &RemoteCodingSubAgent{projectDir: "/home/u/app", workDir: "/home/u/work"}
	workDir, projectDir, err := codingSpawnRemoteChildDirs(parent, codingSpawnSpec{
		Role: codingRoleWorker, Task: "write", Files: []string{"src/a.go"}, projectPath: "/tmp/maclaw-wt-1",
	})
	if err != nil || projectDir != "/tmp/maclaw-wt-1" || workDir != "/tmp/maclaw-wt-1" {
		t.Fatalf("worker must bind isolate dirs, got project=%s work=%s err=%v", projectDir, workDir, err)
	}
	if _, _, err := codingSpawnRemoteChildDirs(parent, codingSpawnSpec{
		Role: codingRoleWorker, Task: "write", Files: []string{"src/a.go"},
	}); err == nil {
		t.Fatal("worker without isolate path must fail closed")
	}
	if _, _, err := codingSpawnRemoteChildDirs(parent, codingSpawnSpec{
		Role: codingRoleWorker, Task: "write", Files: []string{"src/a.go"}, projectPath: "/home/u/app",
	}); err == nil {
		t.Fatal("worker isolate equal to primary must fail closed")
	}
	workDir, projectDir, err = codingSpawnRemoteChildDirs(parent, codingSpawnSpec{
		Role: codingRoleExplorer, Task: "inspect", projectPath: "/tmp/maclaw-wt-1",
	})
	if err != nil || projectDir != "/home/u/app" || workDir != "/home/u/work" {
		t.Fatalf("explorer must stay on primary, got project=%s work=%s err=%v", projectDir, workDir, err)
	}
}

func TestRemoteReadOnlyChildDoesNotInheritPreviewLifecycle(t *testing.T) {
	parent := &RemoteCodingSubAgent{sourcePreviewEnabled: true, sourcePreviewSessionID: "root-preview"}
	child := parent.newReadOnlyNestedRemoteCodingAgent(codingSpawnSpec{Role: codingRoleExplorer, Task: "inspect"}, nil)
	if child == nil || child.sourcePreviewEnabled || child.sourcePreviewSessionID != "" {
		t.Fatalf("read-only child must not inherit root preview lifecycle: %#v", child)
	}
	ownerParent := &RemoteCodingSubAgent{permissionOwnerID: "task-tab-user"}
	ownerChild := ownerParent.newReadOnlyNestedRemoteCodingAgent(codingSpawnSpec{Role: codingRoleExplorer, Task: "inspect"}, nil)
	if ownerChild.permissionOwnerID != "task-tab-user" {
		t.Fatalf("remote read-only child owner=%q", ownerChild.permissionOwnerID)
	}
	if ownerChild.loopCtx != nil && strings.TrimSpace(ownerChild.loopCtx.UserID) == "task-tab-user" {
		t.Fatal("remote read-only child loop must stay separate from the task tab owner")
	}
}

func TestRemoteOperationalTaskUsesFocusedNonMutatingSurface(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{operationalRequest: true, projectDir: "/tmp/app", workDir: "/tmp/app"},
		task:  "run the app",
	}
	prompt := cb.BuildSystemPrompt(cb.task, true)
	// Remote coding has no Horizon exception: it always runs on the
	// uncorrelated static compatibility surface, so the role-specific
	// operational prompt is unreachable until the transport-owned correlation
	// path binds (see usesUncorrelatedStaticCompatibilityModelSurface).
	if !strings.Contains(prompt, "compatibility mode") {
		t.Fatalf("expected the compatibility-mode prompt, got %.200q", prompt)
	}
	if strings.Contains(prompt, "Remote operational task") {
		t.Fatal("operational prompt header must not appear on the compatibility surface")
	}
	for _, name := range codingSubAgentToolDefinitionNamesForTest(cb.BuildTools(cb.task)) {
		if name == "ssh_write_file" || name == "ssh_edit_file" || name == codingSubAgentSpawnToolName || name == "todo_write" {
			t.Fatalf("operational request exposed a mutating or planning tool: %s", name)
		}
	}
	if !isRemoteCodingOperationalTool("ssh_bash") || isRemoteCodingOperationalTool("ssh_write_file") {
		t.Fatal("unexpected remote operational tool policy")
	}
	if rejectCodingOperationalShellCommand("npm run build") != "" {
		t.Fatal("build should stay available for an operational request")
	}
	if rejectCodingOperationalShellCommand("npm install left-pad") == "" {
		t.Fatal("dependency installation must not be treated as a run/build request")
	}
	for _, command := range []string{
		"go generate ./...",
		"cargo fix",
		"cargo clippy --fix",
		"prettier --write src/app.ts",
		"protoc --go_out=. api.proto",
		"python manage.py makemigrations",
		"sh -c 'echo changed > source.go'",
		"python -c 'from pathlib import Path; Path(\"source.py\").write_text(\"changed\")'",
		"node -e 'require(\"fs\").writeFileSync(\"source.js\", \"changed\")'",
		"npm run build $(touch source.go)",
	} {
		if rejectCodingOperationalShellCommand(command) == "" {
			t.Fatalf("source-mutating operational command was allowed: %q", command)
		}
	}
}

func TestRemoteOriginalRequestKindSurvivesExpandedPlanStep(t *testing.T) {
	// A plan-expanded prompt may be long and contain implementation words, but
	// it must not overrule the user's original direct run/build request.
	readOnly, operational := resolveRemoteCodingRequestFlags(codingRequestOperational, "[Plan step T1/3] implement and verify everything")
	if readOnly || !operational {
		t.Fatal("original operational kind must survive plan-step expansion")
	}
}

func TestResolveRemoteCodingRequestFlagsWorkspaceClearIsImplementation(t *testing.T) {
	readOnly, operational := resolveRemoteCodingRequestFlags(codingRequestOperational, "清空当前目录")
	if readOnly || operational {
		t.Fatalf("workspace clear must not stay operational remotely, readOnly=%v operational=%v", readOnly, operational)
	}
}

func TestRemoteSourcePreviewOnlyEnabledForImplementation(t *testing.T) {
	if !remoteCodingShouldEnableSourcePreview(codingRequestImplementation) {
		t.Fatal("implementation should enable remote source preview")
	}
	for _, kind := range []codingRequestKind{codingRequestInquiry, codingRequestOperational, ""} {
		if remoteCodingShouldEnableSourcePreview(kind) {
			t.Fatalf("request kind %q must not enable remote source preview", kind)
		}
	}
}

func TestRemoteOperationalQualityRequiresLaunchOrBuildEvidence(t *testing.T) {
	passed, summary, issues := summarizeRemoteOperationalQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{{
		Command: "npm run build", Succeeded: true,
	}}, 1)
	if passed != codingSubAgentQualityPassed || issues != 0 || !strings.Contains(summary, "launch/build command evidence") {
		t.Fatalf("build evidence=%q %q %d", passed, summary, issues)
	}
	passed, summary, issues = summarizeRemoteOperationalQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{{
		Command: "ls", Succeeded: true,
	}}, 1)
	if passed != codingSubAgentQualityFailed || issues != 1 || !strings.Contains(summary, "no launch/build command") {
		t.Fatalf("listing-only evidence=%q %q %d", passed, summary, issues)
	}
}

func TestRemoteBuildToolsExplorerFiltersWrites(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleExplorer, projectDir: "/tmp/app", workDir: "/tmp/app"},
		task:  "map code",
	}
	tools := cb.BuildTools("map auth")
	names := map[string]bool{}
	for _, d := range tools {
		fn, _ := d["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}
	if names["ssh_write_file"] || names["ssh_edit_file"] || names[codingSubAgentSpawnToolName] {
		t.Fatalf("explorer tools leaked writes/spawn: %#v", names)
	}
	if !names["ssh_read_file"] {
		t.Fatalf("explorer missing read: %#v", names)
	}
}

func TestExecuteSpawnRemoteBlockedAtDepth(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleWorker},
	}
	res := cb.executeSpawnRemoteCodingAgent(map[string]interface{}{
		"role": "explorer",
		"task": "should fail",
	})
	if !strings.Contains(res, "unavailable") {
		t.Fatalf("text=%s", res)
	}
}

func TestRemoteNestedSystemPromptNoRootSpawnSalesPitch(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{
			nestDepth:  1,
			role:       codingRoleWorker,
			projectDir: "/home/u/app",
			workDir:    "/home/u/app",
		},
		task:        "fix bug",
		taskContext: "parent ctx",
	}
	prompt := cb.BuildSystemPrompt("fix bug", true)
	// Compatibility surface only (see TestRemoteOperationalTask...): the
	// nested role header is unreachable until correlation-bound transport
	// binding restores the rich prompt path.
	if !strings.Contains(prompt, "compatibility mode") {
		t.Fatalf("expected the compatibility-mode prompt, got %.200q", prompt)
	}
	if strings.Contains(prompt, "Nested remote coding subagent") {
		t.Fatal("nested role header must not appear on the compatibility surface")
	}
	// Nested worker must not be told to spawn further.
	if strings.Contains(prompt, "用 spawn_coding_agent 派生子代理") {
		t.Fatal("nested prompt should not sell root spawn workflow")
	}
}

func TestRemoteExplorerSystemPromptIsInspectionOnly(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{
			nestDepth:  1,
			role:       codingRoleExplorer,
			projectDir: "/home/u/app",
			workDir:    "/home/u/app",
		},
		task:        "map auth",
		taskContext: "focus on jwt",
	}
	prompt := cb.BuildSystemPrompt("map auth", true)
	// Compatibility surface only (see TestRemoteOperationalTask...): the
	// inspection-role prompt is unreachable for now; the compatibility prompt
	// is read-only by posture and still carries the task context.
	if !strings.Contains(prompt, "compatibility mode") {
		t.Fatalf("expected the compatibility-mode prompt, got %.200q", prompt)
	}
	if strings.Contains(prompt, "Remote Inspection SubAgent") {
		t.Fatal("inspection header must not appear on the compatibility surface")
	}
	if strings.Contains(prompt, "ssh_write_file") {
		t.Fatal("explorer prompt must not advertise write tools")
	}
	if !strings.Contains(prompt, "focus on jwt") {
		t.Fatal("expected task context")
	}
}

func TestApplyRemoteInspectionRoleOutcome(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleExplorer},
	}
	// No inspection evidence → fail.
	got := cb.applyRemoteInspectionRoleOutcome(&RemoteCodingSubAgentResult{
		Status: "success", Summary: "hello", ToolCalls: 2,
	}, nil, nil, nil, codingRoleExplorer)
	if got.Status != "failed" {
		t.Fatalf("expected fail without inspection, got %+v", got)
	}
	// With reads → pass.
	got = cb.applyRemoteInspectionRoleOutcome(&RemoteCodingSubAgentResult{
		Status: "success", Summary: "found auth", ToolCalls: 3,
	}, []string{"/home/u/app/a.go"}, nil, nil, codingRoleExplorer)
	if got.Status != "success" {
		t.Fatalf("expected success with read evidence, got %+v", got)
	}
	// The inspection note moved to the quality channel (94489f8b) so the
	// user-facing Summary stays clean; assert it there.
	if !strings.Contains(got.QualitySummary, "inspection-only") {
		t.Fatalf("expected inspection note in quality summary, got %q", got.QualitySummary)
	}
}
