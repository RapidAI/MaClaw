package diarization

// This file contains the CPU implementation of FunASR CAM++.  It intentionally
// uses a compact project-owned weight format (CMPG) rather than Python's pickle
// checkpoint so production inference remains Go-only.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/asr"
	tensorops "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

const campPlusMagic = "CMPG\x01"

// Keep malformed model files from requesting unbounded allocations. Official
// CAM++ weights are far below this ceiling; this is a parser safety limit.
const maxCAMPlusFloats = 1 << 29

// DefaultCAMPlusFilename is the locally converted official FunASR CAM++
// checkpoint. Keep the binary out of Git; it is downloaded/provisioned with
// the application model assets.
const DefaultCAMPlusFilename = "campplus-cn-common.cmpg"

type tensor struct {
	shape []int
	data  []float32
}

// CAMPlus is an immutable CPU CAM++ embedding model. The model file is created
// with RapidSpeech.cpp/scripts/convert_campplus.py from the official Apache-2.0
// FunASR checkpoint. It exposes a 192-dimensional, L2-normalized embedding.
type CAMPlus struct {
	w       map[string]tensor
	fusedMu sync.Mutex
	fused   map[string]fusedPointwise
}

type fusedPointwise struct {
	weights []float32 // [out,in], BatchNorm scale folded into each row
	bias    []float32 // BatchNorm affine bias
	out     int
}

func LoadCAMPlus(path string) (*CAMPlus, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cam++ open weights: %w", err)
	}
	defer f.Close()
	r := bufio.NewReader(f)
	magic := make([]byte, len(campPlusMagic))
	if _, err = io.ReadFull(r, magic); err != nil {
		return nil, err
	}
	if string(magic) != campPlusMagic {
		return nil, fmt.Errorf("cam++ invalid weights format; convert the official checkpoint with scripts/convert_campplus.py")
	}
	var n uint32
	if err = binary.Read(r, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	if n == 0 || n > 2000 {
		return nil, fmt.Errorf("cam++ invalid tensor count %d", n)
	}
	m := &CAMPlus{w: make(map[string]tensor, n), fused: make(map[string]fusedPointwise)}
	for i := uint32(0); i < n; i++ {
		var l uint16
		if err = binary.Read(r, binary.LittleEndian, &l); err != nil {
			return nil, err
		}
		if l == 0 || l > 255 {
			return nil, fmt.Errorf("cam++ invalid tensor name")
		}
		b := make([]byte, l)
		if _, err = io.ReadFull(r, b); err != nil {
			return nil, err
		}
		var dims uint8
		if err = binary.Read(r, binary.LittleEndian, &dims); err != nil {
			return nil, err
		}
		if dims == 0 || dims > 4 {
			return nil, fmt.Errorf("cam++ invalid dimensions for %s", b)
		}
		sh := make([]int, dims)
		for j := range sh {
			var d uint32
			if err = binary.Read(r, binary.LittleEndian, &d); err != nil {
				return nil, err
			}
			if d == 0 || d > 1<<20 {
				return nil, fmt.Errorf("cam++ invalid dimension")
			}
			sh[j] = int(d)
		}
		size, err := camPlusTensorSize(sh)
		if err != nil {
			return nil, err
		}
		data := make([]float32, size)
		if err = binary.Read(r, binary.LittleEndian, data); err != nil {
			return nil, err
		}
		m.w[string(b)] = tensor{sh, data}
	}
	if _, ok := m.w["head.conv1.weight"]; !ok {
		return nil, fmt.Errorf("cam++ missing head.conv1.weight")
	}
	if _, ok := m.w["xvector.dense.linear.weight"]; !ok {
		return nil, fmt.Errorf("cam++ missing output layer")
	}
	return m, nil
}

// ValidateCAMPlusFile validates every tensor record without constructing the
// model in memory. It makes cache readiness meaningful: a valid header alone
// is insufficient when a download was interrupted after its first few bytes.
func ValidateCAMPlusFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cam++ open weights: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("cam++ stat weights: %w", err)
	}
	offset := int64(0)
	magic := make([]byte, len(campPlusMagic))
	if _, err = io.ReadFull(f, magic); err != nil {
		return fmt.Errorf("cam++ read header: %w", err)
	}
	if string(magic) != campPlusMagic {
		return fmt.Errorf("cam++ invalid weights format")
	}
	offset += int64(len(magic))
	var n uint32
	if err = binary.Read(f, binary.LittleEndian, &n); err != nil {
		return fmt.Errorf("cam++ read tensor count: %w", err)
	}
	if n == 0 || n > 2000 {
		return fmt.Errorf("cam++ invalid tensor count %d", n)
	}
	offset += 4
	hasHead, hasOutput := false, false
	for i := uint32(0); i < n; i++ {
		var nameLen uint16
		if err = binary.Read(f, binary.LittleEndian, &nameLen); err != nil || nameLen == 0 || nameLen > 255 {
			return fmt.Errorf("cam++ invalid tensor name")
		}
		offset += 2
		name := make([]byte, nameLen)
		if _, err = io.ReadFull(f, name); err != nil {
			return fmt.Errorf("cam++ read tensor name: %w", err)
		}
		offset += int64(nameLen)
		var dims uint8
		if err = binary.Read(f, binary.LittleEndian, &dims); err != nil || dims == 0 || dims > 4 {
			return fmt.Errorf("cam++ invalid dimensions for %s", name)
		}
		offset++
		shape := make([]int, dims)
		for j := range shape {
			var d uint32
			if err = binary.Read(f, binary.LittleEndian, &d); err != nil || d == 0 || d > 1<<20 {
				return fmt.Errorf("cam++ invalid dimension")
			}
			shape[j] = int(d)
			offset += 4
		}
		size, err := camPlusTensorSize(shape)
		if err != nil {
			return err
		}
		dataBytes := int64(size) * 4
		if dataBytes > info.Size()-offset {
			return fmt.Errorf("cam++ truncated tensor data")
		}
		if _, err = f.Seek(dataBytes, io.SeekCurrent); err != nil {
			return fmt.Errorf("cam++ skip tensor data: %w", err)
		}
		offset += dataBytes
		switch string(name) {
		case "head.conv1.weight":
			hasHead = true
		case "xvector.dense.linear.weight":
			hasOutput = true
		}
	}
	if !hasHead || !hasOutput {
		return fmt.Errorf("cam++ missing required tensor")
	}
	return nil
}

func camPlusTensorSize(shape []int) (int, error) {
	size := 1
	for _, d := range shape {
		if d <= 0 || size > maxCAMPlusFloats/d {
			return 0, fmt.Errorf("cam++ tensor is too large")
		}
		size *= d
	}
	return size, nil
}

func (m *CAMPlus) t(name string) tensor {
	t, ok := m.w[name]
	if !ok {
		panic("cam++ missing tensor " + name)
	}
	return t
}
func (m *CAMPlus) Embed(pcm []float32) (embedding []float32, err error) {
	// A CMPG file can have a syntactically valid record table yet omit a tensor
	// used by a later CAM++ block. Keep that model-data fault within the
	// diarization error path so the desktop can fall back to ordinary ASR rather
	// than letting a Wails/IM request crash the process.
	defer func() {
		if recovered := recover(); recovered != nil {
			embedding = nil
			err = fmt.Errorf("cam++ inference rejected invalid weights: %v", recovered)
		}
	}()
	if m == nil {
		return nil, fmt.Errorf("cam++ nil model")
	}
	f := asr.SpeakerFbank(pcm)
	if len(f) < 80*8 {
		return nil, fmt.Errorf("cam++ audio too short")
	}
	// The reference frontend (3D-Speaker FBank with mean_nor=True) subtracts the
	// per-utterance mean of each mel bin before the network. This also removes
	// any constant log-amplitude offset from recording level differences.
	frames := len(f) / 80
	for mel := 0; mel < 80; mel++ {
		var sum float64
		for t := 0; t < frames; t++ {
			sum += float64(f[t*80+mel])
		}
		mean := float32(sum / float64(frames))
		for t := 0; t < frames; t++ {
			f[t*80+mel] -= mean
		}
	}
	return m.embedFbank(f, frames), nil
}

// embedFbank is kept separate to enable parity tests against PyTorch feature
// dumps without requiring WAV decoding.
func (m *CAMPlus) embedFbank(feat []float32, frames int) []float32 {
	// [T,80] -> [1,1,80,T].  The CNN front end downsamples only frequency.
	x := make([]float32, 80*frames)
	for t := 0; t < frames; t++ {
		for f := 0; f < 80; f++ {
			x[f*frames+t] = feat[t*80+f]
		}
	}
	x = m.conv2d(x, 1, 80, frames, "head.conv1", "head.bn1", 1, 1, true)
	x = m.res2(x, 32, 80, frames, "head.layer1.0", 2)
	x = m.res2(x, 32, 40, frames, "head.layer1.1", 1)
	x = m.res2(x, 32, 40, frames, "head.layer2.0", 2)
	x = m.res2(x, 32, 20, frames, "head.layer2.1", 1)
	x = m.conv2d(x, 32, 20, frames, "head.conv2", "head.bn2", 2, 1, true)
	// [32,10,T] -> [320,T]
	T := frames
	x = m.conv1d(x, 320, T, "xvector.tdnn.linear", 2, 2)
	x = m.bnRelu(x, 128, (T+1)/2, "xvector.tdnn.nonlinear.batchnorm")
	T = (T + 1) / 2
	c := 128
	// Per-block dilations match the reference CAMPPlus: (12, 24, 16) layers with
	// dilations (1, 2, 2) — later dense blocks see a wider temporal context.
	dilations := []int{1, 2, 2}
	for block, layers := range []int{12, 24, 16} {
		prefix := fmt.Sprintf("xvector.block%d", block+1)
		for i := 1; i <= layers; i++ {
			y := m.denseLayer(x, c, T, fmt.Sprintf("%s.tdnnd%d", prefix, i), dilations[block])
			x = catChannels(x, c, y, 32, T)
			c += 32
		}
		x = m.transit(x, c, T, fmt.Sprintf("xvector.transit%d", block+1))
		c /= 2
	}
	x = m.bnRelu(x, c, T, "xvector.out_nonlinear.batchnorm")
	pooled := stats(x, c, T)
	out := m.conv1dBN(pooled, c*2, 1, "xvector.dense.linear", "xvector.dense.nonlinear.batchnorm", false)
	// DenseLayer's batchnorm_ is affine=False, so its checkpoint contains only
	// running statistics (no gamma/beta).
	normalize(out)
	return out
}

func (m *CAMPlus) res2(x []float32, c, h, w int, prefix string, stride int) []float32 {
	y := m.conv2d(x, c, h, w, prefix+".conv1", prefix+".bn1", stride, 1, true)
	oh := (h+2-3)/stride + 1
	y = m.conv2d(y, c, oh, w, prefix+".conv2", prefix+".bn2", 1, 1, false)
	sc := x
	if stride != 1 {
		sc = m.conv2d(x, c, h, w, prefix+".shortcut.0", "", stride, 0, false)
		sc = m.bn(sc, c, ((h-1)/stride+1)*w, prefix+".shortcut.1", false)
	}
	for i := range y {
		y[i] += sc[i]
		if y[i] < 0 {
			y[i] = 0
		}
	}
	return y
}
func (m *CAMPlus) conv2d(x []float32, in, h, w int, prefix, bnPrefix string, stride, pad int, relu bool) []float32 {
	wt := m.t(prefix + ".weight")
	out := wt.shape[0]
	kh, kw := wt.shape[2], wt.shape[3]
	// CAM++ FCM uses Conv2d strides of (2, 1): frequency is reduced while
	// the time axis is preserved. Its 3x3 kernels are never time-strided.
	oh, ow := (h+2*pad-kh)/stride+1, w+2*pad-kw+1
	// CAM++'s expensive residual convolutions are 3x3, C=32 and hundreds of
	// time frames wide. Lower them to a compact patch matrix and use the shared
	// SIMD GEMM implementation (AVX2/AVX-512 on x86, NEON on arm64). This has
	// more arithmetic per load than the scalar scattered-channel loop below and
	// avoids a branch for each kernel tap. 1x1 shortcuts remain scalar because
	// im2col would cost more than the small projection saves.
	if kh == 3 && kw == 3 && in >= 8 && out >= 8 {
		y := conv3x3SIMD(x, wt.data, in, h, w, out, oh, ow, stride, pad)
		if bnPrefix == "" {
			return y
		}
		return m.bn(y, out, oh*ow, bnPrefix, relu)
	}
	y := make([]float32, out*oh*ow)
	for oc := 0; oc < out; oc++ {
		for yy := 0; yy < oh; yy++ {
			for xx := 0; xx < ow; xx++ {
				var s float32
				for ic := 0; ic < in; ic++ {
					for ky := 0; ky < kh; ky++ {
						iy := yy*stride - pad + ky
						if iy < 0 || iy >= h {
							continue
						}
						for kx := 0; kx < kw; kx++ {
							ix := xx - pad + kx
							if ix >= 0 && ix < w {
								s += wt.data[((oc*in+ic)*kh+ky)*kw+kx] * x[(ic*h+iy)*w+ix]
							}
						}
					}
				}
				y[(oc*oh+yy)*ow+xx] = s
			}
		}
	}
	if bnPrefix == "" {
		return y
	}
	return m.bn(y, out, oh*ow, bnPrefix, relu)
}

// conv3x3SIMD materializes one small [oh*ow, in*3*3] patch matrix then calls
// tensor.MatMul. It is an operator-fusion boundary: the creation of padded
// patches and the convolution's matrix multiplication are performed in one
// cache-friendly pass, with no general-purpose tensor objects or reflection.
func conv3x3SIMD(x, weights []float32, in, h, w, out, oh, ow, stride, pad int) []float32 {
	locations, k := oh*ow, in*9
	patches := make([]float32, locations*k)
	row := 0
	for yy := 0; yy < oh; yy++ {
		baseY := yy*stride - pad
		for xx := 0; xx < ow; xx++ {
			p := patches[row*k : (row+1)*k]
			baseX := xx - pad
			at := 0
			for ic := 0; ic < in; ic++ {
				plane := x[ic*h*w : (ic+1)*h*w]
				for ky := 0; ky < 3; ky++ {
					iy := baseY + ky
					if iy >= 0 && iy < h {
						for kx := 0; kx < 3; kx++ {
							ix := baseX + kx
							if ix >= 0 && ix < w {
								p[at+kx] = plane[iy*w+ix]
							}
						}
					}
					at += 3
				}
			}
			row++
		}
	}
	timeMajor := make([]float32, locations*out)
	tensorops.MatMul(timeMajor, patches, weights, locations, out, k)
	y := make([]float32, out*locations)
	for oc := 0; oc < out; oc++ {
		dst := y[oc*locations : (oc+1)*locations]
		for pos := range dst {
			dst[pos] = timeMajor[pos*out+oc]
		}
	}
	return y
}
func (m *CAMPlus) conv1d(x []float32, in, T int, prefix string, stride, pad int) []float32 {
	wt := m.t(prefix + ".weight")
	out, k := wt.shape[0], wt.shape[2]
	// Almost all CAM++ TDNN projections are 1x1.  The model stores activations
	// channel-major ([C,T]), which makes the straightforward inner-product loop
	// stride through memory once per channel and defeats the CPU's SIMD units.
	// Pack each time frame contiguously, dispatch the projection to the shared
	// AVX2/AVX-512/NEON matrix kernel, then restore the channel-major layout used
	// by the remaining CAM++ operators. This is deliberately limited to 1x1
	// stride-1 projections: temporal 3/5-tap TDNNs retain the exact scalar path.
	if k == 1 && stride == 1 && pad == 0 {
		return conv1x1SIMD(x, wt.data, in, out, T)
	}
	ot := (T+2*pad-k)/stride + 1
	y := make([]float32, out*ot)
	for oc := 0; oc < out; oc++ {
		for t := 0; t < ot; t++ {
			var s float32
			for ic := 0; ic < in; ic++ {
				for z := 0; z < k; z++ {
					at := t*stride - pad + z
					if at >= 0 && at < T {
						s += wt.data[(oc*in+ic)*k+z] * x[ic*T+at]
					}
				}
			}
			y[oc*ot+t] = s
		}
	}
	return y
}

// conv1dPoint applies a 1x1 projection to a single time step. It is used by
// CAM's attention MLP (T=1), where packing through MatMul would be overhead.
// The 4-output unrolling gives the Go compiler independent accumulators and
// bounds-check-free sequential weight access.
func (m *CAMPlus) conv1dPoint(x []float32, in int, prefix string, relu bool) []float32 {
	wt := m.t(prefix + ".weight")
	bias, hasBias := m.w[prefix+".bias"]
	out := wt.shape[0]
	y := make([]float32, out)
	oc := 0
	for ; oc+3 < out; oc += 4 {
		w0, w1 := wt.data[oc*in:(oc+1)*in], wt.data[(oc+1)*in:(oc+2)*in]
		w2, w3 := wt.data[(oc+2)*in:(oc+3)*in], wt.data[(oc+3)*in:(oc+4)*in]
		var s0, s1, s2, s3 float32
		for i, v := range x[:in] {
			s0 += v * w0[i]
			s1 += v * w1[i]
			s2 += v * w2[i]
			s3 += v * w3[i]
		}
		if hasBias {
			s0 += bias.data[oc]
			s1 += bias.data[oc+1]
			s2 += bias.data[oc+2]
			s3 += bias.data[oc+3]
		}
		if relu {
			if s0 < 0 {
				s0 = 0
			}
			if s1 < 0 {
				s1 = 0
			}
			if s2 < 0 {
				s2 = 0
			}
			if s3 < 0 {
				s3 = 0
			}
		}
		y[oc], y[oc+1], y[oc+2], y[oc+3] = s0, s1, s2, s3
	}
	for ; oc < out; oc++ {
		var s float32
		for i, v := range x[:in] {
			s += v * wt.data[oc*in+i]
		}
		if hasBias {
			s += bias.data[oc]
		}
		if relu && s < 0 {
			s = 0
		}
		y[oc] = s
	}
	return y
}

// conv1x1SIMD calculates a pointwise [in,T] -> [out,T] convolution. Packing is
// an operator-layout transform, not an im2col expansion: it is O(in*T) memory
// and lets tensor.MatMul run vectorized dot-product microkernels for the much
// larger O(in*out*T) projection.
func conv1x1SIMD(x, weights []float32, in, out, T int) []float32 {
	packed := make([]float32, T*in) // [T,in]
	for ch := 0; ch < in; ch++ {
		src := x[ch*T : (ch+1)*T]
		for t, v := range src {
			packed[t*in+ch] = v
		}
	}
	timeMajor := make([]float32, T*out) // [T,out]
	tensorops.MatMul(timeMajor, packed, weights, T, out, in)
	y := make([]float32, out*T)
	for ch := 0; ch < out; ch++ {
		dst := y[ch*T : (ch+1)*T]
		for t := range dst {
			dst[t] = timeMajor[t*out+ch]
		}
	}
	return y
}

// conv1dBN fuses a pointwise TDNN projection with its following BatchNorm and
// optional ReLU. CAM++ uses this pattern for every DenseLayer's linear1 and
// final embedding projection. Folding scale into the weights eliminates the
// separate channel-major BN pass and lets MatMulBiasReLU vectorize the store.
func (m *CAMPlus) conv1dBN(x []float32, in, T int, convPrefix, bnPrefix string, relu bool) []float32 {
	wt := m.t(convPrefix + ".weight")
	if wt.shape[2] != 1 {
		y := m.conv1d(x, in, T, convPrefix, 1, 0)
		return m.bn(y, wt.shape[0], T, bnPrefix, relu)
	}
	f := m.fusedPointwise(convPrefix, bnPrefix, wt)
	packed := make([]float32, T*in)
	for ch := 0; ch < in; ch++ {
		src := x[ch*T : (ch+1)*T]
		for t, v := range src {
			packed[t*in+ch] = v
		}
	}
	timeMajor := make([]float32, T*f.out)
	if relu {
		tensorops.MatMulBiasReLU(timeMajor, packed, f.weights, f.bias, T, f.out, in)
	} else {
		tensorops.MatMulBias(timeMajor, packed, f.weights, f.bias, T, f.out, in)
	}
	y := make([]float32, f.out*T)
	for ch := 0; ch < f.out; ch++ {
		dst := y[ch*T : (ch+1)*T]
		for t := range dst {
			dst[t] = timeMajor[t*f.out+ch]
		}
	}
	return y
}

func (m *CAMPlus) fusedPointwise(convPrefix, bnPrefix string, wt tensor) fusedPointwise {
	key := convPrefix + "\x00" + bnPrefix
	m.fusedMu.Lock()
	defer m.fusedMu.Unlock()
	if f, ok := m.fused[key]; ok {
		return f
	}
	out, in := wt.shape[0], wt.shape[1]
	mean, vari := m.t(bnPrefix+".running_mean"), m.t(bnPrefix+".running_var")
	scaleWeight, hasWeight := m.w[bnPrefix+".weight"]
	beta, hasBias := m.w[bnPrefix+".bias"]
	f := fusedPointwise{weights: make([]float32, out*in), bias: make([]float32, out), out: out}
	for oc := 0; oc < out; oc++ {
		scale := float32(1) / float32(math.Sqrt(float64(vari.data[oc]+1e-5)))
		if hasWeight {
			scale *= scaleWeight.data[oc]
		}
		for ic := 0; ic < in; ic++ {
			f.weights[oc*in+ic] = wt.data[oc*in+ic] * scale
		}
		f.bias[oc] = -mean.data[oc] * scale
		if hasBias {
			f.bias[oc] += beta.data[oc]
		}
	}
	m.fused[key] = f
	return f
}
func (m *CAMPlus) bn(x []float32, c, T int, prefix string, relu bool) []float32 {
	w, hasWeight := m.w[prefix+".weight"]
	mean := m.t(prefix + ".running_mean")
	vari := m.t(prefix + ".running_var")
	b, hasB := m.w[prefix+".bias"]
	for ch := 0; ch < c; ch++ {
		scale := float32(1) / float32(math.Sqrt(float64(vari.data[ch]+1e-5)))
		if hasWeight {
			scale = w.data[ch] / float32(math.Sqrt(float64(vari.data[ch]+1e-5)))
		}
		bias := -mean.data[ch] * scale
		if hasB {
			bias += b.data[ch]
		}
		for t := 0; t < T; t++ {
			i := ch*T + t
			x[i] = x[i]*scale + bias
			if relu && x[i] < 0 {
				x[i] = 0
			}
		}
	}
	return x
}
func (m *CAMPlus) bnRelu(x []float32, c, T int, p string) []float32 { return m.bn(x, c, T, p, true) }
func (m *CAMPlus) denseLayer(x []float32, c, T int, p string, dilation int) []float32 {
	// The reference model's nonlinear1 produces a NEW tensor; the carried
	// dense-block input must stay unmodified for the later channel concat.
	// bnRelu normalizes in place, so clone first — otherwise accumulated
	// channels get re-normalized at every layer and activations blow up
	// exponentially (eventually NaN embeddings).
	z := m.bnRelu(append([]float32(nil), x...), c, T, p+".nonlinear1.batchnorm")
	z = m.conv1dBN(z, c, T, p+".linear1", p+".nonlinear2.batchnorm", true)
	// This 3-tap CAM projection preserves T (padding=dilation). CAM++ dense
	// blocks use per-block dilations (1, 2, 2) for a wider receptive field.
	local := m.conv1d3SIMD(z, 128, T, p+".cam_layer.linear_local", dilation)
	// CAM context (reference CAMLayer): per-frame value is the channel's
	// global mean plus its 100-frame segment mean, expanded back over time.
	ctx := make([]float32, 128*T)
	for ch := 0; ch < 128; ch++ {
		var sum float32
		for t := 0; t < T; t++ {
			sum += z[ch*T+t]
		}
		mean := sum / float32(T)
		for start := 0; start < T; start += 100 {
			end := min(start+100, T)
			var seg float32
			for t := start; t < end; t++ {
				seg += z[ch*T+t]
			}
			v := mean + seg/float32(end-start)
			for t := start; t < end; t++ {
				ctx[ch*T+t] = v
			}
		}
	}
	// Gate MLP is 1x1 convs over time: y *= sigmoid(linear2(relu(linear1(ctx)))).
	col := make([]float32, 128)
	for t := 0; t < T; t++ {
		for ch := 0; ch < 128; ch++ {
			col[ch] = ctx[ch*T+t]
		}
		a := m.conv1dPoint(col, 128, p+".cam_layer.linear1", true)
		a = m.conv1dPoint(a, 64, p+".cam_layer.linear2", false)
		for ch := 0; ch < 32; ch++ {
			gate := 1 / (1 + float32(math.Exp(float64(-a[ch]))))
			local[ch*T+t] *= gate
		}
	}
	return local
}

// conv1d3SIMD computes a 3-tap dilated Conv1d (stride=1, padding=dilation,
// preserving T). CAM++ dense blocks 2 and 3 use dilation=2 (receptive field
// t-2, t, t+2) — see 3D-Speaker CAMPPlus block dilations (1, 2, 2).
func (m *CAMPlus) conv1d3SIMD(x []float32, in, T int, prefix string, dilation int) []float32 {
	if dilation < 1 {
		dilation = 1
	}
	wt := m.t(prefix + ".weight")
	out := wt.shape[0]
	patches := make([]float32, T*in*3) // [T,in*3]
	for t := 0; t < T; t++ {
		p := patches[t*in*3 : (t+1)*in*3]
		for ch := 0; ch < in; ch++ {
			base := ch * 3
			if t-dilation >= 0 {
				p[base] = x[ch*T+t-dilation]
			}
			p[base+1] = x[ch*T+t]
			if t+dilation < T {
				p[base+2] = x[ch*T+t+dilation]
			}
		}
	}
	timeMajor := make([]float32, T*out)
	tensorops.MatMul(timeMajor, patches, wt.data, T, out, in*3)
	y := make([]float32, out*T)
	for ch := 0; ch < out; ch++ {
		dst := y[ch*T : (ch+1)*T]
		for t := range dst {
			dst[t] = timeMajor[t*out+ch]
		}
	}
	return y
}
func (m *CAMPlus) transit(x []float32, c, T int, p string) []float32 {
	x = m.bnRelu(x, c, T, p+".nonlinear.batchnorm")
	return m.conv1d(x, c, T, p+".linear", 1, 0)
}
func catChannels(a []float32, ca int, b []float32, cb, T int) []float32 {
	o := make([]float32, (ca+cb)*T)
	copy(o, a)
	copy(o[ca*T:], b)
	return o
}
func stats(x []float32, c, T int) []float32 {
	o := make([]float32, c*2)
	for ch := 0; ch < c; ch++ {
		var sum float32
		for t := 0; t < T; t++ {
			sum += x[ch*T+t]
		}
		mean := sum / float32(T)
		var varianceSum float32
		for t := 0; t < T; t++ {
			d := x[ch*T+t] - mean
			varianceSum += d * d
		}
		o[ch] = mean
		if T > 1 {
			o[c+ch] = float32(math.Sqrt(float64(varianceSum / float32(T-1))))
		}
	}
	return o
}
