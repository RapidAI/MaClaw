package botmgmt

import "strings"

// MaClawSrv reports what a finished run's task really reached as
// task_outcome on the run's metadata and on its assistant message
// (corelib/agentservice TaskOutcome*). The run status stays "succeeded" for
// a run whose execution ended normally, so a "succeeded" run is only a
// finished run: every place here that tells the person or another bot that
// the work is done reads the outcome first. An older MaClawSrv sends no
// outcome; those runs keep the behaviour they had (status and reply only).
const desktopTaskOutcomeKey = "task_outcome"

// desktopTaskOutcome returns a known outcome or "" (an unknown value is
// treated as absent, never as a status).
func desktopTaskOutcome(v string) string {
	switch v = strings.TrimSpace(v); v {
	case "done", "partial", "blocked", "not_submitted", "unconfirmed", "awaiting_user", "running":
		return v
	}
	return ""
}

// desktopOutcomeNotDone: the run finished but its task did not.
func desktopOutcomeNotDone(outcome string) bool {
	o := desktopTaskOutcome(outcome)
	return o != "" && o != "done"
}

// desktopRunOutcomeMeta is the message metadata with the run's outcome
// filled in when the message lacks it (a reply rebuilt after a restart).
func desktopRunOutcomeMeta(msgMeta, runMeta map[string]string) map[string]string {
	if desktopTaskOutcome(msgMeta[desktopTaskOutcomeKey]) != "" {
		return msgMeta
	}
	o := desktopTaskOutcome(runMeta[desktopTaskOutcomeKey])
	if o == "" {
		return msgMeta
	}
	out := make(map[string]string, len(msgMeta)+1)
	for k, v := range msgMeta {
		out[k] = v
	}
	out[desktopTaskOutcomeKey] = o
	return out
}

// desktopOutcomeStatusLine is the status word for a finished run whose
// task is not done ("" for done or unknown).
func desktopOutcomeStatusLine(outcome string, hant, en bool) string {
	type words struct{ zh, hant, en string }
	w, ok := map[string]words{
		"partial":       {"只做完了一部分，还没有完成。", "只做完了一部分，還沒有完成。", "Only part of it is done; it is not finished."},
		"blocked":       {"没有做成，已经停下。", "沒有做成，已經停下。", "It could not be done; the bot stopped."},
		"not_submitted": {"还没有提交，内容没有发出去。", "還沒有提交，內容沒有發出去。", "It was not submitted; nothing went out."},
		"unconfirmed":   {"提交了，但没能确认结果，请以页面上的实际状态为准。", "提交了，但沒能確認結果，請以頁面上的實際狀態為準。", "It was submitted, but the result could not be confirmed; check the page."},
		"awaiting_user": {"还没做完，在等你回复。", "還沒做完，在等你回覆。", "Not finished; it is waiting for your answer."},
		"running":       {"后台任务还在跑，我会定时查看进度并告诉你。", "後台任務還在跑，我會定時查看進度並告訴你。", "A background job is still running; I will check in and tell you."},
	}[desktopTaskOutcome(outcome)]
	if !ok {
		return ""
	}
	switch {
	case en:
		return w.en
	case hant:
		return w.hant
	}
	return w.zh
}

// desktopOutcomeSentence puts the not-done line in front of the sentence the
// run spoke (send_to_user), so every client that shows only that sentence —
// older GUIs included — says the task is not finished. The run's own prose
// already carries MaClawSrv's line.
func desktopOutcomeSentence(outcome, sentence string) string {
	sentence = strings.TrimSpace(sentence)
	line := desktopOutcomeStatusLine(outcome, false, false)
	if line == "" || sentence == "" || strings.Contains(sentence, line) {
		return sentence
	}
	return line + "\n" + sentence
}
