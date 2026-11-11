package guiapp

import "testing"

func TestNormalizeGitHubCloneURL(t *testing.T) {
	cases := []struct {
		name, in, wantURL, wantRepo string
		wantErr                     bool
	}{
		{name: "https plain", in: "https://github.com/octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "https with .git", in: "https://github.com/octocat/Hello-World.git", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "https deep path normalized", in: "https://github.com/octocat/Hello-World/tree/main/docs", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "ssh shorthand", in: "git@github.com:octocat/Hello-World.git", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "ssh scheme", in: "ssh://git@github.com/octocat/Hello-World.git", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "bare host", in: "github.com/octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "uppercase host", in: "GitHub.COM/octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "owner/repo shorthand", in: "octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: "   ", wantErr: true},
		{name: "not github", in: "https://gitlab.com/octocat/Hello-World", wantErr: true},
		{name: "lookalike host rejected", in: "https://github.com.evil.com/o/r", wantErr: true},
		{name: "no repo path", in: "https://github.com/", wantErr: true},
		{name: "owner only", in: "https://github.com/octocat", wantErr: true},
		{name: "ssh non-github", in: "git@gitlab.com:o/r.git", wantErr: true},
		{name: "ssh scheme non-github", in: "ssh://git@gitlab.com/o/r.git", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotRepo, err := normalizeGitHubCloneURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got url=%q repo=%q", gotURL, gotRepo)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotURL != tc.wantURL || gotRepo != tc.wantRepo {
				t.Fatalf("got (%q, %q), want (%q, %q)", gotURL, gotRepo, tc.wantURL, tc.wantRepo)
			}
		})
	}
}
