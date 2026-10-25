package formula

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"strings"
	"testing"
)

func TestLayoutSize(t *testing.T) {
	// 100x30: short side 384, then thumbnail long side 384 → 384x115.
	_, _, w, h := layoutSize(100, 30, canvas)
	if w != 384 || h != 115 {
		t.Fatalf("wide layout %dx%d", w, h)
	}
	_, _, w, h = layoutSize(30, 100, canvas)
	if w != 115 || h != 384 {
		t.Fatalf("tall layout %dx%d", w, h)
	}
	_, _, w, h = layoutSize(10, 10, canvas)
	if w != 384 || h != 384 {
		t.Fatalf("square layout %dx%d", w, h)
	}
}

func TestPreprocessWhite(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 255, 255, 255, 255
	}
	got, err := preprocess(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Shape) != 4 || got.Shape[0] != 1 || got.Shape[1] != 3 || got.Shape[2] != canvas || got.Shape[3] != canvas {
		t.Fatalf("shape %v", got.Shape)
	}
	want := (float32(255)/255 - unimerMean) / unimerStd
	for i, v := range got.F32 {
		if math.Abs(float64(v-want)) > 1e-5 {
			t.Fatalf("pixel %d = %g, want %g", i, v, want)
		}
	}
}

func TestPreprocessCropsWhiteMargin(t *testing.T) {
	// 80x80 white with a 20x20 black square. Cropping leaves only the
	// square, which upscales to a black canvas. Without the crop the
	// white margin would dominate.
	img := image.NewRGBA(image.Rect(0, 0, 80, 80))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 255, 255, 255, 255
	}
	for y := 30; y < 50; y++ {
		for x := 30; x < 50; x++ {
			i := y*img.Stride + x*4
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 0, 0, 0, 255
		}
	}
	got, err := preprocess(img)
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, v := range got.F32 {
		sum += float64(v)
	}
	mean := sum / float64(len(got.F32))
	if mean > -3 {
		t.Fatalf("mean %g, white margin was not cropped", mean)
	}
}

func TestPreprocessPadsGray(t *testing.T) {
	// 40x20 gray content inside white. After the crop the long side is
	// 384 and the short side is padded with black.
	img := image.NewRGBA(image.Rect(0, 0, 80, 80))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 255, 255, 255, 255
	}
	for y := 30; y < 50; y++ {
		for x := 20; x < 60; x++ {
			i := y*img.Stride + x*4
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 100, 100, 100, 255
		}
	}
	got, err := preprocess(img)
	if err != nil {
		t.Fatal(err)
	}
	black := (float32(0)/255 - unimerMean) / unimerStd
	gray := (float32(100)/255 - unimerMean) / unimerStd
	corner := got.F32[0]
	center := got.F32[canvas/2*canvas+canvas/2]
	if math.Abs(float64(corner-black)) > 1e-4 {
		t.Fatalf("corner %g, want pad black %g", corner, black)
	}
	if math.Abs(float64(center-gray)) > 1e-3 {
		t.Fatalf("center %g, want gray %g", center, gray)
	}
}

func TestImageTooLarge(t *testing.T) {
	if err := imageTooLarge(maxImageSide, maxImagePixels/maxImageSide); err != nil {
		t.Fatal(err)
	}
	if err := imageTooLarge(maxImageSide, maxImagePixels/maxImageSide+1); err == nil {
		t.Fatal("pixel cap accepted an extra row")
	}
	if err := imageTooLarge(maxImageSide+1, 1); err == nil {
		t.Fatal("side cap accepted an extra column")
	}
	if err := imageTooLarge(0, 8); err == nil || !strings.Contains(err.Error(), "empty image") {
		t.Fatalf("empty: %v", err)
	}
}

func TestPreprocessRejectsHugeBoundsBeforePixels(t *testing.T) {
	_, err := preprocess(unreadImage{w: maxImageSide + 1, h: 1})
	if err == nil || !strings.Contains(err.Error(), "12001×1") {
		t.Fatalf("side: %v", err)
	}
	_, err = preprocess(unreadImage{w: 0, h: 8})
	if err == nil || !strings.Contains(err.Error(), "empty image") {
		t.Fatalf("empty: %v", err)
	}
}

func TestDecodeImageRejectsDeclaredSize(t *testing.T) {
	_, err := DecodeImage(bytes.NewReader(pngIHDR(100_000, 100_000)))
	if err == nil || !strings.Contains(err.Error(), "100000×100000") {
		t.Fatalf("header bomb: %v", err)
	}
	_, err = DecodeImage(bytes.NewReader(pngIHDR(10_000, 5_000)))
	if err == nil || !strings.Contains(err.Error(), "10000×5000") {
		t.Fatalf("pixel bomb: %v", err)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	img, err := DecodeImage(readerOnly{bytes.NewReader(buf.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 1 || img.Bounds().Dy() != 1 {
		t.Fatalf("bounds %v", img.Bounds())
	}
}

func TestDecodeImageRejectsEncodedSize(t *testing.T) {
	huge := &sizeSeeker{size: maxImageBytes + 1}
	_, err := DecodeImage(huge)
	if err == nil || huge.read || !strings.Contains(err.Error(), "limit is") {
		t.Fatalf("seeker read=%v err=%v", huge.read, err)
	}
	stream := &byteStream{limit: int(maxImageBytes) + 1}
	_, err = DecodeImage(stream)
	if err == nil || stream.n != int(maxImageBytes)+1 || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("stream n=%d err=%v", stream.n, err)
	}
}

// unreadImage panics if a pixel is read, so a size rejection that still
// allocates or samples the bitmap fails the test.
type unreadImage struct{ w, h int }

func (u unreadImage) ColorModel() color.Model { return color.RGBAModel }
func (u unreadImage) Bounds() image.Rectangle { return image.Rect(0, 0, u.w, u.h) }
func (u unreadImage) At(int, int) color.Color { panic("preprocess read pixels") }

type readerOnly struct{ io.Reader }

type sizeSeeker struct {
	size int64
	read bool
}

func (s *sizeSeeker) Read([]byte) (int, error) {
	s.read = true
	return 0, io.EOF
}

func (s *sizeSeeker) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		return off, nil
	case io.SeekCurrent:
		return 0, nil
	case io.SeekEnd:
		return s.size, nil
	default:
		return 0, io.ErrUnexpectedEOF
	}
}

type byteStream struct {
	n, limit int
}

func (s *byteStream) Read(p []byte) (int, error) {
	if s.n >= s.limit {
		return 0, io.EOF
	}
	if len(p) > s.limit-s.n {
		p = p[:s.limit-s.n]
	}
	s.n += len(p)
	return len(p), nil
}

func pngIHDR(w, h uint32) []byte {
	payload := make([]byte, 13)
	binary.BigEndian.PutUint32(payload[0:4], w)
	binary.BigEndian.PutUint32(payload[4:8], h)
	payload[8] = 8
	payload[9] = 2
	var b bytes.Buffer
	b.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	var lenb [4]byte
	binary.BigEndian.PutUint32(lenb[:], uint32(len(payload)))
	b.Write(lenb[:])
	chunk := append([]byte("IHDR"), payload...)
	b.Write(chunk)
	var crcb [4]byte
	binary.BigEndian.PutUint32(crcb[:], crc32.ChecksumIEEE(chunk))
	b.Write(crcb[:])
	return b.Bytes()
}
