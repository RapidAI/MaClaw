package gopdf

// Buff for pdf content
type Buff struct {
	position int
	datas    []byte
}

// Write : write []byte to buffer
func (b *Buff) Write(p []byte) (int, error) {
	needed := b.position + len(p)
	if needed > len(b.datas) {
		if needed > cap(b.datas) {
			// grow with exponential strategy to avoid O(n²) behavior
			newCap := cap(b.datas) * 2
			if newCap < needed {
				newCap = needed
			}
			newData := make([]byte, needed, newCap)
			copy(newData, b.datas)
			b.datas = newData
		} else {
			b.datas = b.datas[:needed]
		}
	}
	copy(b.datas[b.position:], p)
	b.position += len(p)
	return len(p), nil
}

// Len : len of buffer
func (b *Buff) Len() int {
	return len(b.datas)
}

// Bytes : get bytes
func (b *Buff) Bytes() []byte {
	return b.datas
}

// Position : get current position
func (b *Buff) Position() int {
	return b.position
}

// SetPosition : set current position
func (b *Buff) SetPosition(pos int) {
	b.position = pos
}
