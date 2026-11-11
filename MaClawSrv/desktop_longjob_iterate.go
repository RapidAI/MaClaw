package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Experiment iteration (Daniel 11-12 02:08/02:11: 「训练时没有对模型结果进行
// 可用性/正确性判断，没有建议是否继续训练」「判断结果，并自主决定是否继续改进，
// 非常重要」). When a background job ends, the bot judges the result and
// decides itself whether to run another round. The decision is the bot's;
// this controller only keeps it honest and bounded:
//   - a new round may start from a watcher turn only after the bot recorded
//     the ended round's main metric and decision=continue (job_status
//     round_metric / round_metric_name / decision / reason), so progress is
//     measured, not claimed;
//   - at most desktopIterMaxRounds rounds per lineage;
//   - no new round after desktopIterPatience recorded rounds without a gain
//     of desktopIterMinGain over the best before them (diminishing returns);
//   - stop, ask_user or stuck are recorded and reported with the reason.

var (
	desktopIterMaxRounds = desktopEnvInt("MACLAW_ITERATE_MAX_ROUNDS", 4, 1, 20)
	desktopIterPatience  = desktopEnvInt("MACLAW_ITERATE_PATIENCE", 2, 1, 10)
	desktopIterMinGain   = desktopEnvFloat("MACLAW_ITERATE_MIN_GAIN", 0.005)
)

func desktopEnvFloat(name string, def float64) float64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || f < 0 {
		return def
	}
	return f
}

// Watcher turns (checks and resumes) by run id, so a job started inside one
// is the next round of that job's lineage.
var desktopWatchRuns sync.Map // run id -> job id

func desktopLongJobNoteWatchRun(runID, jobID string) {
	if strings.TrimSpace(runID) != "" && strings.TrimSpace(jobID) != "" {
		desktopWatchRuns.Store(runID, jobID)
	}
}

func desktopLongJobWatchParent(runID string) *desktopLongJob {
	if strings.TrimSpace(runID) == "" {
		return nil
	}
	v, ok := desktopWatchRuns.Load(runID)
	if !ok {
		return nil
	}
	return desktopLongJobGet(v.(string))
}

func desktopLongJobLineage(j *desktopLongJob) string {
	if j.Lineage != "" {
		return j.Lineage
	}
	return j.ID
}

func desktopLongJobRoundNo(j *desktopLongJob) int {
	if j.Round <= 0 {
		return 1
	}
	return j.Round
}

// desktopLongJobRounds: every round of a lineage, in order.
func desktopLongJobRounds(lineage string) []*desktopLongJob {
	desktopLongJobMu.Lock()
	defer desktopLongJobMu.Unlock()
	desktopLongJobLoadLocked()
	var out []*desktopLongJob
	for _, j := range desktopLongJobMem {
		if j.ID == lineage || j.Lineage == lineage {
			cp := *j
			out = append(out, &cp)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return desktopLongJobRoundNo(out[a]) < desktopLongJobRoundNo(out[b]) })
	return out
}

var desktopLowerBetterRe = regexp.MustCompile(`(?i)(loss|err|wer|cer|mae|mse|rmse|perplex|ppl|latency|time|fpr|fnr)`)

// desktopIterGain: how much better b is than a for this metric name.
func desktopIterGain(name string, a, b float64) float64 {
	if desktopLowerBetterRe.MatchString(name) {
		return a - b
	}
	return b - a
}

// desktopLongJobIterationGate decides whether a new round may start after
// parent; refuse is what the bot is told instead.
func desktopLongJobIterationGate(parent *desktopLongJob) (round int, refuse string) {
	round = desktopLongJobRoundNo(parent) + 1
	// A failed round may have no metric; its decision and reason still must
	// be on record (what failed, what changes).
	failedWithPlan := parent.Status == "failed" && strings.EqualFold(strings.TrimSpace(parent.Decision), "continue") && strings.TrimSpace(parent.DecisionReason) != ""
	if !failedWithPlan && (parent.RoundMetric == nil || strings.TrimSpace(parent.RoundMetricName) == "") {
		return round, "还没有记录上一轮的结果，不启动新一轮。先用 action=job_status，job_id=" + parent.ID +
			"，round_metric=<这一轮主要指标的数值>，round_metric_name=<指标名，如 test_f1>，decision=continue，reason=<为什么值得再跑一轮、这次改什么>，然后再启动下一轮。"
	}
	if d := strings.ToLower(strings.TrimSpace(parent.Decision)); d != "continue" {
		return round, "你记录的决定是 " + firstNonEmptyString(d, "（空）") + "，不启动新一轮。要继续就先把 decision 改成 continue 并写明理由。"
	}
	if round > desktopIterMaxRounds {
		return round, fmt.Sprintf("已经跑了 %d 轮（自动迭代上限 %d 轮），不再自动开新一轮。停下来，把各轮指标、评估和你的建议告诉这个人，问他是否继续。", round-1, desktopIterMaxRounds)
	}
	rounds := desktopLongJobRounds(desktopLongJobLineage(parent))
	var vals []float64
	for _, r := range rounds {
		if r.RoundMetric != nil {
			vals = append(vals, *r.RoundMetric)
		}
	}
	name := parent.RoundMetricName
	if len(vals) > desktopIterPatience {
		before := vals[:len(vals)-desktopIterPatience]
		best := before[0]
		for _, v := range before[1:] {
			if desktopIterGain(name, best, v) > 0 {
				best = v
			}
		}
		improved := false
		for _, v := range vals[len(vals)-desktopIterPatience:] {
			if desktopIterGain(name, best, v) >= desktopIterMinGain {
				improved = true
			}
		}
		if !improved {
			return round, fmt.Sprintf("最近 %d 轮 %s 没有明显提升（最好 %.4g，要求至少提升 %.3g），收益在递减，不再自动开新一轮。停下来，把各轮指标、评估和建议告诉这个人。", desktopIterPatience, name, best, desktopIterMinGain)
		}
	}
	return round, ""
}

// desktopLongJobRecordRound stores a round's result from job_status args.
func desktopLongJobRecordRound(j *desktopLongJob, args map[string]any) bool {
	changed := false
	if raw := strings.TrimSpace(desktopArg(args, "round_metric")); raw != "" {
		if f, err := strconv.ParseFloat(strings.TrimSuffix(raw, "%"), 64); err == nil {
			j.RoundMetric = &f
			changed = true
		}
	}
	for key, dst := range map[string]*string{"round_metric_name": &j.RoundMetricName, "decision": &j.Decision, "reason": &j.DecisionReason} {
		if v := strings.TrimSpace(desktopArg(args, key)); v != "" {
			*dst = desktopRuneHead(v, 300)
			changed = true
		}
	}
	return changed
}

// desktopLongJobRoundsLine is the lineage's history for the bot's view.
func desktopLongJobRoundsLine(j *desktopLongJob) string {
	rounds := desktopLongJobRounds(desktopLongJobLineage(j))
	if len(rounds) <= 1 && j.RoundMetric == nil && j.Decision == "" {
		return fmt.Sprintf("实验轮次: 第 1 轮（自动迭代上限 %d 轮）", desktopIterMaxRounds)
	}
	parts := make([]string, 0, len(rounds))
	for _, r := range rounds {
		s := fmt.Sprintf("第%d轮 %s", desktopLongJobRoundNo(r), r.Status)
		if r.RoundMetric != nil {
			s += fmt.Sprintf(" %s=%.4g", firstNonEmptyString(r.RoundMetricName, "指标"), *r.RoundMetric)
		}
		if r.Decision != "" {
			s += " → " + r.Decision
			if r.DecisionReason != "" {
				s += "（" + desktopRuneHead(r.DecisionReason, 60) + "）"
			}
		}
		parts = append(parts, s)
	}
	return fmt.Sprintf("实验轮次（上限 %d 轮）: ", desktopIterMaxRounds) + strings.Join(parts, "；")
}

const desktopIterPolicy = "判断结果并自主决定是否继续改进：结果还不够好、而且有明确可行的改进（调超参、修数据、处理类别不平衡、加训练轮数、换模型等）时，先用 action=job_status（job_id=本任务，round_metric=主要指标数值，round_metric_name=指标名，decision=continue，reason=改什么、为什么）记录这一轮，然后自己用后台任务（action=bash，background=true）启动下一轮，不用先问这个人；达到目标、连续几轮没有明显提升、需要这个人决定（花费大量资源、目标不明确）或卡住时，记录 decision=stop 或 decision=ask_user（附理由）并停止。告诉这个人：这一轮的指标、评估、决定和理由。"
