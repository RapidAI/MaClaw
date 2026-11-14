package main

import (
	"strconv"
	"strings"
	"time"
)

// desktopRunStatusLine is the live status of a run for the bot's status area
// (above its chat), not a chat message (Daniel 11-11 21:10): the run id,
// how far it is, its latest measurable progress, and any background job with
// its next watcher check. Chat keeps only stage results, a stall, a failure
// and the final reply.
func desktopRunStatusLine(runID string) string {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return ""
	}
	parts := []string{"单号 " + desktopRuneHead(runID, 12)}
	if snap, ok := desktopWatchdogSnapshot(runID); ok {
		if snap.steps > 0 {
			parts = append(parts, "第 "+strconv.Itoa(snap.steps)+" 步")
		}
		why := strings.TrimSpace(snap.stageWhy)
		if why == "" {
			why = strings.TrimSpace(snap.progressWhy)
		}
		if why != "" && !snap.lastProgress.IsZero() {
			parts = append(parts, snap.lastProgress.Format("15:04")+" "+desktopRuneHead(strings.ReplaceAll(why, "\n", " "), 60))
		}
		if snap.stallWarned {
			parts = append(parts, "自 "+snap.lastProgress.Format("15:04")+" 起没有可测的进展")
		}
	}
	if note := desktopSupplementStatusPhrase(runID); note != "" {
		parts = append(parts, note)
	}
	if j := desktopLongJobForRun(runID); j != nil {
		parts = append(parts, desktopLongJobStatusPhrase(j, desktopLongJobNow()))
	}
	if note := desktopLatestProcess(runID); note != "" {
		parts = append(parts, desktopRuneHead(strings.ReplaceAll(note, "\n", " "), 80))
	}
	return strings.Join(parts, " · ")
}

func desktopLongJobStatusPhrase(j *desktopLongJob, now time.Time) string {
	age := strconv.Itoa(int(now.Sub(j.Started).Minutes()))
	switch j.Status {
	case "running":
		s := "后台任务运行中（已 " + age + " 分钟）"
		if !j.NextCheck.IsZero() {
			s += "，下次查看 " + j.NextCheck.Format("15:04")
			if r := strings.TrimSpace(j.CheckReason); r != "" {
				s += "（" + desktopRuneHead(r, 40) + "）"
			}
		}
		return s
	case "stalled":
		return "后台任务还在，但 " + j.stallBound().Round(time.Minute).String() + " 没有新日志"
	case "done":
		return desktopLongJobDoneHead(j)
	default:
		return "后台任务失败"
	}
}
