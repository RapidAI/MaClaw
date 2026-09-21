package codegenproxy

import (
	"fmt"
	"net/http"
)

type extraHeaderTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t extraHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}
	cloned := req.Clone(req.Context())
	for key, value := range t.headers {
		if key == "" || value == "" {
			continue
		}
		cloned.Header.Set(key, value)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(cloned)
}

// SetUpstreamExtraHeaders attaches extra headers to every upstream request
// made through this server's HTTP client (including the OpenAI SDK path).
// Passing a nil or empty map clears previously installed extra headers.
func (s *Server) SetUpstreamExtraHeaders(headers map[string]string) {
	if s == nil || s.client == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	base := s.client.Transport
	if prev, ok := base.(extraHeaderTransport); ok {
		base = prev.base
	}
	if len(headers) == 0 {
		s.client.Transport = base
		return
	}
	copied := make(map[string]string, len(headers))
	for key, value := range headers {
		copied[key] = value
	}
	s.client.Transport = extraHeaderTransport{base: base, headers: copied}
}
