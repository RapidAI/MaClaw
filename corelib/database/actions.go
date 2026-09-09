package database

import "strings"

// IsWriteAction reports whether a database tool action mutates data or
// exports it across a trust boundary. Query/inspect stay read-only; hosts
// use this to split light-prompt and workflow-phase risk from the single
// tool name.
func IsWriteAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "execute", "batch_execute", "write_table", "export_excel", "propose_profile":
		return true
	default:
		return false
	}
}

// IsReadAction is the complementary projection used by light turns and
// doc/planning workflow phases.
func IsReadAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", "list_connections", "connect", "disconnect", "inspect", "query", "read_table", "job_status", "explain", "list_favorites", "save_favorite", "delete_favorite":
		return true
	default:
		return false
	}
}

// ToolDescriptionReadOnly is the LLM-facing description of the read-only
// database_query projection. Hosts must not maintain a second copy.
func ToolDescriptionReadOnly() string {
	return "只读查看库、查看数据库、列出库/schema/表、查看表结构：list_connections、connect、disconnect、inspect、query、read_table、job_status、explain、list_favorites、save_favorite、delete_favorite。必须调用本工具，禁止用 bash/mysql/psql 传密码，也不要当成 Git 仓库。写入、批处理和导出请使用 database 工具。"
}

// RefuseWriteForReadOnlyTool rejects mutating actions on the database_query
// surface. The same handler implementation serves both tools; this is the
// host-neutral write gate.
func RefuseWriteForReadOnlyTool(args map[string]interface{}) (string, bool) {
	action := ""
	if args != nil {
		if v, ok := args["action"].(string); ok {
			action = v
		}
	}
	if !IsWriteAction(action) {
		return "", false
	}
	return "Error: permission: database_query is read-only; use the database tool for execute, batch_execute, write_table, or export_excel", true
}
