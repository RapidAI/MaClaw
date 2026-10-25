//go:build !darwin

package onnxrt

type gpuGraph struct{}
type gpuRun struct{}

func (g *Graph) gpuStart() *gpuRun                                         { return nil }
func (r *gpuRun) tryOp(*runCtx, *Node, []*Tensor) ([]*Tensor, bool, error) { return nil, false, nil }
func (r *gpuRun) flush() error                                             { return nil }
func (r *gpuRun) track(*Node, []*Tensor)                                   {}
func (r *gpuRun) dead(int, *Node)                                          {}
func (r *gpuRun) finish(map[string]*Tensor) error                          { return nil }
func (r *gpuRun) abort()                                                   {}

func metalSupported() bool { return false }
