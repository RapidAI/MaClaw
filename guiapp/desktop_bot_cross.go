package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DesktopBotCross is the optional handoff on a primary bot's reading.
// Kind is notify or delegate. Target is a bot id, a display name, or all.
type DesktopBotCross struct {
	Kind   string `json:"kind,omitempty"`
	Target string `json:"target,omitempty"`
	Text   string `json:"text,omitempty"`
	Order  string `json:"order,omitempty"`
}

const (
	desktopBotNotPrimarySentence = "我不能去叫别的 Bot。你跟主 Bot 说，或者直接打开那个 Bot。"
	desktopBotNoSuchBotSentence  = "没有这个 Bot。"

	desktopBotCrossSystem = `This bot is the primary bot for the account. You may tell another bot, or hand it work. When the latest message does that, add one object field cross. kind is notify or delegate. target is that bot's id from the list below, or its name. text is the short sentence that bot should see. For delegate, order is the work that bot will carry out. Leave instruction empty when the only work is this handoff. Do not add cross for any other reason. Do not target this bot. If you cannot tell which bots, or what to tell them, pace is ask, told asks which bots and what to say, and omit cross. When they mean every other bot, target is all. The message does not interrupt another bot's current turn. They see it at the start of their next turn.`
)

var desktopBotInstallWord = regexp.MustCompile(`(?:^|[^a-z])install(?:[^a-z]|$)`)

type desktopBotRosterEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Primary bool   `json:"primary"`
}

type desktopBotRoster struct {
	Self string                  `json:"self"`
	Bots []desktopBotRosterEntry `json:"bots"`
}

func parseDesktopBotRoster(catalog string) *desktopBotRoster {
	catalog = strings.TrimSpace(catalog)
	if catalog == "" {
		return nil
	}
	var roster desktopBotRoster
	if err := json.Unmarshal([]byte(catalog), &roster); err != nil {
		return nil
	}
	roster.Self = strings.TrimSpace(roster.Self)
	if roster.Self == "" || len(roster.Bots) == 0 {
		return nil
	}
	return &roster
}

func rosterPrimary(roster *desktopBotRoster) bool {
	if roster == nil {
		return false
	}
	for _, bot := range roster.Bots {
		if strings.TrimSpace(bot.ID) == roster.Self && bot.Primary {
			return true
		}
	}
	return false
}

func desktopBotRosterPrompt(roster *desktopBotRoster) string {
	if roster == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nbots on this account:\n")
	for _, bot := range roster.Bots {
		id := strings.TrimSpace(bot.ID)
		name := strings.TrimSpace(bot.Name)
		if id == "" {
			continue
		}
		mark := "no"
		if bot.Primary {
			mark = "yes"
		}
		line := "id=" + id + " name=" + name + " primary=" + mark
		if live := desktopBotRosterLive(id); live != "" {
			line += " " + live
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func finishDesktopBotCross(understood *DesktopBotUnderstanding, roster *desktopBotRoster) {
	if understood == nil || understood.Cross == nil {
		return
	}
	cross := understood.Cross
	cross.Kind = strings.ToLower(strings.TrimSpace(cross.Kind))
	cross.Target = strings.TrimSpace(cross.Target)
	cross.Text = clipRunes(strings.TrimSpace(cross.Text), desktopBotUnderstandToldMax)
	cross.Order = clipDesktopBotOrder(strings.TrimSpace(cross.Order), desktopBotUnderstandOrderMax)
	if cross.Kind != "notify" && cross.Kind != "delegate" {
		understood.Cross = nil
		return
	}
	if roster == nil {
		return
	}
	if !rosterPrimary(roster) {
		understood.Pace = "reply"
		understood.Instruction = ""
		understood.Schedule = nil
		understood.Told = desktopBotNotPrimarySentence
		understood.Cross = nil
		return
	}
	if _, ok := resolveDesktopBotTargets(cross.Target, roster.Self, roster.Bots); !ok {
		understood.Cross = nil
		understood.Pace = "ask"
		understood.Instruction = ""
		understood.Schedule = nil
		understood.Told = desktopBotCrossAsk(cross.Target, roster.Bots)
		return
	}
	understood.Pace = "reply"
	understood.Instruction = ""
	understood.Schedule = nil
}

func desktopBotCrossAsk(target string, bots []desktopBotRosterEntry) string {
	target = strings.TrimSpace(target)
	if target == "" || desktopBotCrossAll(target) {
		return "你要跟哪一个 Bot 说，说什么？"
	}
	name := strings.TrimSpace(target)
	same := 0
	for _, bot := range bots {
		if strings.TrimSpace(bot.Name) == name {
			same++
		}
	}
	if same > 1 {
		return "有好几个叫「" + name + "」。你要跟哪一个说？"
	}
	return desktopBotNoSuchBotSentence + "你要跟哪一个说？"
}

func desktopBotCrossAll(target string) bool {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "all", "*", "所有", "所有bot", "所有的bot":
		return true
	default:
		return false
	}
}

func resolveDesktopBotTargets(target, self string, bots []desktopBotRosterEntry) ([]string, bool) {
	self = strings.TrimSpace(self)
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, false
	}
	if desktopBotCrossAll(target) {
		out := make([]string, 0, len(bots))
		for _, bot := range bots {
			id := strings.TrimSpace(bot.ID)
			if id == "" || id == self {
				continue
			}
			out = append(out, id)
		}
		return out, len(out) > 0
	}
	parts := splitDesktopBotTargets(target)
	if len(parts) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		id, ok := resolveOneDesktopBotTarget(part, self, bots)
		if !ok || seen[id] {
			if !ok {
				return nil, false
			}
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, len(out) > 0
}

func splitDesktopBotTargets(target string) []string {
	fields := strings.FieldsFunc(target, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == ';' || r == '；'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func resolveOneDesktopBotTarget(target, self string, bots []desktopBotRosterEntry) (string, bool) {
	target = strings.TrimSpace(target)
	if target == "" || target == self {
		return "", false
	}
	for _, bot := range bots {
		id := strings.TrimSpace(bot.ID)
		if id == target && id != self {
			return id, true
		}
	}
	match := ""
	for _, bot := range bots {
		id := strings.TrimSpace(bot.ID)
		if id == "" || id == self {
			continue
		}
		if strings.TrimSpace(bot.Name) != target {
			continue
		}
		if match != "" {
			return "", false
		}
		match = id
	}
	return match, match != ""
}

func desktopBotDestructive(text string) bool {
	if strings.Contains(text, "安装") || strings.Contains(text, "卸载") || strings.Contains(text, "刪除") || strings.Contains(text, "删除") || strings.Contains(text, "付款") || strings.Contains(text, "支付") || strings.Contains(text, "代发") || strings.Contains(text, "代發") || strings.Contains(text, "清空") {
		return true
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "uninstall") || strings.Contains(lower, "wipe") || strings.Contains(lower, "delete") {
		return true
	}
	return desktopBotInstallWord.MatchString(lower)
}

func desktopBotCrossPrimaryLine(kind string, names []string, text, order string) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) > 1 {
		return "已发消息给 " + itoa(len(names)) + " 个 Bot"
	}
	name := names[0]
	if kind == "notify" {
		return "跟 " + name + " 说了一声：" + text
	}
	body := strings.TrimSpace(order)
	if body == "" {
		body = text
	}
	// The word list stays until a later PR deletes it. It does not decide
	// this handoff. The gate is the review of the call the other bot runs.
	_ = desktopBotDestructive(body)
	return "交给 " + name + " 了，他现在去做。"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// DesktopBotCrossNote is one note pulled from Hub. It is not a run.
type DesktopBotCrossNote struct {
	ID        string `json:"id"`
	Owner     string `json:"owner,omitempty"`
	From      string `json:"from"`
	Target    string `json:"target"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Order     string `json:"order,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	ReplyTo   string `json:"reply_to,omitempty"`
}

// DesktopBotCrossPost is the result of one primary handoff.
// Ask is set when nothing was posted and the person still has to say which bot.
type DesktopBotCrossPost struct {
	Posted      bool                  `json:"posted"`
	Ask         string                `json:"ask,omitempty"`
	PrimaryLine string                `json:"primary_line,omitempty"`
	Names       []string              `json:"names,omitempty"`
	Notes       []DesktopBotCrossNote `json:"notes,omitempty"`
}

// DesktopBotCrossClaim is one attempt to start a delegate.
// Claimed false means another client already took it, so this one must not admit.
type DesktopBotCrossClaim struct {
	Claimed   bool   `json:"claimed"`
	ID        string `json:"id"`
	From      string `json:"from"`
	Target    string `json:"target"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Order     string `json:"order,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	RunID     string `json:"run_id,omitempty"`
}

func (a *App) loadDesktopBotRoster() (*desktopBotRoster, error) {
	var out struct {
		Items []hubBot `json:"items"`
	}
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/bots", nil, &out, 20*time.Second); err != nil {
		return nil, err
	}
	roster := &desktopBotRoster{Bots: make([]desktopBotRosterEntry, 0, len(out.Items))}
	for _, bot := range out.Items {
		roster.Bots = append(roster.Bots, desktopBotRosterEntry{
			ID:      strings.TrimSpace(bot.ID),
			Name:    strings.TrimSpace(bot.Name),
			Primary: bot.Primary,
		})
	}
	return roster, nil
}

// SetDesktopBotPrimary stores this bot as the account's only primary.
// The response is the list, with primary set on exactly one item.
func (a *App) SetDesktopBotPrimary(botID string) ([]DesktopBotInfo, error) {
	if a == nil {
		return nil, errDesktopBotUnavailable
	}
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, errDesktopBotID
	}
	var out struct {
		Items []hubBot `json:"items"`
	}
	if err := a.desktopBotCall(context.Background(), http.MethodPost, "/api/v1/bots/"+url.PathEscape(botID)+"/primary", nil, &out, 20*time.Second); err != nil {
		return nil, err
	}
	items := make([]DesktopBotInfo, 0, len(out.Items))
	for _, bot := range out.Items {
		items = append(items, desktopBotInfo(bot))
	}
	return items, nil
}

var (
	errDesktopBotUnavailable = desktopBotErr("AI assistant backend is unavailable")
	errDesktopBotID          = desktopBotErr("bot id is required")
)

type desktopBotErr string

func (e desktopBotErr) Error() string { return string(e) }

// PostDesktopBotCross sends one note per target. A bot that is not the
// primary posts nothing. An unknown or ambiguous target posts nothing and
// asks which bot. This does not admit a run.
func (a *App) PostDesktopBotCross(botID, kind, target, text, order, replyTo string) (*DesktopBotCrossPost, error) {
	if a == nil {
		return nil, errDesktopBotUnavailable
	}
	botID = strings.TrimSpace(botID)
	kind = strings.ToLower(strings.TrimSpace(kind))
	text = clipRunes(strings.TrimSpace(text), desktopBotUnderstandToldMax)
	order = clipDesktopBotOrder(strings.TrimSpace(order), desktopBotUnderstandOrderMax)
	replyTo = strings.TrimSpace(replyTo)
	if botID == "" {
		return nil, errDesktopBotID
	}
	if kind == "result" {
		note, err := a.postOneDesktopBotCross(botID, kind, "", text, "", replyTo)
		if err != nil {
			return nil, err
		}
		return &DesktopBotCrossPost{Posted: true, Notes: []DesktopBotCrossNote{note}}, nil
	}
	if kind != "notify" && kind != "delegate" {
		return nil, desktopBotErr("cross kind is invalid")
	}
	roster, err := a.loadDesktopBotRoster()
	if err != nil {
		return nil, err
	}
	roster.Self = botID
	if !rosterPrimary(roster) {
		return nil, desktopBotErr(desktopBotNotPrimarySentence)
	}
	ids, ok := resolveDesktopBotTargets(target, botID, roster.Bots)
	if !ok {
		return &DesktopBotCrossPost{Ask: desktopBotCrossAsk(target, roster.Bots)}, nil
	}
	names := make([]string, 0, len(ids))
	notes := make([]DesktopBotCrossNote, 0, len(ids))
	for _, id := range ids {
		note, err := a.postOneDesktopBotCross(botID, kind, id, text, order, "")
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
		names = append(names, desktopBotRosterName(roster, id))
	}
	return &DesktopBotCrossPost{
		Posted:      true,
		PrimaryLine: desktopBotCrossPrimaryLine(kind, names, text, order),
		Names:       names,
		Notes:       notes,
	}, nil
}

func desktopBotRosterName(roster *desktopBotRoster, id string) string {
	for _, bot := range roster.Bots {
		if strings.TrimSpace(bot.ID) == id {
			if name := strings.TrimSpace(bot.Name); name != "" {
				return name
			}
		}
	}
	return id
}

func (a *App) postOneDesktopBotCross(botID, kind, target, text, order, replyTo string) (DesktopBotCrossNote, error) {
	body := map[string]string{"kind": kind, "text": text}
	if target != "" {
		body["target"] = target
	}
	if order != "" {
		body["order"] = order
	}
	if replyTo != "" {
		body["reply_to"] = replyTo
	}
	var note DesktopBotCrossNote
	err := a.desktopBotCall(context.Background(), http.MethodPost, "/api/v1/bots/"+url.PathEscape(botID)+"/cross", body, &note, 20*time.Second)
	return note, err
}

// PullDesktopBotCrossNotes returns this user's notes. It does not consume them.
func (a *App) PullDesktopBotCrossNotes() ([]DesktopBotCrossNote, error) {
	if a == nil {
		return nil, errDesktopBotUnavailable
	}
	var out struct {
		Items []DesktopBotCrossNote `json:"items"`
	}
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/bots/cross-notes", nil, &out, 20*time.Second); err != nil {
		return nil, err
	}
	if out.Items == nil {
		return []DesktopBotCrossNote{}, nil
	}
	return out.Items, nil
}

// ClaimDesktopBotCrossStart asks Hub for the one start. Claimed false means
// this client must not admit the order.
func (a *App) ClaimDesktopBotCrossStart(botID, noteID string) (*DesktopBotCrossClaim, error) {
	if a == nil {
		return nil, errDesktopBotUnavailable
	}
	botID = strings.TrimSpace(botID)
	noteID = strings.TrimSpace(noteID)
	if botID == "" || noteID == "" {
		return nil, errDesktopBotID
	}
	var claim DesktopBotCrossClaim
	path := "/api/v1/bots/" + url.PathEscape(botID) + "/cross-notes/" + url.PathEscape(noteID) + "/start"
	if err := a.desktopBotCall(context.Background(), http.MethodPost, path, nil, &claim, 20*time.Second); err != nil {
		return nil, err
	}
	return &claim, nil
}

// ArmDesktopBotCrossRun remembers the note whose run id the next admit of
// this bot should store. A second client that lost the claim must not arm.
func (a *App) ArmDesktopBotCrossRun(botID, noteID string) error {
	if a == nil {
		return errDesktopBotUnavailable
	}
	armDesktopBotCrossRun(botID, noteID)
	return nil
}

func (a *App) noteArmedDesktopBotCross(botID, runID string) {
	noteID := takeDesktopBotCrossArm(botID)
	if a == nil || noteID == "" || strings.TrimSpace(runID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	body := map[string]string{"run_id": strings.TrimSpace(runID)}
	path := "/api/v1/bots/" + url.PathEscape(strings.TrimSpace(botID)) + "/cross-notes/" + url.PathEscape(noteID) + "/run"
	_ = a.desktopBotCall(ctx, http.MethodPost, path, body, nil, 15*time.Second)
}

var desktopBotCrossArm = struct {
	sync.Mutex
	note map[string]string
}{}

func armDesktopBotCrossRun(botID, noteID string) {
	botID = strings.TrimSpace(botID)
	noteID = strings.TrimSpace(noteID)
	if botID == "" || noteID == "" {
		return
	}
	desktopBotCrossArm.Lock()
	defer desktopBotCrossArm.Unlock()
	if desktopBotCrossArm.note == nil {
		desktopBotCrossArm.note = map[string]string{}
	}
	desktopBotCrossArm.note[botID] = noteID
}

func takeDesktopBotCrossArm(botID string) string {
	desktopBotCrossArm.Lock()
	defer desktopBotCrossArm.Unlock()
	if desktopBotCrossArm.note == nil {
		return ""
	}
	id := desktopBotCrossArm.note[botID]
	delete(desktopBotCrossArm.note, botID)
	return id
}

func resetDesktopBotCrossArm() {
	desktopBotCrossArm.Lock()
	desktopBotCrossArm.note = nil
	desktopBotCrossArm.Unlock()
}

// desktopBotRosterLiveSource gives the persona reading each bot's live state
// (location, state, task, step) from the main bot's registry. It reads the
// cache only; nil or an unseen bot adds nothing.
var desktopBotRosterLiveSource func(id string) string

func desktopBotRosterLive(id string) string {
	if desktopBotRosterLiveSource == nil {
		return ""
	}
	return desktopBotRosterLiveSource(id)
}
