package oauth

import "testing"

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
