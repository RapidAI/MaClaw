package kokoro

import (
	"fmt"
	"runtime"
	"sync"
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
	rows := 4 * w.Hidden
	rowWork := rows * (w.InputDim + w.Hidden)
	workers := 1
	if optRound2 && rowWork >= 1<<18 && runtime.GOMAXPROCS(0) > 1 {
		workers = runtime.GOMAXPROCS(0)
		if workers > 8 {
			workers = 8
		}
	}
	chunk := (rows + workers - 1) / workers
	// Each gate-row worker owns dequant scratch so Q8 weights stay safe.
	ihScratches := make([][]float32, workers)
	hhScratches := make([][]float32, workers)
	for i := range ihScratches {
		ihScratches[i] = make([]float32, w.InputDim)
		hhScratches[i] = make([]float32, w.Hidden)
	}
	var wg sync.WaitGroup
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
		if workers == 1 {
			ihScr := ihScratches[0]
			hhScr := hhScratches[0]
			for g := 0; g < rows; g++ {
				if w.WeightIHTensor != nil {
					if err := w.WeightIHTensor.DequantQ8Row(g, ihScr); err != nil {
						return err
					}
					gates[g] += dot32(xt, ihScr)
				} else {
					gates[g] += dot32(xt, w.WeightIH[g*w.InputDim:(g+1)*w.InputDim])
				}
				if w.WeightHHTensor != nil {
					if err := w.WeightHHTensor.DequantQ8Row(g, hhScr); err != nil {
						return err
					}
					gates[g] += dot32(h, hhScr)
				} else {
					gates[g] += dot32(h, w.WeightHH[g*w.Hidden:(g+1)*w.Hidden])
				}
			}
		} else {
			errCh := make(chan error, workers)
			for wk := 0; wk < workers; wk++ {
				g0 := wk * chunk
				g1 := g0 + chunk
				if g1 > rows {
					g1 = rows
				}
				if g0 >= g1 {
					break
				}
				wg.Add(1)
				go func(wk, g0, g1 int) {
					defer wg.Done()
					ihScr := ihScratches[wk]
					hhScr := hhScratches[wk]
					for g := g0; g < g1; g++ {
						if w.WeightIHTensor != nil {
							if err := w.WeightIHTensor.DequantQ8Row(g, ihScr); err != nil {
								errCh <- err
								return
							}
							gates[g] += dot32(xt, ihScr)
						} else {
							gates[g] += dot32(xt, w.WeightIH[g*w.InputDim:(g+1)*w.InputDim])
						}
						if w.WeightHHTensor != nil {
							if err := w.WeightHHTensor.DequantQ8Row(g, hhScr); err != nil {
								errCh <- err
								return
							}
							gates[g] += dot32(h, hhScr)
						} else {
							gates[g] += dot32(h, w.WeightHH[g*w.Hidden:(g+1)*w.Hidden])
						}
					}
				}(wk, g0, g1)
			}
			wg.Wait()
			select {
			case err := <-errCh:
				return err
			default:
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
	// The two directions only read x and write disjoint outputs.
	if optRound2 {
		var wg sync.WaitGroup
		var fwErr, rvErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			fwErr = LSTMLayer(fw, x, steps, w.Forward, false)
		}()
		go func() {
			defer wg.Done()
			rvErr = LSTMLayer(rv, x, steps, w.Reverse, true)
		}()
		wg.Wait()
		if fwErr != nil {
			return fwErr
		}
		if rvErr != nil {
			return rvErr
		}
	} else {
		if err := LSTMLayer(fw, x, steps, w.Forward, false); err != nil {
			return err
		}
		if err := LSTMLayer(rv, x, steps, w.Reverse, true); err != nil {
			return err
		}
	}
	for t := 0; t < steps; t++ {
		copy(out[t*2*h:t*2*h+h], fw[t*h:(t+1)*h])
		copy(out[t*2*h+h:(t+1)*2*h], rv[t*h:(t+1)*h])
	}
	return nil
}
