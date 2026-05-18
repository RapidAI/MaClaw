package memory

import (
	"sort"
	"strings"
)

const (
	lightMemDefaultMaxItems  = 8
	lightMemDefaultMaxTokens = 1600
)

// LightMemRoute is one planned retrieval lane. It mirrors LightMem's online
// controller idea: decide where to look, how many items to take, then keep the
// final prompt budget fixed.
type LightMemRoute struct {
	Name          string   `json:"name"`
	Query         string   `json:"query"`
	Category      Category `json:"category,omitempty"`
	Budget        int      `json:"budget"`
	ProjectScoped bool     `json:"project_scoped,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

// LightMemRecallPlan describes the deterministic online recall plan and the
// two-stage selection result. It is safe to expose in CLI/tool debug output.
type LightMemRecallPlan struct {
	Query                string          `json:"query"`
	NeedMemory           bool            `json:"need_memory"`
	Reason               string          `json:"reason,omitempty"`
	MaxItems             int             `json:"max_items"`
	TokenBudget          int             `json:"token_budget"`
	Routes               []LightMemRoute `json:"routes,omitempty"`
	RouteCandidateCounts map[string]int  `json:"route_candidate_counts,omitempty"`
	ResultEntryIDs       []string        `json:"result_entry_ids,omitempty"`
}

// BuildLightMemRecallPlan creates a lightweight online retrieval plan without
// calling an LLM. Explicit category filters are honored as a single lane;
// otherwise the query is routed across user, project, task-artifact, and
// general-memory lanes based on cheap intent signals.
func BuildLightMemRecallPlan(query string, category Category, projectPath string) LightMemRecallPlan {
	q := strings.TrimSpace(query)
	plan := LightMemRecallPlan{
		Query:       q,
		NeedMemory:  true,
		MaxItems:    lightMemDefaultMaxItems,
		TokenBudget: lightMemDefaultMaxTokens,
	}
	if q == "" {
		plan.NeedMemory = false
		plan.Reason = "empty query"
		return plan
	}
	if category != "" {
		plan.Routes = []LightMemRoute{{
			Name:          "explicit_category",
			Query:         q,
			Category:      category,
			Budget:        plan.MaxItems,
			ProjectScoped: category == CategoryProjectKnowledge || category == CategoryProject || category == CategoryTaskArtifact,
			Reason:        "caller supplied category filter",
		}}
		plan.Reason = "explicit category filter"
		return plan
	}
	if lightMemLooksLikeNoMemory(q) {
		plan.NeedMemory = false
		plan.Reason = "small talk or acknowledgement"
		return plan
	}

	var routes []LightMemRoute
	lq := strings.ToLower(q)
	add := func(route LightMemRoute) {
		if route.Budget <= 0 {
			route.Budget = 3
		}
		key := route.Name + "\x00" + string(route.Category) + "\x00" + route.Query
		for _, existing := range routes {
			if existing.Name+"\x00"+string(existing.Category)+"\x00"+existing.Query == key {
				return
			}
		}
		routes = append(routes, route)
	}

	if containsAny(lq, []string{"prefer", "preference", "like", "habit", "my ", "me ", "user", "profile", "我", "偏好", "习惯"}) {
		add(LightMemRoute{Name: "user_preference", Query: q, Category: CategoryPreference, Budget: 3, Reason: "query may depend on user preferences"})
		add(LightMemRoute{Name: "user_fact", Query: q, Category: CategoryUserFact, Budget: 3, Reason: "query may depend on user facts"})
	}
	if containsAny(lq, []string{"rule", "instruction", "always", "never", "以后", "不要", "必须", "规则", "要求"}) {
		add(LightMemRoute{Name: "instruction", Query: q, Category: CategoryInstruction, Budget: 3, Reason: "query may depend on standing instructions"})
	}
	if projectPath != "" || containsAny(lq, []string{"project", "repo", "code", "test", "build", "bug", "feature", "项目", "代码", "测试", "构建", "修复"}) {
		add(LightMemRoute{Name: "project_memory", Query: q, Category: CategoryProjectKnowledge, Budget: 5, ProjectScoped: projectPath != "", Reason: "query may depend on project facts"})
	}
	if containsAny(lq, []string{"requirement", "design", "task", "plan", "spec", "continue", "resume", "需求", "设计", "任务", "方案", "继续", "开工"}) {
		add(LightMemRoute{Name: "task_artifact", Query: q, Category: CategoryTaskArtifact, Budget: 4, ProjectScoped: projectPath != "", Reason: "query may depend on workflow artifacts"})
	}
	if len(routes) == 0 {
		add(LightMemRoute{Name: "general", Query: q, Budget: plan.MaxItems, ProjectScoped: projectPath != "", Reason: "default memory lane"})
	}
	plan.Routes = routes
	plan.Reason = "deterministic intent routing"
	return plan
}

// RecallLightMem retrieves through the planned online controller and returns a
// compact, semantically reranked result set.
func (s *Store) RecallLightMem(query string, category Category, projectPath string, ownerID ...string) []Entry {
	entries, _ := s.RecallLightMemDebug(query, category, projectPath, ownerID...)
	return entries
}

// RecallLightMemDebug returns both results and the deterministic recall plan.
func (s *Store) RecallLightMemDebug(query string, category Category, projectPath string, ownerID ...string) ([]Entry, LightMemRecallPlan) {
	plan := BuildLightMemRecallPlan(query, category, projectPath)
	plan.RouteCandidateCounts = map[string]int{}
	if s == nil || !plan.NeedMemory || len(plan.Routes) == 0 {
		return nil, plan
	}

	type routedCandidate struct {
		entry Entry
		route LightMemRoute
		rank  int
	}
	var pool []routedCandidate
	seenRouteEntry := map[string]struct{}{}
	for _, route := range plan.Routes {
		var recalled []Entry
		if route.ProjectScoped && projectPath != "" {
			recalled = s.RecallDynamicStrict(route.Query, route.Category, projectPath, ownerID...)
		} else {
			recalled = s.RecallDynamic(route.Query, route.Category, projectPath, ownerID...)
		}
		if route.Budget > 0 && len(recalled) > route.Budget*2 {
			recalled = recalled[:route.Budget*2]
		}
		plan.RouteCandidateCounts[route.Name] = len(recalled)
		for i, entry := range recalled {
			key := route.Name + "\x00" + entry.ID
			if _, ok := seenRouteEntry[key]; ok {
				continue
			}
			seenRouteEntry[key] = struct{}{}
			pool = append(pool, routedCandidate{entry: entry, route: route, rank: i})
		}
	}
	if len(pool) == 0 {
		return nil, plan
	}

	type scored struct {
		entry Entry
		score int
		rank  int
	}
	best := map[string]scored{}
	for i, c := range pool {
		score := lightMemSemanticScore(plan.Query, c.route.Query, c.entry)
		score += lightMemRoutePriority(c.route)
		score += maxInt(0, (c.route.Budget*2)-c.rank)
		if c.entry.Pinned {
			score += 8
		}
		if c.entry.AccessCount > 0 {
			score += minInt(c.entry.AccessCount, 6)
		}
		prev, ok := best[c.entry.ID]
		if !ok || score > prev.score {
			best[c.entry.ID] = scored{entry: c.entry, score: score, rank: i}
		}
	}
	scoredEntries := make([]scored, 0, len(best))
	for _, item := range best {
		scoredEntries = append(scoredEntries, item)
	}
	sort.SliceStable(scoredEntries, func(i, j int) bool {
		if scoredEntries[i].score != scoredEntries[j].score {
			return scoredEntries[i].score > scoredEntries[j].score
		}
		return scoredEntries[i].rank < scoredEntries[j].rank
	})

	var out []Entry
	tokensLeft := plan.TokenBudget
	for _, item := range scoredEntries {
		if len(out) >= plan.MaxItems {
			break
		}
		tokens := EstimateTextTokens(firstNonEmptyString(item.entry.CompactForm, item.entry.Content))
		if tokens > tokensLeft {
			continue
		}
		tokensLeft -= tokens
		out = append(out, item.entry)
		plan.ResultEntryIDs = append(plan.ResultEntryIDs, item.entry.ID)
	}
	return out, plan
}

func lightMemLooksLikeNoMemory(query string) bool {
	lq := strings.ToLower(strings.TrimSpace(query))
	switch lq {
	case "hi", "hello", "hey", "ok", "okay", "thanks", "thank you", "你好", "您好", "谢谢", "好的", "嗯", "好":
		return true
	}
	return len([]rune(lq)) <= 1
}

func lightMemSemanticScore(originalQuery, routeQuery string, entry Entry) int {
	expanded := ExpandQuery(originalQuery + " " + routeQuery)
	if len(expanded.QueryTokens) == 0 && len(expanded.Entities) == 0 {
		return 0
	}
	haystack := strings.ToLower(entry.Title + " " + entry.Content + " " + strings.Join(entry.Tags, " ") + " " + strings.Join(entry.Entities, " ") + " " + string(entry.Category))
	score := 0
	for _, token := range expanded.QueryTokens {
		if token != "" && strings.Contains(haystack, strings.ToLower(token)) {
			score += 3
		}
	}
	for _, entity := range expanded.Entities {
		if entity != "" && strings.Contains(haystack, strings.ToLower(entity)) {
			score += 6
		}
	}
	return score
}

func lightMemRoutePriority(route LightMemRoute) int {
	switch route.Name {
	case "task_artifact":
		return 12
	case "project_memory":
		return 10
	case "user_preference", "user_fact", "instruction":
		return 9
	default:
		return 4
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
