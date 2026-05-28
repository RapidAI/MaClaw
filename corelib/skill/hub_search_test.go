package skill

import "testing"

func TestIsSourceAllowedNormalizesAliases(t *testing.T) {
	allowed := []string{"hubcenter", "git_hub", "enterprise"}
	for _, source := range []string{"skillhub", "github", "enterprise_hub"} {
		if !IsSourceAllowed(source, allowed) {
			t.Fatalf("source %q should be allowed by aliases %#v", source, allowed)
		}
	}
	if IsSourceAllowed("clawhub", allowed) {
		t.Fatal("clawhub should not be allowed")
	}
}
