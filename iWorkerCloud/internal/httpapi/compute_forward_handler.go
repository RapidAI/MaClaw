package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/compute"
)

// ForwardLLM handles POST /api/centers/{id}/llm/chat/completions.
// It authenticates the requesting Center, selects a provider, forwards the
// request to the upstream LLM via the protocol adapter, records token usage,
// and returns the OpenAI-format response.
func (h *ComputeHandler) ForwardLLM() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		centerID := r.PathValue("id")
		if centerID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing center id"})
			return
		}

		// Authenticate center via secret header.
		secret := r.Header.Get("X-Center-Secret")
		if h.centerSvc != nil {
			center, err := h.centerSvc.AuthenticateCenter(ctx, centerID, secret)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication failed"})
				return
			}
			if center.Status == "disabled" {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "CENTER_DISABLED"})
				return
			}
		}

		// Read and parse the incoming OpenAI-format request body.
		defer r.Body.Close()
		body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read body: " + err.Error()})
			return
		}

		var chatReq compute.OpenAIChatRequest
		if err := json.Unmarshal(body, &chatReq); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request: " + err.Error()})
			return
		}

		// Extract diworker_id from header (set by Center when forwarding).
		diworkerID := r.Header.Get("X-DiWorker-ID")

		// Select a provider: use the assigned providers for this center, pick
		// the first enabled one. A smarter selection (priority, model match)
		// can be added later.
		providers, err := h.store.ListAssignedProviders(ctx, centerID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list providers: " + err.Error()})
			return
		}
		if len(providers) == 0 {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "no providers available"})
			return
		}

		// Pick provider: prefer one matching the requested model, else first.
		provider := providers[0]
		for _, p := range providers {
			if p.Model != "" && p.Model == chatReq.Model {
				provider = p
				break
			}
		}

		// Get protocol adapter.
		adapter, err := compute.GetAdapter(provider.Protocol)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		// Override model if provider has a default and request doesn't specify.
		if chatReq.Model == "" && provider.Model != "" {
			chatReq.Model = provider.Model
		}

		// Convert and forward the request.
		upstreamReq, err := adapter.ConvertRequest(&chatReq, provider)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "convert request: " + err.Error()})
			return
		}

		upstreamResp, err := h.forwardClient.Do(upstreamReq)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream request failed: " + err.Error()})
			return
		}

		// Handle non-200 responses: convert error to OpenAI format.
		if upstreamResp.StatusCode != http.StatusOK {
			errResp, statusCode := compute.ConvertErrorResponse(upstreamResp, provider.Protocol)
			h.recordUsageFromError(ctx, centerID, diworkerID, provider, &chatReq)
			writeJSON(w, statusCode, errResp)
			return
		}

		// Parse the successful response.
		chatResp, err := adapter.ConvertResponse(upstreamResp)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "convert response: " + err.Error()})
			return
		}

		// Extract and record token usage.
		usage := adapter.ExtractUsage(chatResp)
		estimated := false
		if usage == nil {
			// Estimate from content.
			inputText := extractInputText(&chatReq)
			outputText := extractOutputText(chatResp)
			usage = compute.EstimateTokenUsage(inputText, outputText)
			estimated = true
		}

		if usage != nil && h.usageStore != nil {
			model := chatResp.Model
			if model == "" {
				model = chatReq.Model
			}
			rec := compute.TokenUsageRecord{
				CenterID:     centerID,
				DiWorkerID:   diworkerID,
				ProviderName: provider.Name,
				Model:        model,
				InputTokens:  usage.InputTokens,
				OutputTokens: usage.OutputTokens,
				TotalTokens:  usage.InputTokens + usage.OutputTokens,
				Estimated:    estimated,
				Timestamp:    time.Now().UTC().Format(time.RFC3339),
			}
			if err := h.usageStore.RecordUsage(ctx, rec); err != nil {
				log.Printf("[compute] record usage error: %v", err)
			}
		}

		writeJSON(w, http.StatusOK, chatResp)
	}
}

// recordUsageFromError attempts to record estimated usage when the upstream
// returns an error. This captures the input tokens that were consumed.
func (h *ComputeHandler) recordUsageFromError(ctx context.Context, centerID, diworkerID string, provider *compute.ComputeProvider, req *compute.OpenAIChatRequest) {
	if h.usageStore == nil {
		return
	}
	inputText := extractInputText(req)
	usage := compute.EstimateTokenUsage(inputText, "")
	if usage == nil {
		return
	}
	rec := compute.TokenUsageRecord{
		CenterID:     centerID,
		DiWorkerID:   diworkerID,
		ProviderName: provider.Name,
		Model:        req.Model,
		InputTokens:  usage.InputTokens,
		OutputTokens: 0,
		TotalTokens:  usage.InputTokens,
		Estimated:    true,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := h.usageStore.RecordUsage(ctx, rec); err != nil {
		log.Printf("[compute] record error usage: %v", err)
	}
}

// extractInputText concatenates all message contents from the request for
// token estimation purposes.
func extractInputText(req *compute.OpenAIChatRequest) string {
	if req == nil {
		return ""
	}
	var parts []string
	for _, msg := range req.Messages {
		if c, ok := msg["content"].(string); ok {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, "\n")
}

// extractOutputText concatenates all choice message contents from the response.
func extractOutputText(resp *compute.OpenAIChatResponse) string {
	if resp == nil {
		return ""
	}
	var parts []string
	for _, c := range resp.Choices {
		if c.Message.Content != "" {
			parts = append(parts, c.Message.Content)
		}
	}
	return strings.Join(parts, "\n")
}
