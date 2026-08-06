package jobs

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"clawmatemaker/internal/firmware"
)

func TestImportRejectsUnsafeExtensionBeforeAnyPackageRead(t *testing.T) {
	if _, err := NewImportJob(t.TempDir(), "bread-compact", "C:/firmware.bin", firmware.TrustStore{}, nil); err == nil {
		t.Fatal("non-clawfw offline package accepted")
	}
}

func TestImportRejectsMissingPackage(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	job, err := NewImportJob(t.TempDir(), "bread-compact", t.TempDir()+"/missing.clawfw", firmware.TrustStore{"test": pub}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := job.Run(t.Context())
	if err == nil || result.InstallStatus != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
