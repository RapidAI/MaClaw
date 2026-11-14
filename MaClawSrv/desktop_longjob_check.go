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

// desktopLongJobKilledNote explains an exit that means the job was ended
// from outside, not that its work failed: papers 11-12 00:50, `timeout 7200`
// ended the local ssh wrapper (exit 124) while the training on the remote
// machine kept going and finished at 00:58. A wrapper's end is not the end of
// what it started elsewhere.
func desktopLongJobKilledNote(j *desktopLongJob) string {
	if j == nil || j.ExitCode == nil {
		return ""
	}
	var why string
	switch *j.ExitCode {
	case 124:
		why = "退出码 124：被命令自带的 timeout 时限结束，不是任务自己失败"
	case 137:
		why = "退出码 137：被 SIGKILL 结束（时限或内存不足），不是任务自己失败"
	case 143:
		why = "退出码 143：被 SIGTERM 结束，不是任务自己失败"
	default:
		return ""
	}
	if desktopLongJobLooksRemote(j.Command) {
		why += "；它是经 ssh 在另一台机器上跑的，那边的进程可能还在运行或已经跑完，先去那台机器查看再下结论"
	}
	return why
}

var desktopLongJobRemoteRe = regexp.MustCompile(`(?:^|[\s;&|('"/])(?:ssh|sshpass|rsh|mosh|kubectl\s+exec|docker\s+exec)(?:\s|'|"|$)`)

func desktopLongJobLooksRemote(cmd string) bool {
	return desktopLongJobRemoteRe.MatchString(desktopLongJobStripHeredocs(cmd))
}

func desktopLongJobAgeText(d time.Duration) string {
	if d < time.Minute {
		return "不到 1 分钟"
	}
	mins := int(d.Round(time.Minute) / time.Minute)
	if mins < 1 {
		mins = 1
	}
	if mins < 60 {
		return strconv.Itoa(mins) + " 分钟"
	}
	h, m := mins/60, mins%60
	if m == 0 {
		return strconv.Itoa(h) + " 小时"
	}
	return strconv.Itoa(h) + " 小时 " + strconv.Itoa(m) + " 分钟"
}

func desktopLongJobRanFor(d time.Duration) string {
	t := desktopLongJobAgeText(d)
	if strings.HasPrefix(t, "不到") {
		return t
	}
	return "已 " + t
}

var (
	desktopLongJobANSIRe      = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	desktopLongJobNoiseRe     = regexp.MustCompile(`(?i)^tr:\s*illegal byte sequence\.?$`)
	desktopLongJobBareNumRe   = regexp.MustCompile(`^[0-9]{1,4}$`)
	desktopLongJobBarRe       = regexp.MustCompile(`\d+%\s*\|`)
	desktopLongJobEpochPairRe = regexp.MustCompile(`(?i)\bepoch\s+([0-9]{1,6})\s*/\s*([0-9]{1,6})\b`)
	desktopLongJobEpochValRe  = regexp.MustCompile(`(?i)["']?\bepoch["']?\s*[:=]\s*["']?([0-9]+(?:\.[0-9]+)?)`)
	desktopLongJobMarkRe      = regexp.MustCompile(`(?i)(?:^|\s)(ALL_DONE|PIPELINE_DONE|TRAINING COMPLETED|TRAINING COMPLETE)(?:\s|$)`)
	desktopLongJobErrLineRe   = regexp.MustCompile(`(?i)(traceback \(most recent call last\)|cuda out of memory|\b(?:[A-Z][A-Za-z]+(?:Error|Exception)):\s*\S|^\s*error:)`)
	desktopLongJobRcLineRe    = regexp.MustCompile(`(?i)^(?:[a-z][a-z0-9_.-]{0,40}(?:_rc|_exit|_status|_exit_code|exit_code)\s*[=:]\s*-?[0-9]{1,3}|\[exit\s+-?[0-9]{1,3}\])$`)
	desktopLongJobSavedLineRe = regexp.MustCompile(`(?i)(saved|checkpoint|已保存|\.safetensors)`)
)

func desktopLongJobNormalizeTail(tail string) string {
	tail = strings.ReplaceAll(tail, "\r", "\n")
	return desktopLongJobANSIRe.ReplaceAllString(tail, "")
}

// desktopLongJobNoticeSummary is the body under the status line: progress,
// the latest metrics, and one outcome or error line. A later signal wins, so
// an error that the run then got past does not hide ALL_DONE, and a failure
// after ALL_DONE is not reported as a clean finish.
func desktopLongJobNoticeSummary(j *desktopLongJob) string {
	if j == nil || strings.TrimSpace(j.LastTail) == "" {
		return ""
	}
	var lines []string
	running := j.Status == "running" || j.Status == "stalled" || j.Status == "unwatched"
	progress := ""
	if running {
		progress = desktopLongJobProgressSentence(j)
		if progress != "" {
			lines = append(lines, progress)
		}
	}
	metrics := desktopLongJobMetricSentence(j.LastTail, !running)
	if metrics != "" {
		lines = append(lines, metrics)
	} else if !running {
		if ep := desktopLongJobEpochPhrase(j.LastTail); ep != "" {
			lines = append(lines, ep)
		}
	}
	if n := desktopLongJobNotableLine(j.LastTail); n != "" {
		if n == "脚本已跑完" && len(desktopLongJobInnerFailures(j.LastTail)) > 0 {
			n = ""
		}
		if strings.HasPrefix(n, "最近：") && (metrics != "" || progress != "") && !desktopLongJobSavedLineRe.MatchString(n) {
			n = ""
		}
		if n != "" {
			lines = append(lines, n)
		}
	}
	return strings.Join(lines, "\n")
}

func desktopLongJobProgressSentence(j *desktopLongJob) string {
	var parts []string
	if f, _, ok := desktopLongJobProgress(j.LastTail); ok {
		parts = append(parts, "进度约 "+strconv.Itoa(int(f*100+0.5))+"%")
	}
	if ep := desktopLongJobEpochPhrase(j.LastTail); ep != "" && (len(parts) == 0 || strings.Contains(ep, "/")) {
		parts = append(parts, ep)
	}
	if !j.EstimateEnd.IsZero() && j.EstimateEnd.After(desktopLongJobNow()) {
		parts = append(parts, "预计 "+j.EstimateEnd.Format("15:04")+" 结束")
	}
	return strings.Join(parts, "，")
}

func desktopLongJobEpochPhrase(tail string) string {
	tail = desktopLongJobNormalizeTail(tail)
	if ms := desktopLongJobEpochPairRe.FindAllStringSubmatch(tail, -1); len(ms) > 0 {
		last := ms[len(ms)-1]
		return "第 " + last[1] + "/" + last[2] + " 轮"
	}
	ms := desktopLongJobEpochValRe.FindAllStringSubmatch(tail, -1)
	if len(ms) == 0 {
		return ""
	}
	f, err := strconv.ParseFloat(ms[len(ms)-1][1], 64)
	if err != nil || f <= 0 {
		return ""
	}
	if f == float64(int(f)) {
		return "第 " + strconv.Itoa(int(f)) + " 轮"
	}
	return "epoch " + strconv.FormatFloat(f, 'f', 2, 64)
}

func desktopLongJobMetricHits(tail string) map[string]string {
	hits := map[string]string{}
	for _, m := range desktopRoundMetricRe.FindAllStringSubmatch(desktopLongJobNormalizeTail(tail), -1) {
		hits[strings.ToLower(m[1])] = m[2]
	}
	return hits
}

func desktopLongJobTrimNum(s string) string {
	// A short literal is already what the log printed. Running it through
	// float64 turns 0.9335 into 0.933.
	if len(s) <= 6 && !strings.ContainsAny(s, "eE") {
		return s
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	abs := f
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= 100:
		return strconv.FormatFloat(f, 'f', 1, 64)
	default:
		return strconv.FormatFloat(f, 'f', 4, 64)
	}
}

func desktopLongJobMetricSentence(tail string, withEpoch bool) string {
	hits := desktopLongJobMetricHits(tail)
	if len(hits) == 0 {
		return ""
	}
	pick := func(keys ...string) (string, string, bool) {
		for _, k := range keys {
			if v, ok := hits[k]; ok {
				return k, desktopLongJobTrimNum(v), true
			}
		}
		return "", "", false
	}
	var parts []string
	add := func(base string, keys ...string) {
		key, v, ok := pick(keys...)
		if !ok {
			return
		}
		label := base
		switch {
		case strings.HasPrefix(key, "test_"):
			label = "测试" + base
		case strings.HasPrefix(key, "eval_"), strings.HasPrefix(key, "val_"), strings.HasPrefix(key, "dev_"):
			label = "验证" + base
		}
		if base == "F1" && label != base {
			label = strings.TrimSuffix(label, "F1") + " F1"
		}
		parts = append(parts, label+" "+v)
	}
	add("准确率", "eval_accuracy", "test_accuracy", "val_accuracy", "dev_accuracy", "accuracy", "eval_acc", "test_acc", "val_acc", "acc")
	add("F1", "eval_f1", "test_f1", "val_f1", "dev_f1", "macro_f1", "micro_f1", "f1")
	add("精确率", "eval_precision", "test_precision", "val_precision", "dev_precision", "precision")
	add("召回率", "eval_recall", "test_recall", "val_recall", "dev_recall", "recall")
	if _, _, ok := pick("eval_loss", "test_loss", "val_loss", "dev_loss"); ok {
		add("损失", "eval_loss", "test_loss", "val_loss", "dev_loss")
	} else {
		add("损失", "loss")
	}
	if len(parts) == 0 {
		return ""
	}
	head := "指标"
	if withEpoch {
		if ep := desktopLongJobEpochPhrase(tail); ep != "" {
			head += "（" + ep + "）"
		}
	}
	return head + "：" + strings.Join(parts, "，")
}

func desktopLongJobReadableLines(tail string) []string {
	var out []string
	for _, l := range strings.Split(desktopLongJobNormalizeTail(tail), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || desktopLongJobNoiseRe.MatchString(l) || desktopLongJobBareNumRe.MatchString(l) {
			continue
		}
		if strings.Contains(l, "Permanently added") {
			continue
		}
		if desktopLongJobBarRe.MatchString(l) && !desktopRoundMetricRe.MatchString(l) {
			continue
		}
		out = append(out, l)
	}
	return out
}

func desktopLongJobMetricBlob(l string) bool {
	return desktopRoundMetricRe.MatchString(l) && (strings.Contains(l, "{") || strings.Contains(l, "eval_") || strings.Count(l, ":") >= 2)
}

func desktopLongJobBoringLine(l string) bool {
	if desktopLongJobRcLineRe.MatchString(l) {
		return true
	}
	lead := strings.TrimLeft(l, " \t")
	return lead == "" || lead[0] == '=' || lead[0] == '#'
}

func desktopLongJobNotableLine(tail string) string {
	var kind, text, plain string
	for _, l := range desktopLongJobReadableLines(tail) {
		switch {
		case desktopLongJobErrLineRe.MatchString(l):
			kind, text = "err", l
		case desktopLongJobMarkRe.MatchString(l):
			kind, text = "mark", l
		case desktopLongJobMetricBlob(l) || desktopLongJobBoringLine(l):
		default:
			plain = l
		}
	}
	switch kind {
	case "err":
		return "出错：" + desktopRuneHead(desktopLongJobSecretRe.ReplaceAllString(text, "$1***"), 120)
	case "mark":
		return "脚本已跑完"
	}
	if plain == "" {
		return ""
	}
	return "最近：" + desktopRuneHead(desktopLongJobSecretRe.ReplaceAllString(plain, "$1***"), 120)
}
