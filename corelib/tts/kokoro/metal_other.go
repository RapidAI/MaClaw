//go:build !darwin

package kokoro

type kokoroGPU struct{}

func (m *Model) gpuResBlocks(x []float32, channels, frames int, style []float32, stage int) ([]float32, bool, error) {
	return nil, false, nil
}

func (m *Model) gpuConv1D(out, x, res []float32, w, bias []float32, key string, inC, inT, outC, outT, k, stride, pad, dil int) (bool, error) {
	return false, nil
}

func (m *Model) gpuConvT1D(out, x, w, bias []float32, key string, inC, inT, outC, outT, k, stride, pad int) (bool, error) {
	return false, nil
}

const gpuMinMACs = 4 << 20

func (m *Model) gpuState() *kokoroGPU { return nil }
