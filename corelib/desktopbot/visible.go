// Package desktopbot decides what a person can see from one desktop-bot run.
// The sentence is the text of send_to_user. Assistant prose and model thinking
// stay on the run. A missing call does not invent a sentence.
package desktopbot

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"
)

// Turn is one finished desktop-bot run, plus an optional working-row phrase.
// Prose and Thinking are never copied into the sentence.
type Turn struct {
	Prose      string
	Thinking   string
	SendToUser string
	Error      string
	Waiting    bool
	Activity   string
	UserText   string
	PlanReply  string
}

// View is the person-facing result of Turn.
// Bubble is false when there is nothing to say. Arrange is false for planning
// prose; the confirm control is decided by the desktop task step for an
// explicit remaining-work mark, a destructive order, or an unfinished program.
// Activity is a closed working-row phrase, never a bubble and never a notification.
type View struct {
	Sentence string
	Bubble   bool
	Failed   bool
	Arrange  bool
	Activity string
	Notify   bool
}

var namedFillers = map[string]struct{}{
	"后台没有返回结果。":                       {},
	"後台沒有返回結果。":                       {},
	"The backend returned no result.": {},
	"MaClawSrv 没有返回结果":                {},
	"run ended without a result":      {},
}

var emptyRunFiller = regexp.MustCompile(`^run \S+ ended as \S+ without a result$`)

// activityPhrase matches the working row. Typed text, a tool name, and model
// thinking do not. The blocked phrases are the row's own words, not steps.
var activityPhrase = regexp.MustCompile(`^正在\p{Han}{2,6}(?: (` + strings.Join(ActivitySites, "|") + `))?$`)

// ActivitySites are the only site names a working-row step may carry
// ("正在登录 CSDN"). A host outside this set shows the bare step, so a URL,
// account or page title never reaches the row.
var ActivitySites = []string{"CSDN", "知乎", "小红书", "微博", "掘金", "B站", "抖音", "公众号", "头条", "GitHub", "百度", "淘宝", "京东", "飞书", "钉钉", "邮箱", "论文站"}

// ActivitySiteForHost maps a host to its closed site name, or "".
func ActivitySiteForHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, m := range activityHostSites {
		if host == m[0] || strings.HasSuffix(host, "."+m[0]) {
			return m[1]
		}
	}
	return ""
}

// activity_sites.json is the host → working-row name table. Domains stay in
// that file so Go code does not name a site.
//
//go:embed activity_sites.json
var activitySitesFile []byte

var activityHostSites [][2]string

func init() {
	var rows []struct {
		Host string `json:"host"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(activitySitesFile, &rows); err != nil {
		panic("activity sites: " + err.Error())
	}
	activityHostSites = make([][2]string, 0, len(rows))
	for _, row := range rows {
		host := strings.ToLower(strings.TrimSpace(row.Host))
		name := strings.TrimSpace(row.Name)
		if host == "" || name == "" {
			continue
		}
		activityHostSites = append(activityHostSites, [2]string{host, name})
	}
}

var activityBlocked = map[string]struct{}{
	"正在处理":    {},
	"正在一点点推进": {},
	"正在询问":    {},
}

// Filler reports a sentence this surface used to invent when a run had no
// send_to_user, plus a routine body that only occupies a message.
// A real transport error is not a filler.
func Filler(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if _, ok := namedFillers[text]; ok {
		return true
	}
	if NotContent(text) {
		return true
	}
	return emptyRunFiller.MatchString(text)
}

// RealChoice is one option a person can pick. A blank label and a placeholder
// such as 无变化 are not choices.
func RealChoice(label string) bool {
	label = strings.TrimSpace(label)
	return label != "" && !Filler(label)
}

// ActivityPhrase returns phrase when it is one closed working-row step.
// Anything else, including a thinking channel, is empty.
func ActivityPhrase(phrase string) string {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" || !activityPhrase.MatchString(phrase) {
		return ""
	}
	if _, blocked := activityBlocked[phrase]; blocked {
		return ""
	}
	return phrase
}

// Visible decides the sentence a person can see.
// Waiting does not fill an empty send_to_user. Prose and Thinking are ignored.
// Arrange is always false here: planning prose is not a confirm button.
// evidenceClause is the measured cause a host used to glue onto a status
// line (a shell command, a job id, a log tail). It stays in logs.
var evidenceClause = regexp.MustCompile(`（依据：(?:[^（）]|（[^（）]*）)*）`)

// ruleTalk is the model answering an internal instruction (proxy, URL
// policy, "the system said") instead of the person's task.
var ruleTalk = regexp.MustCompile(`关于地址要求|代理替换|不存在代理|系统规则|需要 URL 的工具|内部指令|internal rule|internal instruction`)

// CleanUserText drops evidence clauses and system-rule commentary, and
// keeps one copy of a repeated sentence. The order of the remaining lines
// is the order they first appeared.
func CleanUserText(text string) string {
	text = evidenceClause.ReplaceAllString(text, "")
	raw := strings.Split(text, "\n")
	seen := map[string]bool{}
	var kept []string
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" || ruleTalk.MatchString(line) {
			continue
		}
		key := strings.Join(strings.Fields(line), "")
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func Visible(turn Turn) View {
	view := View{Activity: ActivityPhrase(turn.Activity)}
	send := CleanUserText(strings.TrimSpace(turn.SendToUser))
	if send != "" && !Filler(send) {
		view.Sentence = send
		view.Bubble = true
		return view
	}
	errText := strings.TrimSpace(turn.Error)
	if errText != "" && !Filler(errText) {
		view.Sentence = errText
		view.Bubble = true
		view.Failed = true
	}
	return view
}
