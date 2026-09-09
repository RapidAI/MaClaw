package agentservice

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// PetitionLookupPreference is the deterministic preference order when one
// capability is backed by several rule labels. information.search.web is the
// sole required template of both search and live_data; search wins so a
// petition for web_search expands the reference-freshness family, not live_data.
var PetitionLookupPreference = []intent.IntentLabel{intent.LabelSearch, intent.LabelLiveData, intent.LabelWebFetch}

// PetitionPreferredLabel picks one candidate: lookup preference first, then
// lexicographic label name. Empty input yields false.
func PetitionPreferredLabel(candidates []intent.IntentLabel) (intent.IntentLabel, bool) {
	if len(candidates) == 0 {
		return "", false
	}
	for _, preferred := range PetitionLookupPreference {
		for _, candidate := range candidates {
			if candidate == preferred {
				return candidate, true
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	return candidates[0], true
}

// PetitionLabelForCapability resolves the intent label a petition for the
// capability may add, derived from the owner-reviewed rule table rather than
// the turn's classification. A label whose single required template is exactly
// this capability wins; otherwise any label that mentions it qualifies. Ties
// break by PetitionPreferredLabel. GUI IM petition and any host that expands
// from the same table share this so map iteration cannot pick a second label.
func PetitionLabelForCapability(capability coretool.CapabilityID, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate) (intent.IntentLabel, bool) {
	var sole, mentioned []intent.IntentLabel
	for label, templates := range rules {
		required := 0
		requiredMatch := false
		contains := false
		for _, template := range templates {
			if template.Required {
				required++
			}
			if template.Capability == capability {
				contains = true
				if template.Required {
					requiredMatch = true
				}
			}
		}
		if required == 1 && requiredMatch {
			sole = append(sole, label)
		} else if contains {
			mentioned = append(mentioned, label)
		}
	}
	if label, ok := PetitionPreferredLabel(sole); ok {
		return label, true
	}
	return PetitionPreferredLabel(mentioned)
}

// ValidatePetitionExpansion is the strict-superset whitelist for a petitioned
// child revision: every parent selection must survive with unchanged authority
// (a refreshed provider binding is allowed), and every added selection must be
// one of the petitioned label's rule template needs. GUI IM petition and any
// host that expands from the same reviewed templates share this so a child
// cannot grow a need the parent did not already own or the label does not
// declare.
func ValidatePetitionExpansion(parent, child coretool.ToolPlan, templates []IntentCapabilityNeedTemplate) error {
	if strings.TrimSpace(parent.RootTaskID) == "" || parent.RootTaskID != child.RootTaskID {
		return fmt.Errorf("semantic petition expansion root task mismatch")
	}
	if len(child.Unmet) > 0 {
		return fmt.Errorf("semantic petition expansion has unmet needs")
	}
	if len(templates) == 0 {
		return fmt.Errorf("semantic petition label has no rule template")
	}
	parentByNeed, ok := coretool.SelectionsByNeed(parent.Selections)
	if !ok {
		return fmt.Errorf("semantic petition parent needs are not identifiable")
	}
	childByNeed, ok := coretool.SelectionsByNeed(child.Selections)
	if !ok {
		return fmt.Errorf("semantic petition child needs are not identifiable")
	}
	for needID, selection := range parentByNeed {
		replacement, ok := childByNeed[needID]
		if !ok || !coretool.SelectionAuthorityEqualIgnoringProvider(selection, replacement) {
			return fmt.Errorf("semantic petition expansion alters parent authority")
		}
	}
	added := 0
	for needID, selection := range childByNeed {
		if _, ok := parentByNeed[needID]; ok {
			continue
		}
		matched := false
		for _, template := range templates {
			if selection.FitProof.MatchedCapability == template.Capability && coretool.QualifiersEqual(selection.FitProof.QualifierBindings, template.Qualifiers) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("semantic petition expansion adds a need outside the petitioned label")
		}
		added++
	}
	if added == 0 {
		return fmt.Errorf("semantic petition expansion added no governed need")
	}
	return nil
}
