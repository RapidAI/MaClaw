#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include "tray_tahoe_darwin.h"

static NSStatusItem *_tahoeStatusItem = nil;
static NSMenu       *_tahoeMenu       = nil;
static NSMenu       *_tahoeCUMenu     = nil;
static NSMenuItem   *_tahoeShowItem   = nil;
static NSMenuItem   *_tahoeQuitItem   = nil;
static NSMenuItem   *_tahoeCURoot     = nil;
static NSMenuItem   *_tahoeCUStatus   = nil;
static NSMenuItem   *_tahoeCUPause    = nil;
static NSMenuItem   *_tahoeCUResume   = nil;
static NSMenuItem   *_tahoeCUStop     = nil;
static NSMenuItem   *_tahoeCUReset    = nil;

// Tags: 1=show 2=quit 3=pause 4=resume 5=stop 6=reset
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
    } else if (tag == 3) {
        tahoeOnCUPauseClicked();
    } else if (tag == 4) {
        tahoeOnCUResumeClicked();
    } else if (tag == 5) {
        tahoeOnCUStopClicked();
    } else if (tag == 6) {
        tahoeOnCUResetClicked();
    }
}
@end

static _TahoeMenuTarget *_menuTarget = nil;

static NSMenuItem *_makeItem(NSString *title, NSInteger tag, BOOL enabled) {
    NSMenuItem *item = [[NSMenuItem alloc]
        initWithTitle:title
               action:enabled ? @selector(menuAction:) : nil
        keyEquivalent:@""];
    if (enabled) {
        item.target = _menuTarget;
    }
    item.tag = tag;
    item.enabled = enabled;
    return item;
}

void TahoeCreateTray(const void *iconData, int iconLen,
                     const char *tooltip,
                     const char *showLabel, const char *quitLabel) {
    const char *t = tooltip   ? strdup(tooltip)   : NULL;
    const char *s = showLabel ? strdup(showLabel) : NULL;
    const char *q = quitLabel ? strdup(quitLabel) : NULL;
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
                    img.template = NO;
                    _tahoeStatusItem.button.image = img;
                }

                if (t) {
                    _tahoeStatusItem.button.toolTip =
                        [NSString stringWithUTF8String:t];
                }

                _menuTarget = [[_TahoeMenuTarget alloc] init];
                _tahoeMenu = [[NSMenu alloc] init];

                _tahoeShowItem = _makeItem(
                    [NSString stringWithUTF8String:s ? s : "Show"], 1, YES);
                [_tahoeMenu addItem:_tahoeShowItem];

                [_tahoeMenu addItem:[NSMenuItem separatorItem]];

                // Computer Use submenu
                _tahoeCUMenu = [[NSMenu alloc] init];
                _tahoeCUStatus = [[NSMenuItem alloc]
                    initWithTitle:@"Status: idle"
                           action:nil
                    keyEquivalent:@""];
                _tahoeCUStatus.enabled = NO;
                [_tahoeCUMenu addItem:_tahoeCUStatus];
                [_tahoeCUMenu addItem:[NSMenuItem separatorItem]];
                _tahoeCUPause  = _makeItem(@"Pause desktop actions", 3, NO);
                _tahoeCUResume = _makeItem(@"Resume desktop actions", 4, NO);
                _tahoeCUStop   = _makeItem(@"Stop desktop control", 5, NO);
                _tahoeCUReset  = _makeItem(@"Reset control state", 6, NO);
                [_tahoeCUMenu addItem:_tahoeCUPause];
                [_tahoeCUMenu addItem:_tahoeCUResume];
                [_tahoeCUMenu addItem:_tahoeCUStop];
                [_tahoeCUMenu addItem:_tahoeCUReset];

                _tahoeCURoot = [[NSMenuItem alloc]
                    initWithTitle:@"Computer Use"
                           action:nil
                    keyEquivalent:@""];
                _tahoeCURoot.submenu = _tahoeCUMenu;
                [_tahoeMenu addItem:_tahoeCURoot];

                [_tahoeMenu addItem:[NSMenuItem separatorItem]];

                _tahoeQuitItem = _makeItem(
                    [NSString stringWithUTF8String:q ? q : "Quit"], 2, YES);
                [_tahoeMenu addItem:_tahoeQuitItem];

                _tahoeStatusItem.menu = _tahoeMenu;

                NSLog(@"[tray-tahoe] NSStatusItem created with Computer Use submenu");
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

void TahoeUpdateMenu(const char *tooltip,
                      const char *showLabel, const char *quitLabel) {
    const char *t = tooltip   ? strdup(tooltip)   : NULL;
    const char *s = showLabel ? strdup(showLabel) : NULL;
    const char *q = quitLabel ? strdup(quitLabel) : NULL;
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            if (!_tahoeStatusItem) {
                free((void*)t); free((void*)s); free((void*)q);
                return;
            }
            if (t) {
                _tahoeStatusItem.button.toolTip =
                    [NSString stringWithUTF8String:t];
                free((void*)t);
            }
            if (_tahoeShowItem && s) {
                [_tahoeShowItem setTitle:[NSString stringWithUTF8String:s]];
                free((void*)s);
            } else {
                free((void*)s);
            }
            if (_tahoeQuitItem && q) {
                [_tahoeQuitItem setTitle:[NSString stringWithUTF8String:q]];
                free((void*)q);
            } else {
                free((void*)q);
            }
        }
    });
}

void TahoeUpdateComputerUseMenu(const char *menuTitle,
                                const char *statusLabel,
                                const char *pauseLabel, int pauseEnabled,
                                const char *resumeLabel, int resumeEnabled,
                                const char *stopLabel, int stopEnabled,
                                const char *resetLabel, int resetEnabled) {
    const char *mt = menuTitle   ? strdup(menuTitle)   : NULL;
    const char *st = statusLabel ? strdup(statusLabel) : NULL;
    const char *pl = pauseLabel  ? strdup(pauseLabel)  : NULL;
    const char *rl = resumeLabel ? strdup(resumeLabel) : NULL;
    const char *sl = stopLabel   ? strdup(stopLabel)   : NULL;
    const char *xl = resetLabel  ? strdup(resetLabel)  : NULL;
    int pe = pauseEnabled, re = resumeEnabled, se = stopEnabled, xe = resetEnabled;

    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            if (_tahoeCURoot && mt) {
                [_tahoeCURoot setTitle:[NSString stringWithUTF8String:mt]];
            }
            if (_tahoeCUStatus && st) {
                [_tahoeCUStatus setTitle:[NSString stringWithUTF8String:st]];
            }
            if (_tahoeCUPause && pl) {
                [_tahoeCUPause setTitle:[NSString stringWithUTF8String:pl]];
                _tahoeCUPause.enabled = pe ? YES : NO;
                _tahoeCUPause.action = pe ? @selector(menuAction:) : nil;
                _tahoeCUPause.target = pe ? _menuTarget : nil;
            }
            if (_tahoeCUResume && rl) {
                [_tahoeCUResume setTitle:[NSString stringWithUTF8String:rl]];
                _tahoeCUResume.enabled = re ? YES : NO;
                _tahoeCUResume.action = re ? @selector(menuAction:) : nil;
                _tahoeCUResume.target = re ? _menuTarget : nil;
            }
            if (_tahoeCUStop && sl) {
                [_tahoeCUStop setTitle:[NSString stringWithUTF8String:sl]];
                _tahoeCUStop.enabled = se ? YES : NO;
                _tahoeCUStop.action = se ? @selector(menuAction:) : nil;
                _tahoeCUStop.target = se ? _menuTarget : nil;
            }
            if (_tahoeCUReset && xl) {
                [_tahoeCUReset setTitle:[NSString stringWithUTF8String:xl]];
                _tahoeCUReset.enabled = xe ? YES : NO;
                _tahoeCUReset.action = xe ? @selector(menuAction:) : nil;
                _tahoeCUReset.target = xe ? _menuTarget : nil;
            }
            free((void*)mt);
            free((void*)st);
            free((void*)pl);
            free((void*)rl);
            free((void*)sl);
            free((void*)xl);
        }
    });
}

void TahoeDockBounce(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            [NSApp requestUserAttention:NSCriticalRequest];
        }
    });
}
