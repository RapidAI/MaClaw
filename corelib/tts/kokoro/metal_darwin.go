//go:build darwin

package kokoro

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/mtl"
)

// Metal path for the iSTFTNet generator's AdaIN resblocks (the bulk of
// Kokoro's decode time). One command buffer runs all three resblocks of a
// stage; activations stay in shared Metal buffers. Everything else (text
// encoder, LSTMs, F0/N, upsampling, source, iSTFT) stays on the CPU.

type kgConv struct {
	w, b       *mtl.Buffer
	K, Kpad, k int
}

type kokoroGPU struct {
	mu    sync.Mutex
	dev   *mtl.Device
	pipe  map[string]*mtl.Pipeline
	convs map[string]*kgConv
	bufs  map[string]*mtl.Buffer // scratch by role, grown on demand
}

func kokoroMetalDisabled() bool {
	return os.Getenv("MACLAW_KOKORO_METAL") == "0" || os.Getenv("MACLAW_METAL") == "0"
}

var (
	kgOnce sync.Once
	kgDev  *mtl.Device
	kgPipe map[string]*mtl.Pipeline
	kgErr  error
)

func kokoroMetalInit() error {
	kgOnce.Do(func() {
		dev, err := mtl.NewDevice()
		if err != nil {
			kgErr = err
			return
		}
		p, err := dev.CompileOpts(kokoroMetalSrc, []string{"adain_snake", "conv1d", "convt1d", "accum"}, false)
		if err != nil {
			kgErr = err
			return
		}
		kgDev, kgPipe = dev, p
		if err := kokoroMetalSelfCheck(); err != nil {
			kgErr = fmt.Errorf("kokoro metal self-check: %w", err)
			kgDev, kgPipe = nil, nil
		}
	})
	return kgErr
}

func (m *Model) gpuState() *kokoroGPU {
	if kokoroMetalDisabled() {
		return nil
	}
	m.gpuOnce.Do(func() {
		if err := kokoroMetalInit(); err != nil {
			if os.Getenv("KOKORO_METAL_DEBUG") != "" {
				fmt.Fprintln(os.Stderr, "kokoro: metal disabled:", err)
			}
			return
		}
		m.gpu = newKokoroGPU()
	})
	return m.gpu
}

func newKokoroGPU() *kokoroGPU {
	return &kokoroGPU{dev: kgDev, pipe: kgPipe, convs: map[string]*kgConv{}, bufs: map[string]*mtl.Buffer{}}
}

func (g *kokoroGPU) buf(role string, n int) (*mtl.Buffer, error) {
	if b, ok := g.bufs[role]; ok && b.Size >= n*4 {
		return b, nil
	} else if ok {
		b.Release()
	}
	b, err := g.dev.NewBuffer(n * 4)
	if err != nil {
		return nil, err
	}
	g.bufs[role] = b
	return b, nil
}

// conv uploads the weight-normed [outC][inC*k] weight, K padded to 32.
func (g *kokoroGPU) conv(key string, w, bias []float32, outC, inC, k int) (*kgConv, error) {
	if c, ok := g.convs[key]; ok {
		return c, nil
	}
	K := inC * k
	Kpad := (K + 31) / 32 * 32
	wb, err := g.dev.NewBuffer(outC * Kpad * 4)
	if err != nil {
		return nil, err
	}
	dst := wb.Float32s()
	for o := 0; o < outC; o++ {
		copy(dst[o*Kpad:o*Kpad+K], w[o*K:(o+1)*K])
	}
	bb, err := g.dev.NewBuffer(outC * 4)
	if err != nil {
		return nil, err
	}
	if bias != nil {
		copy(bb.Float32s(), bias)
	}
	c := &kgConv{w: wb, b: bb, K: K, Kpad: Kpad, k: k}
	g.convs[key] = c
	return c, nil
}

type kgAdaP struct{ C, T int32 }
type kgConvP struct{ C, M, T, kern, pad, dil, K, Kpad, res, Tin, stride int32 }
type kgAccP struct {
	n, mode int32
	s       float32
}

func (g *kokoroGPU) encAdaIN(e *mtl.Encoder, x, y, gb, alpha *mtl.Buffer, gbOff, alphaOff, C, T int) {
	p := kgAdaP{int32(C), int32(T)}
	e.SetPipeline(g.pipe["adain_snake"])
	e.SetBuffer(x, 0, 0)
	e.SetBuffer(y, 0, 1)
	e.SetBuffer(gb, gbOff, 2)
	e.SetBuffer(alpha, alphaOff, 3)
	e.SetBytes(unsafe.Pointer(&p), int(unsafe.Sizeof(p)), 4)
	e.Dispatch(C, 1, 1, 256, 1, 1)
}

func (g *kokoroGPU) encConv(e *mtl.Encoder, x, y *mtl.Buffer, c *kgConv, C, T, dil int, res bool) {
	g.encConvG(e, x, y, c, C, C, T, T, 1, paddingForKernel(c.k, dil), dil, res)
}

func (g *kokoroGPU) encConvG(e *mtl.Encoder, x, y *mtl.Buffer, c *kgConv, inC, outC, inT, outT, stride, pad, dil int, res bool) {
	p := kgConvP{C: int32(inC), M: int32(outC), T: int32(outT), kern: int32(c.k), pad: int32(pad), dil: int32(dil), K: int32(c.K), Kpad: int32(c.Kpad),
		Tin: int32(inT), stride: int32(stride)}
	if res {
		p.res = 1
	}
	e.SetPipeline(g.pipe["conv1d"])
	e.SetBuffer(x, 0, 0)
	e.SetBuffer(c.w, 0, 1)
	e.SetBuffer(c.b, 0, 2)
	e.SetBuffer(y, 0, 3)
	e.SetBytes(unsafe.Pointer(&p), int(unsafe.Sizeof(p)), 4)
	e.Dispatch((outT+63)/64, (outC+31)/32, 1, 128, 1, 1)
}

func (g *kokoroGPU) encAcc(e *mtl.Encoder, x, y *mtl.Buffer, n, mode int, s float32) {
	p := kgAccP{int32(n), int32(mode), s}
	e.SetPipeline(g.pipe["accum"])
	e.SetBuffer(x, 0, 0)
	e.SetBuffer(y, 0, 1)
	e.SetBytes(unsafe.Pointer(&p), int(unsafe.Sizeof(p)), 2)
	e.Dispatch((n+255)/256, 1, 1, 256, 1, 1)
}

// resBlockWeights holds per-(block,iter) host params.
type kgIter struct {
	ada1, ada2 []float32 // gamma|beta (2C)
	al1, al2   []float32
	c1, c2     *kgConv
	dil        int
}

func (m *Model) gpuResBlocks(x []float32, channels, frames int, style []float32, stage int) ([]float32, bool, error) {
	g := m.gpuState()
	if g == nil {
		return nil, false, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	C, T := channels, frames
	n := C * T
	// Host-side prep: AdaIN fc params (tiny GEMVs) and weight uploads.
	var iters [3][3]kgIter
	for j := 0; j < 3; j++ {
		prefix := fmt.Sprintf("decoder.module.generator.resblocks.%d", stage*3+j)
		k := kernelForGeneratorBlock(prefix)
		for i := 0; i < 3; i++ {
			it := &iters[j][i]
			it.dil = dilationForIndex(i)
			for a, dst := range []*[]float32{&it.ada1, &it.ada2} {
				fcW := mustTensorData(m, fmt.Sprintf("%s.adain%d.%d.fc.weight", prefix, a+1, i))
				fcB := mustTensorData(m, fmt.Sprintf("%s.adain%d.%d.fc.bias", prefix, a+1, i))
				*dst = make([]float32, 2*C)
				if err := Linear(*dst, style, fcW, fcB, len(style), 2*C); err != nil {
					return nil, false, err
				}
			}
			it.al1 = mustTensorData(m, fmt.Sprintf("%s.alpha1.%d", prefix, i))
			it.al2 = mustTensorData(m, fmt.Sprintf("%s.alpha2.%d", prefix, i))
			for ci, dst := range []**kgConv{&it.c1, &it.c2} {
				cp := fmt.Sprintf("%s.convs%d.%d", prefix, ci+1, i)
				w, err := m.cachedFloat32(cp+".weight_norm", func() ([]float32, error) {
					wv, err := m.tensorData(cp + ".weight_v")
					if err != nil {
						return nil, err
					}
					wg, err := m.tensorData(cp + ".weight_g")
					if err != nil {
						return nil, err
					}
					w := make([]float32, len(wv))
					return w, WeightNormConv1DWeight(w, wv, wg, C, C, k)
				})
				if err != nil {
					return nil, false, err
				}
				bias, _ := m.tensorData(cp + ".bias")
				c, err := g.conv(cp, w, bias, C, C, k)
				if err != nil {
					return nil, false, err
				}
				*dst = c
			}
		}
	}
	// Small per-run params buffer: [ada1|ada2|al1|al2] per (j,i).
	stride := 6 * C
	pb, err := g.buf("params", 9*stride)
	if err != nil {
		return nil, false, err
	}
	pf := pb.Float32s()
	for j := 0; j < 3; j++ {
		for i := 0; i < 3; i++ {
			o := (j*3 + i) * stride
			it := &iters[j][i]
			copy(pf[o:], it.ada1)
			copy(pf[o+2*C:], it.ada2)
			copy(pf[o+4*C:], it.al1[:C])
			copy(pf[o+5*C:], it.al2[:C])
		}
	}
	bx, err := g.buf("x", n)
	if err != nil {
		return nil, false, err
	}
	copy(bx.Float32s(), x)
	bo, err := g.buf("out", n)
	if err != nil {
		return nil, false, err
	}
	bt, err := g.buf("t1", n)
	if err != nil {
		return nil, false, err
	}
	bt2, err := g.buf("t2", n)
	if err != nil {
		return nil, false, err
	}
	bs, err := g.buf("sum", n)
	if err != nil {
		return nil, false, err
	}
	e := g.dev.Begin()
	for j := 0; j < 3; j++ {
		g.encAcc(e, bx, bo, n, 0, 0) // out = x
		for i := 0; i < 3; i++ {
			it := &iters[j][i]
			o := (j*3 + i) * stride * 4
			g.encAdaIN(e, bo, bt, pb, pb, o, o+16*C, C, T)
			g.encConv(e, bt, bt2, it.c1, C, T, it.dil, false)
			g.encAdaIN(e, bt2, bt, pb, pb, o+8*C, o+20*C, C, T)
			g.encConv(e, bt, bo, it.c2, C, T, 1, true)
		}
		switch j {
		case 0:
			g.encAcc(e, bo, bs, n, 0, 0)
		case 1:
			g.encAcc(e, bo, bs, n, 1, 0)
		default:
			g.encAcc(e, bo, bs, n, 2, 1.0/3)
		}
	}
	if err := e.Wait(); err != nil {
		return nil, false, err
	}
	return append([]float32(nil), bs.Float32s()[:n]...), true, nil
}

// kokoroMetalSelfCheck compares AdaIN+snake and a dilated residual conv on
// random data against the CPU implementations.
func kokoroMetalSelfCheck() error {
	g := newKokoroGPU()
	defer func() {
		for _, b := range g.bufs {
			b.Release()
		}
		for _, c := range g.convs {
			c.w.Release()
			c.b.Release()
		}
	}()
	rng := uint32(7)
	rnd := func(n int, s float32) []float32 {
		v := make([]float32, n)
		for i := range v {
			rng = rng*1664525 + 1013904223
			v[i] = (float32(int32(rng>>8)%2000)/1000 - 1) * s
		}
		return v
	}
	const C, T, k, dil = 40, 301, 7, 3
	x := rnd(C*T, 2)
	style := rnd(16, 1)
	fcW, fcB, alpha := rnd(2*C*16, 0.3), rnd(2*C, 0.1), rnd(C, 1)
	w, bias := rnd(C*C*k, 0.05), rnd(C, 0.1)
	// CPU reference
	ada := make([]float32, C*T)
	if err := adaINSnake1D(ada, x, C, T, style, fcW, fcB, alpha); err != nil {
		return err
	}
	want := make([]float32, C*T)
	if err := Conv1D(want, ada, w, bias, C, T, C, k, 1, paddingForKernel(k, dil), dil, 1); err != nil {
		return err
	}
	for i := range want {
		want[i] += x[i]
	}
	// GPU
	params := make([]float32, 3*C)
	if err := Linear(params[:2*C], style, fcW, fcB, 16, 2*C); err != nil {
		return err
	}
	copy(params[2*C:], alpha)
	pb, _ := g.buf("p", 3*C)
	copy(pb.Float32s(), params)
	bx, _ := g.buf("x", C*T)
	copy(bx.Float32s(), x)
	bt, _ := g.buf("t", C*T)
	bo, _ := g.buf("o", C*T)
	copy(bo.Float32s(), x)
	c, err := g.conv("sc", w, bias, C, C, k)
	if err != nil {
		return err
	}
	e := g.dev.Begin()
	g.encAdaIN(e, bx, bt, pb, pb, 0, 8*C, C, T)
	g.encConv(e, bt, bo, c, C, T, dil, true)
	if err := e.Wait(); err != nil {
		return err
	}
	got := bo.Float32s()
	maxd := 0.0
	for i, v := range want {
		maxd = math.Max(maxd, math.Abs(float64(v-got[i])))
	}
	if !(maxd < 1e-3) {
		return fmt.Errorf("max |gpu-cpu| = %g", maxd)
	}
	return nil
}

// gpuMinMACs is the work below which single conv ops stay on the CPU (the
// per-op commit/wait round trip would dominate).
const gpuMinMACs = 4 << 20

// gpuConv1D runs one weight-normed conv1d (groups=1) on the GPU:
// out = conv(x) (+ residual when res != nil). w is [outC][inC*k].
func (m *Model) gpuConv1D(out, x, res []float32, w, bias []float32, key string, inC, inT, outC, outT, k, stride, pad, dil int) (bool, error) {
	if outC*outT*inC*k < gpuMinMACs || !gpuScopeOK(key) {
		return false, nil
	}
	g := m.gpuState()
	if g == nil {
		return false, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	c, err := g.conv(key, w, bias, outC, inC, k)
	if err != nil {
		return false, err
	}
	bx, err := g.buf("op_x", inC*inT)
	if err != nil {
		return false, err
	}
	copy(bx.Float32s(), x)
	by, err := g.buf("op_y", outC*outT)
	if err != nil {
		return false, err
	}
	if res != nil {
		copy(by.Float32s(), res)
	}
	e := g.dev.Begin()
	g.encConvG(e, bx, by, c, inC, outC, inT, outT, stride, pad, dil, res != nil)
	if err := e.Wait(); err != nil {
		return false, err
	}
	copy(out, by.Float32s()[:outC*outT])
	return true, nil
}

// gpuConvT1D runs a groups=1 transposed conv1d; w is [inC][outC][k].
func (m *Model) gpuConvT1D(out, x, w, bias []float32, key string, inC, inT, outC, outT, k, stride, pad int) (bool, error) {
	if outC*outT*inC*k/stride < gpuMinMACs || !gpuScopeOK(key) {
		return false, nil
	}
	g := m.gpuState()
	if g == nil {
		return false, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	c, ok := g.convs[key]
	if !ok {
		wb, err := g.dev.NewBuffer(len(w) * 4)
		if err != nil {
			return false, err
		}
		copy(wb.Float32s(), w)
		bb, err := g.dev.NewBuffer(outC * 4)
		if err != nil {
			return false, err
		}
		if bias != nil {
			copy(bb.Float32s(), bias)
		}
		c = &kgConv{w: wb, b: bb, k: k}
		g.convs[key] = c
	}
	bx, err := g.buf("op_x", inC*inT)
	if err != nil {
		return false, err
	}
	copy(bx.Float32s(), x)
	by, err := g.buf("op_y", outC*outT)
	if err != nil {
		return false, err
	}
	p := struct{ inC, outC, inT, outT, kern, stride, pad, hasBias int32 }{int32(inC), int32(outC), int32(inT), int32(outT), int32(k), int32(stride), int32(pad), 1}
	e := g.dev.Begin()
	e.SetPipeline(g.pipe["convt1d"])
	e.SetBuffer(bx, 0, 0)
	e.SetBuffer(c.w, 0, 1)
	e.SetBuffer(c.b, 0, 2)
	e.SetBuffer(by, 0, 3)
	e.SetBytes(unsafe.Pointer(&p), int(unsafe.Sizeof(p)), 4)
	e.Dispatch((outT+63)/64, outC, 1, 64, 1, 1)
	if err := e.Wait(); err != nil {
		return false, err
	}
	copy(out, by.Float32s()[:outC*outT])
	return true, nil
}

// gpuScopeOK limits single-op offload by weight-name substring
// (KOKORO_METAL_SCOPE, debugging aid; empty = all). The prosody predictor
// (F0/N) stays on the CPU by default: its outputs are integrated into the
// harmonic source phase, so ~1e-6 conv rounding differences grow into
// inaudible but measurable waveform drift (SNR ~30 dB vs ~75 dB without),
// while it is only a few percent of runtime. KOKORO_METAL_PREDICTOR=1 opts in.
func gpuScopeOK(key string) bool {
	if strings.Contains(key, "predictor") && os.Getenv("KOKORO_METAL_PREDICTOR") != "1" {
		return false
	}
	sc := os.Getenv("KOKORO_METAL_SCOPE")
	return sc == "" || strings.Contains(key, sc)
}
