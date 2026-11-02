package agent

import "testing"

func repeatToolCalls(name, args, result string, success, poll bool, n int) []toolCallRecord {
	tail := make([]toolCallRecord, n)
	for i := range tail {
		tail[i] = toolCallRecord{name: name, args: args, result: result, success: success, poll: poll}
	}
	return tail
}

func TestDriftFailureHintLeavesSuccessfulPollAlone(t *testing.T) {
	tail := repeatToolCalls("ssh", `{"command":"status"}`, "running", true, true, 4)
	if driftFailureHint(tail) {
		t.Fatal("a successful status poll must stay quiet")
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

func TestRepeatedCommitResetsWhenThePayloadChanges(t *testing.T) {
	calls := repeatToolCalls("write_file", `{"path":"a.go"}`, "wrote a.go", true, false, 2)
	calls = append(calls, toolCallRecord{name: "write_file", args: `{"path":"b.go"}`, result: "wrote b.go", success: true})
	_, seen := repeatedSuccessfulCommit(calls)
	if seen != 1 {
		t.Fatalf("the latest different write is a new commit: seen=%d", seen)
	}
}
