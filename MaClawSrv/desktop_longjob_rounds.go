package main

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// Making the iteration controller (desktop_longjob_iterate.go) the natural
// path. Bot 3's training run (papers 11-12 06:16-07:45) did all its rounds
// inside the first run: round 2 and 3 were launched by its own scripts, the
// test results came back in plain bash output, and it never recorded
// round_metric / decision through job_status. The watcher's gate only
// applies to watcher turns, so nothing asked for them.
//
// srv now follows rounds inside a run too. A round ends when a bash output
// shows a test / final evaluation result, or the run's watched job ends.
// It starts when the run launches another detached training command. Once
// per round, if the bot has not recorded the round, the result carries a
// note: record it with job_status (metrics srv read from the output are
// quoted), and state the assessment in the final reply. The note never
// blocks anything.

var (
	desktopRoundResultRe = regexp.MustCompile(`(?im)^.*(?:\b(?:test|testing|final|eval|evaluation|holdout|benchmark)\b|测试|评估|最终).{0,80}?(?:\b(?:acc|accuracy|f1|macro_f1|micro_f1|precision|recall|auc|roc_auc|map|bleu|rouge[a-z0-9_]*|wer|cer|mae|rmse|mse|exact_match)\b|准确率|召回率|精确率|F1)["']?\s*[:=：]\s*-?[0-9]*\.?[0-9]+.*$`)
	desktopRoundMetricRe = regexp.MustCompile(`(?i)["']?\b((?:test_|eval_|val_|dev_)?(?:accuracy|acc|macro_f1|micro_f1|f1|precision|recall|roc_auc|auc|loss|wer|cer|mae|rmse|mse|bleu|exact_match))["']?\s*[:=]\s*["']?(-?[0-9]*\.?[0-9]+)`)
	desktopRoundTrainRe  = regexp.MustCompile(`(?i)(^|[^\w])(train|training|finetune|fine-?tune|torchrun|deepspeed|accelerate\s+launch|训练|微调)([^\w]|$)`)
)

type desktopRunRounds struct {
	round      int
	resultSeen bool
	recorded   map[int]bool
	nudged     map[int]bool
}

var (
	desktopRunRoundsMu sync.Mutex
	desktopRunRoundsBy = map[string]*desktopRunRounds{}
)

func desktopRunRoundsLocked(runID string) *desktopRunRounds {
	if len(desktopRunRoundsBy) > 4096 {
		desktopRunRoundsBy = map[string]*desktopRunRounds{}
	}
	r := desktopRunRoundsBy[runID]
	if r == nil {
		r = &desktopRunRounds{round: 1, recorded: map[int]bool{}, nudged: map[int]bool{}}
		desktopRunRoundsBy[runID] = r
	}
	return r
}

// desktopRoundNoteRecorded: the run recorded a round result via job_status.
func desktopRoundNoteRecorded(runID string) {
	if strings.TrimSpace(runID) == "" {
		return
	}
	desktopRunRoundsMu.Lock()
	r := desktopRunRoundsLocked(runID)
	r.recorded[r.round] = true
	desktopRunRoundsMu.Unlock()
}

// desktopRoundResultLines: up to two lines of out that report a test or
// final evaluation metric, clipped.
func desktopRoundResultLines(out string) []string {
	var lines []string
	for _, m := range desktopRoundResultRe.FindAllString(out, -1) {
		lines = append(lines, desktopRuneHead(strings.TrimSpace(m), 200))
		if len(lines) == 2 {
			break
		}
	}
	return lines
}

// desktopRoundMetrics: "name=value" pairs read from text, first of each name.
func desktopRoundMetrics(text string) string {
	seen := map[string]bool{}
	var parts []string
	for _, m := range desktopRoundMetricRe.FindAllStringSubmatch(text, -1) {
		name := strings.ToLower(m[1])
		if seen[name] {
			continue
		}
		seen[name] = true
		parts = append(parts, name+"="+m[2])
		if len(parts) == 6 {
			break
		}
	}
	return strings.Join(parts, ", ")
}

// desktopRoundLaunches: cmd starts another training run in the background.
func desktopRoundLaunches(cmd string) bool {
	return desktopRoundTrainRe.MatchString(cmd) && (desktopLongJobDetachRe.MatchString(cmd) || desktopLongJobNohupRe.MatchString(cmd))
}

// desktopRoundNote returns the once-per-round note for this step, or "".
// launched is true when the step itself started a training round (a bash
// that srv turned into a background job).
func desktopRoundNote(scope agentruntime.Scope, cmd, out string, launched bool) string {
	runID := strings.TrimSpace(scope.RunID)
	if runID == "" {
		return ""
	}
	evidence := desktopRoundResultLines(out)
	jobID := ""
	j := desktopLongJobForRun(runID)
	if j != nil {
		jobID = j.ID
		if len(evidence) == 0 && (j.Status == "done" || j.Status == "failed") && j.RoundMetric == nil && strings.TrimSpace(j.Decision) == "" {
			if m := desktopRoundMetrics(j.LastTail); m != "" {
				evidence = []string{"job " + j.ID + " " + j.Status + ": " + m}
			} else {
				evidence = []string{"job " + j.ID + " " + j.Status}
			}
		}
	}
	if j != nil && (j.RoundMetric != nil || strings.TrimSpace(j.Decision) != "") && len(evidence) > 0 && j.Status != "running" {
		// Recorded on the job already (a watcher turn, or an earlier call).
		desktopRoundNoteRecorded(runID)
	}
	launch := launched || desktopRoundLaunches(cmd)
	desktopRunRoundsMu.Lock()
	r := desktopRunRoundsLocked(runID)
	note := ""
	if len(evidence) > 0 {
		r.resultSeen = true
	}
	round := r.round
	if r.resultSeen && (len(evidence) > 0 || launch) && !r.recorded[round] && !r.nudged[round] {
		r.nudged[round] = true
		note = desktopRoundNoteText(round, jobID, evidence, launch)
	}
	if launch && r.resultSeen {
		r.round++
		r.resultSeen = false
	}
	desktopRunRoundsMu.Unlock()
	return note
}

func desktopRoundNoteText(round int, jobID string, evidence []string, launch bool) string {
	var b strings.Builder
	b.WriteString("\n(note: ")
	if launch {
		b.WriteString("a new training round is starting, and round ")
	} else {
		b.WriteString("round ")
	}
	b.WriteString(strconv.Itoa(round))
	b.WriteString(" has a result that is not on record.")
	if len(evidence) > 0 {
		b.WriteString(" Seen: ")
		b.WriteString(strings.Join(evidence, " | "))
		if m := desktopRoundMetrics(strings.Join(evidence, "\n")); m != "" {
			b.WriteString(" (metrics: " + m + ")")
		}
		b.WriteString(".")
	}
	target := "job_id=<the training job>"
	if jobID != "" {
		target = "job_id=" + jobID
	}
	b.WriteString(" Record it: action=job_status, " + target + ", round_metric=<main metric value>, round_metric_name=<e.g. test_f1>, decision=continue|stop|ask_user, reason=<why; what the next round changes>. ")
	b.WriteString("Judge it before deciding: usable or not for the goal, the dev/test gap (overfitting, class imbalance), comparison with the baseline or earlier rounds, a few real predictions. ")
	b.WriteString("Your final reply must state that assessment: the metrics per round, usable or not, the dev/test gap, the baseline comparison, and continue or stop with the reason.)")
	return b.String()
}
