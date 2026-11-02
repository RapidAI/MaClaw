package guiapp

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticSurfaceOptionalNeeds caches optional needs per immutable replan
// identity. Completion gates do not re-plan unless a petition changes that
// identity. ok is false when optionality cannot be proved; callers fail closed.
func semanticSurfaceOptionalNeeds(surface *semanticCallSurface) (map[string]bool, bool) {
	if surface == nil || len(surface.plan.Selections) == 0 || surface.registry == nil || surface.replan == nil {
		return nil, false
	}
	surface.optionalMu.Lock()
	defer surface.optionalMu.Unlock()
	cacheKey := semanticOptionalCacheKey(surface)
	if cacheKey != surface.optionalCacheKey {
		needsCtx := withSemanticArchetypeBundleKeyOverride(context.Background(), surface.replan.BundleKey)
		needs, _, err := semanticNeedsFromClassificationContext(needsCtx, surface.registry, surface.replan.Classification)
		if err != nil {
			surface.optionalByNeed, surface.optionalKnown = nil, false
			surface.optionalCacheKey = ""
		} else {
			surface.optionalByNeed = make(map[string]bool, len(needs))
			for _, need := range needs {
				if !need.Required {
					surface.optionalByNeed[need.ID] = true
				}
			}
			surface.optionalKnown = true
			surface.optionalCacheKey = cacheKey
		}
	}
	return surface.optionalByNeed, surface.optionalKnown
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
		if optionalByNeed[selection.NeedID] {
			continue
		}
		needID := strings.TrimSpace(selection.NeedID)
		if needID != "" && tool.RepeatFamilyID(needID) != needID {
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
