//go:build !amd64

package yolo

// tryForwardFast is a no-op off amd64: the portable forwardSlow path runs.
func (a *Attention) tryForwardFast(x *Tensor) (*Tensor, bool) {
	return nil, false
}
