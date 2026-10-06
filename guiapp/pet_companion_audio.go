package guiapp

import "math"

const petSampleRate = 16000

type petAudioEvent struct {
	speechStart bool
	utterance   []int16
	rms         float64
}

// petSegmenter commits a phrase after about one second of silence, and keeps
// a short pre-roll so the first syllable is not clipped.
type petSegmenter struct {
	playback      bool
	inSpeech      bool
	silence       int
	preRoll       []int16
	current       []int16
	commitSamples int
	preRollMax    int
}

func newPetSegmenter() *petSegmenter {
	return &petSegmenter{
		commitSamples: int(petSilenceCommitSec * petSampleRate),
		preRollMax:    petSampleRate * 3 / 10,
	}
}

func (s *petSegmenter) setPlayback(on bool) {
	if s == nil {
		return
	}
	s.playback = on
}

func (s *petSegmenter) push(frame []int16) (petAudioEvent, bool) {
	if s == nil || len(frame) == 0 {
		return petAudioEvent{}, false
	}
	rms := petPCMRMS(frame)
	threshold := 0.012
	if s.playback {
		threshold = 0.04
	}
	voiced := rms >= threshold
	if !s.inSpeech {
		s.preRoll = append(s.preRoll, frame...)
		if len(s.preRoll) > s.preRollMax {
			s.preRoll = append([]int16(nil), s.preRoll[len(s.preRoll)-s.preRollMax:]...)
		}
		if !voiced {
			return petAudioEvent{}, false
		}
		s.inSpeech = true
		s.silence = 0
		s.current = append(append([]int16{}, s.preRoll...), frame...)
		s.preRoll = nil
		return petAudioEvent{speechStart: true}, true
	}
	s.current = append(s.current, frame...)
	if voiced {
		s.silence = 0
		return petAudioEvent{}, false
	}
	s.silence += len(frame)
	if s.silence < s.commitSamples {
		return petAudioEvent{}, false
	}
	utterance := append([]int16(nil), s.current...)
	level := petPCMRMS(utterance)
	s.inSpeech = false
	s.silence = 0
	s.current = nil
	s.preRoll = nil
	return petAudioEvent{utterance: utterance, rms: level}, true
}

func petPCMRMS(frame []int16) float64 {
	if len(frame) == 0 {
		return 0
	}
	var sum float64
	for _, sample := range frame {
		v := float64(sample) / 32768
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(frame)))
}

func pcm16ToWAV(pcm []int16) []byte {
	dataBytes := len(pcm) * 2
	buf := make([]byte, 44+dataBytes)
	copy(buf[0:], []byte("RIFF"))
	petPutLE32(buf[4:], uint32(36+dataBytes))
	copy(buf[8:], []byte("WAVE"))
	copy(buf[12:], []byte("fmt "))
	petPutLE32(buf[16:], 16)
	petPutLE16(buf[20:], 1)
	petPutLE16(buf[22:], 1)
	petPutLE32(buf[24:], petSampleRate)
	petPutLE32(buf[28:], petSampleRate*2)
	petPutLE16(buf[32:], 2)
	petPutLE16(buf[34:], 16)
	copy(buf[36:], []byte("data"))
	petPutLE32(buf[40:], uint32(dataBytes))
	for i, sample := range pcm {
		petPutLE16(buf[44+i*2:], uint16(sample))
	}
	return buf
}

func petPutLE16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func petPutLE32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
