package skill

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDepsInstallCache_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if DepsInstallCacheValid(dir) {
		t.Error("empty dir should not have valid cache")
	}
}

func TestDepsInstallCache_WriteAndValidate(t *testing.T) {
	dir := t.TempDir()
	// Create a skill.yaml so the hash is non-empty
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: test\n"), 0644)

	// Initially no cache
	if DepsInstallCacheValid(dir) {
		t.Error("should not be valid before write")
	}

	// Write cache
	WriteDepsInstallCache(dir)

	// Should be valid now
	if !DepsInstallCacheValid(dir) {
		t.Error("should be valid after write")
	}
}

func TestDepsInstallCache_InvalidatedByFileChange(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("requests\n"), 0644)

	WriteDepsInstallCache(dir)
	if !DepsInstallCacheValid(dir) {
		t.Fatal("should be valid after write")
	}

	// Modify requirements.txt — cache should invalidate
	time.Sleep(10 * time.Millisecond) // ensure mtime changes
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("requests\nflask\n"), 0644)

	if DepsInstallCacheValid(dir) {
		t.Error("should be invalid after requirements.txt change")
	}
}

func TestDepsInstallCache_InvalidatedByDeletion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: test\n"), 0644)
	WriteDepsInstallCache(dir)

	InvalidateDepsInstallCache(dir)
	if DepsInstallCacheValid(dir) {
		t.Error("should be invalid after explicit invalidation")
	}
}

func TestDepsInstallCache_NoDepsFiles(t *testing.T) {
	dir := t.TempDir()
	// No skill.yaml, no requirements.txt, nothing → hash is empty → no cache written
	WriteDepsInstallCache(dir)
	if DepsInstallCacheValid(dir) {
		t.Error("should not write cache when no dep-relevant files exist")
	}
}

func TestComputeDepsSourceHash_Deterministic(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: test\n"), 0644)
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644)

	hash1 := computeDepsSourceHash(dir)
	hash2 := computeDepsSourceHash(dir)
	if hash1 != hash2 {
		t.Errorf("hash not deterministic: %s != %s", hash1, hash2)
	}
	if hash1 == "" {
		t.Error("hash should not be empty with dep files present")
	}
}
