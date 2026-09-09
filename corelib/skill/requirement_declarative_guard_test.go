package skill

import "testing"

// TestValidDeclarativeRequirement locks the guard on the declared-dependency
// install path (skill.yaml `requires:` -> PipFixer / NpmFixer).
//
// There is no shell in the install path, so argv injection is not the concern;
// a single token is. pip and npm both accept a URL or a filesystem path as the
// install target, so a manifest declaring
// `requires: python: ["https://host/x.whl"]` would otherwise install straight
// from an attacker-chosen location, and a leading `-` would become a flag.
//
// This guard is deliberately looser than validDependencyInstallName (which
// covers runtime-scraped names): declared requirements still allow an npm scope
// (@types/node), an inline version (lodash@^4.17), and version constraints
// that splitPkgVersion leaves attached (">=1.0,<2").
func TestValidDeclarativeRequirement(t *testing.T) {
	accepted := []struct{ name, version string }{
		{"requests", ""},
		{"opencv-python", ""},
		{"Pillow", ""},
		{"rapidocr-onnxruntime", ""},
		{"pytest_cov", ""},
		{"foo.bar", ""},
		{"c++", ""},
		{"requests", ">=2.0"},
		{"requests", "==1.0.*"},
		{"requests", "~=1.4"},
		{"requests", "!=2.0"},
		{"requests", ">1,<2"},
		{"requests", ">=1.0,<2"},
		{"@types/node", ""},
		{"@types/node", "^18.0.0"},
		{"lodash", "^4.17.21"},
	}
	for _, c := range accepted {
		if err := validDeclarativeRequirement(c.name, c.version); err != nil {
			t.Errorf("validDeclarativeRequirement(%q, %q) = %v, want nil", c.name, c.version, err)
		}
	}

	rejected := []struct{ name, version string }{
		// URL / remote install targets.
		{"https://evil.example/x.whl", ""},
		{"http://evil.example/x", ""},
		{"file:///tmp/x", ""},
		{"git+https://evil.example/repo.git", ""},
		// Filesystem paths.
		{"/etc/passwd", ""},
		{"../../evil", ""},
		{".", ""},
		{"..", ""},
		{"C:\\evil", ""},
		// Flags.
		{"--user", ""},
		{"-r", ""},
		{"--index-url", ""},
		// Shell / injection attempts (defence in depth: no shell here, but a
		// stray token must never reach the installer).
		{"a b", ""},
		{"name;rm", ""},
		{"x`id`", ""},
		{"$(id)", ""},
		// Bad version constraints.
		{"requests", ">=1.0; rm -rf /"},
		{"requests", "../../etc"},
		{"requests", "https://evil.example"},
		// Empty name.
		{"", ""},
	}
	for _, c := range rejected {
		if err := validDeclarativeRequirement(c.name, c.version); err == nil {
			t.Errorf("validDeclarativeRequirement(%q, %q) = nil, want rejection", c.name, c.version)
		}
	}

	long := make([]byte, declarativeRequirementMaxLen+1)
	for i := range long {
		long[i] = 'a'
	}
	if err := validDeclarativeRequirement(string(long), ""); err == nil {
		t.Error("an over-long package name must be rejected")
	}
}

// The fixers must refuse before reaching the installer, not merely parse.
func TestPipAndNpmFixersRejectNonPackageRequirements(t *testing.T) {
	pip := &PipFixer{}
	npm := &NpmFixer{}
	for _, name := range []string{"https://evil.example/x.whl", "--user", "/etc/passwd"} {
		if err := pip.Fix(Requirement{Type: "pip", Name: name}); err == nil {
			t.Errorf("PipFixer accepted %q", name)
		}
		if err := npm.Fix(Requirement{Type: "npm", Name: name}); err == nil {
			t.Errorf("NpmFixer accepted %q", name)
		}
	}
}
