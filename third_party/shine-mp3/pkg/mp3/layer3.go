package mp3

import (
	"fmt"
	"io"
)

const SHINE_MAX_SAMPLES = 1152

type channel int

const (
	PCM_MONO   channel = 1
	PCM_STEREO channel = 2
)

type mpegVersion int

const (
	MPEG_25 mpegVersion = 0
	MPEG_II mpegVersion = 2
	MPEG_I  mpegVersion = 3
)

type mpegLayer int

// Only Layer III currently implemented
const LAYER_III mpegLayer = 1

var mpegGranulesPerFrame = [4]int{
	// MPEG 2.5
	1,
	// Reserved
	-1,
	// MPEG II
	1,
	// MPEG I
	2,
}

func getMpegVersion(sampleRateIndex int) mpegVersion {
	if sampleRateIndex < 3 {
		return MPEG_I
	} else if sampleRateIndex < 6 {
		return MPEG_II
	} else {
		return MPEG_25
	}
}

// findSampleRateIndex checks if a given sampleRate is supported by the encoder
func findSampleRateIndex(freq int) int {
	var i int
	for i = 0; i < 9; i++ {
		if freq == int(sampleRates[i]) {
			return i
		}
	}
	return -1
}

// findBitrateIndex checks if a given bitrate is supported by the encoder
func findBitrateIndex(bitr int, mpeg_version mpegVersion) int {
	var i int
	for i = 0; i < 16; i++ {
		if bitr == int(bitRates[i][mpeg_version]) {
			return i
		}
	}
	return -1
}

// CheckConfig checks if a given bitrate and samplerate is supported by the encoder
func CheckConfig(freq int, bitr int) mpegVersion {
	var (
		samplerate_index int
		bitrate_index    int
	)
	samplerate_index = findSampleRateIndex(freq)
	if samplerate_index < 0 {
		return -1
	}
	mpeg_version := getMpegVersion(samplerate_index)
	bitrate_index = findBitrateIndex(bitr, mpeg_version)
	if bitrate_index < 0 {
		return -1
	}
	return mpeg_version
}

// samplesPerPass returns the audio samples expected in each frame.
func (enc *Encoder) samplesPerPass() int64 {
	return enc.Mpeg.GranulesPerFrame * GRANULE_SIZE
}
func NewEncoder(sampleRate, channels int) *Encoder {
	// Validate the sample rate only; the bitrate is chosen below (128 kbps is
	// not even in the MPEG-2.5 table, so gating on it here would reject the
	// 8/11.025/12 kHz rates outright).
	if sampleRate <= 0 || findSampleRateIndex(sampleRate) < 0 || channels < 1 || channels > 2 {
		return nil
	}

	enc := new(Encoder)

	if channels > 1 {
		enc.Mpeg.Mode = STEREO
	} else {
		enc.Mpeg.Mode = MONO
	}

	enc.subbandInitialize()
	enc.mdctInitialize()
	enc.loopInitialize()
	enc.Wave.Channels = int64(channels)
	enc.Wave.SampleRate = int64(sampleRate)
	enc.Mpeg.Emph = NONE
	enc.Mpeg.Copyright = 0
	enc.Mpeg.Original = 1
	enc.reservoirMaxSize = 0
	enc.reservoirSize = 0
	enc.Mpeg.Layer = int64(LAYER_III)
	enc.Mpeg.Crc = 0
	enc.Mpeg.Ext = 0
	enc.Mpeg.ModeExt = 0
	enc.Mpeg.BitsPerSlot = 8
	enc.Mpeg.SampleRateIndex = int64(findSampleRateIndex(int(enc.Wave.SampleRate)))
	enc.Mpeg.Version = getMpegVersion(int(enc.Mpeg.SampleRateIndex))
	enc.Mpeg.GranulesPerFrame = int64(mpegGranulesPerFrame[enc.Mpeg.Version])
	if enc.Mpeg.GranulesPerFrame == 2 {
		enc.sideInfoLen = (func() int64 {
			if enc.Wave.Channels == 1 {
				return 4 + 17
			}
			return 4 + 32
		}()) * 8
	} else {
		enc.sideInfoLen = (func() int64 {
			if enc.Wave.Channels == 1 {
				return 4 + 9
			}
			return 4 + 17
		}()) * 8
	}
	// Pick the highest table bitrate ≤ 128 kbps whose per-channel granule
	// budget still fits the 12-bit part2_3_length side-info field (max 4095
	// bits). Bigger frames — e.g. 128 kbps MPEG-II mono at 16 kHz, where the
	// budget is 4504 bits — get clamped to 4095 by maxReservoirBits, so the
	// unwritten remainder silently desyncs the whole stream.
	//
	// Slot math stays in integers: the float form rounds e.g. 112 kbps at
	// 16 kHz to 503.999…, which would leave FracSlotsPerFrame ≈ 1 and make
	// every frame claim a phantom padding slot.
	slotsDenom := enc.Wave.SampleRate * enc.Mpeg.BitsPerSlot
	enc.Mpeg.Bitrate = -1
	for idx := 15; idx > 0; idx-- {
		candidate := bitRates[idx][enc.Mpeg.Version]
		if candidate <= 0 || candidate > 128 {
			continue
		}
		slotsNum := enc.Mpeg.GranulesPerFrame * GRANULE_SIZE * candidate * 1000
		maxSlots := slotsNum / slotsDenom
		if slotsNum%slotsDenom != 0 {
			// Padding frames carry one extra slot; budget for the worst case.
			maxSlots++
		}
		meanBits := (maxSlots*enc.Mpeg.BitsPerSlot - enc.sideInfoLen) / enc.Mpeg.GranulesPerFrame
		if meanBits/enc.Wave.Channels <= 4095 {
			enc.Mpeg.Bitrate = candidate
			break
		}
	}
	if enc.Mpeg.Bitrate <= 0 {
		return nil
	}
	enc.Mpeg.BitrateIndex = int64(findBitrateIndex(int(enc.Mpeg.Bitrate), enc.Mpeg.Version))
	slotsNum := enc.Mpeg.GranulesPerFrame * GRANULE_SIZE * enc.Mpeg.Bitrate * 1000
	enc.Mpeg.WholeSlotsPerFrame = slotsNum / slotsDenom
	enc.Mpeg.FracSlotsPerFrame = float64(slotsNum%slotsDenom) / float64(slotsDenom)
	enc.Mpeg.Slot_lag = -enc.Mpeg.FracSlotsPerFrame
	if enc.Mpeg.FracSlotsPerFrame == 0 {
		enc.Mpeg.Padding = 0
	}
	enc.bitstream.open(BUFFER_SIZE)
	return enc
}
func (enc *Encoder) encodeBufferInternal(stride int) ([]uint8, int) {
	if enc.Mpeg.FracSlotsPerFrame != 0 {
		if enc.Mpeg.Slot_lag <= (enc.Mpeg.FracSlotsPerFrame - 1.0) {
			enc.Mpeg.Padding = 1
		} else {
			enc.Mpeg.Padding = 0
		}
		enc.Mpeg.Slot_lag += float64(enc.Mpeg.Padding) - enc.Mpeg.FracSlotsPerFrame
	}
	enc.Mpeg.BitsPerFrame = (enc.Mpeg.WholeSlotsPerFrame + enc.Mpeg.Padding) * 8
	enc.meanBits = (enc.Mpeg.BitsPerFrame - enc.sideInfoLen) / enc.Mpeg.GranulesPerFrame
	enc.mdctSub(int64(stride))
	enc.iterationLoop()
	enc.formatBitstream()
	written := enc.bitstream.dataPosition
	enc.bitstream.dataPosition = 0
	return enc.bitstream.data, written
}

func (enc *Encoder) encodeBufferInterleaved(data []int16) ([]uint8, int) {
	enc.buffer[0] = data
	if enc.Wave.Channels == 2 {
		enc.buffer[1] = data[1:]
	} else {
		enc.buffer[1] = nil
	}
	return enc.encodeBufferInternal(int(enc.Wave.Channels))
}

// Write encodes interleaved PCM16 samples as MP3 frames. It may be called
// multiple times to stream; each call encodes whole frames and zero-pads the
// final partial frame of that call's input, so chunk boundaries other than
// samplesPerPass*channels insert silence. Call Flush once after the final
// Write — otherwise the last frame is left incomplete in the bit-cache.
func (enc *Encoder) Write(out io.Writer, data []int16) error {
	samplesPerPass := int(enc.samplesPerPass())
	stride := int(enc.Wave.Channels)
	if stride < 1 {
		stride = 1
	}
	chunkSize := samplesPerPass * stride

	samplesRead := len(data)
	for i := 0; i < samplesRead; i += chunkSize {
		end := i + chunkSize
		if end > samplesRead {
			end = samplesRead
		}

		chunk := data[i:end]
		if len(chunk) == 0 {
			continue
		}
		if len(chunk) < chunkSize {
			padded := make([]int16, chunkSize)
			copy(padded, chunk)
			chunk = padded
		}

		// Encode and write the chunk to the output file.
		data, written := enc.encodeBufferInterleaved(chunk)
		if err := writeFull(out, data[:written]); err != nil {
			return err
		}
	}
	return nil
}

// Flush drains the bit-cache left over from Write. putBits only emits full
// 32-bit words, so up to 3 bytes of the final frame stay buffered after the
// last Write; without Flush the stream ends mid-frame. Every frame is
// slot-aligned (multiple of 8 bits), so the pending bits always make whole
// bytes and Flush can end the stream exactly on the last frame's boundary.
// It is terminal — do not Write after Flush.
func (enc *Encoder) Flush(out io.Writer) error {
	bs := &enc.bitstream
	pendingBits := 32 - bs.cacheBits
	if pendingBits%8 != 0 {
		return fmt.Errorf("mp3: stream ends on non-byte boundary (%d pending bits)", pendingBits)
	}
	for i := 0; i < pendingBits/8; i++ {
		// Cache is left-aligned: pending bits sit in the high positions.
		bs.data[bs.dataPosition] = uint8(bs.cache >> (24 - 8*i))
		bs.dataPosition++
	}
	bs.cache = 0
	bs.cacheBits = 32
	written := bs.dataPosition
	bs.dataPosition = 0
	if written == 0 {
		return nil
	}
	return writeFull(out, bs.data[:written])
}

// writeFull writes the whole buffer, treating a short write with a nil error
// as an error (io.Writer permits both).
func writeFull(out io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := out.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
