package remote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const responsePreviewLimit = 240

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// DecodeHTTPJSONResponse reads an HTTP response body and decodes JSON.
// Some Hub deployments have returned UTF-8 BOM-prefixed JSON, and proxies can
// return HTML error pages. Normalize both into useful caller-facing errors.
func DecodeHTTPJSONResponse(resp *http.Response, target any, label string) error {
	if resp == nil || resp.Body == nil {
		return fmt.Errorf("%s response is empty", responseLabel(label))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s response: %w", responseLabel(label), err)
	}
	if err := DecodeJSONResponseBody(body, target); err != nil {
		preview := responsePreview(body)
		if preview != "" {
			return fmt.Errorf("decode %s response: %w; body starts with %q", responseLabel(label), err, preview)
		}
		return fmt.Errorf("decode %s response: %w", responseLabel(label), err)
	}
	return nil
}

func DecodeJSONResponseBody(body []byte, target any) error {
	body = bytes.TrimSpace(bytes.TrimPrefix(body, utf8BOM))
	if len(body) == 0 {
		return io.EOF
	}
	return json.Unmarshal(body, target)
}

func responseLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "JSON"
	}
	return label
}

func responsePreview(body []byte) string {
	body = bytes.TrimSpace(bytes.TrimPrefix(body, utf8BOM))
	if len(body) == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > responsePreviewLimit {
		text = text[:responsePreviewLimit] + "..."
	}
	return text
}
