package botmgmt

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// desktopResultFetchWait is how long a finished run's result may wait for
// the GUI before Hub tells the person itself. Tests shorten it.
var desktopResultFetchWait = 2 * time.Minute

// sayUnfetchedResultLater: a run finished but no client took its result
// (the GUI was closed, or lost the wait), so the person would see the
// acknowledgement and then nothing (papers 2026-11-11, Bot 3). After
// desktopResultFetchWait, a result nobody acked is said to the person in
// the bot's own conversation. The say key is the run, so it is said once,
// however often this runs.
func (s *Service) sayUnfetchedResultLater(tenantID, userID, botID, runID string, reply Reply) {
	if s == nil || strings.TrimSpace(runID) == "" || strings.TrimSpace(botID) == "" {
		return
	}
	text := strings.TrimSpace(reply.SendToUser)
	if text == "" {
		return // nothing for the person: a quiet finish stays quiet
	}
	// A say carries text only. The result's pictures and files stay with
	// the run (papers 11-11 22:12: Bot 2's reply said 「两个文件都附在这条消息里」
	// but the said copy had no snake.html / screenshots, and the ack then
	// dropped them for good). The said text names what was not shown, and a
	// result with attachments is not acked, so the GUI's pending pull still
	// brings the files when it next runs.
	note, hasFiles := unfetchedAttachmentNote(reply)
	if note != "" {
		text += "\n\n" + note
	}
	wait := desktopResultFetchWait
	go func() {
		time.Sleep(wait)
		if !s.desktopResultUnfetched(tenantID, userID, botID, runID) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := s.Say(ctx, tenantID, userID, botID, "", text, "run-result:"+runID)
		traceBot(botID, "result_said", err, "run", runID, "reason", "not_fetched")
		if err == nil && !hasFiles {
			// Said once: a GUI that opens later must not show it again.
			_ = s.AckDesktopTask(ctx, tenantID, userID, botID, runID)
		}
	}()
}

// desktopResultUnfetched: the run's admit is still here and not acked.
// AckDesktopTask removes the admit, so an acked result is never said again.
func (s *Service) desktopResultUnfetched(tenantID, userID, botID, runID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.desktopAdmits[desktopAdmitKey(tenantID, userID, botID, runID)]
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.done && !state.delivered
}

// unfetchedAttachmentNote names the result's pictures and files that a said
// copy cannot show.
func unfetchedAttachmentNote(reply Reply) (string, bool) {
	var names []string
	for _, f := range reply.Files {
		if n := strings.TrimSpace(f.Name); n != "" {
			names = append(names, n)
		} else {
			names = append(names, "文件")
		}
	}
	if len(reply.Images) > 0 {
		names = append(names, fmt.Sprintf("%d 张图片", len(reply.Images)))
	}
	if len(names) == 0 {
		return "", false
	}
	return "（这条结果带有附件：" + strings.Join(names, "、") + "。这里只能显示文字；附件会在这个 bot 的对话重新取回结果时出现。）", true
}
