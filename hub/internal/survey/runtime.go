package survey

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Runtime owns IM handle state machine against Store.
type Runtime struct {
	Store *Store
	Now   func() time.Time
	// lastCleanup throttles session TTL sweeps (every CleanupEvery; default 1m).
	cleanupMu     sync.Mutex
	lastCleanup   time.Time
	CleanupEvery  time.Duration
}

func NewRuntime(store *Store) *Runtime {
	return &Runtime{Store: store, Now: func() time.Time { return time.Now().UTC() }, CleanupEvery: time.Minute}
}

func (r *Runtime) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}

func (r *Runtime) maybeCleanupSessions(ctx context.Context) {
	now := r.now()
	every := r.CleanupEvery
	if every <= 0 {
		every = time.Minute
	}
	r.cleanupMu.Lock()
	if !r.lastCleanup.IsZero() && now.Sub(r.lastCleanup) < every {
		r.cleanupMu.Unlock()
		return
	}
	r.lastCleanup = now
	r.cleanupMu.Unlock()
	// Run outside the lock so concurrent Handle calls are not serialized on DB delete.
	_ = r.Store.CleanupExpiredSessions(ctx, now)
}

// Handle implements POST /api/v1/surveys/im/handle domain logic.
// Text should already be mention-stripped by the gateway when possible.
func (r *Runtime) Handle(ctx context.Context, tenantID string, req IMHandleRequest) (IMHandleResponse, error) {
	r.maybeCleanupSessions(ctx)

	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	if platform == "" {
		platform = PlatformLansenger
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return IMHandleResponse{}, fmt.Errorf("user_id required")
	}
	text := strings.TrimSpace(req.Text)
	sk := SessionKey(platform, userID)

	// Load active session if any
	sess, err := r.Store.GetSession(ctx, tenantID, sk)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return IMHandleResponse{}, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		sess = nil
	}
	if sess != nil && !sess.ExpiresAt.After(r.now()) {
		_ = r.Store.DeleteSession(ctx, tenantID, sk)
		sess = nil
	}

	// Commands that work without session
	if cmd, args := parseCommand(text); cmd != "" {
		switch cmd {
		case "help":
			return IMHandleResponse{Handled: true, ReplyText: helpText()}, nil
		case "list":
			return r.handleList(ctx, tenantID, platform, req)
		case "cancel":
			if sess != nil {
				_ = r.Store.DeleteSession(ctx, tenantID, sk)
				return IMHandleResponse{Handled: true, ReplyText: "已取消当前问卷填写。"}, nil
			}
			return IMHandleResponse{Handled: true, ReplyText: "当前没有进行中的问卷。"}, nil
		case "status":
			return r.handleStatus(ctx, tenantID, sess)
		case "start":
			return r.handleStart(ctx, tenantID, platform, userID, req.UserName, req.ChatType, req.GroupID, args, sess)
		}
	}

	// Active session path
	if sess != nil {
		return r.handleSessionMessage(ctx, tenantID, sess, text)
	}

	// Not a command and no session → not handled (let agent continue)
	return IMHandleResponse{Handled: false}, nil
}

func parseCommand(text string) (cmd string, args string) {
	t := strings.TrimSpace(text)
	low := strings.ToLower(t)

	// /survey ... (case-insensitive; require end or whitespace after prefix so
	// "/surveys" / "/surveyhelp" are not treated as survey commands).
	if strings.HasPrefix(low, "/survey") {
		rest := ""
		if len(t) == 7 {
			// bare "/survey"
			return "help", ""
		}
		if len(t) > 7 {
			if !isASCIISpace(t[7]) {
				// e.g. /surveys — not our command
				goto notSlashSurvey
			}
			rest = strings.TrimSpace(t[7:])
		}
		if rest == "" {
			return "help", ""
		}
		parts := strings.Fields(rest)
		if len(parts) == 0 {
			return "help", ""
		}
		switch strings.ToLower(parts[0]) {
		case "help":
			return "help", ""
		case "list":
			return "list", ""
		case "cancel":
			return "cancel", ""
		case "status":
			return "status", ""
		default:
			// code [answer...]
			return "start", rest
		}
	}
notSlashSurvey:

	// 问卷 CODE / 调查 CODE — must have code token (ASCII or fullwidth space)
	for _, prefix := range []string{"问卷", "调查"} {
		if strings.HasPrefix(t, prefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(t, prefix))
			// TrimSpace handles regular + some unicode spaces after keyword
			if rest == "" {
				return "", "" // bare keyword — do not match
			}
			return "start", rest
		}
	}
	return "", ""
}

func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

func helpText() string {
	return strings.TrimSpace(`问卷帮助：
· @机器人 /survey <短码> — 开始填写（群须绑定且已发布）
· @机器人 /survey <短码> <答案> — 单题快投
· @机器人 问卷 <短码> / 调查 <短码>
· 答题中：直接回复编号或文本；「上一题」回退；「取消」结束
· 选填题可回复「跳过」
· 已提交且允许修改：回复「修改」重答、「取消」退出
· /survey list — 本群已发布问卷
· /survey status — 当前填写进度
· /survey cancel — 取消进行中的填写
· /survey help — 本说明`)
}

func (r *Runtime) handleList(ctx context.Context, tenantID, platform string, req IMHandleRequest) (IMHandleResponse, error) {
	chatType := strings.ToLower(strings.TrimSpace(req.ChatType))
	groupID := strings.TrimSpace(req.GroupID)
	isP2P := chatType == "p2p" || chatType == "private" || (chatType == "" && groupID == "")
	if isP2P {
		return IMHandleResponse{
			Handled:   true,
			ReplyText: "私聊请直接发送 /survey <短码> 开始填写（需问卷开启私聊）。查看绑定群内问卷请在群里发送 /survey list。",
		}, nil
	}
	if groupID == "" {
		return IMHandleResponse{Handled: true, ReplyText: "无法识别群。"}, nil
	}
	list, err := r.Store.ListPublishedForGroup(ctx, tenantID, platform, groupID)
	if err != nil {
		return IMHandleResponse{}, err
	}
	// Hide already-deadline surveys so list does not advertise dead short codes.
	now := r.now()
	active := list[:0]
	for i := range list {
		if DeadlinePassed(list[i].Settings, now) {
			continue
		}
		active = append(active, list[i])
	}
	list = active
	if len(list) == 0 {
		return IMHandleResponse{Handled: true, ReplyText: "本群暂无进行中的问卷。"}, nil
	}
	var b strings.Builder
	b.WriteString("本群问卷：\n")
	for _, s := range list {
		meta := SurveyIntroMeta(&s)
		if meta != "" {
			fmt.Fprintf(&b, "· %s（%s）%s\n", s.Title, s.ShortCode, " — "+meta)
		} else {
			fmt.Fprintf(&b, "· %s（%s）\n", s.Title, s.ShortCode)
		}
	}
	b.WriteString("回复 /survey <短码> 开始填写")
	return IMHandleResponse{Handled: true, ReplyText: b.String()}, nil
}

func (r *Runtime) handleStatus(ctx context.Context, tenantID string, sess *Session) (IMHandleResponse, error) {
	if sess == nil {
		return IMHandleResponse{Handled: true, ReplyText: "当前没有进行中的问卷。"}, nil
	}
	sv, err := r.Store.Get(ctx, tenantID, sess.SurveyID)
	if err != nil {
		return IMHandleResponse{Handled: true, ReplyText: "会话已失效，请重新开始。"}, nil
	}
	if sess.Phase == PhaseConfirmUpdate {
		return IMHandleResponse{Handled: true, ReplyText: fmt.Sprintf("已提交《%s》。回复「修改」可重新作答，或「取消」退出。", sv.Title)}, nil
	}
	n := len(sv.Questions)
	if n == 0 {
		return IMHandleResponse{Handled: true, ReplyText: "会话已失效，请重新开始。"}, nil
	}
	cur := sess.Cursor + 1
	if cur < 1 {
		cur = 1
	}
	if cur > n {
		cur = n
	}
	return IMHandleResponse{Handled: true, ReplyText: fmt.Sprintf("正在填写《%s》，进度 %d/%d。回复「取消」可退出。", sv.Title, cur, n)}, nil
}

func (r *Runtime) handleStart(ctx context.Context, tenantID, platform, userID, userName, chatType, groupID, args string, existing *Session) (IMHandleResponse, error) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return IMHandleResponse{Handled: true, ReplyText: "请提供问卷短码，例如 /survey A3F9K2"}, nil
	}
	code := parts[0]
	fastAnswer := strings.TrimSpace(strings.TrimPrefix(args, parts[0]))

	sv, err := r.Store.GetByCode(ctx, tenantID, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IMHandleResponse{Handled: true, ReplyText: "未找到该问卷短码。"}, nil
		}
		if errors.Is(err, ErrInvalidShortCode) {
			return IMHandleResponse{Handled: true, ReplyText: "短码无效，请使用 6 位合法短码。"}, nil
		}
		// Real storage/infra fault — do not mask as invalid code.
		return IMHandleResponse{}, err
	}
	if sv.Status != StatusPublished {
		return IMHandleResponse{Handled: true, ReplyText: "该问卷未在收集中。"}, nil
	}
	if DeadlinePassed(sv.Settings, r.now()) {
		return IMHandleResponse{Handled: true, ReplyText: "问卷已截止"}, nil
	}

	chatType = strings.ToLower(strings.TrimSpace(chatType))
	isP2P := chatType == "p2p" || chatType == "private" || (chatType == "" && strings.TrimSpace(groupID) == "")
	// p2p gate
	if isP2P && !sv.Settings.AllowP2P {
		return IMHandleResponse{Handled: true, ReplyText: "该问卷仅支持群内填写。"}, nil
	}
	// binding check for group
	if !isP2P {
		if !boundToGroup(sv, platform, groupID) {
			return IMHandleResponse{Handled: true, ReplyText: "该问卷未绑定到本群。"}, nil
		}
	}

	// conflict with another active session
	if existing != nil && existing.SurveyID != sv.ID {
		old, _ := r.Store.Get(ctx, tenantID, existing.SurveyID)
		title := existing.SurveyID
		if old != nil {
			title = old.Title
		}
		return IMHandleResponse{Handled: true, ReplyText: fmt.Sprintf("您正在填写《%s》。回复「取消」结束后再开始新问卷", title)}, nil
	}

	if len(sv.Questions) == 0 {
		return IMHandleResponse{Handled: true, ReplyText: "问卷配置异常：暂无题目。"}, nil
	}

	// Resume same survey in progress instead of wiping answers / re-prompting.
	if existing != nil && existing.SurveyID == sv.ID {
		// Touch TTL so re-issuing /survey <code> does not leave a nearly-expired session.
		existing.ExpiresAt = r.now().Add(SessionTTL)
		existing.UpdatedAt = r.now()
		// Unknown/corrupt phase: recover as answering rather than falling through
		// into a fresh start that would wipe in-progress answers.
		if existing.Phase != PhaseAnswering && existing.Phase != PhaseConfirmUpdate {
			existing.Phase = PhaseAnswering
		}
		if err := r.Store.SaveSession(ctx, existing); err != nil {
			return IMHandleResponse{}, err
		}
		switch existing.Phase {
		case PhaseConfirmUpdate:
			return IMHandleResponse{
				Handled:   true,
				ReplyText: "您已提交。回复「修改」可重新作答，或「取消」退出",
			}, nil
		default: // PhaseAnswering
			cur := existing.Cursor
			if cur < 0 || cur >= len(sv.Questions) {
				cur = 0
			}
			return IMHandleResponse{
				Handled:   true,
				ReplyText: fmt.Sprintf("继续填写《%s》\n\n%s", sv.Title, FormatQuestionPrompt(sv.Questions[cur], cur, len(sv.Questions))),
			}, nil
		}
	}

	key, err := ComputeRespondentKey(sv.Settings.Anonymous, sv.Settings.AnonymitySalt, userID)
	if err != nil {
		return IMHandleResponse{}, err
	}
	has, err := r.Store.HasResponse(ctx, sv.ID, platform, key)
	if err != nil {
		return IMHandleResponse{}, err
	}
	if has && !sv.Settings.AllowUpdate {
		return IMHandleResponse{Handled: true, ReplyText: "您已提交过该问卷，感谢参与"}, nil
	}
	if has && sv.Settings.AllowUpdate {
		// confirm_update phase
		sess := &Session{
			SessionKey: SessionKey(platform, userID),
			TenantID:   tenantID,
			SurveyID:   sv.ID,
			Platform:   platform,
			UserID:     userID,
			UserName:   userName,
			GroupID:    groupID,
			Phase:      PhaseConfirmUpdate,
			Cursor:     0,
			Answers:    map[string]any{},
			ExpiresAt:  r.now().Add(SessionTTL),
			UpdatedAt:  r.now(),
		}
		if err := r.Store.SaveSession(ctx, sess); err != nil {
			return IMHandleResponse{}, err
		}
		return IMHandleResponse{Handled: true, ReplyText: "您已提交。回复「修改」可重新作答，或「取消」退出"}, nil
	}

	// Fast path: single question single_choice/rating with answer token
	if fastAnswer != "" && len(sv.Questions) == 1 {
		q := sv.Questions[0]
		if q.Type == "single_choice" || q.Type == "rating" {
			val, err := ParseAnswer(q, fastAnswer)
			if err == nil {
				answers := map[string]any{q.ID: val}
				if err := r.finalizeSubmit(ctx, tenantID, sv, platform, userID, userName, groupID, answers); err != nil {
					return r.mapSubmitError(ctx, tenantID, "", err)
				}
				return IMHandleResponse{
					Handled:   true,
					ReplyText: "提交成功，感谢参与！",
					SurveyID:  sv.ID,
					Event:     "response_submitted",
				}, nil
			}
			// Invalid fast answer: still open a session so the next reply is accepted.
			sess := newAnsweringSession(tenantID, platform, userID, userName, groupID, sv.ID, r.now())
			if err := r.Store.SaveSession(ctx, sess); err != nil {
				return IMHandleResponse{}, err
			}
			return IMHandleResponse{
				Handled:   true,
				ReplyText: "答案无效：" + err.Error() + "\n" + FormatQuestionPrompt(q, 0, 1),
			}, nil
		}
	}

	// Start conversational at Q1
	sess := newAnsweringSession(tenantID, platform, userID, userName, groupID, sv.ID, r.now())
	if err := r.Store.SaveSession(ctx, sess); err != nil {
		return IMHandleResponse{}, err
	}
	prompt := FormatQuestionPrompt(sv.Questions[0], 0, len(sv.Questions))
	intro := fmt.Sprintf("开始填写《%s》", sv.Title)
	if meta := SurveyIntroMeta(sv); meta != "" {
		intro += "\n" + meta
	}
	return IMHandleResponse{Handled: true, ReplyText: intro + "\n\n" + prompt}, nil
}

func newAnsweringSession(tenantID, platform, userID, userName, groupID, surveyID string, now time.Time) *Session {
	return &Session{
		SessionKey: SessionKey(platform, userID),
		TenantID:   tenantID,
		SurveyID:   surveyID,
		Platform:   platform,
		UserID:     userID,
		UserName:   userName,
		GroupID:    groupID,
		Phase:      PhaseAnswering,
		Cursor:     0,
		Answers:    map[string]any{},
		ExpiresAt:  now.Add(SessionTTL),
		UpdatedAt:  now,
	}
}

// mapSubmitError maps domain submit faults to IM replies (and clears session when set).
func (r *Runtime) mapSubmitError(ctx context.Context, tenantID, sessionKey string, err error) (IMHandleResponse, error) {
	if err == nil {
		return IMHandleResponse{}, nil
	}
	clear := func() {
		if sessionKey != "" {
			_ = r.Store.DeleteSession(ctx, tenantID, sessionKey)
		}
	}
	switch {
	case errors.Is(err, ErrAlreadySubmitted):
		clear()
		return IMHandleResponse{Handled: true, ReplyText: "您已提交过该问卷，感谢参与"}, nil
	case errors.Is(err, ErrDeadlinePassed):
		clear()
		return IMHandleResponse{Handled: true, ReplyText: "问卷已截止"}, nil
	case errors.Is(err, ErrNotCollecting):
		clear()
		return IMHandleResponse{Handled: true, ReplyText: "问卷已停止收集。"}, nil
	default:
		return IMHandleResponse{}, err
	}
}

func boundToGroup(sv *Survey, platform, groupID string) bool {
	groupID = strings.TrimSpace(groupID)
	for _, b := range sv.Bindings {
		if b.Platform == platform && b.GroupID == groupID {
			return true
		}
	}
	return false
}

func (r *Runtime) handleSessionMessage(ctx context.Context, tenantID string, sess *Session, text string) (IMHandleResponse, error) {
	ctrl := IsControlWord(text)

	// Recover corrupt phase values before branching.
	if sess.Phase != PhaseAnswering && sess.Phase != PhaseConfirmUpdate {
		sess.Phase = PhaseAnswering
	}

	if sess.Phase == PhaseConfirmUpdate {
		switch ctrl {
		case "cancel":
			_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
			return IMHandleResponse{Handled: true, ReplyText: "已取消。"}, nil
		case "modify":
			sv, err := r.Store.Get(ctx, tenantID, sess.SurveyID)
			if err != nil {
				_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
				return IMHandleResponse{Handled: true, ReplyText: "问卷不可用。"}, nil
			}
			if sv.Status != StatusPublished {
				_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
				return IMHandleResponse{Handled: true, ReplyText: "问卷已停止收集。"}, nil
			}
			if DeadlinePassed(sv.Settings, r.now()) {
				_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
				return IMHandleResponse{Handled: true, ReplyText: "问卷已截止"}, nil
			}
			if !sv.Settings.AllowUpdate {
				_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
				return IMHandleResponse{Handled: true, ReplyText: "该问卷不允许修改答卷。"}, nil
			}
			if len(sv.Questions) == 0 {
				_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
				return IMHandleResponse{Handled: true, ReplyText: "问卷配置异常：暂无题目。"}, nil
			}
			sess.Phase = PhaseAnswering
			sess.Cursor = 0
			sess.Answers = map[string]any{}
			sess.ExpiresAt = r.now().Add(SessionTTL)
			sess.UpdatedAt = r.now()
			if err := r.Store.SaveSession(ctx, sess); err != nil {
				return IMHandleResponse{}, err
			}
			return IMHandleResponse{Handled: true, ReplyText: FormatQuestionPrompt(sv.Questions[0], 0, len(sv.Questions))}, nil
		default:
			// do not parse as answer
			return IMHandleResponse{Handled: true, ReplyText: "您已提交。回复「修改」可重新作答，或「取消」退出"}, nil
		}
	}

	// answering phase
	if ctrl == "cancel" {
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{Handled: true, ReplyText: "已取消当前问卷填写。"}, nil
	}

	sv, err := r.Store.Get(ctx, tenantID, sess.SurveyID)
	if err != nil {
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{Handled: true, ReplyText: "问卷不可用，已结束会话。"}, nil
	}
	if sv.Status != StatusPublished {
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{Handled: true, ReplyText: "问卷已停止收集。"}, nil
	}
	if DeadlinePassed(sv.Settings, r.now()) {
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{Handled: true, ReplyText: "问卷已截止"}, nil
	}
	if len(sv.Questions) == 0 {
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{Handled: true, ReplyText: "问卷配置异常：暂无题目。"}, nil
	}

	if ctrl == "prev" {
		if sess.Cursor > 0 {
			sess.Cursor--
			// Require re-answer of the question we returned to (avoid stale answers
			// if user later jumps forward without re-typing).
			if sess.Answers != nil && sess.Cursor < len(sv.Questions) {
				delete(sess.Answers, sv.Questions[sess.Cursor].ID)
			}
		}
		sess.ExpiresAt = r.now().Add(SessionTTL)
		sess.UpdatedAt = r.now()
		if err := r.Store.SaveSession(ctx, sess); err != nil {
			return IMHandleResponse{}, err
		}
		return IMHandleResponse{Handled: true, ReplyText: FormatQuestionPrompt(sv.Questions[sess.Cursor], sess.Cursor, len(sv.Questions))}, nil
	}

	// parse as answer for current question
	if sess.Cursor < 0 || sess.Cursor >= len(sv.Questions) {
		sess.Cursor = 0
	}
	q := sv.Questions[sess.Cursor]
	if sess.Answers == nil {
		sess.Answers = map[string]any{}
	}
	// Optional questions: 「跳过」 advances without storing an answer.
	if !q.Required && IsSkipToken(text) {
		delete(sess.Answers, q.ID)
		return r.advanceAfterAnswer(ctx, tenantID, sess, sv)
	}
	if q.Required && IsSkipToken(text) {
		return IMHandleResponse{Handled: true, ReplyText: "该题为必填，不能跳过。\n" + FormatQuestionPrompt(q, sess.Cursor, len(sv.Questions))}, nil
	}
	val, err := ParseAnswer(q, text)
	if err != nil {
		// unparseable while session active: short-circuit with hint (still handled)
		if strings.TrimSpace(text) != "" && !looksLikeAnswer(text) {
			return IMHandleResponse{Handled: true, ReplyText: "当前正在填写问卷。发送「取消」结束问卷后再对话。\n" + FormatQuestionPrompt(q, sess.Cursor, len(sv.Questions))}, nil
		}
		return IMHandleResponse{Handled: true, ReplyText: "答案无效：" + err.Error() + "\n" + FormatQuestionPrompt(q, sess.Cursor, len(sv.Questions))}, nil
	}
	sess.Answers[q.ID] = val
	return r.advanceAfterAnswer(ctx, tenantID, sess, sv)
}

// advanceAfterAnswer increments cursor and either prompts the next question or finalizes.
func (r *Runtime) advanceAfterAnswer(ctx context.Context, tenantID string, sess *Session, sv *Survey) (IMHandleResponse, error) {
	sess.Cursor++
	sess.ExpiresAt = r.now().Add(SessionTTL)
	sess.UpdatedAt = r.now()
	if sess.Cursor >= len(sv.Questions) {
		if err := r.finalizeSubmit(ctx, tenantID, sv, sess.Platform, sess.UserID, sess.UserName, sess.GroupID, sess.Answers); err != nil {
			return r.mapSubmitError(ctx, tenantID, sess.SessionKey, err)
		}
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{
			Handled:   true,
			ReplyText: "提交成功，感谢参与！",
			SurveyID:  sv.ID,
			Event:     "response_submitted",
		}, nil
	}
	if err := r.Store.SaveSession(ctx, sess); err != nil {
		return IMHandleResponse{}, err
	}
	return IMHandleResponse{Handled: true, ReplyText: FormatQuestionPrompt(sv.Questions[sess.Cursor], sess.Cursor, len(sv.Questions))}, nil
}

func looksLikeAnswer(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	// short tokens likely answers
	if len([]rune(t)) <= 40 {
		return true
	}
	return false
}

func (r *Runtime) finalizeSubmit(ctx context.Context, tenantID string, sv *Survey, platform, userID, userName, groupID string, answers map[string]any) error {
	// Re-load for status/deadline/settings so close/archive races cannot accept new answers.
	cur, err := r.Store.Get(ctx, tenantID, sv.ID)
	if err != nil {
		return err
	}
	if cur.Status != StatusPublished {
		return ErrNotCollecting
	}
	if DeadlinePassed(cur.Settings, r.now()) {
		return ErrDeadlinePassed
	}
	key, err := ComputeRespondentKey(cur.Settings.Anonymous, cur.Settings.AnonymitySalt, userID)
	if err != nil {
		return err
	}
	name := userName
	if cur.Settings.Anonymous {
		name = ""
	}
	raw, err := AnswersToJSON(answers)
	if err != nil {
		return err
	}
	resp := &Response{
		SurveyID:       cur.ID,
		TenantID:       tenantID,
		Platform:       platform,
		RespondentKey:  key,
		RespondentName: name,
		GroupID:        groupID,
		Answers:        raw,
		SubmittedAt:    r.now(),
	}
	return r.Store.SubmitResponse(ctx, tenantID, resp, cur.Settings.AllowUpdate)
}

// ComputeStats builds stats from submitted responses.
func ComputeStats(sv *Survey, responses []Response) Stats {
	st := Stats{
		SurveyID:      sv.ID,
		ResponseCount: len(responses),
		TargetCount:   sv.Settings.TargetCount,
	}
	for _, q := range sv.Questions {
		qs := QuestionStats{QuestionID: q.ID, Title: q.Title, Type: q.Type}
		switch q.Type {
		case "single_choice", "multi_choice":
			counts := map[string]int{}
			for _, o := range q.Options {
				counts[o.ID] = 0
			}
			for _, resp := range responses {
				m := JSONToAnswers(resp.Answers)
				v, ok := m[q.ID]
				if !ok {
					continue
				}
				switch q.Type {
				case "single_choice":
					if id, ok := v.(string); ok {
						counts[id]++
					}
				case "multi_choice":
					switch arr := v.(type) {
					case []any:
						for _, x := range arr {
							if id, ok := x.(string); ok {
								counts[id]++
							}
						}
					case []string:
						for _, id := range arr {
							counts[id]++
						}
					}
				}
			}
			denom := float64(len(responses))
			for _, o := range q.Options {
				c := counts[o.ID]
				pct := 0.0
				if denom > 0 {
					pct = float64(c) / denom * 100
				}
				qs.Options = append(qs.Options, OptionCount{OptionID: o.ID, Label: o.Label, Count: c, Percent: pct})
			}
		case "rating":
			var sum float64
			var n int
			for _, resp := range responses {
				m := JSONToAnswers(resp.Answers)
				v, ok := m[q.ID]
				if !ok {
					continue
				}
				var f float64
				switch t := v.(type) {
				case float64:
					f = t
				case json.Number:
					f, _ = t.Float64()
				case int:
					f = float64(t)
				default:
					continue
				}
				sum += f
				n++
			}
			qs.RatingN = n
			if n > 0 {
				qs.RatingAvg = sum / float64(n)
			}
		case "text":
			for _, resp := range responses {
				m := JSONToAnswers(resp.Answers)
				if _, ok := m[q.ID]; ok {
					qs.TextCount++
				}
			}
		}
		st.ByQuestion = append(st.ByQuestion, qs)
	}
	return st
}

// FormatAnswerCell formats a stored answer for export using option position order for multi.
func FormatAnswerCell(q Question, v any) string {
	if v == nil {
		return ""
	}
	switch q.Type {
	case "single_choice":
		if id, ok := v.(string); ok {
			return OptionLabelByID(q, id)
		}
	case "multi_choice":
		var ids []string
		switch arr := v.(type) {
		case []any:
			for _, x := range arr {
				if id, ok := x.(string); ok {
					ids = append(ids, id)
				}
			}
		case []string:
			ids = arr
		case string:
			var parsed []any
			if err := json.Unmarshal([]byte(arr), &parsed); err == nil {
				for _, x := range parsed {
					if id, ok := x.(string); ok {
						ids = append(ids, id)
					}
				}
			} else if arr != "" {
				ids = []string{arr}
			}
		}
		return strings.Join(MultiLabelsInOptionOrder(q, ids), ", ")
	case "rating":
		return fmt.Sprint(v)
	case "text":
		return fmt.Sprint(v)
	}
	return fmt.Sprint(v)
}
