package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// What a run left behind and why it stopped, for its final reply (papers
// 11-11 run_50546ca1: the reply said 「接连几步都失败了」 without the cause —
// Hub had been restarted — and without the Zhihu draft 「测试2」 it created
// and the one 发布 click it made). Kept apart from the publish record, which
// the reply-link step takes, so the outcome notice can always read it.

var (
	desktopSideMu      sync.Mutex
	desktopSideEffects = map[string][]string{}
	desktopSideCause   = map[string]string{}
)

func desktopSideNote(runID, line string) {
	runID, line = strings.TrimSpace(runID), strings.TrimSpace(line)
	if runID == "" || line == "" {
		return
	}
	desktopSideMu.Lock()
	defer desktopSideMu.Unlock()
	if len(desktopSideEffects) > 2048 {
		desktopSideEffects = map[string][]string{}
	}
	for _, have := range desktopSideEffects[runID] {
		if have == line {
			return
		}
	}
	list := append(desktopSideEffects[runID], line)
	if len(list) > 6 {
		list = list[len(list)-6:]
	}
	desktopSideEffects[runID] = list
}

// desktopSideNoteCause records why work could not go on (first cause wins).
func desktopSideNoteCause(runID, cause string) {
	runID = strings.TrimSpace(runID)
	if runID == "" || cause == "" {
		return
	}
	desktopSideMu.Lock()
	defer desktopSideMu.Unlock()
	if len(desktopSideCause) > 2048 {
		desktopSideCause = map[string]string{}
	}
	if desktopSideCause[runID] == "" {
		desktopSideCause[runID] = cause
	}
}

// desktopSideClearOutage forgets an outage cause once a later desktop step
// of the run worked: the screen did open, so it is not why the run ended
// (papers 2026-11-11: Bot 2 waited for its screen at 14:33, then posted to
// 知乎 and 小红书, and its final reply still said 「原因：这台 bot 的电脑屏幕一直没能打开」).
func desktopSideClearOutage(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	desktopSideMu.Lock()
	defer desktopSideMu.Unlock()
	switch desktopSideCause[runID] {
	case desktopOutageCauseCold, desktopOutageCauseHub:
		delete(desktopSideCause, runID)
	}
}

// desktopSideTake returns and forgets the run's cause and side effects.
func desktopSideTake(runID string) (string, []string) {
	desktopSideMu.Lock()
	defer desktopSideMu.Unlock()
	cause, list := desktopSideCause[runID], desktopSideEffects[runID]
	delete(desktopSideCause, runID)
	delete(desktopSideEffects, runID)
	return cause, list
}

// desktopSideNotePublish turns one publish call's report into the side
// effect the person needs to know about when the post did not go out.
func desktopSideNotePublish(runID string, spec browser.PublishSpec, rep *browser.PublishReport) {
	if rep == nil {
		return
	}
	switch rep.State {
	case browser.PublishLive, browser.PublishLiveIncomplete:
		return // the reply link names live posts
	}
	submitted := false
	for _, st := range rep.Steps {
		if st.Name == "submit" {
			submitted = true
		}
	}
	host := desktopLedgerHost(firstNonEmptyStr(rep.EditorPage, spec.EditorURL))
	if host == "" {
		host = "站点"
	}
	var b strings.Builder
	b.WriteString(host + "：")
	if spec.Title != "" {
		fmt.Fprintf(&b, "编辑器里留下了《%s》", desktopRuneHead(spec.Title, 30))
	} else {
		b.WriteString("编辑器里留下了未完成的内容")
	}
	if rep.EditorPage != "" {
		b.WriteString("（" + rep.EditorPage + "）")
	}
	switch {
	case rep.State == browser.PublishSubmitted:
		b.WriteString("，点过发布但没能确认是否发出，请以页面为准")
	case submitted:
		b.WriteString("，点过一次发布，没有发出去")
	default:
		b.WriteString("，没有发布")
	}
	desktopSideNote(runID, b.String())
}

// desktopSideNotice is the extra notice lines for a run that is not done.
func desktopSideNotice(runID string) string {
	cause, list := desktopSideTake(runID)
	var lines []string
	if cause != "" {
		lines = append(lines, "原因："+cause)
	}
	if len(list) > 0 {
		lines = append(lines, "留下的痕迹："+strings.Join(list, "；"))
	}
	return strings.Join(lines, "\n")
}
