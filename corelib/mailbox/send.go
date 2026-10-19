package mailbox

import (
	"context"
	"strings"
	"sync"
	"time"
)

type OutboundMessage struct {
	From       string
	To         []string
	Cc         []string
	Bcc        []string
	Subject    string
	Text       string
	InReplyTo  string
	References string
}

// MailSender transmits one frozen draft. The availability probe does not use it.
type MailSender interface {
	Send(ctx context.Context, account Account, secret string, msg OutboundMessage) error
}

type Draft struct {
	ID        string
	Account   Account
	Message   OutboundMessage
	ExpiresAt time.Time
	Principal string
	Session   string
	// Kind is empty for a send. DraftKindDelete carries Delete.
	Kind   string
	Delete DeletePlan
}

type DraftStore struct {
	mu      sync.Mutex
	byID    map[string]*Draft
	pending map[string]string
}

func NewDraftStore() *DraftStore {
	return &DraftStore{byID: map[string]*Draft{}, pending: map[string]string{}}
}

func sessionKey(principal, session string) string {
	return strings.TrimSpace(principal) + "\x00" + strings.TrimSpace(session)
}

func (s *DraftStore) Put(d Draft) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := d
	s.byID[d.ID] = &cp
	s.pending[sessionKey(d.Principal, d.Session)] = d.ID
}

// Peek returns a draft without removing it.
func (s *DraftStore) Peek(id string) (Draft, bool) {
	if s == nil || id == "" {
		return Draft{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.byID[id]
	if !ok || d == nil {
		return Draft{}, false
	}
	return *d, true
}

func (s *DraftStore) Pending(principal, session string) (Draft, bool) {
	if s == nil {
		return Draft{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.pending[sessionKey(principal, session)]
	d, ok := s.byID[id]
	if !ok || d == nil {
		return Draft{}, false
	}
	return *d, true
}

func (s *DraftStore) Take(id string, now time.Time) (Draft, string) {
	if s == nil {
		return Draft{}, "missing"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.byID[id]
	if !ok || d == nil {
		return Draft{}, "missing"
	}
	delete(s.byID, id)
	if s.pending[sessionKey(d.Principal, d.Session)] == id {
		delete(s.pending, sessionKey(d.Principal, d.Session))
	}
	if !d.ExpiresAt.IsZero() && !now.Before(d.ExpiresAt) {
		return *d, "expired"
	}
	return *d, "ok"
}

func (s *DraftStore) Drop(id string) {
	if s == nil || id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.byID[id]
	delete(s.byID, id)
	if d != nil && s.pending[sessionKey(d.Principal, d.Session)] == id {
		delete(s.pending, sessionKey(d.Principal, d.Session))
	}
}

func IsApproveText(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "confirm", "发送", "确认":
		return true
	default:
		return false
	}
}

func IsCancelText(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "cancel", "取消":
		return true
	default:
		return false
	}
}

type TurnBudget struct {
	mu         sync.Mutex
	QueryLeft  int
	SendUsed   bool
	DeleteUsed bool
}

func NewTurnBudget() *TurnBudget {
	return &TurnBudget{QueryLeft: QueryBudget}
}

func (b *TurnBudget) ConsumeQuery() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.QueryLeft <= 0 {
		return classErr("budget", MsgQueryBudget)
	}
	b.QueryLeft--
	return nil
}

func (b *TurnBudget) ConsumeSend() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.SendUsed {
		return classErr("budget", MsgSendBudget)
	}
	b.SendUsed = true
	return nil
}

func (b *TurnBudget) ConsumeDelete() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.DeleteUsed {
		return classErr("budget", MsgDeleteBudget)
	}
	b.DeleteUsed = true
	return nil
}

func (h *Host) Approve(ctx context.Context, draftID string) (string, string) {
	if h == nil || h.Drafts == nil {
		return MsgExpired, "expired"
	}
	if recorded, done, err := h.finishedSend(); err != nil {
		return err.Error(), "error"
	} else if done {
		kind := ""
		if pending, ok := h.Drafts.Peek(draftID); ok {
			kind = pending.Kind
		}
		h.Drafts.Drop(draftID)
		if strings.TrimSpace(recorded) == "" {
			if kind == DraftKindDelete {
				recorded = MsgDeletePurged
			} else {
				recorded = "邮件已发送"
			}
		}
		return recorded, "approved"
	}
	draft, state := h.Drafts.Take(draftID, h.now())
	switch state {
	case "missing":
		return MsgExpired, "expired"
	case "expired":
		h.countSend("expired")
		if draft.Kind == DraftKindDelete {
			return MsgDeleteExpired, "expired"
		}
		return MsgExpired, "expired"
	}
	if draft.Kind == DraftKindDelete {
		return h.approveDelete(ctx, draft)
	}
	secret, err := h.secretFor(ctx, draft.Account, SideOutbound)
	if err != nil {
		return err.Error(), classOf(err)
	}
	if h.Sender == nil {
		h.noteUncertain(draft)
		return MsgUncertain, "uncertain"
	}
	if err := h.Sender.Send(ctx, draft.Account, secret, draft.Message); err != nil {
		class, msg := classifyTransport(err, false)
		if class == "uncertain" {
			h.noteUncertain(draft)
			h.countSend("uncertain")
			h.audit(draft, "uncertain")
			return MsgUncertain, "uncertain"
		}
		if class == "rejected" {
			h.countSend("rejected")
			h.audit(draft, "rejected")
			return msg, "rejected"
		}
		h.countSend(class)
		h.audit(draft, class)
		return msg, class
	}
	h.countSend("approved")
	h.audit(draft, "approved")
	const sent = "邮件已发送"
	if err := h.finishSend(sent); err != nil {
		return err.Error(), "error"
	}
	return sent, "approved"
}

func (h *Host) finishedSend() (string, bool, error) {
	if h == nil || h.Steps == nil || strings.TrimSpace(h.StepKey) == "" {
		return "", false, nil
	}
	return h.Steps.Finished(strings.TrimSpace(h.StepKey))
}

func (h *Host) finishSend(result string) error {
	return h.finishStep("email", result)
}

func (h *Host) finishStep(toolName, result string) error {
	if h == nil || h.Steps == nil || strings.TrimSpace(h.StepKey) == "" {
		return nil
	}
	return h.Steps.Finish(strings.TrimSpace(h.StepKey), toolName, result)
}

func (h *Host) Cancel(draftID string) (string, string) {
	if h == nil || h.Drafts == nil {
		return MsgCancelled, "cancelled"
	}
	kind := ""
	if draft, ok := h.Drafts.Peek(draftID); ok {
		kind = draft.Kind
	}
	h.Drafts.Drop(draftID)
	h.countSend("cancelled")
	if kind == DraftKindDelete {
		return MsgDeleteCancelled, "cancelled"
	}
	return MsgCancelled, "cancelled"
}

func (h *Host) noteUncertain(draft Draft) {
	if h != nil && h.OnUncertain != nil {
		h.OnUncertain(draft)
	}
}

func (h *Host) countSend(class string) {
	if h == nil || h.Counters == nil {
		return
	}
	h.Counters.mu.Lock()
	defer h.Counters.mu.Unlock()
	if h.Counters.Send == nil {
		h.Counters.Send = map[string]int{}
	}
	h.Counters.Send[class]++
}

func (h *Host) audit(draft Draft, result string) {
	if h == nil || h.Audit == nil {
		return
	}
	h.Audit(AuditEvent{
		AccountID:      draft.Account.ID,
		RecipientCount: len(draft.Message.To) + len(draft.Message.Cc) + len(draft.Message.Bcc),
		Result:         result,
	})
}

func replySubject(subject string) string {
	s := oneLine(subject)
	if strings.HasPrefix(strings.ToLower(s), "re:") {
		return s
	}
	if s == "" {
		return "Re:"
	}
	return "Re: " + s
}

func forwardSubject(subject string) string {
	s := oneLine(subject)
	if strings.HasPrefix(strings.ToLower(s), "fwd:") || strings.HasPrefix(strings.ToLower(s), "fw:") {
		return s
	}
	if s == "" {
		return "Fwd:"
	}
	return "Fwd: " + s
}

func parseAddressList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		addr := extractAddress(part)
		if addr != "" {
			out = append(out, addr)
		}
	}
	return out
}

func extractAddress(part string) string {
	part = strings.TrimSpace(part)
	if i := strings.LastIndex(part, "<"); i >= 0 {
		if j := strings.Index(part[i:], ">"); j > 1 {
			part = part[i+1 : i+j]
		}
	}
	part = strings.Trim(part, "<> \"'")
	if strings.Count(part, "@") != 1 {
		return ""
	}
	return part
}

func sameAddress(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func uniqueAddrs(in []string, skip ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, addr := range in {
		addr = strings.TrimSpace(addr)
		if addr == "" || addressSkipped(addr, skip) {
			continue
		}
		key := strings.ToLower(addr)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, addr)
	}
	return out
}

func addressSkipped(addr string, skip []string) bool {
	for _, item := range skip {
		if sameAddress(addr, item) {
			return true
		}
	}
	return false
}

func normalizeRecipients(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		addr := extractAddress(item)
		if addr == "" {
			return nil, classErr("invalid", "收件人地址格式不正确")
		}
		normalized, err := normalizeAddress(addr)
		if err != nil {
			return nil, classErr("invalid", "收件人地址格式不正确")
		}
		out = append(out, normalized)
	}
	return out, nil
}
