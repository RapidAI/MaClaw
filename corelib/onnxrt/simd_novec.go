//go:build !amd64 && !arm64

package onnxrt

func fmadd3Vec(out, x []float32, w0, w1, w2 float32, n int) int { return 0 }

func fmadd5Vec(out, x []float32, w0, w1, w2, w3, w4 float32, n int) int { return 0 }

func bilinear4Vec(out, a, b, c, d []float32, omx, wx, omy, wy float32) int { return 0 }

func affineVec(out, x []float32, scale, bias float32) int { return 0 }

func transposeMat4(dst, src []float32, outer, A, B int) bool { return false }
