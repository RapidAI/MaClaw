//go:build windows

package guiapp

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	chatMediaCFDIB        = 8
	chatMediaCFHDROP      = 15
	chatMediaGMEMMoveable = 0x0002
)

var (
	chatMediaUser32                   = syscall.NewLazyDLL("user32.dll")
	chatMediaKernel32                 = syscall.NewLazyDLL("kernel32.dll")
	chatMediaOpenClipboard            = chatMediaUser32.NewProc("OpenClipboard")
	chatMediaCloseClipboard           = chatMediaUser32.NewProc("CloseClipboard")
	chatMediaEmptyClipboard           = chatMediaUser32.NewProc("EmptyClipboard")
	chatMediaSetClipboardData         = chatMediaUser32.NewProc("SetClipboardData")
	chatMediaRegisterClipboardFormatW = chatMediaUser32.NewProc("RegisterClipboardFormatW")
	chatMediaGlobalAlloc              = chatMediaKernel32.NewProc("GlobalAlloc")
	chatMediaGlobalLock               = chatMediaKernel32.NewProc("GlobalLock")
	chatMediaGlobalUnlock             = chatMediaKernel32.NewProc("GlobalUnlock")
	chatMediaGlobalFree               = chatMediaKernel32.NewProc("GlobalFree")

	chatMediaPNGFormatOnce sync.Once
	chatMediaPNGFormat     uintptr
)

type chatMediaClipboardMem struct {
	format uintptr
	mem    uintptr
}

func writeChatMediaClipboardPlatform(payload chatMediaClipboard) error {
	// Allocate before opening the clipboard. A failed allocation must not
	// empty whatever the user had copied already.
	staged, err := stageChatMediaClipboard(payload)
	if err != nil {
		freeChatMediaClipboard(staged)
		return err
	}
	if len(staged) == 0 {
		return fmt.Errorf("nothing to copy")
	}
	// OpenClipboard and CloseClipboard must run on the same OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := waitChatMediaClipboard(); err != nil {
		freeChatMediaClipboard(staged)
		return err
	}
	defer chatMediaCloseClipboard.Call()
	if r, _, err := chatMediaEmptyClipboard.Call(); r == 0 {
		freeChatMediaClipboard(staged)
		if err != nil {
			return fmt.Errorf("empty clipboard: %v", err)
		}
		return fmt.Errorf("empty clipboard")
	}
	placed := 0
	var first error
	for i := range staged {
		if staged[i].mem == 0 {
			continue
		}
		r, _, err := chatMediaSetClipboardData.Call(staged[i].format, staged[i].mem)
		if r == 0 {
			chatMediaGlobalFree.Call(staged[i].mem)
			staged[i].mem = 0
			if first == nil {
				first = err
			}
			continue
		}
		// A successful SetClipboardData transfers ownership of the memory.
		staged[i].mem = 0
		placed++
	}
	if placed == 0 {
		if first == nil {
			return fmt.Errorf("nothing to copy")
		}
		return fmt.Errorf("clipboard set: %v", first)
	}
	return nil
}

func stageChatMediaClipboard(payload chatMediaClipboard) ([]chatMediaClipboardMem, error) {
	var staged []chatMediaClipboardMem
	stage := func(format uintptr, data []byte) error {
		if format == 0 || len(data) == 0 {
			return nil
		}
		mem, err := allocChatMediaClipboard(data)
		if err != nil {
			return err
		}
		staged = append(staged, chatMediaClipboardMem{format: format, mem: mem})
		return nil
	}
	if err := stage(chatMediaCFDIB, payload.dib); err != nil {
		return staged, err
	}
	if err := stage(chatMediaPNGFormatID(), payload.png); err != nil {
		return staged, err
	}
	if err := stage(chatMediaCFHDROP, payload.hdrop); err != nil {
		return staged, err
	}
	return staged, nil
}

func freeChatMediaClipboard(staged []chatMediaClipboardMem) {
	for i := range staged {
		if staged[i].mem == 0 {
			continue
		}
		chatMediaGlobalFree.Call(staged[i].mem)
		staged[i].mem = 0
	}
}

func allocChatMediaClipboard(data []byte) (uintptr, error) {
	h, _, err := chatMediaGlobalAlloc.Call(chatMediaGMEMMoveable, uintptr(len(data)))
	if h == 0 {
		if err != nil {
			return 0, fmt.Errorf("clipboard alloc: %v", err)
		}
		return 0, fmt.Errorf("clipboard alloc")
	}
	ptr, _, err := chatMediaGlobalLock.Call(h)
	if ptr == 0 {
		chatMediaGlobalFree.Call(h)
		if err != nil {
			return 0, fmt.Errorf("clipboard lock: %v", err)
		}
		return 0, fmt.Errorf("clipboard lock")
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(data)), data)
	chatMediaGlobalUnlock.Call(h)
	return h, nil
}

func waitChatMediaClipboard() error {
	// Another app may be reading the clipboard; wait briefly instead of failing
	// the user's copy on the first busy answer.
	deadline := time.Now().Add(time.Second)
	var last error
	for {
		r, _, err := chatMediaOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}
		last = err
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if last == nil {
		return fmt.Errorf("clipboard is busy")
	}
	return fmt.Errorf("clipboard is busy: %v", last)
}

func chatMediaPNGFormatID() uintptr {
	chatMediaPNGFormatOnce.Do(func() {
		name, err := syscall.UTF16PtrFromString("PNG")
		if err != nil {
			return
		}
		r, _, _ := chatMediaRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
		chatMediaPNGFormat = r
	})
	return chatMediaPNGFormat
}
