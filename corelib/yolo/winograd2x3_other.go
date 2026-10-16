//go:build !amd64

package yolo

// WinoFilter2x3 is a placeholder off amd64: the F(2,3) Winograd path uses
// the amd64 GEMM internals and is never initialized on other platforms.
type WinoFilter2x3 struct{}

// initWinograd2x3 is a no-op off amd64 (Wino2x3 stays nil, so Forward never
// routes to the Winograd path).
func (c *Conv2dBNSiLU) initWinograd2x3() {}

// forwardWinograd2x3 is unreachable off amd64.
func (c *Conv2dBNSiLU) forwardWinograd2x3(input *Tensor) *Tensor { return nil }
