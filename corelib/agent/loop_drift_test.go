package agent

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	regexpSyntax "regexp/syntax"
)
func repeatToolCalls(name, args, result string, success, poll bool, n int) []toolCallRecord {
	tail := make([]toolCallRecord, n)
	for i := range tail {
		tail[i] = toolCallRecord{name: name, args: args, result: result, success: success, poll: poll}
	}
	return tail
}

func TestDriftFailureHintLeavesSuccessfulPollAlone(t *testing.T) {
	tail := repeatToolCalls("bash", `{"command":"status"}`, "running", true, true, 4)
	if driftFailureHint(tail) {
		t.Fatal("a successful status poll must stay quiet")
	}
}

func TestDriftFailureHintStopsIdenticalRemoteCommand(t *testing.T) {
	tail := repeatToolCalls("ssh", `{"command":"hostname"}`, "web-1\n", true, true, 4)
	if !driftFailureHint(tail) {
		t.Fatal("the same remote command and the same result must stop")
	}
	moved := repeatToolCalls("ssh", `{"command":"tail log"}`, "line-1", true, true, 3)
	moved[2].result = "line-2"
	if driftFailureHint(moved) {
		t.Fatal("a remote command whose result changed is still a live poll")
	}
}

func TestDriftFailureHintOnIdenticalFailure(t *testing.T) {
	tail := repeatToolCalls("office", `{"path":"deck.pptx"}`, "[system rejected] write failed", false, false, 4)
	if !driftFailureHint(tail) {
		t.Fatal("an identical failed window must hint")
	}
}

func TestRepeatedCommitIgnoresAPollAndASingleWrite(t *testing.T) {
	polls := repeatToolCalls("ssh", `{"command":"status"}`, "running", true, true, 3)
	if _, seen := repeatedSuccessfulCommit(polls); seen != 0 {
		t.Fatalf("polls are not commits: seen=%d", seen)
	}
	once := repeatToolCalls("office", `{"path":"deck.pptx","theme":"clean"}`, "published", true, false, 1)
	if _, seen := repeatedSuccessfulCommit(once); seen != 1 {
		t.Fatalf("one commit is the work, not a repeat: seen=%d", seen)
	}
}

func TestRepeatedCommitCountsSamePayloadDespiteKeyOrder(t *testing.T) {
	published := "Office document artifact published. The host attaches it to this chat. This is not a send."
	calls := []toolCallRecord{
		{name: "office", args: `{"path":"deck.pptx","theme":"clean"}`, result: published, success: true},
		{name: "bash", args: `{"command":"ls"}`, result: "ok", success: true, poll: true},
		{name: "office", args: `{"theme":"clean","path":"deck.pptx"}`, result: published, success: true},
	}
	id, seen := repeatedSuccessfulCommit(calls)
	if seen != 2 || id == "" {
		t.Fatalf("key order must not hide the second commit: id=%q seen=%d", id, seen)
	}
	calls = append(calls, toolCallRecord{name: "office", args: `{"path":"deck.pptx","theme":"clean"}`, result: published, success: true})
	if _, seen = repeatedSuccessfulCommit(calls); seen != 3 {
		t.Fatalf("third identical commit seen=%d", seen)
	}
}

func TestCommitPayloadLandedIgnoresKeyOrder(t *testing.T) {
	key, ok := commitPayloadKey("office", `{"path":"deck.pptx","theme":"clean"}`)
	if !ok {
		t.Fatal("office arguments must have a payload key")
	}
	landed := map[string]struct{}{key: {}}
	if !commitPayloadLanded(landed, "office", `{"theme":"clean","path":"deck.pptx"}`) {
		t.Fatal("the same payload must count as already committed")
	}
	if commitPayloadLanded(landed, "office", `{"path":"other.pptx","theme":"clean"}`) {
		t.Fatal("a different path is a new commit")
	}
	if commitPayloadLanded(nil, "ssh", `{"command":"status"}`) {
		t.Fatal("nothing has landed")
	}
}

func TestRepeatedSpilledPageCountsAsACommit(t *testing.T) {
	if !ImmutableToolResultRead("read_tool_result") || ImmutableToolResultRead("ssh") {
		t.Fatal("only the spilled-page reader is an immutable read")
	}
	page := "[tool_result_read]\noffset: 0\n"
	args := `{"id":"20261102T100255_knowledge_search_abc","limit":1,"offset":0}`
	calls := []toolCallRecord{
		{name: "read_tool_result", args: args, result: page, success: true},
		{name: "read_tool_result", args: `{"id":"other","limit":1,"offset":0}`, result: "other page", success: true},
		{name: "read_tool_result", args: args, result: page, success: true},
	}
	if _, seen := repeatedSuccessfulCommit(calls); seen != 2 {
		t.Fatalf("repeating the same spilled page must count as a commit, seen=%d", seen)
	}
	if commitLands("read_tool_result", false) {
		t.Fatal("a spilled page must stay replayable; landing it withholds the bytes")
	}
	if !commitLands("office", false) {
		t.Fatal("a real commit still lands")
	}
	if commitLands("ssh", true) {
		t.Fatal("a poll does not land")
	}
}

func TestRotatingSpilledPagesCountWhenTheBatchEndsElsewhere(t *testing.T) {
	page := func(id string) toolCallRecord {
		return toolCallRecord{
			name: "read_tool_result", args: `{"id":"` + id + `","offset":0}`,
			result: "page " + id, success: true, batch: 1,
		}
	}
	calls := []toolCallRecord{
		page("a"), page("b"), page("c"),
		page("a"), page("b"),
		{name: "bash", args: `{"command":"ls"}`, result: "ok", success: true, poll: true, batch: 1},
	}
	if _, seen := repeatedSuccessfulCommit(calls); seen != 0 {
		t.Fatalf("a poll at the end of the window is not a repeated commit, seen=%d", seen)
	}
	id, seen := mostRepeatedImmutableRead(calls, 1)
	if seen != 2 || commitIdentityName(id) != "read_tool_result" {
		t.Fatalf("rotating pages must count even though they are not last: id=%q seen=%d", id, seen)
	}
}

func TestSameSpilledPageIgnoresLimitJitter(t *testing.T) {
	calls := []toolCallRecord{
		{name: "read_tool_result", args: `{"id":"page.txt","limit":1,"offset":0}`, result: "limit_applied: 16384\npage", success: true, batch: 1},
		{name: "read_tool_result", args: `{"offset":0,"id":"page"}`, result: "page", success: true, batch: 1},
		{name: "read_tool_result", args: `{"id":"page.enc","offset":0}`, result: "page", success: true, batch: 1},
		{name: "read_tool_result", args: `{"id":"page","offset":16384}`, result: "tail", success: true, batch: 1},
	}
	if _, seen := repeatedSuccessfulCommit(calls[:3]); seen != 3 {
		t.Fatalf("limit and a .txt or .enc suffix must not split one page, seen=%d", seen)
	}
	if _, seen := mostRepeatedImmutableRead(calls[:3], 1); seen != 3 {
		t.Fatalf("the batch counter must use the same page identity, seen=%d", seen)
	}
	if _, seen := repeatedSuccessfulCommit(calls); seen != 1 {
		t.Fatalf("the next offset is a different page, seen=%d", seen)
	}
}

func TestNotedPageStaysArmedAcrossARotation(t *testing.T) {
	noted := map[string]struct{}{}
	for _, id := range []string{"a", "b", "c", "d"} {
		if repeatedCommitStops(noted, id, 2) {
			t.Fatalf("first repeat of %s must warn", id)
		}
		noted[id] = struct{}{}
	}
	if !repeatedCommitStops(noted, "a", 2) {
		t.Fatal("a page noted earlier must stop when the rotation returns to it")
	}
	if repeatedCommitStops(noted, "e", 2) {
		t.Fatal("a new page still gets one warning")
	}
	if !repeatedCommitStops(nil, "a", 3) {
		t.Fatal("three copies in one window stop on the first observation")
	}
}

func TestMixedBatchKeepsTheAdvertisedNextPage(t *testing.T) {
	noted := map[string]struct{}{
		"read_tool_result\nold\n0": {},
	}
	continued := map[string]struct{}{
		"read_tool_result\nold\n16384": {},
	}
	calls := []toolCallRecord{
		{name: "read_tool_result", args: `{"id":"old","offset":0}`, result: "page", success: true, batch: 1},
		{name: "read_tool_result", args: `{"id":"old","offset":0}`, result: "page", success: true, batch: 2},
		{name: "read_tool_result", args: `{"id":"old","offset":"16384.0"}`, result: "tail", success: true, batch: 2},
	}
	if !takeFreshReadContinuation(calls, 2, noted, continued, nil) {
		t.Fatal("the next_offset a truncated page named must keep this batch open")
	}
	if _, ok := continued["read_tool_result\nold\n16384"]; ok {
		t.Fatal("using the named page must consume it")
	}
	if takeFreshReadContinuation(calls, 2, noted, continued, nil) {
		t.Fatal("the same next page must not keep a later batch open")
	}
	invented := map[string]struct{}{
		"read_tool_result\nold\n16384": {},
	}
	fresh := []toolCallRecord{
		{name: "read_tool_result", args: `{"id":"old","offset":0}`, result: "page", success: true, batch: 2},
		{name: "read_tool_result", args: `{"id":"old","offset":1}`, result: "nope", success: true, batch: 2},
	}
	if takeFreshReadContinuation(fresh, 2, noted, invented, nil) {
		t.Fatal("an offset the truncated page did not name must end the turn")
	}
	if _, ok := invented["read_tool_result\nold\n16384"]; !ok {
		t.Fatal("a rejected offset must leave the named page in place")
	}
	rotation := []toolCallRecord{
		{name: "read_tool_result", args: `{"id":"a","offset":0}`, result: "a", success: true, batch: 1},
		{name: "read_tool_result", args: `{"id":"b","offset":0}`, result: "b", success: true, batch: 1},
		{name: "read_tool_result", args: `{"id":"a","offset":0}`, result: "a", success: true, batch: 2},
		{name: "read_tool_result", args: `{"id":"b","offset":0}`, result: "b", success: true, batch: 2},
	}
	if takeFreshReadContinuation(rotation, 2, nil, nil, nil) {
		t.Fatal("reading the same pages again is not a new page")
	}
}

func TestRereadTruncatedPageDoesNotRearmTheNextOffset(t *testing.T) {
	page := toolCallRecord{
		name: "read_tool_result", args: `{"id":"old","offset":0}`,
		result:  "[tool_result_read]\noffset: 0\ntruncated: true\nnext_offset: 16384\n\n---\npage\n--- end ---\n",
		success: true, batch: 2,
	}
	next := toolCallRecord{
		name: "read_tool_result", args: `{"id":"old","offset":16384}`,
		result: "tail", success: true, batch: 2,
	}
	spent := map[string]struct{}{}
	available := rememberReadContinuation(nil, spent, page)
	noted := map[string]struct{}{"read_tool_result\nold\n0": {}}
	calls := []toolCallRecord{page, page, next}
	if !takeFreshReadContinuation(calls, 2, noted, available, spent) {
		t.Fatal("the first delivery of the named page keeps the batch open")
	}
	available = rememberReadContinuation(available, spent, page)
	if _, ok := available["read_tool_result\nold\n16384"]; ok {
		t.Fatal("re-reading the truncated page must not put the used next page back")
	}
	later := []toolCallRecord{
		{name: "read_tool_result", args: `{"id":"old","offset":0}`, result: page.result, success: true, batch: 9},
		next,
	}
	later[1].batch = 9
	if takeFreshReadContinuation(later, 9, noted, available, spent) {
		t.Fatal("the used next page must not keep a later batch open")
	}
	further := toolCallRecord{
		name: "read_tool_result", args: `{"id":"old","offset":16384}`,
		result:  "[tool_result_read]\noffset: 16384\ntruncated: true\nnext_offset: 32768\n\n---\nmore\n--- end ---\n",
		success: true,
	}
	available = rememberReadContinuation(available, spent, further)
	if _, ok := available["read_tool_result\nold\n32768"]; !ok {
		t.Fatal("the following page must still be recorded")
	}
}

func TestAdvertisedContinuationIgnoresThePageBody(t *testing.T) {
	result := "[tool_result_read]\noffset: 0\ntruncated: true\nnext_offset: 16384\n\n---\nresume quotes next_offset: 1\ntruncated: true\n\n--- end ---\nhint: offset=1\n"
	call := toolCallRecord{name: "read_tool_result", args: `{"id":"old","offset":0}`, result: result, success: true}
	id, ok := advertisedReadContinuation(call)
	if !ok || id != "read_tool_result\nold\n16384" {
		t.Fatalf("header next_offset = %q ok=%v", id, ok)
	}
	quoted := toolCallRecord{name: "read_tool_result", args: `{"id":"old","offset":0}`, result: "next_offset: 1\ntruncated: true\n", success: true}
	if _, ok := advertisedReadContinuation(quoted); ok {
		t.Fatal("a result without the page fence must not name a next page")
	}
	same := toolCallRecord{
		name: "read_tool_result", args: `{"id":"old","offset":0}`,
		result: "[tool_result_read]\ntruncated: true\nnext_offset: 0\n\n---\npage\n--- end ---\n", success: true,
	}
	if _, ok := advertisedReadContinuation(same); ok {
		t.Fatal("a next offset that does not move forward must not be recorded")
	}
	failed := call
	failed.success = false
	if _, ok := advertisedReadContinuation(failed); ok {
		t.Fatal("a failed read must not name a next page")
	}
}

func TestReadsPastTheEndAreTheSamePage(t *testing.T) {
	header := "[tool_result_read]\nid: old\noffset: 18732\nreturned_bytes: 0\ntotal_bytes: 18732\ntruncated: false\n\n---\n\n--- end ---\nhint: truncated is false.\n"
	one := toolCallRecord{name: "read_tool_result", args: `{"id":"old","offset":999999}`, result: header, success: true}
	two := toolCallRecord{name: "read_tool_result", args: `{"id":"old","offset":1000000}`, result: header, success: true}
	idOne, okOne := spilledPageIdentity(one)
	idTwo, okTwo := spilledPageIdentity(two)
	if !okOne || !okTwo || idOne != idTwo || idOne != "read_tool_result\nold\n18732" {
		t.Fatalf("past-end pages = %q %q ok=%v %v", idOne, idTwo, okOne, okTwo)
	}
	forward := "[tool_result_read]\nid: old\noffset: 16385\ntruncated: true\nnext_offset: 32768\n\n---\npage\n--- end ---\n"
	noisy := "[tool_result_read]\nnext_offset: 1\noffset: next\noffset: 18732\n\n---\n\n--- end ---\n"
	noisyCall := toolCallRecord{name: "read_tool_result", args: `{"id":"old","offset":999999}`, result: noisy, success: true}
	idNoisy, okNoisy := spilledPageIdentity(noisyCall)
	if !okNoisy || idNoisy != "read_tool_result\nold\n18732" {
		t.Fatalf("a non-numeric offset line must not hide the served page, got %q ok=%v", idNoisy, okNoisy)
	}
	kept := toolCallRecord{name: "read_tool_result", args: `{"id":"old","offset":16384}`, result: forward, success: true}
	id, ok := spilledPageIdentity(kept)
	if !ok || id != "read_tool_result\nold\n16384" {
		t.Fatalf("a later served index must stay the requested page, got %q ok=%v", id, ok)
	}
}

func TestWordOffsetIsNotTheFirstPage(t *testing.T) {
	bad := toolCallRecord{name: "read_tool_result", args: `{"id":"old","offset":"next_offset"}`, result: "page", success: true}
	good := toolCallRecord{name: "read_tool_result", args: `{"id":"old","offset":0}`, result: "page", success: true}
	if _, ok := spilledPageIdentity(bad); ok {
		t.Fatal("a non-numeric offset must not join a byte page")
	}
	id, ok := spilledPageIdentity(good)
	if !ok || id != "read_tool_result\nold\n0" {
		t.Fatalf("first page = %q ok=%v", id, ok)
	}
	ident, ok := immutableReadIdentity(bad)
	if !ok || ident == id {
		t.Fatalf("word offset identity = %q", ident)
	}
}

func TestOldSpilledPageDoesNotStopTheNextBatch(t *testing.T) {
	page := toolCallRecord{
		name: "read_tool_result", args: `{"id":"a","offset":0}`,
		result: "page", success: true, batch: 1,
	}
	calls := []toolCallRecord{
		page, page,
		{name: "edit_file", args: `{"path":"a"}`, result: "ok", success: true, batch: 2},
	}
	id, seen := mostRepeatedImmutableRead(calls, 2)
	if seen != 0 || id != "" {
		t.Fatalf("a later edit must not revive an earlier repeated page: id=%q seen=%d", id, seen)
	}
	if _, seen = mostRepeatedImmutableRead(calls, 1); seen != 2 {
		t.Fatalf("the batch that repeated the page must count it, seen=%d", seen)
	}
}

func TestRepeatedStalledRemoteCommandStopsWithoutACallCount(t *testing.T) {
	progressing := make([]toolCallRecord, 0, 40)
	for index := 0; index < 40; index++ {
		progressing = append(progressing, toolCallRecord{
			name: "ssh", args: `{"command":"tail -n 1 app.log"}`,
			result: "line-" + strings.Repeat("x", index+1),
			success: true, poll: true,
		})
	}
	if _, seen := repeatedStalledContinuousCall(progressing); seen != 1 {
		t.Fatalf("changing remote output must not look stalled, seen=%d", seen)
	}
	if driftFailureHint(progressing[len(progressing)-4:]) {
		t.Fatal("a progressing remote session must not be hinted as a dead loop")
	}
	stalled := repeatToolCalls("ssh", `{"command":"hostname"}`, "web-1\n", true, true, 3)
	id, seen := repeatedStalledContinuousCall(stalled)
	if seen != 3 || id == "" {
		t.Fatalf("identical remote command seen=%d id=%q", seen, id)
	}
	if !repeatedCommitStops(map[string]struct{}{id: {}}, id, 2) {
		t.Fatal("a noted identical remote command must stop on the next repeat")
	}
	reordered := []toolCallRecord{
		{name: "ssh", args: `{"command":"hostname","host":"web-1"}`, result: "web-1\n", success: true, poll: true},
		{name: "ssh", args: `{"host":"web-1","command":"hostname"}`, result: "web-1\n", success: true, poll: true},
		{name: "ssh", args: `{"command":"hostname","host":"web-1"}`, result: "web-1\n", success: true, poll: true},
	}
	if _, seen = repeatedStalledContinuousCall(reordered); seen != 3 {
		t.Fatalf("key order hid the stalled remote command, seen=%d", seen)
	}
	cut := "line\n\n... (已截断，共 20000 字节) ...\n\nline"
	truncated := repeatToolCalls("ssh_bash", `{"command":"tail -n 200 app.log"}`, cut, true, true, 4)
	if _, seen = repeatedStalledContinuousCall(truncated); seen != 1 {
		t.Fatalf("a truncated remote preview must not count as the same result, seen=%d", seen)
	}
	if driftFailureHint(truncated) {
		t.Fatal("a truncated remote preview must not hint a dead loop")
	}
	spilled := repeatToolCalls("ssh", `{"command":"journalctl -n 80"}`, "ok\n[tool_result_handle]\nid: h1\n", true, true, 4)
	if driftFailureHint(spilled) {
		t.Fatal("a spilled remote page must not hint a dead loop")
	}
	word := repeatToolCalls("ssh", `{"command":"echo done"}`, "日志已截断，但命令已结束", true, true, 3)
	if _, seen = repeatedStalledContinuousCall(word); seen != 3 {
		t.Fatalf("a finished observation that merely says 已截断 must still stall, seen=%d", seen)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [16]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

func TestBR696A70521E75DifferentSearchesDoNotStop(t *testing.T) {
	// T4 read one spilled flutter log three times at offset 0. The queries
	// and receipts differed, but the page key ignored the query, so the
	// third read was the same page and the turn hard-exited.
	handle := "20261126T093247_bash_ff5379d95f85feab"
	queries := []string{
		"No issues found|All tests passed|Error|error •",
		"issues found",
		"All tests passed",
	}
	receipts := []string{
		searchReadResult(handle, 0, queries[0], false),
		searchReadResult(handle, 0, queries[1], false) + "[match_offset: 2264]\nhit\n",
		searchReadResult(handle, 0, queries[2], false),
	}
	calls := make([]toolCallRecord, len(queries))
	for i, query := range queries {
		calls[i] = toolCallRecord{
			name:    "read_tool_result",
			args:    `{"id":"` + handle + `","query":` + strconv.Quote(query) + `}`,
			result:  receipts[i],
			success: true,
			batch:   33,
		}
	}
	if _, seen := repeatedSuccessfulCommit(calls); seen != 1 {
		t.Fatalf("the third different search was counted as the same page, seen=%d", seen)
	}
	id, seen := mostRepeatedImmutableRead(calls, 33)
	if seen != 1 || repeatedCommitStops(nil, id, seen) {
		t.Fatalf("one batch of different searches must not hard-exit: id=%q seen=%d", id, seen)
	}
	again := append(calls, calls[2])
	id, seen = mostRepeatedImmutableRead(again, 33)
	if seen != 2 || repeatedCommitStops(nil, id, seen) {
		t.Fatalf("the first repeat of one search warns, it does not stop: seen=%d", seen)
	}
	third := append(again, calls[2])
	id, seen = mostRepeatedImmutableRead(third, 33)
	if seen != 3 || !repeatedCommitStops(nil, id, seen) {
		t.Fatalf("the same search a third time must still stop: id=%q seen=%d", id, seen)
	}
}

func TestDifferentSearchQueriesAreDifferentPages(t *testing.T) {
	queries := []string{
		"No issues found|All tests passed|Error|error •",
		"issues found",
		"All tests passed",
	}
	calls := make([]toolCallRecord, len(queries))
	ids := make([]string, len(queries))
	for i, query := range queries {
		calls[i] = toolCallRecord{
			name:    "read_tool_result",
			args:    `{"id":"h","offset":0,"query":` + strconv.Quote(query) + `}`,
			result:  searchReadResult("h", 0, query, false),
			success: true,
		}
		id, ok := spilledPageIdentity(calls[i])
		if !ok {
			t.Fatalf("query %q has no page identity", query)
		}
		ids[i] = id
	}
	if ids[0] == ids[1] || ids[1] == ids[2] || ids[0] == ids[2] {
		t.Fatalf("different queries collapsed: %q", ids)
	}
	if _, seen := repeatedSuccessfulCommit(calls); seen != 1 {
		t.Fatalf("different queries counted as one page, seen=%d", seen)
	}
	if repeatedCommitStops(nil, ids[2], 1) {
		t.Fatal("a new query must not stop the turn")
	}
	same := []toolCallRecord{calls[2], calls[2], calls[2]}
	id, seen := repeatedSuccessfulCommit(same)
	if seen != 3 || !repeatedCommitStops(nil, id, seen) {
		t.Fatalf("the same search must still stop: id=%q seen=%d", id, seen)
	}
}

func TestSameSearchQueryIgnoresCaseLimitAndHandleSuffix(t *testing.T) {
	one := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0,"query":"NeEdLe","limit":1}`,
		result:  searchReadResult("h", 0, "NeEdLe", false),
		success: true,
	}
	two := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"offset":0,"id":"h.txt","query":"  needle  "}`,
		result:  searchReadResult("h", 0, "needle", false),
		success: true,
	}
	idOne, okOne := spilledPageIdentity(one)
	idTwo, okTwo := spilledPageIdentity(two)
	if !okOne || !okTwo || idOne != idTwo {
		t.Fatalf("the same search was split: %q %q ok=%v %v", idOne, idTwo, okOne, okTwo)
	}
	bare := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0}`,
		result:  "[tool_result_read]\nid: h\noffset: 0\ntruncated: false\n\n---\npage\n--- end ---\n",
		success: true,
	}
	byteID, ok := spilledPageIdentity(bare)
	if !ok || byteID == idOne || byteID != "read_tool_result\nh\n0" {
		t.Fatalf("a byte page must stay id/offset only, got %q search %q", byteID, idOne)
	}
}

func TestSearchQueryFoldFollowsRegexpCaseClass(t *testing.T) {
	// strings.ToLower(U+0130) is ASCII i, but regexp (?i) does not match
	// those spellings. Folding them together made the third different
	// search the same page and hard-exited the turn.
	dotted := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0,"query":"İ"}`,
		result:  searchReadResult("h", 0, "İ", false),
		success: true,
	}
	plain := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0,"query":"i"}`,
		result:  searchReadResult("h", 0, "i", false),
		success: true,
	}
	idDotted, okDotted := spilledPageIdentity(dotted)
	idPlain, okPlain := spilledPageIdentity(plain)
	if !okDotted || !okPlain || idDotted == idPlain {
		t.Fatalf("dotted capital I and i collapsed: %q %q ok=%v %v", idDotted, idPlain, okDotted, okPlain)
	}
	ascii := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0,"query":"I"}`,
		result:  searchReadResult("h", 0, "I", false),
		success: true,
	}
	idASCII, okASCII := spilledPageIdentity(ascii)
	if !okASCII || idASCII != idPlain || idASCII == idDotted {
		t.Fatalf("ASCII I must stay with i and apart from İ: ascii=%q plain=%q dotted=%q", idASCII, idPlain, idDotted)
	}
	// Final sigma and capital sigma are one (?i) search, so they are one page.
	capital := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0,"query":"Σ"}`,
		result:  searchReadResult("h", 0, "Σ", false),
		success: true,
	}
	final := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0,"query":"ς"}`,
		result:  searchReadResult("h", 0, "ς", false),
		success: true,
	}
	idCapital, okCapital := spilledPageIdentity(capital)
	idFinal, okFinal := spilledPageIdentity(final)
	if !okCapital || !okFinal || idCapital != idFinal {
		t.Fatalf("sigma case class split: %q %q ok=%v %v", idCapital, idFinal, okCapital, okFinal)
	}
	id, seen := repeatedSuccessfulCommit([]toolCallRecord{dotted, plain, ascii})
	if seen != 2 || id != idPlain || repeatedCommitStops(nil, id, seen) {
		t.Fatalf("I and i are one page; İ must not join them: id=%q seen=%d", id, seen)
	}
	// (?i) does not fold a rune outside regexp/syntax [minFold, maxFold].
	// Pairing it with its SimpleFold partner made a third search the same page.
	outside := rune(regexpFoldMax + 1)
	partner := unicode.SimpleFold(outside)
	if partner == outside {
		for r := outside; r < outside+0x40; r++ {
			if unicode.SimpleFold(r) != r {
				outside = r
				partner = unicode.SimpleFold(r)
				break
			}
		}
	}
	if partner != outside {
		left := toolCallRecord{
			name:    "read_tool_result",
			args:    `{"id":"h","offset":0,"query":` + strconv.Quote(string(outside)) + `}`,
			result:  searchReadResult("h", 0, string(outside), false),
			success: true,
		}
		right := toolCallRecord{
			name:    "read_tool_result",
			args:    `{"id":"h","offset":0,"query":` + strconv.Quote(string(partner)) + `}`,
			result:  searchReadResult("h", 0, string(partner), false),
			success: true,
		}
		idLeft, okLeft := spilledPageIdentity(left)
		idRight, okRight := spilledPageIdentity(right)
		if !okLeft || !okRight || idLeft == idRight {
			t.Fatalf("rune outside (?i) fold range collapsed with its SimpleFold partner: %#U %#U %q %q", outside, partner, idLeft, idRight)
		}
		if _, seen := repeatedSuccessfulCommit([]toolCallRecord{left, right, plain}); seen != 1 {
			t.Fatalf("an unfolded partner must not join the latest search, seen=%d", seen)
		}
	}
	// A query past the raw 4096-byte cap is rejected before (?i) compiles.
	// Folding Kelvin first shrinks it onto an accepted spelling.
	over := strings.Repeat("a", 4094) + "\u212A"
	accepted := strings.Repeat("a", 4094) + "K"
	if len(over) <= 4096 || len(accepted) > 4096 {
		t.Fatalf("query cap fixture drifted: over=%d accepted=%d", len(over), len(accepted))
	}
	longCall := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0,"query":` + strconv.Quote(over) + `}`,
		result:  "error: toolresult: query exceeds 4096 bytes",
		success: false,
	}
	acceptedCall := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0,"query":` + strconv.Quote(accepted) + `}`,
		result:  searchReadResult("h", 0, accepted, false),
		success: true,
	}
	if normalizeSpilledSearchQuery(over) == normalizeSpilledSearchQuery(accepted) {
		t.Fatal("a rejected over-cap query folded onto the accepted spelling")
	}
	if _, seen := repeatedSuccessfulCommit([]toolCallRecord{longCall, acceptedCall, acceptedCall}); seen != 2 {
		t.Fatal("the rejected spelling must not count as the accepted search")
	}
	// The loop records the tool string before the host outcome. A lowercase
	// "error:" used to classify as success, so the rejected spelling joined
	// the accepted page and the third call hard-stopped a different search.
	if classifyToolResult(longCall.result).kind != toolOutcomeError {
		t.Fatal("read_tool_result error: must not classify as a successful page")
	}
	recorded := longCall
	recorded.success = executionOutcomeFromToolOutcome(classifyToolResult(recorded.result).kind) == ToolExecutionOutcomeOK
	if recorded.success {
		t.Fatal("a rejected query must not be recorded as a successful commit")
	}
	if _, seen := repeatedSuccessfulCommit([]toolCallRecord{recorded, acceptedCall, acceptedCall}); seen != 2 {
		t.Fatal("a misclassified rejection must not join the accepted search")
	}
	// Kelvin matches K under (?i), but search keeps the raw bytes for the
	// overlap window. Folding 3 bytes to 1 made those pages one observation.
	kelvinQuery := "\u212A"
	kelvin := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0,"query":` + strconv.Quote(kelvinQuery) + `}`,
		result:  searchReadResult("h", 0, kelvinQuery, false),
		success: true,
	}
	kelvinID, okKelvin := spilledPageIdentity(kelvin)
	asciiK := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0,"query":"K"}`,
		result:  searchReadResult("h", 0, "K", false),
		success: true,
	}
	kID, okK := spilledPageIdentity(asciiK)
	if !okKelvin || !okK || kelvinID == kID {
		t.Fatalf("Kelvin must stay a distinct page from K: %q %q", kelvinID, kID)
	}
	// minFoldRune('A') is 'A', not 'a'. 'a' still joins that minimum.
	if foldedSearchRune('@') != '@' || foldedSearchRune('A') != 'A' || foldedSearchRune('a') != 'A' || foldedSearchRune('\u212A') != 'K' {
		t.Fatalf("fold bounds drifted: @=%#U A=%#U a=%#U kelvin=%#U", foldedSearchRune('@'), foldedSearchRune('A'), foldedSearchRune('a'), foldedSearchRune('\u212A'))
	}
	// The page is the simplified syntax tree regexp.Compile searches. A local
	// walk that disagrees, or a fold that no longer parses, is not that page.
	// Regexp.String keeps the raw pattern, so it cannot tell I from i.
	for _, query := range []string{"I", "i", "İ", "Σ", "ς", "a\u212A", string(rune(regexpFoldMax + 1))} {
		folded := normalizeSpilledSearchQuery(query)
		left, err := regexpSyntax.Parse("(?i)"+regexp.QuoteMeta(query), regexpSyntax.Perl)
		if err != nil {
			if folded != strings.TrimSpace(query) {
				t.Fatalf("uncompiled %q folded to %q", query, folded)
			}
			continue
		}
		right, ferr := regexpSyntax.Parse("(?i)"+regexp.QuoteMeta(folded), regexpSyntax.Perl)
		if ferr != nil || !left.Simplify().Equal(right.Simplify()) {
			t.Fatalf("folded %q is not the compiled (?i) page of %q: %v %v", folded, query, err, ferr)
		}
		if _, cerr := regexp.Compile("(?i)" + regexp.QuoteMeta(query)); cerr != nil && folded != strings.TrimSpace(query) {
			t.Fatalf("compile failed for %q but it folded to %q: %v", query, folded, cerr)
		}
	}
}

func TestSearchContinuationKeepsTheQuery(t *testing.T) {
	call := toolCallRecord{
		name:    "read_tool_result",
		args:    `{"id":"h","offset":0,"query":"needle"}`,
		result:  "[tool_result_read]\nid: h\noffset: 0\nquery: \"needle\"\nreturned_matches: 1\ntruncated: true\nnext_offset: 400\n\n---\nmatch\n--- end ---\n",
		success: true,
	}
	next, ok := advertisedReadContinuation(call)
	want, wantOK := spilledPageIdentity(toolCallRecord{
		name: "read_tool_result", args: `{"id":"h","offset":400,"query":"needle"}`, result: "page", success: true,
	})
	if !ok || !wantOK || next != want {
		t.Fatalf("continuation = %q ok=%v want %q", next, ok, want)
	}
	body := toolCallRecord{
		name: "read_tool_result", args: `{"id":"h","offset":0}`,
		result:  "[tool_result_read]\noffset: 0\ntruncated: true\nnext_offset: 16384\n\n---\nquery: \"needle\"\n--- end ---\n",
		success: true,
	}
	byteNext, ok := advertisedReadContinuation(body)
	if !ok || byteNext != "read_tool_result\nh\n16384" {
		t.Fatalf("body query must not join a byte page, got %q ok=%v", byteNext, ok)
	}
}

func searchReadResult(id string, offset int, query string, truncated bool) string {
	state := "truncated: false\n"
	if truncated {
		state = "truncated: true\nnext_offset: 400\n"
	}
	return "[tool_result_read]\nid: " + id + "\noffset: " + itoa(offset) + "\nquery: " + strconv.Quote(query) + "\nreturned_matches: 0\n" + state + "\n---\n\n--- end ---\n"
}

func TestRepeatedCommitResetsWhenThePayloadChanges(t *testing.T) {
	calls := repeatToolCalls("write_file", `{"path":"a.go"}`, "wrote a.go", true, false, 2)
	calls = append(calls, toolCallRecord{name: "write_file", args: `{"path":"b.go"}`, result: "wrote b.go", success: true})
	_, seen := repeatedSuccessfulCommit(calls)
	if seen != 1 {
		t.Fatalf("the latest different write is a new commit: seen=%d", seen)
	}
}
