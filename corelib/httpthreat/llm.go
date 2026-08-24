package httpthreat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// NewHTTPArbitrator calls one approved endpoint with one sample. Never used on detect.
func NewHTTPArbitrator(endpoint, token string) ArbitrateFunc {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil
	}
	client := &http.Client{Timeout: 15 * time.Second}
	return func(preview, ruleClass, headClass string, vocab []string) (LLMAdvice, error) {
		body, _ := json.Marshal(map[string]any{
			"preview": preview, "rule_class": ruleClass, "head_class": headClass, "vocab": vocab,
		})
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return LLMAdvice{Abstain: true, Reason: err.Error()}, err
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return LLMAdvice{Abstain: true, Reason: err.Error()}, err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode >= 300 {
			return LLMAdvice{Abstain: true, Reason: fmt.Sprintf("llm http %d", resp.StatusCode)}, fmt.Errorf("llm http %d", resp.StatusCode)
		}
		return ParseLLMAdvice(string(raw)), nil
	}
}

// ArbitrateFunc is an offline label helper. It is never used on the detect hot path.
type ArbitrateFunc func(preview, ruleClass, headClass string, vocab []string) (LLMAdvice, error)

type LLMAdvice struct {
	Class   string `json:"class,omitempty"`
	Abstain bool   `json:"abstain,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func (e *Engine) SetArbitrator(fn ArbitrateFunc) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.arbitrate = fn
	e.mu.Unlock()
}

// ParseLLMAdvice accepts JSON or a bare class token. Unknown class => abstain.
func ParseLLMAdvice(raw string) LLMAdvice {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return LLMAdvice{Abstain: true, Reason: "empty"}
	}
	var out LLMAdvice
	if json.Unmarshal([]byte(raw), &out) == nil {
		return normalizeAdvice(out)
	}
	return normalizeAdvice(LLMAdvice{Class: raw})
}

func normalizeAdvice(in LLMAdvice) LLMAdvice {
	class := strings.TrimSpace(strings.ToLower(in.Class))
	if in.Abstain || class == "" || class == "abstain" || class == ClassUnknown || !IsTrainableClass(class) {
		reason := strings.TrimSpace(in.Reason)
		if reason == "" {
			reason = "abstain"
		}
		return LLMAdvice{Abstain: true, Reason: reason}
	}
	return LLMAdvice{Class: class, Reason: strings.TrimSpace(in.Reason)}
}

func (e *Engine) Arbitrate(id NodeIdentity, sampleID, role string) (LLMAdvice, error) {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return LLMAdvice{}, err
	}
	if role != RoleAnalyst && role != RoleAdmin {
		return LLMAdvice{}, ErrForbidden
	}
	s, ok := e.corpus.Get(sampleID)
	if !ok || s.TenantID != tenant {
		return LLMAdvice{}, ErrNotFound
	}
	if s.GoldSource == GoldHuman {
		return LLMAdvice{}, fmt.Errorf("%w: human gold locked", ErrConflict)
	}
	e.mu.Lock()
	fn := e.arbitrate
	e.mu.Unlock()
	if fn == nil {
		adv := LLMAdvice{Abstain: true, Reason: "llm not configured"}
		_, _ = e.corpus.SetAdvice(sampleID, "", adv.Reason, true)
		e.flushTenant(tenant)
		return adv, nil
	}
	adv, err := fn(s.Preview, s.RuleClass, s.HeadClass, append([]string(nil), TrainableClasses...))
	if err != nil {
		adv = LLMAdvice{Abstain: true, Reason: err.Error()}
	}
	adv = normalizeAdvice(adv)
	triple := !adv.Abstain && adv.Class != s.RuleClass && adv.Class != s.HeadClass
	needHuman := adv.Abstain || triple
	if needHuman {
		reason := adv.Reason
		if triple && reason == "" {
			reason = "triple disagreement"
		}
		_, _ = e.corpus.SetAdvice(sampleID, adv.Class, reason, true)
		e.flushTenant(tenant)
		adv.Abstain = adv.Abstain || triple
		if triple {
			adv.Reason = reason
		}
		return adv, nil
	}
	if err := e.Label(id, LabelRequest{SampleID: sampleID, GoldClass: adv.Class, GoldSource: GoldLLM, Role: role}); err != nil {
		return LLMAdvice{}, err
	}
	_, _ = e.corpus.SetAdvice(sampleID, adv.Class, adv.Reason, false)
	e.flushTenant(tenant)
	return adv, nil
}

func (e *Engine) PromoteLLM(id NodeIdentity, sampleID, role string) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAnalyst && role != RoleAdmin {
		return ErrForbidden
	}
	s, ok := e.corpus.Get(sampleID)
	if !ok || s.TenantID != tenant {
		return ErrNotFound
	}
	if !IsTrainableClass(s.LLMClass) {
		return fmt.Errorf("%w: no llm class", ErrInvalid)
	}
	if err := e.Label(id, LabelRequest{SampleID: sampleID, GoldClass: s.LLMClass, GoldSource: GoldHuman, Role: role}); err != nil {
		return err
	}
	_, _ = e.corpus.SetAdvice(sampleID, s.LLMClass, s.LLMReason, false)
	e.flushTenant(tenant)
	return nil
}

func (e *Engine) RejectLLM(id NodeIdentity, sampleID, role string) error {
	tenant, err := tenantFromIdentity(id, "")
	if err != nil {
		return err
	}
	if role != RoleAnalyst && role != RoleAdmin {
		return ErrForbidden
	}
	s, ok := e.corpus.Get(sampleID)
	if !ok || s.TenantID != tenant {
		return ErrNotFound
	}
	if s.GoldSource == GoldLLM {
		if !e.corpus.Unlabel(sampleID) {
			return ErrNotFound
		}
	}
	_, _ = e.corpus.SetAdvice(sampleID, "", "rejected", true)
	e.flushTenant(tenant)
	return nil
}
