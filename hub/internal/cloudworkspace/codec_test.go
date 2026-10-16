package cloudworkspace

import (
	"bytes"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestCompressObjectReuseIsStable(t *testing.T) {
	// The second sample is larger than one 128KB block, so the multi-block
	// path is compared as well as the single-block path.
	samples := [][]byte{
		bytes.Repeat([]byte("cloud-workspace-sync\n"), 80),
		bytes.Repeat([]byte("cloud-workspace-sync\n"), 8000),
	}
	fresh, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	for _, plain := range samples {
		first, firstCodec, firstLevel := compressObject(plain)
		second, secondCodec, secondLevel := compressObject(plain)
		if firstCodec != "zstd" || secondCodec != firstCodec || secondLevel != firstLevel || !bytes.Equal(first, second) {
			t.Fatalf("codec=%s/%s level=%d/%d len=%d/%d plain=%d", firstCodec, secondCodec, firstLevel, secondLevel, len(first), len(second), len(plain))
		}
		// A default-concurrency writer is the previous per-call encoder.
		// EncodeAll still uses one worker, so concurrency 1 must match it.
		if want := fresh.EncodeAll(plain, nil); !bytes.Equal(first, want) {
			t.Fatalf("shared encoder len=%d, default writer len=%d, plain=%d", len(first), len(want), len(plain))
		}
	}
}

func TestCodecCompressedRoundTrip(t *testing.T) {
	plain := bytes.Repeat([]byte("cloud-workspace-sync\n"), 4096)
	stored, compression, _ := compressObject(plain)
	if compression != "zstd" {
		t.Fatalf("compression=%q, want zstd", compression)
	}
	got, err := decompressObject(stored, compression, int64(len(plain)))
	if err != nil || !bytes.Equal(got, plain) || cap(got) != len(got) {
		t.Fatalf("round trip failed: len=%d cap=%d err=%v", len(got), cap(got), err)
	}
	// The second call reuses the pooled decoder after it dropped the first input.
	got, err = decompressObject(stored, compression, int64(len(plain)))
	if err != nil || !bytes.Equal(got, plain) || cap(got) != len(got) {
		t.Fatalf("reused decoder len=%d cap=%d err=%v", len(got), cap(got), err)
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
	// Declared length is half the plaintext, so expansion must stop there
	// instead of materializing the whole frame first.
	if _, err := decompressObject(stored, compression, int64(len(plain)/2)); err != ErrBlobCorrupt {
		t.Fatalf("overlong frame err=%v, want ErrBlobCorrupt", err)
	}
}

func TestCodecWindowIsCappedAtEncoderDefault(t *testing.T) {
	// Exactly one encoder window. Single-segment frames use that length as
	// their window, so this is the largest window compressObject emits.
	plain := bytes.Repeat([]byte("cloud-workspace-sync\n"), (zstdWindowSize/21)+1)
	plain = plain[:zstdWindowSize]
	stored, compression, _ := compressObject(plain)
	if compression != "zstd" {
		t.Fatalf("compression=%q", compression)
	}
	got, err := decompressObject(stored, compression, int64(len(plain)))
	if err != nil || len(got) != len(plain) || !bytes.Equal(got, plain) {
		t.Fatalf("encoder window round trip len=%d err=%v", len(got), err)
	}
	// Past the window the frame is not single-segment. The header then carries
	// an 8MB window descriptor, which must still decode.
	over := bytes.Repeat([]byte("cloud-workspace-sync\n"), (zstdWindowSize+128*1024)/21+1)
	over = over[:zstdWindowSize+128*1024]
	stored, compression, _ = compressObject(over)
	if compression != "zstd" {
		t.Fatalf("above-window compression=%q", compression)
	}
	got, err = decompressObject(stored, compression, int64(len(over)))
	if err != nil || len(got) != len(over) || !bytes.Equal(got, over) {
		t.Fatalf("above-window round trip len=%d err=%v", len(got), err)
	}

	// A valid frame whose window is the next power of two must fail here and
	// still decode with a decoder that allows that window.
	widePlain := bytes.Repeat([]byte("w"), zstdWindowSize*2)
	wide, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithEncoderConcurrency(1),
		zstd.WithWindowSize(zstdWindowSize*2))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wide.Close() })
	frame := wide.EncodeAll(widePlain, nil)
	if _, err := decompressObject(frame, "zstd", int64(len(widePlain))); err != ErrBlobCorrupt {
		t.Fatalf("wide window err=%v", err)
	}
	allow, err := zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxWindow(zstdWindowSize*2))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(allow.Close)
	decoded, err := allow.DecodeAll(frame, nil)
	if err != nil || len(decoded) != len(widePlain) || !bytes.Equal(decoded, widePlain) {
		t.Fatalf("permissive decode len=%d err=%v", len(decoded), err)
	}

	// The rejected frame closes its decoder. A later object still decodes.
	small := bytes.Repeat([]byte("cloud-workspace-sync\n"), 80)
	stored, compression, _ = compressObject(small)
	got, err = decompressObject(stored, compression, int64(len(small)))
	if err != nil || !bytes.Equal(got, small) {
		t.Fatalf("decode after rejected window len=%d err=%v", len(got), err)
	}
}

func TestCodecRejectsOversizedPlaintextMetadata(t *testing.T) {
	if _, err := decompressObject([]byte("not-compressed"), "zstd", MaxObjectBytes+1); err != ErrBlobTooLarge {
		t.Fatalf("oversized metadata err=%v, want ErrBlobTooLarge", err)
	}
}
