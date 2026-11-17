package inputcorrect

import "os"

func osWrite(path string, raw []byte) error {
	return os.WriteFile(path, raw, 0o600)
}

func readStore(t interface{ Fatalf(string, ...any) }, dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("dir: %v", err)
	}
	var b []byte
	for _, e := range entries {
		if e.IsDir() || !stringsHasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		b = append(b, raw...)
	}
	return string(b)
}

func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
