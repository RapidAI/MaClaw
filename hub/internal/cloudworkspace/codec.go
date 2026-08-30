package cloudworkspace

import (
	"bytes"
	"io"

	"github.com/klauspost/compress/zstd"
)

// compressObject applies bounded, deterministic zstd compression.  Already
// compressed data is kept as-is when compression does not save at least 5%.
func compressObject(plain []byte) ([]byte, string, int) {
	if len(plain) < 1024 {
		return plain, "none", 0
	}
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return plain, "none", 0
	}
	compressed := enc.EncodeAll(plain, nil)
	enc.Close()
	if len(compressed) >= len(plain)-len(plain)/20 {
		return plain, "none", 0
	}
	return compressed, "zstd", 3
}

func decompressObject(data []byte, compression string, plainSize int64) ([]byte, error) {
	if compression != "zstd" {
		if int64(len(data)) > MaxObjectBytes {
			return nil, ErrBlobTooLarge
		}
		return data, nil
	}
	dec, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, ErrBlobCorrupt
	}
	defer dec.Close()
	if plainSize <= 0 || plainSize > MaxObjectBytes {
		return nil, ErrBlobTooLarge
	}
	out := make([]byte, 0, plainSize)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := dec.Read(buf)
		if n > 0 {
			if int64(len(out)+n) > MaxObjectBytes {
				return nil, ErrBlobTooLarge
			}
			out = append(out, buf[:n]...)
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, ErrBlobCorrupt
		}
	}
	if int64(len(out)) != plainSize {
		return nil, ErrBlobCorrupt
	}
	return out, nil
}
