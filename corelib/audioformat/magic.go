package audioformat

import "bytes"

// LooksLikeMP3 reports whether data looks like MP3 audio, either as a bare
// MPEG Layer III frame or an ID3v2 tag followed by a plausible MP3 frame.
func LooksLikeMP3(data []byte) bool {
	if LooksLikeMP3Frame(data) {
		return true
	}
	if !bytes.HasPrefix(data, []byte("ID3")) || len(data) < 14 {
		return false
	}
	tagSize, ok := id3v2TagSize(data)
	if !ok {
		return false
	}
	frameOffset := 10 + tagSize
	if frameOffset < 10 || frameOffset+4 > len(data) {
		return false
	}
	return LooksLikeMP3Frame(data[frameOffset:])
}

// LooksLikeMP3Frame reports whether data begins with a plausible MPEG Layer III
// frame header. It intentionally rejects ADTS AAC, which also starts with 0xff.
func LooksLikeMP3Frame(data []byte) bool {
	if len(data) < 4 || data[0] != 0xff || data[1]&0xe0 != 0xe0 || LooksLikeADTS(data) {
		return false
	}
	version := (data[1] >> 3) & 0x03
	layer := (data[1] >> 1) & 0x03
	bitrate := (data[2] >> 4) & 0x0f
	sampleRate := (data[2] >> 2) & 0x03
	return version != 0x01 && layer == 0x01 && bitrate != 0x00 && bitrate != 0x0f && sampleRate != 0x03
}

// LooksLikeADTS reports whether data begins with an AAC ADTS sync header.
func LooksLikeADTS(data []byte) bool {
	return len(data) >= 2 && data[0] == 0xff && data[1]&0xf6 == 0xf0
}

func id3v2TagSize(data []byte) (int, bool) {
	if len(data) < 10 || !bytes.HasPrefix(data, []byte("ID3")) {
		return 0, false
	}
	sizeBytes := data[6:10]
	for _, b := range sizeBytes {
		if b&0x80 != 0 {
			return 0, false
		}
	}
	size := int(sizeBytes[0])<<21 | int(sizeBytes[1])<<14 | int(sizeBytes[2])<<7 | int(sizeBytes[3])
	return size, true
}
