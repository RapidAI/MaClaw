package security

import (
	"encoding/json"
	"log"
	"strings"
)

// Checker evaluates security policies against incoming requests.
type Checker struct {
	repo *Repo
}

// NewChecker creates a Checker.
func NewChecker(repo *Repo) *Checker {
	return &Checker{repo: repo}
}

// Check evaluates all active policies against the input.
// Returns allowed=true if no policy blocks the request.
func (c *Checker) Check(tenantID string, input CheckInput) CheckResult {
	policies, err := c.repo.ListActivePolicies(tenantID)
	if err != nil {
		log.Printf("[security] failed to load policies: %v", err)
		return CheckResult{Allowed: true} // fail-open on DB error
	}

	for _, p := range policies {
		if !c.scopeMatches(p.Scope, input) {
			continue
		}

		switch p.PolicyType {
		case PolicyTypeKeywordBlock:
			if result, blocked := c.checkKeywordBlock(tenantID, p, input); blocked {
				return result
			}
		case PolicyTypeModelRestrict:
			if result, blocked := c.checkModelRestrict(tenantID, p, input); blocked {
				return result
			}
		}
	}

	return CheckResult{Allowed: true}
}

func (c *Checker) scopeMatches(scope string, input CheckInput) bool {
	if scope == "all" || scope == "" {
		return true
	}
	if strings.HasPrefix(scope, "role:") {
		return strings.TrimPrefix(scope, "role:") == input.RoleCode
	}
	if strings.HasPrefix(scope, "colleague:") {
		return strings.TrimPrefix(scope, "colleague:") == input.ColleagueID
	}
	return false
}

func (c *Checker) checkKeywordBlock(tenantID string, p Policy, input CheckInput) (CheckResult, bool) {
	var rules KeywordBlockRules
	if err := json.Unmarshal([]byte(p.Rules), &rules); err != nil {
		return CheckResult{}, false
	}

	contentLower := strings.ToLower(input.Content)
	for _, kw := range rules.Keywords {
		if strings.Contains(contentLower, strings.ToLower(kw)) {
			action := rules.Action
			if action == "" {
				action = "block"
			}
			// Truncate detail to avoid bloating the DB
			detail := input.Content
			if len([]rune(detail)) > 200 {
				detail = string([]rune(detail)[:200]) + "..."
			}
			if action == "block" {
				_ = c.repo.RecordHit(tenantID, p.ID, p.Name, input.ColleagueID, "blocked", detail)
				return CheckResult{
					Allowed:    false,
					PolicyID:   p.ID,
					PolicyName: p.Name,
					Reason:     "blocked",
				}, true
			}
			// warn/log: record hit but allow; only record once per check
			_ = c.repo.RecordHit(tenantID, p.ID, p.Name, input.ColleagueID, action, detail)
			return CheckResult{Allowed: true}, false
		}
	}
	return CheckResult{}, false
}

func (c *Checker) checkModelRestrict(tenantID string, p Policy, input CheckInput) (CheckResult, bool) {
	if input.Model == "" {
		return CheckResult{}, false
	}
	var rules struct {
		BlockedModels []string `json:"blocked_models"`
	}
	if err := json.Unmarshal([]byte(p.Rules), &rules); err != nil {
		return CheckResult{}, false
	}
	modelLower := strings.ToLower(input.Model)
	for _, m := range rules.BlockedModels {
		if strings.ToLower(m) == modelLower {
			_ = c.repo.RecordHit(tenantID, p.ID, p.Name, input.ColleagueID, "blocked", input.Model)
			return CheckResult{
				Allowed:    false,
				PolicyID:   p.ID,
				PolicyName: p.Name,
				Reason:     "model_restricted",
			}, true
		}
	}
	return CheckResult{}, false
}
