#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include "tray_tahoe_darwin.h"

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

void TahoeUpdateMenu(const char *tooltip,
                      const char *showLabel, const char *quitLabel) {
    const char *t = tooltip   ? strdup(tooltip)   : NULL;
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

void TahoeDockBounce(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            [NSApp requestUserAttention:NSCriticalRequest];
        }
    });
}
