package longhorizon

type HorizonProjection struct {
	TaskID       string   `json:"task_id"`
	OwnerID      string   `json:"owner_id,omitempty"`
	Status       string   `json:"status"`
	RoundIndex   int      `json:"round_index"`
	MaxRounds    int      `json:"max_rounds"`
	ManagerNext  NextStep `json:"manager_next,omitempty"`
	Completed    bool     `json:"completed"`
	EventScopeID string   `json:"event_scope_id,omitempty"`
	SessionKey   string   `json:"session_key,omitempty"`
}

func ProjectTaskState(state *TaskState) HorizonProjection {
	if state == nil {
		return HorizonProjection{}
	}
	return HorizonProjection{
		TaskID:       state.TaskID,
		OwnerID:      state.Policy.OwnerID,
		Status:       state.Status,
		RoundIndex:   state.RoundIndex,
		MaxRounds:    ClampMaxRounds(state.MaxRounds),
		ManagerNext:  state.ManagerNext,
		Completed:    state.Completed,
		EventScopeID: state.Policy.EventScopeID,
		SessionKey:   state.Policy.OwnerID,
	}
}
