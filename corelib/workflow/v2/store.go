package v2

// WorkflowStore persists workflow state.
// Production uses SQLite; tests use MemoryStore.
type WorkflowStore interface {
	Save(state *WorkflowState) error
	Load(userID string) (*WorkflowState, error)
	Delete(userID string) error
}
