package lansengerwatch

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// Engine applies watch jobs to an incoming message.
type Engine struct {
	Store *Store
	// RunCLI can be overridden in tests.
	RunCLI func(ctx context.Context, command string, p CLIParams, timeoutSec int) CLIResult
	Now    func() time.Time
}

func (e *Engine) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) runCLI(ctx context.Context, command string, p CLIParams, timeoutSec int) CLIResult {
	if e != nil && e.RunCLI != nil {
		return e.RunCLI(ctx, command, p, timeoutSec)
	}
	return RunCLI(ctx, command, p, timeoutSec)
}

// Process evaluates all jobs for the message group and applies record/keyword/forward actions.
func (e *Engine) Process(ctx context.Context, jobs []Job, msg Incoming) ActionResult {
	var res ActionResult
	if e == nil || e.Store == nil {
		return res
	}
	if !msg.IsGroup || strings.TrimSpace(msg.GroupID) == "" {
		return res
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return res
	}
	at := e.now()
	if msg.ReceivedAt.IsZero() {
		msg.ReceivedAt = at
	}
	// Cap extremely long messages in logs/forwards (CLI already truncates content).
	const maxProcessRunes = 8000
	if utf8.RuneCountInString(text) > maxProcessRunes {
		text = TruncateRunes(text, maxProcessRunes)
	}
	msg.Text = text
	stamp := msg.ReceivedAt.Local().Format("2006-01-02 15:04:05")
	// Per job+channel: at most one forward package (keyword upgrades speech).
	forwardByKey := map[string]int{} // jobID\x00channel -> index in res.Forwards

	for _, job := range JobsForGroup(jobs, msg.GroupID) {
		if !JobNeedsMessage(job, msg.SpeakerID) {
			continue
		}
		watched := JobWatchesStaff(job, msg.SpeakerID)
		res.MatchedJobIDs = append(res.MatchedJobIDs, job.ID)

		// --- Record all speech from watched targets ---
		if watched && job.RecordAll {
			line := FormatTranscriptLine(stamp, msg.SpeakerName, msg.SpeakerID, text)
			if _, err := e.Store.AppendTranscript(job.ID, "all", line); err != nil {
				res.CLILogs = append(res.CLILogs, "record_all write: "+err.Error())
			} else {
				res.RecordedAll = true
			}
		}

		// --- Forward every speech from watched targets (may be upgraded by keyword) ---
		if watched && job.ForwardOnTargetSpeech && len(NormalizeForwardChannels(job.ForwardChannels)) > 0 {
			upsertForward(&res, forwardByKey, job, msg, "target_speech", "")
		}

		// --- Keyword rules ---
		scopeAnyone := NormalizeKeywordScope(job.KeywordScope) == KeywordScopeAnyone
		if !scopeAnyone && !watched {
			continue
		}
		for _, rule := range job.Keywords {
			if ctx != nil && ctx.Err() != nil {
				res.CLILogs = append(res.CLILogs, "context cancelled before keyword rule")
				break
			}
			kw := MatchKeyword(rule, text)
			if kw == "" {
				continue
			}
			hit := KeywordHit{
				JobID:      job.ID,
				RuleID:     rule.ID,
				Keyword:    kw,
				ReplyText:  strings.TrimSpace(rule.ReplyText),
				CLICommand: strings.TrimSpace(rule.CLICommand),
			}

			if rule.RecordOnMatch {
				line := FormatTranscriptLine(stamp, msg.SpeakerName, msg.SpeakerID, text, "KW:"+kw)
				if _, err := e.Store.AppendTranscript(job.ID, "keyword", line); err != nil {
					res.CLILogs = append(res.CLILogs, "keyword log write: "+err.Error())
				}
			}

			params := CLIParams{
				Date:        msg.ReceivedAt.Format(time.RFC3339),
				Content:     TruncateRunes(text, 4000),
				SpeakerID:   msg.SpeakerID,
				SpeakerName: msg.SpeakerName,
				GroupID:     msg.GroupID,
				GroupName:   msg.GroupName,
				Keyword:     kw,
				MessageID:   msg.MessageID,
			}

			var cliRes CLIResult
			if hit.CLICommand != "" {
				cliRes = e.runCLI(ctx, hit.CLICommand, params, rule.CLITimeoutSec)
				hit.CLIStdout = cliRes.Stdout
				hit.CLIStderr = cliRes.Stderr
				if cliRes.Err != nil {
					hit.CLIError = cliRes.Err.Error()
					res.CLILogs = append(res.CLILogs, "cli error: "+hit.CLIError)
					if cliRes.Stderr != "" {
						res.CLILogs = append(res.CLILogs, "cli stderr: "+TruncateRunes(cliRes.Stderr, 500))
					}
				} else if cliRes.Command != "" {
					res.CLILogs = append(res.CLILogs, "cli ok: "+TruncateRunes(cliRes.Command, 200))
				}
			}

			reply, usedCLI := PreferCLIStdout(rule, cliRes)
			hit.UsedCLIReply = usedCLI
			if reply != "" {
				res.Replies = append(res.Replies, reply)
			}

			// Keyword auto-replies go to the source group. Owner-channel forwards are
			// separate: explicit rule.ForwardOnMatch, or job-level "speech forward"
			// with channels configured (so keyword hits under scope=anyone still
			// notify the owner — speech forward alone only covers watched targets).
			if shouldForwardKeywordHit(job, rule) {
				hit.Forwarded = markKeywordForward(&res, forwardByKey, job, msg, kw)
			}
			res.KeywordHits = append(res.KeywordHits, hit)
		}
	}
	res.Replies = DedupeNonEmpty(res.Replies)
	return res
}

// shouldForwardKeywordHit reports whether a keyword match should also push to
// the owner's IM channels (WeChat / Lansenger / …).
func shouldForwardKeywordHit(job Job, rule KeywordRule) bool {
	// No usable channels → never claim a forward (avoids Forwarded=true with empty list).
	if len(NormalizeForwardChannels(job.ForwardChannels)) == 0 {
		return false
	}
	if rule.ForwardOnMatch {
		return true
	}
	// Job enabled "forward watched speech" + channels: also forward keyword hits
	// (including non-target speakers when scope=anyone). Without this, users see
	// group auto-replies work but never receive channel packages for keyword hits.
	return job.ForwardOnTargetSpeech
}

// upsertForward keeps one package per (job, channel). Keyword reason upgrades speech.
func upsertForward(res *ActionResult, byKey map[string]int, job Job, msg Incoming, reason, keyword string) {
	_ = markForward(res, byKey, job, msg, reason, keyword)
}

// markKeywordForward applies keyword forward and reports whether any channel was touched.
func markKeywordForward(res *ActionResult, byKey map[string]int, job Job, msg Incoming, keyword string) bool {
	return markForward(res, byKey, job, msg, "keyword", keyword)
}

func markForward(res *ActionResult, byKey map[string]int, job Job, msg Incoming, reason, keyword string) bool {
	channels := NormalizeForwardChannels(job.ForwardChannels)
	if len(channels) == 0 {
		return false
	}
	body := FormatForwardPackage(msg, reason, keyword)
	touched := false
	for _, ch := range channels {
		key := job.ID + "\x00" + ch
		if idx, ok := byKey[key]; ok {
			// Prefer keyword package over plain speech for the same channel.
			if reason == "keyword" && res.Forwards[idx].Reason != "keyword" {
				res.Forwards[idx] = ForwardRequest{
					JobID: job.ID, Channel: ch, Text: body, Reason: reason, Keyword: keyword,
				}
				touched = true
			} else if reason == "keyword" {
				touched = true // already keyword package present
			}
			continue
		}
		byKey[key] = len(res.Forwards)
		res.Forwards = append(res.Forwards, ForwardRequest{
			JobID: job.ID, Channel: ch, Text: body, Reason: reason, Keyword: keyword,
		})
		touched = true
	}
	return touched
}

// DedupeNonEmpty removes empty and duplicate strings (stable order).
func DedupeNonEmpty(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
