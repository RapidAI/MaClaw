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

	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

// Runtime owns IM handle state machine against Store.
type Runtime struct {
	Store *Store
	Now   func() time.Time
	// lastCleanup throttles session TTL sweeps (every CleanupEvery; default 1m).
	cleanupMu    sync.Mutex
	lastCleanup  time.Time
	CleanupEvery time.Duration
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
	lang := i18n.NormalizeLang(req.Lang)
	// Group chats get a group-scoped session key so one user can run
	// independent sessions in different groups; p2p keeps the legacy key.
	sk := IMSessionKey(platform, req.ChatType, req.GroupID, userID)

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
			return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgHelp)}, nil
		case "list":
			return r.handleList(ctx, tenantID, platform, req, lang)
		case "cancel":
			if sess != nil {
				_ = r.Store.DeleteSession(ctx, tenantID, sk)
				return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgCancelDone), Event: EventSessionEnded}, nil
			}
			return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgNoActiveSurvey), Event: EventSessionEnded}, nil
		case "status":
			return r.handleStatus(ctx, tenantID, sess, lang)
		case "start":
			return r.handleStart(ctx, tenantID, platform, userID, req.UserName, req.ChatType, req.GroupID, args, sk, lang, sess)
		}
	}

	// Active session path
	if sess != nil {
		return r.handleSessionMessage(ctx, tenantID, sess, text, lang)
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

func (r *Runtime) handleList(ctx context.Context, tenantID, platform string, req IMHandleRequest, lang string) (IMHandleResponse, error) {
	chatType := strings.ToLower(strings.TrimSpace(req.ChatType))
	groupID := strings.TrimSpace(req.GroupID)
	isP2P := chatType == "p2p" || chatType == "private" || (chatType == "" && groupID == "")
	if isP2P {
		return IMHandleResponse{
			Handled:   true,
			ReplyText: tr(lang, msgListP2P),
		}, nil
	}
	if groupID == "" {
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgListNoGroup)}, nil
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
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgListEmpty)}, nil
	}
	var b strings.Builder
	b.WriteString(tr(lang, msgListHeader))
	for _, s := range list {
		meta := SurveyIntroMeta(&s, lang)
		if meta != "" {
			b.WriteString(tr(lang, msgListItemMeta, s.Title, s.ShortCode, meta))
		} else {
			b.WriteString(tr(lang, msgListItem, s.Title, s.ShortCode))
		}
	}
	b.WriteString(tr(lang, msgListFooter))
	return IMHandleResponse{Handled: true, ReplyText: b.String()}, nil
}

func (r *Runtime) handleStatus(ctx context.Context, tenantID string, sess *Session, lang string) (IMHandleResponse, error) {
	if sess == nil {
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgNoActiveSurvey), Event: EventSessionEnded}, nil
	}
	sv, err := r.Store.Get(ctx, tenantID, sess.SurveyID)
	if err != nil {
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgSessionExpired), Event: EventSessionEnded}, nil
	}
	if sess.Phase == PhaseConfirmUpdate {
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgStatusSubmitted, sv.Title), Event: EventSessionActive}, nil
	}
	n := len(sv.Questions)
	if n == 0 {
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgSessionExpired), Event: EventSessionEnded}, nil
	}
	cur := sess.Cursor + 1
	if cur < 1 {
		cur = 1
	}
	if cur > n {
		cur = n
	}
	return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgStatusProgress, sv.Title, cur, n), Event: EventSessionActive}, nil
}

func (r *Runtime) handleStart(ctx context.Context, tenantID, platform, userID, userName, chatType, groupID, args, sessionKey, lang string, existing *Session) (IMHandleResponse, error) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgNeedCode)}, nil
	}
	code := parts[0]
	// Prefer field-join over TrimPrefix(args, code): leading/odd whitespace after
	// Fields-normalize would leave the whole args string as a false "answer".
	fastAnswer := ""
	if len(parts) > 1 {
		fastAnswer = strings.Join(parts[1:], " ")
	}

	sv, err := r.Store.GetByCode(ctx, tenantID, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgCodeNotFound)}, nil
		}
		if errors.Is(err, ErrInvalidShortCode) {
			return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgCodeInvalid)}, nil
		}
		// Real storage/infra fault — do not mask as invalid code.
		return IMHandleResponse{}, err
	}
	if sv.Status != StatusPublished {
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgNotCollecting)}, nil
	}
	if DeadlinePassed(sv.Settings, r.now()) {
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgDeadlinePassed), Event: EventSessionEnded}, nil
	}

	chatType = strings.ToLower(strings.TrimSpace(chatType))
	isP2P := chatType == "p2p" || chatType == "private" || (chatType == "" && strings.TrimSpace(groupID) == "")
	// p2p gate
	if isP2P && !sv.Settings.AllowP2P {
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgGroupOnly)}, nil
	}
	// binding check for group
	if !isP2P {
		if !boundToGroup(sv, platform, groupID) {
			return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgNotBoundGroup)}, nil
		}
	}

	// conflict with another active session
	if existing != nil && existing.SurveyID != sv.ID {
		old, _ := r.Store.Get(ctx, tenantID, existing.SurveyID)
		title := existing.SurveyID
		if old != nil {
			title = old.Title
		}
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgBusyOtherSurvey, title), Event: EventSessionActive}, nil
	}

	if len(sv.Questions) == 0 {
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgNoQuestions)}, nil
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
				ReplyText: tr(lang, msgSubmittedEditable),
				Event:     EventSessionActive,
			}, nil
		default: // PhaseAnswering
			// `/survey CODE 1` while mid-session answers the current question
			// instead of only re-printing the prompt (common when re-@ the bot).
			if fastAnswer != "" {
				return r.handleSessionMessage(ctx, tenantID, existing, fastAnswer, lang)
			}
			cur := existing.Cursor
			if cur < 0 || cur >= len(sv.Questions) {
				cur = 0
			}
			return IMHandleResponse{
				Handled:   true,
				ReplyText: tr(lang, msgResumeIntro, sv.Title, FormatQuestionPrompt(sv.Questions[cur], cur, len(sv.Questions), lang)),
				Event:     EventSessionActive,
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
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgAlreadySubmitted), Event: EventSessionEnded}, nil
	}
	if has && sv.Settings.AllowUpdate {
		// confirm_update phase
		sess := &Session{
			SessionKey: sessionKey,
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
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgSubmittedEditable), Event: EventSessionActive}, nil
	}

	// Fast path: single-question survey with answer token (any supported type).
	if fastAnswer != "" && len(sv.Questions) == 1 {
		q := sv.Questions[0]
		if IsSkipToken(fastAnswer) {
			if q.Required {
				sess := newAnsweringSession(sessionKey, tenantID, platform, userID, userName, groupID, sv.ID, r.now())
				if err := r.Store.SaveSession(ctx, sess); err != nil {
					return IMHandleResponse{}, err
				}
				return IMHandleResponse{
					Handled:   true,
					ReplyText: tr(lang, msgRequiredNoSkip) + FormatQuestionPrompt(q, 0, 1, lang),
					Event:     EventSessionActive,
				}, nil
			}
			if err := r.finalizeSubmit(ctx, tenantID, sv, platform, userID, userName, groupID, map[string]any{}); err != nil {
				return r.mapSubmitError(ctx, tenantID, "", err, lang)
			}
			return IMHandleResponse{
				Handled:   true,
				ReplyText: tr(lang, msgSubmitOK),
				SurveyID:  sv.ID,
				Event:     EventResponseSubmitted,
			}, nil
		}
		val, err := ParseAnswer(q, fastAnswer)
		if err == nil {
			if err := r.finalizeSubmit(ctx, tenantID, sv, platform, userID, userName, groupID, map[string]any{q.ID: val}); err != nil {
				return r.mapSubmitError(ctx, tenantID, "", err, lang)
			}
			return IMHandleResponse{
				Handled:   true,
				ReplyText: tr(lang, msgSubmitOK),
				SurveyID:  sv.ID,
				Event:     EventResponseSubmitted,
			}, nil
		}
		// Invalid fast answer: still open a session so the next reply is accepted.
		sess := newAnsweringSession(sessionKey, tenantID, platform, userID, userName, groupID, sv.ID, r.now())
		if err := r.Store.SaveSession(ctx, sess); err != nil {
			return IMHandleResponse{}, err
		}
		return IMHandleResponse{
			Handled:   true,
			ReplyText: tr(lang, msgInvalidAnswer) + LocalizedAnswerError(q, err, lang) + "\n" + FormatQuestionPrompt(q, 0, 1, lang),
			Event:     EventSessionActive,
		}, nil
	}

	// Start conversational at Q1. If the user already provided an answer token
	// (e.g. `/survey CODE 1` on a multi-question survey), apply it immediately
	// so they do not need an extra round trip for the first question.
	sess := newAnsweringSession(sessionKey, tenantID, platform, userID, userName, groupID, sv.ID, r.now())
	if err := r.Store.SaveSession(ctx, sess); err != nil {
		return IMHandleResponse{}, err
	}
	if fastAnswer != "" {
		return r.handleSessionMessage(ctx, tenantID, sess, fastAnswer, lang)
	}
	prompt := FormatQuestionPrompt(sv.Questions[0], 0, len(sv.Questions), lang)
	intro := tr(lang, msgStartIntro, sv.Title)
	if meta := SurveyIntroMeta(sv, lang); meta != "" {
		intro += "\n" + meta
	}
	return IMHandleResponse{Handled: true, ReplyText: intro + "\n\n" + prompt, Event: EventSessionActive}, nil
}

func newAnsweringSession(sessionKey, tenantID, platform, userID, userName, groupID, surveyID string, now time.Time) *Session {
	return &Session{
		SessionKey: sessionKey,
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
func (r *Runtime) mapSubmitError(ctx context.Context, tenantID, sessionKey string, err error, lang string) (IMHandleResponse, error) {
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
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgAlreadySubmitted), Event: EventSessionEnded}, nil
	case errors.Is(err, ErrDeadlinePassed):
		clear()
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgDeadlinePassed), Event: EventSessionEnded}, nil
	case errors.Is(err, ErrNotCollecting):
		clear()
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgStoppedCollecting), Event: EventSessionEnded}, nil
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

func (r *Runtime) handleSessionMessage(ctx context.Context, tenantID string, sess *Session, text, lang string) (IMHandleResponse, error) {
	ctrl := IsControlWord(text)

	// Recover corrupt phase values before branching.
	if sess.Phase != PhaseAnswering && sess.Phase != PhaseConfirmUpdate {
		sess.Phase = PhaseAnswering
	}

	if sess.Phase == PhaseConfirmUpdate {
		switch ctrl {
		case "cancel":
			_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
			return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgCancelled), Event: EventSessionEnded}, nil
		case "modify":
			sv, err := r.Store.Get(ctx, tenantID, sess.SurveyID)
			if err != nil {
				_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
				return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgSurveyUnavailable), Event: EventSessionEnded}, nil
			}
			if sv.Status != StatusPublished {
				_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
				return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgStoppedCollecting), Event: EventSessionEnded}, nil
			}
			if DeadlinePassed(sv.Settings, r.now()) {
				_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
				return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgDeadlinePassed), Event: EventSessionEnded}, nil
			}
			if !sv.Settings.AllowUpdate {
				_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
				return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgNoModifyAllowed), Event: EventSessionEnded}, nil
			}
			if len(sv.Questions) == 0 {
				_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
				return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgNoQuestions), Event: EventSessionEnded}, nil
			}
			sess.Phase = PhaseAnswering
			sess.Cursor = 0
			sess.Answers = map[string]any{}
			sess.ExpiresAt = r.now().Add(SessionTTL)
			sess.UpdatedAt = r.now()
			if err := r.Store.SaveSession(ctx, sess); err != nil {
				return IMHandleResponse{}, err
			}
			return IMHandleResponse{Handled: true, ReplyText: FormatQuestionPrompt(sv.Questions[0], 0, len(sv.Questions), lang), Event: EventSessionActive}, nil
		default:
			// do not parse as answer
			return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgSubmittedEditable), Event: EventSessionActive}, nil
		}
	}

	// answering phase
	if ctrl == "cancel" {
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgCancelDone), Event: EventSessionEnded}, nil
	}

	sv, err := r.Store.Get(ctx, tenantID, sess.SurveyID)
	if err != nil {
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgSurveyGoneEnded), Event: EventSessionEnded}, nil
	}
	if sv.Status != StatusPublished {
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgStoppedCollecting), Event: EventSessionEnded}, nil
	}
	if DeadlinePassed(sv.Settings, r.now()) {
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgDeadlinePassed), Event: EventSessionEnded}, nil
	}
	if len(sv.Questions) == 0 {
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgNoQuestions), Event: EventSessionEnded}, nil
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
		return IMHandleResponse{Handled: true, ReplyText: FormatQuestionPrompt(sv.Questions[sess.Cursor], sess.Cursor, len(sv.Questions), lang), Event: EventSessionActive}, nil
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
		return r.advanceAfterAnswer(ctx, tenantID, sess, sv, lang)
	}
	if q.Required && IsSkipToken(text) {
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgRequiredNoSkip) + FormatQuestionPrompt(q, sess.Cursor, len(sv.Questions), lang), Event: EventSessionActive}, nil
	}
	val, err := ParseAnswer(q, text)
	if err != nil {
		// unparseable while session active: short-circuit with hint (still handled)
		if strings.TrimSpace(text) != "" && !looksLikeAnswer(text) {
			return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgBusyAnswering) + FormatQuestionPrompt(q, sess.Cursor, len(sv.Questions), lang), Event: EventSessionActive}, nil
		}
		return IMHandleResponse{Handled: true, ReplyText: tr(lang, msgInvalidAnswer) + LocalizedAnswerError(q, err, lang) + "\n" + FormatQuestionPrompt(q, sess.Cursor, len(sv.Questions), lang), Event: EventSessionActive}, nil
	}
	sess.Answers[q.ID] = val
	return r.advanceAfterAnswer(ctx, tenantID, sess, sv, lang)
}

// advanceAfterAnswer increments cursor and either prompts the next question or finalizes.
func (r *Runtime) advanceAfterAnswer(ctx context.Context, tenantID string, sess *Session, sv *Survey, lang string) (IMHandleResponse, error) {
	sess.Cursor++
	sess.ExpiresAt = r.now().Add(SessionTTL)
	sess.UpdatedAt = r.now()
	if sess.Cursor >= len(sv.Questions) {
		if err := r.finalizeSubmit(ctx, tenantID, sv, sess.Platform, sess.UserID, sess.UserName, sess.GroupID, sess.Answers); err != nil {
			return r.mapSubmitError(ctx, tenantID, sess.SessionKey, err, lang)
		}
		_ = r.Store.DeleteSession(ctx, tenantID, sess.SessionKey)
		return IMHandleResponse{
			Handled:   true,
			ReplyText: tr(lang, msgSubmitOK),
			SurveyID:  sv.ID,
			Event:     EventResponseSubmitted,
		}, nil
	}
	if err := r.Store.SaveSession(ctx, sess); err != nil {
		return IMHandleResponse{}, err
	}
	return IMHandleResponse{Handled: true, ReplyText: FormatQuestionPrompt(sv.Questions[sess.Cursor], sess.Cursor, len(sv.Questions), lang), Event: EventSessionActive}, nil
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
