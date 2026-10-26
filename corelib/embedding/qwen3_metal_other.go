//go:build !darwin

package embedding

import (
	"errors"

	"github.com/RapidAI/CodeClaw/corelib/mtl"
)

type qwen3GPU struct {
	WeightBytes int
	dev         *mtl.Device
}

func newQwen3GPU(q *Qwen3Embedder) (*qwen3GPU, error) {
	return nil, errors.New("metal: not available on this OS")
}

func (g *qwen3GPU) forward(q *Qwen3Embedder, seqs [][]int, outs [][]float32) error {
	return errors.New("metal: not available")
}

func (g *qwen3GPU) release()      {}
func (g *qwen3GPU) ActBytes() int { return 0 }

// shutdown exists so the hardware-acceleration toggle can treat both GPU
// backends the same; Metal is unavailable here so it is a no-op.
func (g *qwen3GPU) shutdown() {}
