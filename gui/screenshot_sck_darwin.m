// screenshot_sck_darwin.m — ScreenCaptureKit bridge for Go/CGO.
// Uses SCScreenshotManager.captureImage (macOS 14+) to capture the primary
// display as a single screenshot. Runs synchronously via dispatch_semaphore
// so it can be called from Go without async callback complexity.
//
// All ScreenCaptureKit classes are loaded via NSClassFromString at runtime,
// so this compiles fine even without the ScreenCaptureKit framework header.
// The framework is weak-linked to avoid crash on macOS < 12.3.

#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
#import <ImageIO/ImageIO.h>
#include <stdlib.h>
#include <dlfcn.h>
#include <dispatch/dispatch.h>

// Error codes:
// 1 = SCK not available (macOS < 14)
// 2 = getShareableContent failed
// 3 = no displays
// 4 = captureImage failed
// 5 = PNG encode failed
// 6 = timeout

const void* SCKCaptureScreenshot(size_t* outLen, int* outErrCode) {
    *outLen = 0;
    *outErrCode = 0;

    @autoreleasepool {

    // Ensure ScreenCaptureKit framework is loaded (not linked at build time).
    static dispatch_once_t onceToken;
    dispatch_once(&onceToken, ^{
        dlopen("/System/Library/Frameworks/ScreenCaptureKit.framework/ScreenCaptureKit", RTLD_LAZY);
    });

    // Check if ScreenCaptureKit is available at runtime
    Class SCShareableContent = NSClassFromString(@"SCShareableContent");
    Class SCContentFilter = NSClassFromString(@"SCContentFilter");
    Class SCStreamConfiguration = NSClassFromString(@"SCStreamConfiguration");
    Class SCScreenshotManager = NSClassFromString(@"SCScreenshotManager");

    if (!SCShareableContent || !SCContentFilter || !SCStreamConfiguration || !SCScreenshotManager) {
        *outErrCode = 1;
        return NULL;
    }

    // Step 1: Get shareable content (list of displays/windows)
    __block id shareableContent = nil;
    __block NSError* contentError = nil;
    dispatch_semaphore_t contentSem = dispatch_semaphore_create(0);

    SEL getContentSel = NSSelectorFromString(@"getShareableContentExcludingDesktopWindows:onScreenWindowsOnly:completionHandler:");
    if (![SCShareableContent respondsToSelector:getContentSel]) {
        *outErrCode = 1;
        return NULL;
    }

    NSMethodSignature* sig = [SCShareableContent methodSignatureForSelector:getContentSel];
    NSInvocation* inv = [NSInvocation invocationWithMethodSignature:sig];
    [inv setSelector:getContentSel];
    [inv setTarget:SCShareableContent];
    BOOL excludeDesktop = NO;
    BOOL onScreenOnly = YES;
    [inv setArgument:&excludeDesktop atIndex:2];
    [inv setArgument:&onScreenOnly atIndex:3];

    void (^contentBlock)(id, NSError*) = ^(id content, NSError* error) {
        shareableContent = content;
        contentError = error;
        dispatch_semaphore_signal(contentSem);
    };
    [inv setArgument:&contentBlock atIndex:4];
    [inv invoke];

    if (dispatch_semaphore_wait(contentSem, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC)) != 0) {
        *outErrCode = 6;
        return NULL;
    }

    if (contentError || !shareableContent) {
        *outErrCode = 2;
        return NULL;
    }

    // Step 2: Get the primary display
    NSArray* displays = [shareableContent valueForKey:@"displays"];
    if (!displays || displays.count == 0) {
        *outErrCode = 3;
        return NULL;
    }
    id primaryDisplay = displays[0];

    // Step 3: Create content filter for the display (excluding no windows = full screen)
    SEL initFilterSel = NSSelectorFromString(@"initWithDisplay:excludingWindows:");
    if (![SCContentFilter instancesRespondToSelector:initFilterSel]) {
        *outErrCode = 1;
        return NULL;
    }
    id filterAlloc = [SCContentFilter alloc];
    NSArray* emptyWindows = @[];

    NSMethodSignature* filterSig = [SCContentFilter instanceMethodSignatureForSelector:initFilterSel];
    if (!filterSig) {
        *outErrCode = 1;
        return NULL;
    }
    NSInvocation* filterInv = [NSInvocation invocationWithMethodSignature:filterSig];
    [filterInv setSelector:initFilterSel];
    [filterInv setTarget:filterAlloc];
    [filterInv setArgument:&primaryDisplay atIndex:2];
    [filterInv setArgument:&emptyWindows atIndex:3];
    [filterInv invoke];

    __unsafe_unretained id filter = nil;
    [filterInv getReturnValue:&filter];
    if (!filter) {
        *outErrCode = 4;
        return NULL;
    }

    // Step 4: Create and configure stream configuration
    id config = [[SCStreamConfiguration alloc] init];

    // Get display's native pixel dimensions for Retina-correct capture.
    CGDirectDisplayID displayID = 0;
    if ([primaryDisplay respondsToSelector:NSSelectorFromString(@"displayID")]) {
        NSMethodSignature* didSig = [primaryDisplay methodSignatureForSelector:NSSelectorFromString(@"displayID")];
        NSInvocation* didInv = [NSInvocation invocationWithMethodSignature:didSig];
        [didInv setSelector:NSSelectorFromString(@"displayID")];
        [didInv setTarget:primaryDisplay];
        [didInv invoke];
        [didInv getReturnValue:&displayID];
    }
    if (displayID != 0) {
        // Use CGDisplayMode to get actual pixel width/height (Retina physical pixels).
        CGDisplayModeRef mode = CGDisplayCopyDisplayMode(displayID);
        if (mode) {
            size_t pixelW = CGDisplayModeGetPixelWidth(mode);
            size_t pixelH = CGDisplayModeGetPixelHeight(mode);
            CGDisplayModeRelease(mode);
            if (pixelW > 0 && pixelH > 0) {
                [config setValue:@(pixelW) forKey:@"width"];
                [config setValue:@(pixelH) forKey:@"height"];
            }
        } else {
            // Fallback to logical size (non-Retina or mode unavailable)
            size_t logW = CGDisplayPixelsWide(displayID);
            size_t logH = CGDisplayPixelsHigh(displayID);
            if (logW > 0 && logH > 0) {
                [config setValue:@(logW) forKey:@"width"];
                [config setValue:@(logH) forKey:@"height"];
            }
        }
    }

    // Step 5: Capture screenshot via SCScreenshotManager
    SEL captureSel = NSSelectorFromString(@"captureImageWithFilter:configuration:completionHandler:");
    if (![SCScreenshotManager respondsToSelector:captureSel]) {
        *outErrCode = 1;
        return NULL;
    }

    __block CGImageRef capturedImage = NULL;
    __block NSError* capturedError = nil;
    dispatch_semaphore_t captureSem = dispatch_semaphore_create(0);

    NSMethodSignature* capSig = [SCScreenshotManager methodSignatureForSelector:captureSel];
    NSInvocation* capInv = [NSInvocation invocationWithMethodSignature:capSig];
    [capInv setSelector:captureSel];
    [capInv setTarget:SCScreenshotManager];
    [capInv setArgument:&filter atIndex:2];
    [capInv setArgument:&config atIndex:3];

    void (^captureBlock)(CGImageRef, NSError*) = ^(CGImageRef image, NSError* error) {
        if (image) {
            capturedImage = CGImageRetain(image);
        }
        capturedError = error;
        dispatch_semaphore_signal(captureSem);
    };
    [capInv setArgument:&captureBlock atIndex:4];
    [capInv invoke];

    if (dispatch_semaphore_wait(captureSem, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC)) != 0) {
        *outErrCode = 6;
        return NULL;
    }

    if (capturedError || !capturedImage) {
        *outErrCode = 4;
        return NULL;
    }

    // Step 6: Encode CGImageRef to PNG in memory
    CFMutableDataRef pngData = CFDataCreateMutable(kCFAllocatorDefault, 0);
    if (!pngData) {
        CGImageRelease(capturedImage);
        *outErrCode = 5;
        return NULL;
    }

    CGImageDestinationRef dest = CGImageDestinationCreateWithData(pngData, CFSTR("public.png"), 1, NULL);
    if (!dest) {
        CFRelease(pngData);
        CGImageRelease(capturedImage);
        *outErrCode = 5;
        return NULL;
    }

    CGImageDestinationAddImage(dest, capturedImage, NULL);
    bool ok = CGImageDestinationFinalize(dest);
    CFRelease(dest);
    CGImageRelease(capturedImage);

    if (!ok) {
        CFRelease(pngData);
        *outErrCode = 5;
        return NULL;
    }

    CFIndex len = CFDataGetLength(pngData);
    if (len <= 0) {
        CFRelease(pngData);
        *outErrCode = 5;
        return NULL;
    }

    // Copy to malloc'd buffer for Go ownership (outside ARC scope).
    void* buf = malloc((size_t)len);
    if (!buf) {
        CFRelease(pngData);
        *outErrCode = 5;
        return NULL;
    }
    CFDataGetBytes(pngData, CFRangeMake(0, len), (UInt8*)buf);
    CFRelease(pngData);

    *outLen = (size_t)len;
    return buf;

    } // @autoreleasepool
}
