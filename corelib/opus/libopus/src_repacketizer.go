package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"math"
	"unsafe"
)

func opus_repacketizer_get_size() int {
	return int(unsafe.Sizeof(OpusRepacketizer{}))
}
func opus_repacketizer_init(rp *OpusRepacketizer) *OpusRepacketizer {
	rp.Nb_frames = 0
	return rp
}
func opus_repacketizer_create() *OpusRepacketizer {
	var rp *OpusRepacketizer
	rp = (*OpusRepacketizer)(libc.Malloc(opus_repacketizer_get_size()))
	if rp == nil {
		return nil
	}
	return opus_repacketizer_init(rp)
}
func opus_repacketizer_destroy(rp *OpusRepacketizer) {
	libc.Free(unsafe.Pointer(rp))
}
func opus_repacketizer_cat_impl(rp *OpusRepacketizer, data *uint8, len_ int32, self_delimited int) int {
	var (
		tmp_toc        uint8
		curr_nb_frames int
		ret            int
	)
	if int(len_) < 1 {
		return -4
	}
	if rp.Nb_frames == 0 {
		rp.Toc = *data
		rp.Framesize = opus_packet_get_samples_per_frame(data, 8000)
	} else if (int(rp.Toc) & 0xFC) != (int(*data) & 0xFC) {
		return -4
	}
	curr_nb_frames = opus_packet_get_nb_frames(unsafe.Slice(data, len_), len_)
	if curr_nb_frames < 1 {
		return -4
	}
	if (curr_nb_frames+rp.Nb_frames)*rp.Framesize > 960 {
		return -4
	}
	ret = opus_packet_parse_impl(data, len_, self_delimited, &tmp_toc, rp.Frames, rp.Len, nil, nil)
	if ret < 1 {
		return ret
	}
	rp.Nb_frames += curr_nb_frames
	return OPUS_OK
}
func opus_repacketizer_cat(rp *OpusRepacketizer, data *uint8, len_ int32) int {
	return opus_repacketizer_cat_impl(rp, data, len_, 0)
}
func opus_repacketizer_get_nb_frames(rp *OpusRepacketizer) int {
	return rp.Nb_frames
}
func opus_repacketizer_out_range_impl(rp *OpusRepacketizer, begin int, end int, data *uint8, maxlen int32, self_delimited int, pad int) int32 {
	var (
		i        int
		count    int
		tot_size int32
		len_     *int16
		frames   **uint8
		ptr      *uint8
	)
	if begin < 0 || begin >= end || end > rp.Nb_frames {
		return -1
	}
	count = end - begin
	len_ = &rp.Len[begin]
	frames = &rp.Frames[begin]
	if self_delimited != 0 {
		tot_size = int32(int(libc.BoolToInt(int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*uintptr(count-1)))) >= 252)) + 1)
	} else {
		tot_size = 0
	}
	ptr = data
	if count == 1 {
		tot_size += int32(int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*0))) + 1)
		if int(tot_size) > int(maxlen) {
			return -2
		}
		*func() *uint8 {
			p := &ptr
			x := *p
			*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
			return x
		}() = uint8(int8(int(rp.Toc) & 0xFC))
	} else if count == 2 {
		if int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*1))) == int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*0))) {
			tot_size += int32(int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*0)))*2 + 1)
			if int(tot_size) > int(maxlen) {
				return -2
			}
			*func() *uint8 {
				p := &ptr
				x := *p
				*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
				return x
			}() = uint8(int8((int(rp.Toc) & 0xFC) | 0x1))
		} else {
			tot_size += int32(int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*0))) + int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*1))) + 2 + int(libc.BoolToInt(int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*0))) >= 252)))
			if int(tot_size) > int(maxlen) {
				return -2
			}
			*func() *uint8 {
				p := &ptr
				x := *p
				*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
				return x
			}() = uint8(int8((int(rp.Toc) & 0xFC) | 0x2))
			ptr = (*uint8)(unsafe.Add(unsafe.Pointer(ptr), encode_size(int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*0))), ptr)))
		}
	}
	if count > 2 || pad != 0 && int(tot_size) < int(maxlen) {
		var (
			vbr        int
			pad_amount int = 0
		)
		ptr = data
		if self_delimited != 0 {
			tot_size = int32(int(libc.BoolToInt(int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*uintptr(count-1)))) >= 252)) + 1)
		} else {
			tot_size = 0
		}
		vbr = 0
		for i = 1; i < count; i++ {
			if int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*uintptr(i)))) != int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*0))) {
				vbr = 1
				break
			}
		}
		if vbr != 0 {
			tot_size += 2
			for i = 0; i < count-1; i++ {
				tot_size += int32(int(libc.BoolToInt(int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*uintptr(i)))) >= 252)) + 1 + int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*uintptr(i)))))
			}
			tot_size += int32(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*uintptr(count-1))))
			if int(tot_size) > int(maxlen) {
				return -2
			}
			*func() *uint8 {
				p := &ptr
				x := *p
				*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
				return x
			}() = uint8(int8((int(rp.Toc) & 0xFC) | 0x3))
			*func() *uint8 {
				p := &ptr
				x := *p
				*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
				return x
			}() = uint8(int8(count | 0x80))
		} else {
			tot_size += int32(count*int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*0))) + 2)
			if int(tot_size) > int(maxlen) {
				return -2
			}
			*func() *uint8 {
				p := &ptr
				x := *p
				*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
				return x
			}() = uint8(int8((int(rp.Toc) & 0xFC) | 0x3))
			*func() *uint8 {
				p := &ptr
				x := *p
				*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
				return x
			}() = uint8(int8(count))
		}
		if pad != 0 {
			pad_amount = int(maxlen) - int(tot_size)
		} else {
			pad_amount = 0
		}
		if pad_amount != 0 {
			var nb_255s int
			*(*uint8)(unsafe.Add(unsafe.Pointer(data), 1)) |= 0x40
			nb_255s = (pad_amount - 1) / math.MaxUint8
			for i = 0; i < nb_255s; i++ {
				*func() *uint8 {
					p := &ptr
					x := *p
					*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
					return x
				}() = math.MaxUint8
			}
			*func() *uint8 {
				p := &ptr
				x := *p
				*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
				return x
			}() = uint8(int8(pad_amount - nb_255s*math.MaxUint8 - 1))
			tot_size += int32(pad_amount)
		}
		if vbr != 0 {
			for i = 0; i < count-1; i++ {
				ptr = (*uint8)(unsafe.Add(unsafe.Pointer(ptr), encode_size(int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*uintptr(i)))), ptr)))
			}
		}
	}
	if self_delimited != 0 {
		var sdlen int = encode_size(int(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*uintptr(count-1)))), ptr)
		ptr = (*uint8)(unsafe.Add(unsafe.Pointer(ptr), sdlen))
	}
	for i = 0; i < count; i++ {
		libc.MemMove(unsafe.Pointer(ptr), unsafe.Pointer(*(**uint8)(unsafe.Add(unsafe.Pointer(frames), unsafe.Sizeof((*uint8)(nil))*uintptr(i)))), int(uintptr(*(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*uintptr(i))))*unsafe.Sizeof(uint8(0))+uintptr((int64(uintptr(unsafe.Pointer(ptr))-uintptr(unsafe.Pointer(*(**uint8)(unsafe.Add(unsafe.Pointer(frames), unsafe.Sizeof((*uint8)(nil))*uintptr(i)))))))*0)))
		ptr = (*uint8)(unsafe.Add(unsafe.Pointer(ptr), *(*int16)(unsafe.Add(unsafe.Pointer(len_), unsafe.Sizeof(int16(0))*uintptr(i)))))
	}
	if pad != 0 {
		for uintptr(unsafe.Pointer(ptr)) < uintptr(unsafe.Pointer((*uint8)(unsafe.Add(unsafe.Pointer(data), maxlen)))) {
			*func() *uint8 {
				p := &ptr
				x := *p
				*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
				return x
			}() = 0
		}
	}
	return tot_size
}
func opus_repacketizer_out_range(rp *OpusRepacketizer, begin int, end int, data *uint8, maxlen int32) int32 {
	return opus_repacketizer_out_range_impl(rp, begin, end, data, maxlen, 0, 0)
}
func opus_repacketizer_out(rp *OpusRepacketizer, data *uint8, maxlen int32) int32 {
	return opus_repacketizer_out_range_impl(rp, 0, rp.Nb_frames, data, maxlen, 0, 0)
}
func opus_packet_pad(data *uint8, len_ int32, new_len int32) int {
	var (
		rp  OpusRepacketizer
		ret int32
	)
	if int(len_) < 1 {
		return -1
	}
	if int(len_) == int(new_len) {
		return OPUS_OK
	} else if int(len_) > int(new_len) {
		return -1
	}
	opus_repacketizer_init(&rp)
	libc.MemMove(unsafe.Pointer((*uint8)(unsafe.Add(unsafe.Pointer((*uint8)(unsafe.Add(unsafe.Pointer(data), new_len))), -len_))), unsafe.Pointer(data), int(uintptr(len_)*unsafe.Sizeof(uint8(0))+uintptr((int64(uintptr(unsafe.Pointer((*uint8)(unsafe.Add(unsafe.Pointer((*uint8)(unsafe.Add(unsafe.Pointer(data), new_len))), -len_))))-uintptr(unsafe.Pointer(data))))*0)))
	ret = int32(opus_repacketizer_cat(&rp, (*uint8)(unsafe.Add(unsafe.Pointer((*uint8)(unsafe.Add(unsafe.Pointer(data), new_len))), -len_)), len_))
	if int(ret) != OPUS_OK {
		return int(ret)
	}
	ret = opus_repacketizer_out_range_impl(&rp, 0, rp.Nb_frames, data, new_len, 0, 1)
	if int(ret) > 0 {
		return OPUS_OK
	} else {
		return int(ret)
	}
}
func opus_packet_unpad(data *uint8, len_ int32) int32 {
	var (
		rp  OpusRepacketizer
		ret int32
	)
	if int(len_) < 1 {
		return -1
	}
	opus_repacketizer_init(&rp)
	ret = int32(opus_repacketizer_cat(&rp, data, len_))
	if int(ret) < 0 {
		return ret
	}
	ret = opus_repacketizer_out_range_impl(&rp, 0, rp.Nb_frames, data, len_, 0, 0)
	return ret
}
func opus_multistream_packet_pad(data *uint8, len_ int32, new_len int32, nb_streams int) int {
	var (
		s             int
		count         int
		toc           uint8
		size          [48]int16
		packet_offset int32
		amount        int32
	)
	if int(len_) < 1 {
		return -1
	}
	if int(len_) == int(new_len) {
		return OPUS_OK
	} else if int(len_) > int(new_len) {
		return -1
	}
	amount = int32(int(new_len) - int(len_))
	for s = 0; s < nb_streams-1; s++ {
		if int(len_) <= 0 {
			return -4
		}
		count = opus_packet_parse_impl(data, len_, 1, &toc, [48]*uint8{}, size, nil, &packet_offset)
		if count < 0 {
			return count
		}
		data = (*uint8)(unsafe.Add(unsafe.Pointer(data), packet_offset))
		len_ -= packet_offset
	}
	return opus_packet_pad(data, len_, int32(int(len_)+int(amount)))
}
func opus_multistream_packet_unpad(data *uint8, len_ int32, nb_streams int) int32 {
	var (
		s             int
		toc           uint8
		size          [48]int16
		packet_offset int32
		rp            OpusRepacketizer
		dst           *uint8
		dst_len       int32
	)
	if int(len_) < 1 {
		return -1
	}
	dst = data
	dst_len = 0
	for s = 0; s < nb_streams; s++ {
		var (
			ret            int32
			self_delimited int = int(libc.BoolToInt(s != nb_streams-1))
		)
		if int(len_) <= 0 {
			return -4
		}
		opus_repacketizer_init(&rp)
		ret = int32(opus_packet_parse_impl(data, len_, self_delimited, &toc, [48]*uint8{}, size, nil, &packet_offset))
		if int(ret) < 0 {
			return ret
		}
		ret = int32(opus_repacketizer_cat_impl(&rp, data, packet_offset, self_delimited))
		if int(ret) < 0 {
			return ret
		}
		ret = opus_repacketizer_out_range_impl(&rp, 0, rp.Nb_frames, dst, len_, self_delimited, 0)
		if int(ret) < 0 {
			return ret
		} else {
			dst_len += ret
		}
		dst = (*uint8)(unsafe.Add(unsafe.Pointer(dst), ret))
		data = (*uint8)(unsafe.Add(unsafe.Pointer(data), packet_offset))
		len_ -= packet_offset
	}
	return dst_len
}
