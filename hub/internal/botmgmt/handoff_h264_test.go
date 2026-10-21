package botmgmt

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestH264FallbackLocationOnlySiblingVNC(t *testing.T) {
	cases := map[string]string{
		"vnc.html?autoconnect=1&path=x": "vnc.html?autoconnect=1&path=x",
		"https://evil.example/":         "",
		"vnc.html//evil":                "",
		"../vnc.html":                   "",
		"":                              "",
	}
	for in, want := range cases {
		if got := h264FallbackLocation(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// An old desktop answers h264.html with 404; Hub turns that into a redirect
// to the noVNC page the GUI passed in ?vnc=.
func TestProxyDesktopHandoffFallsBackToVNC(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gate" {
			t.Errorf("missing gate bearer")
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	u.User = url.UserPassword("x", "gate")
	u.Path = "/vnc.html"
	s := &Service{}
	gated := s.gateDesktopHandoff(u.String())
	token, _, err := ParseHandoffPath("http://hub" + gated)
	if err != nil {
		t.Fatal(err)
	}
	vnc := "vnc.html?autoconnect=1&path=x"
	req := httptest.NewRequest("GET", "/api/v1/desktop-handoff/"+token+"/h264.html?view_only=0&vnc="+url.QueryEscape(vnc), nil)
	rec := httptest.NewRecorder()
	s.ProxyDesktopHandoff(rec, req, token, "h264.html")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != vnc {
		t.Fatalf("code %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = httptest.NewRecorder()
	s.ProxyDesktopHandoff(rec, httptest.NewRequest("GET", "/x/other.js", nil), token, "other.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other 404 changed to %d", rec.Code)
	}
}
