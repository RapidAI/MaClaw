package browser

import (
	"strings"
	"testing"
)

// An anchor with no picture file is a spec error caught before anything is
// filled (ZN-22 posted with a third anchor's caption as plain text).
func TestPublishAnchorWithoutFileRefused(t *testing.T) {
	rep, err := RunPublish(nil, PublishSpec{Files: []string{"a.png", "b.png"}, Title: "t", Body: "x\n[[img:1|a]]\ny\n[[img:3|c]]\nz"})
	if err == nil || rep.State != PublishNotSubmitted || !strings.Contains(err.Error(), "[[img:3]]") {
		t.Fatalf("err=%v rep=%+v", err, rep)
	}
}
