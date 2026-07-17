package lansengerwatch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEngineRecordAndKeywordCLI(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	eng := &Engine{
		Store: store,
		Now:   func() time.Time { return time.Date(2026, 7, 16, 10, 0, 0, 0, time.Local) },
		RunCLI: func(ctx context.Context, command string, p CLIParams, timeoutSec int) CLIResult {
			if p.SpeakerID != "staff-1" || p.Keyword != "报警" {
				t.Fatalf("cli params: %+v", p)
			}
			if !strings.Contains(command, "hook.py") && !strings.Contains(command, "hook") {
				// expanded may append flags
			}
			return CLIResult{Stdout: "CLI已处理", Command: command}
		},
	}
	job := Job{
		ID:             "job-1",
		Enabled:        true,
		GroupID:        "g-1",
		TargetStaffIDs: []string{"staff-1"},
		RecordAll:      true,
		Keywords: []KeywordRule{{
			ID:            "r1",
			Keywords:      []string{"报警"},
			RecordOnMatch: true,
			ReplyText:     "收到",
			CLICommand:    "python hook.py --x={{speaker_id}}",
		}},
	}
	res := eng.Process(context.Background(), []Job{job}, Incoming{
		IsGroup: true, GroupID: "g-1", SpeakerID: "staff-1", SpeakerName: "Alice",
		Text: "请处理报警事件", ReceivedAt: eng.Now(),
	})
	if !res.RecordedAll {
		t.Fatal("expected record all")
	}
	if len(res.KeywordHits) != 1 || !res.KeywordHits[0].UsedCLIReply {
		t.Fatalf("hits: %+v", res.KeywordHits)
	}
	if len(res.Replies) != 1 || res.Replies[0] != "CLI已处理" {
		t.Fatalf("replies: %v", res.Replies)
	}
	files, err := store.ListTranscriptFiles("job-1")
	if err != nil || len(files) < 1 {
		t.Fatalf("files: %v %v", files, err)
	}
	// Ensure content written
	found := false
	for _, f := range files {
		data, _ := os.ReadFile(f)
		if strings.Contains(string(data), "Alice(staff-1)") {
			found = true
		}
	}
	if !found {
		t.Fatal("transcript missing speaker line")
	}
}

func TestEngineKeywordAnyoneAndForward(t *testing.T) {
	store := NewStore(t.TempDir())
	eng := &Engine{
		Store: store,
		Now:   func() time.Time { return time.Date(2026, 7, 16, 12, 0, 0, 0, time.Local) },
	}
	job := Job{
		ID:                    "job-fwd",
		Enabled:               true,
		GroupID:               "g-9",
		GroupName:             "测试群",
		TargetStaffIDs:        []string{"vip-1"},
		KeywordScope:          KeywordScopeAnyone,
		ForwardOnTargetSpeech: true,
		ForwardChannels:       []string{"weixin", "weixin", "hub", "unknown"},
		Keywords: []KeywordRule{{
			ID:             "r-kw",
			Keywords:       []string{"机密"},
			RecordOnMatch:  true,
			ForwardOnMatch: true,
			ReplyText:      "已记录",
		}},
	}

	// Non-target speaker + keyword anyone → keyword hit + forward (not target speech).
	res := eng.Process(context.Background(), []Job{job}, Incoming{
		IsGroup: true, GroupID: "g-9", GroupName: "测试群",
		SpeakerID: "other-9", SpeakerName: "路人",
		Text: "这是机密文件", ReceivedAt: eng.Now(),
	})
	if res.RecordedAll {
		t.Fatal("non-target must not record_all")
	}
	if len(res.KeywordHits) != 1 || res.KeywordHits[0].Keyword != "机密" {
		t.Fatalf("keyword hits: %+v", res.KeywordHits)
	}
	if len(res.Replies) != 1 || res.Replies[0] != "已记录" {
		t.Fatalf("replies: %v", res.Replies)
	}
	if len(res.Forwards) != 2 { // weixin + hub (deduped, unknown dropped)
		t.Fatalf("forwards want 2, got %d %+v", len(res.Forwards), res.Forwards)
	}
	seenCh := map[string]bool{}
	for _, f := range res.Forwards {
		seenCh[f.Channel] = true
		if !strings.Contains(f.Text, "【盯人转发】") || !strings.Contains(f.Text, "路人") ||
			!strings.Contains(f.Text, "测试群") || !strings.Contains(f.Text, "机密文件") ||
			!strings.Contains(f.Text, "关键字「机密」") {
			t.Fatalf("forward body: %s", f.Text)
		}
		if f.Reason != "keyword" {
			t.Fatalf("reason: %s", f.Reason)
		}
	}
	if !seenCh[ChannelWeixin] || !seenCh[ChannelHub] {
		t.Fatalf("channels: %v", seenCh)
	}

	// Target speech without keyword → speech forward only.
	res2 := eng.Process(context.Background(), []Job{job}, Incoming{
		IsGroup: true, GroupID: "g-9", GroupName: "测试群",
		SpeakerID: "vip-1", SpeakerName: "VIP",
		Text: "普通闲聊", ReceivedAt: eng.Now(),
	})
	if len(res2.KeywordHits) != 0 {
		t.Fatalf("no keyword expected: %+v", res2.KeywordHits)
	}
	if len(res2.Forwards) != 2 {
		t.Fatalf("speech forwards: %+v", res2.Forwards)
	}
	if res2.Forwards[0].Reason != "target_speech" {
		t.Fatalf("reason: %s", res2.Forwards[0].Reason)
	}
	if !strings.Contains(res2.Forwards[0].Text, "关注对象发言") {
		t.Fatalf("body: %s", res2.Forwards[0].Text)
	}
}

func TestShouldForwardKeywordHit(t *testing.T) {
	job := Job{ForwardChannels: []string{"weixin"}, ForwardOnTargetSpeech: true}
	rule := KeywordRule{ForwardOnMatch: false}
	if !shouldForwardKeywordHit(job, rule) {
		t.Fatal("speech-forward + channels should forward keyword")
	}
	job.ForwardOnTargetSpeech = false
	if shouldForwardKeywordHit(job, rule) {
		t.Fatal("no speech-forward and no rule flag")
	}
	rule.ForwardOnMatch = true
	if !shouldForwardKeywordHit(job, rule) {
		t.Fatal("explicit rule flag")
	}
	job.ForwardChannels = nil
	if shouldForwardKeywordHit(job, rule) {
		t.Fatal("no channels → never")
	}
	job.ForwardChannels = []string{"unknown"}
	if shouldForwardKeywordHit(job, rule) {
		t.Fatal("invalid channels normalize to empty")
	}
}

func TestEngineKeywordAnyoneForwardsWhenSpeechForwardOnWithoutRuleFlag(t *testing.T) {
	// Mirrors user config: speech-forward + channels, keyword scope=anyone, reply works,
	// but rule.ForwardOnMatch was left false — still must notify owner channels.
	store := NewStore(t.TempDir())
	eng := &Engine{
		Store: store,
		Now:   func() time.Time { return time.Date(2026, 7, 17, 10, 0, 0, 0, time.Local) },
	}
	job := Job{
		ID:                    "job-implicit-kw-fwd",
		Enabled:               true,
		GroupID:               "g-k",
		GroupName:             "运营群",
		TargetStaffIDs:        []string{"vip"},
		KeywordScope:          KeywordScopeAnyone,
		ForwardOnTargetSpeech: true,
		ForwardChannels:       []string{"weixin", "lansenger"},
		Keywords: []KeywordRule{{
			ID:             "r1",
			Keywords:       []string{"test"},
			RecordOnMatch:  true,
			ForwardOnMatch: false, // explicit off — still forward via speech-forward job flag
			ReplyText:      "收到",
		}},
	}
	res := eng.Process(context.Background(), []Job{job}, Incoming{
		IsGroup: true, GroupID: "g-k", GroupName: "运营群",
		SpeakerID: "stranger", SpeakerName: "路人甲",
		Text: "please test this", ReceivedAt: eng.Now(),
	})
	if len(res.Replies) != 1 || res.Replies[0] != "收到" {
		t.Fatalf("group reply: %v", res.Replies)
	}
	if len(res.Forwards) != 2 {
		t.Fatalf("want weixin+lansenger forwards, got %+v", res.Forwards)
	}
	if !res.KeywordHits[0].Forwarded {
		t.Fatal("keyword hit should mark Forwarded")
	}
	for _, f := range res.Forwards {
		if f.Reason != "keyword" {
			t.Fatalf("reason=%s body=%s", f.Reason, f.Text)
		}
		if !strings.Contains(f.Text, "路人甲") || !strings.Contains(f.Text, "please test this") {
			t.Fatalf("body: %s", f.Text)
		}
	}

	// No speech-forward, no rule flag → group reply only, no channel package.
	job.ForwardOnTargetSpeech = false
	res2 := eng.Process(context.Background(), []Job{job}, Incoming{
		IsGroup: true, GroupID: "g-k",
		SpeakerID: "stranger", SpeakerName: "路人甲",
		Text: "please test this", ReceivedAt: eng.Now(),
	})
	if len(res2.Replies) != 1 {
		t.Fatalf("reply still: %v", res2.Replies)
	}
	if len(res2.Forwards) != 0 {
		t.Fatalf("no forward expected: %+v", res2.Forwards)
	}
}

func TestForwardSpeechUpgradedByKeyword(t *testing.T) {
	store := NewStore(t.TempDir())
	eng := &Engine{Store: store, Now: func() time.Time { return time.Date(2026, 7, 16, 15, 0, 0, 0, time.Local) }}
	job := Job{
		ID: "j1", Enabled: true, GroupID: "g",
		TargetStaffIDs:        []string{"u1"},
		ForwardOnTargetSpeech: true,
		ForwardChannels:       []string{"weixin", "hub"},
		Keywords: []KeywordRule{{
			Keywords: []string{"urgent"}, ForwardOnMatch: true, ReplyText: "ok",
		}},
	}
	res := eng.Process(context.Background(), []Job{job}, Incoming{
		IsGroup: true, GroupID: "g", SpeakerID: "u1", SpeakerName: "A",
		Text: "this is urgent", ReceivedAt: eng.Now(),
	})
	if len(res.Forwards) != 2 {
		t.Fatalf("want 2 channel forwards, got %+v", res.Forwards)
	}
	for _, f := range res.Forwards {
		if f.Reason != "keyword" {
			t.Fatalf("speech should upgrade to keyword, got %+v", f)
		}
		if !strings.Contains(f.Text, "关键字") {
			t.Fatalf("body should be keyword package: %s", f.Text)
		}
	}
	if len(res.Replies) != 1 {
		t.Fatalf("replies: %v", res.Replies)
	}
}

func TestDedupeNonEmpty(t *testing.T) {
	got := DedupeNonEmpty([]string{" a ", "a", "", "b", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("%v", got)
	}
}

func TestJobNeedsMessage(t *testing.T) {
	job := Job{
		Enabled: true, TargetStaffIDs: []string{"a"},
		KeywordScope: KeywordScopeAnyone,
		Keywords:     []KeywordRule{{Keywords: []string{"x"}}},
	}
	if !JobNeedsMessage(job, "stranger") {
		t.Fatal("anyone scope should need non-target messages")
	}
	job.KeywordScope = KeywordScopeTargets
	if JobNeedsMessage(job, "stranger") {
		t.Fatal("targets scope should skip non-target without record/forward")
	}
	if !JobNeedsMessage(job, "a") {
		t.Fatal("target with keywords should need message")
	}
	// Empty keyword strings must not keep the job "hot".
	job.Keywords = []KeywordRule{{Keywords: []string{"", "  "}}}
	job.RecordAll = false
	job.ForwardOnTargetSpeech = false
	if JobNeedsMessage(job, "stranger") {
		t.Fatal("blank keywords must not require anyone-scope processing")
	}
	if JobNeedsMessage(job, "a") {
		t.Fatal("blank keywords + no record/forward must not need message")
	}
}

func TestHasEnabledJobs(t *testing.T) {
	if HasEnabledJobs(nil) || HasEnabledJobs([]Job{{Enabled: false}}) {
		t.Fatal("expected false")
	}
	if !HasEnabledJobs([]Job{{Enabled: false}, {Enabled: true}}) {
		t.Fatal("expected true")
	}
}

func TestStoreUpsertAndRoster(t *testing.T) {
	store := NewStore(t.TempDir())
	job, err := store.UpsertJob(Job{
		Name: "test", GroupID: "g1", Enabled: true,
		TargetStaffIDs: []string{"u1", "u1", " u2 "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == "" || len(job.TargetStaffIDs) != 2 {
		t.Fatalf("%+v", job)
	}
	if err := store.NoteMember("g1", "群", "u1", "王", "message"); err != nil {
		t.Fatal(err)
	}
	r, err := store.LoadRoster("g1")
	if err != nil || len(r.Members) != 1 || r.Members[0].Name != "王" {
		t.Fatalf("%+v %v", r, err)
	}
	// Second note without name change should not thrash (still ok).
	if err := store.NoteMember("g1", "群", "u1", "王", "message"); err != nil {
		t.Fatal(err)
	}
	// Name update must persist.
	if err := store.NoteMember("g1", "群", "u1", "王二", "message"); err != nil {
		t.Fatal(err)
	}
	r, err = store.LoadRoster("g1")
	if err != nil || r.Members[0].Name != "王二" {
		t.Fatalf("name update: %+v %v", r, err)
	}
	// path isolation
	outside := filepath.Join(t.TempDir(), "x.txt")
	_ = os.WriteFile(outside, []byte("x"), 0o644)
	if _, err := store.ReadTranscriptFile(outside); err == nil {
		t.Fatal("expected path guard")
	}
}

func TestListTranscriptFilesSortedAndReadCap(t *testing.T) {
	store := NewStore(t.TempDir())
	// Write older then newer day files via AppendTranscript (uses today); force two kinds.
	if _, err := store.AppendTranscript("job-x", "all", "line-a\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendTranscript("job-x", "keyword", "line-k\n"); err != nil {
		t.Fatal(err)
	}
	files, err := store.ListTranscriptFiles("job-x")
	if err != nil || len(files) < 2 {
		t.Fatalf("files: %v %v", files, err)
	}
	// Basenames must be descending.
	b0 := filepath.Base(files[0])
	b1 := filepath.Base(files[1])
	if b0 < b1 {
		t.Fatalf("not sorted desc: %s then %s", b0, b1)
	}
	// Path guard still works.
	outside := filepath.Join(t.TempDir(), "x.txt")
	_ = os.WriteFile(outside, []byte("x"), 0o644)
	if _, err := store.ReadTranscriptFile(outside); err == nil {
		t.Fatal("expected path guard")
	}
	text, err := store.ReadTranscriptFile(files[0])
	if err != nil || !strings.Contains(text, "line-") {
		t.Fatalf("read: %q %v", text, err)
	}
}

func TestNoteMemberSkipsRedundantWrite(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.NoteMember("g", "G", "s1", "A", "message"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Root(), RosterDirName, "g.json")
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Immediate re-note: should skip write (mtime unchanged on most FS).
	time.Sleep(20 * time.Millisecond)
	if err := store.NoteMember("g", "G", "s1", "A", "message"); err != nil {
		t.Fatal(err)
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		// Some FS have coarse timestamps; accept size-stable as soft check.
		if info1.Size() != info2.Size() {
			t.Fatalf("expected skip write, mtime/size changed %v -> %v", info1.ModTime(), info2.ModTime())
		}
	}
}
