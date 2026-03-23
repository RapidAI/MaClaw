//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// Forward declarations for Go callbacks.
extern void tahoeOnShowClicked(void);
extern void tahoeOnQuitClicked(void);

static NSStatusItem *_tahoeStatusItem = nil;
static NSMenu       *_tahoeMenu       = nil;

@interface _TahoeMenuTarget : NSObject
- (void)menuAction:(id)sender;
@end

@implementation _TahoeMenuTarget
- (void)menuAction:(id)sender {
    NSInteger tag = [sender tag];
    if (tag == 1) {
        tahoeOnShowClicked();
    } else if (tag == 2) {
        tahoeOnQuitClicked();
    }
}
@end

static _TahoeMenuTarget *_menuTarget = nil;

static void TahoeCreateTray(const void *iconData, int iconLen,
                            const char *tooltip,
                            const char *showLabel, const char *quitLabel) {
    // strdup so the async block owns the strings; originals freed by Go caller.
    const char *t = tooltip   ? strdup(tooltip)   : NULL;
    const char *s = showLabel ? strdup(showLabel) : NULL;
    const char *q = quitLabel ? strdup(quitLabel) : NULL;
    // Copy icon data synchronously — Go slice backing array is stable (embed)
    // but defensive copy is safer.
    NSData *iconCopy = nil;
    if (iconData && iconLen > 0) {
        iconCopy = [NSData dataWithBytes:iconData length:iconLen];
    }
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            @try {
                _tahoeStatusItem = [[NSStatusBar systemStatusBar]
                    statusItemWithLength:NSVariableStatusItemLength];

                if (iconCopy) {
                    NSImage *img = [[NSImage alloc] initWithData:iconCopy];
                    [img setSize:NSMakeSize(18, 18)];
                    img.template = YES;
                    _tahoeStatusItem.button.image = img;
                }

                if (t) {
                    _tahoeStatusItem.button.toolTip =
                        [NSString stringWithUTF8String:t];
                }

                _menuTarget = [[_TahoeMenuTarget alloc] init];
                _tahoeMenu = [[NSMenu alloc] init];

                NSMenuItem *showItem = [[NSMenuItem alloc]
                    initWithTitle:[NSString stringWithUTF8String:s ? s : "Show"]
                          action:@selector(menuAction:)
                   keyEquivalent:@""];
                showItem.target = _menuTarget;
                showItem.tag = 1;
                [_tahoeMenu addItem:showItem];

                [_tahoeMenu addItem:[NSMenuItem separatorItem]];

                NSMenuItem *quitItem = [[NSMenuItem alloc]
                    initWithTitle:[NSString stringWithUTF8String:q ? q : "Quit"]
                          action:@selector(menuAction:)
                   keyEquivalent:@""];
                quitItem.target = _menuTarget;
                quitItem.tag = 2;
                [_tahoeMenu addItem:quitItem];

                _tahoeStatusItem.menu = _tahoeMenu;

                NSLog(@"[tray-tahoe] NSStatusItem created successfully");
            } @catch (NSException *exception) {
                NSLog(@"[tray-tahoe] EXCEPTION creating tray: %@ — %@",
                      exception.name, exception.reason);
            } @finally {
                free((void*)t);
                free((void*)s);
                free((void*)q);
            }
        }
    });
}

static void TahoeUpdateMenu(const char *tooltip,
                             const char *showLabel, const char *quitLabel) {
    // Copy params for the async block, then free originals after block captures.
    const char *t = tooltip  ? strdup(tooltip)  : NULL;
    const char *s = showLabel ? strdup(showLabel) : NULL;
    const char *q = quitLabel ? strdup(quitLabel) : NULL;
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            if (!_tahoeStatusItem) { free((void*)t); free((void*)s); free((void*)q); return; }
            if (t) {
                _tahoeStatusItem.button.toolTip =
                    [NSString stringWithUTF8String:t];
                free((void*)t);
            }
            if (_tahoeMenu && [_tahoeMenu numberOfItems] >= 3) {
                if (s) {
                    [[_tahoeMenu itemAtIndex:0]
                        setTitle:[NSString stringWithUTF8String:s]];
                    free((void*)s);
                }
                if (q) {
                    [[_tahoeMenu itemAtIndex:2]
                        setTitle:[NSString stringWithUTF8String:q]];
                    free((void*)q);
                }
            } else {
                free((void*)s);
                free((void*)q);
            }
        }
    });
}
*/
import "C"

import (
	"log"
	"unsafe"
)

// tahoeShowCallback and tahoeQuitCallback are set by setupTahoeTray.
var tahoeShowCallback func()
var tahoeQuitCallback func()

//export tahoeOnShowClicked
func tahoeOnShowClicked() {
	if tahoeShowCallback != nil {
		go tahoeShowCallback()
	}
}

//export tahoeOnQuitClicked
func tahoeOnQuitClicked() {
	if tahoeQuitCallback != nil {
		go tahoeQuitCallback()
	}
}

// setupTahoeTray creates a minimal NSStatusItem tray without energye/systray.
func setupTahoeTray(iconBytes []byte, tooltip, showLabel, quitLabel string,
	onShow func(), onQuit func()) {

	log.Println("[tray] using Tahoe-safe native NSStatusItem")

	tahoeShowCallback = onShow
	tahoeQuitCallback = onQuit

	var iconPtr unsafe.Pointer
	var iconLen C.int
	if len(iconBytes) > 0 {
		iconPtr = unsafe.Pointer(&iconBytes[0])
		iconLen = C.int(len(iconBytes))
	}

	cTooltip := C.CString(tooltip)
	cShow := C.CString(showLabel)
	cQuit := C.CString(quitLabel)
	// TahoeCreateTray strdup's these before dispatch_async, so we can free
	// them immediately after the call returns.
	C.TahoeCreateTray(iconPtr, iconLen, cTooltip, cShow, cQuit)
	C.free(unsafe.Pointer(cTooltip))
	C.free(unsafe.Pointer(cShow))
	C.free(unsafe.Pointer(cQuit))
}

// updateTahoeTrayMenu updates the Tahoe tray labels.
func updateTahoeTrayMenu(tooltip, showLabel, quitLabel string) {
	cTooltip := C.CString(tooltip)
	cShow := C.CString(showLabel)
	cQuit := C.CString(quitLabel)
	// TahoeUpdateMenu strdup's these before dispatch_async, so we can free
	// them immediately after the call returns.
	C.TahoeUpdateMenu(cTooltip, cShow, cQuit)
	C.free(unsafe.Pointer(cTooltip))
	C.free(unsafe.Pointer(cShow))
	C.free(unsafe.Pointer(cQuit))
}
