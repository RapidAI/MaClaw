package skill

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// AppendRunParamPlaceholders appends a skill parameter schema to a command
// template as placeholders. It is used when a generated script exposes
// argv/argparse parameters but the base command only launches the script.
func AppendRunParamPlaceholders(command string, params []corelib.NLSkillParam) string {
	command = strings.TrimSpace(command)
	if command == "" || len(params) == 0 {
		return command
	}
	seen := map[string]bool{}
	var parts []string
	for _, param := range params {
		name := strings.TrimSpace(param.Name)
		key := canonicalRunVarKey(name)
		if name == "" || key == "" || seen[key] || CommandReferencesParam(command, name) {
			continue
		}
		seen[key] = true
		placeholder := "{{" + name + "}}"
		if flag := strings.TrimSpace(param.CLIFlag); flag != "" {
			if strings.HasSuffix(flag, "=") || strings.HasSuffix(flag, ":") {
				parts = append(parts, flag+placeholder)
			} else {
				parts = append(parts, flag, placeholder)
			}
			continue
		}
		parts = append(parts, placeholder)
	}
	if len(parts) == 0 {
		return command
	}
	return command + " " + strings.Join(parts, " ")
}
