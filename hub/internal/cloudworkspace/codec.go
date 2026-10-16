package cloudworkspace

import (
	"bytes"
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// One encoder for the process. EncodeAll runs the whole buffer on one borrowed
// worker, so concurrency 1 and lower encoder memory keep the same bytes as the
// old per-call writer. The default builds one SpeedDefault worker per CPU and
// gives each a second copy of the 8MB window as history.
var (
	zstdEncOnce sync.Once
	zstdEnc     *zstd.Encoder
	zstdEncErr  error
)

func zstdEncoder() (*zstd.Encoder, error) {
	zstdEncOnce.Do(func() {
		zstdEnc, zstdEncErr = zstd.NewWriter(nil,
			zstd.WithEncoderLevel(zstd.SpeedDefault),
			zstd.WithEncoderConcurrency(1),
			// Allocation only. Match tables and the window stay the same, so
			// EncodeAll still matches a default writer. History then stays near
			// one window instead of two.
			zstd.WithLowerEncoderMem(true))
	})
	return zstdEnc, zstdEncErr
}

// compressObject applies bounded, deterministic zstd compression. Already
// compressed data is kept as-is when compression does not save at least 5%.
func compressObject(plain []byte) ([]byte, string, int) {
	if len(plain) < 1024 {
		return plain, "none", 0
	}
	enc, err := zstdEncoder()
	if err != nil {
		return plain, "none", 0
	}
	compressed := enc.EncodeAll(plain, nil)
	if len(compressed) >= len(plain)-len(plain)/20 {
		return plain, "none", 0
	}
	return compressed, "zstd", 3
}

// zstdMagic is the little-endian zstd frame magic. Legacy blobs were sealed
// after compressObject, but their rows kept compression=none and
// plain_size_bytes=0. Readers recognize this header only after the declared
// codec fails the content-address check.
var zstdMagic = []byte{0x28, 0xB5, 0x2F, 0xFD}

func hasZstdMagic(data []byte) bool {
	return len(data) >= len(zstdMagic) && bytes.Equal(data[:len(zstdMagic)], zstdMagic)
}

// zstdWindowSize is the SpeedDefault back-reference window. compressObject
// never advertises a larger one. The pooled decoder rejects anything bigger
// so a hostile frame cannot allocate the 512MB library default and leave
// that history buffer in the pool.
const zstdWindowSize = 8 << 20

// zstdDecPool reuses single-threaded decoders across a pull. Concurrency is 1,
// so a decoder has no background goroutine. It is returned only after the
// frame is fully consumed and its reader reference is dropped; otherwise the
// pooled decoder would keep the compressed buffer alive. Abandoning a frame
// mid-read closes that decoder so the next borrow cannot observe it.
var zstdDecPool = sync.Pool{
	New: func() any {
		dec, err := zstd.NewReader(nil,
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderMaxWindow(zstdWindowSize))
		if err != nil {
			return err
		}
		return dec
	},
}

func borrowZstdDecoder() (*zstd.Decoder, error) {
	switch v := zstdDecPool.Get().(type) {
	case *zstd.Decoder:
		return v, nil
	case error:
		zstdDecPool.Put(v)
		return nil, v
	default:
		return nil, io.ErrUnexpectedEOF
	}
}

func decompressObject(data []byte, compression string, plainSize int64) ([]byte, error) {
	if compression != "zstd" {
		if int64(len(data)) > MaxObjectBytes {
			return nil, ErrBlobTooLarge
		}
		return data, nil
	}
	if plainSize <= 0 || plainSize > MaxObjectBytes {
		return nil, ErrBlobTooLarge
	}
	dec, err := borrowZstdDecoder()
	if err != nil {
		return nil, ErrBlobCorrupt
	}
	finished := false
	defer func() {
		if finished {
			zstdDecPool.Put(dec)
			return
		}
		dec.Close()
	}()
	if err := dec.Reset(bytes.NewReader(data)); err != nil {
		return nil, ErrBlobCorrupt
	}
	// Read straight into the output. Capacity stays inside the declared
	// length: a corrupt frame cannot reserve that length up front, and a
	// valid frame is not copied through a scratch buffer.
	const zstdDecodeChunk = 32 * 1024
	limit := int(plainSize)
	hint := limit
	if hint > zstdDecodeChunk {
		hint = zstdDecodeChunk
	}
	out := make([]byte, 0, hint)
	for {
		if len(out) == limit {
			// The declared length is full. One more decoded byte means the
			// frame is longer than the row allows.
			var extra [1]byte
			n, readErr := dec.Read(extra[:])
			if n > 0 || readErr != io.EOF {
				return nil, ErrBlobCorrupt
			}
			break
		}
		want := limit - len(out)
		if want > zstdDecodeChunk {
			want = zstdDecodeChunk
		}
		if cap(out) < len(out)+want {
			ncap := cap(out) * 2
			if ncap < len(out)+want {
				ncap = len(out) + want
			}
			if ncap > limit {
				ncap = limit
			}
			grown := make([]byte, len(out), ncap)
			copy(grown, out)
			out = grown
		}
		n, readErr := dec.Read(out[len(out) : len(out)+want])
		if n > 0 {
			out = out[:len(out)+n]
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, ErrBlobCorrupt
		}
		if n == 0 {
			return nil, ErrBlobCorrupt
		}
	}
	// Reset(nil) drops the bytes.Reader so the compressed input can be freed
	// while this decoder waits in the pool.
	if err := dec.Reset(nil); err != nil {
		return nil, ErrBlobCorrupt
	}
	finished = true
	if int64(len(out)) != plainSize {
		return nil, ErrBlobCorrupt
	}
	return out, nil
}
