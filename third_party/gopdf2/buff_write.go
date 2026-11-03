package gopdf

import "io"

// WriteUInt32  writes a 32-bit unsigned integer value to w io.Writer
func WriteUInt32(w io.Writer, v uint) error {
	var buf [4]byte
	buf[0] = byte(v >> 24)
	buf[1] = byte(v >> 16)
	buf[2] = byte(v >> 8)
	buf[3] = byte(v)
	_, err := w.Write(buf[:])
	return err
}

// WriteUInt16 writes a 16-bit unsigned integer value to w io.Writer
func WriteUInt16(w io.Writer, v uint) error {
	var buf [2]byte
	buf[0] = byte(v >> 8)
	buf[1] = byte(v)
	_, err := w.Write(buf[:])
	return err
}

// WriteTag writes string value to w io.Writer
func WriteTag(w io.Writer, tag string) error {
	_, err := io.WriteString(w, tag)
	return err
}

// WriteBytes writes []byte value to w io.Writer
func WriteBytes(w io.Writer, data []byte, offset int, count int) error {
	_, err := w.Write(data[offset : offset+count])
	return err
}
