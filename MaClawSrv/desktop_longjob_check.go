package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// longJobCheck: the estimated check of a job whose own log states nothing
// measurable (papers 11-11 23:49: Bot 1's BERT pipeline wrote its steps and
// metrics to files on the remote machine; job.log said only "bert training
// start", so the watcher could neither see the dev F1 nor that training had
// slowed from 24 to 5.7 samples/s and would outrun the command's own
// 2-hour timeout; it called the job "stalled" instead). Only the bot knows
// where its progress is written, so the check is a short bot turn that reads
// it and speaks only when something is new; no canned line with an empty
// tail.
const longJobCheck desktopLongJobDecision = "check"

// desktopLongJobTailMeasurable: the job's log states progress or metrics.
func desktopLongJobTailMeasurable(tail string) bool {
	if _, _, ok := desktopLongJobProgress(tail); ok {
		return true
	}
	return desktopRunProgressLines(tail) != ""
}

var desktopLongJobTimeoutRe = regexp.MustCompile(`(?:^|[\s;&|('"])timeout\s+(?:(?:-[ks]\s+\S+|--\S+|-\S+)\s+)*([0-9]+(?:\.[0-9]+)?)([smhd]?)(?:\s|$)`)

// desktopLongJobTimeLimit is the command's own time limit (`timeout N`), the
// first, outermost one; 0 when there is none.
func desktopLongJobTimeLimit(cmd string) time.Duration {
	m := desktopLongJobTimeoutRe.FindStringSubmatch(desktopLongJobStripHeredocs(cmd))
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || v <= 0 {
		return 0
	}
	unit := time.Second
	switch m[2] {
	case "m":
		unit = time.Minute
	case "h":
		unit = time.Hour
	case "d":
		unit = 24 * time.Hour
	}
	return time.Duration(v * float64(unit))
}

// desktopLongJobDeadline is when the command's own limit ends it.
func desktopLongJobDeadline(j *desktopLongJob) time.Time {
	if j == nil || j.Started.IsZero() {
		return time.Time{}
	}
	if d := desktopLongJobTimeLimit(j.Command); d > 0 {
		return j.Started.Add(d)
	}
	return time.Time{}
}

// desktopLongJobPastDeadline: the job's estimated end is after its own
// time limit, not yet told.
func desktopLongJobPastDeadline(j *desktopLongJob) bool {
	if j == nil || j.DeadlineSaid || j.EstimateEnd.IsZero() {
		return false
	}
	dl := desktopLongJobDeadline(j)
	return !dl.IsZero() && j.EstimateEnd.After(dl)
}

var desktopLongJobInnerRcRe = regexp.MustCompile(`(?i)(?:^|[\s\[(,;])([a-z][a-z0-9_.-]{0,40}?(?:_rc|_exit|_status|_exit_code|exit_code))\s*[=:]\s*(-?[0-9]{1,3})\b`)
var desktopLongJobExitBracketRe = regexp.MustCompile(`(?i)\[exit\s+(-?[0-9]{1,3})\]`)

// desktopLongJobInnerFailures lists the steps a job's own output says failed
// ("train_rc=1", "[exit 1]"): the wrapper can exit 0 while a step inside it
// did not succeed (papers 11-11: 「退出码 0」 with train_rc=1 in the tail).
// The latest value per step counts.
func desktopLongJobInnerFailures(tail string) []string {
	last := map[string]string{}
	var order []string
	for _, line := range strings.Split(tail, "\n") {
		for _, m := range desktopLongJobInnerRcRe.FindAllStringSubmatch(line, -1) {
			name := strings.ToLower(m[1])
			if _, ok := last[name]; !ok {
				order = append(order, name)
			}
			last[name] = m[2]
		}
		for _, m := range desktopLongJobExitBracketRe.FindAllStringSubmatch(line, -1) {
			if _, ok := last["exit"]; !ok {
				order = append(order, "exit")
			}
			last["exit"] = m[1]
		}
	}
	var out []string
	for _, name := range order {
		if v := last[name]; v != "0" {
			if name == "exit" {
				out = append(out, "[exit "+v+"]")
			} else {
				out = append(out, name+"="+v)
			}
		}
	}
	return out
}

// desktopLongJobDoneHead: the honest finish phrase for a job whose wrapper
// exited 0.
func desktopLongJobDoneHead(j *desktopLongJob) string {
	if f := desktopLongJobInnerFailures(j.LastTail); len(f) > 0 {
		return "后台命令已结束（退出码 0），但输出显示有步骤没成功：" + strings.Join(f, "，")
	}
	return "后台任务已结束（退出码 0）"
}

// desktopLongJobLimitLine names the command's own time limit for the bot.
func desktopLongJobLimitLine(j *desktopLongJob) string {
	dl := desktopLongJobDeadline(j)
	if dl.IsZero() {
		return ""
	}
	s := "命令自带时限: " + desktopLongJobTimeLimit(j.Command).String() + "，到 " + dl.Format("15:04") + " 会被强制结束"
	if !j.EstimateEnd.IsZero() && j.EstimateEnd.After(dl) {
		s += "；按目前估计 " + j.EstimateEnd.Format("15:04") + " 才结束，会超过时限"
	}
	return s
}
