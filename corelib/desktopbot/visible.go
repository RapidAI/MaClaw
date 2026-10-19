// Package desktopbot decides what a person can see from one desktop-bot run.
// The sentence is the text of send_to_user. Assistant prose and model thinking
// stay on the run. A missing call does not invent a sentence.
package desktopbot

import (
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
var activityPhrase = regexp.MustCompile(`^正在\p{Han}{2,6}$`)

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
func Visible(turn Turn) View {
	view := View{Activity: ActivityPhrase(turn.Activity)}
	send := strings.TrimSpace(turn.SendToUser)
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
