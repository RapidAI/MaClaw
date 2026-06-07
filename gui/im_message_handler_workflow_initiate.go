package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

type WorkflowInitiationHandler struct {
	app      *App
	mu       sync.Mutex
	sessions map[string]*initiationSession
}
type initiationSession struct {
	UserID        string
	WorkflowID    string
	WorkflowName  string
	Schema        []initiationFormField
	ExtractedData map[string]interface{}
	MissingFields []string
	Confirmed     bool
	CreatedAt     time.Time
}
type initiationFormField struct {
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	Type      string   `json:"type"`
	Required  bool     `json:"required"`
	MaxLength int      `json:"max_length,omitempty"`
	Options   []string `json:"options,omitempty"`
}
type WorkflowMatch struct {
	WorkflowID   string
	WorkflowName string
	Schema       []initiationFormField
	Confidence   float64
}

func NewWorkflowInitiationHandler(app *App) *WorkflowInitiationHandler {
	return &WorkflowInitiationHandler{app: app, sessions: make(map[string]*initiationSession)}
}
func (h *WorkflowInitiationHandler) lang() string {
	if h == nil || h.app == nil {
		return ""
	}
	return i18n.NormalizeLang(h.app.CurrentLanguage)
}
func (h *WorkflowInitiationHandler) getSession(userID string) *initiationSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[userID]
}
func (h *WorkflowInitiationHandler) setSession(userID string, s *initiationSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[userID] = s
}
func (h *WorkflowInitiationHandler) deleteSession(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, userID)
}

type publishedWorkflowInfo struct {
	WorkflowID   string
	WorkflowName string
	Schema       []initiationFormField
}

func (h *WorkflowInitiationHandler) HandleInitiationIntent(ctx context.Context, userID, message string) (*IMAgentResponse, error) {
	session := h.getSession(userID)
	if session != nil {
		if len(session.ExtractedData) > 0 && len(session.MissingFields) == 0 && !session.Confirmed {
			return h.handleConfirmation(ctx, userID, message, session)
		}
		if len(session.MissingFields) > 0 {
			return h.handleMissingFieldsResponse(ctx, userID, message, session)
		}
	}
	match, err := h.matchWorkflowByMessage(ctx, message)
	if err != nil {
		return nil, err
	}
	if match == nil {
		return h.buildNoMatchResponse(ctx)
	}
	session = &initiationSession{UserID: userID, WorkflowID: match.WorkflowID, WorkflowName: match.WorkflowName, Schema: match.Schema, ExtractedData: make(map[string]interface{}), CreatedAt: time.Now()}
	h.extractFieldsFromMessage(session, message)
	session.MissingFields = h.findMissingRequiredFields(session)
	h.setSession(userID, session)
	if len(session.MissingFields) == 0 {
		return &IMAgentResponse{Text: i18n.Tf(i18n.MsgWorkflowInitiationExtractedConfirm, h.lang(), h.presentExtractedData(session))}, nil
	}
	return h.buildMissingFieldsPrompt(session), nil
}

func (h *WorkflowInitiationHandler) matchWorkflowByMessage(ctx context.Context, message string) (*WorkflowMatch, error) {
	workflows, err := h.fetchPublishedWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	if len(workflows) == 0 {
		return nil, nil
	}
	var best *WorkflowMatch
	var bestScore float64
	for _, wf := range workflows {
		score := h.scoreWorkflowMatch(message, wf)
		if score > bestScore {
			bestScore = score
			best = &WorkflowMatch{WorkflowID: wf.WorkflowID, WorkflowName: wf.WorkflowName, Schema: wf.Schema, Confidence: score}
		}
	}
	if best == nil || best.Confidence < 0.3 {
		return nil, nil
	}
	return best, nil
}

func (h *WorkflowInitiationHandler) fetchPublishedWorkflows(ctx context.Context) ([]publishedWorkflowInfo, error) {
	hubURL, token, err := h.app.getHubCredentials()
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, hubURL+"/api/v1/workflows/published", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Hub status %d: %s", resp.StatusCode, string(b))
	}
	var result struct {
		Workflows []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Schema []struct {
				Name     string `json:"name"`
				Label    string `json:"label"`
				Type     string `json:"type"`
				Required bool   `json:"required"`
			} `json:"schema"`
		} `json:"workflows"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	var out []publishedWorkflowInfo
	for _, wf := range result.Workflows {
		info := publishedWorkflowInfo{WorkflowID: wf.ID, WorkflowName: wf.Name}
		for _, f := range wf.Schema {
			info.Schema = append(info.Schema, initiationFormField{Name: f.Name, Label: f.Label, Type: f.Type, Required: f.Required})
		}
		out = append(out, info)
	}
	return out, nil
}

func (h *WorkflowInitiationHandler) scoreWorkflowMatch(message string, wf publishedWorkflowInfo) float64 {
	msgLower := strings.ToLower(message)
	nameLower := strings.ToLower(wf.WorkflowName)
	var score float64
	if strings.Contains(msgLower, nameLower) {
		score += 0.6
	}
	for _, kw := range strings.FieldsFunc(wf.WorkflowName, func(r rune) bool { return r == ' ' || r == '/' }) {
		if len([]rune(kw)) >= 2 && strings.Contains(msgLower, strings.ToLower(kw)) {
			score += 0.3
		}
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}

func (h *WorkflowInitiationHandler) extractFieldsFromMessage(s *initiationSession, msg string) {
	for _, f := range s.Schema {
		if v := h.extractSingleField(f, msg); v != "" {
			s.ExtractedData[f.Name] = v
		}
	}
}
func (h *WorkflowInitiationHandler) extractSingleField(f initiationFormField, msg string) string {
	if f.Type == "date" {
		if v := extractDateValue(msg); v != "" {
			return v
		}
	}
	if f.Name == "leave_type" || strings.Contains(f.Label, "类型") {
		for _, t := range []string{"事假", "年假", "病假", "婚假", "产假", "调休"} {
			if strings.Contains(msg, t) {
				return t
			}
		}
	}
	if f.Name == "duration" || strings.Contains(f.Label, "时长") {
		for _, d := range []string{"半天", "一天", "两天", "三天", "1天", "2天", "3天"} {
			if strings.Contains(msg, d) {
				return d
			}
		}
	}
	return ""
}
func extractDateValue(msg string) string {
	for p, off := range map[string]int{"今天": 0, "明天": 1, "后天": 2} {
		if strings.Contains(msg, p) {
			return time.Now().AddDate(0, 0, off).Format("2006-01-02")
		}
	}
	return ""
}
func (h *WorkflowInitiationHandler) findMissingRequiredFields(s *initiationSession) []string {
	var m []string
	for _, f := range s.Schema {
		if f.Required {
			if _, ok := s.ExtractedData[f.Name]; !ok {
				m = append(m, f.Name)
			}
		}
	}
	return m
}
func (h *WorkflowInitiationHandler) buildMissingFieldsPrompt(s *initiationSession) *IMAgentResponse {
	var d []string
	for _, n := range s.MissingFields {
		for _, f := range s.Schema {
			if f.Name == n {
				l := f.Label
				if l == "" {
					l = f.Name
				}
				d = append(d, "- "+l)
			}
		}
	}
	return &IMAgentResponse{Text: i18n.Tf(i18n.MsgWorkflowInitiationMissingFields, h.lang(), s.WorkflowName, strings.Join(d, "\n"))}
}
func (h *WorkflowInitiationHandler) handleMissingFieldsResponse(ctx context.Context, userID, msg string, s *initiationSession) (*IMAgentResponse, error) {
	h.extractFieldsFromMessage(s, msg)
	if len(s.MissingFields) == 1 {
		fn := s.MissingFields[0]
		if _, ok := s.ExtractedData[fn]; !ok {
			t := strings.TrimSpace(msg)
			if t != "" && len([]rune(t)) < 100 {
				s.ExtractedData[fn] = t
			}
		}
	}
	s.MissingFields = h.findMissingRequiredFields(s)
	h.setSession(userID, s)
	if len(s.MissingFields) == 0 {
		return &IMAgentResponse{Text: i18n.Tf(i18n.MsgWorkflowInitiationExtractedConfirm, h.lang(), h.presentExtractedData(s))}, nil
	}
	return h.buildMissingFieldsPrompt(s), nil
}
func (h *WorkflowInitiationHandler) buildNoMatchResponse(ctx context.Context) (*IMAgentResponse, error) {
	wfs, err := h.fetchPublishedWorkflows(ctx)
	if err != nil || len(wfs) == 0 {
		return &IMAgentResponse{Text: i18n.T(i18n.MsgWorkflowInitiationNoMatch, h.lang())}, nil
	}
	var names []string
	for _, wf := range wfs {
		if len(names) >= 5 {
			break
		}
		names = append(names, "- "+wf.WorkflowName)
	}
	return &IMAgentResponse{Text: i18n.Tf(i18n.MsgWorkflowInitiationNoMatchList, h.lang(), strings.Join(names, "\n"))}, nil
}

var initiationConfirmWords = []string{"确认", "是", "对", "没问题", "可以", "好的", "好", "行", "yes", "ok", "confirm"}
var initiationCancelWords = []string{"取消", "算了", "不要了", "放弃", "cancel", "abort"}

func isInitiationConfirm(msg string) bool {
	l := strings.ToLower(strings.TrimSpace(msg))
	for _, w := range initiationConfirmWords {
		if l == w {
			return true
		}
	}
	return false
}
func isInitiationCancel(msg string) bool {
	l := strings.ToLower(strings.TrimSpace(msg))
	for _, w := range initiationCancelWords {
		if l == w {
			return true
		}
	}
	return false
}

func (h *WorkflowInitiationHandler) handleConfirmation(ctx context.Context, userID, message string, session *initiationSession) (*IMAgentResponse, error) {
	trimmed := strings.TrimSpace(message)
	if isInitiationConfirm(trimmed) {
		return h.submitToHub(ctx, session)
	}
	if isInitiationCancel(trimmed) {
		h.deleteSession(userID)
		return &IMAgentResponse{Text: i18n.Tf(i18n.MsgWorkflowInitiationCancelled, h.lang(), session.WorkflowName)}, nil
	}
	if h.tryUpdateField(session, trimmed) {
		h.setSession(userID, session)
		return &IMAgentResponse{Text: i18n.Tf(i18n.MsgWorkflowInitiationUpdatedConfirm, h.lang(), h.presentExtractedData(session))}, nil
	}
	return &IMAgentResponse{Text: i18n.T(i18n.MsgWorkflowInitiationConfirmHelp, h.lang())}, nil
}
func (h *WorkflowInitiationHandler) tryUpdateField(s *initiationSession, msg string) bool {
	for _, sep := range []string{"改为", "改成"} {
		idx := strings.Index(msg, sep)
		if idx <= 0 {
			continue
		}
		hint := strings.TrimSpace(msg[:idx])
		val := strings.TrimSpace(msg[idx+len(sep):])
		if val == "" {
			continue
		}
		for _, f := range s.Schema {
			l := f.Label
			if l == "" {
				l = f.Name
			}
			if strings.Contains(l, hint) || strings.Contains(hint, l) {
				s.ExtractedData[f.Name] = val
				return true
			}
		}
	}
	return false
}
func (h *WorkflowInitiationHandler) submitToHub(ctx context.Context, session *initiationSession) (*IMAgentResponse, error) {
	hubURL, token, err := h.app.getHubCredentials()
	if err != nil {
		return &IMAgentResponse{Text: i18n.T(i18n.MsgWorkflowInitiationHubConnectError, h.lang())}, err
	}
	body, _ := json.Marshal(map[string]interface{}{"form_data": session.ExtractedData, "channel": "im"})
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, fmt.Sprintf("%s/api/v1/workflows/%s/initiate", hubURL, session.WorkflowID), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return &IMAgentResponse{Text: i18n.T(i18n.MsgWorkflowInitiationNetworkError, h.lang())}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		log.Printf("[WorkflowInitiation] submitToHub: status %d body_len=%d", resp.StatusCode, len(respBody))
		return &IMAgentResponse{Text: i18n.Tf(i18n.MsgWorkflowInitiationCreateFailed, h.lang(), resp.StatusCode)}, nil
	}
	var result struct {
		InstanceID string `json:"instance_id"`
	}
	json.Unmarshal(respBody, &result)
	h.deleteSession(session.UserID)
	return &IMAgentResponse{Text: i18n.Tf(i18n.MsgWorkflowInitiationStarted, h.lang(), formatWFNumber(result.InstanceID))}, nil
}
func formatWFNumber(id string) string {
	return fmt.Sprintf("WF-%s-%s", time.Now().Format("20060102"), extractSeqFromID(id))
}
func extractSeqFromID(id string) string {
	if id == "" {
		return "001"
	}
	var d []byte
	for i := len(id) - 1; i >= 0 && len(d) < 3; i-- {
		if id[i] >= '0' && id[i] <= '9' {
			d = append([]byte{id[i]}, d...)
		}
	}
	for len(d) < 3 {
		d = append([]byte{'0'}, d...)
	}
	return string(d)
}
func (h *WorkflowInitiationHandler) presentExtractedData(s *initiationSession) string {
	var lines []string
	for _, f := range s.Schema {
		l := f.Label
		if l == "" {
			l = f.Name
		}
		val := i18n.T(i18n.MsgWorkflowInitiationUnset, h.lang())
		if v, ok := s.ExtractedData[f.Name]; ok && v != nil {
			val = fmt.Sprintf("%v", v)
		}
		lines = append(lines, fmt.Sprintf("- %s：%s", l, val))
	}
	return strings.Join(lines, "\n")
}
