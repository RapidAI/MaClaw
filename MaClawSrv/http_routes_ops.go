package main

func (s *HTTPServer) registerOpsRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /livez", s.handleLive)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.HandleFunc("GET /version", s.handleVersion)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)
	s.mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
	s.mux.HandleFunc("GET /api/v1/openapi.json", s.handleOpenAPI)
	s.mux.HandleFunc("GET /admin", s.handleAdminWeb)
	s.mux.HandleFunc("GET /admin/", s.handleAdminWeb)
	s.mux.HandleFunc("GET /app", s.handleUserWeb)
	s.mux.HandleFunc("GET /app/", s.handleUserWeb)
	s.mux.HandleFunc("POST /api/v1/web/refresh", s.handleWebAccessTokenRefresh)
	s.mux.HandleFunc("GET /api/im-gateway/v1/health", s.handleThirdPartyGatewayHealth)
}
