package asr

import (
	"encoding/binary"
	"io"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

// DefaultModelFilename and DefaultModelDownloadURL identify the SenseVoice
// Small GGUF artifact distributed with MaClaw. Keep download clients aligned by
// consuming these values instead of duplicating the release asset details.
const DefaultModelFilename = "sensevoice-small-q8.gguf"
const DefaultModelDownloadURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/sensevoice-small-q8.gguf"

// ModelFileStatus reports whether path is a non-empty GGUF file. It performs
// only a four-byte header read, not model loading or full GGUF parsing.
func ModelFileStatus(path string) (int64, bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Size() <= 0 {
		return 0, false
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var header [4]byte
	if _, err := io.ReadFull(f, header[:]); err != nil || binary.LittleEndian.Uint32(header[:]) != gguf.Magic {
		return 0, false
	}
	return fi.Size(), true
}
