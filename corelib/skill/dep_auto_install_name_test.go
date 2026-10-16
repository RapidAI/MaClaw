package skill

import "testing"

// TestValidDependencyInstallName locks the guard on auto-installed package
// names, which are scraped from runtime tool output rather than a manifest.
//
// There is no shell in the install path, so argv injection is not the concern;
// a single token is. pip and npm both accept a URL or a filesystem path as the
// install target, so an error line of `No module named https://host/x.whl`
// would have installed straight from an attacker-controlled location, and a
// leading `-` would have been parsed as a flag.
func TestValidDependencyInstallName(t *testing.T) {
	accepted := []string{
		"requests", "opencv-python", "Pillow", "rapidocr-onnxruntime",
		"pytest_cov", "foo.bar", "c++", "a", "numpy1",
	}
	for _, name := range accepted {
		if !validDependencyInstallName(name) {
			t.Errorf("validDependencyInstallName(%q) = false, want true", name)
		}
	}

	rejected := []string{
		"",
		"--user", "-r", "--index-url",
		"https://evil.example/x.whl", "http://evil.example/x",
		"file:///tmp/x", "pkg@https://evil.example",
		"/etc/passwd", "../../evil", ".", "..",
		"a b", "name;rm", "x`id`", "C://evil",
	}
	for _, name := range rejected {
		if validDependencyInstallName(name) {
			t.Errorf("validDependencyInstallName(%q) = true, want false", name)
		}
	}

	long := make([]byte, depInstallNameMaxLen+1)
	for i := range long {
		long[i] = 'a'
	}
	if validDependencyInstallName(string(long)) {
		t.Error("an over-long package name must be rejected")
	}
}
