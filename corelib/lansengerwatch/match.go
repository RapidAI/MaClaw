package lansengerwatch

import (
	"strings"
	"time"
	"unicode/utf8"
)

// NormalizeStaffID trims staff ids for map keys.
func NormalizeStaffID(id string) string {
	return strings.TrimSpace(id)
}

// JobWatchesStaff reports whether job targets this speaker.
func JobWatchesStaff(job Job, staffID string) bool {
	staffID = NormalizeStaffID(staffID)
	if staffID == "" || !job.Enabled {
		return false
	}
	for _, id := range job.TargetStaffIDs {
		if NormalizeStaffID(id) == staffID {
			return true
		}
	}
	return false
}

// JobsForGroup returns enabled jobs bound to groupID.
func JobsForGroup(jobs []Job, groupID string) []Job {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil
	}
	var out []Job
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		if strings.TrimSpace(j.GroupID) == groupID {
			out = append(out, j)
		}
	}
	return out
}

// AnyActiveWatchForGroup is a fast prefilter for the gateway.
func AnyActiveWatchForGroup(jobs []Job, groupID string) bool {
	return len(JobsForGroup(jobs, groupID)) > 0
}

// NormalizeKeywordScope returns targets|anyone (default targets).
func NormalizeKeywordScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case KeywordScopeAnyone, "all", "any":
		return KeywordScopeAnyone
	default:
		return KeywordScopeTargets
	}
}

// JobHasKeywords reports whether any rule has a non-empty keyword list.
func JobHasKeywords(job Job) bool {
	for _, r := range job.Keywords {
		for _, kw := range r.Keywords {
			if strings.TrimSpace(kw) != "" {
				return true
			}
		}
	}
	return false
}

// JobNeedsMessage reports whether an enabled job may act on this speaker
// (record-all for targets, keyword anyone/targets, or forward-on-speech).
func JobNeedsMessage(job Job, staffID string) bool {
	return JobNeedsMessageFor(job, JobWatchesStaff(job, staffID))
}

// JobNeedsMessageFor is JobNeedsMessage with a precomputed watched flag
// (avoids a second TargetStaffIDs scan on the engine hot path).
func JobNeedsMessageFor(job Job, watched bool) bool {
	if !job.Enabled {
		return false
	}
	if watched && (job.RecordAll || job.ForwardOnTargetSpeech) {
		return true
	}
	if !JobHasKeywords(job) {
		return false
	}
	if NormalizeKeywordScope(job.KeywordScope) == KeywordScopeAnyone {
		return true
	}
	return watched
}

// HasEnabledJobs is a cheap prefilter for the gateway hot path.
func HasEnabledJobs(jobs []Job) bool {
	for _, j := range jobs {
		if j.Enabled {
			return true
		}
	}
	return false
}

// GroupNeedsWatchMessage is a prefilter: any job on the group may care about this speaker.
func GroupNeedsWatchMessage(jobs []Job, groupID, staffID string) bool {
	for _, j := range JobsForGroup(jobs, groupID) {
		if JobNeedsMessage(j, staffID) {
			return true
		}
	}
	return false
}

// NormalizeForwardChannel maps aliases to canonical channel ids.
func NormalizeForwardChannel(ch string) string {
	switch strings.ToLower(strings.TrimSpace(ch)) {
	case ChannelWeixin, "wechat", "weixin_local", "wx":
		return ChannelWeixin
	case ChannelLansenger, "lanxin", "lansenger_local", "蓝信":
		return ChannelLansenger
	case ChannelTelegram, "tg", "telegram_local":
		return ChannelTelegram
	case ChannelQQ, "qqbot", "qq_local":
		return ChannelQQ
	case ChannelHub, "proactive", "active":
		return ChannelHub
	default:
		return ""
	}
}

// NormalizeForwardChannels dedupes and canonicalizes channel list.
func NormalizeForwardChannels(chs []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(chs))
	for _, c := range chs {
		id := NormalizeForwardChannel(c)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// FormatForwardPackage builds the IM-channel forward body (to the owner's channel).
func FormatForwardPackage(msg Incoming, reason, keyword string) string {
	at := msg.ReceivedAt
	if at.IsZero() {
		at = time.Now()
	}
	stamp := at.Local().Format("2006-01-02 15:04:05")
	group := strings.TrimSpace(msg.GroupName)
	gid := strings.TrimSpace(msg.GroupID)
	switch {
	case group != "" && gid != "" && group != gid:
		group = group + " (id=" + gid + ")"
	case group == "":
		group = gid
	}
	if group == "" {
		group = "(未知群)"
	}
	who := strings.TrimSpace(msg.SpeakerName)
	sid := strings.TrimSpace(msg.SpeakerID)
	switch {
	case who != "" && sid != "" && who != sid:
		who = who + " (staffId=" + sid + ")"
	case who == "":
		who = sid
	}
	if who == "" {
		who = "(未知)"
	}
	var b strings.Builder
	b.WriteString("【盯人转发】\n")
	b.WriteString("时间: " + stamp + "\n")
	b.WriteString("群: " + group + "\n")
	b.WriteString("说话人: " + who + "\n")
	switch reason {
	case "keyword":
		if kw := strings.TrimSpace(keyword); kw != "" {
			b.WriteString("触发: 关键字「" + kw + "」\n")
		} else {
			b.WriteString("触发: 关键字\n")
		}
	default:
		b.WriteString("触发: 关注对象发言\n")
	}
	b.WriteString("内容:\n")
	b.WriteString(strings.TrimSpace(msg.Text))
	return b.String()
}

// MatchKeyword returns the first matching keyword in rule, or "".
func MatchKeyword(rule KeywordRule, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	var lowerText string
	if !rule.CaseSensitive {
		lowerText = strings.ToLower(text)
	}
	for _, kw := range rule.Keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		if rule.CaseSensitive {
			if strings.Contains(text, kw) {
				return kw
			}
			continue
		}
		if strings.Contains(lowerText, strings.ToLower(kw)) {
			return kw
		}
	}
	return ""
}

// FilterMembers filters roster by query against name or staff id (case-insensitive).
func FilterMembers(members []Member, query string) []Member {
	query = strings.TrimSpace(query)
	if query == "" {
		return members
	}
	q := strings.ToLower(query)
	out := make([]Member, 0, len(members))
	for _, m := range members {
		if strings.Contains(strings.ToLower(m.StaffID), q) ||
			strings.Contains(strings.ToLower(m.Name), q) {
			out = append(out, m)
		}
	}
	return out
}

// FormatTranscriptLine builds one append line for the text log.
func FormatTranscriptLine(at string, speakerName, speakerID, text string, tags ...string) string {
	who := strings.TrimSpace(speakerName)
	id := strings.TrimSpace(speakerID)
	switch {
	case who != "" && id != "" && who != id:
		who = who + "(" + id + ")"
	case who == "":
		who = id
	}
	if who == "" {
		who = "?"
	}
	tag := ""
	if len(tags) > 0 {
		var parts []string
		for _, t := range tags {
			t = strings.TrimSpace(t)
			if t != "" {
				parts = append(parts, t)
			}
		}
		if len(parts) > 0 {
			tag = " [" + strings.Join(parts, "|") + "]"
		}
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return at + " " + who + tag + ": " + text + "\n"
}

// TruncateRunes limits a string for CLI args / logs.
func TruncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
