//go:build windows

package guiapp

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/image/draw"
	"golang.org/x/sys/windows"
)

const (
	shellFileAttributeNormal  = 0x80
	shellGFISysIconIndex      = 0x000004000
	shellGFIIcon              = 0x000000100
	shellGFIUseFileAttributes = 0x000000010
	shellImageListExtraLarge  = 0x2 // 48px
	shellImageListJumbo       = 0x4 // 256px
	shellILDTransparent       = 0x1
	shellDINormal             = 0x3
	shellDIBRGBColors         = 0
	shellIconMaxEdge          = 128
	shellIconMaxBitmap        = 256
)

// IID_IImageList. GetIcon is vtable slot 10 (IUnknown plus seven methods).
var shellImageListIID = windows.GUID{
	Data1: 0x46EB5926,
	Data2: 0x582E,
	Data3: 0x4017,
	Data4: [8]byte{0x9F, 0xDF, 0xE8, 0x99, 0x8D, 0xAA, 0x09, 0x50},
}

var (
	shellIconProcOnce sync.Once
	shellIconProc     shellIconProcs
)

type shellIconProcs struct {
	shGetFileInfo    *windows.LazyProc
	shGetImageList   *windows.LazyProc
	destroyIcon      *windows.LazyProc
	getIconInfo      *windows.LazyProc
	getObject        *windows.LazyProc
	deleteObject     *windows.LazyProc
	getDC            *windows.LazyProc
	releaseDC        *windows.LazyProc
	createCompatDC   *windows.LazyProc
	deleteDC         *windows.LazyProc
	selectObject     *windows.LazyProc
	createDIBSection *windows.LazyProc
	drawIconEx       *windows.LazyProc
	getDIBits        *windows.LazyProc
}

type shFileInfoW struct {
	hIcon         uintptr
	iIcon         int32
	dwAttributes  uint32
	szDisplayName [260]uint16
	szTypeName    [80]uint16
}

type shellIconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

// BITMAP on amd64. Go inserts the pointer padding that the Windows struct uses.
type shellWinBitmap struct {
	bmType       int32
	bmWidth      int32
	bmHeight     int32
	bmWidthBytes int32
	bmPlanes     uint16
	bmBitsPixel  uint16
	bmBits       uintptr
}

type shellBitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

func loadShellIconProcs() {
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	user32 := windows.NewLazySystemDLL("user32.dll")
	gdi32 := windows.NewLazySystemDLL("gdi32.dll")
	shellIconProc = shellIconProcs{
		shGetFileInfo:    shell32.NewProc("SHGetFileInfoW"),
		shGetImageList:   shell32.NewProc("SHGetImageList"),
		destroyIcon:      user32.NewProc("DestroyIcon"),
		getIconInfo:      user32.NewProc("GetIconInfo"),
		getObject:        gdi32.NewProc("GetObjectW"),
		deleteObject:     gdi32.NewProc("DeleteObject"),
		getDC:            user32.NewProc("GetDC"),
		releaseDC:        user32.NewProc("ReleaseDC"),
		createCompatDC:   gdi32.NewProc("CreateCompatibleDC"),
		deleteDC:         gdi32.NewProc("DeleteDC"),
		selectObject:     gdi32.NewProc("SelectObject"),
		createDIBSection: gdi32.NewProc("CreateDIBSection"),
		drawIconEx:       user32.NewProc("DrawIconEx"),
		getDIBits:        gdi32.NewProc("GetDIBits"),
	}
}

// shellCOMReady reports a CoInitializeEx result that took a reference.
func shellCOMReady(err error) bool {
	return err == nil || err == syscall.Errno(1) // S_OK or S_FALSE
}

func shellFileTypeIconPNG(path string) ([]byte, error) {
	// Shell icon lookup is COM and must stay on one OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	shellIconProcOnce.Do(loadShellIconProcs)

	// S_OK is nil. S_FALSE (1) also succeeds and still increments this thread's
	// COM init count, so both must be paired with CoUninitialize. A changed
	// apartment (RPC_E_CHANGED_MODE) did not increment and must be left alone.
	hr := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	if shellCOMReady(hr) {
		defer windows.CoUninitialize()
	}

	// Documents resolve from the extension alone. Opening a large PDF just to
	// paint its chip would read the file for an icon the association already has.
	useAttributes := uint32(shellGFIUseFileAttributes)
	if fileTypeIconUsesOwnIcon(path) {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			useAttributes = 0
		}
	}

	img, err := shellAssociatedIcon(path, useAttributes)
	if err != nil {
		return nil, err
	}
	img = fitIcon(img, shellIconMaxEdge)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// shellAssociatedIcon prefers the jumbo image list, then 48px, then SHGetFileInfo.
// A handle with no visible pixels is discarded. Caching that blank result would
// make every later file of the same type show an empty chip.
func shellAssociatedIcon(path string, useAttributes uint32) (*image.NRGBA, error) {
	index, err := shellIconIndex(path, useAttributes)
	if err != nil {
		return nil, err
	}
	for _, size := range []uintptr{shellImageListJumbo, shellImageListExtraLarge} {
		hicon, listErr := shellImageListIcon(index, size)
		if listErr != nil || hicon == 0 {
			continue
		}
		if img, convErr := nrgbaFromHICON(hicon); convErr == nil {
			return img, nil
		}
	}
	hicon, err := shellLargeIcon(path, useAttributes)
	if err != nil {
		return nil, err
	}
	return nrgbaFromHICON(hicon)
}

func nrgbaFromHICON(hicon uintptr) (*image.NRGBA, error) {
	defer shellIconProc.destroyIcon.Call(hicon)
	img, err := hiconToNRGBA(hicon)
	if err != nil {
		return nil, err
	}
	img = trimIconAlpha(img)
	if !shellIconVisible(img) {
		return nil, errors.New("shell icon is empty")
	}
	return img, nil
}

func shellIconVisible(img *image.NRGBA) bool {
	if img == nil {
		return false
	}
	b := img.Bounds()
	if b.Dx() < 8 || b.Dy() < 8 || img.Stride < b.Dx()*4 {
		return false
	}
	pix := img.Pix
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := (y-img.Rect.Min.Y)*img.Stride + (b.Min.X-img.Rect.Min.X)*4
		if row < 0 || row+b.Dx()*4 > len(pix) {
			return false
		}
		for x := 0; x < b.Dx(); x++ {
			if pix[row+x*4+3] > 12 {
				return true
			}
		}
	}
	return false
}

func shellIconIndex(path string, useAttributes uint32) (int32, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var info shFileInfoW
	flags := uintptr(shellGFISysIconIndex | useAttributes)
	ret, _, callErr := shellIconProc.shGetFileInfo.Call(
		uintptr(unsafe.Pointer(ptr)),
		shellFileAttributeNormal,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
		flags,
	)
	if ret == 0 {
		if callErr != syscall.Errno(0) {
			return 0, callErr
		}
		return 0, errors.New("shell has no icon for this file type")
	}
	return info.iIcon, nil
}

func shellImageListIcon(index int32, listSize uintptr) (uintptr, error) {
	var list uintptr
	hr, _, _ := shellIconProc.shGetImageList.Call(
		listSize,
		uintptr(unsafe.Pointer(&shellImageListIID)),
		uintptr(unsafe.Pointer(&list)),
	)
	if int32(hr) < 0 || list == 0 {
		return 0, errors.New("shell image list is unavailable")
	}
	defer comRelease(list)

	vtbl := *(*uintptr)(unsafe.Pointer(list))
	getIcon := *(*uintptr)(unsafe.Pointer(vtbl + 10*unsafe.Sizeof(uintptr(0))))
	var hicon uintptr
	hr, _, _ = syscall.SyscallN(getIcon, list, uintptr(uint32(index)), shellILDTransparent, uintptr(unsafe.Pointer(&hicon)))
	if int32(hr) < 0 || hicon == 0 {
		if hicon != 0 {
			shellIconProc.destroyIcon.Call(hicon)
		}
		return 0, errors.New("shell image list has no icon")
	}
	return hicon, nil
}

func shellLargeIcon(path string, useAttributes uint32) (uintptr, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var info shFileInfoW
	flags := uintptr(shellGFIIcon | useAttributes)
	ret, _, callErr := shellIconProc.shGetFileInfo.Call(
		uintptr(unsafe.Pointer(ptr)),
		shellFileAttributeNormal,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
		flags,
	)
	if ret == 0 || info.hIcon == 0 {
		// A failed lookup can still hand back an icon. Drop it before the error.
		if info.hIcon != 0 {
			shellIconProc.destroyIcon.Call(info.hIcon)
		}
		if callErr != syscall.Errno(0) {
			return 0, callErr
		}
		return 0, errors.New("shell has no icon for this file type")
	}
	return info.hIcon, nil
}

func comRelease(obj uintptr) {
	if obj == 0 {
		return
	}
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	release := *(*uintptr)(unsafe.Pointer(vtbl + 2*unsafe.Sizeof(uintptr(0))))
	_, _, _ = syscall.SyscallN(release, obj)
}

func hiconToNRGBA(hicon uintptr) (*image.NRGBA, error) {
	var info shellIconInfo
	ok, _, _ := shellIconProc.getIconInfo.Call(hicon, uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return nil, errors.New("GetIconInfo failed")
	}
	if info.hbmMask != 0 {
		defer shellIconProc.deleteObject.Call(info.hbmMask)
	}
	if info.hbmColor != 0 {
		defer shellIconProc.deleteObject.Call(info.hbmColor)
	}

	width, height := 32, 32
	if info.hbmColor != 0 {
		if w, h, err := gdiBitmapSize(info.hbmColor); err == nil {
			width, height = w, h
		}
	}
	if info.hbmColor != 0 {
		if img, err := dibFromBitmap(info.hbmColor, width, height); err == nil && imageHasAlpha(img) {
			return unpremultiplyIcon(img), nil
		}
	}

	black, err := drawIconOn(hicon, width, height, false)
	if err != nil {
		return nil, err
	}
	if imageHasAlpha(black) {
		return unpremultiplyIcon(black), nil
	}
	white, err := drawIconOn(hicon, width, height, true)
	if err != nil {
		return black, nil
	}
	return reconstructMaskAlpha(black, white), nil
}

func gdiBitmapSize(hbm uintptr) (int, int, error) {
	var bmp shellWinBitmap
	ret, _, err := shellIconProc.getObject.Call(hbm, unsafe.Sizeof(bmp), uintptr(unsafe.Pointer(&bmp)))
	if ret == 0 || bmp.bmWidth <= 0 || bmp.bmHeight <= 0 || bmp.bmWidth > shellIconMaxBitmap || bmp.bmHeight > shellIconMaxBitmap {
		if err != syscall.Errno(0) && ret == 0 {
			return 0, 0, err
		}
		return 0, 0, errors.New("icon bitmap has no usable size")
	}
	return int(bmp.bmWidth), int(bmp.bmHeight), nil
}

func dibFromBitmap(hbm uintptr, width, height int) (*image.NRGBA, error) {
	if width <= 0 || height <= 0 {
		return nil, errors.New("icon bitmap has no size")
	}
	hdc, _, _ := shellIconProc.getDC.Call(0)
	if hdc == 0 {
		return nil, errors.New("GetDC failed")
	}
	defer shellIconProc.releaseDC.Call(0, hdc)
	mem, _, _ := shellIconProc.createCompatDC.Call(hdc)
	if mem == 0 {
		return nil, errors.New("CreateCompatibleDC failed")
	}
	defer shellIconProc.deleteDC.Call(mem)

	header := shellBitmapInfoHeader{
		size:     uint32(unsafe.Sizeof(shellBitmapInfoHeader{})),
		width:    int32(width),
		height:   -int32(height),
		planes:   1,
		bitCount: 32,
	}
	pixels := make([]byte, width*height*4)
	lines, _, _ := shellIconProc.getDIBits.Call(
		mem,
		hbm,
		0,
		uintptr(height),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&header)),
		shellDIBRGBColors,
	)
	if lines == 0 {
		return nil, errors.New("GetDIBits failed")
	}
	return bgraToNRGBA(pixels, width, height)
}

func drawIconOn(hicon uintptr, width, height int, white bool) (*image.NRGBA, error) {
	hdc, _, _ := shellIconProc.getDC.Call(0)
	if hdc == 0 {
		return nil, errors.New("GetDC failed")
	}
	defer shellIconProc.releaseDC.Call(0, hdc)
	mem, _, _ := shellIconProc.createCompatDC.Call(hdc)
	if mem == 0 {
		return nil, errors.New("CreateCompatibleDC failed")
	}
	defer shellIconProc.deleteDC.Call(mem)

	var bits *byte
	header := shellBitmapInfoHeader{
		size:     uint32(unsafe.Sizeof(shellBitmapInfoHeader{})),
		width:    int32(width),
		height:   -int32(height),
		planes:   1,
		bitCount: 32,
	}
	// BITMAPINFO is the header plus one unused color. The header must lead.
	type bitmapInfo struct {
		header shellBitmapInfoHeader
		colors [1]uint32
	}
	bmi := bitmapInfo{header: header}
	hbmp, _, _ := shellIconProc.createDIBSection.Call(
		mem,
		uintptr(unsafe.Pointer(&bmi)),
		shellDIBRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if hbmp == 0 || bits == nil {
		return nil, errors.New("CreateDIBSection failed")
	}
	defer shellIconProc.deleteObject.Call(hbmp)
	old, _, _ := shellIconProc.selectObject.Call(mem, hbmp)
	defer shellIconProc.selectObject.Call(mem, old)

	if white {
		raw := unsafe.Slice(bits, width*height*4)
		for i := 0; i < len(raw); i += 4 {
			raw[i] = 255
			raw[i+1] = 255
			raw[i+2] = 255
			raw[i+3] = 255
		}
	}
	ok, _, _ := shellIconProc.drawIconEx.Call(mem, 0, 0, hicon, uintptr(width), uintptr(height), 0, 0, shellDINormal)
	if ok == 0 {
		return nil, errors.New("DrawIconEx failed")
	}
	raw := append([]byte(nil), unsafe.Slice(bits, width*height*4)...)
	return bgraToNRGBA(raw, width, height)
}

func bgraToNRGBA(raw []byte, width, height int) (*image.NRGBA, error) {
	count := width * height * 4
	if width <= 0 || height <= 0 || count < 0 || len(raw) < count {
		return nil, errors.New("short icon bitmap")
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	dst := img.Pix
	for i := 0; i < count; i += 4 {
		dst[i] = raw[i+2]
		dst[i+1] = raw[i+1]
		dst[i+2] = raw[i]
		dst[i+3] = raw[i+3]
	}
	return img, nil
}

func imageHasAlpha(img *image.NRGBA) bool {
	pix := img.Pix
	for i := 3; i < len(pix); i += 4 {
		if pix[i] != 0 {
			return true
		}
	}
	return false
}

func unpremultiplyIcon(img *image.NRGBA) *image.NRGBA {
	needs := false
	for i := 3; i < len(img.Pix); i += 4 {
		a := img.Pix[i]
		if a != 0 && a != 255 {
			needs = true
			break
		}
	}
	if !needs {
		return img
	}
	out := image.NewNRGBA(img.Bounds())
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b, a := img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
		if a != 0 && a != 255 {
			r = scaleUnpremultiplied(r, a)
			g = scaleUnpremultiplied(g, a)
			b = scaleUnpremultiplied(b, a)
		}
		out.Pix[i] = r
		out.Pix[i+1] = g
		out.Pix[i+2] = b
		out.Pix[i+3] = a
	}
	return out
}

func scaleUnpremultiplied(v, a uint8) uint8 {
	scaled := uint32(v) * 255 / uint32(a)
	if scaled > 255 {
		return 255
	}
	return uint8(scaled)
}

func reconstructMaskAlpha(black, white *image.NRGBA) *image.NRGBA {
	b := black.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			bp := black.NRGBAAt(b.Min.X+x, b.Min.Y+y)
			wp := white.NRGBAAt(b.Min.X+x, b.Min.Y+y)
			dr := int(wp.R) - int(bp.R)
			dg := int(wp.G) - int(bp.G)
			db := int(wp.B) - int(bp.B)
			if dr < 0 {
				dr = 0
			}
			if dg < 0 {
				dg = 0
			}
			if db < 0 {
				db = 0
			}
			transparency := (dr + dg + db) / 3
			if transparency > 255 {
				transparency = 255
			}
			a := uint8(255 - transparency)
			r, g, bl := bp.R, bp.G, bp.B
			if a == 0 {
				r, g, bl = 0, 0, 0
			}
			out.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: bl, A: a})
		}
	}
	return out
}

func trimIconAlpha(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if src.NRGBAAt(x, y).A <= 12 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < minX || maxY < minY {
		return src
	}
	if minX <= b.Min.X && minY <= b.Min.Y && maxX >= b.Max.X-1 && maxY >= b.Max.Y-1 {
		return src
	}
	if minX > b.Min.X {
		minX--
	}
	if minY > b.Min.Y {
		minY--
	}
	if maxX+1 < b.Max.X {
		maxX++
	}
	if maxY+1 < b.Max.Y {
		maxY++
	}
	cropped := image.NewNRGBA(image.Rect(0, 0, maxX-minX+1, maxY-minY+1))
	draw.Draw(cropped, cropped.Bounds(), src, image.Point{X: minX, Y: minY}, draw.Src)
	return cropped
}

func fitIcon(src *image.NRGBA, maxEdge int) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxEdge && h <= maxEdge {
		return src
	}
	nw, nh := w, h
	if w >= h {
		nh = max(1, h*maxEdge/w)
		nw = maxEdge
	} else {
		nw = max(1, w*maxEdge/h)
		nh = maxEdge
	}
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}
