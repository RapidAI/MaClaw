package agent

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

// ToolReadToolResult re-reads a spilled tool_result handle (id or path) with
// optional byte offset/limit. Used when large tool outputs were projected to a
// preview + handle footer.
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
	offset := toolresult.ParseArgsInt(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	limit := toolresult.ParseArgsInt(args, "limit", toolresult.DefaultReadLimit)
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
	return toolresult.FormatReadResult(res)
}
