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
	mode          string
	developerMode bool // true when mode == "developer"
}

// NewPolicyEngine creates a PolicyEngine with default (relaxed) rules.
func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{
		mode:    "relaxed",
		rules:   DefaultPolicyRules(),
		reCache: make(map[string]*regexp.Regexp),
	}
}

// NewPolicyEngineWithMode creates a PolicyEngine using rules for the given mode.
func NewPolicyEngineWithMode(mode string) *PolicyEngine {
	mode = normalizePolicyEngineMode(mode)
	return &PolicyEngine{
		rules:         PolicyRulesForMode(mode),
		reCache:       make(map[string]*regexp.Regexp),
		mode:          mode,
		developerMode: normalizePolicyEngineMode(mode) == "developer",
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
	e.developerMode = normalizePolicyEngineMode(mode) == "developer"
	e.mu.Unlock()
}

func normalizePolicyEngineMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "none", "developer", "relaxed", "standard", "strict":
		return strings.ToLower(strings.TrimSpace(mode))
	case "permissive":
		return "relaxed"
	default:
		return "relaxed"
	}
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

// DefaultPolicyRules returns the built-in policy rule set (relaxed mode).
func DefaultPolicyRules() []PolicyRule {
	return PolicyRulesForMode("relaxed")
}

// IsDeveloperMode returns true when the engine is in "developer" mode.
// Developer mode disables blocking guardrails and keeps execution observable.
func (e *PolicyEngine) IsDeveloperMode() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.developerMode
}

func (e *PolicyEngine) Mode() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if strings.TrimSpace(e.mode) == "" {
		return "relaxed"
	}
	return e.mode
}

// PolicyRulesForMode returns the policy rules for the given security mode.
//
// Supported modes:
//
//	none:      risk guardrail decisions disabled; independent security controls still apply.
//	developer: allow all operations; callers may still audit observations.
//	relaxed:   allow all risk levels so skills can install and run.
//	standard:  low allow, medium audit, high/critical ask when a UI is available.
//	strict:    low allow, medium/high ask, critical deny; dangerous critical args deny.
func PolicyRulesForMode(mode string) []PolicyRule {
	mode = normalizePolicyEngineMode(mode)
	// Strict mode is the only built-in mode that hard-blocks dangerous command
	// patterns. Softer modes rely on risk assessment, confirmation, and audit so
	// skills remain usable when installers or scripts contain powerful commands.
	//
	// The denyDangerous rule fires only after RiskAssessor has already classified
	// the call as critical. Patterns here are an additional ArgsPattern filter
	// to ensure only the truly dangerous subset of critical-risk calls is denied:
	//   - DROP TABLE: SQL destructive — dangerousKeywords still flags this as critical
	//   - sudo su / sudo -i: privilege escalation — Assess() flags \bsudo\b as critical
	//     unless a safe context guard matches; su/-i are not in safe contexts
	//
	// NOTE: "rm -rf" was intentionally removed from ArgsPattern here because
	// Assess() no longer raises rm-rf to critical (it was moved to the destructive
	// threat category scanned via ScanLiveThreatPatterns, which only escalates to
	// high, not critical). So rm-rf can only reach denyDangerous if it were
	// already critical from another rule — which won't happen. Any rm-rf /system
	// targeting is caught at high → PolicyAsk level in standard mode.
	denyDangerous := PolicyRule{
		Name:        "deny-dangerous-keywords",
		Priority:    10,
		ToolPattern: "*",
		ArgsPattern: `(?i)(DROP\s+TABLE|\bsudo\s+(su\b|-i\b))`,
		RiskLevels:  []RiskLevel{RiskCritical},
		Action:      PolicyDeny,
	}

	var rules []PolicyRule
	switch mode {
	case "none":
		rules = []PolicyRule{
			{Name: "guardrails-off-allow-critical", Priority: 10, ToolPattern: "*", RiskLevels: []RiskLevel{RiskCritical}, Action: PolicyAllow},
			{Name: "guardrails-off-allow-high", Priority: 20, ToolPattern: "*", RiskLevels: []RiskLevel{RiskHigh}, Action: PolicyAllow},
			{Name: "guardrails-off-allow-medium", Priority: 30, ToolPattern: "*", RiskLevels: []RiskLevel{RiskMedium}, Action: PolicyAllow},
			{Name: "guardrails-off-allow-low", Priority: 100, ToolPattern: "*", RiskLevels: []RiskLevel{RiskLow}, Action: PolicyAllow},
		}
	case "developer":
		rules = []PolicyRule{
			{Name: "allow-critical", Priority: 10, ToolPattern: "*", RiskLevels: []RiskLevel{RiskCritical}, Action: PolicyAllow},
			{Name: "allow-high", Priority: 20, ToolPattern: "*", RiskLevels: []RiskLevel{RiskHigh}, Action: PolicyAllow},
			{Name: "allow-medium", Priority: 30, ToolPattern: "*", RiskLevels: []RiskLevel{RiskMedium}, Action: PolicyAllow},
			{Name: "allow-low", Priority: 100, ToolPattern: "*", RiskLevels: []RiskLevel{RiskLow}, Action: PolicyAllow},
		}
	case "relaxed":
		rules = []PolicyRule{
			{Name: "allow-critical", Priority: 20, ToolPattern: "*", RiskLevels: []RiskLevel{RiskCritical}, Action: PolicyAllow},
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
