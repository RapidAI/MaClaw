package systray

/*
#cgo darwin CFLAGS: -DDARWIN -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>
#include <stdbool.h>
#include <stdlib.h>
#include "systray.h"

void setInternalLoop(bool);

// showNotification displays a macOS user notification with title and message.
// Uses NSUserNotification for compatibility; on macOS 11+ this is a no-op
// because NSUserNotification was removed.  A future improvement could use
// UNUserNotificationCenter for newer systems.
static void showNotification(const char* title, const char* message) {
    @autoreleasepool {
        @try {
            NSUserNotification *notification = [[NSUserNotification alloc] init];
            notification.title = [NSString stringWithUTF8String:title];
            notification.informativeText = [NSString stringWithUTF8String:message];
            notification.soundName = NSUserNotificationDefaultSoundName;
            [[NSUserNotificationCenter defaultUserNotificationCenter] deliverNotification:notification];
        } @catch (NSException *exception) {
            // NSUserNotification removed in macOS 11+; silently ignore.
            NSLog(@"systray: notification failed: %@", exception);
        }
    }
}

// requestAttention bounces the dock icon to draw user attention.
static void requestAttention() {
    @autoreleasepool {
        [NSApp requestUserAttention:NSCriticalRequest];
    }
}
*/
import "C"

import (
	"time"
	"unsafe"
)

var st = &systray{}

type systray struct {
}

func (m *systray) ShowMenu() error {
	C.show_menu()
	return nil
}

// SetTemplateIcon sets the systray icon as a template icon (on Mac), falling back
// to a regular icon on other platforms.
// templateIconBytes and regularIconBytes should be the content of .ico for windows and
// .ico/.jpg/.png for other platforms.
func SetTemplateIcon(templateIconBytes []byte, regularIconBytes []byte) {
	cstr := (*C.char)(unsafe.Pointer(&templateIconBytes[0]))
	C.setIcon(cstr, (C.int)(len(templateIconBytes)), true)
}

// SetIcon sets the icon of a menu item. Only works on macOS and Windows.
// iconBytes should be the content of .ico/.jpg/.png
func (item *MenuItem) SetIcon(iconBytes []byte) {
	cstr := (*C.char)(unsafe.Pointer(&iconBytes[0]))
	C.setMenuItemIcon(cstr, (C.int)(len(iconBytes)), C.int(item.id), false)
}

// SetTemplateIcon sets the icon of a menu item as a template icon (on macOS). On Windows, it
// falls back to the regular icon bytes and on Linux it does nothing.
// templateIconBytes and regularIconBytes should be the content of .ico for windows and
// .ico/.jpg/.png for other platforms.
func (item *MenuItem) SetTemplateIcon(templateIconBytes []byte, regularIconBytes []byte) {
	cstr := (*C.char)(unsafe.Pointer(&templateIconBytes[0]))
	C.setMenuItemIcon(cstr, (C.int)(len(templateIconBytes)), C.int(item.id), true)
}

func registerSystray() {
	C.registerSystray()
}

func nativeLoop() {
	C.nativeLoop()
}

func nativeEnd() {
	C.nativeEnd()
}

func nativeStart() {
	C.nativeStart()
}

func quit() {
	C.quit()
}

func setInternalLoop(internal bool) {
	C.setInternalLoop(C.bool(internal))
}

var (
	onClick         func(menu IMenu)
	onDClick        func(menu IMenu)
	onRClick        func(menu IMenu)
	dClickTime      int64
	isEnableOnClick = false
)

func setOnClick(fn func(menu IMenu)) {
	enableOnClick()
	onClick = fn
}

func setOnDClick(fn func(menu IMenu)) {
	enableOnClick()
	onDClick = fn
}

func setOnRClick(fn func(menu IMenu)) {
	enableOnClick()
	onRClick = fn
}

// CreateMenu 创建托盘菜单, 如果托盘菜单是空, 把菜单项添加到托盘
// 该方法主动调用后 如果托盘菜单已创建则添加进去, 之后鼠标事件失效
//
// 仅MacOSX平台
func CreateMenu() {
	createMenu()
}

// SetMenuNil 托盘菜单设置为nil, 如果托盘菜单不是空, 把菜单项设置为nil
// 该方法主动调用后 将移除托盘菜单, 之后鼠标事件生效
//
// 仅MacOSX平台
func SetMenuNil() {
	setMenuNil()
}

// SetIcon sets the systray icon.
// iconBytes should be the content of .ico for windows and .ico/.jpg/.png
// for other platforms.
func SetIcon(iconBytes []byte) {
	cstr := (*C.char)(unsafe.Pointer(&iconBytes[0]))
	C.setIcon(cstr, (C.int)(len(iconBytes)), false)
}

// SetTitle sets the systray title, only available on Mac and Linux.
func SetTitle(title string) {
	cstr := C.CString(title)
	defer C.free(unsafe.Pointer(cstr))
	C.setTitle(cstr)
}

// SetTooltip sets the systray tooltip to display on mouse hover of the tray icon,
// only available on Mac and Windows.
func SetTooltip(tooltip string) {
	cstr := C.CString(tooltip)
	defer C.free(unsafe.Pointer(cstr))
	C.setTooltip(cstr)
}

func addOrUpdateMenuItem(item *MenuItem) {
	var disabled C.short
	if item.disabled {
		disabled = 1
	}
	var checked C.short
	if item.checked {
		checked = 1
	}
	var isCheckable C.short
	if item.isCheckable {
		isCheckable = 1
	}
	var parentID uint32 = 0
	if item.parent != nil {
		parentID = item.parent.id
	}
	cTitle := C.CString(item.title)
	defer C.free(unsafe.Pointer(cTitle))
	cTooltip := C.CString(item.tooltip)
	defer C.free(unsafe.Pointer(cTooltip))
	cShortcut := C.CString(item.shortcutKey)
	defer C.free(unsafe.Pointer(cShortcut))
	C.add_or_update_menu_item(
		C.int(item.id),
		C.int(parentID),
		cTitle,
		cTooltip,
		cShortcut,
		disabled,
		checked,
		isCheckable,
	)
}

func addSeparator(id uint32) {
	C.add_separator(C.int(id))
}

func hideMenuItem(item *MenuItem) {
	C.hide_menu_item(
		C.int(item.id),
	)
}

func showMenuItem(item *MenuItem) {
	C.show_menu_item(
		C.int(item.id),
	)
}

func resetMenu() {
	C.reset_menu()
}

func createMenu() {
	C.create_menu()
}

func setMenuNil() {
	C.set_menu_nil()
}
func enableOnClick() {
	if !isEnableOnClick {
		isEnableOnClick = true
		C.enable_on_click()
	}
}

//export systray_ready
func systray_ready() {
	if systrayReady != nil {
		systrayReady()
	}
}

//export systray_on_exit
func systray_on_exit() {
	systrayExit()
}

//export systray_menu_item_selected
func systray_menu_item_selected(cID C.int) {
	systrayMenuItemSelected(uint32(cID))
}

//export systray_on_click
func systray_on_click() {
	if dClickTime == 0 {
		dClickTime = time.Now().UnixMilli()
	} else {
		nowMilli := time.Now().UnixMilli()
		if nowMilli-dClickTime < dClickTimeMinInterval {
			dClickTime = dClickTimeMinInterval
			if onDClick != nil {
				onDClick(st)
				return
			}
		} else {
			dClickTime = nowMilli
		}
	}
	if onClick != nil {
		onClick(st)
	}
}

//export systray_on_rclick
func systray_on_rclick() {
	if onRClick != nil {
		onRClick(st)
	} else {
		C.show_menu()
	}
}

// ShowBalloonNotification displays a macOS notification with sound.
// iconFlag is ignored on macOS (always uses default notification style).
func ShowBalloonNotification(title, message string, iconFlag uint32) error {
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	cMessage := C.CString(message)
	defer C.free(unsafe.Pointer(cMessage))
	C.showNotification(cTitle, cMessage)
	return nil
}

// FlashAndBeep bounces the dock icon and plays the default notification sound
// on macOS to draw the user's attention for scheduled task alerts.
// The notification sound is already played by ShowBalloonNotification via
// NSUserNotificationDefaultSoundName, so this is a supplementary dock bounce.
func FlashAndBeep() {
	// NSApplication requestUserAttention bounces the dock icon.
	// NSCriticalRequest bounces until the app is activated.
	C.requestAttention()
}
