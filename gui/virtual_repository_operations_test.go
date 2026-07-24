package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSanitizeVirtualRepositoryOperationResultTextRedactsAndBounds(t *testing.T) {
	secret := "operation-secret-value"
	value := "https://alice:" + secret + "@example.com/repo?access_token=" + secret + " password=" + secret + "\n" + strings.Repeat("界", virtualRepositoryOperationResultTextMaxBytes)
	sanitized := sanitizeVirtualRepositoryOperationResultText(value)
	if strings.Contains(sanitized, secret) || strings.Contains(sanitized, "alice:") {
		t.Fatalf("operation result leaked a credential: %q", sanitized[:min(len(sanitized), 256)])
	}
	if len(sanitized) > virtualRepositoryOperationResultTextMaxBytes {
		t.Fatalf("sanitized result has %d bytes, limit is %d", len(sanitized), virtualRepositoryOperationResultTextMaxBytes)
	}
	if !strings.HasSuffix(sanitized, virtualRepositoryOperationResultTruncatedMarker) {
		t.Fatalf("truncated result is missing marker")
	}
	if !utf8.ValidString(sanitized) {
		t.Fatal("sanitized result is not valid UTF-8")
	}
	encoded, err := json.Marshal(VirtualRepositoryOperationResult{Items: []VirtualRepositoryOperationItemResult{{Error: sanitized, Output: sanitized}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatal("marshaled operation result leaked a credential")
	}
}

func TestSanitizeVirtualRepositoryOperationResultTextRepairsInvalidUTF8(t *testing.T) {
	sanitized := sanitizeVirtualRepositoryOperationResultText(string([]byte{'o', 'k', 0xff, 'x'}))
	if !utf8.ValidString(sanitized) || !strings.Contains(sanitized, "\uFFFD") {
		t.Fatalf("invalid UTF-8 was not repaired: %q", sanitized)
	}
}

func TestVirtualRepositoryOperationJobTextAndEventBudgets(t *testing.T) {
	if !shouldEmitVirtualRepositoryJobProgress(1, 10000) || !shouldEmitVirtualRepositoryJobProgress(100, 10000) || !shouldEmitVirtualRepositoryJobProgress(10000, 10000) {
		t.Fatal("required progress milestones must be emitted")
	}
	if shouldEmitVirtualRepositoryJobProgress(11, 10000) || shouldEmitVirtualRepositoryJobProgress(101, 10000) {
		t.Fatal("large jobs should not emit a full result for every item")
	}
	remaining := virtualRepositoryOperationJobTextMaxBytes
	retained := 0
	oversized := strings.Repeat("x", virtualRepositoryOperationResultTextMaxBytes+1)
	for remaining > 0 {
		value := sanitizeVirtualRepositoryOperationResultTextLimit(oversized, min(remaining, virtualRepositoryOperationResultTextMaxBytes))
		retained += len(value)
		remaining -= len(value)
	}
	if retained > virtualRepositoryOperationJobTextMaxBytes || remaining != 0 {
		t.Fatalf("retained=%d remaining=%d", retained, remaining)
	}
}

func TestCollectVirtualRepositoryTargetsScopesSubtreeAndSkipsLocal(t *testing.T) {
	repo := &VirtualRepository{Nodes: []VirtualRepositoryNode{
		{ID: "a", Name: "A", Order: 10},
		{ID: "git", ParentID: "a", Name: "Git", Order: 10, Repository: &VirtualRepositoryBinding{Kind: "git", Enabled: true}},
		{ID: "local", ParentID: "a", Name: "Build", Order: 20, Repository: &VirtualRepositoryBinding{Kind: "local", Enabled: true}},
		{ID: "svn", Name: "SVN", Order: 20, Repository: &VirtualRepositoryBinding{Kind: "svn", Enabled: true}},
	}}
	targets, skipped, err := collectVirtualRepositoryTargets(repo, VirtualRepositoryOperationRequest{NodeID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID != "git" || skipped != 1 {
		t.Fatalf("targets=%#v skipped=%d", targets, skipped)
	}
}

func TestCollectVirtualRepositoryTargetsRejectsUnknownNode(t *testing.T) {
	repo := &VirtualRepository{Nodes: []VirtualRepositoryNode{{ID: "git", Name: "Git", Repository: &VirtualRepositoryBinding{Kind: "git", Enabled: true}}}}
	if _, _, err := collectVirtualRepositoryTargets(repo, VirtualRepositoryOperationRequest{NodeID: "missing"}); err == nil {
		t.Fatal("unknown target node should fail instead of selecting nothing")
	}
}

func TestParseVirtualRepositoryOperationRequiresCommitMessage(t *testing.T) {
	if _, err := parseVirtualRepositoryOperationRequest(`{"action":"commit"}`); err == nil {
		t.Fatal("commit without message should fail")
	}
	if req, err := parseVirtualRepositoryOperationRequest(`{"action":"revert"}`); err != nil || req.Action != "revert" {
		t.Fatalf("revert parse: %#v %v", req, err)
	}
	if req, err := parseVirtualRepositoryOperationRequest(`{"action":"sync"}`); err != nil || req.Action != "sync" {
		t.Fatalf("sync parse: %#v %v", req, err)
	}
}

func TestParseVirtualRepositoryOperationBoundsCommitMessage(t *testing.T) {
	tooLong, _ := json.Marshal(VirtualRepositoryOperationRequest{Action: "commit", Message: strings.Repeat("x", virtualRepositoryFieldMaxLength+1)})
	if _, err := parseVirtualRepositoryOperationRequest(string(tooLong)); err == nil {
		t.Fatal("oversized commit message should fail")
	}
	withNUL, _ := json.Marshal(VirtualRepositoryOperationRequest{Action: "commit", Message: "subject\x00suffix"})
	if _, err := parseVirtualRepositoryOperationRequest(string(withNUL)); err == nil {
		t.Fatal("commit message containing NUL should fail")
	}
}

func TestParseVirtualRepositoryOperationRejectsInvalidIDsAndExcessNodes(t *testing.T) {
	invalidID, _ := json.Marshal(VirtualRepositoryOperationRequest{Action: "push", RepositoryID: "repo\nother"})
	if _, err := parseVirtualRepositoryOperationRequest(string(invalidID)); err == nil || !strings.Contains(err.Error(), "invalid id") {
		t.Fatalf("invalid id error = %v", err)
	}
	many := make([]string, 10001)
	for i := range many {
		many[i] = "node"
	}
	tooMany, _ := json.Marshal(VirtualRepositoryOperationRequest{Action: "push", NodeIDs: many})
	if _, err := parseVirtualRepositoryOperationRequest(string(tooMany)); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("too many node ids error = %v", err)
	}
}

func TestCollectVirtualRepositoryTargetsHandlesDeepSubtree(t *testing.T) {
	const count = 5000
	nodes := make([]VirtualRepositoryNode, count)
	for i := range nodes {
		nodes[i] = VirtualRepositoryNode{ID: fmt.Sprintf("node-%d", i), Name: fmt.Sprintf("Node %d", i)}
		if i > 0 {
			nodes[i].ParentID = nodes[i-1].ID
		}
	}
	nodes[count-1].Repository = &VirtualRepositoryBinding{Kind: "git", Enabled: true}
	targets, skipped, err := collectVirtualRepositoryTargets(&VirtualRepository{Nodes: nodes}, VirtualRepositoryOperationRequest{NodeID: nodes[0].ID})
	if err != nil || skipped != 0 || len(targets) != 1 || targets[0].ID != nodes[count-1].ID {
		t.Fatalf("targets=%d skipped=%d err=%v", len(targets), skipped, err)
	}
}

func TestSortVirtualRepositoryNodesHandlesLargeReverseOrderedInput(t *testing.T) {
	const count = virtualRepositoryNodeMaxCount
	nodes := make([]VirtualRepositoryNode, count)
	for i := range nodes {
		nodes[i] = VirtualRepositoryNode{ID: fmt.Sprintf("node-%05d", count-i), Name: fmt.Sprintf("Node %05d", count-i), Order: count - i}
	}
	sortVirtualRepositoryNodes(nodes)
	for i := 1; i < len(nodes); i++ {
		if nodes[i-1].Order >= nodes[i].Order {
			t.Fatalf("nodes are not ordered at index %d: %d >= %d", i, nodes[i-1].Order, nodes[i].Order)
		}
	}
}

func TestSortVirtualRepositoryNodesUsesDeterministicIDTieBreak(t *testing.T) {
	nodes := []VirtualRepositoryNode{{ID: "b", Name: "same", Order: 1}, {ID: "a", Name: "same", Order: 1}}
	sortVirtualRepositoryNodes(nodes)
	if nodes[0].ID != "a" || nodes[1].ID != "b" {
		t.Fatalf("unexpected tie order: %#v", nodes)
	}
}

func TestUnsafeVCSState(t *testing.T) {
	if !hasUnsafeVCSState("UU file.go", "git") {
		t.Fatal("git conflict should be unsafe")
	}
	if !hasUnsafeVCSState("C    file.txt", "svn") {
		t.Fatal("svn conflict should be unsafe")
	}
	if hasUnsafeVCSState(" M file.go", "git") {
		t.Fatal("ordinary modification should be allowed")
	}
}

func TestClassifyVirtualRepositoryOperationError(t *testing.T) {
	for _, test := range []struct{ message, want string }{
		{"nothing to commit", "nothing_to_commit"},
		{"remote rejected non-fast-forward", "push_rejected"},
		{"authentication failed", "authentication_failed"},
	} {
		if got := classifyVirtualRepositoryOperationError(errors.New(test.message)); got != test.want {
			t.Fatalf("classify %q = %q, want %q", test.message, got, test.want)
		}
	}
}

func TestClassifyVirtualRepositoryOperationErrorPreservesCommitPushStage(t *testing.T) {
	err := &virtualRepositoryOperationError{Code: "push_failed_after_commit", Err: errors.New("commit succeeded but push failed")}
	if got := classifyVirtualRepositoryOperationError(err); got != "push_failed_after_commit" {
		t.Fatalf("classification=%q", got)
	}
}

func TestGitPushRequiresConfiguredRemoteURL(t *testing.T) {
	_, err := executeGitVirtualRepositoryOperation(context.Background(), "git", t.TempDir(), VirtualRepositoryOperationRequest{Action: "push"}, nil, "", "")
	if err == nil || classifyVirtualRepositoryOperationError(err) != "remote_unavailable" {
		t.Fatalf("missing remote error = %v", err)
	}
}

func TestExecuteVirtualRepositoryOperationWithClientsCachesUnavailableClient(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir()}
	clients := virtualRepositoryVCSClients{
		"git": {Kind: "git", Error: "cached missing Git"},
	}
	_, err := app.executeVirtualRepositoryOperationWithClients(context.Background(), &VirtualRepository{RootPath: root}, VirtualRepositoryNode{ID: "git", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "repo", Enabled: true}}, VirtualRepositoryOperationRequest{Action: "push"}, clients)
	if err == nil || err.Error() != "cached missing Git" {
		t.Fatalf("cached unavailable Git error = %v", err)
	}
}

func TestOperationPreviewIncludesManifestSnapshot(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "local")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := &VirtualRepository{Name: "snapshot", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "local", Name: "Local", Repository: &VirtualRepositoryBinding{Kind: "local", RelativePath: "local", Enabled: true}}}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	raw, err := (&App{}).PreviewVirtualRepositoryOperation(`{"root_path":` + strconv.Quote(root) + `,"action":"revert"}`)
	if err != nil {
		t.Fatal(err)
	}
	var preview VirtualRepositoryOperationPreview
	if err := json.Unmarshal([]byte(raw), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.RepositoryID != repo.ID || preview.UpdatedAt.IsZero() {
		t.Fatalf("preview lacks manifest snapshot: %#v", preview)
	}
}

func TestStartOperationRejectsStaleDisplayedPreview(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "local")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := &VirtualRepository{Name: "snapshot", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "local", Name: "Local", Repository: &VirtualRepositoryBinding{Kind: "local", RelativePath: "local", Enabled: true}}}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	oldUpdatedAt := repo.UpdatedAt
	time.Sleep(time.Millisecond)
	repo.Name = "changed"
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	request := VirtualRepositoryOperationRequest{RootPath: root, Action: "revert", ExpectedRepositoryID: repo.ID, ExpectedUpdatedAt: oldUpdatedAt}
	raw, _ := json.Marshal(request)
	if _, err := (&App{}).StartVirtualRepositoryOperation(string(raw)); err == nil {
		t.Fatal("stale displayed preview should be rejected")
	}
}

func TestQueuedLocalOperationRejectsManifestChange(t *testing.T) {
	root := t.TempDir()
	repo := &VirtualRepository{Name: "queued", RootPath: root, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	queued := *repo
	time.Sleep(time.Millisecond)
	repo.Name = "changed"
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	job := &virtualRepositoryOperationJob{result: VirtualRepositoryOperationResult{JobID: "local-stale", Action: "push", Status: "running", StartedAt: time.Now().UTC()}}
	(&App{}).runVirtualRepositoryOperationJob(context.Background(), job, &queued, []VirtualRepositoryNode{{ID: "git", Name: "Git", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "git", Enabled: true}}}, VirtualRepositoryOperationRequest{Action: "push"})
	job.mu.RLock()
	defer job.mu.RUnlock()
	if len(job.result.Items) != 1 || !strings.Contains(job.result.Items[0].Error, "changed after") {
		t.Fatalf("stale queued operation result = %#v", job.result)
	}
}

func TestVirtualRepositoryOperationJobLookupAndCancel(t *testing.T) {
	job := &virtualRepositoryOperationJob{result: VirtualRepositoryOperationResult{JobID: "test-job", Status: "running"}}
	cancelled := make(chan struct{})
	var cancelCalls int
	job.cancel = func() {
		cancelCalls++
		close(cancelled)
	}
	virtualRepositoryOperationJobs.Lock()
	virtualRepositoryOperationJobs.items[job.result.JobID] = job
	virtualRepositoryOperationJobs.Unlock()
	t.Cleanup(func() {
		virtualRepositoryOperationJobs.Lock()
		delete(virtualRepositoryOperationJobs.items, job.result.JobID)
		virtualRepositoryOperationJobs.Unlock()
	})
	a := &App{}
	if _, err := a.GetVirtualRepositoryOperation(job.result.JobID); err != nil {
		t.Fatal(err)
	}
	if err := a.CancelVirtualRepositoryOperation(job.result.JobID); err != nil {
		t.Fatal(err)
	}
	if err := a.CancelVirtualRepositoryOperation(job.result.JobID); err != nil {
		t.Fatalf("repeated cancellation should be idempotent: %v", err)
	}
	if cancelCalls != 1 {
		t.Fatalf("cancel called %d times", cancelCalls)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("job was not cancelled")
	}
}

func TestCancelledOperationStaysCancelledWhenContextCancellationIsDelayed(t *testing.T) {
	root := t.TempDir()
	repo := &VirtualRepository{Name: "cancelled", RootPath: root, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	job := &virtualRepositoryOperationJob{
		cancelRequested: true,
		targetCount:     1,
		result:          VirtualRepositoryOperationResult{JobID: "delayed-cancel", Status: "running"},
	}
	(&App{}).runVirtualRepositoryOperationJob(context.Background(), job, repo, nil, VirtualRepositoryOperationRequest{Action: "push"})
	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.result.Status != "cancelled" {
		t.Fatalf("status=%q, want cancelled", job.result.Status)
	}
}

func TestJobCancellationRequestedIsSynchronized(t *testing.T) {
	job := &virtualRepositoryOperationJob{}
	if jobCancellationRequested(job) {
		t.Fatal("new job must not be cancelled")
	}
	job.mu.Lock()
	job.cancelRequested = true
	job.mu.Unlock()
	if !jobCancellationRequested(job) {
		t.Fatal("cancel request was not observed")
	}
}

func TestCancelVirtualRepositoryOperationRejectsCompletedAndInvalidJobs(t *testing.T) {
	app := &App{}
	if err := app.CancelVirtualRepositoryOperation(" "); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("empty job id error = %v", err)
	}
	job := &virtualRepositoryOperationJob{result: VirtualRepositoryOperationResult{JobID: "completed-job", Status: "success"}, cancel: func() { t.Fatal("completed job cancel was called") }}
	virtualRepositoryOperationJobs.Lock()
	virtualRepositoryOperationJobs.items[job.result.JobID] = job
	virtualRepositoryOperationJobs.Unlock()
	t.Cleanup(func() {
		virtualRepositoryOperationJobs.Lock()
		delete(virtualRepositoryOperationJobs.items, job.result.JobID)
		virtualRepositoryOperationJobs.Unlock()
	})
	if err := app.CancelVirtualRepositoryOperation(job.result.JobID); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("completed job cancel error = %v", err)
	}
}

func TestStartOperationRejectsConcurrentOperationForSameRepository(t *testing.T) {
	job := &virtualRepositoryOperationJob{result: VirtualRepositoryOperationResult{JobID: "existing-job", Status: "running"}}
	if err := registerVirtualRepositoryOperationJob("repo", job.result.JobID, job); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		virtualRepositoryOperationJobs.Lock()
		delete(virtualRepositoryOperationJobs.activeRepositories, "repo")
		delete(virtualRepositoryOperationJobs.items, job.result.JobID)
		virtualRepositoryOperationJobs.order = nil
		virtualRepositoryOperationJobs.Unlock()
	})
	second := &virtualRepositoryOperationJob{result: VirtualRepositoryOperationResult{JobID: "second-job", Status: "running"}}
	if err := registerVirtualRepositoryOperationJob("repo", second.result.JobID, second); err == nil || !strings.Contains(err.Error(), "already has a running operation") {
		t.Fatalf("expected concurrent operation rejection, got %v", err)
	}
}

func TestOperationJobHistoryEvictsCompletedJobBehindRunningOldest(t *testing.T) {
	virtualRepositoryOperationJobs.Lock()
	previousItems := virtualRepositoryOperationJobs.items
	previousOrder := virtualRepositoryOperationJobs.order
	previousActive := virtualRepositoryOperationJobs.activeRepositories
	virtualRepositoryOperationJobs.items = make(map[string]*virtualRepositoryOperationJob)
	virtualRepositoryOperationJobs.order = nil
	virtualRepositoryOperationJobs.activeRepositories = make(map[string]string)
	for i := 0; i < virtualRepositoryOperationJobLimit; i++ {
		id := fmt.Sprintf("job-%d", i)
		status := "success"
		if i == 0 {
			status = "running"
		}
		virtualRepositoryOperationJobs.items[id] = &virtualRepositoryOperationJob{result: VirtualRepositoryOperationResult{JobID: id, Status: status}}
		virtualRepositoryOperationJobs.order = append(virtualRepositoryOperationJobs.order, id)
	}
	virtualRepositoryOperationJobs.Unlock()
	t.Cleanup(func() {
		virtualRepositoryOperationJobs.Lock()
		virtualRepositoryOperationJobs.items = previousItems
		virtualRepositoryOperationJobs.order = previousOrder
		virtualRepositoryOperationJobs.activeRepositories = previousActive
		virtualRepositoryOperationJobs.Unlock()
	})

	newJob := &virtualRepositoryOperationJob{result: VirtualRepositoryOperationResult{JobID: "new-job", Status: "running"}}
	if err := registerVirtualRepositoryOperationJob("new-repo", newJob.result.JobID, newJob); err != nil {
		t.Fatal(err)
	}
	virtualRepositoryOperationJobs.Lock()
	_, runningOldestSurvived := virtualRepositoryOperationJobs.items["job-0"]
	_, completedOldestSurvived := virtualRepositoryOperationJobs.items["job-1"]
	virtualRepositoryOperationJobs.Unlock()
	if !runningOldestSurvived || completedOldestSurvived {
		t.Fatalf("unexpected eviction: running oldest=%v completed oldest=%v", runningOldestSurvived, completedOldestSurvived)
	}
}

func TestSVNPasswordFromStdinCapabilityCachesByExecutableIdentity(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "svn-capability")
	if goruntime.GOOS == "windows" {
		executable += ".cmd"
		if err := os.WriteFile(executable, []byte("@echo off\r\necho --password-from-stdin\r\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(executable, []byte("#!/bin/sh\necho --password-from-stdin\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	svnPasswordStdinCapability.Lock()
	svnPasswordStdinCapability.known = false
	svnPasswordStdinCapability.Unlock()
	if !svnSupportsPasswordFromStdin(context.Background(), executable) {
		t.Fatal("expected capability detection")
	}
	if err := os.Remove(executable); err != nil {
		t.Fatal(err)
	}
	if svnSupportsPasswordFromStdin(context.Background(), executable) {
		t.Fatal("deleted executable must not use a stale cached capability")
	}
}
