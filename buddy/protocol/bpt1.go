package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// AssetModeBPT1 is the hello asset_mode value for raw pet frames.
// Devices that omit it still receive base64 JSON chunks.
const AssetModeBPT1 = "bpt1"

const (
	bpt1Magic   = "BPT1"
	bpt1Prefix  = 28
	BPT1MaxData = 1024
)

// BPT1Chunk is one raw pet-asset slice. The byte layout matches
// buddy/firmware/main/buddy_bpt1.h.
type BPT1Chunk struct {
	Frame   int
	Frames  int
	Width   int
	Height  int
	FrameMS int
	Offset  int
	Total   int
	CRC32   uint32
	Data    []byte
}

// EncodeBPT1 serialises one chunk. The result has no trailing newline so it
// can sit between JSON lines on the same USB stream.
func EncodeBPT1(c BPT1Chunk) ([]byte, error) {
	if len(c.Data) == 0 || len(c.Data) > BPT1MaxData {
		return nil, fmt.Errorf("buddy: bpt1 data %d bytes outside 1..%d", len(c.Data), BPT1MaxData)
	}
	if c.Frame < 0 || c.Frame > 255 || c.Frames < 1 || c.Frames > 255 {
		return nil, errors.New("buddy: bpt1 frame out of range")
	}
	buf := make([]byte, bpt1Prefix+len(c.Data))
	copy(buf, bpt1Magic)
	buf[4] = byte(c.Frame)
	buf[5] = byte(c.Frames)
	binary.LittleEndian.PutUint16(buf[6:], uint16(c.Width))
	binary.LittleEndian.PutUint16(buf[8:], uint16(c.Height))
	binary.LittleEndian.PutUint16(buf[10:], uint16(c.FrameMS))
	binary.LittleEndian.PutUint32(buf[12:], uint32(c.Offset))
	binary.LittleEndian.PutUint32(buf[16:], uint32(c.Total))
	binary.LittleEndian.PutUint32(buf[20:], c.CRC32)
	binary.LittleEndian.PutUint16(buf[24:], uint16(len(c.Data)))
	copy(buf[bpt1Prefix:], c.Data)
	return buf, nil
}

// DecodeBPT1 parses one complete frame. It returns the chunk and the number
// of bytes consumed.
func DecodeBPT1(buf []byte) (BPT1Chunk, int, error) {
	if len(buf) < bpt1Prefix || string(buf[:4]) != bpt1Magic {
		return BPT1Chunk{}, 0, errors.New("buddy: bpt1 magic missing")
	}
	n := int(binary.LittleEndian.Uint16(buf[24:]))
	if n <= 0 || n > BPT1MaxData || len(buf) < bpt1Prefix+n {
		return BPT1Chunk{}, 0, errors.New("buddy: bpt1 length invalid")
	}
	data := make([]byte, n)
	copy(data, buf[bpt1Prefix:bpt1Prefix+n])
	return BPT1Chunk{
		Frame:   int(buf[4]),
		Frames:  int(buf[5]),
		Width:   int(binary.LittleEndian.Uint16(buf[6:])),
		Height:  int(binary.LittleEndian.Uint16(buf[8:])),
		FrameMS: int(binary.LittleEndian.Uint16(buf[10:])),
		Offset:  int(binary.LittleEndian.Uint32(buf[12:])),
		Total:   int(binary.LittleEndian.Uint32(buf[16:])),
		CRC32:   binary.LittleEndian.Uint32(buf[20:]),
		Data:    data,
	}, bpt1Prefix + n, nil
}
