package guiapp

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticSurfaceOptionalNeeds caches optional needs per immutable replan
// identity. A plan that recorded obligations is projected from its selections
// and does not resolve classification again. Older plans cache that resolve,
// then overlay the current Optional bits so an appended sibling cannot reuse
// a stale map. ok is false only when neither source can prove optionality.
func semanticSurfaceOptionalNeeds(surface *semanticCallSurface) (map[string]bool, bool) {
	if surface == nil || len(surface.plan.Selections) == 0 {
		return nil, false
	}
	if surface.plan.ObligationsRecorded {
		return optionalNeedsFromPlan(surface.plan), true
	}
	surface.optionalMu.Lock()
	defer surface.optionalMu.Unlock()
	cacheKey := semanticOptionalCacheKey(surface)
	// A failed resolve is cached too. Gating the hit on optionalKnown re-ran
	// classification on every completion check after the first error. The set
	// flag distinguishes that cached failure from the zero key, which would
	// otherwise look like a hit before the first resolve. Bits are overlaid
	// on the way out, so an appended sibling does not need a new resolve.
	if !surface.optionalCacheSet || cacheKey != surface.optionalCacheKey {
		byNeed := map[string]bool{}
		known := false
		if surface.registry != nil && surface.replan != nil {
			needsCtx := withSemanticArchetypeBundleKeyOverride(context.Background(), surface.replan.BundleKey)
			needs, _, err := semanticNeedsFromClassificationContext(needsCtx, surface.registry, surface.replan.Classification)
			if err == nil {
				known = true
				for _, need := range needs {
					if !need.Required {
						if id := strings.TrimSpace(need.ID); id != "" {
							byNeed[id] = true
						}
					}
				}
			}
		}
		if !known {
			byNeed = nil
		}
		surface.optionalByNeed = byNeed
		surface.optionalKnown = known
		surface.optionalCacheKey = cacheKey
		surface.optionalCacheSet = true
	}
	return overlayPlanOptional(surface.optionalByNeed, surface.optionalKnown, surface.plan)
}

func overlayPlanOptional(base map[string]bool, known bool, plan tool.ToolPlan) (map[string]bool, bool) {
	extra := optionalNeedsFromPlan(plan)
	if len(extra) == 0 {
		return base, known
	}
	if !known {
		return extra, true
	}
	merged := make(map[string]bool, len(base)+len(extra))
	for id := range base {
		merged[id] = true
	}
	for id := range extra {
		merged[id] = true
	}
	return merged, true
}

func optionalNeedsFromPlan(plan tool.ToolPlan) map[string]bool {
	var byNeed map[string]bool
	for _, selection := range plan.Selections {
		if !selection.Optional {
			continue
		}
		id := strings.TrimSpace(selection.NeedID)
		if id == "" {
			continue
		}
		if byNeed == nil {
			byNeed = make(map[string]bool)
		}
		byNeed[id] = true
	}
	return byNeed
}

func semanticOptionalCacheKey(surface *semanticCallSurface) string {
	if surface == nil {
		return ""
	}
	key := surface.plan.ID
	if surface.replan != nil {
		key += "\n" + string(surface.replan.BundleKey) + "\n" + string(surface.replan.Classification.Primary) + "\n" + surface.replan.Classification.Reason
	}
	return key
}

func fileDeliverableCapability(capability tool.CapabilityID) bool {
	switch capability {
	case agentservice.CapabilityDocumentGenerate, tool.CapabilityDocumentWriteOffice:
		return true
	default:
		return false
	}
}

func planHasFileDeliverable(plan tool.ToolPlan) bool {
	for _, selection := range plan.Selections {
		if fileDeliverableCapability(selection.FitProof.MatchedCapability) {
			return true
		}
	}
	return false
}

// unassignedFileDeliverableNames lists required file producers the host has
// not completed. Optional needs and repeat-family ceilings are not a contract.
// The names are capability ids. A prose claim does not remove a name; only
// the completed map does.
func unassignedFileDeliverableNames(plan tool.ToolPlan, completed, optionalByNeed map[string]bool) []string {
	var names []string
	seen := map[string]bool{}
	for _, selection := range plan.Selections {
		capability := selection.FitProof.MatchedCapability
		if !fileDeliverableCapability(capability) {
			continue
		}
		if completed[selection.ID] {
			continue
		}
		// Same skips as the hop: optional bit, companion offer, repeat ceiling.
		// A lost Optional bit on an offer must not tell the model the file is
		// still missing after the turn has already settled.
		if tool.SelectionCallIsSkippable(selection) || optionalByNeed[selection.NeedID] {
			continue
		}
		name := strings.TrimSpace(string(capability))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// hostAttachedDocumentNames is the files this response is already carrying.
// Paths win, so a leftover name cannot close a different producer than the
// file the host actually attached. A name with no path counts only when the
// bytes are on the response (an IM send that never wrote a local path).
func hostAttachedDocumentNames(resp *IMAgentResponse) []string {
	if resp == nil {
		return nil
	}
	var names []string
	if path := strings.TrimSpace(resp.LocalFilePath); path != "" {
		names = append(names, path)
	}
	for _, path := range resp.LocalFilePaths {
		if path = strings.TrimSpace(path); path != "" {
			names = append(names, path)
		}
	}
	if len(names) > 0 {
		return names
	}
	if strings.TrimSpace(resp.FileData) == "" {
		return nil
	}
	if name := strings.TrimSpace(resp.FileName); name != "" {
		return []string{name}
	}
	return nil
}

// hostAttachedFilesSatisfySelection reports that an attached file is the
// effect this selection owed. Office and PDF producers match by type. A
// current-channel delivery matches when the host attached that format,
// because the response is the delivery. A specified-target send is a
// different channel and stays open.
func hostAttachedFilesSatisfySelection(names []string, selection tool.PlannedSelection) bool {
	for _, name := range names {
		if hostArtifactSatisfiesFileProducer(name, selection.FitProof.MatchedCapability) {
			return true
		}
		if hostArtifactSatisfiesCurrentChannelDelivery(name, selection) {
			return true
		}
	}
	return false
}

func hostArtifactSatisfiesFileProducer(name string, capability tool.CapabilityID) bool {
	switch capability {
	case tool.CapabilityDocumentWriteOffice:
		return officeDocumentExt(name)
	case agentservice.CapabilityDocumentGenerate:
		return strings.EqualFold(filepath.Ext(name), ".pdf")
	default:
		return false
	}
}

func officeDocumentExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pptx", ".ppt", ".docx", ".doc", ".xlsx", ".xls", ".odp", ".odt", ".ods":
		return true
	default:
		return false
	}
}

func hostArtifactSatisfiesCurrentChannelDelivery(name string, selection tool.PlannedSelection) bool {
	if !tool.CurrentChannelArtifactDeliverySelection(selection) {
		return false
	}
	format := ""
	if selection.FitProof.QualifierBindings != nil {
		format = strings.ToLower(strings.TrimSpace(selection.FitProof.QualifierBindings["format"]))
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch format {
	case "", "file":
		return officeDocumentExt(name) || ext == ".pdf"
	case "pdf":
		return ext == ".pdf"
	default:
		return false
	}
}
