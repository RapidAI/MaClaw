package mp3

import (
	"encoding/binary"
	"io"
	"unsafe"
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
	var (
		avg_slots_per_frame float64
	)

	if sampleRate <= 0 || CheckConfig(sampleRate, 128) < 0 || channels < 1 || channels > 2 {
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
	enc.Mpeg.Bitrate = 128
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
	enc.Mpeg.BitrateIndex = int64(findBitrateIndex(int(enc.Mpeg.Bitrate), enc.Mpeg.Version))
	enc.Mpeg.GranulesPerFrame = int64(mpegGranulesPerFrame[enc.Mpeg.Version])
	avg_slots_per_frame = (float64(enc.Mpeg.GranulesPerFrame) * GRANULE_SIZE / (float64(enc.Wave.SampleRate))) * (float64(enc.Mpeg.Bitrate) * 1000 / float64(enc.Mpeg.BitsPerSlot))
	enc.Mpeg.WholeSlotsPerFrame = int64(avg_slots_per_frame)
	enc.Mpeg.FracSlotsPerFrame = avg_slots_per_frame - float64(enc.Mpeg.WholeSlotsPerFrame)
	enc.Mpeg.Slot_lag = -enc.Mpeg.FracSlotsPerFrame
	if enc.Mpeg.FracSlotsPerFrame == 0 {
		enc.Mpeg.Padding = 0
	}
	enc.bitstream.open(BUFFER_SIZE)
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

func (enc *Encoder) encodeBufferInterleaved(data *int16) ([]uint8, int) {
	enc.buffer[0] = data
	if enc.Wave.Channels == 2 {
		enc.buffer[1] = (*int16)(unsafe.Add(unsafe.Pointer(data), unsafe.Sizeof(int16(0))*1))
	}
	return enc.encodeBufferInternal(int(enc.Wave.Channels))
}

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
		data, written := enc.encodeBufferInterleaved(&chunk[0])
		err := binary.Write(out, binary.LittleEndian, data[:written])
		if err != nil {
			return err
		}
	}
	return nil
}
