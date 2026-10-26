//go:build windows

package intent

import (
	"sync"
	"syscall"
	"unsafe"
)

// relationCache is LOGICAL_PROCESSOR_RELATIONSHIP.RelationCache.
const relationCache = 2

const cacheUnified = 0

var (
	l3Once  sync.Once
	l3Bytes int64
)

// l3CacheBytes reports the last-level (L3) cache visible to one core: the
// largest single LLC domain, not the sum over CCXs. A prediction runs on one
// core, so a 2×32 MB part gives a 32 MB working set, not 64 MB.
// Returns 0 when the query fails, which makes the Q8P auto policy keep the
// float32 path (never silently trade accuracy without knowing the hardware).
func l3CacheBytes() int64 {
	l3Once.Do(func() {
		l3Bytes = queryL3()
	})
	return l3Bytes
}

func queryL3() (largest int64) {
	dll := syscall.NewLazyDLL("kernel32.dll")
	proc := dll.NewProc("GetLogicalProcessorInformationEx")
	if err := proc.Find(); err != nil {
		return 0
	}
	var length uint32
	// The size probe is expected to fail with ERROR_INSUFFICIENT_BUFFER.
	if r, _, _ := proc.Call(uintptr(relationCache), 0, uintptr(unsafe.Pointer(&length))); r != 0 || length == 0 {
		return 0
	}
	for attempt := 0; attempt < 2; attempt++ {
		buf := make([]byte, length)
		r, _, _ := proc.Call(uintptr(relationCache),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&length)))
		if r == 0 {
			return 0
		}
		if n := maxUnifiedL3(buf, length); n > 0 {
			return n
		}
	}
	return 0
}

// maxUnifiedL3 walks the variable-length records and returns the largest
// Level-3 unified cache instance:
//
//	struct { uint32 Relationship; uint32 Size; CACHE_DESCRIPTOR Cache; }
//	CACHE_DESCRIPTOR { uint8 Level; uint8 Associativity; uint16 LineSize;
//	                   uint32 CacheSize; uint32 Type; uint8 Reserved[20]; }
func maxUnifiedL3(buf []byte, length uint32) (largest int64) {
	for off := uint32(0); off+8 <= length; {
		rel := *(*uint32)(unsafe.Pointer(&buf[off]))
		size := *(*uint32)(unsafe.Pointer(&buf[off+4]))
		if size < 8 || off+size > length {
			return largest
		}
		if rel == relationCache && off+40 <= length {
			level := buf[off+8]
			cacheSize := *(*uint32)(unsafe.Pointer(&buf[off+12]))
			cacheType := *(*uint32)(unsafe.Pointer(&buf[off+16]))
			if level == 3 && cacheType == cacheUnified && int64(cacheSize) > largest {
				largest = int64(cacheSize)
			}
		}
		off += size
	}
	return largest
}
