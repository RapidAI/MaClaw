package tts

import (
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// PiperResBlock2Forward runs a Piper HiFi-GAN ResBlock.
// ONNX graph structure (verified):
//   residual = input (BEFORE LeakyReLU)
//   x = LeakyReLU(input) → DilatedConv(dil[0]) → x + residual
//   residual = x
//   x = LeakyReLU(x) → DilatedConv(dil[1]) → x + residual
func PiperResBlock2Forward(x []float32, ch, T int, rb *PiperResBlock, dilations []int) []float32 {
	for i := 0; i < len(rb.Convs); i++ {
		// Residual taken BEFORE LeakyReLU (critical: matches ONNX graph)
		residual := make([]float32, len(x))
		copy(residual, x)

		LeakyReLU(x, lreluSlope)
		c := &rb.Convs[i]
		dilation := 1
		if i < len(dilations) {
			dilation = dilations[i]
		}
		padding := (c.KSize - 1) * dilation / 2
		y := conv1DDilated(x, ch, T, c.Weight, c.Bias, c.KSize, ch, 1, padding, dilation)

		x = make([]float32, len(y))
		tensor.Add(x, residual, y)
	}
	return x
}

// PiperHiFiGANForward runs the Piper HiFi-GAN vocoder.
// z: [interChannels, T_mel] latent representation
// Returns: [T_audio] PCM waveform (float32, normalized to [-1, 1])
func PiperHiFiGANForward(z []float32, interCh, tMel int,
	voc *PiperVocoderWeights, hp PiperHParams) []float32 {

	ch := hp.UpsampleInitialChannel
	T := tMel

	// conv_pre: [interCh, T] → [initCh, T]
	x := Conv1D(z, interCh, T, voc.ConvPre.Weight, voc.ConvPre.KSize, ch, 1,
		(voc.ConvPre.KSize-1)/2, voc.ConvPre.Bias)

	nResKernels := len(hp.ResblockKernelSizes)

	for i, upRate := range hp.UpsampleRates {
		LeakyReLU(x, lreluSlope)

		up := &voc.Ups[i]
		newCh := ch / 2
		padding := (up.KSize - upRate) / 2
		x = ConvTranspose1D(x, ch, T, up.Weight, up.KSize, newCh, upRate, padding, up.Bias)
		ch = newCh
		T = T * upRate

		// ResBlocks: average of nResKernels parallel ResBlocks
		var sum []float32
		for j := 0; j < nResKernels; j++ {
			rbIdx := i*nResKernels + j
			rb := &voc.ResBlocks[rbIdx]

			// Dilations from ONNX graph (verified):
			// kernel=3: [1, 2], kernel=5: [2, 6], kernel=7: [3, 12]
			kSize := rb.Convs[0].KSize
			var dilations []int
			switch kSize {
			case 3:
				dilations = []int{1, 2}
			case 5:
				dilations = []int{2, 6}
			case 7:
				dilations = []int{3, 12}
			default:
				dilations = []int{1, 1}
			}

			xClone := make([]float32, len(x))
			copy(xClone, x)
			xClone = PiperResBlock2Forward(xClone, ch, T, rb, dilations)

			if sum == nil {
				sum = xClone
			} else {
				tensor.Add(sum, sum, xClone)
			}
		}
		scale := 1.0 / float32(nResKernels)
		tensor.Scale(sum, scale)
		x = sum
	}

	// Final: LeakyReLU → conv_post → tanh
	LeakyReLU(x, lreluSlope)
	audio := Conv1D(x, ch, T, voc.ConvPost.Weight, voc.ConvPost.KSize, 1, 1,
		(voc.ConvPost.KSize-1)/2, voc.ConvPost.Bias)
	tensor.Tanh(audio)

	// Mild peak limiter — prevent clipping without changing dynamics
	var peak float32
	for _, v := range audio {
		if v > peak { peak = v } else if -v > peak { peak = -v }
	}
	if peak > 0.95 {
		scale := float32(0.95) / peak
		for i := range audio {
			audio[i] *= scale
		}
	}

	return audio
}
