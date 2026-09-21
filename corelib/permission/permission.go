// Package permission is the rule-engine core of the unified permission
// policy layer (docs/design/tool-routing-improvement-plan-zh.md, decisions
// R3/R5). It parses and merges rules from managed settings, the user
// config, the project directory, and a Claude-style compat fallback into
// one immutable Snapshot, and evaluates deny > ask > allow decisions.
// Per R3 this package deliberately does NOT evaluate gates centrally: the
// six existing gates remain independent consumers of the same snapshot.
// Per R5, parameter-conditioned policies are expressed as args predicate
// rules (When) instead of being flattened into a static kind. A rule source
// that is unreadable or malformed fails closed: Load returns a
// DenyAllSnapshot in which every Decide reports deny.
package permission

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Effect is the outcome a rule assigns to a matching tool call.
type Effect string

const (
	EffectDeny  Effect = "deny"
	EffectAsk   Effect = "ask"
	EffectAllow Effect = "allow"
)

// AccessKind classifies what a tool call does to the local environment.
type AccessKind string

const (
	// KindRead covers non-mutating local reads (file reads, search, stats).
	KindRead AccessKind = "read"
	// KindMutate covers state-changing local tools and files (writes,
	// edits, memory, downloads).
	KindMutate AccessKind = "mutate"
	// KindExecute covers shell and process execution (bash, ssh, browser).
	KindExecute AccessKind = "execute"
)

// ArgsPredicate matches when args[Field] equals Equals, is contained in In,
// starts with Prefix when Prefix is non-empty, or case-insensitively equals
// (after trimming whitespace) an entry of InFold (OR semantics across the
// matchers). Prefix exists for path-scoped policies such as out-of-project
// file access; InFold exists for enumerated action vocabularies where the
// legacy gate compared case-insensitively (e.g. database write actions). An
// empty Field is invalid and never matches.
//
// Prefix is a RAW strings.HasPrefix: it carries no path-segment boundary
// semantics. A rule with Prefix "/safe" also matches "/safe-evil/x" — the
// sibling name merely shares the character prefix. Authors scoping paths
// must include the trailing separator in the rule ("/safe/") so the boundary
// is expressed in the rule text itself; the engine deliberately does not
// rewrite prefixes, and validation does not reject boundary-less ones
// (that would silently change existing rule behavior).
type ArgsPredicate struct {
	Field  string   `json:"field"`
	Equals string   `json:"equals"`
	In     []string `json:"in"`
	Prefix string   `json:"prefix,omitempty"`
	InFold []string `json:"in_fold,omitempty"`
}

// matches reports whether the predicate holds against args. A nil args map
// never matches (there is no field to compare). Only scalar values
// (string, number, bool, json.Number) participate in matching: rendering a
// slice or map through %v would produce artifacts like "[a b]" that could
// spuriously satisfy a Prefix matcher, so non-scalar values never match.
func (p ArgsPredicate) matches(args map[string]interface{}) bool {
	if p.Field == "" || args == nil {
		return false
	}
	raw, ok := args[p.Field]
	if !ok {
		return false
	}
	var val string
	switch v := raw.(type) {
	case string:
		val = v
	case bool:
		val = strconv.FormatBool(v)
	case json.Number:
		val = v.String()
	case int:
		val = strconv.Itoa(v)
	case int64:
		val = strconv.FormatInt(v, 10)
	case float64:
		val = strconv.FormatFloat(v, 'g', -1, 64)
	default:
		return false
	}
	if p.Equals != "" && val == p.Equals {
		return true
	}
	for _, v := range p.In {
		if val == v {
			return true
		}
	}
	if p.Prefix != "" && strings.HasPrefix(val, p.Prefix) {
		return true
	}
	for _, v := range p.InFold {
		if strings.EqualFold(strings.TrimSpace(val), strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}

// Rule is one permission rule. Tool is an exact tool name or "*" for a
// catch-all. Subject optionally scopes the rule to one principal (e.g. an
// expert id or workflow phase owner): "" or "*" matches any subject, a
// concrete value matches only DecideFor calls carrying it. Source is
// provenance, set by the loader (e.g. "managed", "user-config", "project",
// "claude-fallback").
type Rule struct {
	Tool    string         `json:"tool"`
	Effect  Effect         `json:"effect"`
	Kind    *AccessKind    `json:"kind,omitempty"`
	When    *ArgsPredicate `json:"when,omitempty"`
	Subject string         `json:"subject,omitempty"`
	Reason  string         `json:"reason,omitempty"`
	Source  string         `json:"source,omitempty"`
}

// Decision is the outcome of evaluating one tool call against a Snapshot.
// Default is true when no rule matched; the engine only reports the default,
// it does not invent an allow — callers decide what a default means.
type Decision struct {
	Effect  Effect
	Rule    *Rule
	Default bool
}

// Snapshot is an immutable merged rule set plus the tool-kind table.
// Per R3 it is published to independent gate consumers; it is not a central
// evaluator.
type Snapshot struct {
	rules []Rule
	kinds map[string]AccessKind
}

// HasArgsRules reports whether any rule in the snapshot carries a When
// args predicate. Callers that evaluate per-call args can skip parsing the
// args payload entirely when this returns false: no rule could observe the
// parsed values.
func (s *Snapshot) HasArgsRules() bool {
	for i := range s.rules {
		if s.rules[i].When != nil {
			return true
		}
	}
	return false
}

// Decide evaluates one tool call. kind is the caller-supplied access kind
// ("" means unknown/unclassified); BuiltinKind is the fallback for callers
// that do not classify their own tools. Evaluation is:
//
//   - deny > ask > allow, regardless of declaration order or source order
//     (this priority is global across sources: a user deny beats a managed
//     allow, matching grok-build semantics);
//   - within one effect, an exact tool name beats "*", and a rule with a
//     When predicate beats one without.
//
// No match yields Decision{Default: true}.
func (s *Snapshot) Decide(tool string, kind AccessKind, args map[string]interface{}) Decision {
	return s.DecideFor("", tool, kind, args)
}

// DecideFor evaluates one tool call for one subject. Rules with a concrete
// Subject match only that subject; rules with "" or "*" Subject match any
// subject, so a subject-less Decide sees only the global rule half. Within
// one effect, specificity is: exact tool name > subject-scoped > When
// predicate (each beating the absence of that narrowing).
func (s *Snapshot) DecideFor(subject, tool string, kind AccessKind, args map[string]interface{}) Decision {
	best := -1
	for i := range s.rules {
		if !ruleMatches(&s.rules[i], subject, tool, kind, args) {
			continue
		}
		if best < 0 || ruleBeats(&s.rules[i], &s.rules[best]) {
			best = i
		}
	}
	if best < 0 {
		return Decision{Default: true}
	}
	// Return a copy: callers must not be able to mutate the snapshot's rule
	// through the returned pointer. The copy is deep for the pointer fields
	// (Kind, When, and When's slices): mutating decision.Rule.Kind or
	// decision.Rule.When.In/InFold must not leak back into the snapshot.
	rule := s.rules[best]
	if rule.Kind != nil {
		kind := *rule.Kind
		rule.Kind = &kind
	}
	if rule.When != nil {
		when := *rule.When
		when.In = append([]string(nil), when.In...)
		when.InFold = append([]string(nil), when.InFold...)
		rule.When = &when
	}
	return Decision{Effect: rule.Effect, Rule: &rule}
}

// ruleBeats reports whether a is strictly more specific than b: higher
// effect priority first, then exact tool over "*", then subject-scoped over
// global, then a When predicate over none. Ties keep the earlier rule (source
// order, then declaration order within a source).
func ruleBeats(a, b *Rule) bool {
	if ra, rb := effectRank(a.Effect), effectRank(b.Effect); ra != rb {
		return ra > rb
	}
	if sa, sb := a.Tool != "*", b.Tool != "*"; sa != sb {
		return sa
	}
	if sa, sb := ruleHasSubject(a), ruleHasSubject(b); sa != sb {
		return sa
	}
	return a.When != nil && b.When == nil
}

func ruleHasSubject(r *Rule) bool {
	return r.Subject != "" && r.Subject != "*"
}

func effectRank(e Effect) int {
	switch e {
	case EffectDeny:
		return 3
	case EffectAsk:
		return 2
	case EffectAllow:
		return 1
	}
	return 0
}

func ruleMatches(r *Rule, subject, tool string, kind AccessKind, args map[string]interface{}) bool {
	if r.Tool != "*" && r.Tool != tool {
		return false
	}
	if ruleHasSubject(r) && r.Subject != subject {
		return false
	}
	if r.Kind != nil && *r.Kind != kind {
		return false
	}
	if r.When != nil && !r.When.matches(args) {
		return false
	}
	return true
}

// sourceAdditional labels rules appended via WithAdditionalRules when the
// caller did not set their own provenance.
const sourceAdditional = "additional"

// WithAdditionalRules returns a NEW Snapshot with rules appended after the
// loaded rule set; the receiver is left unchanged (snapshots stay immutable
// after Load). Appended rules participate in Decide/DecideFor at the lowest
// tie-break position: within one effect/specificity level they sort after
// every rule Load produced, so a loaded rule always beats an identical
// appended one.
//
// Validation is deliberately minimal and fail-safe: each rule must have a
// non-empty Tool, a valid Effect, a valid Kind (when set), and a non-dead
// When predicate (when set): Field non-empty and at least one matcher
// populated. If ANY rule in the batch is invalid the WHOLE batch
// is dropped and the receiver is returned unchanged — an appended batch can
// never corrupt or partially apply. Rules keep their own Source when set;
// otherwise Source becomes "additional". Callers wanting Load-grade fail-closed
// behavior must validate before calling.
func (s *Snapshot) WithAdditionalRules(rules []Rule) *Snapshot {
	if s == nil {
		return nil
	}
	if len(rules) == 0 {
		return s
	}
	cleaned := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.Tool == "" {
			return s
		}
		switch r.Effect {
		case EffectDeny, EffectAsk, EffectAllow:
		default:
			return s
		}
		if r.Kind != nil {
			switch *r.Kind {
			case KindRead, KindMutate, KindExecute:
			default:
				return s
			}
		}
		if r.When != nil {
			if r.When.Field == "" {
				return s
			}
			if whenPredicateDead(r.When) {
				return s
			}
		}
		if r.Source == "" {
			r.Source = sourceAdditional
		}
		cleaned = append(cleaned, r)
	}
	out := &Snapshot{
		rules: make([]Rule, 0, len(s.rules)+len(cleaned)),
		kinds: s.kinds,
	}
	out.rules = append(out.rules, s.rules...)
	out.rules = append(out.rules, cleaned...)
	return out
}

// DenyAllSnapshot returns a fail-closed fallback snapshot in which every
// Decide reports deny with a rule carrying the given Reason.
func DenyAllSnapshot(reason string) *Snapshot {
	return &Snapshot{
		rules: []Rule{{Tool: "*", Effect: EffectDeny, Reason: reason, Source: sourceFailClosed}},
		kinds: builtinKindsSnapshot(),
	}
}

var builtinKinds = map[string]AccessKind{
	// read-only
	"web_search":             KindRead,
	"web_fetch":              KindRead,
	"read_file":              KindRead,
	"knowledge_search":       KindRead,
	"knowledge_list_sources": KindRead,
	"knowledge_stats":        KindRead,
	"current_datetime":       KindRead,
	"glob":                   KindRead,
	"grep":                   KindRead,
	"ask_user":               KindRead,
	"list_mcp_tools":         KindRead,
	"database_query":         KindRead,
	"read_tool_result":       KindRead,
	"knowledge_context_pack": KindRead,
	// tts renders speech locally without filesystem or process effects;
	// the three-state model has no audio-output kind, so it groups with Read.
	"tts": KindRead,
	// mutate
	"write_file":          KindMutate,
	"edit_file":           KindMutate,
	"memory":              KindMutate,
	"database":            KindMutate,
	"knowledge_save_text": KindMutate,
	"download_file":       KindMutate,
	"manage_skill":        KindMutate,
	"set_nickname":        KindMutate,
	// execute
	"bash":          KindExecute,
	"ssh":           KindExecute,
	"browser":       KindExecute,
	"screenshot":    KindExecute,
	"record_audio":  KindExecute,
	"craft_tool":    KindExecute,
	"task":          KindExecute,
	"delegate_task": KindExecute,
}

func builtinKindsSnapshot() map[string]AccessKind {
	m := make(map[string]AccessKind, len(builtinKinds))
	for k, v := range builtinKinds {
		m[k] = v
	}
	return m
}

// BuiltinKind classifies a well-known MaClaw tool name into an AccessKind,
// returning "" for unknown tools. Callers may pass a kind explicitly; this
// table is the fallback.
func BuiltinKind(tool string) AccessKind {
	if k, ok := builtinKinds[tool]; ok {
		return k
	}
	return ""
}

// Source provenance labels set by Load.
const (
	sourceManaged      = "managed"
	sourceUserConfig   = "user-config"
	sourceProject      = "project"
	sourceClaudeCompat = "claude-fallback"
	sourceFailClosed   = "fail-closed"
)

// Options controls Load.
type Options struct {
	// ManagedRules are the highest-priority rules (managed settings).
	ManagedRules []Rule
	// ManagedYoloPin strips catch-all allow rules (Tool=="*" &&
	// Effect==allow && When==nil) from every non-managed source, so a
	// managed yolo pin cannot be undone by lower-priority config.
	ManagedYoloPin bool
	// UserConfigPath is a JSON file with a top-level "permission" key:
	// {"permission": {"rules": [...]}}.
	UserConfigPath string
	// ProjectDir is scanned for .maclaw/permission.json and
	// .claude/settings.json when TrustProject is true.
	ProjectDir   string
	TrustProject bool
}

// Load merges rules from all configured sources into one Snapshot. Source
// order (highest to lowest priority, also the tie-break order within one
// effect/specificity level): ManagedRules → UserConfigPath →
// ProjectDir/.maclaw/permission.json → ProjectDir/.claude/settings.json
// (Claude-style compat: plain names under permissions.deny/ask/allow, no
// predicates). When TrustProject is false the project-level sources are
// skipped entirely — an untrusted project contributes no rules.
//
// Fail-closed: a malformed or unreadable rule source makes Load return an
// error AND a DenyAllSnapshot whose rule Reason is
// "rule source unreadable: <path-or-source>". A missing file is not an
// error — the source is simply absent.
func Load(opts Options) (*Snapshot, error) {
	managed, err := validateRules(sourceManaged, opts.ManagedRules)
	if err != nil {
		return failClosed(sourceManaged, err)
	}
	all := make([]Rule, 0, len(managed))
	all = append(all, managed...)

	if opts.UserConfigPath != "" {
		rules, err := loadUserConfigRules(opts.UserConfigPath)
		if err != nil {
			return failClosed(opts.UserConfigPath, err)
		}
		rules, err = validateRules(sourceUserConfig, rules)
		if err != nil {
			return failClosed(opts.UserConfigPath, err)
		}
		all = append(all, rules...)
	}

	if opts.ProjectDir != "" && opts.TrustProject {
		rules, err := loadProjectRules(filepath.Join(opts.ProjectDir, ".maclaw", "permission.json"))
		if err != nil {
			return failClosed(filepath.Join(opts.ProjectDir, ".maclaw", "permission.json"), err)
		}
		rules, err = validateRules(sourceProject, rules)
		if err != nil {
			return failClosed(filepath.Join(opts.ProjectDir, ".maclaw", "permission.json"), err)
		}
		all = append(all, rules...)

		claude, err := loadClaudeRules(filepath.Join(opts.ProjectDir, ".claude", "settings.json"))
		if err != nil {
			return failClosed(filepath.Join(opts.ProjectDir, ".claude", "settings.json"), err)
		}
		claude, err = validateRules(sourceClaudeCompat, claude)
		if err != nil {
			return failClosed(filepath.Join(opts.ProjectDir, ".claude", "settings.json"), err)
		}
		all = append(all, claude...)
	}

	if opts.ManagedYoloPin {
		kept := all[:len(managed)]
		for _, r := range all[len(managed):] {
			// Only GLOBAL catch-all allows are stripped; subject-scoped
			// allows (e.g. one expert's allowlist) survive the pin.
			if r.Tool == "*" && r.Effect == EffectAllow && r.When == nil && !ruleHasSubject(&r) {
				continue
			}
			kept = append(kept, r)
		}
		all = kept
	}

	return &Snapshot{rules: all, kinds: builtinKindsSnapshot()}, nil
}

func failClosed(source string, detail error) (*Snapshot, error) {
	reason := "rule source unreadable: " + source
	return DenyAllSnapshot(reason), fmt.Errorf("%s: %w", reason, detail)
}

// validateRules normalizes provenance and rejects structurally invalid
// rules; an invalid rule fails the whole source closed.
func validateRules(source string, rules []Rule) ([]Rule, error) {
	out := make([]Rule, 0, len(rules))
	for i, r := range rules {
		if r.Tool == "" {
			return nil, fmt.Errorf("rule %d: empty tool", i)
		}
		switch r.Effect {
		case EffectDeny, EffectAsk, EffectAllow:
		default:
			return nil, fmt.Errorf("rule %d: invalid effect %q", i, r.Effect)
		}
		if r.Kind != nil {
			switch *r.Kind {
			case KindRead, KindMutate, KindExecute:
			default:
				return nil, fmt.Errorf("rule %d: invalid kind %q", i, *r.Kind)
			}
		}
		if r.When != nil {
			if r.When.Field == "" {
				return nil, fmt.Errorf("rule %d: when predicate has empty field", i)
			}
			if whenPredicateDead(r.When) {
				return nil, fmt.Errorf("rule %d: when predicate has no matchers (permanently dead rule)", i)
			}
		}
		r.Source = source
		out = append(out, r)
	}
	return out, nil
}

// whenPredicateDead reports whether a When predicate can never match any
// value: all four matchers are empty, so every comparison is vacuous.
func whenPredicateDead(w *ArgsPredicate) bool {
	return w.Equals == "" && len(w.In) == 0 && w.Prefix == "" && len(w.InFold) == 0
}

func readSource(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // absent source is not an error
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil // empty source is treated as absent
	}
	return data, nil
}

func loadUserConfigRules(path string) ([]Rule, error) {
	data, err := readSource(path)
	if err != nil || data == nil {
		return nil, err
	}
	var doc struct {
		Permission struct {
			Rules []Rule `json:"rules"`
		} `json:"permission"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc.Permission.Rules, nil
}

func loadProjectRules(path string) ([]Rule, error) {
	data, err := readSource(path)
	if err != nil || data == nil {
		return nil, err
	}
	var doc struct {
		Rules []Rule `json:"rules"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc.Rules, nil
}

// loadClaudeRules maps Claude-style {"permissions": {"deny": [...], "ask":
// [...], "allow": [...]}} name lists into rules with no predicates.
func loadClaudeRules(path string) ([]Rule, error) {
	data, err := readSource(path)
	if err != nil || data == nil {
		return nil, err
	}
	var doc struct {
		Permissions struct {
			Deny  []string `json:"deny"`
			Ask   []string `json:"ask"`
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var out []Rule
	add := func(names []string, eff Effect) {
		for _, n := range names {
			if n == "" {
				continue
			}
			out = append(out, Rule{Tool: n, Effect: eff})
		}
	}
	add(doc.Permissions.Deny, EffectDeny)
	add(doc.Permissions.Ask, EffectAsk)
	add(doc.Permissions.Allow, EffectAllow)
	return out, nil
}
