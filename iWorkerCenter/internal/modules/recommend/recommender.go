package recommend

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// ColleagueProfile holds the data needed for matching.
type ColleagueProfile struct {
	ID        string
	Name      string
	RoleCode  string
	RoleName  string
	Strengths []string
	Tasks     []string
}

// Recommendation is a scored colleague match.
type Recommendation struct {
	ColleagueID string  `json:"colleague_id"`
	Name        string  `json:"name"`
	RoleCode    string  `json:"role_code"`
	Score       float64 `json:"score"`
	Reason      string  `json:"reason"`
}

// Recommend returns colleagues ranked by relevance to the given task description.
// Pure function, no IO. Uses keyword overlap scoring.
func Recommend(taskDesc string, colleagues []ColleagueProfile, topN int) []Recommendation {
	if len(colleagues) == 0 || taskDesc == "" {
		return nil
	}
	if topN <= 0 {
		topN = 3
	}

	taskWords := tokenize(taskDesc)
	if len(taskWords) == 0 {
		return nil
	}

	type scored struct {
		profile ColleagueProfile
		score   float64
		reason  string
	}

	var results []scored
	for _, c := range colleagues {
		s, reason := scoreColleague(taskWords, c)
		if s > 0 {
			results = append(results, scored{profile: c, score: s, reason: reason})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if len(results) > topN {
		results = results[:topN]
	}

	recs := make([]Recommendation, 0, len(results))
	for _, r := range results {
		recs = append(recs, Recommendation{
			ColleagueID: r.profile.ID,
			Name:        r.profile.Name,
			RoleCode:    r.profile.RoleCode,
			Score:       r.score,
			Reason:      r.reason,
		})
	}
	return recs
}

func scoreColleague(taskWords []string, c ColleagueProfile) (float64, string) {
	var score float64
	var reasons []string

	// Match against strengths (weight 3)
	for _, strength := range c.Strengths {
		sWords := tokenize(strength)
		for _, tw := range taskWords {
			for _, sw := range sWords {
				if tw == sw || strings.Contains(tw, sw) || strings.Contains(sw, tw) {
					score += 3
					reasons = append(reasons, "擅长「"+strength+"」")
					goto nextStrength
				}
			}
		}
	nextStrength:
	}

	// Match against tasks (weight 2)
	for _, task := range c.Tasks {
		tWords := tokenize(task)
		for _, tw := range taskWords {
			for _, ttw := range tWords {
				if tw == ttw || strings.Contains(tw, ttw) || strings.Contains(ttw, tw) {
					score += 2
					reasons = append(reasons, "会做「"+task+"」")
					goto nextTask
				}
			}
		}
	nextTask:
	}

	// Match against role name (weight 1)
	roleWords := tokenize(c.RoleName)
	for _, tw := range taskWords {
		for _, rw := range roleWords {
			if tw == rw || strings.Contains(tw, rw) || strings.Contains(rw, tw) {
				score += 1
				reasons = append(reasons, "角色匹配")
				goto doneRole
			}
		}
	}
doneRole:

	// Role code keyword match (weight 1.5)
	roleKeywords := roleCodeKeywords(c.RoleCode)
	for _, tw := range taskWords {
		for _, rk := range roleKeywords {
			if tw == rk || strings.Contains(tw, rk) || strings.Contains(rk, tw) {
				score += 1.5
				goto doneRoleKw
			}
		}
	}
doneRoleKw:

	reason := ""
	if len(reasons) > 0 {
		seen := make(map[string]bool)
		var unique []string
		for _, r := range reasons {
			if !seen[r] {
				seen[r] = true
				unique = append(unique, r)
			}
		}
		if len(unique) > 3 {
			unique = unique[:3]
		}
		reason = strings.Join(unique, "，")
	}

	return score, reason
}

// roleCodeKeywords maps role codes to domain keywords for matching.
func roleCodeKeywords(code string) []string {
	switch code {
	case "office":
		return []string{"通知", "纪要", "周报", "邮件", "公文", "汇报", "办公", "文档", "写作"}
	case "data":
		return []string{"数据", "表格", "图表", "分析", "汇总", "统计", "报表", "可视化"}
	case "production":
		return []string{"生产", "日报", "交接", "产线", "异常", "设备", "产量", "工单"}
	case "quality":
		return []string{"质量", "质检", "整改", "问题", "缺陷", "审核", "合格", "不良"}
	default:
		return nil
	}
}

// tokenize splits Chinese/English text into searchable tokens.
func tokenize(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}

	var tokens []string
	// Split by common delimiters
	for _, part := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '，' || r == '、' || r == '。' || r == '；' ||
			r == '：' || r == ',' || r == ';' || r == ':' || r == '.' ||
			r == '\n' || r == '\t' || r == '（' || r == '）' || r == '(' || r == ')'
	}) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tokens = append(tokens, part)
		// For Chinese text, also generate 2-gram tokens
		runes := []rune(part)
		if utf8.RuneCountInString(part) >= 2 && isChinese(runes[0]) {
			for i := 0; i+2 <= len(runes); i++ {
				tokens = append(tokens, string(runes[i:i+2]))
			}
		}
	}
	return tokens
}

func isChinese(r rune) bool {
	return r >= 0x4e00 && r <= 0x9fff
}
