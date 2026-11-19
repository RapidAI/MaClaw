package botmgmt

import (
	"context"
	"encoding/base64"
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
	wait := desktopResultFetchWait
	go func() {
		time.Sleep(wait)
		if !s.desktopResultUnfetched(tenantID, userID, botID, runID) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// The said note carries the files itself (as a server-started turn's
		// say does), so the person sees them now; only when they cannot all
		// be stored does the note name them and the result stay unacked.
		files, stored := s.storeReplyFiles(tenantID, reply)
		said := text
		if hasFiles && !stored {
			files = nil
			said += "\n\n" + note
		}
		_, err := s.sayWith(ctx, tenantID, userID, botID, "", said, "run-result:"+runID, files, "outcome")
		traceBot(botID, "result_said", err, "run", runID, "reason", "not_fetched", "files", fmt.Sprintf("%d", len(files)))
		if err == nil && (!hasFiles || stored) {
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

// storeReplyFiles keeps a result's files and pictures as mail files for the
// said note: at most three, files first. stored is false when any of them
// could not be kept, so the caller falls back to naming them.
func (s *Service) storeReplyFiles(tenantID string, reply Reply) ([]BotMailFile, bool) {
	type item struct{ name, mime, data string }
	var items []item
	for _, f := range reply.Files {
		items = append(items, item{firstNonEmptyStr(strings.TrimSpace(f.Name), "文件"), f.MIME, f.Data})
	}
	for i, im := range reply.Images {
		ext := ".png"
		if strings.Contains(im.MIME, "jpeg") || strings.Contains(im.MIME, "jpg") {
			ext = ".jpg"
		}
		items = append(items, item{fmt.Sprintf("截图%d%s", i+1, ext), im.MIME, im.Data})
	}
	if len(items) == 0 {
		return nil, true
	}
	if len(items) > 3 {
		return nil, false
	}
	out := make([]BotMailFile, 0, len(items))
	for _, it := range items {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(it.data))
		if err != nil {
			return nil, false
		}
		f, err := s.StoreBotMailFile(tenantID, it.name, it.mime, raw)
		if err != nil {
			return nil, false
		}
		out = append(out, f)
	}
	return out, true
}

func firstNonEmptyStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
