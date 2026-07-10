//go:build windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	mbOK              = 0x00000000
	mbOKCancel        = 0x00000001
	mbIconInformation = 0x00000040
	mbIconError       = 0x00000010
	idOK              = 1

	wsOverlapped    = 0x00000000
	wsCaption       = 0x00C00000
	wsSysMenu       = 0x00080000
	wsMinimizeBox   = 0x00020000
	wsVisible       = 0x10000000
	wsChild         = 0x40000000
	wsTabStop       = 0x00010000
	wsGroup         = 0x00020000
	ssLeft          = 0x00000000
	bsPushButton    = 0x00000000
	bsDefPushButton = 0x00000001
	bsAutoRadio     = 0x00000009
	bsGroupBox      = 0x00000007
	wsClipChildren  = 0x02000000
	swpNoZOrder     = 0x0004

	wizardWindowStyle  = wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren
	wizardClientWidth  = 780
	wizardClientHeight = 540

	// Layout geometry: content area starts after sidebar
	wizardSidebarWidth = 178
	wizardContentLeft  = 194                                   // sidebar + gap
	wizardContentRight = 750                                   // right margin from client edge
	wizardPanelInset   = 20                                    // inset from content edges to panel interior
	wizardPanelLeft    = wizardContentLeft + wizardPanelInset  // 214
	wizardPanelRight   = wizardContentRight - wizardPanelInset // 730
	wizardBrandItemH   = 66                                    // vertical stride between brand options
	wizardBrandCardH   = 62                                    // visible card height (< stride to leave gap)
	wizardButtonW      = 90
	wizardButtonH      = 30
	wizardButtonGap    = 16 // horizontal gap between buttons

	cwUseDefault      = 0x80000000
	swHide            = 0
	swShow            = 5
	wmCreate          = 0x0001
	wmDestroy         = 0x0002
	wmCommand         = 0x0111
	wmSetIcon         = 0x0080
	wmLButtonDown     = 0x0201
	wmPaint           = 0x000F
	wmSetFont         = 0x0030
	wmCtlColorBtn     = 0x0135
	wmCtlColorStatic  = 0x0138
	wmInstallDone     = 0x8001
	wmInstallProgress = 0x8002

	colorWindow      = 5
	defaultGUIFont   = 17
	dibRGBColors     = 0
	srccopy          = 0x00CC0020
	halftone         = 4
	dtLeft           = 0x00000000
	dtRight          = 0x00000002
	dtSingleLine     = 0x00000020
	dtWordBreak      = 0x00000010
	dtNoPrefix       = 0x00000800
	bkTransparent    = 1
	wmUser           = 0x0400
	pbmSetPos        = wmUser + 2
	pbmSetRange32    = wmUser + 6
	iccProgressClass = 0x00000020
	idBrandFirst     = 1001
	idWizardNext     = 1101
	idWizardCancel   = 1102
	iconSmall        = 0
	iconBig          = 1
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	Paint     rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
}

type wizardLogoDIB struct {
	Pixels []byte
	Info   bitmapInfo
	Width  int32
	Height int32
}
type initCommonControlsEx struct {
	Size uint32
	ICC  uint32
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	gdi32                    = syscall.NewLazyDLL("gdi32.dll")
	comctl32                 = syscall.NewLazyDLL("comctl32.dll")
	procMessageBoxW          = user32.NewProc("MessageBoxW")
	procRegisterClassExW     = user32.NewProc("RegisterClassExW")
	procSetProcessDPIAware   = user32.NewProc("SetProcessDPIAware")
	procCreateWindowExW      = user32.NewProc("CreateWindowExW")
	procAdjustWindowRectEx   = user32.NewProc("AdjustWindowRectEx")
	procDefWindowProcW       = user32.NewProc("DefWindowProcW")
	procDestroyWindow        = user32.NewProc("DestroyWindow")
	procShowWindow           = user32.NewProc("ShowWindow")
	procUpdateWindow         = user32.NewProc("UpdateWindow")
	procGetMessageW          = user32.NewProc("GetMessageW")
	procTranslateMessage     = user32.NewProc("TranslateMessage")
	procDispatchMessageW     = user32.NewProc("DispatchMessageW")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procSetWindowTextW       = user32.NewProc("SetWindowTextW")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procEnableWindow         = user32.NewProc("EnableWindow")
	procInvalidateRect       = user32.NewProc("InvalidateRect")
	procCheckRadioButton     = user32.NewProc("CheckRadioButton")
	procIsDlgButtonChecked   = user32.NewProc("IsDlgButtonChecked")
	procSendMessageW         = user32.NewProc("SendMessageW")
	procGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	procGetStockObject       = gdi32.NewProc("GetStockObject")
	procBeginPaint           = user32.NewProc("BeginPaint")
	procEndPaint             = user32.NewProc("EndPaint")
	procDrawTextW            = user32.NewProc("DrawTextW")
	procFillRect             = user32.NewProc("FillRect")
	procSetStretchBltMode    = gdi32.NewProc("SetStretchBltMode")
	procStretchDIBits        = gdi32.NewProc("StretchDIBits")
	procCreateSolidBrush     = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject         = gdi32.NewProc("DeleteObject")
	procSetBkMode            = gdi32.NewProc("SetBkMode")
	procSetTextColor         = gdi32.NewProc("SetTextColor")
	procCreateFontW          = gdi32.NewProc("CreateFontW")
	procSelectObject         = gdi32.NewProc("SelectObject")
	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")

	wizardSurfaceBrush uintptr
	wizardPanelBrush   uintptr
	wizardSidebarBrush uintptr

	wizardLogoOnce sync.Once
	wizardLogoData wizardLogoDIB
	wizardLogoErr  error

	wizardSelected         = 0
	wizardOK               = false
	wizardFont             uintptr
	wizardTitleFont        uintptr
	wizardSidebarFont      uintptr
	wizardFontOwned        bool
	wizardTitleFontOwned   bool
	wizardSidebarFontOwned bool
	wizardHwnd             uintptr
	wizardDPIOnce          sync.Once

	wizardInstallMode    = false
	wizardInstallState   = 0
	wizardCurrentVersion = ""
	wizardCheckOnly      = false
	wizardNoLaunch       = false
	wizardInstallCancel  context.CancelFunc
	wizardResult         installResult
	wizardInstallErr     error
	wizardProgressText   = ""
	wizardProgressPct    = int64(0)
	wizardMu             sync.Mutex

	wizardHeaderHwnd   uintptr
	wizardIntroHwnd    uintptr
	wizardGroupHwnd    uintptr
	wizardBrandHwnds   []uintptr
	wizardDetailHwnd   uintptr
	wizardNextHwnd     uintptr
	wizardCancelHwnd   uintptr
	wizardProgressHwnd uintptr
)

func guiChooseBrand(defaultBrand brandOption) (brandOption, bool) {
	selected, ok := showBrandWizard(defaultBrand)
	if !ok {
		return brandOption{}, false
	}
	return selected, true
}

func guiRunInstallWizard(defaultBrand brandOption, currentVersion string, checkOnly, noLaunch bool) (bool, error) {
	wizardInstallMode = true
	wizardInstallState = 0
	wizardCurrentVersion = currentVersion
	wizardCheckOnly = checkOnly
	wizardNoLaunch = noLaunch
	wizardInstallErr = nil
	wizardInstallCancel = nil
	wizardResult = installResult{}
	wizardProgressText = ""
	defer func() { wizardInstallMode = false }()

	_, ok := showBrandWizard(defaultBrand)
	if !ok && wizardInstallErr == nil {
		return true, nil
	}
	return true, wizardInstallErr
}

func showBrandWizard(defaultBrand brandOption) (brandOption, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	wizardSelected = 0
	for i, brand := range brandOptions {
		if defaultBrand.ID == brand.ID {
			wizardSelected = i
			break
		}
	}
	wizardOK = false
	initWizardDPI()
	initWizardFonts()
	initWizardBrushes()
	initProgressControls()

	className := utf16Ptr("InsMaclawWizardWindow")
	instance := currentModuleHandle()
	icon := loadWindowIconBig()
	smallIcon := loadWindowIconSmall()
	wc := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    syscall.NewCallback(wizardWndProc),
		Icon:       icon,
		Instance:   instance,
		Background: colorWindow + 1,
		ClassName:  className,
		IconSm:     smallIcon,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	width, height := adjustedWindowSize(wizardClientWidth, wizardClientHeight, wizardWindowStyle, 0)
	x, y := centerPoint(width, height)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr(tr("app.title")))),
		wizardWindowStyle,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		return brandOption{}, false
	}
	wizardHwnd = hwnd
	setWindowIcons(hwnd, smallIcon, icon)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		if wizardOK || wizardSelected == -1 {
			break
		}
	}
	if !wizardOK {
		return brandOption{}, false
	}
	if wizardSelected >= 0 && wizardSelected < len(brandOptions) {
		return brandOptions[wizardSelected], true
	}
	return brandOptions[0], true
}

func wizardWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmCreate:
		createWizardControls(hwnd)
		return 0
	case wmPaint:
		paintWizard(hwnd)
		return 0
	case wmCtlColorStatic, wmCtlColorBtn:
		return paintControlBackground(wParam, lParam)
	case wmLButtonDown:
		if wizardInstallMode && wizardInstallState == 0 {
			selectBrandFromPoint(lParam)
		}
		return 0
	case wmCommand:
		id := int(wParam & 0xffff)
		if id >= idBrandFirst && id < idBrandFirst+len(brandOptions) {
			selectWizardBrand(id - idBrandFirst)
			return 0
		}
		switch id {
		case idWizardNext:
			if wizardInstallMode {
				if wizardInstallState == 2 {
					wizardOK = true
					procDestroyWindow.Call(hwnd)
					return 0
				}
				if wizardInstallState == 0 {
					startWizardInstall(hwnd)
				}
				return 0
			}
			updateWizardSelectedFromControls(hwnd)
			wizardOK = true
			procDestroyWindow.Call(hwnd)
		case idWizardCancel:
			if wizardInstallMode && wizardInstallState == 1 {
				if wizardInstallCancel != nil {
					wizardInstallCancel()
				}
				setControlText(wizardDetailHwnd, tr("cancelling"))
				enableControl(wizardCancelHwnd, false)
				return 0
			}
			wizardSelected = -1
			procDestroyWindow.Call(hwnd)
		}
	case wmInstallProgress:
		if wizardInstallMode && wizardInstallState == 1 {
			wizardMu.Lock()
			progress := wizardProgressText
			pct := wizardProgressPct
			wizardMu.Unlock()
			if progress != "" {
				setControlText(wizardDetailHwnd, progress)
				setProgress(pct)
			}
		}
		return 0
	case wmInstallDone:
		if wizardInstallMode {
			if finishWizardInstall(hwnd) {
				return 0
			}
		}
		return 0
	case wmDestroy:
		if wizardInstallCancel != nil {
			wizardInstallCancel()
			wizardInstallCancel = nil
		}
		if !wizardOK {
			wizardSelected = -1
		}
		cleanupWizardResources()
		return 0
	}
	return defWindowProc(hwnd, msg, wParam, lParam)
}

func createWizardControls(hwnd uintptr) {
	contentW := int32(wizardContentRight - wizardContentLeft - 32)
	wizardHeaderHwnd = addStatic(hwnd, tr("welcome.title"), wizardContentLeft+16, 34, contentW, 30)
	setControlFont(wizardHeaderHwnd, wizardTitleFont)
	wizardIntroHwnd = addStatic(hwnd, tr("welcome.body"), wizardContentLeft+16, 74, contentW, 58)
	wizardGroupHwnd = addStatic(hwnd, tr("choose.brand"), wizardContentLeft+16, 168, contentW, 22)
	wizardBrandHwnds = make([]uintptr, 0, len(brandOptions))
	for i, brand := range brandOptions {
		wizardBrandHwnds = append(wizardBrandHwnds, addRadio(hwnd, idBrandFirst+i, brandLabel(brand), wizardPanelLeft+12, brandOptionTop(i)+8, wizardPanelRight-wizardPanelLeft-24, 24))
	}
	detailY := int32(brandOptionTop(len(brandOptions)-1)) + wizardBrandCardH + 18
	maxDetailY := int32(wizardClientHeight) - wizardButtonH - 26 - 16 - 30 // must be above separator - gap
	if detailY > maxDetailY {
		detailY = maxDetailY
	}
	wizardDetailHwnd = addStatic(hwnd, tr("step.select"), wizardContentLeft+16, detailY, contentW, 28)
	wizardProgressHwnd = addProgress(hwnd, wizardContentLeft+16, detailY-42, contentW, 12)
	showControl(wizardProgressHwnd, false)
	btnY := int32(wizardClientHeight) - wizardButtonH - 26 // anchor from bottom
	btnCancelRight := wizardContentRight - 10
	btnCancelLeft := btnCancelRight - wizardButtonW
	btnNextRight := btnCancelLeft - wizardButtonGap
	btnNextLeft := btnNextRight - wizardButtonW
	wizardNextHwnd = addButton(hwnd, idWizardNext, tr("next"), int32(btnNextLeft), btnY, wizardButtonW, wizardButtonH, bsDefPushButton)
	wizardCancelHwnd = addButton(hwnd, idWizardCancel, tr("cancel"), int32(btnCancelLeft), btnY, wizardButtonW, wizardButtonH, bsPushButton)
	checkWizardBrandRadio(hwnd)
}

func selectBrandFromPoint(lParam uintptr) {
	x := int32(lParam & 0xffff)
	y := int32((lParam >> 16) & 0xffff)
	if x >= wizardPanelLeft && x <= wizardPanelRight {
		for i := range brandOptions {
			top := brandOptionTop(i)
			if y >= top && y <= top+wizardBrandCardH {
				selectWizardBrand(i)
				return
			}
		}
	}
}

func selectWizardBrand(index int) {
	if index < 0 || index >= len(brandOptions) {
		return
	}
	wizardSelected = index
	checkWizardBrandRadio(wizardHwnd)
	invalidateWizard()
}

func checkWizardBrandRadio(hwnd uintptr) {
	if hwnd != 0 && len(brandOptions) > 0 {
		procCheckRadioButton.Call(hwnd, idBrandFirst, idBrandFirst+uintptr(len(brandOptions)-1), uintptr(idBrandFirst+wizardSelected))
	}
}

func updateWizardSelectedFromControls(hwnd uintptr) {
	for i := range brandOptions {
		if checked(hwnd, idBrandFirst+i) {
			wizardSelected = i
			return
		}
	}
	wizardSelected = 0
}

func brandOptionTop(index int) int32 {
	return int32(194 + index*wizardBrandItemH)
}

func startWizardInstall(hwnd uintptr) {
	updateWizardSelectedFromControls(hwnd)
	brand := brandOptions[wizardSelected]
	wizardInstallState = 1
	setControlText(wizardHeaderHwnd, fmt.Sprintf(tr("installing"), brand.ProductName))
	setControlText(wizardIntroHwnd, brandLabel(brand))
	setControlText(wizardDetailHwnd, tr("step.download")+"\n"+tr("checking"))
	configureWizardBusyLayout()
	setProgress(0)
	showControl(wizardProgressHwnd, true)
	invalidateWizard()
	setControlText(wizardNextHwnd, tr("working"))
	enableWizardBrandControls(false)
	enableControl(wizardNextHwnd, false)
	enableControl(wizardCancelHwnd, true)

	currentVersion := wizardCurrentVersion
	checkOnly := wizardCheckOnly
	noLaunch := wizardNoLaunch
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	wizardInstallCancel = cancel
	go func() {
		defer cancel()
		lastProgress := int64(-1)
		result, err := runInstall(ctx, installOptions{
			Brand:          brand,
			CurrentVersion: currentVersion,
			CheckOnly:      checkOnly,
			NoLaunch:       noLaunch,
			WaitInstaller:  true,
			Log: func(message string) {
				wizardMu.Lock()
				wizardProgressText = message
				wizardMu.Unlock()
				procPostMessageW.Call(hwnd, wmInstallProgress, 0, 0)
			},
			Progress: func(downloaded, total int64) {
				text := fmt.Sprintf(tr("downloading.bytes"), humanBytes(downloaded))
				if total > 0 {
					pct := downloaded * 100 / total
					if pct == lastProgress || (pct != 100 && pct-lastProgress < 3) {
						return
					}
					lastProgress = pct
					text = fmt.Sprintf(tr("downloading.percent"), pct, humanBytes(downloaded), humanBytes(total))
				}
				wizardMu.Lock()
				wizardProgressText = text
				if total > 0 {
					wizardProgressPct = downloaded * 100 / total
				}
				wizardMu.Unlock()
				procPostMessageW.Call(hwnd, wmInstallProgress, 0, 0)
			},
		})
		wizardMu.Lock()
		wizardResult = result
		wizardInstallErr = err
		wizardMu.Unlock()
		procPostMessageW.Call(hwnd, wmInstallDone, 0, 0)
	}()
}

func finishWizardInstall(hwnd uintptr) bool {
	wizardMu.Lock()
	result := wizardResult
	err := wizardInstallErr
	wizardMu.Unlock()
	wizardInstallState = 2
	wizardInstallCancel = nil
	if err == nil && !result.Skipped && !wizardCheckOnly && !wizardNoLaunch {
		wizardOK = true
		procDestroyWindow.Call(hwnd)
		return true
	}
	configureWizardDoneLayout()
	enableControl(wizardNextHwnd, true)
	setControlText(wizardNextHwnd, tr("close"))
	setControlText(wizardCancelHwnd, tr("close"))
	showControl(wizardCancelHwnd, false)
	enableControl(wizardCancelHwnd, false)
	if err != nil {
		setControlText(wizardHeaderHwnd, tr("failed.title"))
		setControlText(wizardIntroHwnd, tr("failed.body"))
		setControlText(wizardDetailHwnd, err.Error())
		showControl(wizardProgressHwnd, false)
		setProgress(0)
		invalidateWizard()
		return false
	}
	setControlText(wizardHeaderHwnd, tr("completed.title"))
	setControlText(wizardIntroHwnd, tr("completed.body"))
	setControlText(wizardDetailHwnd, tr("step.done")+"\n"+guiResultMessage(result, wizardCheckOnly, wizardNoLaunch))
	setProgress(100)
	invalidateWizard()
	return false
}

func setControlText(hwnd uintptr, text string) {
	if hwnd == 0 {
		return
	}
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func enableControl(hwnd uintptr, enabled bool) {
	value := uintptr(0)
	if enabled {
		value = 1
	}
	if hwnd != 0 {
		procEnableWindow.Call(hwnd, value)
	}
}
func showControl(hwnd uintptr, visible bool) {
	if hwnd == 0 {
		return
	}
	cmd := uintptr(swHide)
	if visible {
		cmd = swShow
	}
	procShowWindow.Call(hwnd, cmd)
}

func moveControl(hwnd uintptr, x, y, width, height int32) {
	if hwnd != 0 {
		procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(width), uintptr(height), swpNoZOrder)
	}
}

func configureWizardBusyLayout() {
	showControl(wizardGroupHwnd, false)
	showWizardBrandControls(false)
	contentW := int32(wizardContentRight - wizardContentLeft - 32)
	moveControl(wizardDetailHwnd, wizardContentLeft+16, 190, contentW, 92)
	moveControl(wizardProgressHwnd, wizardContentLeft+16, 306, contentW, 14)
}

func configureWizardDoneLayout() {
	showControl(wizardGroupHwnd, false)
	showWizardBrandControls(false)
	showControl(wizardProgressHwnd, false)
	contentW := int32(wizardContentRight - wizardContentLeft - 32)
	moveControl(wizardDetailHwnd, wizardContentLeft+16, 190, contentW, 178)
}

func enableWizardBrandControls(enabled bool) {
	for _, hwnd := range wizardBrandHwnds {
		enableControl(hwnd, enabled)
	}
}

func showWizardBrandControls(visible bool) {
	for _, hwnd := range wizardBrandHwnds {
		showControl(hwnd, visible)
	}
}

func paintWizard(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	fill(hdc, 0, 0, wizardClientWidth, wizardClientHeight, rgb(248, 250, 252))
	fill(hdc, 0, 0, wizardSidebarWidth, wizardClientHeight, rgb(238, 246, 255))
	fill(hdc, wizardSidebarWidth, 0, wizardSidebarWidth+2, wizardClientHeight, rgb(219, 234, 254))
	fill(hdc, 0, 0, wizardClientWidth, 6, rgb(59, 130, 246))
	drawWizardLogo(hdc)
	if wizardInstallState == 0 {
		lastBrandBottom := brandOptionTop(len(brandOptions)-1) + wizardBrandCardH + 4
		panelBottom := lastBrandBottom + 6
		drawPanel(hdc, wizardContentLeft, 155, wizardContentRight, panelBottom)
		for i, brand := range brandOptions {
			top := brandOptionTop(i)
			drawBrandOptionPanel(hdc, i, wizardPanelLeft, top, wizardSelected == i)
			drawTextWithFont(hdc, brandDescription(brand), wizardPanelLeft+40, top+32, wizardPanelRight-10, top+wizardBrandCardH, rgb(100, 116, 139), dtLeft|dtWordBreak|dtNoPrefix, wizardFont)
		}
	} else if wizardInstallState == 1 {
		drawPanel(hdc, wizardContentLeft, 158, wizardContentRight, 342)
	} else {
		drawPanel(hdc, wizardContentLeft, 158, wizardContentRight, 398)
	}
	drawTextWithFont(hdc, tr("sidebar.subtitle"), 30, 166, 156, 190, rgb(59, 130, 246), dtLeft|dtWordBreak|dtNoPrefix, wizardFont)
	drawWizardSteps(hdc)
	drawTextWithFont(hdc, tr("language"), 30, 326, 164, 350, rgb(71, 85, 105), dtLeft|dtWordBreak|dtNoPrefix, wizardFont)
	drawTextWithFont(hdc, tr("sidebar.secure"), 30, 356, 166, 402, rgb(100, 116, 139), dtLeft|dtWordBreak|dtNoPrefix, wizardFont)
	drawTextWithFont(hdc, "v"+version, 594, 34, wizardContentRight, 56, rgb(100, 116, 139), dtRight|dtSingleLine|dtNoPrefix, wizardFont)
	separatorY := int32(wizardClientHeight) - wizardButtonH - 26 - 16 // above buttons
	fill(hdc, wizardContentLeft, separatorY, wizardContentRight, separatorY+2, rgb(226, 232, 240))
}

func brandDescription(brand brandOption) string {
	switch brand.ID {
	case "qianxin":
		return tr("brand.tiger.desc")
	case "metastaff":
		return tr("brand.meta.desc")
	default:
		return tr("brand.maclaw.desc")
	}
}

func drawWizardLogo(hdc uintptr) {
	logo, err := getWizardLogoDIB()
	if err != nil {
		fill(hdc, 38, 58, 96, 116, rgb(37, 99, 235))
		drawTextWithFont(hdc, "M", 56, 73, 90, 104, rgb(255, 255, 255), dtLeft|dtSingleLine|dtNoPrefix, wizardTitleFont)
		return
	}
	oldMode, _, _ := procSetStretchBltMode.Call(hdc, halftone)
	defer procSetStretchBltMode.Call(hdc, oldMode)
	destW := int32(118)
	destH := int32(int64(destW) * int64(logo.Height) / int64(logo.Width))
	if destH > 92 {
		destH = 92
		destW = int32(int64(destH) * int64(logo.Width) / int64(logo.Height))
	}
	destX := int32(24) + (118-destW)/2
	destY := int32(58) + (92-destH)/2
	procStretchDIBits.Call(
		hdc,
		uintptr(destX), uintptr(destY), uintptr(destW), uintptr(destH),
		0, 0, uintptr(logo.Width), uintptr(logo.Height),
		uintptr(unsafe.Pointer(&logo.Pixels[0])),
		uintptr(unsafe.Pointer(&logo.Info)),
		dibRGBColors, srccopy,
	)
}

func getWizardLogoDIB() (wizardLogoDIB, error) {
	wizardLogoOnce.Do(func() {
		wizardLogoData, wizardLogoErr = decodeWizardLogoDIB()
	})
	return wizardLogoData, wizardLogoErr
}

func decodeWizardLogoDIB() (wizardLogoDIB, error) {
	img, err := png.Decode(bytes.NewReader(wizardLogoPNG))
	if err != nil {
		return wizardLogoDIB{}, err
	}
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return wizardLogoDIB{}, fmt.Errorf("invalid logo dimensions: %dx%d", srcW, srcH)
	}
	pixels := make([]byte, srcW*srcH*4)
	bgR, bgG, bgB := uint32(238), uint32(246), uint32(255)
	for y := 0; y < srcH; y++ {
		for x := 0; x < srcW; x++ {
			r, g, b, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			alpha := a >> 8
			inv := 255 - alpha
			outR := ((r>>8)*alpha + bgR*inv) / 255
			outG := ((g>>8)*alpha + bgG*inv) / 255
			outB := ((b>>8)*alpha + bgB*inv) / 255
			i := (y*srcW + x) * 4
			pixels[i+0] = byte(outB)
			pixels[i+1] = byte(outG)
			pixels[i+2] = byte(outR)
		}
	}
	info := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(srcW),
		Height:      -int32(srcH),
		Planes:      1,
		BitCount:    32,
		Compression: 0,
		SizeImage:   uint32(len(pixels)),
	}}
	return wizardLogoDIB{Pixels: pixels, Info: info, Width: int32(srcW), Height: int32(srcH)}, nil
}

func drawPanel(hdc uintptr, left, top, right, bottom int32) {
	fill(hdc, left, top, right, bottom, rgb(255, 255, 255))
	fill(hdc, left, top, right, top+1, rgb(226, 232, 240))
	fill(hdc, left, bottom-1, right, bottom, rgb(226, 232, 240))
	fill(hdc, left, top, left+1, bottom, rgb(226, 232, 240))
	fill(hdc, right-1, top, right, bottom, rgb(226, 232, 240))
}

func drawBrandOptionPanel(hdc uintptr, index int, left, top int32, selected bool) {
	_ = index
	right := int32(wizardPanelRight)
	bottom := top + wizardBrandCardH
	fillColor := rgb(255, 255, 255)
	borderColor := rgb(226, 232, 240)
	accentColor := rgb(203, 213, 225)
	if selected {
		borderColor = rgb(125, 211, 252)
		accentColor = rgb(14, 165, 233)
	}
	fill(hdc, left, top, right, bottom, fillColor)
	fill(hdc, left, top, right, top+1, borderColor)
	fill(hdc, left, bottom-1, right, bottom, borderColor)
	fill(hdc, left, top, left+1, bottom, borderColor)
	fill(hdc, right-1, top, right, bottom, borderColor)
	fill(hdc, left, top, left+4, bottom, accentColor)
}

func drawWizardSteps(hdc uintptr) {
	current := wizardInstallState
	if current < 0 {
		current = 0
	}
	drawWizardStep(hdc, 0, 208, "1", tr("side.step.select"), current >= 0, current == 0)
	drawWizardStep(hdc, 1, 248, "2", tr("side.step.download"), current >= 1, current == 1)
	drawWizardStep(hdc, 2, 288, "3", tr("side.step.done"), current >= 2, current == 2)
}

func drawWizardStep(hdc uintptr, index int, y int32, number, label string, complete, active bool) {
	_ = index
	boxColor := rgb(203, 213, 225)
	textColor := rgb(71, 85, 105)
	if complete {
		boxColor = rgb(59, 130, 246)
		textColor = rgb(30, 64, 175)
	}
	if active {
		boxColor = rgb(37, 99, 235)
		textColor = rgb(15, 23, 42)
	}
	fill(hdc, 26, y, 48, y+22, boxColor)
	drawTextWithFont(hdc, number, 33, y+2, 46, y+20, rgb(255, 255, 255), dtLeft|dtSingleLine|dtNoPrefix, wizardFont)
	drawTextWithFont(hdc, label, 58, y+1, 168, y+24, textColor, dtLeft|dtSingleLine|dtNoPrefix, wizardFont)
}

func initWizardDPI() {
	wizardDPIOnce.Do(func() {
		procSetProcessDPIAware.Call()
	})
}

func initWizardFonts() {
	deleteWizardFonts()
	face := "Segoe UI"
	if activeLanguage == langChinese {
		face = "Microsoft YaHei UI"
	}
	wizardFont, wizardFontOwned = createOwnedFont(-16, 400, face)
	wizardTitleFont, wizardTitleFontOwned = createOwnedFont(-22, 600, face)
	wizardSidebarFont, wizardSidebarFontOwned = createOwnedFont(-18, 600, face)
	if wizardFont == 0 {
		wizardFont, _, _ = procGetStockObject.Call(defaultGUIFont)
		wizardFontOwned = false
	}
	if wizardTitleFont == 0 {
		wizardTitleFont = wizardFont
		wizardTitleFontOwned = false
	}
	if wizardSidebarFont == 0 {
		wizardSidebarFont = wizardFont
		wizardSidebarFontOwned = false
	}
}

func createOwnedFont(height int32, weight int32, face string) (uintptr, bool) {
	font := createFont(height, weight, face)
	return font, font != 0
}

func deleteWizardFonts() {
	if wizardFontOwned {
		procDeleteObject.Call(wizardFont)
	}
	if wizardTitleFontOwned {
		procDeleteObject.Call(wizardTitleFont)
	}
	if wizardSidebarFontOwned {
		procDeleteObject.Call(wizardSidebarFont)
	}
	wizardFont, wizardTitleFont, wizardSidebarFont = 0, 0, 0
	wizardFontOwned, wizardTitleFontOwned, wizardSidebarFontOwned = false, false, false
}

func createFont(height int32, weight int32, face string) uintptr {
	font, _, _ := procCreateFontW.Call(
		uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(utf16Ptr(face))),
	)
	return font
}

func initWizardBrushes() {
	deleteBrush(wizardSurfaceBrush)
	deleteBrush(wizardPanelBrush)
	deleteBrush(wizardSidebarBrush)
	wizardSurfaceBrush, _, _ = procCreateSolidBrush.Call(rgb(248, 250, 252))
	wizardPanelBrush, _, _ = procCreateSolidBrush.Call(rgb(255, 255, 255))
	wizardSidebarBrush, _, _ = procCreateSolidBrush.Call(rgb(238, 246, 255))
}

func paintControlBackground(hdc, child uintptr) uintptr {
	procSetBkMode.Call(hdc, bkTransparent)
	procSetTextColor.Call(hdc, rgb(15, 23, 42))
	if isWizardBrandControl(child) || child == wizardGroupHwnd || (child == wizardDetailHwnd && wizardInstallState > 0) {
		return nonZeroBrush(wizardPanelBrush, wizardSurfaceBrush)
	}
	return nonZeroBrush(wizardSurfaceBrush, wizardPanelBrush)
}

func isWizardBrandControl(child uintptr) bool {
	for _, hwnd := range wizardBrandHwnds {
		if child == hwnd {
			return true
		}
	}
	return false
}

func nonZeroBrush(primary, fallback uintptr) uintptr {
	if primary != 0 {
		return primary
	}
	return fallback
}

func cleanupWizardResources() {
	deleteBrush(wizardSurfaceBrush)
	deleteBrush(wizardPanelBrush)
	deleteBrush(wizardSidebarBrush)
	wizardSurfaceBrush, wizardPanelBrush, wizardSidebarBrush = 0, 0, 0
	deleteWizardFonts()
}

func deleteBrush(brush uintptr) {
	if brush != 0 {
		procDeleteObject.Call(brush)
	}
}

func fill(hdc uintptr, left, top, right, bottom int32, color uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(color)
	if brush == 0 {
		return
	}
	r := rect{Left: left, Top: top, Right: right, Bottom: bottom}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brush)
	procDeleteObject.Call(brush)
}

func drawText(hdc uintptr, text string, left, top, right, bottom int32, color uintptr, flags uintptr) {
	drawTextWithFont(hdc, text, left, top, right, bottom, color, flags, wizardFont)
}

func drawTextWithFont(hdc uintptr, text string, left, top, right, bottom int32, color uintptr, flags uintptr, font uintptr) {
	var oldFont uintptr
	if font != 0 {
		oldFont, _, _ = procSelectObject.Call(hdc, font)
	}
	procSetBkMode.Call(hdc, bkTransparent)
	procSetTextColor.Call(hdc, color)
	r := rect{Left: left, Top: top, Right: right, Bottom: bottom}
	ptr := utf16Ptr(text)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(ptr)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags)
	if oldFont != 0 {
		procSelectObject.Call(hdc, oldFont)
	}
}

func rgb(r, g, b byte) uintptr {
	return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16
}
func initProgressControls() {
	icc := initCommonControlsEx{Size: uint32(unsafe.Sizeof(initCommonControlsEx{})), ICC: iccProgressClass}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
}

func addProgress(parent uintptr, x, y, w, h int32) uintptr {
	hwnd := addControl(parent, "msctls_progress32", "", wsChild|wsVisible, 0, x, y, w, h, 0)
	if hwnd != 0 {
		procSendMessageW.Call(hwnd, pbmSetRange32, 0, 100)
		procSendMessageW.Call(hwnd, pbmSetPos, 0, 0)
	}
	return hwnd
}

func setProgress(percent int64) {
	if wizardProgressHwnd == 0 {
		return
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	procSendMessageW.Call(wizardProgressHwnd, pbmSetPos, uintptr(percent), 0)
}
func invalidateWizard() {
	if wizardHwnd != 0 {
		procInvalidateRect.Call(wizardHwnd, 0, 1)
	}
}

func addStatic(parent uintptr, text string, x, y, w, h int32) uintptr {
	return addControl(parent, "STATIC", text, wsChild|wsVisible|ssLeft, 0, x, y, w, h, 0)
}

func addGroup(parent uintptr, text string, x, y, w, h int32) uintptr {
	return addControl(parent, "BUTTON", text, wsChild|wsVisible|bsGroupBox, 0, x, y, w, h, 0)
}

func addRadio(parent uintptr, id int, text string, x, y, w, h int32) uintptr {
	return addControl(parent, "BUTTON", text, wsChild|wsVisible|wsTabStop|bsAutoRadio, 0, x, y, w, h, uintptr(id))
}

func addButton(parent uintptr, id int, text string, x, y, w, h int32, style uintptr) uintptr {
	return addControl(parent, "BUTTON", text, wsChild|wsVisible|wsTabStop|style, 0, x, y, w, h, uintptr(id))
}

func addControl(parent uintptr, className, text string, style, exStyle uintptr, x, y, w, h int32, id uintptr) uintptr {
	hwnd, _, _ := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		style,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, id, 0, 0,
	)
	if hwnd != 0 && wizardFont != 0 {
		setControlFont(hwnd, wizardFont)
	}
	return hwnd
}

func setControlFont(hwnd uintptr, font uintptr) {
	if hwnd != 0 && font != 0 {
		procSendMessageW.Call(hwnd, wmSetFont, font, 1)
	}
}

func checked(hwnd uintptr, id int) bool {
	ret, _, _ := procIsDlgButtonChecked.Call(hwnd, uintptr(id))
	return ret != 0
}

func adjustedWindowSize(clientWidth, clientHeight int32, style, exStyle uintptr) (int32, int32) {
	r := rect{Right: clientWidth, Bottom: clientHeight}
	ret, _, _ := procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), style, 0, exStyle)
	if ret == 0 {
		return clientWidth, clientHeight
	}
	return r.Right - r.Left, r.Bottom - r.Top
}

func centerPoint(width, height int32) (int32, int32) {
	sw, _, _ := procGetSystemMetrics.Call(0)
	sh, _, _ := procGetSystemMetrics.Call(1)
	return (int32(sw) - width) / 2, (int32(sh) - height) / 2
}

func setWindowIcons(hwnd, smallIcon, bigIcon uintptr) {
	if hwnd == 0 {
		return
	}
	if smallIcon != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, smallIcon)
	}
	if bigIcon != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, bigIcon)
	}
}

func defWindowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func guiConfirm(title, message string) bool {
	return messageBox(title, message, mbOKCancel|mbIconInformation) == idOK
}

func guiStatus(title, message string) {
	messageBox(title, message, mbOK|mbIconInformation)
}

func guiError(message string) {
	messageBox("Ins-maclaw", message, mbOK|mbIconError)
}

func messageBox(title, text string, flags uintptr) int {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	ret, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
	return int(ret)
}

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}
