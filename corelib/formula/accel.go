package formula

import "github.com/RapidAI/CodeClaw/corelib/onnxrt"

// Accel reports the ONNX backends Recognize will use.
// Metal, when present, runs encoder convolutions. MatMul and the decoder
// GEMVs use AVX-512, AVX2, or NEON. See onnxrt.Accel.
func Accel() string { return onnxrt.Accel() }
