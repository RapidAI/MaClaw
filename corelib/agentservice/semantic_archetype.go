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

// ExpandArchetypeBundleNeeds adds optional companion needs for the turn's
// archetype. Delivery legs are never offered here: they stay on the producing
// label's own rule and unlock through the plan DAG.
func ExpandArchetypeBundleNeeds(registry *coretool.CapabilityRegistry, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate, result intent.ClassificationResult, managed bool, needs []coretool.CapabilityNeed, bundleKey intent.IntentLabel) []coretool.CapabilityNeed {
	companions, ok := SemanticArchetypeBundles()[bundleKey]
	if !ok || len(companions) == 0 {
		return needs
	}
	return expandCompanionLabelNeeds(registry, rules, result, managed, needs, companions, archetypeBundleEvidence)
}

// ExpandBaselineWorkspaceNeeds keeps file-read, file-write, and local shell
// on a managed turn as optional, policy-omittable fallbacks. Capabilities
// already offered by the primary or archetype stay as they are; baseline
// only fills gaps so a search turn still has a last-resort command runner.
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
			case coretool.CapabilityFSWriteLocal, coretool.CapabilityShellExecuteLocal:
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
	return expandCompanionLabelNeeds(registry, synthetic, result, managed, needs, []intent.IntentLabel{intent.LabelFileRead}, baselineWorkspaceEvidence)
}

func expandCompanionLabelNeeds(registry *coretool.CapabilityRegistry, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate, result intent.ClassificationResult, managed bool, needs []coretool.CapabilityNeed, companions []intent.IntentLabel, evidence string) []coretool.CapabilityNeed {
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
	for _, label := range companions {
		for _, template := range rules[label] {
			if strings.HasPrefix(string(template.Capability), "artifact.deliver.") {
				continue
			}
			budget := coretool.RepeatSiblingBudget(template.MaxInvocations)
			if entry, exists := offered[template.Capability]; exists {
				if evidence == baselineWorkspaceEvidence {
					continue
				}
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
		}
	}
	return out
}
