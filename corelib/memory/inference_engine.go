package memory

import (
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Multi-Hop Fact Reasoning Engine
//
// This module performs rule-based multi-hop reasoning over the SemanticGraph's
// typed facts (subject-predicate-object triples). It derives new implicit facts
// from existing ones using two mechanisms:
//
//   1. Transitive inference: if relation R is transitive and (A R B) + (B R C)
//      exist, derive (A R C).
//   2. Compositional inference: if rules define R1 + R2 → R3, and (A R1 B) +
//      (B R2 C) exist, derive (A R3 C).
//
// Derived facts are NOT persisted. They are computed on-demand during recall
// and injected into the system prompt as reasoning chains.
//
// Performance budget: <5ms for 500 facts, max 200 visited facts, max 20 derived.
// ---------------------------------------------------------------------------

// InferenceRule defines a single multi-hop reasoning rule.
type InferenceRule struct {
	// Name is a human-readable identifier for debugging/explainability.
	Name string

	// Type: "transitive" or "compositional"
	Type InferenceRuleType

	// For transitive rules: the relation that is transitive.
	Relation string

	// For compositional rules: the two input relations and the output relation.
	InputRelation1 string // first hop relation (subject → intermediate)
	InputRelation2 string // second hop relation (intermediate → object)
	OutputRelation string // derived relation (subject → object)

	// MaxChainLength limits transitive closure depth (default 3).
	MaxChainLength int

	// ConfidenceDecay is the multiplicative decay factor per hop (0.0-1.0).
	// Derived facts have confidence = ConfidenceDecay^hops.
	ConfidenceDecay float64
}

// InferenceRuleType distinguishes rule categories.
type InferenceRuleType string

const (
	RuleTransitive    InferenceRuleType = "transitive"
	RuleCompositional InferenceRuleType = "compositional"
)

// DerivedFact is a fact inferred from existing SemanticFacts via rules.
type DerivedFact struct {
	Subject   string
	Predicate string
	Object    string

	// SourceFacts is the chain of source facts that produced this derivation.
	SourceFacts []SemanticFact

	// RuleName identifies which rule produced this fact.
	RuleName string

	// Confidence score (decays with chain length). Range [0, 1].
	Confidence float64

	// Explanation is a human-readable reasoning chain.
	Explanation string
}

// InferenceOptions controls the inference process.
type InferenceOptions struct {
	Now         time.Time
	OwnerID     string
	ProjectPath string

	// MaxDerived limits the number of derived facts returned (default 20).
	MaxDerived int

	// MinConfidence is the minimum confidence threshold (default 0.50).
	MinConfidence float64

	// MaxVisitedFacts limits traversal scope (default 200).
	MaxVisitedFacts int
}

// InferenceEngine performs rule-based multi-hop reasoning over SemanticGraph.
type InferenceEngine struct {
	rules []InferenceRule
	graph *SemanticGraph

	// Pre-computed normalized relation names for rules (avoid repeated normalization).
	normalizedRelations map[string]string // rule.Relation/InputRelation → normalized
}

// Rules returns the registered inference rules.
func (ie *InferenceEngine) Rules() []InferenceRule { return ie.rules }

// NewInferenceEngine creates an engine with the given rules bound to a graph.
func NewInferenceEngine(graph *SemanticGraph, rules []InferenceRule) *InferenceEngine {
	if rules == nil {
		rules = BuiltinInferenceRules
	}
	// Pre-compute normalized relation names.
	normalized := make(map[string]string, len(rules)*3)
	for _, r := range rules {
		if r.Relation != "" {
			normalized[r.Relation] = normalizeRelationName(r.Relation)
		}
		if r.InputRelation1 != "" {
			normalized[r.InputRelation1] = normalizeRelationName(r.InputRelation1)
		}
		if r.InputRelation2 != "" {
			normalized[r.InputRelation2] = normalizeRelationName(r.InputRelation2)
		}
	}
	return &InferenceEngine{graph: graph, rules: rules, normalizedRelations: normalized}
}

// normRel returns the pre-computed normalized relation name, falling back to runtime normalization.
func (ie *InferenceEngine) normRel(rel string) string {
	if n, ok := ie.normalizedRelations[rel]; ok {
		return n
	}
	return normalizeRelationName(rel)
}

// Infer takes a set of query entities and returns derived facts reachable
// through the registered rules. It performs pure in-memory graph traversal
// with no LLM calls.
func (ie *InferenceEngine) Infer(queryEntities []string, opts InferenceOptions) []DerivedFact {
	if ie.graph == nil || len(queryEntities) == 0 || len(ie.rules) == 0 {
		return nil
	}
	if opts.MaxDerived <= 0 {
		opts.MaxDerived = 20
	}
	if opts.MinConfidence <= 0 {
		opts.MinConfidence = 0.50
	}
	if opts.MaxVisitedFacts <= 0 {
		opts.MaxVisitedFacts = 200
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	projectLower := semanticNormalizeProjectPath(opts.ProjectPath)

	ie.graph.mu.RLock()
	defer ie.graph.mu.RUnlock()

	// Fast path: if the graph has no facts, no inference is possible.
	// Must be checked under RLock to avoid racing with Rebuild.
	if len(ie.graph.facts) == 0 {
		return nil
	}

	// Normalize query entities and expand through aliases.
	seeds := make(map[string]struct{}, len(queryEntities))
	for _, ent := range queryEntities {
		if n := normalizeEntityName(ent); n != "" {
			seeds[n] = struct{}{}
			// Resolve aliases: if "张三" is alias_of "zhang-san", add both.
			// This mirrors SemanticGraph.SearchWithOptions' alias expansion.
			for _, alias := range ie.graph.semanticAliasesForEntityLocked(n, opts.OwnerID, projectLower, now, SemanticTemporalCurrent) {
				seeds[alias] = struct{}{}
			}
		}
	}
	if len(seeds) == 0 {
		return nil
	}

	var derived []DerivedFact
	visited := 0

	for _, rule := range ie.rules {
		if len(derived) >= opts.MaxDerived || visited >= opts.MaxVisitedFacts {
			break
		}
		switch rule.Type {
		case RuleTransitive:
			results := ie.inferTransitive(rule, seeds, opts, projectLower, now, &visited)
			derived = append(derived, results...)
		case RuleCompositional:
			results := ie.inferCompositional(rule, seeds, opts, projectLower, now, &visited)
			derived = append(derived, results...)
		}
	}

	// Deduplicate: same (Subject, Predicate, Object) keeps highest confidence.
	derived = deduplicateDerivedFacts(derived)

	// Contradiction filter: suppress derived facts that contradict existing direct facts.
	derived = ie.filterContradictions(derived, seeds, opts, projectLower, now)

	// Filter by confidence threshold.
	filtered := derived[:0]
	for _, df := range derived {
		if df.Confidence >= opts.MinConfidence {
			filtered = append(filtered, df)
		}
	}

	// Limit output.
	if len(filtered) > opts.MaxDerived {
		filtered = filtered[:opts.MaxDerived]
	}
	return filtered
}

// inferTransitive performs transitive closure for a single relation.
// Example: located_in is transitive → A in B, B in C → A in C.
func (ie *InferenceEngine) inferTransitive(
	rule InferenceRule,
	seeds map[string]struct{},
	opts InferenceOptions,
	projectLower string,
	now time.Time,
	visited *int,
) []DerivedFact {
	maxChain := rule.MaxChainLength
	if maxChain <= 0 {
		maxChain = 3
	}
	decay := rule.ConfidenceDecay
	if decay <= 0 || decay > 1 {
		decay = 0.85
	}
	relation := ie.normRel(rule.Relation)

	var results []DerivedFact

	// For each seed entity, follow the transitive relation forward.
	for seed := range seeds {
		chain := ie.followTransitiveChain(seed, relation, maxChain, opts.OwnerID, projectLower, now, visited, opts.MaxVisitedFacts)
		if len(chain) < 2 {
			continue // need at least 2 hops to derive something new
		}
		// Generate derived facts for each intermediate→final pair.
		// chain[0] is the first hop fact, chain[1] is the second, etc.
		// Derived fact: seed → relation → chain[last].Object
		for i := 1; i < len(chain); i++ {
			conf := 1.0
			for j := 0; j <= i; j++ {
				conf *= decay
			}
			if conf < opts.MinConfidence {
				break
			}
			// Defensive copy of source facts slice to prevent aliasing.
			sourceFacts := make([]SemanticFact, i+1)
			copy(sourceFacts, chain[:i+1])
			df := DerivedFact{
				Subject:     seed,
				Predicate:   relation,
				Object:      chain[i].Object,
				SourceFacts: sourceFacts,
				RuleName:    rule.Name,
				Confidence:  conf,
				Explanation: buildTransitiveExplanation(seed, relation, chain[:i+1]),
			}
			results = append(results, df)
		}
	}
	return results
}

// followTransitiveChain follows a relation transitively from a starting entity.
// Returns the chain of facts traversed (ordered by hop).
// When multiple facts match at a hop, selects the one with the highest
// provenance signal (pinned > newer > older) for deterministic results.
func (ie *InferenceEngine) followTransitiveChain(
	start, relation string,
	maxHops int,
	ownerID, projectLower string,
	now time.Time,
	visited *int,
	maxVisited int,
) []SemanticFact {
	var chain []SemanticFact
	current := start
	seen := map[string]struct{}{start: {}}

	for hop := 0; hop < maxHops; hop++ {
		if *visited >= maxVisited {
			break
		}
		// Find ALL facts where current is the subject and predicate matches,
		// then select the best one (deterministic selection).
		var candidates []SemanticFact
		for _, idx := range ie.graph.adjacency[current] {
			if *visited >= maxVisited {
				break
			}
			*visited++
			fact := ie.graph.facts[idx]
			if !ie.factAllowed(fact, ownerID, projectLower, now) {
				continue
			}
			if normalizeRelationName(fact.Predicate) != relation {
				continue
			}
			if fact.Subject != current {
				continue
			}
			if fact.Negated {
				continue
			}
			// Avoid cycles.
			if _, ok := seen[fact.Object]; ok {
				continue
			}
			candidates = append(candidates, fact)
		}
		if len(candidates) == 0 {
			break
		}
		// Deterministic selection: prefer pinned, then most recently updated.
		best := candidates[0]
		for _, c := range candidates[1:] {
			if c.Pinned && !best.Pinned {
				best = c
			} else if c.Pinned == best.Pinned && c.UpdatedAt.After(best.UpdatedAt) {
				best = c
			}
		}
		seen[best.Object] = struct{}{}
		chain = append(chain, best)
		current = best.Object
	}
	return chain
}

// inferCompositional performs two-hop compositional inference.
// Example: works_at + located_in → based_in.
func (ie *InferenceEngine) inferCompositional(
	rule InferenceRule,
	seeds map[string]struct{},
	opts InferenceOptions,
	projectLower string,
	now time.Time,
	visited *int,
) []DerivedFact {
	decay := rule.ConfidenceDecay
	if decay <= 0 || decay > 1 {
		decay = 0.75
	}
	// Early exit: if two-hop confidence is below threshold, this rule cannot produce results.
	conf := decay * decay
	if conf < opts.MinConfidence {
		return nil
	}
	rel1 := ie.normRel(rule.InputRelation1)
	rel2 := ie.normRel(rule.InputRelation2)

	var results []DerivedFact

	for seed := range seeds {
		if *visited >= opts.MaxVisitedFacts {
			break
		}
		// First hop: find facts where seed is subject with rel1.
		firstHopFacts := ie.findFacts(seed, rel1, opts.OwnerID, projectLower, now, visited, opts.MaxVisitedFacts)
		for _, fact1 := range firstHopFacts {
			if *visited >= opts.MaxVisitedFacts {
				break
			}
			intermediate := fact1.Object
			// Second hop: find facts where intermediate is subject with rel2.
			secondHopFacts := ie.findFacts(intermediate, rel2, opts.OwnerID, projectLower, now, visited, opts.MaxVisitedFacts)
			for _, fact2 := range secondHopFacts {
				df := DerivedFact{
					Subject:     seed,
					Predicate:   rule.OutputRelation,
					Object:      fact2.Object,
					SourceFacts: []SemanticFact{fact1, fact2},
					RuleName:    rule.Name,
					Confidence:  conf,
					Explanation: buildCompositionalExplanation(seed, fact1, fact2, rule.OutputRelation),
				}
				results = append(results, df)
			}
		}
	}
	return results
}

// findFacts returns facts where entity is the subject and predicate matches.
// Results are sorted by (pinned desc, updatedAt desc) for deterministic output.
func (ie *InferenceEngine) findFacts(
	entity, relation string,
	ownerID, projectLower string,
	now time.Time,
	visited *int,
	maxVisited int,
) []SemanticFact {
	var results []SemanticFact
	for _, idx := range ie.graph.adjacency[entity] {
		if *visited >= maxVisited {
			break
		}
		*visited++
		fact := ie.graph.facts[idx]
		if !ie.factAllowed(fact, ownerID, projectLower, now) {
			continue
		}
		if fact.Subject != entity {
			continue
		}
		if normalizeRelationName(fact.Predicate) != relation {
			continue
		}
		if fact.Negated {
			continue
		}
		results = append(results, fact)
	}
	// Deterministic ordering: pinned first, then most recently updated.
	for i := 1; i < len(results); i++ {
		for j := i; j > 0; j-- {
			a, b := results[j], results[j-1]
			swap := false
			if a.Pinned && !b.Pinned {
				swap = true
			} else if a.Pinned == b.Pinned && a.UpdatedAt.After(b.UpdatedAt) {
				swap = true
			}
			if !swap {
				break
			}
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
	return results
}

// factAllowed checks visibility constraints (owner, project, temporal, status).
func (ie *InferenceEngine) factAllowed(fact SemanticFact, ownerID, projectLower string, now time.Time) bool {
	// Owner isolation.
	if ownerID != "" && fact.OwnerID != "" && fact.OwnerID != ownerID {
		return false
	}
	// Project scope.
	if projectLower != "" && !semanticProjectAllowed(fact.Scope, fact.Tags, projectLower) {
		return false
	}
	// Temporal: only current facts.
	if !semanticFactCurrent(fact.ValidAt, fact.InvalidAt, now) {
		return false
	}
	// Status: only active.
	if fact.Status != StatusActive {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Contradiction detection
// ---------------------------------------------------------------------------

// filterContradictions removes derived facts that contradict existing direct facts.
// A contradiction occurs when a derived fact asserts (S, R, O1) but a direct
// fact already asserts (S, R, O2) where R is a functional relation (one value
// per subject) AND the derived fact is NOT a transitive extension of the direct fact.
//
// Example of NOT a contradiction: direct (A, located_in, B) + derived (A, located_in, C)
// where B is in the derivation chain (A→B→C). This is a valid transitive extension.
//
// Example of a contradiction: direct (A, located_in, Beijing) + derived (A, based_in, Hangzhou)
// via compositional rule — these are genuinely conflicting claims.
func (ie *InferenceEngine) filterContradictions(
	derived []DerivedFact,
	seeds map[string]struct{},
	opts InferenceOptions,
	projectLower string,
	now time.Time,
) []DerivedFact {
	if len(derived) == 0 {
		return derived
	}
	// Build a set of existing direct facts for seed entities.
	type factKey struct{ subject, predicate string }
	directObjects := make(map[factKey]map[string]struct{}) // (subject, predicate) → set of objects

	const maxContradictionScanPerSeed = 50 // limit traversal per seed for high-degree entities
	for seed := range seeds {
		scanned := 0
		for _, idx := range ie.graph.adjacency[seed] {
			if scanned >= maxContradictionScanPerSeed {
				break
			}
			scanned++
			fact := ie.graph.facts[idx]
			if fact.Subject != seed || fact.Negated {
				continue
			}
			if !ie.factAllowed(fact, opts.OwnerID, projectLower, now) {
				continue
			}
			rel := normalizeRelationName(fact.Predicate)
			if isFunctionalRelation(rel) {
				k := factKey{subject: seed, predicate: rel}
				if directObjects[k] == nil {
					directObjects[k] = make(map[string]struct{})
				}
				directObjects[k][fact.Object] = struct{}{}
			}
		}
	}

	if len(directObjects) == 0 {
		return derived
	}

	// Filter: suppress derived facts that contradict direct facts.
	result := derived[:0]
	for _, df := range derived {
		k := factKey{subject: df.Subject, predicate: normalizeRelationName(df.Predicate)}
		existingObjs, exists := directObjects[k]
		if !exists {
			result = append(result, df)
			continue
		}
		// If the derived object is already a known direct object, it's redundant (not a contradiction).
		if _, isKnown := existingObjs[df.Object]; isKnown {
			continue // redundant, skip
		}
		// Check if this is a transitive extension: if any source fact's subject
		// is one of the direct objects, it's extending the chain, not contradicting.
		isTransitiveExtension := false
		for _, sf := range df.SourceFacts {
			if _, ok := existingObjs[sf.Subject]; ok {
				isTransitiveExtension = true
				break
			}
		}
		if isTransitiveExtension {
			result = append(result, df) // valid transitive extension
		}
		// else: genuine contradiction, suppress
	}
	return result
}

// isFunctionalRelation returns true for relations where a subject should have
// at most one value. Uses the SemanticGraph's relation schema as the single
// source of truth, plus derived output relations from compositional rules.
func isFunctionalRelation(relation string) bool {
	// Check the canonical schema first.
	if spec, ok := semanticRelationSchema[relation]; ok {
		return spec.Functional
	}
	// Derived output relations from compositional rules — these inherit
	// functionality from their source relations.
	switch relation {
	case "based_in", "deployed_in", "created_at_org", "manages_at", "uses_version":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Explanation builders
// ---------------------------------------------------------------------------

func buildTransitiveExplanation(seed, relation string, chain []SemanticFact) string {
	// Format: "seed → rel → A → rel → B ∴ seed rel B"
	// Use the original fact content's entity names for readability.
	var sb strings.Builder
	sb.WriteString(seed)
	for _, fact := range chain {
		sb.WriteString(" → ")
		sb.WriteString(fact.Predicate) // use original predicate (may have underscores)
		sb.WriteString(" → ")
		sb.WriteString(fact.Object)
	}
	sb.WriteString(" ∴ ")
	sb.WriteString(seed)
	sb.WriteString(" ")
	sb.WriteString(relation)
	sb.WriteString(" ")
	sb.WriteString(chain[len(chain)-1].Object)
	return sb.String()
}

func buildCompositionalExplanation(seed string, fact1, fact2 SemanticFact, outputRel string) string {
	return fmt.Sprintf("%s → %s → %s → %s → %s ∴ %s %s %s",
		seed, fact1.Predicate, fact1.Object, fact2.Predicate, fact2.Object,
		seed, outputRel, fact2.Object)
}

// ---------------------------------------------------------------------------
// Deduplication
// ---------------------------------------------------------------------------

func deduplicateDerivedFacts(facts []DerivedFact) []DerivedFact {
	type key struct{ s, p, o string }
	best := make(map[key]DerivedFact)
	order := make([]key, 0, len(facts)) // preserve insertion order for determinism
	for _, df := range facts {
		k := key{s: df.Subject, p: df.Predicate, o: df.Object}
		if existing, ok := best[k]; !ok {
			best[k] = df
			order = append(order, k)
		} else if df.Confidence > existing.Confidence {
			best[k] = df
		}
	}
	result := make([]DerivedFact, 0, len(order))
	for _, k := range order {
		result = append(result, best[k])
	}
	// Sort by confidence descending for stable output.
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j].Confidence > result[j-1].Confidence; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Builtin Rules
// ---------------------------------------------------------------------------

// BuiltinInferenceRules is the default set of reasoning rules.
var BuiltinInferenceRules = []InferenceRule{
	// Transitive relations.
	{Name: "located_in_transitive", Type: RuleTransitive, Relation: "located_in", MaxChainLength: 3, ConfidenceDecay: 0.85},
	{Name: "belongs_to_transitive", Type: RuleTransitive, Relation: "belongs_to", MaxChainLength: 3, ConfidenceDecay: 0.85},
	{Name: "part_of_transitive", Type: RuleTransitive, Relation: "part_of", MaxChainLength: 3, ConfidenceDecay: 0.80},
	{Name: "depends_on_transitive", Type: RuleTransitive, Relation: "depends_on", MaxChainLength: 2, ConfidenceDecay: 0.70},

	// Compositional rules.
	{Name: "works_at+located_in=based_in", Type: RuleCompositional, InputRelation1: "works_at", InputRelation2: "located_in", OutputRelation: "based_in", ConfidenceDecay: 0.75},
	{Name: "uses+version_of=uses_version", Type: RuleCompositional, InputRelation1: "uses", InputRelation2: "version_of", OutputRelation: "uses_version", ConfidenceDecay: 0.80},
	{Name: "created_by+works_at=created_at_org", Type: RuleCompositional, InputRelation1: "created_by", InputRelation2: "works_at", OutputRelation: "created_at_org", ConfidenceDecay: 0.70},
	{Name: "deployed_on+located_in=deployed_in", Type: RuleCompositional, InputRelation1: "deployed_on", InputRelation2: "located_in", OutputRelation: "deployed_in", ConfidenceDecay: 0.75},
	{Name: "manages+works_at=manages_at", Type: RuleCompositional, InputRelation1: "manages", InputRelation2: "works_at", OutputRelation: "manages_at", ConfidenceDecay: 0.65},
}

// FormatDerivedFactsForPrompt formats derived facts as a human-readable section
// suitable for injection into the system prompt.
func FormatDerivedFactsForPrompt(facts []DerivedFact, maxFacts int) string {
	if len(facts) == 0 {
		return ""
	}
	if maxFacts <= 0 {
		maxFacts = 5
	}
	if len(facts) > maxFacts {
		facts = facts[:maxFacts]
	}
	var sb strings.Builder
	sb.WriteString("\n[推理链（自动推导，非确定事实）]\n")
	for _, df := range facts {
		sb.WriteString(fmt.Sprintf("• %s (置信度: %.0f%%)\n", df.Explanation, df.Confidence*100))
	}
	return sb.String()
}
