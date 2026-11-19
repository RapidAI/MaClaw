// Objective-C implementation for the macOS desktop pet window.
// The ObjC classes live here, not in floating_darwin.go: cgo compiles the Go
// preamble into the same object as the .m, and @implementation in both would
// emit duplicate _OBJC_CLASS_ symbols at link time.
#import <Cocoa/Cocoa.h>
#include "floating_darwin.h"
#import <math.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>

// Desktop pet window. The previous build painted the app icon inside a
// circular badge and ignored the mouse, so the pet could not be seen or
// dragged. This window paints the pack still with a clear background and
// tracks left-drag itself.


static NSWindow *floatingWin = nil;
static NSView *petView = nil;
static NSImage *petImage = nil;
static NSBitmapImageRep *petRep = nil;
static int petRepW = 0;
static int petRepH = 0;
static int petRepPointW = 0;
static int petRepPointH = 0;

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
static atomic_int petDragActive = 0;
static atomic_uint acceptedPaintGen = 0;
static BOOL petFaceLeft = NO;
static void dropPendingPetMove(void);
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
	// The PNG path owns this image. Drop the reused animation rep so the
	// next frame builds a new one instead of writing into a detached buffer.
	petRep = nil;
	petRepW = 0;
	petRepH = 0;
	petRepPointW = 0;
	petRepPointH = 0;
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
	[NSGraphicsContext saveGraphicsState];
	[[NSColor clearColor] set];
	NSRectFillUsingOperation(self.bounds, NSCompositingOperationClear);
	if (petImage != nil) {
		if (petFaceLeft) {
			NSAffineTransform *flip = [NSAffineTransform transform];
			[flip translateXBy:NSMaxX(self.bounds) yBy:0];
			[flip scaleXBy:-1 yBy:1];
			[flip concat];
		}
		[petImage drawInRect:self.bounds
					fromRect:NSZeroRect
				   operation:NSCompositingOperationSourceOver
					fraction:1.0];
	}
	[NSGraphicsContext restoreGraphicsState];
}

- (void)showContextMenu:(NSEvent *)event {
	NSMenu *menu = [[NSMenu alloc] initWithTitle:@""];
	NSMenuItem *pin = [[NSMenuItem alloc] initWithTitle:@"定住"
												 action:@selector(maclawMenuAction:)
										  keyEquivalent:@""];
	pin.target = self;
	pin.tag = 6;
	pin.state = maclawDarwinPetPositionLocked() ? NSControlStateValueOn : NSControlStateValueOff;
	[menu addItem:pin];
	[menu addItem:[NSMenuItem separatorItem]];
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
	}
	[NSMenu popUpContextMenu:menu withEvent:event forView:self];
}

- (void)maclawMenuAction:(id)sender {
	int action = (int)[(NSMenuItem *)sender tag];
	darwinPerformAfterTracking(^{
		maclawDarwinPetMenu(action);
	});
}

// A non-activating panel often receives mouseDown and then never gets
// mouseDragged/mouseUp. Track the button here until it is released.
// ARC holds the view and the strong local holds the window, so a hide during
// the gesture cannot free them out from under this call.
- (void)mouseDown:(NSEvent *)event {
	// ARC keeps self alive for the scope of this call, and the strong local
	// holds the window, so the gesture survives a hide mid-drag.
	NSWindow *tracked = floatingWin;
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
			if (!moved && (fabs(dx) > 5.0 || fabs(dy) > 5.0)) {
				moved = YES;
				atomic_store(&petDragActive, 1);
			}
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
		if (floatingWin == nil || !moved) {
			atomic_store(&petDragActive, 0);
			if (floatingWin != nil) {
				darwinPerformAfterTracking(^{
					maclawDarwinPetClicked();
				});
			}
		} else {
			NSScreen *landed = screenForWindow();
			if (landed != nil) rememberScreen(landed);
			int x = 0, y = 0;
			darwinReadTopLeft(&x, &y);
			// The main queue did not run during tracking, so a walk move from
			// before the drag threshold is still pending. Drop it before Go
			// stores this origin, or it lands first and yanks the pet back.
			// petDragActive stays set until that store finishes.
			dropPendingPetMove();
			darwinPerformAfterTracking(^{
				maclawDarwinPetDragged(x, y);
			});
		}
		if ([tracked isKeyWindow]) [tracked resignKeyWindow];
	}
}

- (void)rightMouseDown:(NSEvent *)event {
	[self showContextMenu:event];
}

@end

int createFloatingWindowDarwin(int x, int y, int w, int h, const void *pngData, int pngLen) {
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
		// ARC owns this panel. releasedWhenClosed:YES makes close send an
		// extra release, so the following nil (and any later skin switch)
		// frees the window twice and crashes the process.
		[panel setReleasedWhenClosed:NO];
		petFaceLeft = NO;
		[floatingWin setCollectionBehavior:
			NSWindowCollectionBehaviorCanJoinAllSpaces |
			NSWindowCollectionBehaviorStationary |
			NSWindowCollectionBehaviorFullScreenAuxiliary];

		MaclawPetView *view = [[MaclawPetView alloc] initWithFrame:NSMakeRect(0, 0, w, h)];
		[floatingWin setContentView:view];
		petView = view;
		replacePetImage(img);
		movePetTopLeft(x, y);
		ok = 1;
	});
	return ok;
}

static void showFloatingWindowNow(void) {
	if (floatingWin == nil) return;
	[floatingWin orderFrontRegardless];
	if (petView != nil) [petView displayIfNeeded];
}

static void hideFloatingWindowNow(void) {
	if (floatingWin != nil) [floatingWin orderOut:nil];
}

void showFloatingWindowDarwin(void) {
	// A background caller must not wait on the main queue. Pet switches run
	// while the manager lock is held, and the main thread may be inside the
	// drag tracking loop, which does not drain that queue.
	if ([NSThread isMainThread]) {
		showFloatingWindowNow();
		return;
	}
	dispatch_async(dispatch_get_main_queue(), ^{
		showFloatingWindowNow();
	});
}

void hideFloatingWindowDarwin(void) {
	if ([NSThread isMainThread]) {
		hideFloatingWindowNow();
		return;
	}
	dispatch_async(dispatch_get_main_queue(), ^{
		hideFloatingWindowNow();
	});
}

void destroyFloatingWindowDarwin(void) {
	darwinSync(^{
		// Drop the globals before close. With releasedWhenClosed left at NO,
		// the local strong reference is the last owner and releases once.
		NSWindow *win = floatingWin;
		floatingWin = nil;
		petView = nil;
		petImage = nil;
		petRep = nil;
		petRepW = 0;
		petRepH = 0;
		petRepPointW = 0;
		petRepPointH = 0;
		atomic_store(&petDragActive, 0);
		petFaceLeft = NO;
		if (win != nil) {
			[win setReleasedWhenClosed:NO];
			[win orderOut:nil];
			[win close];
		}
	});
}

// Latest pose, facing, and position. The animator thread copies pixels and
// returns; the main queue applies only the newest update. A paint generation
// keeps a frame that was queued before destroy from landing on the next pet.
typedef struct {
	uint8_t *px;
	int w, h, stride;
	int pointW, pointH;
	int faceLeft;
	int moveX, moveY;
	int resizeW, resizeH;
	unsigned gen;
	int hasPixels;
	int hasFace;
	int hasMove;
	int hasResize;
} MaclawPetUpdate;

static MaclawPetUpdate petPending;
static pthread_mutex_t petPendingMu = PTHREAD_MUTEX_INITIALIZER;
static atomic_int petUpdateQueued = 0;

// Go hands over straight RGBA. SourceOver on this clear window treats the
// bitmap as premultiplied, so a straight copy leaves a dark fringe and can
// fail to build a rep at all. Premultiply once, off the main thread.
static uint8_t *copyPremulRGBA(const uint8_t *src, int w, int h, int stride) {
	size_t row = (size_t)w * 4u;
	uint8_t *dst = (uint8_t *)malloc(row * (size_t)h);
	if (dst == NULL || src == NULL) {
		free(dst);
		return NULL;
	}
	for (int y = 0; y < h; y++) {
		const uint8_t *s = src + (size_t)y * (size_t)stride;
		uint8_t *d = dst + (size_t)y * row;
		for (int x = 0; x < w; x++) {
			unsigned a = s[3];
			d[0] = (uint8_t)((s[0] * a + 127u) / 255u);
			d[1] = (uint8_t)((s[1] * a + 127u) / 255u);
			d[2] = (uint8_t)((s[2] * a + 127u) / 255u);
			d[3] = (uint8_t)a;
			s += 4;
			d += 4;
		}
	}
	return dst;
}

static void ensurePetRep(int w, int h, int pointW, int pointH) {
	if (pointW < 1) pointW = w;
	if (pointH < 1) pointH = h;
	if (petRep != nil && petRepW == w && petRepH == h &&
		petRepPointW == pointW && petRepPointH == pointH && petImage != nil) {
		return;
	}
	// Default format is premultiplied RGBA. The little-endian flag would
	// swap channels. The rep's point size must match the window or a 2x
	// bitmap draws as if it were twice as wide and gets clipped.
	NSBitmapImageRep *rep = [[NSBitmapImageRep alloc]
		initWithBitmapDataPlanes:NULL
		pixelsWide:w
		pixelsHigh:h
		bitsPerSample:8
		samplesPerPixel:4
		hasAlpha:YES
		isPlanar:NO
		colorSpaceName:NSDeviceRGBColorSpace
		bitmapFormat:0
		bytesPerRow:w * 4
		bitsPerPixel:32];
	if (rep == nil || [rep bitmapData] == NULL) return;
	[rep setSize:NSMakeSize(pointW, pointH)];
	NSImage *img = [[NSImage alloc] initWithSize:NSMakeSize(pointW, pointH)];
	[img addRepresentation:rep];
	petRep = rep;
	petRepW = w;
	petRepH = h;
	petRepPointW = pointW;
	petRepPointH = pointH;
	petImage = img;
}

static BOOL blitPetPixels(const uint8_t *px, int w, int h, int pointW, int pointH) {
	ensurePetRep(w, h, pointW, pointH);
	if (petRep == nil || petRepW != w || petRepH != h || [petRep bitmapData] == NULL) return NO;
	uint8_t *dst = [petRep bitmapData];
	NSInteger rowBytes = [petRep bytesPerRow];
	size_t srcRow = (size_t)w * 4u;
	if (rowBytes < (NSInteger)srcRow) return NO;
	if (rowBytes == (NSInteger)srcRow) {
		memcpy(dst, px, srcRow * (size_t)h);
	} else {
		for (int y = 0; y < h; y++) {
			memcpy(dst + (size_t)y * (size_t)rowBytes, px + (size_t)y * srcRow, srcRow);
		}
	}
	[petImage recache];
	return YES;
}

static void resizePetNow(int w, int h) {
	if (floatingWin == nil) return;
	if (w < 1) w = 1;
	if (h < 1) h = 1;
	NSRect frame = floatingWin.frame;
	CGFloat top = NSMaxY(frame);
	frame.size.width = w;
	frame.size.height = h;
	frame.origin.y = top - h;
	[floatingWin setFrame:frame display:NO];
	if (petView != nil) {
		[petView setFrame:NSMakeRect(0, 0, w, h)];
		[petView setNeedsDisplay:YES];
	}
}

static void dropPendingPetMove(void) {
	pthread_mutex_lock(&petPendingMu);
	petPending.hasMove = 0;
	pthread_mutex_unlock(&petPendingMu);
}

static void applyPetUpdate(MaclawPetUpdate update) {
	if (update.gen != atomic_load(&acceptedPaintGen) || floatingWin == nil) return;
	// Move before the flush. Drawing first composites this pose at the old
	// origin, then the window jumps.
	if (update.hasResize) resizePetNow(update.resizeW, update.resizeH);
	if (update.hasMove) movePetTopLeft(update.moveX, update.moveY);
	if (update.hasFace) petFaceLeft = update.faceLeft ? YES : NO;
	BOOL drew = NO;
	if (update.hasPixels && update.px != NULL) {
		drew = blitPetPixels(update.px, update.w, update.h, update.pointW, update.pointH);
	}
	// A clear borderless panel often ignores a dirty mark until something
	// else dirties the screen. Draw now when the pet is on screen so a
	// pose change is visible without waiting for the next click.
	if ((drew || update.hasFace || update.hasResize) && petView != nil) {
		[petView setNeedsDisplay:YES];
		if (floatingWin != nil && [floatingWin isVisible]) {
			[petView displayIfNeeded];
		}
	}
}

static void drainPetUpdates(void) {
	for (;;) {
		pthread_mutex_lock(&petPendingMu);
		MaclawPetUpdate update = petPending;
		memset(&petPending, 0, sizeof(petPending));
		if (!update.hasPixels && !update.hasFace && !update.hasMove && !update.hasResize) {
			atomic_store(&petUpdateQueued, 0);
			int raced = petPending.hasPixels || petPending.hasFace || petPending.hasMove || petPending.hasResize;
			if (raced) atomic_store(&petUpdateQueued, 1);
			pthread_mutex_unlock(&petPendingMu);
			if (!raced) return;
			continue;
		}
		pthread_mutex_unlock(&petPendingMu);
		applyPetUpdate(update);
		free(update.px);
	}
}

static void enqueuePetUpdate(MaclawPetUpdate next) {
	if (next.gen != atomic_load(&acceptedPaintGen)) {
		free(next.px);
		return;
	}
	pthread_mutex_lock(&petPendingMu);
	if (next.gen != atomic_load(&acceptedPaintGen)) {
		pthread_mutex_unlock(&petPendingMu);
		free(next.px);
		return;
	}
	if (next.hasPixels) {
		free(petPending.px);
		petPending.px = next.px;
		petPending.w = next.w;
		petPending.h = next.h;
		petPending.stride = next.stride;
		petPending.pointW = next.pointW;
		petPending.pointH = next.pointH;
		petPending.hasPixels = 1;
		next.px = NULL;
	}
	if (next.hasFace) {
		petPending.faceLeft = next.faceLeft;
		petPending.hasFace = 1;
	}
	if (next.hasMove) {
		petPending.moveX = next.moveX;
		petPending.moveY = next.moveY;
		petPending.hasMove = 1;
	}
	if (next.hasResize) {
		petPending.resizeW = next.resizeW;
		petPending.resizeH = next.resizeH;
		petPending.hasResize = 1;
	}
	petPending.gen = next.gen;
	int queued = atomic_exchange(&petUpdateQueued, 1);
	pthread_mutex_unlock(&petPendingMu);
	free(next.px);
	if (!queued) {
		dispatch_async(dispatch_get_main_queue(), ^{
			drainPetUpdates();
		});
	}
}

void darwinSetPaintGen(unsigned gen) {
	// Atomic only: callers may hold the Go window lock. Do not hop to main.
	atomic_store(&acceptedPaintGen, gen);
	pthread_mutex_lock(&petPendingMu);
	free(petPending.px);
	memset(&petPending, 0, sizeof(petPending));
	pthread_mutex_unlock(&petPendingMu);
}

void darwinPetDragClear(void) {
	atomic_store(&petDragActive, 0);
}

void setPetPixelsDarwin(const void *pixels, int w, int h, int stride, int pointW, int pointH, int faceLeft, unsigned gen) {
	if (pixels == NULL || w < 1 || h < 1 || w > 4096 || h > 4096 || stride < w * 4) return;
	if (gen != atomic_load(&acceptedPaintGen)) return;
	uint8_t *copy = copyPremulRGBA((const uint8_t *)pixels, w, h, stride);
	if (copy == NULL) return;
	MaclawPetUpdate update;
	memset(&update, 0, sizeof(update));
	update.px = copy;
	update.w = w;
	update.h = h;
	update.stride = w * 4;
	update.pointW = pointW;
	update.pointH = pointH;
	update.faceLeft = faceLeft ? 1 : 0;
	update.gen = gen;
	update.hasPixels = 1;
	update.hasFace = 1;
	enqueuePetUpdate(update);
}

void setPetFaceDarwin(int faceLeft, unsigned gen) {
	MaclawPetUpdate update;
	memset(&update, 0, sizeof(update));
	update.faceLeft = faceLeft ? 1 : 0;
	update.gen = gen;
	update.hasFace = 1;
	enqueuePetUpdate(update);
}

void moveFloatingWindowDarwin(int x, int y, unsigned gen) {
	if (gen != atomic_load(&acceptedPaintGen)) return;
	MaclawPetUpdate update;
	memset(&update, 0, sizeof(update));
	update.moveX = x;
	update.moveY = y;
	update.gen = gen;
	update.hasMove = 1;
	enqueuePetUpdate(update);
}

void resizeFloatingWindowDarwin(int w, int h, unsigned gen) {
	if (w < 1) w = 1;
	if (h < 1) h = 1;
	MaclawPetUpdate update;
	memset(&update, 0, sizeof(update));
	update.resizeW = w;
	update.resizeH = h;
	update.gen = gen;
	update.hasResize = 1;
	enqueuePetUpdate(update);
}

int darwinPetDragActive(void) {
	return atomic_load(&petDragActive);
}

void setPetImageDarwin(const void *pngData, int pngLen) {
	darwinSync(^{
		NSImage *img = imageFromPNG(pngData, pngLen);
		if (img == nil) return;
		replacePetImage(img);
	});
}

// Prefer the cache filled on the main thread. A direct NSScreen read is only
// the startup fallback; AppKit screen queries off the main thread crash, and
// bouncing them onto the main queue deadlocks tests that have no run loop.
int darwinBackingScale(void) {
	int scale = atomic_load(&cachedScale);
	if (scale > 0) return scale;
	if (![NSThread isMainThread]) return 2;
	NSScreen *screen = [NSScreen mainScreen];
	if (screen == nil) return 2;
	rememberScreen(screen);
	scale = atomic_load(&cachedScale);
	return scale > 0 ? scale : 2;
}

int darwinScreenWidth(void) {
	int width = atomic_load(&cachedScreenW);
	if (width > 0) return width;
	if (![NSThread isMainThread]) return 0;
	NSScreen *screen = [NSScreen mainScreen];
	if (screen == nil) return 0;
	return (int)llround(screen.frame.size.width);
}

int darwinScreenHeight(void) {
	int height = atomic_load(&cachedScreenH);
	if (height > 0) return height;
	if (![NSThread isMainThread]) return 0;
	NSScreen *screen = [NSScreen mainScreen];
	if (screen == nil) return 0;
	return (int)llround(screen.frame.size.height);
}
