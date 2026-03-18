package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// PolicyAction represents the action a policy rule dictates for a tool invocation.
type PolicyAction string

const (
	PolicyAllow PolicyAction = "allow"
	PolicyDeny  PolicyAction = "deny"
	PolicyAsk   PolicyAction = "ask"
	PolicyAudit PolicyAction = "audit"
)

// PolicyRule defines a single policy rule for evaluating tool invocations.
type PolicyRule struct {
	Name        string       `json:"name"`
	Priority    int          `json:"priority"`     // lower number = higher priority
	ToolPattern string       `json:"tool_pattern"` // glob pattern for tool name matching
	ArgsPattern string       `json:"args_pattern"` // regex pattern for argument matching
	RiskLevels  []RiskLevel  `json:"risk_levels"`  // risk levels this rule applies to
	Action      PolicyAction `json:"action"`
}

// PolicyEngine evaluates tool invocations against a set of ordered policy rules.
type PolicyEngine struct {
	mu       sync.RWMutex
	rules    []PolicyRule
	reCache  map[string]*regexp.Regexp // compiled regex cache
}

// NewPolicyEngine creates a PolicyEngine initialised with the default policy rules.
func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{
		rules:   DefaultPolicyRules(),
		reCache: make(map[string]*regexp.Regexp),
	}
}

// NewPolicyEngineWithMode creates a PolicyEngine using rules for the given mode.
// Supported modes: "relaxed", "standard" (default), "strict".
func NewPolicyEngineWithMode(mode string) *PolicyEngine {
	return &PolicyEngine{
		rules:   PolicyRulesForMode(mode),
		reCache: make(map[string]*regexp.Regexp),
	}
}

// SetMode replaces the current rule set with rules for the given mode.
func (e *PolicyEngine) SetMode(mode string) {
	rules := PolicyRulesForMode(mode)
	e.mu.Lock()
	e.rules = rules
	e.reCache = make(map[string]*regexp.Regexp)
	e.mu.Unlock()
}

// Evaluate determines the PolicyAction for a tool invocation by walking the
// rules in priority order and returning the action of the first matching rule.
// If no rule matches, the default action is "ask".
func (e *PolicyEngine) Evaluate(toolName string, args map[string]interface{}, risk RiskLevel) PolicyAction {
	e.mu.Lock()
	if e.reCache == nil {
		e.reCache = make(map[string]*regexp.Regexp)
	}
	// Pre-compile any uncached regex patterns under write lock.
	for _, rule := range e.rules {
		if rule.ArgsPattern != "" {
			if _, ok := e.reCache[rule.ArgsPattern]; !ok {
				if re, err := regexp.Compile(rule.ArgsPattern); err == nil {
					e.reCache[rule.ArgsPattern] = re
				}
			}
		}
	}

	// Snapshot rules and cache under the same lock to avoid a race
	// between Unlock and RLock where LoadRules could replace the rules.
	rules := e.rules
	reCache := e.reCache
	e.mu.Unlock()

	argStr := flattenArgs(args)

	for _, rule := range rules {
		if e.matchesRuleSnapshot(rule, toolName, argStr, risk, reCache) {
			return rule.Action
		}
	}

	// No rule matched — default to asking the user.
	return PolicyAsk
}

// LoadRules reads a JSON file containing an array of PolicyRule and replaces
// the current rule set. Rules are sorted by Priority after loading.
func (e *PolicyEngine) LoadRules(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	var rules []PolicyRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return err
	}

	sortRulesByPriority(rules)

	e.mu.Lock()
	e.rules = rules
	e.reCache = make(map[string]*regexp.Regexp) // invalidate cache
	e.mu.Unlock()

	return nil
}

// Rules returns a copy of the current rule set (useful for inspection/testing).
func (e *PolicyEngine) Rules() []PolicyRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]PolicyRule, len(e.rules))
	copy(out, e.rules)
	return out
}

// DefaultPolicyRules returns the built-in policy rule set (standard mode).
// Standard: critical → deny (dangerous keywords) or ask, high → ask, medium → audit, low → allow.
func DefaultPolicyRules() []PolicyRule {
	return PolicyRulesForMode("standard")
}

// PolicyRulesForMode returns the policy rules for the given security mode.
//
//	relaxed:  low/medium/high → allow, critical → ask (dangerous keywords → deny)
//	standard: low → allow, medium → audit, high → ask, critical → ask (dangerous keywords → deny)
//	strict:   low → allow, medium/high → ask, critical → deny (dangerous keywords → deny)
func PolicyRulesForMode(mode string) []PolicyRule {
	// Dangerous-keyword deny rule is shared across all modes.
	denyDangerous := PolicyRule{
		Name:        "deny-dangerous-keywords",
		Priority:    10,
		ToolPattern: "*",
		ArgsPattern: "(?i)(rm\\s+-rf|DROP\\s+TABLE|sudo)",
		RiskLevels:  []RiskLevel{RiskCritical},
		Action:      PolicyDeny,
	}

	var rules []PolicyRule

	switch mode {
	case "relaxed":
		rules = []PolicyRule{
			denyDangerous,
			{Name: "ask-critical", Priority: 20, ToolPattern: "*", RiskLevels: []RiskLevel{RiskCritical}, Action: PolicyAsk},
			{Name: "allow-high", Priority: 30, ToolPattern: "*", RiskLevels: []RiskLevel{RiskHigh}, Action: PolicyAllow},
			{Name: "allow-medium", Priority: 40, ToolPattern: "*", RiskLevels: []RiskLevel{RiskMedium}, Action: PolicyAllow},
			{Name: "allow-low", Priority: 100, ToolPattern: "*", RiskLevels: []RiskLevel{RiskLow}, Action: PolicyAllow},
		}
	case "strict":
		rules = []PolicyRule{
			denyDangerous,
			{Name: "deny-critical", Priority: 20, ToolPattern: "*", RiskLevels: []RiskLevel{RiskCritical}, Action: PolicyDeny},
			{Name: "ask-high", Priority: 30, ToolPattern: "*", RiskLevels: []RiskLevel{RiskHigh}, Action: PolicyAsk},
			{Name: "ask-medium", Priority: 40, ToolPattern: "*", RiskLevels: []RiskLevel{RiskMedium}, Action: PolicyAsk},
			{Name: "allow-low", Priority: 100, ToolPattern: "*", RiskLevels: []RiskLevel{RiskLow}, Action: PolicyAllow},
		}
	default: // "standard"
		rules = []PolicyRule{
			denyDangerous,
			{Name: "ask-critical", Priority: 20, ToolPattern: "*", RiskLevels: []RiskLevel{RiskCritical}, Action: PolicyAsk},
			{Name: "ask-high", Priority: 30, ToolPattern: "*", RiskLevels: []RiskLevel{RiskHigh}, Action: PolicyAsk},
			{Name: "audit-medium", Priority: 40, ToolPattern: "*", RiskLevels: []RiskLevel{RiskMedium}, Action: PolicyAudit},
			{Name: "allow-low", Priority: 100, ToolPattern: "*", RiskLevels: []RiskLevel{RiskLow}, Action: PolicyAllow},
		}
	}

	sortRulesByPriority(rules)
	return rules
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// matchesRule checks whether a rule applies to the given tool invocation.
// This is the standalone version used by tests and other callers.
func matchesRule(rule PolicyRule, toolName, argStr string, risk RiskLevel) bool {
	// 1. Match tool name via glob pattern.
	if rule.ToolPattern != "" && rule.ToolPattern != "*" {
		matched, err := filepath.Match(rule.ToolPattern, toolName)
		if err != nil || !matched {
			return false
		}
	}

	// 2. Match flattened args via regex pattern.
	if rule.ArgsPattern != "" {
		re, err := regexp.Compile(rule.ArgsPattern)
		if err != nil {
			return false
		}
		if !re.MatchString(argStr) {
			return false
		}
	}

	// 3. Match risk level.
	if len(rule.RiskLevels) > 0 {
		found := false
		for _, rl := range rule.RiskLevels {
			if strings.EqualFold(string(rl), string(risk)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// matchesRuleLocked is like matchesRule but uses the engine's compiled regex
// cache. Must be called with e.mu held (at least RLock).
// Note: writes to reCache are safe because Evaluate ensures reCache is
// initialized under a write lock before calling this method under RLock.
// Concurrent reads of the same pattern may compile it twice, but the result
// is identical and the map write is guarded by the fact that only one
// goroutine holds the write lock at a time during cache init.
func (e *PolicyEngine) matchesRuleLocked(rule PolicyRule, toolName, argStr string, risk RiskLevel) bool {
	if rule.ToolPattern != "" && rule.ToolPattern != "*" {
		matched, err := filepath.Match(rule.ToolPattern, toolName)
		if err != nil || !matched {
			return false
		}
	}

	if rule.ArgsPattern != "" {
		re, ok := e.reCache[rule.ArgsPattern]
		if !ok {
			var err error
			re, err = regexp.Compile(rule.ArgsPattern)
			if err != nil {
				return false
			}
			// Cache miss under RLock — skip caching to avoid data race.
			// The regex will be recompiled on next call, which is acceptable
			// since rule sets are small and this path is rare.
		}
		if !re.MatchString(argStr) {
			return false
		}
	}

	if len(rule.RiskLevels) > 0 {
		found := false
		for _, rl := range rule.RiskLevels {
			if strings.EqualFold(string(rl), string(risk)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// sortRulesByPriority sorts rules in ascending priority order (lower number = higher priority).
func sortRulesByPriority(rules []PolicyRule) {
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})
}

// matchesRuleSnapshot is like matchesRule but uses a pre-captured regex cache
// snapshot. This avoids holding the lock during evaluation.
func (e *PolicyEngine) matchesRuleSnapshot(rule PolicyRule, toolName, argStr string, risk RiskLevel, cache map[string]*regexp.Regexp) bool {
	if rule.ToolPattern != "" && rule.ToolPattern != "*" {
		matched, err := filepath.Match(rule.ToolPattern, toolName)
		if err != nil || !matched {
			return false
		}
	}

	if rule.ArgsPattern != "" {
		re, ok := cache[rule.ArgsPattern]
		if !ok {
			var err error
			re, err = regexp.Compile(rule.ArgsPattern)
			if err != nil {
				return false
			}
		}
		if !re.MatchString(argStr) {
			return false
		}
	}

	if len(rule.RiskLevels) > 0 {
		found := false
		for _, rl := range rule.RiskLevels {
			if strings.EqualFold(string(rl), string(risk)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
