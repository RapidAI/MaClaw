//go:build windows

package guiapp

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	waveMapper         = 0xFFFFFFFF
	waveCallbackFunc   = 0x00030000
	waveDataMessage    = 0x3C0
	waveHeaderDone     = 0x00000001
	petMicBufferBytes  = 3200
	petMicBufferCount  = 4
)

type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	Size           uint16
}

type waveHeader struct {
	Data          uintptr
	BufferLength  uint32
	BytesRecorded uint32
	User          uintptr
	Flags         uint32
	Loops         uint32
	Next          uintptr
	Reserved      uintptr
}

var (
	winmm              = windows.NewLazySystemDLL("winmm.dll")
	procWaveInOpen     = winmm.NewProc("waveInOpen")
	procWaveInClose    = winmm.NewProc("waveInClose")
	procWaveInPrepare  = winmm.NewProc("waveInPrepareHeader")
	procWaveInUnprep   = winmm.NewProc("waveInUnprepareHeader")
	procWaveInAdd      = winmm.NewProc("waveInAddBuffer")
	procWaveInStart    = winmm.NewProc("waveInStart")
	procWaveInStop     = winmm.NewProc("waveInStop")
	procWaveInReset    = winmm.NewProc("waveInReset")
	openWaveMics       sync.Map
	waveInCallbackOnce sync.Once
	waveInCallbackPtr  uintptr
)

type waveMic struct {
	mu     sync.Mutex
	handle uintptr
	closed bool
	frames chan []int16
	bufs   [][]byte
	hdrs   []waveHeader
	pinner runtime.Pinner
}

func openPetMic() (petMic, error) {
	waveInCallbackOnce.Do(func() {
		waveInCallbackPtr = syscall.NewCallback(waveInCallback)
	})
	format := waveFormatEx{
		FormatTag:      1,
		Channels:       1,
		SamplesPerSec:  petSampleRate,
		AvgBytesPerSec: petSampleRate * 2,
		BlockAlign:     2,
		BitsPerSample:  16,
	}
	var handle uintptr
	r, _, err := procWaveInOpen.Call(
		uintptr(unsafe.Pointer(&handle)),
		waveMapper,
		uintptr(unsafe.Pointer(&format)),
		waveInCallbackPtr,
		0,
		waveCallbackFunc,
	)
	if r != 0 {
		return nil, fmt.Errorf("waveInOpen: %v (%d)", err, r)
	}
	mic := &waveMic{
		handle: handle,
		frames: make(chan []int16, petMicBufferCount),
		bufs:   make([][]byte, petMicBufferCount),
		hdrs:   make([]waveHeader, petMicBufferCount),
	}
	for i := range mic.bufs {
		mic.bufs[i] = make([]byte, petMicBufferBytes)
		mic.pinner.Pin(&mic.bufs[i][0])
		mic.hdrs[i].Data = uintptr(unsafe.Pointer(&mic.bufs[i][0]))
		mic.hdrs[i].BufferLength = petMicBufferBytes
		r, _, err = procWaveInPrepare.Call(handle, uintptr(unsafe.Pointer(&mic.hdrs[i])), unsafe.Sizeof(mic.hdrs[i]))
		if r != 0 {
			_ = mic.Close()
			return nil, fmt.Errorf("waveInPrepareHeader: %v (%d)", err, r)
		}
		r, _, err = procWaveInAdd.Call(handle, uintptr(unsafe.Pointer(&mic.hdrs[i])), unsafe.Sizeof(mic.hdrs[i]))
		if r != 0 {
			_ = mic.Close()
			return nil, fmt.Errorf("waveInAddBuffer: %v (%d)", err, r)
		}
	}
	openWaveMics.Store(handle, mic)
	r, _, err = procWaveInStart.Call(handle)
	if r != 0 {
		_ = mic.Close()
		return nil, fmt.Errorf("waveInStart: %v (%d)", err, r)
	}
	return mic, nil
}

func (m *waveMic) Frames() <-chan []int16 {
	if m == nil {
		return nil
	}
	return m.frames
}

func (m *waveMic) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	openWaveMics.Delete(m.handle)
	_, _, _ = procWaveInStop.Call(m.handle)
	_, _, _ = procWaveInReset.Call(m.handle)
	for i := range m.hdrs {
		_, _, _ = procWaveInUnprep.Call(m.handle, uintptr(unsafe.Pointer(&m.hdrs[i])), unsafe.Sizeof(m.hdrs[i]))
	}
	_, _, _ = procWaveInClose.Call(m.handle)
	m.pinner.Unpin()
	close(m.frames)
	return nil
}

// deliverBuf consumes the recorded data of header i and re-arms it. The OS
// hands the header reference back through the callback; resolving it against
// the mic's own prepared headers keeps every access on Go pointers instead of
// converting the callback's uintptr (or the WAVEHDR.Data field) back into a
// pointer. The underlying buffers stay runtime-pinned for the mic's lifetime.
func (m *waveMic) deliverBuf(headerIndex int) {
	if m == nil || headerIndex < 0 || headerIndex >= len(m.hdrs) {
		return
	}
	header := &m.hdrs[headerIndex]
	if header.BytesRecorded < 2 {
		return
	}
	n := int(header.BytesRecorded) / 2
	buf := m.bufs[headerIndex]
	if 2*n > len(buf) {
		return
	}
	pcm := make([]int16, n)
	raw := buf[:2*n]
	for i := 0; i < n; i++ {
		pcm[i] = int16(uint16(raw[i*2]) | uint16(raw[i*2+1])<<8)
	}
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return
	}
	select {
	case m.frames <- pcm:
	default:
	}
	if header.Flags&waveHeaderDone != 0 {
		header.Flags &^= waveHeaderDone
	}
	_, _, _ = procWaveInAdd.Call(m.handle, uintptr(unsafe.Pointer(header)), unsafe.Sizeof(*header))
}

func waveInCallback(hwi, msg, _, param1, _ uintptr) uintptr {
	if msg != waveDataMessage || param1 == 0 {
		return 0
	}
	value, ok := openWaveMics.Load(hwi)
	if !ok {
		return 0
	}
	mic := value.(*waveMic)
	for i := range mic.hdrs {
		if uintptr(unsafe.Pointer(&mic.hdrs[i])) == param1 {
			mic.deliverBuf(i)
			return 0
		}
	}
	return 0
}
