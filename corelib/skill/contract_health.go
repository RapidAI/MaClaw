package skill

import "github.com/RapidAI/CodeClaw/corelib"

// HasIncompleteSkillContract reports executable skills whose step templates use
// parameters that are not fully represented by params/required_args metadata.
func HasIncompleteSkillContract(skillType string, steps []corelib.NLSkillStep, params []corelib.NLSkillParam, requiredArgs []string) bool {
	if len(steps) == 0 || IsKnowledgeSkillType(skillType) || IsInstructionOnlySkillType(skillType) {
		return false
	}
	completed := CompleteParamsForRunner(params, steps, requiredArgs)
	return len(completed) > len(params)
}
