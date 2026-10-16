package windsurf

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// MatchesProvider reports whether a maclaw provider should speak the Devin
// Cascade API. The name match covers the built-in entry; the host match
// covers a custom endpoint pointed at an official Devin server.
func MatchesProvider(name, rawURL string) bool {
	if strings.EqualFold(strings.TrimSpace(name), "Devin") {
		return true
	}
	return isDevinAPIServer(rawURL)
}

// Transport turns OpenAI chat and model calls into Devin Cascade calls and
// writes the OpenAI-shaped response back to the caller.
type Transport struct {
	Base http.RoundTripper
}

// WrapClient returns a client whose Devin chat and model calls are translated.
func WrapClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Transport: http.DefaultTransport}
	}
	if _, ok := client.Transport.(*Transport); ok {
		return client
	}
	clone := *client
	clone.Transport = &Transport{Base: client.Transport}
	return &clone
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || !isDevinAPIPath(req.URL.Path) {
		return t.base().RoundTrip(req)
	}
	body, err := readRequestBody(req)
	if err != nil {
		return nil, err
	}
	key := bearerToken(req)
	upstream := Upstream{}
	if strings.HasSuffix(req.URL.Path, "/models") {
		log.Printf("[Devin] list models keyLen=%d", len(key))
		resp, err := upstream.DoModels(req.Context(), req.URL.String(), key, "")
		if err != nil {
			log.Printf("[Devin] list models failed: %v", err)
			return nil, err
		}
		log.Printf("[Devin] list models status=%d", resp.StatusCode)
		return resp, nil
	}
	model := chatModelName(body)
	stream := chatWantsStream(body)
	log.Printf("[Devin] chat start model=%q stream=%v keyLen=%d", model, stream, len(key))
	resp, err := upstream.DoChatCompletions(req.Context(), req.URL.String(), key, "", body, "", stream)
	if err != nil {
		log.Printf("[Devin] chat failed model=%q: %v", model, err)
		return nil, err
	}
	log.Printf("[Devin] chat status=%d model=%q", resp.StatusCode, model)
	return resp, nil
}

func (t *Transport) base() http.RoundTripper {
	if t != nil && t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func isDevinAPIPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/models")
}

func readRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	defer req.Body.Close()
	return io.ReadAll(req.Body)
}

func bearerToken(req *http.Request) string {
	if req == nil {
		return ""
	}
	value := strings.TrimSpace(req.Header.Get("Authorization"))
	if scheme, token, ok := strings.Cut(value, " "); ok && strings.EqualFold(scheme, "bearer") {
		value = strings.TrimSpace(token)
	}
	if value != "" {
		return value
	}
	return strings.TrimSpace(req.Header.Get("X-Api-Key"))
}

func chatModelName(body []byte) string {
	var probe struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &probe)
	return strings.TrimSpace(probe.Model)
}

func chatWantsStream(body []byte) bool {
	var probe struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &probe)
	return probe.Stream
}
