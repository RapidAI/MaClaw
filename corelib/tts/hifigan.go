package tts

import (
	"math"
	"runtime"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

const lreluSlope = 0.1

// ── ResBlock1 ──

// ResBlock1Forward runs a HiFi-GAN ResBlock1.
// input: [ch, T], output: [ch, T] (same shape, residual connection).
// Each ResBlock1 has 3 dilated conv pairs: (convs1[i], convs2[i]).
// Pattern: x → LeakyReLU → DilatedConv1 → LeakyReLU → Conv1 → + residual
func ResBlock1Forward(x []float32, ch, T int, rb *ResBlock, dilations []int) []float32 {
	for i := 0; i < 3; i++ {
		residual := make([]float32, len(x))
		copy(residual, x)

		// LeakyReLU → dilated Conv1d
		LeakyReLU(x, lreluSlope)
		c1 := &rb.Convs1[i]
		dilation := dilations[i]
		padding := (c1.KSize - 1) * dilation / 2
		y := conv1DDilated(x, ch, T, c1.Weight, c1.Bias, c1.KSize, ch, 1, padding, dilation)

		// LeakyReLU → Conv1d (dilation=1)
		LeakyReLU(y, lreluSlope)
		c2 := &rb.Convs2[i]
		padding2 := (c2.KSize - 1) / 2
		z := Conv1D(y, ch, T, c2.Weight, c2.KSize, ch, 1, padding2, c2.Bias)

		// Residual
		tensor.Add(x, residual, z)
	}
	return x
}

// conv1DDilated computes dilated 1D convolution.
// For dilation=1, delegates to the optimized Conv1D.
func conv1DDilated(input []float32, inCh, inLen int,
	kernel []float32, bias []float32, kSize, outCh, stride, padding, dilation int) []float32 {

	// Fast path: dilation=1 → use optimized Conv1D
	if dilation == 1 {
		return Conv1D(input, inCh, inLen, kernel, kSize, outCh, stride, padding, bias)
	}

	effKSize := (kSize-1)*dilation + 1
	paddedLen := inLen + 2*padding
	outLen := (paddedLen - effKSize) / stride + 1
	if outLen <= 0 {
		return nil
	}
	out := make([]float32, outCh*outLen)

	// Middle region: no boundary checks needed
	// inStart = o*stride - padding; need inStart >= 0 and inStart + (kSize-1)*dilation < inLen
	midStart := (padding + stride - 1) / stride
	midEnd := (inLen - 1 - (kSize-1)*dilation + padding) / stride + 1
	if midStart < 0 {
		midStart = 0
	}
	if midEnd > outLen {
		midEnd = outLen
	}
	if midEnd < midStart {
		midEnd = midStart
	}

	nWorkers := runtime.NumCPU()
	if nWorkers > outCh {
		nWorkers = outCh
	}

	// For SIMD: transpose input once before parallelization
	var inputT []float32
	useSIMD := inCh >= 16
	if useSIMD {
		inputT = transposeInput(input, inCh, inLen)
	}

	dispatchRange := func(s, e int) {
		if useSIMD {
			conv1DDilatedRangeSIMDWithTranspose(input, inputT, kernel, out, bias, s, e, inCh, inLen, kSize, outLen, stride, padding, dilation, midStart, midEnd, false)
		} else {
			conv1DDilatedRange(input, kernel, out, bias, s, e, inCh, inLen, kSize, outLen, stride, padding, dilation, midStart, midEnd)
		}
	}

	if nWorkers <= 1 || outCh < 4 {
		dispatchRange(0, outCh)
		return out
	}

	var wg sync.WaitGroup
	chunk := (outCh + nWorkers - 1) / nWorkers
	for w := 0; w < nWorkers; w++ {
		s, e := w*chunk, (w+1)*chunk
		if e > outCh {
			e = outCh
		}
		if s >= e {
			break
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			dispatchRange(s, e)
		}(s, e)
	}
	wg.Wait()
	return out
}

func conv1DDilatedRange(input, kernel, out []float32, bias []float32,
	ocStart, ocEnd, inCh, inLen, kSize, outLen, stride, padding, dilation, midStart, midEnd int) {
	for oc := ocStart; oc < ocEnd; oc++ {
		var b float64
		if bias != nil {
			b = float64(bias[oc])
		}
		ocKernBase := oc * inCh * kSize
		outBase := oc * outLen

		// Left boundary
		for o := 0; o < midStart; o++ {
			var sum float64
			inStart := o*stride - padding
			for ic := 0; ic < inCh; ic++ {
				kOff := ocKernBase + ic*kSize
				iOff := ic * inLen
				for k := 0; k < kSize; k++ {
					inPos := inStart + k*dilation
					if inPos >= 0 && inPos < inLen {
						sum += float64(input[iOff+inPos]) * float64(kernel[kOff+k])
					}
				}
			}
			out[outBase+o] = float32(sum + b)
		}

		// Middle (no bounds checking) — hot path, optimized per kSize
		switch kSize {
		case 3:
			for o := midStart; o < midEnd; o++ {
				var sum float32
				inStart := o*stride - padding
				for ic := 0; ic < inCh; ic++ {
					kOff := ocKernBase + ic*3
					iOff := ic*inLen + inStart
					sum += input[iOff] * kernel[kOff]
					sum += input[iOff+dilation] * kernel[kOff+1]
					sum += input[iOff+dilation*2] * kernel[kOff+2]
				}
				out[outBase+o] = sum + float32(b)
			}
		case 5:
			for o := midStart; o < midEnd; o++ {
				var sum float32
				inStart := o*stride - padding
				for ic := 0; ic < inCh; ic++ {
					kOff := ocKernBase + ic*5
					iOff := ic*inLen + inStart
					sum += input[iOff] * kernel[kOff]
					sum += input[iOff+dilation] * kernel[kOff+1]
					sum += input[iOff+dilation*2] * kernel[kOff+2]
					sum += input[iOff+dilation*3] * kernel[kOff+3]
					sum += input[iOff+dilation*4] * kernel[kOff+4]
				}
				out[outBase+o] = sum + float32(b)
			}
		case 7:
			for o := midStart; o < midEnd; o++ {
				var sum float32
				inStart := o*stride - padding
				for ic := 0; ic < inCh; ic++ {
					kOff := ocKernBase + ic*7
					iOff := ic*inLen + inStart
					sum += input[iOff] * kernel[kOff]
					sum += input[iOff+dilation] * kernel[kOff+1]
					sum += input[iOff+dilation*2] * kernel[kOff+2]
					sum += input[iOff+dilation*3] * kernel[kOff+3]
					sum += input[iOff+dilation*4] * kernel[kOff+4]
					sum += input[iOff+dilation*5] * kernel[kOff+5]
					sum += input[iOff+dilation*6] * kernel[kOff+6]
				}
				out[outBase+o] = sum + float32(b)
			}
		default:
			for o := midStart; o < midEnd; o++ {
				var sum float64
				inStart := o*stride - padding
				for ic := 0; ic < inCh; ic++ {
					kOff := ocKernBase + ic*kSize
					iOff := ic*inLen + inStart
					for k := 0; k < kSize; k++ {
						sum += float64(input[iOff+k*dilation]) * float64(kernel[kOff+k])
					}
				}
				out[outBase+o] = float32(sum + b)
			}
		}

		// Right boundary
		for o := midEnd; o < outLen; o++ {
			var sum float64
			inStart := o*stride - padding
			for ic := 0; ic < inCh; ic++ {
				kOff := ocKernBase + ic*kSize
				iOff := ic * inLen
				for k := 0; k < kSize; k++ {
					inPos := inStart + k*dilation
					if inPos >= 0 && inPos < inLen {
						sum += float64(input[iOff+inPos]) * float64(kernel[kOff+k])
					}
				}
			}
			out[outBase+o] = float32(sum + b)
		}
	}
}

// ── HiFi-GAN Generator Forward ──

// HiFiGANForward runs the HiFi-GAN vocoder.
// z: [interChannels, T_mel] latent representation
// g: [ginChannels, 1] speaker embedding (broadcast over time)
// Returns: [1, T_audio] PCM waveform where T_audio = T_mel * product(upsampleRates)
func HiFiGANForward(z []float32, interCh, tMel int,
	g []float32, ginCh int,
	voc *VocoderWeights, hp HParams) []float32 {

	ch := hp.UpsampleInitialChannel // 512
	T := tMel

	// conv_pre: [interCh, T] → [512, T]
	x := Conv1D(z, interCh, T, voc.ConvPre.Weight, voc.ConvPre.KSize, ch, 1,
		(voc.ConvPre.KSize-1)/2, voc.ConvPre.Bias)

	// Speaker conditioning: x += cond(g)
	if g != nil && voc.Cond.Weight != nil {
		// cond: [ginCh, 1] → [upsampleInitCh, 1]
		gProj := Conv1D(g, ginCh, 1, voc.Cond.Weight, 1, ch, 1, 0, voc.Cond.Bias)
		// Broadcast add over time
		for c := 0; c < ch; c++ {
			gVal := gProj[c]
			for t := 0; t < T; t++ {
				x[c*T+t] += gVal
			}
		}
	}

	nResKernels := len(hp.ResblockKernelSizes)

	for i, upRate := range hp.UpsampleRates {
		// LeakyReLU
		LeakyReLU(x, lreluSlope)

		// ConvTranspose1d upsample
		up := &voc.Ups[i]
		newCh := ch / 2
		padding := (up.KSize - upRate) / 2
		x = ConvTranspose1D(x, ch, T, up.Weight, up.KSize, newCh, upRate, padding, up.Bias)
		ch = newCh
		T = T * upRate // output length after upsample

		// ResBlocks: average of nResKernels parallel ResBlocks
		var sum []float32
		for j := 0; j < nResKernels; j++ {
			rbIdx := i*nResKernels + j
			rb := &voc.ResBlocks[rbIdx]
			dilations := hp.ResblockDilationSizes[j]

			// Clone x for each resblock (they run in parallel on same input)
			xClone := make([]float32, len(x))
			copy(xClone, x)
			xClone = ResBlock1Forward(xClone, ch, T, rb, dilations)

			if sum == nil {
				sum = xClone
			} else {
				tensor.Add(sum, sum, xClone)
			}
		}
		// Average
		scale := 1.0 / float32(nResKernels)
		tensor.Scale(sum, scale)
		x = sum
	}

	// Final: LeakyReLU → conv_post → tanh
	LeakyReLU(x, lreluSlope)
	audio := Conv1D(x, ch, T, voc.ConvPost.Weight, voc.ConvPost.KSize, 1, 1,
		(voc.ConvPost.KSize-1)/2, voc.ConvPost.Bias)
	tensor.Tanh(audio)

	return audio
}

// ── WAV encoding ──

// EncodeWAV converts float32 PCM [-1,1] to a WAV file byte slice.
func EncodeWAV(samples []float32, sampleRate int) []byte {
	nSamples := len(samples)
	dataSize := nSamples * 2 // 16-bit PCM
	fileSize := 44 + dataSize

	buf := make([]byte, fileSize)
	copy(buf[0:4], "RIFF")
	le32(buf[4:], uint32(fileSize-8))
	copy(buf[8:12], "WAVE")

	// fmt chunk
	copy(buf[12:16], "fmt ")
	le32(buf[16:], 16) // chunk size
	le16(buf[20:], 1)  // PCM format
	le16(buf[22:], 1)  // mono
	le32(buf[24:], uint32(sampleRate))
	le32(buf[28:], uint32(sampleRate*2)) // byte rate
	le16(buf[32:], 2)                    // block align
	le16(buf[34:], 16)                   // bits per sample

	// data chunk
	copy(buf[36:40], "data")
	le32(buf[40:], uint32(dataSize))

	for i, s := range samples {
		// Clamp to [-1, 1]
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		v := int16(math.Round(float64(s) * 32767))
		le16(buf[44+i*2:], uint16(v))
	}
	return buf
}

func le32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func le16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}
