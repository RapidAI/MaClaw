package agentservice

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type archetypeBundleKeyOverrideKey struct{}

// SemanticArchetypeBundles maps a classification primary to companion labels
// of its task archetype. GUI and headless hosts must expand from this table
// so a document-producing turn cannot grow a second, host-private face.
func SemanticArchetypeBundles() map[intent.IntentLabel][]intent.IntentLabel {
	return map[intent.IntentLabel][]intent.IntentLabel{
		intent.LabelOffice:           {intent.LabelSearch, intent.LabelWebFetch, intent.LabelFileDownload, intent.LabelFileRead},
		intent.LabelDocumentGenerate: {intent.LabelSearch, intent.LabelWebFetch, intent.LabelFileDownload, intent.LabelFileRead},
		intent.LabelSearch:           {intent.LabelWebFetch, intent.LabelSearch},
		intent.LabelLiveData:         {intent.LabelWebFetch, intent.LabelSearch},
		intent.LabelWebFetch:         {intent.LabelWebFetch, intent.LabelSearch},
		intent.LabelFileRead:         {intent.LabelFileRead, intent.LabelFileWrite},
		intent.LabelFileWrite:        {intent.LabelFileRead, intent.LabelFileWrite},
		intent.LabelDocumentRead:     {intent.LabelFileRead, intent.LabelFileWrite},
		intent.LabelShellCommand:     {intent.LabelFileRead, intent.LabelFileWrite},
		intent.LabelDelegateTask:     {intent.LabelFileRead, intent.LabelFileWrite},
		intent.LabelBrowser:          {intent.LabelScreenshot, intent.LabelAppLaunch},
		intent.LabelComputerUse:      {intent.LabelScreenshot, intent.LabelAppLaunch},
	}
}

// WithArchetypeBundleKeyOverride records the bundle key a published plan was
// built with so a later petition re-plan cannot swap archetypes.
func WithArchetypeBundleKeyOverride(ctx context.Context, key intent.IntentLabel) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(string(key)) == "" {
		return ctx
	}
	return context.WithValue(ctx, archetypeBundleKeyOverrideKey{}, key)
}

// ArchetypeBundleKey picks the archetype whose bundle the turn expands with.
func ArchetypeBundleKey(result intent.ClassificationResult) intent.IntentLabel {
	switch result.Primary {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelWebFetch:
		if classificationHasIntentLabel(result, intent.LabelOffice) {
			return intent.LabelOffice
		}
		if classificationHasIntentLabel(result, intent.LabelDocumentGenerate) {
			return intent.LabelDocumentGenerate
		}
	}
	return result.Primary
}

// ArchetypeBundleKeyFor returns a petition-stable override when present.
func ArchetypeBundleKeyFor(ctx context.Context, result intent.ClassificationResult) intent.IntentLabel {
	if ctx != nil {
		if override, ok := ctx.Value(archetypeBundleKeyOverrideKey{}).(intent.IntentLabel); ok && strings.TrimSpace(string(override)) != "" {
			return override
		}
	}
	return ArchetypeBundleKey(result)
}

func classificationHasIntentLabel(result intent.ClassificationResult, label intent.IntentLabel) bool {
	return result.HasLabel(label)
}

// BaselineWorkspaceLabels are last-resort workspace tools kept on every
// managed turn. When a specialized provider is missing, exhausted, or never
// classified, the agent can still read/write files and run a local command
// instead of stalling on a spent petition budget.
func BaselineWorkspaceLabels() []intent.IntentLabel {
	return []intent.IntentLabel{intent.LabelFileRead, intent.LabelFileWrite, intent.LabelShellCommand}
}

const baselineWorkspaceEvidence = "intent:baseline_workspace"
const archetypeBundleEvidence = "intent:archetype_bundle"

// localFileWriteRepeatFloor is how many fs.write.local nodes a turn that is
// itself editing a file publishes up front. It is the same floor the baseline
// bundle already uses. It is not a usage cap: a later call can still append a
// sibling. A one-shot file_write rule, or a slim document turn that skips the
// rest of the baseline bundle, would otherwise leave a single node.
const localFileWriteRepeatFloor = 8

// ExpandArchetypeBundleNeeds adds optional companion needs for the turn's
// archetype. Delivery legs are never offered here: they stay on the producing
// label's own rule and unlock through the plan DAG.
func ExpandArchetypeBundleNeeds(registry *coretool.CapabilityRegistry, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate, result intent.ClassificationResult, managed bool, needs []coretool.CapabilityNeed, bundleKey intent.IntentLabel) []coretool.CapabilityNeed {
	companions, ok := SemanticArchetypeBundles()[bundleKey]
	if !ok || len(companions) == 0 {
		return needs
	}
	return expandCompanionLabelNeeds(registry, rules, result, managed, needs, companions, archetypeBundleEvidence, bundleKey)
}

// ExpandBaselineWorkspaceNeeds keeps file-read, file-write, and local shell
// on a managed turn as optional, policy-omittable fallbacks. A capability the
// primary already offered below the floor is raised to that floor. Baseline
// still does not add a second family, so a search turn only gains the tools
// it was missing.
func ExpandBaselineWorkspaceNeeds(registry *coretool.CapabilityRegistry, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate, result intent.ClassificationResult, managed bool, needs []coretool.CapabilityNeed) []coretool.CapabilityNeed {
	if !managed {
		return needs
	}
	templates := make([]IntentCapabilityNeedTemplate, 0, 3)
	for _, label := range BaselineWorkspaceLabels() {
		for _, template := range rules[label] {
			if strings.HasPrefix(string(template.Capability), "artifact.deliver.") {
				continue
			}
			template.Required = false
			switch template.Capability {
			case coretool.CapabilityFSReadLocal:
				if template.MaxInvocations < 12 {
					template.MaxInvocations = 12
				}
			case coretool.CapabilityFSWriteLocal:
				if template.MaxInvocations < localFileWriteRepeatFloor {
					template.MaxInvocations = localFileWriteRepeatFloor
				}
			case coretool.CapabilityShellExecuteLocal:
				if template.MaxInvocations < 8 {
					template.MaxInvocations = 8
				}
			}
			templates = append(templates, template)
		}
	}
	if len(templates) == 0 {
		return needs
	}
	synthetic := map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
		intent.LabelFileRead: templates,
	}
	return expandCompanionLabelNeeds(registry, synthetic, result, managed, needs, []intent.IntentLabel{intent.LabelFileRead}, baselineWorkspaceEvidence, "")
}

// RaiseExistingLocalFileWriteFloor extends an fs.write.local family the turn
// already declared up to localFileWriteRepeatFloor. It does not add a read,
// a shell, or a second write family. A slim document continuation keeps only
// the file it is editing; that file is still iterative. Production 2026-10-04
// planned one write for "继续" on a bibliography and then told the model
// write_file had reached this turn's usage limit.
func RaiseExistingLocalFileWriteFloor(needs []coretool.CapabilityNeed, confidence float64) []coretool.CapabilityNeed {
	if len(needs) == 0 {
		return needs
	}
	start, count := -1, 0
	ambiguous := false
	for index, need := range needs {
		if need.Capability != coretool.CapabilityFSWriteLocal {
			continue
		}
		if start < 0 {
			start = index
			count = 1
			continue
		}
		if coretool.RepeatFamilyID(needs[start].ID) != coretool.RepeatFamilyID(need.ID) {
			ambiguous = true
		}
		count++
	}
	if start < 0 || ambiguous || count >= localFileWriteRepeatFloor {
		return needs
	}
	extra := coretool.ExtendRepeatFamily(needs[start], count, localFileWriteRepeatFloor, confidence, needs[start].EvidenceIDs)
	if len(extra) == 0 {
		return needs
	}
	out := make([]coretool.CapabilityNeed, len(needs), len(needs)+len(extra))
	copy(out, needs)
	return append(out, extra...)
}

func expandCompanionLabelNeeds(registry *coretool.CapabilityRegistry, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate, result intent.ClassificationResult, managed bool, needs []coretool.CapabilityNeed, companions []intent.IntentLabel, evidence string, bundleKey intent.IntentLabel) []coretool.CapabilityNeed {
	if !managed || len(companions) == 0 {
		return needs
	}
	type familyRange struct {
		start, count int
		ambiguous    bool
	}
	offered := make(map[coretool.CapabilityID]familyRange, len(needs))
	for index, need := range needs {
		family := coretool.RepeatFamilyID(need.ID)
		entry, exists := offered[need.Capability]
		if !exists {
			offered[need.Capability] = familyRange{start: index, count: 1}
			continue
		}
		if coretool.RepeatFamilyID(needs[entry.start].ID) != family {
			entry.ambiguous = true
		}
		entry.count++
		offered[need.Capability] = entry
	}
	out := needs
	addedArchetypeFamilies := 0
	for _, label := range companions {
		for _, template := range rules[label] {
			if strings.HasPrefix(string(template.Capability), "artifact.deliver.") {
				continue
			}
			budget := coretool.RepeatSiblingBudget(template.MaxInvocations)
			if entry, exists := offered[template.Capability]; exists {
				// The baseline floor raises a one-shot family that is already
				// on the turn. Skipping it left a paper rewrite with a single
				// read and a single write: the template was already on disk,
				// the second file (a .bib) was refused, and the model was told
				// the earlier write still stood.
				if entry.ambiguous || budget <= entry.count {
					continue
				}
				out = append(out, coretool.ExtendRepeatFamily(out[entry.start], entry.count, budget, result.Confidence, []string{evidence})...)
				entry.count = budget
				offered[template.Capability] = entry
				continue
			}
			if registry == nil {
				continue
			}
			if _, exists := registry.Lookup(template.Capability); !exists {
				continue
			}
			// Search, fetch, download, and screenshot stay off the first
			// surface unless the turn already declared that capability. A
			// budget upgrade of a declared family still happens above.
			if evidence == archetypeBundleEvidence && latentArchetypeCapability(template.Capability, bundleKey) {
				continue
			}
			if evidence == archetypeBundleEvidence && addedArchetypeFamilies >= maxArchetypeCompanionFamilies {
				continue
			}
			idPrefix := "need:"
			if evidence == baselineWorkspaceEvidence {
				idPrefix = "need:zz-baseline:"
			}
			siblings := ExpandNeedTemplateSiblings(template, ExpandNeedTemplateOptions{
				IDPrefix:      idPrefix,
				Confidence:    result.Confidence,
				EvidenceIDs:   []string{evidence},
				ForceOptional: true,
			})
			offered[template.Capability] = familyRange{start: len(out), count: len(siblings)}
			out = append(out, siblings...)
			if evidence == archetypeBundleEvidence {
				addedArchetypeFamilies++
			}
		}
	}
	return out
}

const maxArchetypeCompanionFamilies = 2

// latentArchetypeCapability is a companion the bundle may raise a ceiling for
// when the turn already declared it, but must not add to a cold surface.
func latentArchetypeCapability(id coretool.CapabilityID, bundleKey intent.IntentLabel) bool {
	switch id {
	case CapabilityInformationSearchWeb, coretool.CapabilityInformationFetchWeb, CapabilityVisualCapture:
		return true
	case coretool.CapabilityArtifactAcquireRemote:
		return bundleKey != intent.LabelOffice && bundleKey != intent.LabelDocumentGenerate
	default:
		return false
	}
}
