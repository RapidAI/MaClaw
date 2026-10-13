//go:build !darwin

package asr

import "errors"

type svGPU struct{ WeightSize int }

func newSVGPU(m *SenseVoiceModel) (*svGPU, error) { return nil, errors.New("metal: not available") }

func (g *svGPU) encode(feats []float32, T, featsDim int) ([]float32, error) {
	return nil, errors.New("metal: not available")
}
func (g *svGPU) release()         {}
func (g *svGPU) ActBytes() int    { return 0 }
func (g *svGPU) logitStride() int { return 0 }
