package skill

import (
	"reflect"
	"testing"
)

func TestValidateSourceNamesAcceptsAliasesAndEnterpriseHub(t *testing.T) {
	if err := ValidateSourceNames([]string{"hubcenter", "git_hub", "enterprise"}); err != nil {
		t.Fatalf("ValidateSourceNames() error = %v", err)
	}
	if err := ValidateSourceNames([]string{"unknown"}); err == nil {
		t.Fatal("ValidateSourceNames() should reject unknown source")
	}
}

func TestIntersectSourcesNormalizesAliases(t *testing.T) {
	got := IntersectSources([]string{"hubcenter", "git_hub", "enterprise"}, []string{"skillhub", "github", "enterprise_hub"})
	want := []string{"skillhub", "github", "enterprise_hub"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IntersectSources() = %#v, want %#v", got, want)
	}
}
