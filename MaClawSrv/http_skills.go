package main

import (
	"context"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"net/http"
	"strings"
)

func (s *HTTPServer) handleListSkills(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ListSkills(r.Context(), p)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parseSkillPageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateSkills(sanitizeSkillEntriesForAPI(s.svc.DataRoot(), out), page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleGetSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetSkill(r.Context(), p, r.PathValue("skillName"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeSkillEntryPtrForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleDeleteSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := s.svc.DeleteSkill(r.Context(), p, r.PathValue("skillName")); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *HTTPServer) handleSearchSkills(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SkillSearchInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.SearchSkills(r.Context(), p, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": sanitizeSkillSearchResultsForAPI(s.svc.DataRoot(), out)})
}

func (s *HTTPServer) handleInstallSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SkillInstallInput
	if !decodeJSON(w, r, &in) {
		return
	}
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		job, admissionErr := s.admitUserJob(r, p, "skill.install", agentruntime.JobRecoveryPolicyReconcile, agentruntime.JobRetryPolicy{}, map[string]any{"source": in.Source, "repo_url": in.RepoURL, "raw_url": in.RawURL, "repo_full_name": in.RepoFullName, "file_path": in.FilePath, "branch": in.Branch, "definition_type": in.DefinitionType, "skill_hub_url": in.SkillHubURL, "skill_market_url": in.SkillMarketURL, "skill_id": in.SkillID, "zip_digest": shortSkillArchiveDigest(in.ZipBase64), "overwrite": in.Overwrite}, func(ctx context.Context) (any, error) {
			return executeRecordedJobEffect(ctx, "skill.install", map[string]any{"operation": "install", "source": in.Source, "skill_id": in.SkillID}, false, func(ctx context.Context) (any, string, error) {
				out, err := s.svc.InstallSkill(ctx, p, in)
				if err != nil {
					return nil, "", err
				}
				result := map[string]any{"items": sanitizeSkillEntriesForAPI(s.svc.DataRoot(), out)}
				names := make([]string, 0, len(out))
				for _, item := range out {
					names = append(names, item.Name)
				}
				return result, skillEffectResourceID(names), nil
			})
		})
		if admissionErr != nil {
			writeAsyncJobAdmissionError(w, admissionErr)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.InstallSkill(r.Context(), p, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"items": sanitizeSkillEntriesForAPI(s.svc.DataRoot(), out)})
}

func (s *HTTPServer) handleImportSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SkillImportInput
	if !decodeJSON(w, r, &in) {
		return
	}
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		job, admissionErr := s.admitUserJob(r, p, "skill.import", agentruntime.JobRecoveryPolicyReconcile, agentruntime.JobRetryPolicy{}, map[string]any{"archive_digest": shortSkillArchiveDigest(in.ZipBase64), "overwrite": in.Overwrite}, func(ctx context.Context) (any, error) {
			return executeRecordedJobEffect(ctx, "skill.import", map[string]any{"operation": "import", "overwrite": in.Overwrite}, false, func(ctx context.Context) (any, string, error) {
				out, err := s.svc.ImportSkillArchive(ctx, p, in)
				if err != nil {
					return nil, "", err
				}
				result := map[string]any{"items": sanitizeSkillEntriesForAPI(s.svc.DataRoot(), out)}
				names := make([]string, 0, len(out))
				for _, item := range out {
					names = append(names, item.Name)
				}
				return result, skillEffectResourceID(names), nil
			})
		})
		if admissionErr != nil {
			writeAsyncJobAdmissionError(w, admissionErr)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.ImportSkillArchive(r.Context(), p, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"items": sanitizeSkillEntriesForAPI(s.svc.DataRoot(), out)})
}

func (s *HTTPServer) handleExportSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ExportSkill(r.Context(), p, r.PathValue("skillName"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleValidateSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ValidateSkill(r.Context(), p, r.PathValue("skillName"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeSkillValidateResultForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleImproveSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SkillImproveInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ImproveSkill(r.Context(), p, r.PathValue("skillName"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeSkillImproveResultForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleUploadSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SkillUploadInput
	if !decodeJSON(w, r, &in) {
		return
	}
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		name := r.PathValue("skillName")
		job, admissionErr := s.admitUserJob(r, p, "skill.upload", agentruntime.JobRecoveryPolicyReconcile, agentruntime.JobRetryPolicy{}, map[string]any{"skill_name": name, "skill_market_url": in.SkillMarketURL, "email": in.Email}, func(ctx context.Context) (any, error) {
			return executeRecordedJobEffect(ctx, "skill.upload", map[string]any{"operation": "upload", "skill_name": name, "skill_market_url": in.SkillMarketURL}, true, func(ctx context.Context) (any, string, error) {
				out, err := s.svc.UploadSkill(ctx, p, name, in)
				if out == nil {
					return nil, name, err
				}
				result := sanitizeSkillUploadResultForAPI(s.svc.DataRoot(), out)
				if err != nil {
					return result, out.SubmissionID, err
				}
				return result, out.SubmissionID, nil
			})
		})
		if admissionErr != nil {
			writeAsyncJobAdmissionError(w, admissionErr)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.UploadSkill(r.Context(), p, r.PathValue("skillName"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeSkillUploadResultForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleGetSkillUploadStatus(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetSkillUploadStatus(r.Context(), p, r.PathValue("submissionId"), strings.TrimSpace(r.URL.Query().Get("base_url")))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeSkillSubmissionStatusForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleGetSkillMarketAccount(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetSkillMarketAccount(r.Context(), p, strings.TrimSpace(r.URL.Query().Get("base_url")), strings.TrimSpace(r.URL.Query().Get("email")))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
