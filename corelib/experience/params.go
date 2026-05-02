package experience

import (
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func synthesizeSkillParams(steps []corelib.NLSkillStep, args []string) []corelib.NLSkillParam {
	params := coreskill.SynthesizeParams(steps, args)
	if len(params) == 0 && len(args) > 0 {
		params = make([]corelib.NLSkillParam, 0, len(args))
		for _, arg := range args {
			arg = strings.TrimSpace(arg)
			if arg == "" {
				continue
			}
			params = append(params, corelib.NLSkillParam{Name: arg, Required: true, Synthetic: true})
		}
	}
	for i := range params {
		params[i].Description = describeRequiredArg(params[i].Name)
		params[i].Aliases = mergeAliases(params[i].Aliases, learnedParamAliases(params[i].Name))
	}
	sort.Slice(params, func(i, j int) bool { return params[i].Name < params[j].Name })
	return params
}

func mergeAliases(existing []string, extra []string) []string {
	if len(existing) == 0 && len(extra) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(existing)+len(extra))
	for _, alias := range append(existing, extra...) {
		alias = strings.TrimSpace(alias)
		if alias == "" || seen[alias] {
			continue
		}
		seen[alias] = true
		out = append(out, alias)
	}
	return out
}

func learnedParamAliases(arg string) []string {
	switch arg {
	case "env":
		return []string{"environment", "target_env", "stage"}
	case "environment":
		return []string{"env", "target_env", "stage"}
	case "service":
		return []string{"service_name", "app", "component"}
	case "service_name":
		return []string{"service", "app", "component"}
	case projectPathArg:
		return []string{"project", "project_root", "repo", "repo_path", "workspace"}
	default:
		return nil
	}
}

func describeRequiredArg(arg string) string {
	switch arg {
	case projectPathArg:
		return "Project root path to run this learned workflow against."
	case "env", "environment":
		return "Target environment for this learned workflow."
	case "service", "service_name":
		return "Service name used by this learned workflow."
	case "input", "input_path":
		return "Input path or value for this learned workflow."
	case "output", "output_path":
		return "Output path or value for this learned workflow."
	default:
		return "Required value for this learned workflow."
	}
}
