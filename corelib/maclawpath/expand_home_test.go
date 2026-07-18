package maclawpath

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsHomePath(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"~", true},
		{"~/", true},
		{`~\`, true},
		{"~/.maclaw", true},
		{`~\.maclaw`, true},
		{"~backup", false},
		{"~alice/docs", false},
		{"relative", false},
		{"  ~/.x  ", true},
	}
	for _, tt := range tests {
		if got := IsHomePath(tt.in); got != tt.want {
			t.Fatalf("IsHomePath(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestExpandHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "whitespace", in: "  ", want: ""},
		{name: "bare_tilde", in: "~", want: filepath.Clean(home)},
		{name: "tilde_only_slash", in: "~/", want: filepath.Clean(home)},
		{name: "tilde_slash_unix", in: "~/.maclaw/workspace/self_evolving_papers/", want: filepath.Clean(filepath.Join(home, ".maclaw/workspace/self_evolving_papers"))},
		{name: "tilde_backslash", in: `~\.maclaw\workspace`, want: filepath.Clean(filepath.Join(home, ".maclaw", "workspace"))},
		{name: "mixed_separators", in: `~/.maclaw\workspace/foo`, want: filepath.Clean(filepath.Join(home, ".maclaw", "workspace", "foo"))},
		{name: "absolute_unchanged", in: filepath.Join(home, "docs"), want: filepath.Join(home, "docs")},
		{name: "relative_unchanged", in: "relative/path", want: "relative/path"},
		{name: "not_home_prefix", in: "~backup", want: "~backup"},
		{name: "other_user_tilde", in: "~alice/docs", want: "~alice/docs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandHomePath(tt.in)
			if got != tt.want {
				t.Fatalf("ExpandHomePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExpandHomePathWithHomeOverride(t *testing.T) {
	fakeHome := filepath.Join(t.TempDir(), "fake-home")
	got := ExpandHomePathWithHome("~/.maclaw/workspace", fakeHome)
	want := filepath.Clean(filepath.Join(fakeHome, ".maclaw", "workspace"))
	if got != want {
		t.Fatalf("ExpandHomePathWithHome = %q, want %q", got, want)
	}
	// Empty home leaves tilde intact.
	if got := ExpandHomePathWithHome("~/.x", ""); got != "~/.x" {
		t.Fatalf("empty home should leave tilde path, got %q", got)
	}
}

func TestExpandHomePathDoesNotDropHomeOnLeadingSlashSegment(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}
	got := ExpandHomePath("~/.maclaw")
	want := filepath.Clean(filepath.Join(home, ".maclaw"))
	if got != want {
		t.Fatalf("ExpandHomePath(~/.maclaw) = %q, want %q", got, want)
	}
	if !pathHasHomePrefix(got, home) {
		t.Fatalf("expanded path %q is not under home %q", got, home)
	}
	if strings.Contains(got, "~") {
		t.Fatalf("expanded path still contains tilde: %q", got)
	}
	if runtime.GOOS == "windows" && strings.HasPrefix(got, `\.`) {
		t.Fatalf("expanded path lost drive/home: %q", got)
	}
}

func pathHasHomePrefix(path, home string) bool {
	if path == home {
		return true
	}
	sep := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(path), strings.ToLower(home+sep)) ||
			strings.EqualFold(path, home)
	}
	return strings.HasPrefix(path, home+sep) || path == home
}
