package nnapi

import (
	"crypto/sha256"
	"os"
	"path/filepath"
)

// CacheDir is a directory the process can create files in. NNAPI stores the
// compiled graph there. Android's os.TempDir is /data/local/tmp unless TMPDIR
// is set, and an app cannot write that, so the model file's directory is
// preferred. An empty return means the caller should compile without a cache.
func CacheDir(modelPath string) string {
	var candidates []string
	if filepath.IsAbs(modelPath) {
		dir := filepath.Dir(modelPath)
		if dir != "" && dir != "." {
			candidates = append(candidates, filepath.Join(dir, "nnapi-cache"))
		}
	}
	if d, err := os.UserCacheDir(); err == nil && d != "" {
		candidates = append(candidates, filepath.Join(d, "maclaw-nnapi"))
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), "maclaw-nnapi"))
	for _, dir := range candidates {
		if cacheDirWritable(dir) {
			return dir
		}
	}
	return ""
}

func cacheDirWritable(dir string) bool {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".w")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// scopeToken mixes the accelerator name into the model token. NNAPI cannot
// tell two graphs apart when they share a token, and a compilation is for
// one device.
func scopeToken(token [32]byte, device string) [32]byte {
	h := sha256.New()
	_, _ = h.Write(token[:])
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(device))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
