package desktopbot

import (
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// Connector gate. After a connector returns timeout, 401, 5xx, or a schema
// rejection, the same operation stays off the logged-in browser (level 4)
// and off pixels (level 5). web_search and web_fetch are not this rule.
// Level 4 runs only when the person said to use the browser and the call
// carries user_authorized_bypass. Level 5 does not get that release.

const (
	LevelBrowser = 4
	LevelPixel   = 5
)

// BrowserLevel reports 4 for a logged-in browser action, 5 for computer, and 0
// for everything else.
func BrowserLevel(action string) int {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "navigate", "probe", "script", "click", "type", "press", "task_run", "skill_run":
		return LevelBrowser
	case "computer":
		return LevelPixel
	default:
		return 0
	}
}

// ConnectorObservable is a tool whose error can open the gate. Desktop, public
// web, ssh, files, and the ask itself do not. A log line that mentions 401 is
// not a connector failure.
func ConnectorObservable(name string) bool {
	name = strings.TrimSpace(name)
	switch name {
	case "", "desktop", "web_search", "web_fetch", "ssh", "bash", "ask_user", "memory", "send_to_user":
		return false
	}
	if strings.HasPrefix(name, "file_") {
		return false
	}
	return true
}

// UserSaidUseBrowser is the person's release phrase for one turn.
func UserSaidUseBrowser(text string) bool {
	if strings.Contains(text, "改用浏览器") {
		return true
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "use the browser") || strings.Contains(lower, "use a browser")
}

var (
	status401 = regexp.MustCompile(`\b401\b`)
	status5xx = regexp.MustCompile(`\b5\d\d\b`)
)

// ClassifyConnectorFailure reports 401, 5xx, timeout, or schema_reject.
// ok is false when the result is not one of those failures.
func ClassifyConnectorFailure(status int, errText string) (string, bool) {
	switch {
	case status == 401:
		return "401", true
	case status == 408:
		return "timeout", true
	case status >= 500 && status <= 599:
		return "5xx", true
	}
	text := strings.ToLower(errText)
	if strings.Contains(text, "schema_reject") || strings.Contains(text, "schema reject") {
		return "schema_reject", true
	}
	if status401.MatchString(errText) || strings.Contains(text, "unauthorized") {
		return "401", true
	}
	if strings.Contains(text, "timeout") || strings.Contains(text, "timed out") || strings.Contains(text, "deadline exceeded") {
		return "timeout", true
	}
	if status5xx.MatchString(errText) {
		return "5xx", true
	}
	return "", false
}

// HostOf returns the lowercase host of a URL. A bare host is accepted.
// An empty or unparseable value is empty, which matches an open failure.
func HostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// BypassCall is one desktop action the model wants to run.
type BypassCall struct {
	Action               string
	URL                  string
	UserAuthorizedBypass bool
	UserSaidUseBrowser   bool
}

// ConnectorAsk is the one question the person sees. It is not assistant prose.
type ConnectorAsk struct {
	Kind string
	Text string
}

// BypassDecision is the gate's answer. Sentence is what the model reads.
// Ask is set on the first refusal of that failure only.
type BypassDecision struct {
	Allowed  bool
	Level    int
	Sentence string
	Ask      ConnectorAsk
}

type connectorFailure struct {
	Name   string
	Site   string
	Kind   string
	Detail string
}

// ConnectorGate records connector failures for one conversation.
type ConnectorGate struct {
	mu       sync.Mutex
	failures []connectorFailure
	asked    map[string]bool
}

// NewConnectorGate returns a gate that is not shared across conversations.
func NewConnectorGate() *ConnectorGate {
	return &ConnectorGate{asked: map[string]bool{}}
}

var conversationGates sync.Map

func resetConnectorGates() {
	conversationGates = sync.Map{}
}

// ConversationGate returns the process-local gate for one bot conversation.
// A later turn of the same conversation sees the failure recorded earlier.
func ConversationGate(botID, conversationID string) *ConnectorGate {
	key := botID + "\x00" + conversationID
	value, _ := conversationGates.LoadOrStore(key, NewConnectorGate())
	return value.(*ConnectorGate)
}

// Observe records a connector failure. A result that is not 401, 5xx, timeout,
// or schema rejection is ignored. Desktop and public-web tools are ignored.
func (g *ConnectorGate) Observe(name string, status int, errText, site string) {
	if g == nil || !ConnectorObservable(name) {
		return
	}
	kind, ok := ClassifyConnectorFailure(status, errText)
	if !ok {
		return
	}
	name = strings.TrimSpace(name)
	site = HostOf(site)
	detail := ScrubSecrets(strings.TrimSpace(errText))
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := range g.failures {
		if g.failures[i].Name == name && g.failures[i].Site == site {
			g.failures[i].Kind = kind
			g.failures[i].Detail = detail
			return
		}
	}
	g.failures = append(g.failures, connectorFailure{Name: name, Site: site, Kind: kind, Detail: detail})
}

// Decide refuses level 4 and level 5 while a matching failure is open.
// A navigate to a different known host is allowed. An empty call host matches
// the open failure. Level 4 needs both the person's phrase and the bypass flag.
func (g *ConnectorGate) Decide(call BypassCall) BypassDecision {
	level := BrowserLevel(call.Action)
	if g == nil || level == 0 {
		return BypassDecision{Allowed: true, Level: level}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	fail, ok := g.match(call.URL)
	if !ok {
		return BypassDecision{Allowed: true, Level: level}
	}
	if level == LevelBrowser && call.UserSaidUseBrowser && call.UserAuthorizedBypass {
		return BypassDecision{Allowed: true, Level: level}
	}
	sentence := connectorDownSentence(fail.Name, fail.Kind, level, call.UserSaidUseBrowser && call.UserAuthorizedBypass)
	decision := BypassDecision{Allowed: false, Level: level, Sentence: sentence}
	key := fail.Name + "\x00" + fail.Site
	if g.asked == nil {
		g.asked = map[string]bool{}
	}
	if !g.asked[key] {
		g.asked[key] = true
		decision.Ask = ConnectorAsk{Kind: "ask", Text: connectorAskText(fail.Name, fail.Kind)}
	}
	return decision
}

func (g *ConnectorGate) match(rawURL string) (connectorFailure, bool) {
	host := HostOf(rawURL)
	var blank connectorFailure
	blankHit := false
	for _, fail := range g.failures {
		if fail.Site == "" {
			blank = fail
			blankHit = true
			continue
		}
		if host == "" || strings.EqualFold(fail.Site, host) {
			return fail, true
		}
	}
	if blankHit {
		return blank, true
	}
	return connectorFailure{}, false
}

func connectorDownSentence(name, kind string, level int, bypassTried bool) string {
	name = scrubName(name)
	if kind == "" {
		kind = "失败"
	}
	sentence := "连接器 " + name + " 现在不可用（" + kind + "）。不要改用浏览器。调用 send_to_user，kind=ask，把这次失败告诉这个人，并问要不要改用浏览器"
	if level == LevelPixel && bypassTried {
		sentence += "。user_authorized_bypass 不放行 computer"
	}
	return sentence
}

func connectorAskText(name, kind string) string {
	name = scrubName(name)
	if kind == "" {
		kind = "失败"
	}
	return "连接器 " + name + " 这次失败（" + kind + "）。要停在这里，还是改用浏览器？"
}

func scrubName(name string) string {
	name = ScrubSecrets(strings.TrimSpace(name))
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || strings.Contains(name, "<redacted>") {
		return "未命名"
	}
	return name
}
