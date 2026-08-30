package cloudworkspace

import (
	"bytes"
	"testing"
)

func TestCodecCompressedRoundTrip(t *testing.T) {
	plain := bytes.Repeat([]byte("cloud-workspace-sync\n"), 4096)
	stored, compression, _ := compressObject(plain)
	if compression != "zstd" {
		t.Fatalf("compression=%q, want zstd", compression)
	}
	got, err := decompressObject(stored, compression, int64(len(plain)))
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("round trip failed: len=%d err=%v", len(got), err)
	}
}

func TestCodecRejectsCorruptOrWrongSize(t *testing.T) {
	plain := bytes.Repeat([]byte("x"), 4096)
	stored, compression, _ := compressObject(plain)
	if _, err := decompressObject(stored, compression, int64(len(plain)-1)); err != ErrBlobCorrupt {
		t.Fatalf("wrong size err=%v, want ErrBlobCorrupt", err)
	}
	if _, err := decompressObject(stored[:len(stored)/2], compression, int64(len(plain))); err == nil {
		t.Fatal("truncated zstd stream unexpectedly decoded")
	}
}

func TestCodecRejectsOversizedPlaintextMetadata(t *testing.T) {
	if _, err := decompressObject([]byte("not-compressed"), "zstd", MaxObjectBytes+1); err != ErrBlobTooLarge {
		t.Fatalf("oversized metadata err=%v, want ErrBlobTooLarge", err)
	}
}
