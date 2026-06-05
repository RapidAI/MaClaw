package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

// PolicyEngine evaluates tool invocations against a set of ordered policy rules.
type PolicyEngine struct {
	mu            sync.RWMutex
	rules         []security.PolicyRule
	reCache       map[string]*regexp.Regexp // compiled regex cache
	mode          string
	developerMode bool // true when mode == "developer"
}

// NewPolicyEngine creates a PolicyEngine initialised with the default policy rules.
func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{
		mode:    "standard",
		rules:   DefaultPolicyRules(),
		reCache: make(map[string]*regexp.Regexp),
	}
}

// NewPolicyEngineWithMode creates a PolicyEngine using rules for the given mode.
// Supported modes: "none", "developer", "relaxed"/"permissive", "standard" (default), "strict".
func NewPolicyEngineWithMode(mode string) *PolicyEngine {
	mode = normalizePolicyEngineMode(mode)
	return &PolicyEngine{
		rules:         PolicyRulesForMode(mode),
		reCache:       make(map[string]*regexp.Regexp),
		mode:          mode,
		developerMode: mode == "developer",
	}
}

// SetMode replaces the current rule set with rules for the given mode.
func (e *PolicyEngine) SetMode(mode string) {
	mode = normalizePolicyEngineMode(mode)
	rules := PolicyRulesForMode(mode)
	e.mu.Lock()
	e.rules = rules
	e.reCache = make(map[string]*regexp.Regexp)
	e.mode = mode
	e.developerMode = mode == "developer"
	e.mu.Unlock()
}

func normalizePolicyEngineMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "none", "developer", "relaxed", "standard", "strict":
		return strings.ToLower(strings.TrimSpace(mode))
	case "permissive":
		return "relaxed"
	default:
		return "standard"
	}
}

// IsDeveloperMode returns true when the engine is in "developer" mode.
// Developer mode disables all security guardrails - intended for security
// researchers who need to observe raw tool behaviour without interception.
func (e *PolicyEngine) IsDeveloperMode() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.developerMode
}

func (e *PolicyEngine) Mode() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if strings.TrimSpace(e.mode) == "" {
		return "standard"
	}
	return e.mode
}

// Evaluate determines the PolicyAction for a tool invocation by walking the
// rules in priority order and returning the action of the first matching rule.
// If no rule matches, the default action is "ask".
func (e *PolicyEngine) Evaluate(toolName string, args map[string]interface{}, risk security.RiskLevel) security.PolicyAction {
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

	// No rule matched - default to asking the user.
	return security.PolicyAsk
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

	var rules []security.PolicyRule
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
func (e *PolicyEngine) Rules() []security.PolicyRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]security.PolicyRule, len(e.rules))
	copy(out, e.rules)
	return out
}

// DefaultPolicyRules returns the built-in policy rule set (standard mode).
// Standard: critical/high ask, medium audit, low allow. Dangerous command
// patterns are recorded through risk/audit paths instead of being denied here.
func DefaultPolicyRules() []security.PolicyRule {
	return PolicyRulesForMode("standard")
}

// PolicyRulesForMode returns the policy rules for the given security mode.
//
// Supported modes:
//
//	none:      risk guardrail decisions disabled; independent security controls still apply.
//	developer: all operations allowed; callers may still audit observations.
//	relaxed:   low/medium/high/critical allow so skills can install and run.
//	standard:  low allow, medium audit, high/critical ask when a UI is available.
//	strict:    low allow, medium/high ask, critical deny; dangerous critical args deny.
func PolicyRulesForMode(mode string) []security.PolicyRule {
	// Strict mode is the only built-in mode that hard-blocks dangerous command
	// patterns. Softer modes rely on risk assessment, confirmation, and audit so
	// skills remain usable when installers or scripts contain powerful commands.
	// NOTE: \bsudo\b uses word boundaries to avoid matching "pseudo", "sudoku", etc.
	denyDangerous := security.PolicyRule{
		Name:        "deny-dangerous-keywords",
		Priority:    10,
		ToolPattern: "*",
		ArgsPattern: "(?i)(rm\\s+-rf|DROP\\s+TABLE|\\bsudo\\b)",
		RiskLevels:  []security.RiskLevel{security.RiskCritical},
		Action:      security.PolicyDeny,
	}

	var rules []security.PolicyRule

	switch mode {
	case "none":
		rules = []security.PolicyRule{
			{Name: "guardrails-off-allow-critical", Priority: 10, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskCritical}, Action: security.PolicyAllow},
			{Name: "guardrails-off-allow-high", Priority: 20, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskHigh}, Action: security.PolicyAllow},
			{Name: "guardrails-off-allow-medium", Priority: 30, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskMedium}, Action: security.PolicyAllow},
			{Name: "guardrails-off-allow-low", Priority: 100, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskLow}, Action: security.PolicyAllow},
		}
	case "developer":
		rules = []security.PolicyRule{
			{Name: "allow-critical", Priority: 10, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskCritical}, Action: security.PolicyAllow},
			{Name: "allow-high", Priority: 20, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskHigh}, Action: security.PolicyAllow},
			{Name: "allow-medium", Priority: 30, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskMedium}, Action: security.PolicyAllow},
			{Name: "allow-low", Priority: 100, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskLow}, Action: security.PolicyAllow},
		}
	case "relaxed":
		rules = []security.PolicyRule{
			{Name: "allow-critical", Priority: 20, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskCritical}, Action: security.PolicyAllow},
			{Name: "allow-high", Priority: 30, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskHigh}, Action: security.PolicyAllow},
			{Name: "allow-medium", Priority: 40, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskMedium}, Action: security.PolicyAllow},
			{Name: "allow-low", Priority: 100, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskLow}, Action: security.PolicyAllow},
		}
	case "strict":
		rules = []security.PolicyRule{
			denyDangerous,
			{Name: "deny-critical", Priority: 20, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskCritical}, Action: security.PolicyDeny},
			{Name: "ask-high", Priority: 30, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskHigh}, Action: security.PolicyAsk},
			{Name: "ask-medium", Priority: 40, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskMedium}, Action: security.PolicyAsk},
			{Name: "allow-low", Priority: 100, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskLow}, Action: security.PolicyAllow},
		}
	default: // "standard"
		rules = []security.PolicyRule{
			{Name: "ask-critical", Priority: 20, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskCritical}, Action: security.PolicyAsk},
			{Name: "ask-high", Priority: 30, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskHigh}, Action: security.PolicyAsk},
			{Name: "audit-medium", Priority: 40, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskMedium}, Action: security.PolicyAudit},
			{Name: "allow-low", Priority: 100, ToolPattern: "*", RiskLevels: []security.RiskLevel{security.RiskLow}, Action: security.PolicyAllow},
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
func matchesRule(rule security.PolicyRule, toolName, argStr string, risk security.RiskLevel) bool {
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
func (e *PolicyEngine) matchesRuleLocked(rule security.PolicyRule, toolName, argStr string, risk security.RiskLevel) bool {
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
			// Cache miss under RLock - skip caching to avoid data race.
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
func sortRulesByPriority(rules []security.PolicyRule) {
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})
}

// matchesRuleSnapshot is like matchesRule but uses a pre-captured regex cache
// snapshot. This avoids holding the lock during evaluation.
func (e *PolicyEngine) matchesRuleSnapshot(rule security.PolicyRule, toolName, argStr string, risk security.RiskLevel, cache map[string]*regexp.Regexp) bool {
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
