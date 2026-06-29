package v2

// WorkflowStore persists workflow state.
// Production uses SQLite; tests use MemoryStore.
type WorkflowStore interface {
	Save(state *WorkflowState) error
	Load(userID string) (*WorkflowState, error)
	Delete(userID string) error
	// ListAllUserIDs returns all user IDs that have stored workflow state.
	// Used by startup cleanup to cancel stale workflows across all users/tabs.
	ListAllUserIDs() ([]string, error)
}
