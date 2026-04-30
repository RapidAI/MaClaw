package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"math"
	"unsafe"
)

func opus_pcm_soft_clip(_x *float32, N int, C int, declip_mem *float32) {
	var (
		c int
		i int
		x *float32
	)
	if C < 1 || N < 1 || _x == nil || declip_mem == nil {
		return
	}
	for i = 0; i < N*C; i++ {
		if (-2.0) > (func() float32 {
			if 2.0 < (*(*float32)(unsafe.Add(unsafe.Pointer(_x), unsafe.Sizeof(float32(0))*uintptr(i)))) {
				return 2.0
			}
			return *(*float32)(unsafe.Add(unsafe.Pointer(_x), unsafe.Sizeof(float32(0))*uintptr(i)))
		}()) {
			*(*float32)(unsafe.Add(unsafe.Pointer(_x), unsafe.Sizeof(float32(0))*uintptr(i))) = -2.0
		} else if 2.0 < (*(*float32)(unsafe.Add(unsafe.Pointer(_x), unsafe.Sizeof(float32(0))*uintptr(i)))) {
			*(*float32)(unsafe.Add(unsafe.Pointer(_x), unsafe.Sizeof(float32(0))*uintptr(i))) = 2.0
		} else {
			*(*float32)(unsafe.Add(unsafe.Pointer(_x), unsafe.Sizeof(float32(0))*uintptr(i))) = *(*float32)(unsafe.Add(unsafe.Pointer(_x), unsafe.Sizeof(float32(0))*uintptr(i)))
		}
	}
	for c = 0; c < C; c++ {
		var (
			a    float32
			x0   float32
			curr int
		)
		x = (*float32)(unsafe.Add(unsafe.Pointer(_x), unsafe.Sizeof(float32(0))*uintptr(c)))
		a = *(*float32)(unsafe.Add(unsafe.Pointer(declip_mem), unsafe.Sizeof(float32(0))*uintptr(c)))
		for i = 0; i < N; i++ {
			if *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))*a >= 0 {
				break
			}
			*(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))) = *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))) + a**(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))**(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))
		}
		curr = 0
		x0 = *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*0))
		for {
			var (
				start    int
				end      int
				maxval   float32
				special  int = 0
				peak_pos int
			)
			for i = curr; i < N; i++ {
				if *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))) > 1 || *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))) < float32(-1) {
					break
				}
			}
			if i == N {
				a = 0
				break
			}
			peak_pos = i
			start = func() int {
				end = i
				return end
			}()
			maxval = float32(math.Abs(float64(*(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))))))
			for start > 0 && *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))**(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr((start-1)*C))) >= 0 {
				start--
			}
			for end < N && *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))**(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(end*C))) >= 0 {
				if (float32(math.Abs(float64(*(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(end*C))))))) > maxval {
					maxval = float32(math.Abs(float64(*(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(end*C))))))
					peak_pos = end
				}
				end++
			}
			special = int(libc.BoolToInt(start == 0 && *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))**(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*0)) >= 0))
			a = (maxval - 1) / (maxval * maxval)
			a += a * 2.4e-07
			if *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))) > 0 {
				a = -a
			}
			for i = start; i < end; i++ {
				*(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))) = *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))) + a**(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))**(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))
			}
			if special != 0 && peak_pos >= 2 {
				var (
					delta  float32
					offset float32 = x0 - *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*0))
				)
				delta = offset / float32(peak_pos)
				for i = curr; i < peak_pos; i++ {
					offset -= delta
					*(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))) += offset
					if (-1.0) > (func() float32 {
						if 1.0 < (*(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))) {
							return 1.0
						}
						return *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))
					}()) {
						*(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))) = -1.0
					} else if 1.0 < (*(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))) {
						*(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))) = 1.0
					} else {
						*(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C))) = *(*float32)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(float32(0))*uintptr(i*C)))
					}
				}
			}
			curr = end
			if curr == N {
				break
			}
		}
		*(*float32)(unsafe.Add(unsafe.Pointer(declip_mem), unsafe.Sizeof(float32(0))*uintptr(c))) = a
	}
}
func encode_size(size int, data *uint8) int {
	if size < 252 {
		*data = uint8(int8(size))
		return 1
	} else {
		*data = uint8(int8((size & 0x3) + 252))
		*(*uint8)(unsafe.Add(unsafe.Pointer(data), 1)) = uint8(int8((size - int(*data)) >> 2))
		return 2
	}
}
func parse_size(data *uint8, len_ int32, size *int16) int {
	if int(len_) < 1 {
		*size = -1
		return -1
	} else if int(*data) < 252 {
		*size = int16(*data)
		return 1
	} else if int(len_) < 2 {
		*size = -1
		return -1
	} else {
		*size = int16(int(*(*uint8)(unsafe.Add(unsafe.Pointer(data), 1)))*4 + int(*data))
		return 2
	}
}
func opus_packet_get_samples_per_frame(data *uint8, Fs int32) int {
	var audiosize int
	if int(*data)&0x80 != 0 {
		audiosize = (int(*data) >> 3) & 0x3
		audiosize = (int(Fs) << audiosize) / 400
	} else if (int(*data) & 0x60) == 0x60 {
		if (int(*data) & 0x8) != 0 {
			audiosize = int(Fs) / 50
		} else {
			audiosize = int(Fs) / 100
		}
	} else {
		audiosize = (int(*data) >> 3) & 0x3
		if audiosize == 3 {
			audiosize = int(Fs) * 60 / 1000
		} else {
			audiosize = (int(Fs) << audiosize) / 100
		}
	}
	return audiosize
}
func opus_packet_parse_impl(data *uint8, len_ int32, self_delimited int, out_toc *uint8, frames [48]*uint8, size [48]int16, payload_offset *int, packet_offset *int32) int {
	var (
		i         int
		bytes     int
		count     int
		cbr       int
		ch        uint8
		toc       uint8
		framesize int
		last_size int32
		pad       int32  = 0
		data0     *uint8 = data
	)
	if false || int(len_) < 0 { // C: NULL check on array �?always non-nil in Go
		return -1
	}
	if int(len_) == 0 {
		return -4
	}
	framesize = opus_packet_get_samples_per_frame(data, 48000)
	cbr = 0
	toc = *func() *uint8 {
		p := &data
		x := *p
		*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
		return x
	}()
	len_--
	last_size = len_
	switch int(toc) & 0x3 {
	case 0:
		count = 1
	case 1:
		count = 2
		cbr = 1
		if self_delimited == 0 {
			if int(len_)&0x1 != 0 {
				return -4
			}
			last_size = int32(int(len_) / 2)
			size[0] = int16(last_size)
		}
	case 2:
		count = 2
		bytes = parse_size(data, len_, &size[0])
		len_ -= int32(bytes)
		if int(size[0]) < 0 || int(size[0]) > int(len_) {
			return -4
		}
		data = (*uint8)(unsafe.Add(unsafe.Pointer(data), bytes))
		last_size = int32(int(len_) - int(size[0]))
	default:
		if int(len_) < 1 {
			return -4
		}
		ch = *func() *uint8 {
			p := &data
			x := *p
			*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
			return x
		}()
		count = int(ch) & 0x3F
		if count <= 0 || framesize*int(int32(count)) > 5760 {
			return -4
		}
		len_--
		if int(ch)&0x40 != 0 {
			var p int
			for {
				{
					var tmp int
					if int(len_) <= 0 {
						return -4
					}
					p = int(*func() *uint8 {
						p := &data
						x := *p
						*p = (*uint8)(unsafe.Add(unsafe.Pointer(*p), 1))
						return x
					}())
					len_--
					if p == math.MaxUint8 {
						tmp = 254
					} else {
						tmp = p
					}
					len_ -= int32(tmp)
					pad += int32(tmp)
				}
				if p != math.MaxUint8 {
					break
				}
			}
		}
		if int(len_) < 0 {
			return -4
		}
		cbr = int(libc.BoolToInt((int(ch) & 0x80) == 0))
		if cbr == 0 {
			last_size = len_
			for i = 0; i < count-1; i++ {
				bytes = parse_size(data, len_, &size[i])
				len_ -= int32(bytes)
				if int(size[i]) < 0 || int(size[i]) > int(len_) {
					return -4
				}
				data = (*uint8)(unsafe.Add(unsafe.Pointer(data), bytes))
				last_size -= int32(bytes + int(size[i]))
			}
			if int(last_size) < 0 {
				return -4
			}
		} else if self_delimited == 0 {
			last_size = int32(int(len_) / count)
			if int(last_size)*count != int(len_) {
				return -4
			}
			for i = 0; i < count-1; i++ {
				size[i] = int16(last_size)
			}
		}
	}
	if self_delimited != 0 {
		bytes = parse_size(data, len_, (*int16)(unsafe.Add(unsafe.Pointer(&size[count]), -int(unsafe.Sizeof(int16(0))*1))))
		len_ -= int32(bytes)
		if int(size[count-1]) < 0 || int(size[count-1]) > int(len_) {
			return -4
		}
		data = (*uint8)(unsafe.Add(unsafe.Pointer(data), bytes))
		if cbr != 0 {
			if int(size[count-1])*count > int(len_) {
				return -4
			}
			for i = 0; i < count-1; i++ {
				size[i] = size[count-1]
			}
		} else if bytes+int(size[count-1]) > int(last_size) {
			return -4
		}
	} else {
		if int(last_size) > 1275 {
			return -4
		}
		size[count-1] = int16(last_size)
	}
	if payload_offset != nil {
		*payload_offset = int(int64(uintptr(unsafe.Pointer(data)) - uintptr(unsafe.Pointer(data0))))
	}
	for i = 0; i < count; i++ {
		if true { // C: NULL check on array �?always non-nil in Go
			frames[i] = data
		}
		data = (*uint8)(unsafe.Add(unsafe.Pointer(data), size[i]))
	}
	if packet_offset != nil {
		*packet_offset = int32(int(pad) + int(int32(int64(uintptr(unsafe.Pointer(data))-uintptr(unsafe.Pointer(data0))))))
	}
	if out_toc != nil {
		*out_toc = toc
	}
	return count
}
func opus_packet_parse(data *uint8, len_ int32, out_toc *uint8, frames [48]*uint8, size [48]int16, payload_offset *int) int {
	return opus_packet_parse_impl(data, len_, 0, out_toc, frames, size, payload_offset, nil)
}
