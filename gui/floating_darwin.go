//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics -framework QuartzCore

#import <Cocoa/Cocoa.h>

// ── Floating window state ───────────────────────────────────────────────────

static NSWindow *floatingWin = nil;
static NSImageView *logoView = nil;
static NSView *haloView = nil;

// ── Create floating window ──────────────────────────────────────────────────

static void createFloatingWindow(int x, int y, int w, int h, const void *pngData, int pngLen) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (floatingWin != nil) return;

		// macOS screen coordinates: origin is bottom-left.
		NSScreen *screen = [NSScreen mainScreen];
		CGFloat screenH = screen.frame.size.height;
		NSRect frame = NSMakeRect(x, screenH - y - h, w, h);

		floatingWin = [[NSWindow alloc]
			initWithContentRect:frame
			styleMask:NSWindowStyleMaskBorderless
			backing:NSBackingStoreBuffered
			defer:NO];
		[floatingWin setLevel:NSFloatingWindowLevel];
		[floatingWin setBackgroundColor:[NSColor clearColor]];
		[floatingWin setOpaque:NO];
		[floatingWin setHasShadow:NO];
		[floatingWin setIgnoresMouseEvents:NO];
		[floatingWin setMovableByWindowBackground:NO];
		[floatingWin setCollectionBehavior:
			NSWindowCollectionBehaviorCanJoinAllSpaces |
			NSWindowCollectionBehaviorStationary];

		NSView *content = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, w, h)];
		[content setWantsLayer:YES];
		content.layer.backgroundColor = [NSColor clearColor].CGColor;

		// Halo glow ring (behind logo).
		int margin = 8;
		haloView = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, w, h)];
		[haloView setWantsLayer:YES];
		haloView.layer.cornerRadius = w / 2.0;
		haloView.layer.borderWidth = 3.0;
		haloView.layer.borderColor = [NSColor colorWithRed:0.4 green:0.55 blue:1.0 alpha:0.5].CGColor;
		haloView.layer.shadowColor = [NSColor colorWithRed:0.4 green:0.55 blue:1.0 alpha:0.8].CGColor;
		haloView.layer.shadowRadius = 8.0;
		haloView.layer.shadowOpacity = 0.6;
		haloView.layer.shadowOffset = CGSizeMake(0, 0);
		[content addSubview:haloView];

		// Circular dark background + logo.
		int logoSize = w - margin * 2;
		NSView *circleBack = [[NSView alloc] initWithFrame:NSMakeRect(margin, margin, logoSize, logoSize)];
		[circleBack setWantsLayer:YES];
		circleBack.layer.cornerRadius = logoSize / 2.0;
		circleBack.layer.backgroundColor = [NSColor colorWithRed:0.90 green:0.92 blue:0.96 alpha:0.96].CGColor;
		circleBack.layer.masksToBounds = YES;
		[content addSubview:circleBack];

		// Decode PNG and set as logo.
		NSData *imgData = [NSData dataWithBytes:pngData length:pngLen];
		NSImage *img = [[NSImage alloc] initWithData:imgData];
		logoView = [[NSImageView alloc] initWithFrame:NSMakeRect(0, 0, logoSize, logoSize)];
		[logoView setImage:img];
		[logoView setImageScaling:NSImageScaleProportionallyUpOrDown];
		[circleBack addSubview:logoView];

		[floatingWin setContentView:content];

		// Pulse animation on halo border opacity.
		CABasicAnimation *pulse = [CABasicAnimation animationWithKeyPath:@"borderColor"];
		pulse.fromValue = (id)[NSColor colorWithRed:0.4 green:0.55 blue:1.0 alpha:0.3].CGColor;
		pulse.toValue = (id)[NSColor colorWithRed:0.4 green:0.55 blue:1.0 alpha:0.8].CGColor;
		pulse.duration = 1.5;
		pulse.autoreverses = YES;
		pulse.repeatCount = HUGE_VALF;
		[haloView.layer addAnimation:pulse forKey:@"haloPulse"];

		CABasicAnimation *shadowPulse = [CABasicAnimation animationWithKeyPath:@"shadowOpacity"];
		shadowPulse.fromValue = @(0.2);
		shadowPulse.toValue = @(0.8);
		shadowPulse.duration = 1.5;
		shadowPulse.autoreverses = YES;
		shadowPulse.repeatCount = HUGE_VALF;
		[haloView.layer addAnimation:shadowPulse forKey:@"shadowPulse"];
	});
}

static void showFloatingWindow(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (floatingWin != nil) {
			[floatingWin makeKeyAndOrderFront:nil];
		}
	});
}

static void hideFloatingWindow(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (floatingWin != nil) {
			[floatingWin orderOut:nil];
		}
	});
}

static void destroyFloatingWindow(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (floatingWin != nil) {
			[floatingWin orderOut:nil];
			[floatingWin close];
			floatingWin = nil;
			logoView = nil;
			haloView = nil;
		}
	});
}

static void moveFloatingWindow(int x, int y) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (floatingWin != nil) {
			NSScreen *screen = [NSScreen mainScreen];
			CGFloat screenH = screen.frame.size.height;
			CGFloat winH = floatingWin.frame.size.height;
			NSPoint origin = NSMakePoint(x, screenH - y - winH);
			[floatingWin setFrameOrigin:origin];
		}
	});
}

static int darwinScreenWidth(void) {
	return (int)[[NSScreen mainScreen] frame].size.width;
}

static int darwinScreenHeight(void) {
	return (int)[[NSScreen mainScreen] frame].size.height;
}
*/
import "C"

import (
	_ "embed"
	"log"
	"sync"
	"unsafe"
)

//go:embed build/appicon.png
var floatingLogoPNGDarwin []byte

// darwinFloatingWindow implements floatingWindow on macOS using native
// NSWindow + CoreAnimation for the circular logo and pulsing halo glow.
type darwinFloatingWindow struct {
	app     *App
	created bool
	mu      sync.Mutex
}

func newFloatingWindow(app *App) floatingWindow {
	return &darwinFloatingWindow{app: app}
}

func init() {
	platformGetScreenWidth = func() int {
		w := int(C.darwinScreenWidth())
		if w <= 0 {
			return 1920
		}
		return w
	}
	platformGetScreenHeight = func() int {
		h := int(C.darwinScreenHeight())
		if h <= 0 {
			return 1080
		}
		return h
	}
}

func (w *darwinFloatingWindow) Create(x, y, width, height int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.created {
		return nil
	}

	pngPtr := unsafe.Pointer(&floatingLogoPNGDarwin[0])
	pngLen := C.int(len(floatingLogoPNGDarwin))
	C.createFloatingWindow(C.int(x), C.int(y), C.int(width), C.int(height), pngPtr, pngLen)
	w.created = true
	log.Printf("[floating-window] macOS window created: pos=(%d,%d) size=(%d,%d)", x, y, width, height)
	return nil
}

func (w *darwinFloatingWindow) Show() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.showFloatingWindow()
}

func (w *darwinFloatingWindow) Hide() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.hideFloatingWindow()
}

func (w *darwinFloatingWindow) Destroy() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.destroyFloatingWindow()
	w.created = false
}

func (w *darwinFloatingWindow) MoveTo(x, y int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.moveFloatingWindow(C.int(x), C.int(y))
}

func (w *darwinFloatingWindow) IsCreated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.created
}
