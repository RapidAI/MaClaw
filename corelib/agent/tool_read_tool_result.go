package agent

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

// ToolReadToolResult re-reads a spilled tool_result handle with an optional byte
// offset/limit. Hosts inject session_key authoritatively; path remains accepted
// only for legacy internal callers and is not exposed in model tool schemas.
func ToolReadToolResult(args map[string]interface{}) string {
	id := toolresult.ParseArgsString(args, "id")
	if id == "" {
		id = toolresult.ParseArgsString(args, "handle_id")
	}
	path := toolresult.ParseArgsString(args, "path")
	session := toolresult.ParseArgsString(args, "session_key")
	if session == "" {
		session = toolresult.ParseArgsString(args, "session")
	}
	if id == "" && path == "" {
		return "error: read_tool_result requires id (from [tool_result_handle]) or path"
	}
	// A present offset that is not an integer must not become the first page.
	// The warning text names the field next_offset, and models copy those
	// words into the argument. That used to read offset 0 again and then
	// trip the repeat stop before the real next page was requested.
	if toolresult.InvalidArgsInt(args, "offset") {
		return "error: offset must be an integer byte index. Copy the next_offset number from the previous result."
	}
	offset := toolresult.ParseArgsInt(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	// Missing limit is 0. ModelReadLimit turns that, and any sub-page limit,
	// into one full page. The raw request is kept so the result can say the
	// page size changed; otherwise the model keeps sending limit=1.
	requested := toolresult.ParseArgsInt(args, "limit", 0)
	limit := toolresult.ModelReadLimit(requested)
	res, err := toolresult.Read(toolresult.ReadOptions{
		ID:         id,
		Path:       path,
		SessionKey: session,
		Offset:     offset,
		Limit:      limit,
	})
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return annotateAdjustedReadLimit(requested, limit, toolresult.FormatReadResult(res))
}

// annotateAdjustedReadLimit tells the model the page it received is not the
// limit it asked for. A one-byte request used to come back truncated with
// "call again", and the model repeated limit=1, offset=0.
func annotateAdjustedReadLimit(requested, applied int, body string) string {
	if requested <= 0 || requested == applied {
		return body
	}
	return fmt.Sprintf("limit_applied: %d\nnote: requested limit %d is not one page; the page size is %d bytes. If truncated is false, stop. If truncated is true, set offset to the integer on the next_offset line and omit limit.\n%s",
		applied, requested, applied, body)
}
