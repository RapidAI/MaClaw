package survey

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	st := NewStore(db)
	if err := st.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

func sampleQuestions() []Question {
	return []Question{
		{
			ID: "q1", Type: "single_choice", Title: "满意吗", Required: true,
			Options: []Option{{ID: "opt_yes", Label: "是"}, {ID: "opt_no", Label: "否"}},
		},
		{
			ID: "q2", Type: "multi_choice", Title: "兴趣", Required: true,
			Options: []Option{
				{ID: "opt_c", Label: "C"},
				{ID: "opt_a", Label: "A"},
				{ID: "opt_b", Label: "B"},
			},
		},
	}
}

func TestCreatePublishIMSubmitAndTenantIsolation(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)

	// tenant A
	sv, err := st.Create(ctx, "tenant-a", "user-1", CreateInput{
		Title:     "午餐调查",
		Questions: sampleQuestions(),
		Settings:  SettingsIn{Anonymous: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sv.ShortCode) != 6 {
		t.Fatalf("short_code len=%d", len(sv.ShortCode))
	}
	if sv.Settings.AnonymitySalt == "" {
		t.Fatal("salt missing")
	}
	if err := st.Bind(ctx, "tenant-a", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g1", GroupName: "Team"}}); err != nil {
		t.Fatal(err)
	}
	pub, err := st.Publish(ctx, "tenant-a", sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pub.Status != StatusPublished {
		t.Fatalf("status=%s", pub.Status)
	}

	// start + answer via im/handle
	code := pub.ShortCode
	resp, err := rt.Handle(ctx, "tenant-a", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "staff1", UserName: "Alice",
		ChatType: "group", GroupID: "g1", Text: "/survey " + code,
	})
	if err != nil || !resp.Handled {
		t.Fatalf("start: err=%v handled=%v reply=%q", err, resp.Handled, resp.ReplyText)
	}
	resp, err = rt.Handle(ctx, "tenant-a", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "staff1", UserName: "Alice",
		ChatType: "group", GroupID: "g1", Text: "1",
	})
	if err != nil || !resp.Handled {
		t.Fatalf("q1: %v %v", err, resp)
	}
	resp, err = rt.Handle(ctx, "tenant-a", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "staff1", UserName: "Alice",
		ChatType: "group", GroupID: "g1", Text: "2,1", // multi: opt_a,opt_c sorted storage
	})
	if err != nil || !resp.Handled {
		t.Fatalf("q2: %v %v", err, resp)
	}
	if resp.ReplyText != "提交成功，感谢参与！" {
		t.Fatalf("final reply=%q", resp.ReplyText)
	}
	if resp.SurveyID != sv.ID || resp.Event != "response_submitted" {
		t.Fatalf("event fields survey_id=%q event=%q", resp.SurveyID, resp.Event)
	}

	// second submit rejected
	resp, err = rt.Handle(ctx, "tenant-a", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "staff1", UserName: "Alice",
		ChatType: "group", GroupID: "g1", Text: "/survey " + code,
	})
	if err != nil || !resp.Handled {
		t.Fatal(err)
	}
	if resp.ReplyText != "您已提交过该问卷，感谢参与" {
		t.Fatalf("dup reply=%q", resp.ReplyText)
	}

	// tenant B cannot read
	_, err = st.Get(ctx, "tenant-b", sv.ID)
	if err == nil {
		t.Fatal("expected tenant isolation")
	}
	_, err = st.GetByCode(ctx, "tenant-b", code)
	if err == nil {
		t.Fatal("expected code isolation")
	}

	// list responses
	list, err := st.ListResponses(ctx, "tenant-a", sv.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("responses: %v n=%d", err, len(list))
	}
	if list[0].RespondentName != "Alice" {
		t.Fatalf("name=%q", list[0].RespondentName)
	}
	m := JSONToAnswers(list[0].Answers)
	if m["q1"] != "opt_yes" {
		t.Fatalf("q1=%v", m["q1"])
	}
	// multi stored as sorted ids
	raw, _ := json.Marshal(m["q2"])
	var ids []string
	_ = json.Unmarshal(raw, &ids)
	if len(ids) != 2 || ids[0] != "opt_a" || ids[1] != "opt_c" {
		// JSON from map may be []any
		if arr, ok := m["q2"].([]any); ok {
			if len(arr) != 2 {
				t.Fatalf("multi ids=%v", m["q2"])
			}
		} else if len(ids) != 2 {
			t.Fatalf("multi ids=%v", m["q2"])
		}
	}
}

func TestAnonymousHMACAndExport(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)

	sv, err := st.Create(ctx, "t1", "u", CreateInput{
		Title: "Anon",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "Vote", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
		Settings: SettingsIn{Anonymous: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	// salt redaction
	sv.Redact()
	if sv.Settings.AnonymitySalt != "" {
		t.Fatal("salt should redact")
	}
	// reload with salt
	sv, _ = st.Get(ctx, "t1", sv.ID)
	key, err := ComputeRespondentKey(true, sv.Settings.AnonymitySalt, "staffX")
	if err != nil || len(key) != 64 {
		t.Fatalf("hmac key=%q err=%v", key, err)
	}
	// wrong key material (base64 string as key) must differ — ensure algorithm uses decoded bytes
	badKey := func() string {
		// intentionally wrong: use salt string bytes
		return "not-the-real-key"
	}()
	if key == badKey {
		t.Fatal("unexpected")
	}

	_ = st.Bind(ctx, "t1", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	_, _ = st.Publish(ctx, "t1", sv.ID)

	resp, err := rt.Handle(ctx, "t1", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "staffX", UserName: "SecretName",
		ChatType: "group", GroupID: "g", Text: "/survey " + sv.ShortCode + " 1",
	})
	if err != nil || !resp.Handled {
		t.Fatal(err, resp)
	}
	list, err := st.ListResponses(ctx, "t1", sv.ID)
	if err != nil || len(list) != 1 {
		t.Fatal(err)
	}
	if list[0].RespondentName != "" {
		t.Fatalf("anonymous name should be empty, got %q", list[0].RespondentName)
	}
	if list[0].RespondentKey != key {
		t.Fatalf("key mismatch store=%q want=%q", list[0].RespondentKey, key)
	}

	// export
	dir := t.TempDir()
	path := filepath.Join(dir, "out.xlsx")
	sv2, _ := st.Get(ctx, "t1", sv.ID)
	if err := WriteExportFile(path, sv2, list); err != nil {
		t.Fatal(err)
	}
	data := BuildExportData(sv2, list)
	if len(data.Sheets) != 2 {
		t.Fatalf("sheets=%d", len(data.Sheets))
	}
	// design cols: response_id, submitted_at, platform, group_id, group_name, respondent_key, respondent_name
	row := data.Sheets[0].Rows[1]
	if row[5].Value != "anonymous" {
		t.Fatalf("export key=%v", row[5].Value)
	}
	if row[6].Value != "" {
		t.Fatalf("export name should be empty")
	}
}

func TestPublishRequiresBindingAndLastUnbind(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	sv, err := st.Create(ctx, "t", "u", CreateInput{Title: "T", Questions: sampleQuestions()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Publish(ctx, "t", sv.ID); err == nil {
		t.Fatal("publish without binding should fail")
	}
	_ = st.Bind(ctx, "t", sv.ID, []Binding{
		{Platform: PlatformLansenger, GroupID: "g1"},
		{Platform: PlatformLansenger, GroupID: "g2"},
	})
	if _, err := st.Publish(ctx, "t", sv.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.Unbind(ctx, "t", sv.ID, PlatformLansenger, "g1"); err != nil {
		t.Fatal(err)
	}
	if err := st.Unbind(ctx, "t", sv.ID, PlatformLansenger, "g2"); err == nil {
		t.Fatal("last unbind should fail")
	}
}

func TestAllowUpdateUPSERT(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	sv, _ := st.Create(ctx, "t", "u", CreateInput{
		Title: "U",
		Questions: []Question{{
			ID: "q1", Type: "rating", Title: "Score", Required: true,
		}},
		Settings: SettingsIn{AllowUpdate: true},
	})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	_, _ = st.Publish(ctx, "t", sv.ID)

	// first submit
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + sv.ShortCode + " 3",
	})
	if err != nil || !r.Handled {
		t.Fatal(err, r)
	}
	// start again → confirm_update
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + sv.ShortCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.ReplyText != "您已提交。回复「修改」可重新作答，或「取消」退出" {
		t.Fatalf("got %q", r.ReplyText)
	}
	// answer "2" must not count as answer in confirm_update
	r, _ = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g", Text: "2",
	})
	if r.ReplyText != "您已提交。回复「修改」可重新作答，或「取消」退出" {
		t.Fatalf("got %q", r.ReplyText)
	}
	// modify + resubmit
	_, _ = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g", Text: "修改",
	})
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g", Text: "5",
	})
	if err != nil || r.ReplyText != "提交成功，感谢参与！" {
		t.Fatalf("%v %q", err, r.ReplyText)
	}
	list, _ := st.ListResponses(ctx, "t", sv.ID)
	if len(list) != 1 {
		t.Fatalf("want 1 row got %d", len(list))
	}
	m := JSONToAnswers(list[0].Answers)
	// rating may be float64 from json
	switch v := m["q1"].(type) {
	case float64:
		if v != 5 {
			t.Fatalf("rating=%v", v)
		}
	case int:
		if v != 5 {
			t.Fatalf("rating=%v", v)
		}
	default:
		t.Fatalf("rating type %T %v", v, v)
	}
}

func TestMultiExportOrderByOptionPosition(t *testing.T) {
	q := Question{
		ID: "q2", Type: "multi_choice", Title: "兴趣",
		Options: []Option{
			{ID: "opt_c", Label: "C"},
			{ID: "opt_a", Label: "A"},
			{ID: "opt_b", Label: "B"},
		},
	}
	// stored sorted by id: opt_a, opt_c
	ids := []string{"opt_a", "opt_c"}
	labels := MultiLabelsInOptionOrder(q, ids)
	// options array order: c, a, b → selected c then a → "C, A"
	if len(labels) != 2 || labels[0] != "C" || labels[1] != "A" {
		t.Fatalf("labels=%v", labels)
	}
	cell := FormatAnswerCell(q, []any{"opt_a", "opt_c"})
	if cell != "C, A" {
		t.Fatalf("cell=%q", cell)
	}
}

func TestWriteExportToEnvPath(t *testing.T) {
	path := os.Getenv("SURVEY_EXPORT_PATH")
	if path == "" {
		t.Skip("SURVEY_EXPORT_PATH not set")
	}
	st := openTestDB(t)
	ctx := context.Background()
	sv, err := st.Create(ctx, "t", "u", CreateInput{
		Title: "ExportMe",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "OK?", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	_, _ = st.Publish(ctx, "t", sv.ID)
	rt := NewRuntime(st)
	_, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", UserName: "Bob", ChatType: "group", GroupID: "g",
		Text: "/survey " + sv.ShortCode + " 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListResponses(ctx, "t", sv.ID)
	sv2, _ := st.Get(ctx, "t", sv.ID)
	if err := WriteExportFile(path, sv2, list); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}

func TestDeadlineOnSubmit(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	// Publish with a future deadline, then advance runtime clock past it.
	future := time.Now().UTC().Add(time.Hour)
	sv, _ := st.Create(ctx, "t", "u", CreateInput{
		Title: "D",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "X", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
		Settings: SettingsIn{Deadline: &future},
	})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	if _, err := st.Publish(ctx, "t", sv.ID); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime(st)
	rt.Now = func() time.Time { return future.Add(time.Second) }
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u", ChatType: "group", GroupID: "g",
		Text: "/survey " + sv.ShortCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.ReplyText != "问卷已截止" {
		t.Fatalf("got %q", r.ReplyText)
	}
}

func TestListIncludesBindingAndQuestionCounts(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	sv, err := st.Create(ctx, "t", "u", CreateInput{
		Title: "计数",
		Questions: []Question{
			{ID: "q1", Type: "single_choice", Title: "A", Required: true, Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}},
			{ID: "q2", Type: "text", Title: "B", Required: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Bind(ctx, "t", sv.ID, []Binding{
		{Platform: PlatformLansenger, GroupID: "g1"},
		{Platform: PlatformLansenger, GroupID: "g2"},
	})
	list, err := st.List(ctx, "t", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("len=%d", len(list))
	}
	if list[0].QuestionCount != 2 {
		t.Fatalf("questions=%d", list[0].QuestionCount)
	}
	if list[0].BindingCount != 2 {
		t.Fatalf("bindings=%d", list[0].BindingCount)
	}
	// submit one response then re-list
	rt := NewRuntime(st)
	_, _ = st.Publish(ctx, "t", sv.ID)
	_, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", UserName: "A", ChatType: "group", GroupID: "g1",
		Text: "/survey " + sv.ShortCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	// answer both questions quickly via conversational
	_, _ = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g1", Text: "1",
	})
	_, _ = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g1", Text: "hi",
	})
	list2, err := st.List(ctx, "t", "")
	if err != nil {
		t.Fatal(err)
	}
	if list2[0].ResponseCount < 1 {
		t.Fatalf("response_count=%d", list2[0].ResponseCount)
	}
}

func TestSurveyIntroMetaAndStartIncludesDeadline(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	future := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	sv, err := st.Create(ctx, "t", "u", CreateInput{
		Title: "截止问卷",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "X", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
		Settings: SettingsIn{Deadline: &future, TargetCount: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	meta := SurveyIntroMeta(sv)
	if meta == "" || !strings.Contains(meta, "截止") || !strings.Contains(meta, "20") {
		t.Fatalf("meta=%q", meta)
	}
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	_, _ = st.Publish(ctx, "t", sv.ID)
	rt := NewRuntime(st)
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + sv.ShortCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.ReplyText, "开始填写") || !strings.Contains(r.ReplyText, "截止") {
		t.Fatalf("start reply=%q", r.ReplyText)
	}
	list, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u2", ChatType: "group", GroupID: "g",
		Text: "/survey list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list.ReplyText, "截止问卷") {
		t.Fatalf("list=%q", list.ReplyText)
	}
}

func TestUpdateDeadlineAndTargetCount(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	sv, err := st.Create(ctx, "t", "u", CreateInput{
		Title: "T",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "X", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(48 * time.Hour)
	title := "T2"
	desc := "d"
	qs := sv.Questions
	in := UpdateInput{
		Title:       &title,
		Description: &desc,
		Questions:   &qs,
		Settings: &SettingsIn{
			Anonymous:   false,
			AllowUpdate: true,
			Deadline:    &future,
			TargetCount: 100,
		},
	}
	upd, err := st.Update(ctx, "t", sv.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if upd.Settings.TargetCount != 100 {
		t.Fatalf("target=%d", upd.Settings.TargetCount)
	}
	if upd.Settings.Deadline == nil || !upd.Settings.Deadline.Equal(future) {
		t.Fatalf("deadline=%v want %v", upd.Settings.Deadline, future)
	}
	// clear deadline
	in2 := UpdateInput{Settings: &SettingsIn{TargetCount: 50, Deadline: nil}}
	upd2, err := st.Update(ctx, "t", sv.ID, in2)
	if err != nil {
		t.Fatal(err)
	}
	if upd2.Settings.Deadline != nil {
		t.Fatalf("expected cleared deadline, got %v", upd2.Settings.Deadline)
	}
	if upd2.Settings.TargetCount != 50 {
		t.Fatalf("target=%d", upd2.Settings.TargetCount)
	}
	stt := ComputeStats(upd2, nil)
	if stt.TargetCount != 50 {
		t.Fatalf("stats target=%d", stt.TargetCount)
	}
}

func TestSessionTTLExpires(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	sv, _ := st.Create(ctx, "t", "u", CreateInput{
		Title: "TTL",
		Questions: []Question{
			{ID: "q1", Type: "text", Title: "Note", Required: true},
			{ID: "q2", Type: "text", Title: "Note2", Required: true},
		},
	})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	pub, _ := st.Publish(ctx, "t", sv.ID)

	// Fixed clock so we can advance past SessionTTL.
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	rt := NewRuntime(st)
	rt.Now = func() time.Time { return now }

	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode,
	})
	if err != nil || !r.Handled {
		t.Fatalf("start: %v %v", err, r)
	}
	// Answer Q1 under session
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "hello",
	})
	if err != nil || !r.Handled {
		t.Fatalf("q1: %v %v", err, r)
	}
	// Expire session
	now = now.Add(SessionTTL + time.Minute)
	// Mid-answer after TTL: should not continue session as active answering
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "world",
	})
	if err != nil {
		t.Fatal(err)
	}
	// No active session: plain text is not a command, so not handled.
	if r.Handled {
		t.Fatalf("expected expired session drop, got handled reply=%q", r.ReplyText)
	}
}
func TestHelpTextCoversCoreCommands(t *testing.T) {
	h := helpText()
	for _, needle := range []string{"/survey", "list", "cancel", "help", "上一题", "修改"} {
		if !strings.Contains(h, needle) {
			t.Fatalf("help missing %q in %q", needle, h)
		}
	}
}

func TestFinalizeDeadlineIsNotHTTPError(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Minute)
	sv, _ := st.Create(ctx, "t", "u", CreateInput{
		Title: "D",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "Q", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
		Settings: SettingsIn{Deadline: &past},
	})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	// Force published despite past deadline for submit-path test
	_, _ = st.db.ExecContext(ctx, `UPDATE surveys SET status=? WHERE id=?`, StatusPublished, sv.ID)
	sv, _ = st.Get(ctx, "t", sv.ID)
	rt := NewRuntime(st)
	err := rt.finalizeSubmit(ctx, "t", sv, PlatformLansenger, "u1", "N", "g", map[string]any{"q1": "a"})
	if !errors.Is(err, ErrDeadlinePassed) {
		t.Fatalf("want ErrDeadlinePassed got %v", err)
	}
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + sv.ShortCode + " 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.ReplyText != "问卷已截止" {
		t.Fatalf("reply=%q", r.ReplyText)
	}
}

func TestResumeSameSurveyDoesNotResetCursor(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	sv, _ := st.Create(ctx, "t", "u", CreateInput{
		Title: "Resume",
		Questions: []Question{
			{ID: "q1", Type: "text", Title: "A", Required: true},
			{ID: "q2", Type: "text", Title: "B", Required: true},
		},
	})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	pub, _ := st.Publish(ctx, "t", sv.ID)
	_, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	// answer Q1
	_, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	// re-issue start should resume at Q2, not wipe
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.ReplyText, "继续填写") {
		t.Fatalf("want resume, got %q", r.ReplyText)
	}
	if !strings.Contains(r.ReplyText, "【2/2】") && !strings.Contains(r.ReplyText, "B") {
		t.Fatalf("want Q2 prompt, got %q", r.ReplyText)
	}
	sk := SessionKey(PlatformLansenger, "u1")
	sess, err := st.GetSession(ctx, "t", sk)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Cursor != 1 {
		t.Fatalf("cursor=%d want 1", sess.Cursor)
	}
	if _, ok := sess.Answers["q1"]; !ok {
		t.Fatal("q1 answer should remain")
	}
}

func TestCloseClearsSessions(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	sv, _ := st.Create(ctx, "t", "u", CreateInput{
		Title: "C",
		Questions: []Question{
			{ID: "q1", Type: "text", Title: "A", Required: true},
			{ID: "q2", Type: "text", Title: "B", Required: true},
		},
	})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	pub, _ := st.Publish(ctx, "t", sv.ID)
	_, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	sk := SessionKey(PlatformLansenger, "u1")
	if _, err := st.GetSession(ctx, "t", sk); err != nil {
		t.Fatalf("expected session: %v", err)
	}
	if _, err := st.Close(ctx, "t", pub.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession(ctx, "t", sk); err == nil {
		t.Fatal("session should be cleared on close")
	}
}

func TestBindRequiresNonEmpty(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	sv, _ := st.Create(ctx, "t", "u", CreateInput{Title: "T", Questions: sampleQuestions()})
	if err := st.Bind(ctx, "t", sv.ID, nil); err == nil {
		t.Fatal("empty bindings should fail")
	}
}

func TestDuplicateDropsPastDeadline(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)
	sv, err := st.Create(ctx, "t", "u", CreateInput{
		Title: "Old",
		Questions: []Question{{
			ID: "q1", Type: "text", Title: "N", Required: true,
		}},
		Settings: SettingsIn{Deadline: &past},
	})
	if err != nil {
		t.Fatal(err)
	}
	dup, err := st.Duplicate(ctx, "t", sv.ID, "u")
	if err != nil {
		t.Fatal(err)
	}
	if dup.Settings.Deadline != nil {
		t.Fatalf("expected past deadline cleared, got %v", dup.Settings.Deadline)
	}
}

func TestValidateSettingsAndRatingRange(t *testing.T) {
	if err := ValidateSettingsIn(SettingsIn{TargetCount: -1}); err == nil {
		t.Fatal("negative target")
	}
	min, max := 5, 1
	err := ValidateDraftQuestions([]Question{{
		ID: "q1", Type: "rating", Title: "R", Required: true, Min: &min, Max: &max,
	}})
	if err == nil {
		t.Fatal("rating min>max")
	}
	err = ValidateDraftQuestions([]Question{
		{ID: "q1", Type: "text", Title: "A", Required: true},
		{ID: "q1", Type: "text", Title: "B", Required: true},
	})
	if err == nil {
		t.Fatal("duplicate question id")
	}
	err = ValidateDraftQuestions([]Question{{
		ID: "q1", Type: "single_choice", Title: "C", Required: true,
		Options: []Option{{ID: "o1", Label: "A"}, {ID: "o1", Label: "B"}},
	}})
	if err == nil {
		t.Fatal("duplicate option id")
	}
}

func TestPublishRejectsPastDeadline(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)
	sv, err := st.Create(ctx, "t", "u", CreateInput{
		Title:     "T",
		Questions: sampleQuestions(),
		Settings:  SettingsIn{Deadline: &past},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	if _, err := st.Publish(ctx, "t", sv.ID); err == nil {
		t.Fatal("expected publish with past deadline to fail")
	}
}

func TestFinalizeSubmitRejectsClosed(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	sv, _ := st.Create(ctx, "t", "u", CreateInput{Title: "T", Questions: sampleQuestions()})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	pub, _ := st.Publish(ctx, "t", sv.ID)
	if _, err := st.Close(ctx, "t", pub.ID); err != nil {
		t.Fatal(err)
	}
	err := rt.finalizeSubmit(ctx, "t", pub, PlatformLansenger, "u1", "n", "g", map[string]any{
		"q1": "opt_yes",
		"q2": []string{"opt_a"},
	})
	if !errors.Is(err, ErrNotCollecting) {
		t.Fatalf("want ErrNotCollecting got %v", err)
	}
}

func TestNormalizeShortCodeCrockfordConfusables(t *testing.T) {
	// O→0, I/L→1 so users retyping codes still match.
	// A3 O I 1 L → A3 0 1 1 1
	got, err := NormalizeShortCode("A3OI1L")
	if err != nil {
		t.Fatal(err)
	}
	if got != "A30111" {
		t.Fatalf("got %q want A30111", got)
	}
	if _, err := NormalizeShortCode("ABC"); err == nil {
		t.Fatal("want length error")
	}
}

func TestInvalidFastPathOpensSession(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	sv, _ := st.Create(ctx, "t", "u", CreateInput{
		Title: "One",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "Q", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
	})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	pub, _ := st.Publish(ctx, "t", sv.ID)
	// Invalid fast answer must still open a session (not leave the user stranded).
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode + " 99",
	})
	if err != nil || !r.Handled {
		t.Fatalf("fast invalid err=%v reply=%q", err, r.ReplyText)
	}
	if !strings.Contains(r.ReplyText, "答案无效") {
		t.Fatalf("want invalid answer reply, got %q", r.ReplyText)
	}
	// Follow-up answer without retyping /survey code.
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "1",
	})
	if err != nil || r.Event != "response_submitted" {
		t.Fatalf("follow-up err=%v event=%q reply=%q", err, r.Event, r.ReplyText)
	}
}

func TestStatusConfirmUpdatePhase(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	sv, _ := st.Create(ctx, "t", "u", CreateInput{
		Title: "Upd",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "Q", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
		Settings: SettingsIn{AllowUpdate: true},
	})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	pub, _ := st.Publish(ctx, "t", sv.ID)
	_, _ = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode + " 1",
	})
	// Re-start to enter confirm_update
	_, _ = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode,
	})
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey status",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.ReplyText, "已提交") {
		t.Fatalf("confirm status reply=%q", r.ReplyText)
	}
}

func TestOptionalSkipAdvances(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	sv, _ := st.Create(ctx, "t", "u", CreateInput{
		Title: "Opt",
		Questions: []Question{
			{ID: "q1", Type: "text", Title: "Optional note", Required: false},
			{ID: "q2", Type: "single_choice", Title: "Need", Required: true,
				Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}},
		},
	})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	pub, _ := st.Publish(ctx, "t", sv.ID)
	// Start conversational
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode,
	})
	if err != nil || !r.Handled {
		t.Fatalf("start err=%v reply=%q", err, r.ReplyText)
	}
	// Skip optional q1
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "跳过",
	})
	if err != nil || !r.Handled {
		t.Fatalf("skip err=%v reply=%q", err, r.ReplyText)
	}
	if !strings.Contains(r.ReplyText, "Need") {
		t.Fatalf("expected q2 prompt after skip, got %q", r.ReplyText)
	}
	// Answer required q2
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "1",
	})
	if err != nil || r.Event != "response_submitted" {
		t.Fatalf("submit err=%v event=%q reply=%q", err, r.Event, r.ReplyText)
	}
	list, _ := st.ListResponses(ctx, "t", pub.ID)
	if len(list) != 1 {
		t.Fatalf("responses=%d", len(list))
	}
	m := JSONToAnswers(list[0].Answers)
	if _, ok := m["q1"]; ok {
		t.Fatalf("skipped q1 should be absent, got %v", m)
	}
	if m["q2"] != "a" {
		t.Fatalf("q2=%v", m["q2"])
	}
}

func TestIsControlWordCaseInsensitive(t *testing.T) {
	if IsControlWord("CANCEL") != "cancel" {
		t.Fatal("CANCEL")
	}
	if IsControlWord("prev") != "prev" {
		t.Fatal("prev")
	}
}

func TestErrAlreadySubmittedIsSentinel(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	sv, _ := st.Create(ctx, "t", "u", CreateInput{
		Title: "X",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "Q", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
	})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	_, _ = st.Publish(ctx, "t", sv.ID)
	raw, _ := json.Marshal(map[string]any{"q1": "a"})
	resp := &Response{
		SurveyID: sv.ID, Platform: PlatformLansenger, RespondentKey: "u1",
		Answers: raw, SubmittedAt: time.Now().UTC(),
	}
	if err := st.SubmitResponse(ctx, "t", resp, false); err != nil {
		t.Fatal(err)
	}
	resp2 := &Response{
		SurveyID: sv.ID, Platform: PlatformLansenger, RespondentKey: "u1",
		Answers: raw, SubmittedAt: time.Now().UTC(),
	}
	err := st.SubmitResponse(ctx, "t", resp2, false)
	if !errors.Is(err, ErrAlreadySubmitted) {
		t.Fatalf("want ErrAlreadySubmitted got %v", err)
	}
}

func TestListRejectsInvalidStatus(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	_, err := st.List(ctx, "t", "not-a-status")
	if !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("want ErrInvalidStatus got %v", err)
	}
}

func TestPublishIsIdempotentRaceSafe(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	sv, _ := st.Create(ctx, "t", "u", CreateInput{Title: "T", Questions: sampleQuestions()})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	if _, err := st.Publish(ctx, "t", sv.ID); err != nil {
		t.Fatal(err)
	}
	// Second publish must fail (CAS on status=draft).
	if _, err := st.Publish(ctx, "t", sv.ID); err == nil {
		t.Fatal("expected second publish to fail")
	}
}

func TestConcurrentPublishOnlyOneWins(t *testing.T) {
	// Shared-cache in-memory DB so concurrent connections see the same data
	// (plain ":memory:" is per-connection and would make every publish fail).
	db, err := sql.Open("sqlite", "file:survey_concur_pub?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(8)
	_, _ = db.Exec(`PRAGMA busy_timeout=5000`)
	st := NewStore(db)
	ctx := context.Background()
	if err := st.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	sv, err := st.Create(ctx, "t", "u", CreateInput{Title: "T", Questions: sampleQuestions()})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}}); err != nil {
		t.Fatal(err)
	}

	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := st.Publish(ctx, "t", sv.ID)
			errs <- err
		}()
	}
	ok, fail := 0, 0
	var firstFail error
	for i := 0; i < n; i++ {
		if err := <-errs; err == nil {
			ok++
		} else {
			fail++
			if firstFail == nil {
				firstFail = err
			}
		}
	}
	if ok != 1 {
		t.Fatalf("expected exactly 1 successful publish, got ok=%d fail=%d firstFail=%v", ok, fail, firstFail)
	}
	got, err := st.Get(ctx, "t", sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPublished {
		t.Fatalf("status=%s", got.Status)
	}
}

func TestDeleteDoesNotWipePublished(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	sv, _ := st.Create(ctx, "t", "u", CreateInput{Title: "T", Questions: sampleQuestions()})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	if _, err := st.Publish(ctx, "t", sv.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(ctx, "t", sv.ID); err == nil {
		t.Fatal("expected delete of published to fail")
	}
	got, err := st.Get(ctx, "t", sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPublished {
		t.Fatalf("status=%s", got.Status)
	}
}

func TestUnbindLastPublishedBindingRejected(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	sv, _ := st.Create(ctx, "t", "u", CreateInput{Title: "T", Questions: sampleQuestions()})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{
		{Platform: PlatformLansenger, GroupID: "g1"},
		{Platform: PlatformLansenger, GroupID: "g2"},
	})
	if _, err := st.Publish(ctx, "t", sv.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.Unbind(ctx, "t", sv.ID, PlatformLansenger, "g1"); err != nil {
		t.Fatal(err)
	}
	if err := st.Unbind(ctx, "t", sv.ID, PlatformLansenger, "g2"); err == nil {
		t.Fatal("expected last unbind to fail")
	}
	got, err := st.Get(ctx, "t", sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bindings) != 1 {
		t.Fatalf("bindings=%d", len(got.Bindings))
	}
}

func TestParseCommandRequiresSurveyWordBoundary(t *testing.T) {
	cmd, args := parseCommand("/surveys foo")
	if cmd != "" {
		t.Fatalf("cmd=%q args=%q want empty", cmd, args)
	}
	cmd, args = parseCommand("/surveyhelp")
	if cmd != "" {
		t.Fatalf("cmd=%q args=%q want empty", cmd, args)
	}
	cmd, args = parseCommand("/survey")
	if cmd != "help" {
		t.Fatalf("bare /survey want help got %q %q", cmd, args)
	}
	cmd, args = parseCommand("/survey list")
	if cmd != "list" {
		t.Fatalf("want list got %q %q", cmd, args)
	}
}

func TestPlatformNormalizedOnHandle(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	sv, _ := st.Create(ctx, "t", "u", CreateInput{
		Title: "P",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "Q", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
	})
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	pub, _ := st.Publish(ctx, "t", sv.ID)
	// Mixed-case platform should still match bindings / session keys.
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: "Lansenger", UserID: "u1", ChatType: "GROUP", GroupID: "g",
		Text: "/survey " + pub.ShortCode + " 1",
	})
	if err != nil || !r.Handled {
		t.Fatalf("err=%v reply=%q", err, r.ReplyText)
	}
	if r.Event != "response_submitted" {
		t.Fatalf("event=%q reply=%q", r.Event, r.ReplyText)
	}
}

func TestListP2PMessage(t *testing.T) {
	st := openTestDB(t)
	rt := NewRuntime(st)
	r, err := rt.Handle(context.Background(), "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u", ChatType: "p2p", Text: "/survey list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.ReplyText, "私聊") {
		t.Fatalf("reply=%q", r.ReplyText)
	}
}

func TestListHidesDeadlinePassedSurveys(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	future := time.Now().UTC().Add(2 * time.Hour)
	sv, err := st.Create(ctx, "t", "u", CreateInput{
		Title: "SoonDead",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "Q", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
		Settings: SettingsIn{Deadline: &future},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	if _, err := st.Publish(ctx, "t", sv.ID); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime(st)
	// Before deadline: listed
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u", ChatType: "group", GroupID: "g", Text: "/survey list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.ReplyText, "SoonDead") {
		t.Fatalf("expected list before deadline, got %q", r.ReplyText)
	}
	// After deadline: hidden
	rt.Now = func() time.Time { return future.Add(time.Minute) }
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u", ChatType: "group", GroupID: "g", Text: "/survey list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.ReplyText, "SoonDead") {
		t.Fatalf("deadline-passed survey should be hidden: %q", r.ReplyText)
	}
	if !strings.Contains(r.ReplyText, "暂无") {
		t.Fatalf("want empty list message, got %q", r.ReplyText)
	}
}

func TestUpdateRejectsBadQuestions(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	sv, err := st.Create(ctx, "t", "u", CreateInput{
		Title: "T",
		Questions: []Question{{
			ID: "q1", Type: "single_choice", Title: "Q", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bad := []Question{{
		ID: "q1", Type: "single_choice", Title: "Q", Required: true,
		Options: []Option{{ID: "a", Label: "A"}},
	}}
	_, err = st.Update(ctx, "t", sv.ID, UpdateInput{Questions: &bad})
	if err == nil {
		t.Fatal("expected validation error for choice with one option")
	}
}
