package routingeval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const defaultEvalNow = "2026-08-15T00:00:00Z"

// LoadDatasetFiles reads every JSON document under dir.
func LoadDatasetFiles(dir string) ([]DatasetFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]DatasetFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		var file DatasetFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, fmt.Errorf("decode %s: %w", entry.Name(), err)
		}
		if strings.TrimSpace(file.Category) == "" {
			return nil, fmt.Errorf("%s: category is required", entry.Name())
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].CategoryID == files[j].CategoryID {
			return files[i].Category < files[j].Category
		}
		return files[i].CategoryID < files[j].CategoryID
	})
	return files, nil
}

// RunSample evaluates one sample against ToolPlanner.
func RunSample(sample Sample) error {
	_, err := EvaluateSample(sample)
	return err
}

// EvaluateSample plans one sample and returns the ToolPlan when planning
// succeeded. Expected publish/plan errors return a nil plan and a nil error.
func EvaluateSample(sample Sample) (*tool.ToolPlan, error) {
	now, err := sampleNow(sample)
	if err != nil {
		return nil, err
	}
	registry, err := evalRegistry()
	if err != nil {
		return nil, err
	}
	if override := strings.TrimSpace(sample.Catalog.RegistryVersionOverride); override != "" {
		registry = tool.NewCapabilityRegistry(override)
		if err := tool.RegisterBuiltinCapabilityOntology(registry); err != nil {
			return nil, err
		}
	}
	snapshot, publishErr := publishSampleCatalog(registry, sample, now)
	if want := strings.TrimSpace(sample.Expected.PublishErrorContains); want != "" {
		if publishErr == nil || !strings.Contains(publishErr.Error(), want) {
			return nil, fmt.Errorf("publish error %v, want substring %q", publishErr, want)
		}
		return nil, nil
	}
	if publishErr != nil {
		return nil, fmt.Errorf("publish catalog: %w", publishErr)
	}
	needs, err := sampleNeeds(sample)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(strings.TrimSpace(sample.Mode), "needs") {
		if err := assertNeeds(needs, sample.Expected.Needs); err != nil {
			return nil, err
		}
	}
	facts := sampleFacts(sample, now)
	plan, planErr := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID:   sampleRootTaskID(sample),
		SessionID:    sampleSessionID(sample),
		TurnID:       sampleTurnID(sample),
		ChannelScope: sampleChannel(sample),
		Snapshot:     snapshot,
		Needs:        needs,
		Facts:        facts,
		Constraints:  sampleConstraints(sample, now),
		Budget:       tool.PlanningBudget{MaxSelections: sample.Context.MaxSelections, MaxSchemaTokens: sample.Context.MaxSchemaTokens},
		Now:          now,
	})
	if want := strings.TrimSpace(sample.Expected.PlanErrorContains); want != "" {
		if planErr == nil || !strings.Contains(planErr.Error(), want) {
			return nil, fmt.Errorf("plan error %v, want substring %q", planErr, want)
		}
		return nil, nil
	}
	if planErr != nil {
		return nil, fmt.Errorf("plan: %w", planErr)
	}
	if err := assertPlan(plan, sample, now, facts); err != nil {
		return nil, err
	}
	return &plan, nil
}

func sampleNeeds(sample Sample) ([]tool.CapabilityNeed, error) {
	if strings.EqualFold(strings.TrimSpace(sample.Mode), "needs") {
		return deriveNeeds(sample.Labels)
	}
	needs := make([]tool.CapabilityNeed, 0, len(sample.Needs))
	for _, spec := range sample.Needs {
		polarity := tool.NeedPolarity(spec.Polarity)
		if polarity == "" {
			polarity = tool.NeedRequire
		}
		required := polarity == tool.NeedRequire
		if spec.Required != nil {
			required = *spec.Required
		}
		// An iterative meaning is expanded the way the host resolver expands
		// it: one sibling need per permitted invocation, sharing a family ID.
		// Writing the siblings out by hand would let the dataset drift from
		// the identity scheme the planner and host actually agree on.
		for index := 0; index < tool.RepeatSiblingBudget(spec.MaxInvocations); index++ {
			needs = append(needs, tool.CapabilityNeed{
				ID:         tool.RepeatSiblingNeedID(spec.ID, index),
				Capability: tool.CapabilityID(spec.Capability),
				Qualifiers: spec.Qualifiers,
				Polarity:   polarity,
				Required:   tool.RepeatSiblingRequired(required, index),
			})
		}
	}
	return needs, nil
}

func publishSampleCatalog(registry *tool.CapabilityRegistry, sample Sample, now time.Time) (tool.ToolCatalogSnapshot, error) {
	providers := make([]tool.ProviderSpec, 0, len(sample.Catalog.Providers))
	for _, spec := range sample.Catalog.Providers {
		authorization, err := tool.NewParameterAuthorization(map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false,
		})
		if err != nil {
			return tool.ToolCatalogSnapshot{}, err
		}
		if salt := strings.TrimSpace(spec.SchemaSalt); salt != "" {
			authorization, err = tool.NewParameterAuthorization(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					salt: map[string]interface{}{"type": "string"},
				},
				"additionalProperties": false,
			})
			if err != nil {
				return tool.ToolCatalogSnapshot{}, err
			}
		}
		ready := true
		if spec.Ready != nil {
			ready = *spec.Ready
		}
		kind := strings.TrimSpace(spec.Kind)
		if kind == "" {
			kind = "builtin"
		}
		providerID := strings.TrimSpace(spec.ProviderID)
		if providerID == "" {
			providerID = "core"
		}
		implementationID := strings.TrimSpace(spec.ImplementationID)
		if implementationID == "" {
			implementationID = spec.Adapter
		}
		provides := make([]tool.CapabilityProvision, 0, len(spec.Capabilities))
		for _, provision := range spec.Capabilities {
			quality := 1.0
			if provision.Quality != nil {
				quality = *provision.Quality
			}
			provides = append(provides, tool.CapabilityProvision{
				Capability: tool.CapabilityID(provision.Capability),
				Qualifiers: provision.Qualifiers,
				Quality:    quality,
			})
		}
		effects := make([]tool.EffectClass, 0, len(spec.Effects))
		for _, effect := range spec.Effects {
			effects = append(effects, tool.EffectClass(effect))
		}
		var readyUntil time.Time
		if spec.ReadySeconds != nil {
			readyUntil = now.Add(time.Duration(*spec.ReadySeconds) * time.Second)
		}
		providers = append(providers, tool.ProviderSpec{
			AdapterName: spec.Adapter,
			Binding: tool.ProviderBinding{
				Kind:             kind,
				ProviderID:       providerID,
				ImplementationID: implementationID,
				SchemaDigest:     tool.SchemaDigest([]byte(spec.Adapter + spec.SchemaSalt)),
			},
			Classification:         tool.ProviderClassification(spec.Classification),
			ParameterAuthorization: authorization,
			Provides:               provides,
			Consumes:               toToolContracts(spec.Consumes),
			Produces:               toToolContracts(spec.Produces),
			Effects:                effects,
			Ready:                  ready,
			ReadyUntil:             readyUntil,
			ChannelScopes:          spec.ChannelScopes,
		})
	}
	catalog := tool.NewToolCatalog(registry)
	if sample.Catalog.Coverage == nil {
		return catalog.Publish(providers, now)
	}
	return catalog.PublishWithCoverage(providers, toCoverage(*sample.Catalog.Coverage, now), now)
}

func toToolContracts(in []ArtifactContract) []tool.ArtifactContract {
	out := make([]tool.ArtifactContract, 0, len(in))
	for _, contract := range in {
		out = append(out, tool.ArtifactContract{Kind: contract.Kind, MIMEType: contract.MIMEType, Required: contract.Required})
	}
	return out
}

func toCoverage(spec CoverageSpec, now time.Time) tool.CatalogCoverage {
	coverage := tool.CatalogCoverage{
		State:      tool.CatalogCoverageState(spec.State),
		ReasonCode: spec.ReasonCode,
		ObservedAt: now,
	}
	if spec.StaleSeconds != nil {
		coverage.StaleUntil = now.Add(time.Duration(*spec.StaleSeconds) * time.Second)
	}
	for _, family := range spec.Families {
		item := tool.CatalogCoverageFamily{
			Kind: family.Kind, State: tool.CatalogCoverageState(family.State), ReasonCode: family.ReasonCode,
		}
		if family.StaleSeconds != nil {
			item.StaleUntil = now.Add(time.Duration(*family.StaleSeconds) * time.Second)
		}
		coverage.Families = append(coverage.Families, item)
	}
	return coverage
}

func sampleFacts(sample Sample, now time.Time) []tool.RoutingFact {
	facts := make([]tool.RoutingFact, 0, len(sample.Facts))
	for _, spec := range sample.Facts {
		fact := tool.RoutingFact{
			ID: spec.ID, Kind: spec.Kind, Authority: tool.FactAuthority(spec.Authority), Attributes: spec.Attributes,
		}
		if spec.ValidSeconds != nil {
			fact.ValidUntil = now.Add(time.Duration(*spec.ValidSeconds) * time.Second)
		}
		if spec.Artifact != nil {
			binding := tool.ArtifactBinding{
				ID: spec.Artifact.ID, Kind: spec.Artifact.Kind, MIMEType: spec.Artifact.MIMEType,
				IntegrityDigest: spec.Artifact.IntegrityDigest, ProducerSelection: spec.Artifact.ProducerSelection,
				Scope: tool.InvocationScope{
					RootTaskID: spec.Artifact.Scope.RootTaskID, PlanID: spec.Artifact.Scope.PlanID,
					SessionID: spec.Artifact.Scope.SessionID, TurnID: spec.Artifact.Scope.TurnID,
					PrincipalID: spec.Artifact.Scope.PrincipalID,
				},
			}
			fact.Artifact = &binding
		}
		facts = append(facts, fact)
	}
	return facts
}

func sampleConstraints(sample Sample, now time.Time) []tool.RoutingConstraint {
	constraints := make([]tool.RoutingConstraint, 0, len(sample.Constraints))
	for _, spec := range sample.Constraints {
		item := tool.RoutingConstraint{
			ID: spec.ID, Capability: tool.CapabilityID(spec.Capability), Effect: spec.Effect,
			Authority: tool.FactAuthority(spec.Authority), Attributes: spec.Attributes,
		}
		if spec.ValidSeconds != nil {
			item.ValidUntil = now.Add(time.Duration(*spec.ValidSeconds) * time.Second)
		}
		constraints = append(constraints, item)
	}
	return constraints
}

func assertNeeds(got []tool.CapabilityNeed, expected []ExpectedNeedSpec) error {
	gotSet := make(map[string]bool, len(got))
	for _, need := range got {
		gotSet[needKey(need.Capability, need.Qualifiers, need.Polarity)] = true
	}
	wantSet := make(map[string]bool, len(expected))
	for _, spec := range expected {
		polarity := tool.NeedPolarity(spec.Polarity)
		if polarity == "" {
			polarity = tool.NeedRequire
		}
		wantSet[needKey(tool.CapabilityID(spec.Capability), spec.Qualifiers, polarity)] = true
	}
	for key := range wantSet {
		if !gotSet[key] {
			return fmt.Errorf("missing need %s", key)
		}
	}
	for key := range gotSet {
		if !wantSet[key] {
			return fmt.Errorf("unexpected need %s", key)
		}
	}
	return nil
}

func assertPlan(plan tool.ToolPlan, sample Sample, now time.Time, facts []tool.RoutingFact) error {
	if sample.Expected.NoSelections && len(plan.Selections) != 0 {
		return fmt.Errorf("selections=%d, want none", len(plan.Selections))
	}
	if len(sample.Expected.Selections) > 0 {
		if err := assertSelections(plan, sample.Expected.Selections); err != nil {
			return err
		}
	}
	if err := assertUnmet(plan, sample.Expected.Unmet); err != nil {
		return err
	}
	if err := assertArtifactEdges(plan, sample.Expected.ArtifactEdges); err != nil {
		return err
	}
	if sample.Expected.ReadyWithoutFacts != nil {
		ready := plan.ReadySelections(nil)
		got := make([]string, 0, len(ready))
		for _, selection := range ready {
			got = append(got, selection.NeedID)
		}
		if !sameStringSet(got, sample.Expected.ReadyWithoutFacts) {
			return fmt.Errorf("ready without facts=%v, want %v", got, sample.Expected.ReadyWithoutFacts)
		}
	}
	if sample.Expected.TrustedConfirmations != nil {
		satisfied := plan.TrustedSatisfiedDependencies(facts, now)
		got := make([]string, 0, len(satisfied))
		for requirement, ok := range satisfied {
			if ok {
				got = append(got, requirement)
			}
		}
		if !sameStringSet(got, sample.Expected.TrustedConfirmations) {
			return fmt.Errorf("trusted confirmations=%v, want %v", got, sample.Expected.TrustedConfirmations)
		}
	}
	return nil
}

func assertCrossSampleInvariants(plans map[string]tool.ToolPlan, category, sampleID string, expected ExpectedSpec) error {
	current, ok := plans[sampleKey(category, sampleID)]
	if !ok {
		return fmt.Errorf("sample %s/%s has no planned identity", category, sampleID)
	}
	if ref := strings.TrimSpace(expected.PlanIDEquals); ref != "" {
		other, err := lookupCrossSamplePlan(plans, category, ref)
		if err != nil {
			return err
		}
		if current.ID != other.ID {
			return fmt.Errorf("plan_id_equals %s: got %s, want %s", ref, current.ID, other.ID)
		}
	}
	if ref := strings.TrimSpace(expected.PlanIDDiffers); ref != "" {
		other, err := lookupCrossSamplePlan(plans, category, ref)
		if err != nil {
			return err
		}
		if current.ID == other.ID {
			return fmt.Errorf("plan_id_differs %s: both produced %s", ref, current.ID)
		}
	}
	if ref := strings.TrimSpace(expected.EquivalentTo); ref != "" {
		other, err := lookupCrossSamplePlan(plans, category, ref)
		if err != nil {
			return err
		}
		if !sameSelectionAuthority(current, other) {
			return fmt.Errorf("equivalent_to %s: selections %#v vs %#v", ref, current.Selections, other.Selections)
		}
	}
	return nil
}

func lookupCrossSamplePlan(plans map[string]tool.ToolPlan, category, ref string) (tool.ToolPlan, error) {
	key := ref
	if !strings.Contains(ref, "/") {
		key = sampleKey(category, ref)
	}
	plan, ok := plans[key]
	if !ok {
		return tool.ToolPlan{}, fmt.Errorf("cross-sample ref %q is missing", ref)
	}
	return plan, nil
}

func sampleKey(category, id string) string {
	return strings.TrimSpace(category) + "/" + strings.TrimSpace(id)
}

func sameSelectionAuthority(left, right tool.ToolPlan) bool {
	if len(left.Selections) != len(right.Selections) || len(left.Unmet) != len(right.Unmet) {
		return false
	}
	byNeed := make(map[string]tool.PlannedSelection, len(right.Selections))
	for _, selection := range right.Selections {
		byNeed[selection.NeedID] = selection
	}
	for _, selection := range left.Selections {
		other, ok := byNeed[selection.NeedID]
		if !ok || string(selection.FitProof.MatchedCapability) != string(other.FitProof.MatchedCapability) || string(selection.Phase) != string(other.Phase) {
			return false
		}
	}
	return true
}

func assertSelections(plan tool.ToolPlan, expected []SelectionExpectation) error {
	byNeed := make(map[string]tool.PlannedSelection, len(plan.Selections))
	for _, selection := range plan.Selections {
		byNeed[selection.NeedID] = selection
	}
	if len(byNeed) != len(expected) {
		return fmt.Errorf("planned %d selections, expected %d", len(byNeed), len(expected))
	}
	for _, want := range expected {
		got, ok := byNeed[want.NeedID]
		if !ok {
			return fmt.Errorf("missing selection for need %q", want.NeedID)
		}
		if want.Capability != "" && string(got.FitProof.MatchedCapability) != want.Capability {
			return fmt.Errorf("need %s capability=%s, want %s", want.NeedID, got.FitProof.MatchedCapability, want.Capability)
		}
		if want.Adapter != "" && got.AdapterName != want.Adapter {
			return fmt.Errorf("need %s adapter=%s, want %s", want.NeedID, got.AdapterName, want.Adapter)
		}
		if want.Phase != "" && string(got.Phase) != want.Phase {
			return fmt.Errorf("need %s phase=%s, want %s", want.NeedID, got.Phase, want.Phase)
		}
		if want.RequiresConfirmation != nil && got.RequiresConfirm != *want.RequiresConfirmation {
			return fmt.Errorf("need %s confirm=%v, want %v", want.NeedID, got.RequiresConfirm, *want.RequiresConfirmation)
		}
		if want.ProviderBindingContain != "" && !strings.Contains(got.Provider.StableID(), want.ProviderBindingContain) {
			return fmt.Errorf("need %s binding=%s, want substring %q", want.NeedID, got.Provider.StableID(), want.ProviderBindingContain)
		}
	}
	return nil
}

func assertUnmet(plan tool.ToolPlan, expected []UnmetExpectation) error {
	if expected == nil {
		return nil
	}
	got := make(map[string]string, len(plan.Unmet))
	for _, unmet := range plan.Unmet {
		got[unmet.NeedID] = unmet.ReasonCode
	}
	if len(got) != len(expected) {
		return fmt.Errorf("unmet=%v, want %d entries", plan.Unmet, len(expected))
	}
	for _, want := range expected {
		if got[want.NeedID] != want.Reason {
			return fmt.Errorf("unmet %s=%q, want %q", want.NeedID, got[want.NeedID], want.Reason)
		}
	}
	return nil
}

func assertArtifactEdges(plan tool.ToolPlan, expected []ArtifactEdgeSpec) error {
	if len(expected) == 0 {
		return nil
	}
	byNeed := make(map[string]tool.PlannedSelection, len(plan.Selections))
	for _, selection := range plan.Selections {
		byNeed[selection.NeedID] = selection
	}
	for _, want := range expected {
		consumer, ok := byNeed[want.ConsumerNeed]
		if !ok {
			return fmt.Errorf("artifact edge consumer %q not planned", want.ConsumerNeed)
		}
		found := false
		for _, dep := range consumer.ArtifactDependencies {
			if want.ProducerNeed != "" {
				producer, ok := byNeed[want.ProducerNeed]
				if ok && dep.ProducerSelection == producer.ID {
					found = true
				}
			}
			if want.ArtifactID != "" && dep.ArtifactID == want.ArtifactID {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("missing artifact edge %#v on %s deps=%#v", want, want.ConsumerNeed, consumer.ArtifactDependencies)
		}
	}
	return nil
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]int, len(got))
	for _, item := range got {
		seen[item]++
	}
	for _, item := range want {
		seen[item]--
		if seen[item] < 0 {
			return false
		}
	}
	return true
}

func sampleNow(sample Sample) (time.Time, error) {
	raw := strings.TrimSpace(sample.Context.Now)
	if raw == "" {
		raw = defaultEvalNow
	}
	return time.Parse(time.RFC3339, raw)
}

func sampleRootTaskID(sample Sample) string {
	if sample.Context.RootTaskID != "" {
		return sample.Context.RootTaskID
	}
	return "task-eval"
}

func sampleSessionID(sample Sample) string {
	if sample.Context.SessionID != "" {
		return sample.Context.SessionID
	}
	return "sess-eval"
}

func sampleTurnID(sample Sample) string {
	if sample.Context.TurnID != "" {
		return sample.Context.TurnID
	}
	return "turn-1"
}

func sampleChannel(sample Sample) string {
	if sample.Context.Channel != "" {
		return sample.Context.Channel
	}
	return "lansenger"
}
