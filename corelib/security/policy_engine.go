package security

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// PolicyEngine evaluates tool invocations against a set of ordered policy rules.
type PolicyEngine struct {
	mu            sync.RWMutex
	rules         []PolicyRule
	reCache       map[string]*regexp.Regexp
	developerMode bool // true when mode == "developer"
}

// NewPolicyEngine creates a PolicyEngine with default (standard) rules.
func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{
		rules:   DefaultPolicyRules(),
		reCache: make(map[string]*regexp.Regexp),
	}
}

// NewPolicyEngineWithMode creates a PolicyEngine using rules for the given mode.
func NewPolicyEngineWithMode(mode string) *PolicyEngine {
	return &PolicyEngine{
		rules:         PolicyRulesForMode(mode),
		reCache:       make(map[string]*regexp.Regexp),
		developerMode: mode == "developer",
	}
}

// SetMode replaces the current rule set with rules for the given mode.
func (e *PolicyEngine) SetMode(mode string) {
	rules := PolicyRulesForMode(mode)
	e.mu.Lock()
	e.rules = rules
	e.reCache = make(map[string]*regexp.Regexp)
	e.developerMode = mode == "developer"
	e.mu.Unlock()
}

// Evaluate determines the PolicyAction for a tool invocation.
func (e *PolicyEngine) Evaluate(toolName string, args map[string]interface{}, risk RiskLevel) PolicyAction {
	e.mu.Lock()
	if e.reCache == nil {
		e.reCache = make(map[string]*regexp.Regexp)
	}
	for _, rule := range e.rules {
		if rule.ArgsPattern != "" {
			if _, ok := e.reCache[rule.ArgsPattern]; !ok {
				if re, err := regexp.Compile(rule.ArgsPattern); err == nil {
					e.reCache[rule.ArgsPattern] = re
				}
			}
		}
	}
	rules := e.rules
	reCache := e.reCache
	e.mu.Unlock()

	argStr := flattenArgs(args)

	for _, rule := range rules {
		if matchesRuleSnapshot(rule, toolName, argStr, risk, reCache) {
			return rule.Action
		}
	}
	return PolicyAsk
}

// LoadRules reads a JSON file containing PolicyRule array and replaces the current set.
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
	e.reCache = make(map[string]*regexp.Regexp)
	e.mu.Unlock()
	return nil
}

// Rules returns a copy of the current rule set.
func (e *PolicyEngine) Rules() []PolicyRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]PolicyRule, len(e.rules))
	copy(out, e.rules)
	return out
}

// DefaultPolicyRules returns the built-in policy rule set (standard mode).
func DefaultPolicyRules() []PolicyRule {
	return PolicyRulesForMode("standard")
}

// IsDeveloperMode returns true when the engine is in "developer" mode.
// Developer mode disables all security guardrails — intended for security
// researchers who need to observe raw tool behaviour without interception.
func (e *PolicyEngine) IsDeveloperMode() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.developerMode
}

// PolicyRulesForMode returns the policy rules for the given security mode.
//
// Supported modes:
//
//	developer: all operations allowed unconditionally (no deny, no ask, no audit).
//	           Intended for security researchers. Use with caution.
//	relaxed:   low/medium/high → allow, critical → ask (dangerous keywords → deny)
//	standard:  low → allow, medium → audit, high → ask, critical → ask (dangerous keywords → deny)
//	strict:    low → allow, medium/high → ask, critical → deny (dangerous keywords → deny)
func PolicyRulesForMode(mode string) []PolicyRule {
	// Dangerous-keyword deny rule is shared across all non-developer modes.
	// NOTE: \bsudo\b uses word boundaries to avoid matching "pseudo", "sudoku", etc.
	denyDangerous := PolicyRule{
		Name: "deny-dangerous-keywords", Priority: 10, ToolPattern: "*",
		ArgsPattern: "(?i)(rm\\s+-rf|DROP\\s+TABLE|\\bsudo\\b)",
		RiskLevels: []RiskLevel{RiskCritical}, Action: PolicyDeny,
	}

	var rules []PolicyRule
	switch mode {
	case "developer":
		// Developer mode: allow everything unconditionally.
		// No deny rules, no ask rules, no audit rules.
		// Risk assessment still runs (for logging), but policy never blocks.
		rules = []PolicyRule{
			{Name: "allow-critical", Priority: 10, ToolPattern: "*", RiskLevels: []RiskLevel{RiskCritical}, Action: PolicyAllow},
			{Name: "allow-high", Priority: 20, ToolPattern: "*", RiskLevels: []RiskLevel{RiskHigh}, Action: PolicyAllow},
			{Name: "allow-medium", Priority: 30, ToolPattern: "*", RiskLevels: []RiskLevel{RiskMedium}, Action: PolicyAllow},
			{Name: "allow-low", Priority: 100, ToolPattern: "*", RiskLevels: []RiskLevel{RiskLow}, Action: PolicyAllow},
		}
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

func sortRulesByPriority(rules []PolicyRule) {
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})
}

func matchesRuleSnapshot(rule PolicyRule, toolName, argStr string, risk RiskLevel, cache map[string]*regexp.Regexp) bool {
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
