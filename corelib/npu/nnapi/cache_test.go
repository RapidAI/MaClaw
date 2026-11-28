package nnapi

import (
	"path/filepath"
	"testing"
)

func TestCacheDirPrefersModelDirectory(t *testing.T) {
	root := t.TempDir()
	model := filepath.Join(root, "models", "sensevoice.gguf")
	got := CacheDir(model)
	want := filepath.Join(root, "models", "nnapi-cache")
	if got != want {
		t.Fatalf("cache dir %q want %q", got, want)
	}
}

func TestScopeTokenDependsOnDevice(t *testing.T) {
	var token [32]byte
	token[0] = 7
	htp := scopeToken(token, "htp")
	dsp := scopeToken(token, "dsp")
	if htp == dsp || htp == token {
		t.Fatal("device token collided")
	}
}

func TestOrderAccelRank(t *testing.T) {
	got := orderAccelRank([]int64{1000006, 1000008, 1000006})
	want := []int{1, 0, 2}
	if len(got) != len(want) {
		t.Fatalf("rank %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rank %v want %v", got, want)
		}
	}
}
