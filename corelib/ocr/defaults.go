package ocr

import (
	"fmt"
	"os"
)

// DefaultModelTier selects the PP-OCRv6 model size used when the caller does
// not specify one. "tiny", "small" and "medium" are published by PaddlePaddle.
const DefaultModelTier = "small"

// DetModelFilename returns the local file name for the detection model of the
// given tier, e.g. "ppocrv6_small_det.onnx".
func DetModelFilename(tier string) string {
	return fmt.Sprintf("ppocrv6_%s_det.onnx", tier)
}

// RecModelFilename returns the local file name for the recognition model of
// the given tier, e.g. "ppocrv6_small_rec.onnx".
func RecModelFilename(tier string) string {
	return fmt.Sprintf("ppocrv6_%s_rec.onnx", tier)
}

// ModelsZipFilename and DefaultModelsZipURL identify the PP-OCRv6 model bundle
// (the det+rec ONNX files of every tier) distributed with MaClaw. Keep download
// clients aligned by consuming these values instead of duplicating the release
// asset details.
const ModelsZipFilename = "ocr-models.zip"
const DefaultModelsZipURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/ocr-models.zip"

// ModelFileStatus reports whether path is a non-empty file that starts with a
// plausible ONNX protobuf header (field 1, ir_version, as a varint). It
// performs only a two-byte header read, not model loading or full parsing.
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
	var header [2]byte
	if _, err := f.Read(header[:]); err != nil {
		return 0, false
	}
	// ONNX ModelProto field 1 (ir_version) is a varint: tag byte 0x08 followed
	// by a small positive version number (1..10 for all existing opsets).
	if header[0] != 0x08 || header[1] == 0x00 || header[1] > 0x0a {
		return 0, false
	}
	return fi.Size(), true
}
