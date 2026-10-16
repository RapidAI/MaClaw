package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticSelectionProvenanceNote names the implementation that answered a
// dynamic selection.
//
// A managed surface deliberately hides implementation names from the model: a
// dynamic adapter renders as its capability's stable prompt spelling (or an
// opaque grant token) so prompt, history and the live tool list agree. The
// cost of that choice is paid at exactly two moments — when the user asks for
// an implementation by name, and when they want to know which one produced an
// answer. Without a statement of origin the model is left to guess, and the
// guess it produces is the expensive one: it reports the installed MCP as
// missing while the answer in front of it came from that MCP.
//
// The note is appended to the model-visible result only for dynamic
// providers. A builtin answer needs no provenance — there is exactly one
// implementation and the user already knows which — and repeating it would
// spend context on the common case.
func semanticSelectionProvenanceNote(selection tool.PlannedSelection) string {
	kind := strings.TrimSpace(selection.Provider.Kind)
	providerID := strings.TrimSpace(selection.Provider.ProviderID)
	implementationID := strings.TrimSpace(selection.Provider.ImplementationID)
	switch kind {
	case "mcp":
		subject := providerID
		if implementationID != "" {
			subject += "/" + implementationID
		}
		return "\n[来源：已安装的 MCP 服务器 " + subject + "]"
	case "skill":
		return "\n[来源：已安装的 Skill " + providerID + "]"
	default:
		return ""
	}
}

// withSemanticSelectionProvenance appends the origin statement to a successful
// dynamic result. Failure paths keep the host's rejection text untouched: a
// note naming the implementation that just failed would read as an attribution
// the turn never earned.
func withSemanticSelectionProvenance(result string, selection tool.PlannedSelection) string {
	note := semanticSelectionProvenanceNote(selection)
	if note == "" {
		return result
	}
	return result + note
}
