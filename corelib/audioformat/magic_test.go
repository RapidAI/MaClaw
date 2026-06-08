package audioformat

import "testing"

func TestLooksLikeMP3FrameRejectsAACAndInvalidHeaders(t *testing.T) {
	if !LooksLikeMP3Frame([]byte{0xff, 0xfb, 0x90, 0x64}) {
		t.Fatal("valid mp3 frame header was rejected")
	}
	if LooksLikeMP3Frame([]byte{0xff, 0xf1, 0x50, 0x80}) {
		t.Fatal("adts aac header must not be treated as mp3")
	}
	if LooksLikeMP3Frame([]byte{0xff, 0xe0, 0x00, 0x00}) {
		t.Fatal("invalid mp3 frame header was accepted")
	}
	if LooksLikeMP3Frame([]byte{0xff, 0xfd, 0x90, 0x64}) {
		t.Fatal("mpeg layer ii frame header must not be treated as mp3")
	}
}

func TestLooksLikeMP3AcceptsID3OnlyWithFollowingFrame(t *testing.T) {
	if !LooksLikeMP3(append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}, []byte{0xff, 0xfb, 0x90, 0x64}...)) {
		t.Fatal("id3 tag followed by mp3 frame was rejected")
	}
	if LooksLikeMP3([]byte("ID3-not-a-real-mp3")) {
		t.Fatal("id3-only data must not be treated as mp3")
	}
	if LooksLikeMP3([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0x80, 0}) {
		t.Fatal("invalid id3 syncsafe size was accepted")
	}
}

func TestLooksLikeADTS(t *testing.T) {
	if !LooksLikeADTS([]byte{0xff, 0xf1, 0x50, 0x80}) {
		t.Fatal("adts aac header was rejected")
	}
	if LooksLikeADTS([]byte{0xff, 0xfb, 0x90, 0x64}) {
		t.Fatal("mp3 frame header must not be treated as adts")
	}
}
