package survey

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// publishIMTestSurvey creates+binds+publishes a survey for IM handle tests.
func publishIMTestSurvey(t *testing.T, st *Store, title string, groupIDs []string, qs []Question) *Survey {
	t.Helper()
	ctx := context.Background()
	sv, err := st.Create(ctx, "t", "u", CreateInput{Title: title, Questions: qs})
	if err != nil {
		t.Fatal(err)
	}
	var bindings []Binding
	for _, g := range groupIDs {
		bindings = append(bindings, Binding{Platform: PlatformLansenger, GroupID: g})
	}
	if err := st.Bind(ctx, "t", sv.ID, bindings); err != nil {
		t.Fatal(err)
	}
	pub, err := st.Publish(ctx, "t", sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func singleChoiceQuestions() []Question {
	return []Question{{
		ID: "q1", Type: "single_choice", Title: "Pick one", Required: true,
		Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
	}}
}

func TestIMEnglishReplies(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	pub := publishIMTestSurvey(t, st, "Lunch", []string{"g"}, singleChoiceQuestions())

	// help
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey help", Lang: "en",
	})
	if err != nil || !strings.Contains(r.ReplyText, "Survey help") {
		t.Fatalf("help err=%v reply=%q", err, r.ReplyText)
	}

	// start → English intro + prompt, and Q1 must NOT offer "prev"
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode, Lang: "en-US",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.ReplyText, "Starting \"Lunch\"") || !strings.Contains(r.ReplyText, "[1/1]") {
		t.Fatalf("en start reply=%q", r.ReplyText)
	}
	if strings.Contains(r.ReplyText, "prev") {
		t.Fatalf("en Q1 must not offer prev: %q", r.ReplyText)
	}
	if r.Event != EventSessionActive {
		t.Fatalf("en start event=%q", r.Event)
	}

	// invalid choice → localized error wrapper
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "9", Lang: "en",
	})
	if err != nil || !strings.Contains(r.ReplyText, "Invalid answer: option index out of range") {
		t.Fatalf("en invalid reply err=%v reply=%q", err, r.ReplyText)
	}

	// answer → submit success
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "1", Lang: "en",
	})
	if err != nil || r.ReplyText != "Submitted successfully. Thank you!" {
		t.Fatalf("en submit err=%v reply=%q", err, r.ReplyText)
	}
	if r.Event != EventResponseSubmitted || r.SurveyID == "" {
		t.Fatalf("en submit event=%q sid=%q", r.Event, r.SurveyID)
	}

	// cancel with no session → English terminal message
	r, err = rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey cancel", Lang: "en",
	})
	if err != nil || r.ReplyText != "No survey in progress." || r.Event != EventSessionEnded {
		t.Fatalf("en cancel err=%v reply=%q event=%q", err, r.ReplyText, r.Event)
	}
}

// TestIMGroupSessionIsolation: same user, two groups → independent sessions.
func TestIMGroupSessionIsolation(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	twoQ := []Question{
		{ID: "q1", Type: "text", Title: "A", Required: true},
		{ID: "q2", Type: "text", Title: "B", Required: true},
	}
	pubA := publishIMTestSurvey(t, st, "SurveyA", []string{"g1"}, twoQ)
	pubB := publishIMTestSurvey(t, st, "SurveyB", []string{"g2"}, twoQ)

	// Start survey A in g1 and answer Q1.
	if _, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g1",
		Text: "/survey " + pubA.ShortCode,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g1",
		Text: "ans-g1",
	}); err != nil {
		t.Fatal(err)
	}

	// Same user starts survey B in g2 — must NOT hit the "busy with A" conflict.
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g2",
		Text: "/survey " + pubB.ShortCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.ReplyText, "开始填写") {
		t.Fatalf("want fresh start in g2, got %q", r.ReplyText)
	}

	// g1 session is still at Q2 with its answer intact.
	sk1 := IMSessionKey(PlatformLansenger, "group", "g1", "u1")
	sess1, err := st.GetSession(ctx, "t", sk1)
	if err != nil {
		t.Fatal(err)
	}
	if sess1.Cursor != 1 || sess1.Answers["q1"] != "ans-g1" {
		t.Fatalf("g1 session disturbed: cursor=%d answers=%v", sess1.Cursor, sess1.Answers)
	}
	// Sessions are stored under distinct keys.
	sk2 := IMSessionKey(PlatformLansenger, "group", "g2", "u1")
	if sk1 == sk2 {
		t.Fatalf("group session keys must differ: %q", sk1)
	}
	if _, err := st.GetSession(ctx, "t", sk2); err != nil {
		t.Fatalf("g2 session missing: %v", err)
	}
	// p2p key stays legacy-compatible.
	if got := IMSessionKey(PlatformLansenger, "p2p", "", "u1"); got != SessionKey(PlatformLansenger, "u1") {
		t.Fatalf("p2p key=%q want legacy %q", got, SessionKey(PlatformLansenger, "u1"))
	}
}

// TestIMEventsDrivesGatewayHints: terminal replies carry session_ended,
// in-progress replies session_active, submit response_submitted.
func TestIMEventsDrivesGatewayHints(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	twoQ := []Question{
		{ID: "q1", Type: "single_choice", Title: "A", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}},
		{ID: "q2", Type: "single_choice", Title: "B", Required: true,
			Options: []Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}},
	}
	pub := publishIMTestSurvey(t, st, "Ev", []string{"g"}, twoQ)
	call := func(text string) IMHandleResponse {
		t.Helper()
		r, err := rt.Handle(ctx, "t", IMHandleRequest{
			Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g", Text: text,
		})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	if r := call("/survey " + pub.ShortCode); r.Event != EventSessionActive {
		t.Fatalf("start event=%q", r.Event)
	}
	if r := call("/survey status"); r.Event != EventSessionActive {
		t.Fatalf("status event=%q", r.Event)
	}
	if r := call("9"); r.Event != EventSessionActive || !strings.Contains(r.ReplyText, "答案无效") {
		t.Fatalf("invalid answer event=%q reply=%q", r.Event, r.ReplyText)
	}
	if r := call("1"); r.Event != EventSessionActive {
		t.Fatalf("answer Q1 event=%q", r.Event)
	}
	if r := call("2"); r.Event != EventResponseSubmitted || r.SurveyID == "" {
		t.Fatalf("submit event=%q sid=%q", r.Event, r.SurveyID)
	}
	// New session, then cancel → session_ended.
	if r := call("/survey " + pub.ShortCode); r.Event != EventSessionEnded {
		// Already submitted & AllowUpdate=false → already-submitted terminal reply.
		t.Fatalf("re-start after submit event=%q", r.Event)
	}
	if r := call("/survey cancel"); r.Event != EventSessionEnded {
		t.Fatalf("cancel event=%q", r.Event)
	}
}

// TestIMDeadlineEventEnded: starting an expired survey is terminal.
func TestIMDeadlineEventEnded(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	past := time.Now().UTC().Add(-time.Minute)
	sv, err := st.Create(ctx, "t", "u", CreateInput{
		Title:     "D",
		Questions: singleChoiceQuestions(),
		Settings:  SettingsIn{Deadline: &past},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Bind(ctx, "t", sv.ID, []Binding{{Platform: PlatformLansenger, GroupID: "g"}})
	// Force published despite past deadline (Publish validates deadline).
	if _, err := st.db.ExecContext(ctx, `UPDATE surveys SET status=? WHERE id=?`, StatusPublished, sv.ID); err != nil {
		t.Fatal(err)
	}
	pub, err := st.Get(ctx, "t", sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Event != EventSessionEnded {
		t.Fatalf("deadline start event=%q reply=%q", r.Event, r.ReplyText)
	}
}

func TestLocalizedAnswerErrorSentinels(t *testing.T) {
	rq := Question{ID: "q1", Type: "rating", Title: "R", Required: true}
	if _, err := ParseAnswer(rq, "9"); !errors.Is(err, ErrRatingOutOfRange) {
		t.Fatalf("want ErrRatingOutOfRange got %v", err)
	}
	if msg := LocalizedAnswerError(rq, ErrRatingOutOfRange, "en"); !strings.Contains(msg, "[1,5]") {
		t.Fatalf("en rating msg=%q", msg)
	}
	if msg := LocalizedAnswerError(rq, ErrRatingOutOfRange, "zh"); !strings.Contains(msg, "评分超出范围") {
		t.Fatalf("zh rating msg=%q", msg)
	}
	if _, err := ParseAnswer(singleChoiceQuestions()[0], "7"); !errors.Is(err, ErrOptionIndexOutOfRange) {
		t.Fatalf("want ErrOptionIndexOutOfRange got %v", err)
	}
	if msg := LocalizedAnswerError(singleChoiceQuestions()[0], ErrOptionIndexOutOfRange, "zh"); msg != "选项序号超出范围" {
		t.Fatalf("zh option msg=%q", msg)
	}
}

// TestIMFastAnswerExtractionWithOddSpacing: multi-space between code and answer
// must still parse the answer token (not re-include the short code).
func TestIMFastAnswerExtractionWithOddSpacing(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	pub := publishIMTestSurvey(t, st, "SpaceFast", []string{"g"}, singleChoiceQuestions())
	// Extra spaces between code and answer (Fields-normalized).
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey  " + pub.ShortCode + "   1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Event != EventResponseSubmitted {
		t.Fatalf("odd-spacing fast path event=%q reply=%q", r.Event, r.ReplyText)
	}
}

// TestIMMultiQuestionFastAnswerOnStart: `/survey CODE 1` on a multi-question
// survey applies Q1 immediately and prompts Q2 (no wasted round trip).
func TestIMMultiQuestionFastAnswerOnStart(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	rt := NewRuntime(st)
	twoQ := []Question{
		{ID: "q1", Type: "single_choice", Title: "A", Required: true,
			Options: []Option{{ID: "a", Label: "是"}, {ID: "b", Label: "否"}}},
		{ID: "q2", Type: "single_choice", Title: "B", Required: true,
			Options: []Option{{ID: "a", Label: "是"}, {ID: "b", Label: "否"}}},
	}
	pub := publishIMTestSurvey(t, st, "MultiFast", []string{"g"}, twoQ)
	r, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode + " 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Event != EventSessionActive {
		t.Fatalf("event=%q want session_active", r.Event)
	}
	if !strings.Contains(r.ReplyText, "【2/2】") && !strings.Contains(r.ReplyText, "[2/2]") {
		t.Fatalf("want Q2 prompt after fast Q1 answer, reply=%q", r.ReplyText)
	}
	// Resume with another fast answer should complete.
	r2, err := rt.Handle(ctx, "t", IMHandleRequest{
		Platform: PlatformLansenger, UserID: "u1", ChatType: "group", GroupID: "g",
		Text: "/survey " + pub.ShortCode + " 2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Event != EventResponseSubmitted {
		t.Fatalf("resume+answer event=%q reply=%q", r2.Event, r2.ReplyText)
	}
}

// TestIMFirstQuestionHidesPrevHint: Q1 prompt offers cancel only; Q2 adds prev.
func TestIMFirstQuestionHidesPrevHint(t *testing.T) {
	q := singleChoiceQuestions()[0]
	p1 := FormatQuestionPrompt(q, 0, 2, "zh")
	if strings.Contains(p1, "上一题") {
		t.Fatalf("Q1 must not mention 上一题: %q", p1)
	}
	if !strings.Contains(p1, "取消") {
		t.Fatalf("Q1 must keep cancel hint: %q", p1)
	}
	p2 := FormatQuestionPrompt(q, 1, 2, "zh")
	if !strings.Contains(p2, "上一题") {
		t.Fatalf("Q2 must mention 上一题: %q", p2)
	}
	en1 := FormatQuestionPrompt(q, 0, 2, "en")
	if strings.Contains(en1, "prev") || !strings.Contains(en1, "cancel") {
		t.Fatalf("en Q1 hint wrong: %q", en1)
	}
	en2 := FormatQuestionPrompt(q, 1, 2, "en")
	if !strings.Contains(en2, "prev") {
		t.Fatalf("en Q2 hint wrong: %q", en2)
	}
}
