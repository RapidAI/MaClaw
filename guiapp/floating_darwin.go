//go:build darwin

package guiapp

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics

#import <Cocoa/Cocoa.h>
#import <math.h>
#include <stdatomic.h>

// Desktop pet window. The previous build painted the app icon inside a
// circular badge and ignored the mouse, so the pet could not be seen or
// dragged. This window paints the pack still with a clear background and
// tracks left-drag itself.

extern void maclawDarwinPetClicked(void);
extern void maclawDarwinPetDragged(int x, int y);
extern void maclawDarwinPetMenu(int action);

static NSWindow *floatingWin = nil;
static NSView *petView = nil;
static NSImage *petImage = nil;

static void darwinSync(void (^block)(void)) {
	if (block == nil) return;
	if ([NSThread isMainThread]) {
		block();
	} else {
		dispatch_sync(dispatch_get_main_queue(), block);
	}
}

// Run after mouse/menu tracking returns to the default run loop. Calling Go
// from inside the tracking loop deadlocks: the callback hops to a goroutine
// that dispatch_syncs back onto the main thread, which is still in mouseDown.
static void darwinPerformAfterTracking(void (^block)(void)) {
	if (block == nil) return;
	CFRunLoopPerformBlock(CFRunLoopGetMain(), kCFRunLoopDefaultMode, block);
	CFRunLoopWakeUp(CFRunLoopGetMain());
}

// Coordinate space for the pet. NSScreen.mainScreen follows the key window, so
// using it mid-drag teleports the pet onto whichever display currently has
// keyboard focus. The cache is written on the main thread and read from Go
// when clamping a saved position, including off the main thread.
static CGFloat cachedOriginX = 0;
static CGFloat cachedOriginY = 0;
static CGFloat cachedFrameW = 0;
static CGFloat cachedFrameH = 0;
// Width, height, and scale are read from Go threads. The frame origin stays
// on the main thread with the window.
static atomic_int cachedScreenW = 0;
static atomic_int cachedScreenH = 0;
static atomic_int cachedScale = 0;
static BOOL cachedFrameValid = NO;

static void rememberScreen(NSScreen *screen) {
	if (screen == nil) return;
	NSRect frame = screen.frame;
	int scale = (int)llround(screen.backingScaleFactor);
	int w = (int)llround(frame.size.width);
	int h = (int)llround(frame.size.height);
	if (scale < 1) scale = 1;
	if (scale > 3) scale = 3;
	if (w < 1) w = 1;
	if (h < 1) h = 1;
	cachedOriginX = frame.origin.x;
	cachedOriginY = frame.origin.y;
	cachedFrameW = frame.size.width;
	cachedFrameH = frame.size.height;
	atomic_store(&cachedScreenW, w);
	atomic_store(&cachedScreenH, h);
	atomic_store(&cachedScale, scale);
	cachedFrameValid = YES;
}

static NSScreen *screenAtPoint(NSPoint point) {
	for (NSScreen *screen in [NSScreen screens]) {
		if (NSPointInRect(point, screen.frame)) return screen;
	}
	return nil;
}

static NSScreen *screenForWindow(void) {
	if (floatingWin == nil) return [NSScreen mainScreen];
	NSRect frame = floatingWin.frame;
	NSScreen *hit = screenAtPoint(NSMakePoint(NSMidX(frame), NSMidY(frame)));
	if (hit != nil) return hit;
	if (floatingWin.screen != nil) return floatingWin.screen;
	return [NSScreen mainScreen];
}

static void ensurePetFrame(void) {
	if (cachedFrameValid) {
		NSRect cached = NSMakeRect(cachedOriginX, cachedOriginY, cachedFrameW, cachedFrameH);
		for (NSScreen *screen in [NSScreen screens]) {
			if (NSIntersectsRect(screen.frame, cached)) return;
		}
		cachedFrameValid = NO;
	}
	NSScreen *screen = [NSScreen mainScreen];
	if (screen != nil) rememberScreen(screen);
}

static NSRect petFrame(void) {
	ensurePetFrame();
	if (!cachedFrameValid) return NSMakeRect(0, 0, 1920, 1080);
	return NSMakeRect(cachedOriginX, cachedOriginY, cachedFrameW, cachedFrameH);
}

// Top-left space matches the rest of the floating-pet code: x right, y down,
// relative to the cached pet screen. darwinReadTopLeft and movePetTopLeft
// are inverses of that rectangle.
static void darwinReadTopLeft(int *x, int *y) {
	if (x) *x = 0;
	if (y) *y = 0;
	if (floatingWin == nil || !cachedFrameValid) return;
	NSRect sf = NSMakeRect(cachedOriginX, cachedOriginY, cachedFrameW, cachedFrameH);
	NSRect frame = floatingWin.frame;
	if (x) *x = (int)llround(frame.origin.x - sf.origin.x);
	if (y) *y = (int)llround(NSMaxY(sf) - NSMaxY(frame));
}

static void movePetTopLeft(int x, int y) {
	if (floatingWin == nil) return;
	NSRect sf = petFrame();
	CGFloat w = floatingWin.frame.size.width;
	CGFloat h = floatingWin.frame.size.height;
	int maxX = (int)llround(sf.size.width - w);
	int maxY = (int)llround(sf.size.height - h);
	if (maxX < 0) maxX = 0;
	if (maxY < 0) maxY = 0;
	if (x < 0) x = 0;
	if (y < 0) y = 0;
	if (x > maxX) x = maxX;
	if (y > maxY) y = maxY;
	[floatingWin setFrameOrigin:NSMakePoint(sf.origin.x + x, NSMaxY(sf) - y - h)];
}

static void clampOriginToVisible(NSPoint *origin, NSScreen *screen) {
	if (floatingWin == nil || origin == nil || screen == nil) return;
	NSRect vis = screen.visibleFrame;
	CGFloat w = floatingWin.frame.size.width;
	CGFloat h = floatingWin.frame.size.height;
	CGFloat maxX = NSMaxX(vis) - w;
	CGFloat maxY = NSMaxY(vis) - h;
	if (maxX < NSMinX(vis)) maxX = NSMinX(vis);
	if (maxY < NSMinY(vis)) maxY = NSMinY(vis);
	if (origin->x < NSMinX(vis)) origin->x = NSMinX(vis);
	if (origin->y < NSMinY(vis)) origin->y = NSMinY(vis);
	if (origin->x > maxX) origin->x = maxX;
	if (origin->y > maxY) origin->y = maxY;
}

static NSImage *imageFromPNG(const void *pngData, int pngLen) {
	if (pngData == NULL || pngLen <= 0) return nil;
	NSData *data = [NSData dataWithBytes:pngData length:(NSUInteger)pngLen];
	if (data == nil) return nil;
	return [[NSImage alloc] initWithData:data];
}

static void replacePetImage(NSImage *next) {
	if (next == nil) return;
	if (petImage != nil) [petImage release];
	petImage = next;
	if (petView == nil) return;
	[petView setNeedsDisplay:YES];
	// Drawing a window that has not been ordered in yet can leave it blank.
	if (floatingWin != nil && [floatingWin isVisible]) {
		[petView displayIfNeeded];
	}
}

@interface MaclawPetWindow : NSPanel
@end

@implementation MaclawPetWindow
// A borderless panel does not receive clicks unless it can become key.
// It stays a nonactivating panel, so this does not pull the app forward.
// It must not become main: that would retarget NSScreen.mainScreen.
- (BOOL)canBecomeKeyWindow { return YES; }
- (BOOL)canBecomeMainWindow { return NO; }
@end

@interface MaclawPetView : NSView
- (void)showContextMenu:(NSEvent *)event;
- (void)maclawMenuAction:(id)sender;
@end

@implementation MaclawPetView

- (BOOL)isOpaque { return NO; }
- (BOOL)acceptsFirstMouse:(NSEvent *)event {
	(void)event;
	return YES;
}
- (BOOL)mouseDownCanMoveWindow { return NO; }

// A layer-backed view paints this window black or upside down. This view is
// not layer-backed: Clear punches the buffered window to transparent, then
// NSImage draws the pack still upright.
- (void)drawRect:(NSRect)dirtyRect {
	(void)dirtyRect;
	[[NSColor clearColor] set];
	NSRectFillUsingOperation(self.bounds, NSCompositingOperationClear);
	if (petImage != nil) {
		[petImage drawInRect:self.bounds
					fromRect:NSZeroRect
				   operation:NSCompositingOperationSourceOver
					fraction:1.0];
	}
}

- (void)showContextMenu:(NSEvent *)event {
	NSMenu *menu = [[NSMenu alloc] initWithTitle:@""];
	struct { const char *title; NSInteger tag; } items[] = {
		{"打开主窗口", 4},
		{"打开宠物对话", 5},
		{"设置", 1},
		{"隐藏", 2},
		{"退出", 3},
	};
	for (size_t i = 0; i < sizeof(items) / sizeof(items[0]); i++) {
		NSString *title = [NSString stringWithUTF8String:items[i].title];
		NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title
													  action:@selector(maclawMenuAction:)
											   keyEquivalent:@""];
		item.target = self;
		item.tag = items[i].tag;
		[menu addItem:item];
		[item release];
	}
	[NSMenu popUpContextMenu:menu withEvent:event forView:self];
	[menu release];
}

- (void)maclawMenuAction:(id)sender {
	int action = (int)[(NSMenuItem *)sender tag];
	darwinPerformAfterTracking(^{
		maclawDarwinPetMenu(action);
	});
}

// A non-activating panel often receives mouseDown and then never gets
// mouseDragged/mouseUp. Track the button here until it is released.
// Retain the view and window so a hide during the gesture cannot free them
// out from under this call.
- (void)mouseDown:(NSEvent *)event {
	[self retain];
	NSWindow *tracked = floatingWin;
	if (tracked != nil) [tracked retain];
	if ((event.modifierFlags & NSEventModifierFlagControl) != 0) {
		[self showContextMenu:event];
	} else if (tracked != nil) {
		// Key status is what delivers mouseDragged to a borderless panel.
		// Resign afterwards so the pet does not keep the keyboard.
		[tracked makeKeyWindow];
		NSPoint anchorMouse = [NSEvent mouseLocation];
		NSPoint anchorOrigin = tracked.frame.origin;
		NSScreen *dragScreen = screenForWindow();
		BOOL moved = NO;
		// Wait for the button. Checking pressedMouseButtons while a drag event
		// is already in hand ends the gesture early: the mask lags mouseDown,
		// so the pet never moves. The mask is only a fallback when no event
		// arrives, so a lost mouseUp cannot block the main thread forever.
		for (;;) {
			if (floatingWin == nil) break;
			NSEvent *next = [tracked nextEventMatchingMask:(NSEventMaskLeftMouseDragged | NSEventMaskLeftMouseUp)
												  untilDate:[NSDate dateWithTimeIntervalSinceNow:0.05]
													 inMode:NSEventTrackingRunLoopMode
													dequeue:YES];
			if (floatingWin == nil) break;
			if (next != nil && next.type == NSEventTypeLeftMouseUp) break;
			if (next == nil) {
				if (([NSEvent pressedMouseButtons] & 1) == 0) break;
				continue;
			}
			if (next.type != NSEventTypeLeftMouseDragged) continue;
			NSPoint now = [NSEvent mouseLocation];
			CGFloat dx = now.x - anchorMouse.x;
			CGFloat dy = now.y - anchorMouse.y;
			if (!moved && (fabs(dx) > 5.0 || fabs(dy) > 5.0)) moved = YES;
			if (floatingWin == nil) break;
			if (!moved) continue;
			NSPoint origin = NSMakePoint(anchorOrigin.x + dx, anchorOrigin.y + dy);
			NSPoint center = NSMakePoint(origin.x + tracked.frame.size.width / 2.0,
										 origin.y + tracked.frame.size.height / 2.0);
			// [NSScreen screens] asks the window server. Keep the current
			// display until the pet actually crosses onto another one.
			if (dragScreen == nil || !NSPointInRect(center, dragScreen.frame)) {
				NSScreen *hit = screenAtPoint(center);
				if (hit != nil) dragScreen = hit;
			}
			clampOriginToVisible(&origin, dragScreen);
			[floatingWin setFrameOrigin:origin];
		}
		if (floatingWin != nil && !moved) {
			darwinPerformAfterTracking(^{
				maclawDarwinPetClicked();
			});
		} else if (floatingWin != nil) {
			NSScreen *landed = screenForWindow();
			if (landed != nil) rememberScreen(landed);
			int x = 0, y = 0;
			darwinReadTopLeft(&x, &y);
			darwinPerformAfterTracking(^{
				maclawDarwinPetDragged(x, y);
			});
		}
		if ([tracked isKeyWindow]) [tracked resignKeyWindow];
	}
	if (tracked != nil) [tracked release];
	[self release];
}

- (void)rightMouseDown:(NSEvent *)event {
	[self retain];
	[self showContextMenu:event];
	[self release];
}

@end

static int createFloatingWindowDarwin(int x, int y, int w, int h, const void *pngData, int pngLen) {
	if (w < 1) w = 104;
	if (h < 1) h = 104;
	__block int ok = 0;
	darwinSync(^{
		if (floatingWin != nil) {
			ok = 1;
			return;
		}
		NSImage *img = imageFromPNG(pngData, pngLen);
		if (img == nil) {
			NSLog(@"[floating-window] macOS pet PNG decode failed");
			return;
		}
		// Origin is applied by movePetTopLeft after the window exists so the
		// initial position and later drags share one conversion.

		// Nonactivating panel: the pet receives clicks and drags without
		// pulling the main window forward. Panels hide on deactivate unless
		// told otherwise, which would dismiss the pet with the app.
		MaclawPetWindow *panel = [[MaclawPetWindow alloc]
			initWithContentRect:NSMakeRect(0, 0, w, h)
			styleMask:(NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel)
			backing:NSBackingStoreBuffered
			defer:NO];
		floatingWin = panel;
		[panel setFloatingPanel:YES];
		[panel setBecomesKeyOnlyIfNeeded:NO];
		[panel setLevel:NSFloatingWindowLevel];
		[panel setOpaque:NO];
		[panel setBackgroundColor:[NSColor clearColor]];
		[panel setHasShadow:NO];
		[panel setIgnoresMouseEvents:NO];
		[panel setMovable:NO];
		[panel setMovableByWindowBackground:NO];
		[panel setHidesOnDeactivate:NO];
		[panel setReleasedWhenClosed:YES];
		[floatingWin setCollectionBehavior:
			NSWindowCollectionBehaviorCanJoinAllSpaces |
			NSWindowCollectionBehaviorStationary |
			NSWindowCollectionBehaviorFullScreenAuxiliary];

		MaclawPetView *view = [[MaclawPetView alloc] initWithFrame:NSMakeRect(0, 0, w, h)];
		[floatingWin setContentView:view];
		petView = view;
		[view release];
		replacePetImage(img);
		movePetTopLeft(x, y);
		ok = 1;
	});
	return ok;
}

static void showFloatingWindowDarwin(void) {
	darwinSync(^{
		if (floatingWin != nil) {
			[floatingWin orderFrontRegardless];
			if (petView != nil) [petView displayIfNeeded];
		}
	});
}

static void hideFloatingWindowDarwin(void) {
	darwinSync(^{
		if (floatingWin != nil) {
			[floatingWin orderOut:nil];
		}
	});
}

static void destroyFloatingWindowDarwin(void) {
	darwinSync(^{
		if (petImage != nil) {
			[petImage release];
			petImage = nil;
		}
		if (petView != nil) {
			petView = nil;
		}
		if (floatingWin != nil) {
			[floatingWin orderOut:nil];
			[floatingWin close];
			floatingWin = nil;
		}
	});
}

static void moveFloatingWindowDarwin(int x, int y) {
	darwinSync(^{
		movePetTopLeft(x, y);
	});
}

static void setPetImageDarwin(const void *pngData, int pngLen) {
	darwinSync(^{
		NSImage *img = imageFromPNG(pngData, pngLen);
		if (img == nil) return;
		replacePetImage(img);
	});
}

// Prefer the cache filled on the main thread. A direct NSScreen read is only
// the startup fallback; AppKit screen queries off the main thread crash, and
// bouncing them onto the main queue deadlocks tests that have no run loop.
static int darwinBackingScale(void) {
	int scale = atomic_load(&cachedScale);
	if (scale > 0) return scale;
	if (![NSThread isMainThread]) return 2;
	NSScreen *screen = [NSScreen mainScreen];
	if (screen == nil) return 2;
	rememberScreen(screen);
	scale = atomic_load(&cachedScale);
	return scale > 0 ? scale : 2;
}

static int darwinScreenWidth(void) {
	int width = atomic_load(&cachedScreenW);
	if (width > 0) return width;
	if (![NSThread isMainThread]) return 0;
	NSScreen *screen = [NSScreen mainScreen];
	if (screen == nil) return 0;
	return (int)llround(screen.frame.size.width);
}

static int darwinScreenHeight(void) {
	int height = atomic_load(&cachedScreenH);
	if (height > 0) return height;
	if (![NSThread isMainThread]) return 0;
	NSScreen *screen = [NSScreen mainScreen];
	if (screen == nil) return 0;
	return (int)llround(screen.frame.size.height);
}
*/
import "C"

import (
	_ "embed"
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

//go:embed build/appicon.png
var floatingLogoPNGDarwin []byte

// darwinFloatingWindow is the macOS desktop pet. It shows a pack still on a
// clear window and can be dragged. Full skeleton/character animation remains
// a Windows path; state changes swap the matching still.
type darwinFloatingWindow struct {
	app              *App
	created          bool
	mu               sync.Mutex
	size             int
	petSkin          string
	petVariant       string
	petRuntimeState  string
	petStateDeadline time.Time
	packFrameCache   *petpack.FrameCache
}

func newFloatingWindow(app *App) floatingWindow {
	return &darwinFloatingWindow{app: app}
}

var globalDarwinFloating struct {
	sync.RWMutex
	window *darwinFloatingWindow
}

func setGlobalDarwinFloating(w *darwinFloatingWindow) {
	globalDarwinFloating.Lock()
	globalDarwinFloating.window = w
	globalDarwinFloating.Unlock()
}

func clearGlobalDarwinFloating(w *darwinFloatingWindow) {
	globalDarwinFloating.Lock()
	if globalDarwinFloating.window == w {
		globalDarwinFloating.window = nil
	}
	globalDarwinFloating.Unlock()
}

func currentDarwinFloating() *darwinFloatingWindow {
	globalDarwinFloating.RLock()
	w := globalDarwinFloating.window
	globalDarwinFloating.RUnlock()
	return w
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

func (w *darwinFloatingWindow) petPNGBytesLocked() []byte {
	sz := w.size
	scale := int(C.darwinBackingScale())
	if scale < 1 {
		scale = 1
	}
	if scale > 3 {
		scale = 3
	}
	if sz > 0 {
		sz *= scale
	}
	return desktopPetStillPNG(w.petSkin, w.petVariant, w.petRuntimeState, sz, w.packFrameCache, floatingLogoPNGDarwin)
}

func (w *darwinFloatingWindow) applyPetImageLocked() {
	if !w.created {
		return
	}
	png := w.petPNGBytesLocked()
	if len(png) == 0 {
		return
	}
	C.setPetImageDarwin(unsafe.Pointer(&png[0]), C.int(len(png)))
}

func (w *darwinFloatingWindow) Create(x, y, width, height int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.created {
		return nil
	}

	w.size = width
	w.petSkin = petpack.DefaultPackID
	w.petVariant = petpack.VariantDefault
	w.petRuntimeState = string(petpack.StateIdle)
	w.petStateDeadline = time.Time{}
	w.packFrameCache = petpack.NewFrameCache()
	if w.app != nil {
		if cfg, err := w.app.LoadConfig(); err == nil {
			if cfg.PetSkin != "" {
				w.petSkin = cfg.PetSkin
			}
			w.petVariant = petpack.ResolveVariantForRuntime(cfg.PetVariant)
		}
	}

	png := w.petPNGBytesLocked()
	if len(png) == 0 {
		return fmt.Errorf("darwin pet: no pack frame or logo PNG to paint")
	}
	if C.createFloatingWindowDarwin(C.int(x), C.int(y), C.int(width), C.int(height), unsafe.Pointer(&png[0]), C.int(len(png))) == 0 {
		return fmt.Errorf("darwin pet: window was not created")
	}
	w.created = true
	setGlobalDarwinFloating(w)
	log.Printf("[floating-window] macOS window created: pos=(%d,%d) size=(%d,%d) skin=%s png=%d", x, y, width, height, w.petSkin, len(png))
	return nil
}

func (w *darwinFloatingWindow) Show() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.showFloatingWindowDarwin()
}

func (w *darwinFloatingWindow) Hide() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.hideFloatingWindowDarwin()
}

func (w *darwinFloatingWindow) Destroy() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	clearGlobalDarwinFloating(w)
	C.destroyFloatingWindowDarwin()
	w.created = false
}

func (w *darwinFloatingWindow) MoveTo(x, y int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.moveFloatingWindowDarwin(C.int(x), C.int(y))
}

func (w *darwinFloatingWindow) IsCreated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.created
}

func (w *darwinFloatingWindow) UpdateSoundConfig(soundEnabled bool, preset string) {
}

func (w *darwinFloatingWindow) UpdateMotionConfig(motionEnabled, quiet, reducedMotion bool, interactionMode, skin, variant string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	changed := false
	if skin != "" && skin != w.petSkin {
		w.petSkin = skin
		changed = true
	}
	if variant != "" {
		resolved := petpack.ResolveVariantForRuntime(variant)
		if resolved != w.petVariant {
			w.petVariant = resolved
			changed = true
		}
	}
	if changed {
		w.packFrameCache = petpack.NewFrameCache()
		w.applyPetImageLocked()
	}
}

func (w *darwinFloatingWindow) InvalidatePetPackAssets() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.packFrameCache = petpack.NewFrameCache()
	w.applyPetImageLocked()
}

func (w *darwinFloatingWindow) SetPetRuntimeState(state string, ttlMs int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	next := string(petpack.NormalizeState(state))
	if next == w.petRuntimeState && ttlMs == 0 && w.petStateDeadline.IsZero() {
		return
	}
	w.petRuntimeState = next
	if ttlMs > 0 {
		w.petStateDeadline = time.Now().Add(time.Duration(ttlMs) * time.Millisecond)
	} else {
		w.petStateDeadline = time.Time{}
	}
	w.applyPetImageLocked()
}

func (w *darwinFloatingWindow) CurrentPetRuntimeState() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.petStateDeadline.IsZero() && time.Now().After(w.petStateDeadline) {
		w.petRuntimeState = string(petpack.StateIdle)
		w.petStateDeadline = time.Time{}
		w.applyPetImageLocked()
	}
	if w.petRuntimeState == "" {
		return string(petpack.StateIdle)
	}
	return w.petRuntimeState
}

func (w *darwinFloatingWindow) PetPackRuntimeLevel(declared string) (string, string) {
	if declared == petpack.RendererNative {
		return declared, ""
	}
	return petpack.RendererNative, "当前平台暂不支持宠物动画，仅显示静态图像"
}

//export maclawDarwinPetClicked
func maclawDarwinPetClicked() {
	w := currentDarwinFloating()
	if w == nil || w.app == nil {
		return
	}
	go w.app.onFloatingButtonClicked()
}

//export maclawDarwinPetDragged
func maclawDarwinPetDragged(x, y C.int) {
	w := currentDarwinFloating()
	if w == nil || w.app == nil {
		return
	}
	go w.app.onFloatingButtonDragged(int(x), int(y))
}

//export maclawDarwinPetMenu
func maclawDarwinPetMenu(action C.int) {
	w := currentDarwinFloating()
	if w == nil || w.app == nil {
		return
	}
	switch int(action) {
	case 1:
		go w.app.openPetSettingsFromMenu()
	case 2:
		go w.app.DisablePetFromMenu()
	case 3:
		go w.app.QuitApp()
	case 4:
		go w.app.showMainWindowFromPet()
	case 5:
		go w.app.openPetConversationFromMenu()
	}
}
