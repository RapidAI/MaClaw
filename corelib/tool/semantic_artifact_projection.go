package tool

import (
	"fmt"
	"path/filepath"
	"strings"
)

// FirstRequiredArtifactContract returns the first required input contract in
// planner order. The order is part of the immutable plan and must not be
// reconstructed independently by a host adapter.
func FirstRequiredArtifactContract(contracts []ArtifactContract) ArtifactContract {
	for _, contract := range contracts {
		if contract.Required {
			return contract
		}
	}
	return ArtifactContract{}
}

// ArtifactFileName returns a safe display/materialization name for an
// artifact. Producer names are reduced to a basename; MIME supplies a stable
// fallback when no name was published.
func ArtifactFileName(ref ArtifactRef) string {
	if name := filepath.Base(strings.ReplaceAll(strings.TrimSpace(ref.Name), "\\", "/")); name != "" && name != "." && name != string(filepath.Separator) {
		return name
	}
	const base = "attachment"
	switch strings.ToLower(strings.TrimSpace(ref.MIMEType)) {
	case "application/pdf":
		return base + ".pdf"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return base + ".docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return base + ".xlsx"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return base + ".pptx"
	case "text/plain":
		return base + ".txt"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return base + ".wav"
	default:
		return base + ".bin"
	}
}

// DocumentTempSuffixForSelection maps the reviewed document format qualifier
// to a materialization suffix. Unknown or absent formats remain plain text so
// a host never infers a binary type from an untrusted adapter name.
func DocumentTempSuffixForSelection(selection PlannedSelection) string {
	switch strings.ToLower(strings.TrimSpace(selection.FitProof.QualifierBindings["format"])) {
	case "pdf":
		return ".pdf"
	case "word":
		return ".docx"
	case "spreadsheet":
		return ".xlsx"
	case "presentation":
		return ".pptx"
	default:
		return ".txt"
	}
}

// DocumentGenerateSelection identifies a host-owned document generate
// selection by capability first. Adapter names are a legacy fallback so a
// generate_pdf or host_document_generate_file binding without FitProof still
// participates in the same skip/hold-dependant filter.
func DocumentGenerateSelection(selection PlannedSelection) bool {
	if IsDocumentGenerateFile(selection.FitProof.MatchedCapability) {
		return true
	}
	switch strings.TrimSpace(selection.AdapterName) {
	case "generate_pdf", "host_document_generate_file":
		return true
	default:
		return false
	}
}

// HasUnissuedReadyDocumentGenerate reports a ready generate selection that
// still has no grant. GUI hold-dependant issue and any host that delays
// generate until lookup evidence exists share this check.
func HasUnissuedReadyDocumentGenerate(plan ToolPlan, completed, materialized map[string]bool) bool {
	for _, selection := range plan.ReadySelections(completed) {
		if completed[selection.ID] || materialized[selection.ID] {
			continue
		}
		if DocumentGenerateSelection(selection) {
			return true
		}
	}
	return false
}

// ForEachHostSatisfiedLookupForGenerate visits uncompleted lookup
// dependencies of host-owned generate selections. Confirmation requirements
// and non-lookup blockers fail closed so a host cannot mark generate ready
// from user text or a missing edge. The visit callback is host-owned
// (complete/retire); this helper only chooses which IDs may be satisfied.
func ForEachHostSatisfiedLookupForGenerate(plan ToolPlan, completed map[string]bool, visit func(lookupID string) error) error {
	if visit == nil {
		return nil
	}
	for _, selection := range plan.Selections {
		if !DocumentGenerateSelection(selection) || completed[selection.ID] {
			continue
		}
		for _, requirement := range selection.Requires {
			if completed[requirement] {
				continue
			}
			if strings.HasPrefix(requirement, "confirmation:") {
				return fmt.Errorf("generate still requires confirmation")
			}
			lookup, ok := PlanSelectionByID(plan, requirement)
			if !ok || !IsLookupSelection(lookup) {
				return fmt.Errorf("generate blocked by %s", requirement)
			}
			if err := visit(requirement); err != nil {
				return err
			}
		}
	}
	return nil
}

// CurrentChannelArtifactDeliverySelection identifies the reviewed channel
// delivery capability by provider kind and capability contract, not adapter
// name. This keeps receipt logic stable as hosts add channel implementations.
func CurrentChannelArtifactDeliverySelection(selection PlannedSelection) bool {
	return strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "channel") &&
		strings.EqualFold(strings.TrimSpace(string(selection.FitProof.MatchedCapability)), "artifact.deliver.current_channel")
}

// CurrentChannelDeliverNeed reports whether a granted need is current-channel
// delivery. GUI file-deliver binding and headless reviewed attachment deliver
// share this so a host cannot treat a specified-target send as channel ingress.
func CurrentChannelDeliverNeed(need CapabilityNeed) bool {
	return strings.EqualFold(strings.TrimSpace(string(need.Capability)), "artifact.deliver.current_channel")
}

// CurrentChannelDeliverAccepts reports whether the need is current-channel
// delivery whose format is one of formats. Empty format is only accepted when
// the caller lists "". GUI IM file-deliver passes "file"; headless reviewed
// deliver passes file/image/voice and empty.
func CurrentChannelDeliverAccepts(need CapabilityNeed, formats ...string) bool {
	if !CurrentChannelDeliverNeed(need) {
		return false
	}
	format := ""
	if need.Qualifiers != nil {
		format = need.Qualifiers["format"]
	}
	for _, allowed := range formats {
		if format == allowed {
			return true
		}
	}
	return false
}

// ArtifactContractMatches reports whether produced satisfies required.
// Kind is EqualFold; an empty required MIMEType is a wildcard so a consumer
// that does not pin a type can still bind a typed producer. Required is not
// compared — that belongs to sameArtifactContract / ArtifactContractsEqual.
func ArtifactContractMatches(produced, required ArtifactContract) bool {
	return strings.EqualFold(strings.TrimSpace(produced.Kind), strings.TrimSpace(required.Kind)) &&
		(strings.TrimSpace(required.MIMEType) == "" || strings.EqualFold(strings.TrimSpace(produced.MIMEType), strings.TrimSpace(required.MIMEType)))
}

// ArtifactBindingMatchesContract is the binding form of ArtifactContractMatches.
func ArtifactBindingMatchesContract(binding ArtifactBinding, contract ArtifactContract) bool {
	return ArtifactContractMatches(ArtifactContract{Kind: binding.Kind, MIMEType: binding.MIMEType}, contract)
}

// ProducerArtifactPublished reports a RouteState artifact published by
// producerSelection that satisfies contract. Empty producer or no match
// fail closed so a host cannot auto-deliver from an unpublished producer.
func ProducerArtifactPublished(refs []RouteArtifactRef, producerSelection string, contract ArtifactContract) bool {
	producerSelection = strings.TrimSpace(producerSelection)
	if producerSelection == "" {
		return false
	}
	for _, candidate := range refs {
		if candidate.ProducerSelection != producerSelection {
			continue
		}
		if ArtifactContractMatches(ArtifactContract{Kind: candidate.Kind, MIMEType: candidate.MIMEType, Required: true}, contract) {
			return true
		}
	}
	return false
}

func uniqueMatchingArtifactDependency(deps []ArtifactDependency, contract ArtifactContract, boundOnly bool) (ArtifactDependency, error) {
	matches := make([]ArtifactDependency, 0, 1)
	for _, dependency := range deps {
		if boundOnly && strings.TrimSpace(dependency.ArtifactID) == "" {
			continue
		}
		if ArtifactContractMatches(dependency.Contract, contract) {
			matches = append(matches, dependency)
		}
	}
	switch len(matches) {
	case 0:
		return ArtifactDependency{}, fmt.Errorf("artifact_dependency_unbound")
	case 1:
		return matches[0], nil
	default:
		return ArtifactDependency{}, fmt.Errorf("artifact_dependency_ambiguous")
	}
}

// UniqueMatchingArtifactDependency returns the single planned dependency
// matching contract. Zero matches are unbound; two or more are ambiguous.
func UniqueMatchingArtifactDependency(deps []ArtifactDependency, contract ArtifactContract) (ArtifactDependency, error) {
	return uniqueMatchingArtifactDependency(deps, contract, false)
}

// UniqueBoundArtifactDependency is UniqueMatchingArtifactDependency restricted
// to dependencies that already carry an ArtifactID.
func UniqueBoundArtifactDependency(deps []ArtifactDependency, contract ArtifactContract) (ArtifactDependency, error) {
	return uniqueMatchingArtifactDependency(deps, contract, true)
}

// ValidateBoundArtifactDependency checks that a bound edge's ArtifactID and
// binding agree and satisfy contract. Hosts call this after UniqueBound.
func ValidateBoundArtifactDependency(dep ArtifactDependency, contract ArtifactContract) error {
	if strings.TrimSpace(dep.ArtifactID) == "" {
		return fmt.Errorf("artifact_dependency_unbound")
	}
	if dep.Artifact.ID != dep.ArtifactID || !ArtifactBindingMatchesContract(dep.Artifact, contract) {
		return fmt.Errorf("artifact_dependency_invalid")
	}
	return nil
}

// NewestFamilyProducerArtifact picks the newest RouteState artifact in the
// producer selection's repeat family that satisfies contract. Delivery bound
// to the first sibling must still reach a later revision of the same meaning;
// across families the lookup stays fail-closed.
func NewestFamilyProducerArtifact(refs []RouteArtifactRef, producerSelection string, contract ArtifactContract) (ArtifactRef, bool) {
	producerFamily := RepeatFamilyID(producerSelection)
	if producerFamily == "" {
		return ArtifactRef{}, false
	}
	var source ArtifactRef
	for _, candidate := range refs {
		ref := candidate.ArtifactRef()
		if RepeatFamilyID(ref.ProducerSelection) != producerFamily {
			continue
		}
		if !ArtifactContractMatches(ArtifactContract{Kind: ref.Kind, MIMEType: ref.MIMEType, Required: true}, contract) {
			continue
		}
		if source.ID != "" && !ref.CreatedAt.After(source.CreatedAt) {
			continue
		}
		source = ref
	}
	if source.ID == "" {
		return ArtifactRef{}, false
	}
	return source, true
}

// ArtifactDependencyKind is the planned artifact kind for a delivery edge.
// Binding.Kind wins; contract.Kind is the fallback when the producer has not
// yet projected a concrete artifact.
func ArtifactDependencyKind(dep ArtifactDependency) string {
	kind := strings.TrimSpace(dep.Artifact.Kind)
	if kind == "" {
		kind = strings.TrimSpace(dep.Contract.Kind)
	}
	return kind
}

// CurrentChannelDeliveryDependency returns the single artifact edge of a
// current-channel delivery grant. Zero or many edges fail closed.
func CurrentChannelDeliveryDependency(plan ToolPlan, grant InvocationGrant) (ArtifactDependency, bool) {
	if strings.TrimSpace(grant.SelectionID) == "" {
		return ArtifactDependency{}, false
	}
	selection, ok := PlanSelectionByID(plan, grant.SelectionID)
	if !ok || !CurrentChannelArtifactDeliverySelection(selection) || len(selection.ArtifactDependencies) != 1 {
		return ArtifactDependency{}, false
	}
	return selection.ArtifactDependencies[0], true
}

// ScheduleChannelDispatchSelection is the schedule counterpart to
// CurrentChannelArtifactDeliverySelection.
func ScheduleChannelDispatchSelection(selection PlannedSelection) bool {
	return strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "channel") &&
		strings.EqualFold(strings.TrimSpace(string(selection.FitProof.MatchedCapability)), string(CapabilityScheduleDispatchChannel))
}

// TurnRequiredDeliveryComplete reports that every required planned selection
// completed and at least one completed selection is a current-channel
// delivery. Optional needs (optionalByNeed) never gate the stop. Hosts still
// compute optionality from their classification; this helper only applies it.
func TurnRequiredDeliveryComplete(plan ToolPlan, completed, optionalByNeed map[string]bool) bool {
	if len(plan.Selections) == 0 {
		return false
	}
	delivered := false
	for _, selection := range plan.Selections {
		if completed[selection.ID] {
			if CurrentChannelArtifactDeliverySelection(selection) {
				delivered = true
			}
			continue
		}
		if optionalByNeed[selection.NeedID] {
			continue
		}
		return false
	}
	return delivered
}
