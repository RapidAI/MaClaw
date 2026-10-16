package guiapp

import (
	"strings"
	"testing"
)

// The darwin process PATH fix lives behind a build tag, but its merging rules
// are pure string work — keeping them here lets every platform assert them,
// including the one CI runs.
func TestAppendMissingPathEntries(t *testing.T) {
	stripped := "/usr/bin:/bin:/usr/sbin:/sbin"

	// A Finder-launched app inherits launchd's stripped PATH; the standard
	// directories must be added without disturbing the existing order.
	got, changed := appendMissingPathEntries(stripped,
		"/private/tools/bin", "/usr/local/bin", "/opt/homebrew/bin",
		"/usr/bin", "/bin", "/usr/sbin", "/sbin")
	if !changed {
		t.Fatal("expected PATH to change for a stripped launch path")
	}
	want := "/private/tools/bin:/usr/local/bin:/opt/homebrew/bin:" + stripped
	if got != want {
		t.Fatalf("merged PATH = %q, want %q", got, want)
	}

	// Idempotent: platformStartup runs twice (before and after the data
	// directory is known), so the second pass must be a no-op.
	if again, changed := appendMissingPathEntries(got, "/private/tools/bin", "/usr/local/bin", "/opt/homebrew/bin"); changed || again != got {
		t.Fatalf("second call changed PATH: %q -> %q (changed=%v)", got, again, changed)
	}

	// An empty entry means the current directory, which silently turns every
	// exec into a lookup in whatever directory the process sits in.
	withEmpty, _ := appendMissingPathEntries(stripped, "", "/usr/local/bin")
	if containsString(strings.Split(withEmpty, ":"), "") {
		t.Fatalf("empty entry leaked into PATH: %q", withEmpty)
	}
}
