package kokoro

import (
	"fmt"
)

type LSTMWeights struct {
	WeightIH       []float32 // [4H, I]
	WeightHH       []float32 // [4H, H]
	WeightIHTensor *Tensor
	WeightHHTensor *Tensor
	BiasIH         []float32 // [4H]
	BiasHH         []float32 // [4H]
	InputDim       int
	Hidden         int
}

func LSTMLayer(out []float32, x []float32, steps int, w LSTMWeights, reverse bool) error {
	if w.InputDim <= 0 || w.Hidden <= 0 {
		return fmt.Errorf("kokoro: invalid lstm dims")
	}
	if len(x) != steps*w.InputDim || len(out) != steps*w.Hidden {
		return fmt.Errorf("kokoro: lstm shape mismatch")
	}
	if w.WeightIHTensor == nil && len(w.WeightIH) != 4*w.Hidden*w.InputDim {
		return fmt.Errorf("kokoro: lstm weight_ih shape mismatch")
	}
	if w.WeightHHTensor == nil && len(w.WeightHH) != 4*w.Hidden*w.Hidden {
		return fmt.Errorf("kokoro: lstm weight shape mismatch")
	}
	h := make([]float32, w.Hidden)
	c := make([]float32, w.Hidden)
	gates := make([]float32, 4*w.Hidden)
	ihScratch := make([]float32, w.InputDim)
	hhScratch := make([]float32, w.Hidden)
	for step := 0; step < steps; step++ {
		t := step
		if reverse {
			t = steps - 1 - step
		}
		xt := x[t*w.InputDim : (t+1)*w.InputDim]
		for i := range gates {
			v := float32(0)
			if w.BiasIH != nil {
				v += w.BiasIH[i]
			}
			if w.BiasHH != nil {
				v += w.BiasHH[i]
			}
			gates[i] = v
		}
		for g := 0; g < 4*w.Hidden; g++ {
			if w.WeightIHTensor != nil {
				if err := w.WeightIHTensor.DequantQ8Row(g, ihScratch); err != nil {
					return err
				}
				gates[g] += dot32(xt, ihScratch)
			} else {
				gates[g] += dot32(xt, w.WeightIH[g*w.InputDim:(g+1)*w.InputDim])
			}
			if w.WeightHHTensor != nil {
				if err := w.WeightHHTensor.DequantQ8Row(g, hhScratch); err != nil {
					return err
				}
				gates[g] += dot32(h, hhScratch)
			} else {
				gates[g] += dot32(h, w.WeightHH[g*w.Hidden:(g+1)*w.Hidden])
			}
		}
		for i := 0; i < w.Hidden; i++ {
			ig := sigmoid(gates[i])
			fg := sigmoid(gates[w.Hidden+i])
			gg := tanh(gates[2*w.Hidden+i])
			og := sigmoid(gates[3*w.Hidden+i])
			c[i] = fg*c[i] + ig*gg
			h[i] = og * tanh(c[i])
			out[t*w.Hidden+i] = h[i]
		}
	}
	return nil
}

type BiLSTMWeights struct {
	Forward LSTMWeights
	Reverse LSTMWeights
}

func BiLSTMLayer(out []float32, x []float32, steps int, w BiLSTMWeights) error {
	h := w.Forward.Hidden
	if h == 0 || w.Reverse.Hidden != h || len(out) != steps*h*2 {
		return fmt.Errorf("kokoro: bilstm shape mismatch")
	}
	fw := make([]float32, steps*h)
	rv := make([]float32, steps*h)
	if err := LSTMLayer(fw, x, steps, w.Forward, false); err != nil {
		return err
	}
	if err := LSTMLayer(rv, x, steps, w.Reverse, true); err != nil {
		return err
	}
	for t := 0; t < steps; t++ {
		copy(out[t*2*h:t*2*h+h], fw[t*h:(t+1)*h])
		copy(out[t*2*h+h:(t+1)*2*h], rv[t*h:(t+1)*h])
	}
	return nil
}
