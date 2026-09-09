package database

import "context"

// Handle executes the shared database tool contract. It is retained as the
// short, source-compatible entry point for hosts that used the original
// database package API; HandleTool contains the single authoritative
// implementation and validation path.
func Handle(ctx context.Context, manager *Manager, args map[string]interface{}) string {
	return HandleTool(ctx, manager, args)
}
