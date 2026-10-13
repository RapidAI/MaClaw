//go:build !darwin

package kokoro

type kokoroGPU struct{}

func (m *Model) gpuResBlocks(x []float32, channels, frames int, style []float32, stage int) ([]float32, bool, error) {
	return nil, false, nil
}
