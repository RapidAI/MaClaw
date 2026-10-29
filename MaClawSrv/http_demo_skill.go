package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/demoskill"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// POST /api/v1/skills/demo/distill — Hub sends a recorded timeline (and
// optional screenshot base64 map). MaClawSrv captions shots then installs
// the distilled demo skill.
func (s *HTTPServer) handleDistillDemoSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.DemoDistillInput
	if !decodeJSON(w, r, &in) {
		return
	}
	entry, err := s.runDemoDistill(r.Context(), p, in.Timeline, in.Shots, in.Notes)
	if err != nil {
		writeDemoSkillError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": sanitizeSkillEntriesForAPI(s.svc.DataRoot(), []corelib.NLSkillEntry{entry})[0]})
}

// POST /api/v1/skills/demo/redistill
func (s *HTTPServer) handleRedistillDemoSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.DemoRedistillInput
	if !decodeJSON(w, r, &in) {
		return
	}
	llmFn, vision, ocr, err := s.demoSkillModelFns(r.Context(), p)
	if err != nil {
		writeDemoSkillError(w, err, s.svc.DataRoot())
		return
	}
	entry, err := s.svc.RedistillDemoSkill(r.Context(), p, llmFn, in, vision, ocr)
	if err != nil {
		writeDemoSkillError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": sanitizeSkillEntriesForAPI(s.svc.DataRoot(), []corelib.NLSkillEntry{entry})[0]})
}

// PUT /api/v1/skills/{skillName}/demo — edit page saves guidance text.
func (s *HTTPServer) handleUpdateDemoSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.DemoSkillUpdateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	entry, err := s.svc.UpdateDemoSkillContent(r.Context(), p, r.PathValue("skillName"), in)
	if err != nil {
		writeDemoSkillError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": sanitizeSkillEntriesForAPI(s.svc.DataRoot(), []corelib.NLSkillEntry{entry})[0]})
}

func (s *HTTPServer) runDemoDistill(ctx context.Context, p agentservice.Principal, tl demoskill.Timeline, shotsB64 map[string]string, notes string) (corelib.NLSkillEntry, error) {
	llmFn, vision, ocr, err := s.demoSkillModelFns(ctx, p)
	if err != nil {
		return corelib.NLSkillEntry{}, err
	}
	shots := map[string][]byte{}
	for ref, b64 := range shotsB64 {
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err == nil && len(data) > 0 {
			shots[ref] = data
		}
	}
	return s.svc.DistillDemoSkillWithCaptions(ctx, p, llmFn, tl, shots, vision, ocr, notes)
}

func (s *HTTPServer) demoSkillModelFns(ctx context.Context, p agentservice.Principal) (demoskill.LLM, demoskill.VisionCaptioner, demoskill.OCRCaptioner, error) {
	cfg, err := s.svc.GetUserConfig(ctx, p)
	if err != nil || cfg == nil {
		return nil, nil, nil, fmt.Errorf("user config unavailable")
	}
	llmCfg, err := agentservice.ResolveLLMConfig(cfg.AppConfig)
	if err != nil {
		return nil, nil, nil, err
	}
	llmFn := func(c context.Context, system, user string) (string, error) {
		messages := []interface{}{
			map[string]interface{}{"role": "system", "content": system},
			map[string]interface{}{"role": "user", "content": user},
		}
		resp, err := demoSkillComplete(c, llmCfg, nil, messages)
		if err != nil {
			return "", err
		}
		if resp == nil || len(resp.Choices) == 0 {
			return "", fmt.Errorf("empty llm response")
		}
		return resp.Choices[0].Message.Content, nil
	}
	var vision demoskill.VisionCaptioner
	if llmCfg.SupportsVision {
		vision = func(c context.Context, png []byte) (string, error) {
			b64 := base64.StdEncoding.EncodeToString(png)
			content := agent.BuildOpenAIVisionContent(demoskill.CaptionPrompt(), []agent.MessageAttachment{{MimeType: "image/png", Data: b64}})
			if strings.EqualFold(strings.TrimSpace(llmCfg.Protocol), "anthropic") {
				content = agent.BuildAnthropicVisionContent(demoskill.CaptionPrompt(), []agent.MessageAttachment{{MimeType: "image/png", Data: b64}})
			}
			messages := []interface{}{map[string]interface{}{"role": "user", "content": content}}
			resp, err := demoSkillComplete(c, llmCfg, nil, messages)
			if err != nil || resp == nil || len(resp.Choices) == 0 {
				return "", err
			}
			return strings.TrimSpace(resp.Choices[0].Message.Content), nil
		}
	}
	ocr := func(c context.Context, png []byte) (string, error) {
		if desktopOCRLines == nil {
			return "", nil
		}
		lines := desktopOCRLines(c, png)
		var texts []string
		for _, line := range lines {
			if t := strings.TrimSpace(line.Text); t != "" { // taskengine.OCRResult
				texts = append(texts, t)
			}
		}
		return demoskill.FormatOCRLines(texts), nil
	}
	return llmFn, vision, ocr, nil
}

func demoSkillComplete(ctx context.Context, cfg corelib.MaclawLLMConfig, _ *http.Client, messages []interface{}) (*llm.Response, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		return llm.DoAnthropicRequest(ctx, cfg, messages, nil, http.DefaultClient)
	}
	return llm.DoOpenAIRequestWithOptions(ctx, cfg, messages, nil, http.DefaultClient, llm.OpenAIChatRequestOptions{})
}

func writeDemoSkillError(w http.ResponseWriter, err error, dataRoot string) {
	if err == nil {
		return
	}
	if errorsIsDemoDisabled(err) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeRedactedError(w, err, dataRoot)
}

func errorsIsDemoDisabled(err error) bool {
	return err != nil && (err == agentservice.ErrDemoSkillDisabled || strings.Contains(err.Error(), agentservice.ErrDemoSkillDisabled.Error()))
}
