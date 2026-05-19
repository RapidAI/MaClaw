package memory

import "strings"

// InferenceDiagnosticsData holds multi-hop reasoning diagnostics for host UIs.
type InferenceDiagnosticsData struct {
	EngineActive     bool                       `json:"engine_active"`
	RuleCount        int                        `json:"rule_count"`
	Rules            []InferenceDiagnosticsRule `json:"rules"`
	LastDerived      []InferenceDiagnosticsFact `json:"last_derived"`
	SemanticFacts    int                        `json:"semantic_facts"`
	SemanticEntities int                        `json:"semantic_entities"`
}

// InferenceDiagnosticsRule describes one inference rule for host UIs.
type InferenceDiagnosticsRule struct {
	Name            string  `json:"name"`
	Type            string  `json:"type"`
	Relation        string  `json:"relation,omitempty"`
	InputRelation1  string  `json:"input_relation1,omitempty"`
	InputRelation2  string  `json:"input_relation2,omitempty"`
	OutputRelation  string  `json:"output_relation,omitempty"`
	MaxChainLength  int     `json:"max_chain_length,omitempty"`
	ConfidenceDecay float64 `json:"confidence_decay"`
}

// InferenceDiagnosticsFact describes one derived fact for host UIs.
type InferenceDiagnosticsFact struct {
	Subject     string  `json:"subject"`
	Predicate   string  `json:"predicate"`
	Object      string  `json:"object"`
	RuleName    string  `json:"rule_name"`
	Confidence  float64 `json:"confidence"`
	Explanation string  `json:"explanation"`
	SourceCount int     `json:"source_count"`
}

func (s *Store) InferenceDiagnosticsForHost() *InferenceDiagnosticsData {
	result := &InferenceDiagnosticsData{}
	if s == nil || s.InferenceEngine() == nil {
		return result
	}
	ie := s.InferenceEngine()
	result.EngineActive = true
	result.RuleCount = len(ie.Rules())
	for _, rule := range ie.Rules() {
		result.Rules = append(result.Rules, inferenceRuleDiagnostics(rule))
	}
	result.LastDerived = InferenceFactsForHost(s.LastDerivedFacts())
	if sg := s.SemanticGraph(); sg != nil {
		result.SemanticEntities, result.SemanticFacts, _ = sg.Stats()
	}
	return result
}

func (s *Store) TestInferenceForHost(query string, opts InferenceOptions) []InferenceDiagnosticsFact {
	if s == nil || strings.TrimSpace(query) == "" || s.InferenceEngine() == nil {
		return nil
	}
	expanded := ExpandQuery(query)
	if len(expanded.Entities) == 0 {
		return nil
	}
	if opts.MaxDerived <= 0 {
		opts.MaxDerived = 20
	}
	if opts.MinConfidence <= 0 {
		opts.MinConfidence = 0.40
	}
	if opts.MaxVisitedFacts <= 0 {
		opts.MaxVisitedFacts = 200
	}
	return InferenceFactsForHost(s.InferenceEngine().Infer(expanded.Entities, opts))
}

func InferenceFactsForHost(facts []DerivedFact) []InferenceDiagnosticsFact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]InferenceDiagnosticsFact, 0, len(facts))
	for _, fact := range facts {
		out = append(out, InferenceDiagnosticsFact{
			Subject:     fact.Subject,
			Predicate:   fact.Predicate,
			Object:      fact.Object,
			RuleName:    fact.RuleName,
			Confidence:  fact.Confidence,
			Explanation: fact.Explanation,
			SourceCount: len(fact.SourceFacts),
		})
	}
	return out
}

func inferenceRuleDiagnostics(rule InferenceRule) InferenceDiagnosticsRule {
	result := InferenceDiagnosticsRule{
		Name:            rule.Name,
		Type:            string(rule.Type),
		ConfidenceDecay: rule.ConfidenceDecay,
	}
	if rule.Type == RuleTransitive {
		result.Relation = rule.Relation
		result.MaxChainLength = rule.MaxChainLength
	} else {
		result.InputRelation1 = rule.InputRelation1
		result.InputRelation2 = rule.InputRelation2
		result.OutputRelation = rule.OutputRelation
	}
	return result
}
