// corelib/audioconv/ogg.go — OGG container demuxer (minimal, for Opus).
//
// Implements just enough of the OGG framing spec (RFC 3533) to extract
// Opus audio packets. Pure Go, zero dependencies.
package audioconv

import (
	"encoding/binary"
	"fmt"
	"io"
)

const oggCapturePattern = "OggS"
const oggHeaderContinuation = 0x01

// oggPage represents a single OGG page.
type oggPage struct {
	GranulePosition uint64
	Segments        [][]byte // payload segments merged into packets
}

// oggReader reads OGG pages from a byte stream.
type oggReader struct {
	data    []byte
	pos     int
	pending []byte
}

func newOggReader(data []byte) *oggReader {
	return &oggReader{data: data}
}

// readPage reads the next OGG page. Returns io.EOF when done.
func (r *oggReader) readPage() (*oggPage, error) {
	if r.pos+27 > len(r.data) {
		return nil, io.EOF
	}

	// Capture pattern
	if string(r.data[r.pos:r.pos+4]) != oggCapturePattern {
		return nil, fmt.Errorf("audioconv/ogg: invalid capture pattern at offset %d", r.pos)
	}

	// Version (byte 4) must be 0
	if r.data[r.pos+4] != 0 {
		return nil, fmt.Errorf("audioconv/ogg: unsupported version %d", r.data[r.pos+4])
	}

	headerType := r.data[r.pos+5]
	if headerType&oggHeaderContinuation != 0 && len(r.pending) == 0 {
		return nil, fmt.Errorf("audioconv/ogg: continuation page without pending packet at offset %d", r.pos)
	}

	granule := binary.LittleEndian.Uint64(r.data[r.pos+6 : r.pos+14])
	numSegments := int(r.data[r.pos+26])

	segTableStart := r.pos + 27
	if segTableStart+numSegments > len(r.data) {
		return nil, io.EOF
	}

	// Read segment table
	totalPayload := 0
	segSizes := make([]int, numSegments)
	for i := 0; i < numSegments; i++ {
		segSizes[i] = int(r.data[segTableStart+i])
		totalPayload += segSizes[i]
	}

	payloadStart := segTableStart + numSegments
	if payloadStart+totalPayload > len(r.data) {
		return nil, io.EOF
	}

	// Merge segments into packets. A segment of size < 255 terminates a packet.
	// A segment of exactly 255 means the packet continues in the next segment.
	var packets [][]byte
	current := r.pending
	r.pending = nil
	off := payloadStart
	for i := 0; i < numSegments; i++ {
		sz := segSizes[i]
		current = append(current, r.data[off:off+sz]...)
		off += sz
		if sz < 255 {
			packets = append(packets, current)
			current = nil
		}
	}
	if len(current) > 0 {
		r.pending = current
	}

	r.pos = payloadStart + totalPayload

	return &oggPage{
		GranulePosition: granule,
		Segments:        packets,
	}, nil
}

// extractOggOpusPackets reads all OGG pages and returns the Opus audio
// packets (skipping the OpusHead and OpusTags header packets).
func extractOggOpusPackets(data []byte) ([][]byte, error) {
	reader := newOggReader(data)
	var packets [][]byte
	headersDone := false

	for {
		page, err := reader.readPage()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		for _, pkt := range page.Segments {
			if len(pkt) == 0 {
				continue
			}
			// Skip OpusHead and OpusTags header packets by checking content.
			if !headersDone {
				if len(pkt) >= 8 && (string(pkt[:8]) == "OpusHead" || string(pkt[:8]) == "OpusTags") {
					continue
				}
				headersDone = true
			}
			packets = append(packets, pkt)
		}
	}
	if len(reader.pending) > 0 {
		return nil, fmt.Errorf("audioconv/ogg: truncated packet at end of stream")
	}

	if len(packets) == 0 {
		return nil, fmt.Errorf("audioconv/ogg: no audio packets found")
	}
	return packets, nil
}
