package windsurf

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Protobuf and Connect-RPC helpers for Devin's Windsurf-compatible API.
// Field tags are varints, so field numbers past 15 stay valid.

func encodeVarint(v uint64) []byte {
	var out []byte
	for v > 0x7f {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func encodeTag(field, wire int) []byte {
	return encodeVarint(uint64(field<<3 | wire))
}

func encodeString(field int, s string) []byte {
	body := []byte(s)
	return append(append(encodeTag(field, 2), encodeVarint(uint64(len(body)))...), body...)
}

func encodeBytes(field int, body []byte) []byte {
	return append(append(encodeTag(field, 2), encodeVarint(uint64(len(body)))...), body...)
}

func encodeVarintField(field int, v uint64) []byte {
	return append(encodeTag(field, 0), encodeVarint(v)...)
}

func encodeDouble(field int, n float64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], math.Float64bits(n))
	return append(encodeTag(field, 1), buf[:]...)
}

func encodeTimestamp(seconds, nanos int64) []byte {
	body := encodeVarintField(1, uint64(seconds))
	if nanos > 0 {
		body = append(body, encodeVarintField(2, uint64(nanos))...)
	}
	return body
}

type protoField struct {
	num   int
	wire  int
	value any // uint64 or []byte
}

func iterFields(buf []byte) ([]protoField, error) {
	var fields []protoField
	i := 0
	for i < len(buf) {
		tag, n, err := decodeVarint(buf, i)
		if err != nil {
			return fields, err
		}
		i = n
		num := int(tag >> 3)
		wire := int(tag & 7)
		switch wire {
		case 0:
			v, n, err := decodeVarint(buf, i)
			if err != nil {
				return fields, err
			}
			i = n
			fields = append(fields, protoField{num: num, wire: wire, value: v})
		case 1:
			if i+8 > len(buf) {
				return fields, fmt.Errorf("truncated 64-bit field")
			}
			fields = append(fields, protoField{num: num, wire: wire, value: append([]byte(nil), buf[i:i+8]...)})
			i += 8
		case 2:
			length, n, err := decodeVarint(buf, i)
			if err != nil {
				return fields, err
			}
			i = n
			if i+int(length) > len(buf) {
				return fields, fmt.Errorf("truncated length-delimited field")
			}
			fields = append(fields, protoField{num: num, wire: wire, value: append([]byte(nil), buf[i:i+int(length)]...)})
			i += int(length)
		case 5:
			if i+4 > len(buf) {
				return fields, fmt.Errorf("truncated 32-bit field")
			}
			fields = append(fields, protoField{num: num, wire: wire, value: append([]byte(nil), buf[i:i+4]...)})
			i += 4
		default:
			return fields, fmt.Errorf("unsupported wire type %d", wire)
		}
	}
	return fields, nil
}

func decodeVarint(buf []byte, offset int) (uint64, int, error) {
	var v uint64
	var shift uint
	i := offset
	for i < len(buf) {
		b := buf[i]
		i++
		v |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return v, i, nil
		}
		shift += 7
		if shift > 63 {
			return 0, i, fmt.Errorf("varint overflow")
		}
	}
	return 0, i, fmt.Errorf("truncated varint")
}

func fieldString(fields []protoField, num int) string {
	for _, f := range fields {
		if f.num == num && f.wire == 2 {
			if b, ok := f.value.([]byte); ok {
				return string(b)
			}
		}
	}
	return ""
}

func fieldVarint(fields []protoField, num int) (uint64, bool) {
	for _, f := range fields {
		if f.num == num && f.wire == 0 {
			v, ok := f.value.(uint64)
			return v, ok
		}
	}
	return 0, false
}

func frameConnect(body []byte, compress bool) ([]byte, error) {
	payload := body
	flags := byte(0)
	if compress {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(body); err != nil {
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		payload = buf.Bytes()
		flags |= 0x01
	}
	header := make([]byte, 5)
	header[0] = flags
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	return append(header, payload...), nil
}

func readConnectFrame(r io.Reader) (payload []byte, eos bool, err error) {
	var header [5]byte
	if _, err = io.ReadFull(r, header[:]); err != nil {
		return nil, false, err
	}
	flags := header[0]
	n := binary.BigEndian.Uint32(header[1:])
	if n > 32<<20 {
		return nil, false, fmt.Errorf("connect frame too large")
	}
	raw := make([]byte, n)
	if _, err = io.ReadFull(r, raw); err != nil {
		// ReadFull reports a clean EOF when the declared payload is missing
		// entirely. The header already promised those bytes, so the frame was cut.
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, false, err
	}
	if flags&0x01 != 0 {
		zr, zerr := gzip.NewReader(bytes.NewReader(raw))
		if zerr != nil {
			return nil, false, zerr
		}
		payload, err = io.ReadAll(zr)
		_ = zr.Close()
		if err != nil {
			return nil, false, err
		}
	} else {
		payload = raw
	}
	return payload, flags&0x02 != 0, nil
}
