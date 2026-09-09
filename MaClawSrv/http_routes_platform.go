package main

func (s *HTTPServer) registerPlatformRoutes() {
	s.mux.HandleFunc("GET /api/platform/runtime/report", s.withPlatformAdmin(s.handlePlatformRuntimeReport))
	s.mux.HandleFunc("POST /api/platform/virtual-employees", s.withPlatformAdmin(s.handlePlatformCreateVirtualEmployee))
	s.mux.HandleFunc("POST /api/platform/virtual-employees/{employeeId}/config", s.withPlatformAdmin(s.handlePlatformUpdateVirtualEmployeeConfig))
	s.mux.HandleFunc("DELETE /api/platform/virtual-employees/{employeeId}", s.withPlatformAdmin(s.handlePlatformDeleteVirtualEmployee))
	s.mux.HandleFunc("POST /api/runtime/virtual-employees/{employeeId}/discussion-messages", s.withPlatformAdmin(s.handleRuntimeVirtualEmployeeDiscussionMessage))
	s.mux.HandleFunc("POST /api/platform/source-users/runtime-status", s.withPlatformAdmin(s.handlePlatformSourceUsersRuntimeStatus))
	s.mux.HandleFunc("GET /api/platform/source-users/{sourceUserId}/runtime-status", s.withPlatformAdmin(s.handlePlatformSourceUserRuntimeStatus))
	s.mux.HandleFunc("GET /api/platform/source-users/{sourceUserId}/assistant-instances", s.withPlatformAdmin(s.handlePlatformSourceUserAssistantInstances))
	s.mux.HandleFunc("POST /api/platform/source-users/{sourceUserId}/assistant-instances", s.withPlatformAdmin(s.handlePlatformCreateSourceUserAssistantInstance))
	s.mux.HandleFunc("POST /api/platform/source-users/{sourceUserId}/assistant-link", s.withPlatformAdmin(s.handlePlatformSourceUserAssistantLink))
	s.mux.HandleFunc("POST /api/platform/source-users/{sourceUserId}/knowledge-link", s.withPlatformAdmin(s.handlePlatformSourceUserKnowledgeLink))
	s.mux.HandleFunc("POST /api/platform/source-users/{sourceUserId}/settings-link", s.withPlatformAdmin(s.handlePlatformSourceUserSettingsLink))
	s.mux.HandleFunc("POST /api/platform/virtual-employees/{employeeId}/knowledge/imports", s.withPlatformAdmin(s.handlePlatformKnowledgeImport))
	s.mux.HandleFunc("POST /api/platform/virtual-employees/{employeeId}/migrations/imports", s.withPlatformAdmin(s.handlePlatformMigrationImport))
	s.mux.HandleFunc("POST /api/platform/sync/jobs/{jobId}/run", s.withPlatformAdmin(s.handlePlatformSyncJobRun))
	s.mux.HandleFunc("POST /api/platform/sync/conflicts/{conflictId}/resolve", s.withPlatformAdmin(s.handlePlatformSyncConflictResolve))
}
