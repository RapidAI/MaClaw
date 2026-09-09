package main

import (
	"errors"
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	coreconfig "github.com/RapidAI/CodeClaw/corelib/config"
	"net/http"
)

func (s *HTTPServer) handleGetConfigSchema(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetParameterDefinitions(r.Context(), p)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	out = filterUserConfigSchema(out)
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": coreconfig.AppConfigSchemaVersion, "items": out})
}

func (s *HTTPServer) handleGetConfig(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetUserConfig(r.Context(), p)
	if err != nil {
		if errors.Is(err, agentservice.ErrUserConfigNotFound) {
			writeUserConfigResponse(w, http.StatusOK, &agentservice.UserConfig{TenantID: p.TenantID, UserID: p.UserID, AppConfig: forceSrvAIAutoEnabledConfig(corelib.AppConfigDefaults())})
			return
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeUserConfigResponse(w, http.StatusOK, out)
}

func (s *HTTPServer) handleUpdateConfig(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	// Accept both raw AppConfig JSON and {"app_config": {...}} envelope format.
	inPtr, ok := decodeOptionalAppConfig(w, r)
	if !ok {
		return
	}
	if inPtr == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty or invalid config body"})
		return
	}
	next, err := s.userVisibleConfigUpdate(r.Context(), p, *inPtr)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	next = forceSrvAIAutoEnabledConfig(next)
	if err := s.validateThirdPartyGatewayTokenUnique(r.Context(), p, next); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	before, _ := s.svc.GetRawUserConfig(r.Context(), p)
	out, err := s.svc.UpdateUserConfig(r.Context(), p, next)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	s.syncWeixinRuntimeFromRawConfig(r.Context(), p)
	s.syncIMRuntimeFromRawConfig(r.Context(), p)
	beforeCfg := corelib.AppConfig{}
	if before != nil {
		beforeCfg = before.AppConfig
	}
	after, _ := s.svc.GetRawUserConfig(r.Context(), p)
	afterCfg := next
	if after != nil {
		afterCfg = after.AppConfig
	}
	s.syncThirdPartyIMConfigTransition(p, beforeCfg, afterCfg)
	s.ensureConfiguredAIModelsAsync(out.AppConfig)
	writeUserConfigResponse(w, http.StatusOK, out)
}

func (s *HTTPServer) handleValidateConfig(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	candidate, ok := decodeOptionalAppConfig(w, r)
	if !ok {
		return
	}
	candidate, err := s.userVisibleConfigCandidate(r.Context(), p, candidate)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	out, err := s.svc.ValidateConfigCandidate(r.Context(), p, candidate)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleTestConfig(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	candidate, ok := decodeOptionalAppConfig(w, r)
	if !ok {
		return
	}
	candidate, err := s.userVisibleConfigCandidate(r.Context(), p, candidate)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	out, err := s.svc.TestConfigCandidate(r.Context(), p, candidate)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeConfigTestResultForAPI(s.svc.DataRoot(), out))
}
