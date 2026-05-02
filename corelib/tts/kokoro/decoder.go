package kokoro

import "fmt"

type DecoderFeatures struct {
	X      []float32 // [512, T]
	Frames int
	Style  []float32 // selected voice style [256]
}

func (m *Model) DecoderPreGenerator(cond *Conditioning, f0n *F0NResult, voice *TensorFile) (*DecoderFeatures, error) {
	if cond == nil || f0n == nil {
		return nil, fmt.Errorf("kokoro: missing decoder inputs")
	}
	style, err := m.SelectVoiceStyle(voice, len(cond.Durations)-2)
	if err != nil {
		return nil, err
	}
	s := style[:128]
	asr := timeMajorToChannelMajor(cond.Text, cond.Frames, 512)
	f0In := make([]float32, f0n.Frames)
	copy(f0In, f0n.F0)
	nIn := make([]float32, f0n.Frames)
	copy(nIn, f0n.Noise)
	f0, err := m.weightNormConv1D(f0In, 1, f0n.Frames, 1, 3, 2, 1, 1, 1, "decoder.module.F0_conv")
	if err != nil {
		return nil, err
	}
	n, err := m.weightNormConv1D(nIn, 1, f0n.Frames, 1, 3, 2, 1, 1, 1, "decoder.module.N_conv")
	if err != nil {
		return nil, err
	}
	frames := len(f0)
	if frames != cond.Frames || len(n) != frames {
		return nil, fmt.Errorf("kokoro: decoder F0/N frames=%d/%d want conditioning=%d", frames, len(n), cond.Frames)
	}
	x := concatChannels([]channelTensor{{asr, 512}, {f0, 1}, {n, 1}}, frames)
	x, frames, _, err = m.adainResBlk1d(x, frames, 514, 1024, s, "decoder.module.encode", false)
	if err != nil {
		return nil, err
	}
	asrRes, err := m.weightNormConv1D(asr, 512, frames, 64, 1, 1, 0, 1, 1, "decoder.module.asr_res.0")
	if err != nil {
		return nil, err
	}
	res := true
	inC := 1024
	for i := 0; i < 4; i++ {
		if res {
			x = concatChannels([]channelTensor{{x, inC}, {asrRes, 64}, {f0, 1}, {n, 1}}, frames)
			inC += 66
		}
		outC := 1024
		upsample := false
		if i == 3 {
			outC = 512
			upsample = true
		}
		x, frames, inC, err = m.adainResBlk1d(x, frames, inC, outC, s, fmt.Sprintf("decoder.module.decode.%d", i), upsample)
		if err != nil {
			return nil, err
		}
		if upsample {
			res = false
		}
	}
	return &DecoderFeatures{X: x, Frames: frames, Style: append([]float32(nil), style...)}, nil
}

type channelTensor struct {
	Data     []float32
	Channels int
}

func concatChannels(parts []channelTensor, frames int) []float32 {
	totalC := 0
	for _, p := range parts {
		totalC += p.Channels
	}
	out := make([]float32, totalC*frames)
	offC := 0
	for _, p := range parts {
		for c := 0; c < p.Channels; c++ {
			copy(out[(offC+c)*frames:(offC+c+1)*frames], p.Data[c*frames:(c+1)*frames])
		}
		offC += p.Channels
	}
	return out
}
