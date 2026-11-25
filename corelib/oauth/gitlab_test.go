package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeGitLabOrigin(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty defaults", in: "", want: "https://gitlab.com"},
		{name: "bare host", in: "gitlab.example.com", want: "https://gitlab.example.com"},
		{name: "https kept", in: "https://gitlab.com", want: "https://gitlab.com"},
		{name: "trailing slash", in: "https://gitlab.example.com/", want: "https://gitlab.example.com"},
		{name: "port kept", in: "https://gitlab.local:8443", want: "https://gitlab.local:8443"},
		{name: "http rejected", in: "http://gitlab.com", wantErr: true},
		{name: "ssh rejected", in: "ssh://git@gitlab.com", wantErr: true},
		{name: "scp rejected", in: "git@gitlab.com:o/r.git", wantErr: true},
		{name: "project path rejected", in: "https://gitlab.com/octocat/repo", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeGitLabOrigin(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizeGitLabOrigin(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeGitLabOrigin(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeGitLabOrigin(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGitLabScopesHaveRepo(t *testing.T) {
	tests := []struct {
		scopes string
		want   bool
	}{
		{"", true}, // unknown grant — the clone reports the real error
		{"api", true},
		{"write_repository", true},
		{"read_api,write_repository", true},
		{"read_repository", false},
		{"read_user", false},
	}
	for _, tc := range tests {
		if got := GitLabScopesHaveRepo(tc.scopes); got != tc.want {
			t.Fatalf("GitLabScopesHaveRepo(%q) = %v, want %v", tc.scopes, got, tc.want)
		}
	}
}

func TestGitLabTokenPageURL(t *testing.T) {
	got := GitLabTokenPageURL("https://gitlab.example.com/")
	want := "https://gitlab.example.com/-/user_settings/personal_access_tokens?name=MaClaw&scopes=api"
	if got != want {
		t.Fatalf("GitLabTokenPageURL = %q, want %q", got, want)
	}
}

func TestFetchGitLabProjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "pat-token" {
			t.Fatalf("PRIVATE-TOKEN = %q", got)
		}
		if got := r.URL.Query().Get("membership"); got != "true" {
			t.Fatalf("membership = %q", got)
		}
		if got := r.URL.Query().Get("simple"); got != "" {
			t.Fatalf("simple response omits visibility, simple = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"path_with_namespace":"acme/app","description":"App","web_url":"https://gitlab.example.com/acme/app","visibility":"private","default_branch":"main","last_activity_at":"2026-01-02T03:04:05Z"},
			{"path_with_namespace":""}
		]`))
	}))
	defer server.Close()

	rows, err := FetchGitLabProjects(context.Background(), server.URL, "pat-token", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].PathWithNamespace != "acme/app" || rows[0].Visibility != "private" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestGitLabHTTPErrorKeepsAPIMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":{"simple":["cannot be used"]}}`))
	}))
	defer server.Close()

	_, err := FetchGitLabProjects(context.Background(), server.URL, "pat-token", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cannot be used") {
		t.Fatalf("error hides GitLab message: %v", err)
	}
	if strings.Contains(err.Error(), "body_len=") {
		t.Fatalf("error still uses opaque body length: %v", err)
	}
	if strings.Contains(err.Error(), "invalid or revoked") {
		t.Fatalf("permission failure misreported as a revoked token: %v", err)
	}
}
