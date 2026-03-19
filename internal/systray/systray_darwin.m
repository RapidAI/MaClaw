#import <Cocoa/Cocoa.h>
#include "systray.h"

#if __MAC_OS_X_VERSION_MIN_REQUIRED < 101400

    #ifndef NSControlStateValueOff
      #define NSControlStateValueOff NSOffState
    #endif

    #ifndef NSControlStateValueOn
      #define NSControlStateValueOn NSOnState
    #endif

#endif

@interface MenuItem : NSObject {
  @public
    NSNumber* menuId;
    NSNumber* parentMenuId;
    NSString* title;
    NSString* tooltip;
    NSString* shortcutKey;
    short disabled;
    short checked;
}

-(id) initWithId: (int)theMenuId
withParentMenuId: (int)theParentMenuId
       withTitle: (const char*)theTitle
     withTooltip: (const char*)theTooltip
 withShortcutKey: (const char*)theShortcutKey
    withDisabled: (short)theDisabled
     withChecked: (short)theChecked;
     @end
     @implementation MenuItem
     -(id) initWithId: (int)theMenuId
     withParentMenuId: (int)theParentMenuId
            withTitle: (const char*)theTitle
          withTooltip: (const char*)theTooltip
      withShortcutKey: (const char*)theShortcutKey
         withDisabled: (short)theDisabled
          withChecked: (short)theChecked
{
  menuId = [NSNumber numberWithInt:theMenuId];
  parentMenuId = [NSNumber numberWithInt:theParentMenuId];
  title = [[NSString alloc] initWithCString:theTitle
                                   encoding:NSUTF8StringEncoding];
  tooltip = [[NSString alloc] initWithCString:theTooltip
                                     encoding:NSUTF8StringEncoding];
  disabled = theDisabled;
  checked = theChecked;
  return self;
}
@end

@interface SystrayDelegate: NSObject <NSApplicationDelegate>
  - (void) add_or_update_menu_item:(MenuItem*) item;
  - (IBAction)menuHandler:(id)sender;
  - (void)statusOnClick:(NSButton *)btn;
  - (void)initStatusBar;
  @property (assign) IBOutlet NSWindow *window;
  @end

@implementation SystrayDelegate {
  NSStatusItem *statusItem;
  NSMenu *menu;
  NSCondition* cond;
}

@synthesize window = _window;

- (void)applicationDidFinishLaunching:(NSNotification *)aNotification {
  [self initStatusBar];
  systray_ready();
}

- (void)initStatusBar {
  self->statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
  self->menu = [[NSMenu alloc] init];
  [self->menu setAutoenablesItems: FALSE];
}

- (void)applicationWillTerminate:(NSNotification *)aNotification {
  systray_on_exit();
}

- (void)setIcon:(NSImage *)image {
  statusItem.button.image = image;
  [self updateTitleButtonStyle];
}

- (void)setTitle:(NSString *)title {
  statusItem.button.title = title;
  [self updateTitleButtonStyle];
}

-(void)updateTitleButtonStyle {
  if (statusItem.button.image != nil) {
    if ([statusItem.button.title length] == 0) {
      statusItem.button.imagePosition = NSImageOnly;
    } else {
      statusItem.button.imagePosition = NSImageLeft;
    }
  } else {
    statusItem.button.imagePosition = NSNoImage;
  }
}


- (void)setTooltip:(NSString *)tooltip {
  statusItem.button.toolTip = tooltip;
}

- (IBAction)menuHandler:(id)sender {
  NSNumber* menuId = [sender representedObject];
  systray_menu_item_selected(menuId.intValue);
}

- (void)add_or_update_menu_item:(MenuItem *)item {
  NSMenu *theMenu = self->menu;
  NSMenuItem *parentItem;
  //create_menu();
  if ([item->parentMenuId integerValue] > 0) {
    parentItem = find_menu_item(menu, item->parentMenuId);
    if (parentItem.hasSubmenu) {
      theMenu = parentItem.submenu;
    } else {
      theMenu = [[NSMenu alloc] init];
      [theMenu setAutoenablesItems:NO];
      [parentItem setSubmenu:theMenu];
    }
  }
  
  NSMenuItem *menuItem;
  menuItem = find_menu_item(theMenu, item->menuId);
  //item->shortcutKey
  if (menuItem == NULL) {
    menuItem = [theMenu addItemWithTitle:item->title action:@selector(menuHandler:) keyEquivalent:@""];
    [menuItem setRepresentedObject:item->menuId];
  }
  [menuItem setTitle:item->title];
  [menuItem setTag:[item->menuId integerValue]];
  [menuItem setTarget:self];
  [menuItem setToolTip:item->tooltip];
  if (item->disabled == 1) {
    menuItem.enabled = FALSE;
  } else {
    menuItem.enabled = TRUE;
  }
  if (item->checked == 1) {
    menuItem.state = NSControlStateValueOn;
  } else {
    menuItem.state = NSControlStateValueOff;
  }
}

NSMenuItem *find_menu_item(NSMenu *ourMenu, NSNumber *menuId) {
  NSMenuItem *foundItem = [ourMenu itemWithTag:[menuId integerValue]];
  if (foundItem != NULL) {
    return foundItem;
  }
  NSArray *menu_items = ourMenu.itemArray;
  int i;
  for (i = 0; i < [menu_items count]; i++) {
    NSMenuItem *i_item = [menu_items objectAtIndex:i];
    if (i_item.hasSubmenu) {
      foundItem = find_menu_item(i_item.submenu, menuId);
      if (foundItem != NULL) {
        return foundItem;
      }
    }
  }

  return NULL;
};

- (void) add_separator:(NSNumber*) menuId {
  [menu addItem: [NSMenuItem separatorItem]];
}

- (void) hide_menu_item:(NSNumber*) menuId {
  NSMenuItem* menuItem = find_menu_item(menu, menuId);
  if (menuItem != NULL) {
    [menuItem setHidden:TRUE];
  }
}

- (void) setMenuItemIcon:(NSArray*)imageAndMenuId {
  NSImage* image = [imageAndMenuId objectAtIndex:0];
  NSNumber* menuId = [imageAndMenuId objectAtIndex:1];

  NSMenuItem* menuItem;
  menuItem = find_menu_item(menu, menuId);
  if (menuItem == NULL) {
    return;
  }
  menuItem.image = image;
}

- (void) show_menu_item:(NSNumber*) menuId {
  NSMenuItem* menuItem = find_menu_item(menu, menuId);
  if (menuItem != NULL) {
    [menuItem setHidden:FALSE];
  }
}

- (void) create_menu {
  if(statusItem.menu == NULL){
    [statusItem setMenu:menu];
  }
}

- (void) set_menu_nil {
  if(statusItem.menu != NULL){
    [statusItem setMenu:NULL];
  }
}

- (void) reset_menu {
  [self->menu removeAllItems];
}

- (void) quit {
  // When running inside a host app (Wails), do NOT call [NSApp terminate:]
  // because the host owns the application lifecycle.  Simply remove the
  // status item from the menu bar so the tray disappears cleanly.
  if (self->statusItem != nil) {
    [[NSStatusBar systemStatusBar] removeStatusItem:self->statusItem];
    self->statusItem = nil;
  }
}

- (void) statusOnClick:(NSButton *)btn {
    NSEvent *event = [NSApp currentEvent];
    if(event.type == NSEventTypeLeftMouseUp){
        systray_on_click();
    }else if(event.type == NSEventTypeRightMouseUp){
        systray_on_rclick();
    }
}

- (void) show_menu {
    create_menu();
    [statusItem.button performClick:nil];
    set_menu_nil();
}

- (void) enable_on_click {
  [statusItem.button setAction:@selector(statusOnClick:)];
  [statusItem.button sendActionOn:(NSEventMaskLeftMouseUp|NSEventMaskRightMouseUp)];
}

@end

bool internalLoop = false;
SystrayDelegate *owner;

void setInternalLoop(bool i) {
	internalLoop = i;
}

void registerSystray(void) {
  if (!internalLoop) { // with an external loop we don't take ownership of the app
    NSLog(@"[systray] registerSystray: external loop mode, skipping");
    return;
  }
  NSLog(@"[systray] registerSystray: internal loop mode, setting delegate");
  owner = [[SystrayDelegate alloc] init];
  [[NSApplication sharedApplication] setDelegate:owner];

  // A workaround to avoid crashing on macOS versions before Catalina. Somehow
  // SIGSEGV would happen inside AppKit if [NSApp run] is called from a
  // different function, even if that function is called right after this.
  if (floor(NSAppKitVersionNumber) <= /*NSAppKitVersionNumber10_14*/ 1671){
    [NSApp run];
  }
}

void nativeEnd(void) {
  systray_on_exit();
}

int nativeLoop(void) {
  if (floor(NSAppKitVersionNumber) > /*NSAppKitVersionNumber10_14*/ 1671){
    [NSApp run];
  }
  return EXIT_SUCCESS;
}

// _systrayInitBlock performs the actual status bar creation on the main thread.
// Extracted so it can be called from multiple code paths (direct dispatch or
// notification observer).
static void _systrayInitBlock(void) {
  @autoreleasepool {
    @try {
      NSLog(@"[systray] init block executing on main thread");
      owner = [[SystrayDelegate alloc] init];
      NSLog(@"[systray] SystrayDelegate allocated");
      [owner initStatusBar];
      NSLog(@"[systray] initStatusBar done, calling systray_ready");
      systray_ready();
      NSLog(@"[systray] systray_ready returned");
    } @catch (NSException *exception) {
      NSLog(@"[systray] EXCEPTION in init block: %@ — %@", exception.name, exception.reason);
    }
  }
}

void nativeStart(void) {
  // When used with an external event loop (e.g. Wails), this function is called
  // from a background goroutine.  We must initialise the status bar on the main
  // thread after the host app has fully launched.
  //
  // On macOS 26 (Tahoe) with Liquid Glass, an immediate dispatch_async can
  // crash because the window server / rendering pipeline isn't ready yet.
  // Strategy:
  //   1. If NSApp is already running and active, use dispatch_after with a
  //      short delay to let the first frame settle.
  //   2. Otherwise, observe NSApplicationDidFinishLaunchingNotification and
  //      init after a delay once it fires.
  NSLog(@"[systray] nativeStart: scheduling deferred init on main queue");

  // Helper: schedule the init block after a delay on the main queue.
  // The 1.5 s delay gives Wails + Liquid Glass time to finish first-frame
  // rendering so that creating an NSStatusItem doesn't race with the
  // window server.
  void (^scheduleInit)(void) = ^{
    dispatch_after(
      dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1.5 * NSEC_PER_SEC)),
      dispatch_get_main_queue(),
      ^{ _systrayInitBlock(); }
    );
  };

  dispatch_async(dispatch_get_main_queue(), ^{
    if ([NSApp isRunning]) {
      NSLog(@"[systray] NSApp already running, scheduling delayed init");
      scheduleInit();
    } else {
      NSLog(@"[systray] NSApp not yet running, observing launch notification");
      __block id observer = [[NSNotificationCenter defaultCenter]
        addObserverForName:NSApplicationDidFinishLaunchingNotification
                    object:nil
                     queue:[NSOperationQueue mainQueue]
                usingBlock:^(NSNotification *note) {
                  NSLog(@"[systray] didFinishLaunching fired, scheduling delayed init");
                  [[NSNotificationCenter defaultCenter] removeObserver:observer];
                  scheduleInit();
                }];
    }
  });
}

void runInMainThread(SEL method, id object) {
  if (owner == nil) {
    // Owner not yet initialized (systray not ready); silently skip.
    return;
  }
  [owner
    performSelectorOnMainThread:method
                     withObject:object
                  waitUntilDone: YES];
}

void setIcon(const char* iconBytes, int length, bool template) {
  NSData* buffer = [NSData dataWithBytes: iconBytes length:length];
  NSImage *image = [[NSImage alloc] initWithData:buffer];
  [image setSize:NSMakeSize(16, 16)];
  image.template = template;
  runInMainThread(@selector(setIcon:), (id)image);
}

void setMenuItemIcon(const char* iconBytes, int length, int menuId, bool template) {
  NSData* buffer = [NSData dataWithBytes: iconBytes length:length];
  NSImage *image = [[NSImage alloc] initWithData:buffer];
  [image setSize:NSMakeSize(16, 16)];
  image.template = template;
  NSNumber *mId = [NSNumber numberWithInt:menuId];
  runInMainThread(@selector(setMenuItemIcon:), @[image, (id)mId]);
}

void setTitle(char* ctitle) {
  NSString* title = [[NSString alloc] initWithCString:ctitle
                                             encoding:NSUTF8StringEncoding];
  free(ctitle);
  runInMainThread(@selector(setTitle:), (id)title);
}

void setTooltip(char* ctooltip) {
  NSString* tooltip = [[NSString alloc] initWithCString:ctooltip
                                               encoding:NSUTF8StringEncoding];
  free(ctooltip);
  runInMainThread(@selector(setTooltip:), (id)tooltip);
}

void add_or_update_menu_item(int menuId, int parentMenuId, char* title, char* tooltip, char* shortcutKey, short disabled, short checked, short isCheckable) {
  MenuItem* item = [[MenuItem alloc] initWithId: menuId withParentMenuId: parentMenuId withTitle: title withTooltip: tooltip withShortcutKey: shortcutKey withDisabled: disabled withChecked: checked];
  free(title);
  free(tooltip);
  runInMainThread(@selector(add_or_update_menu_item:), (id)item);
}

void add_separator(int menuId) {
  NSNumber *mId = [NSNumber numberWithInt:menuId];
  runInMainThread(@selector(add_separator:), (id)mId);
}

void hide_menu_item(int menuId) {
  NSNumber *mId = [NSNumber numberWithInt:menuId];
  runInMainThread(@selector(hide_menu_item:), (id)mId);
}

void show_menu_item(int menuId) {
  NSNumber *mId = [NSNumber numberWithInt:menuId];
  runInMainThread(@selector(show_menu_item:), (id)mId);
}

void reset_menu() {
  runInMainThread(@selector(reset_menu), nil);
}

void create_menu() {
  runInMainThread(@selector(create_menu), nil);
}

void set_menu_nil() {
  runInMainThread(@selector(set_menu_nil), nil);
}

void show_menu(){
  runInMainThread(@selector(show_menu), nil);
}

void enable_on_click(void) {
  runInMainThread(@selector(enable_on_click), nil);
}

void quit() {
  if (owner == nil) {
    return;
  }
  if (!internalLoop) {
    // External loop mode: just remove the status item, don't terminate the app.
    runInMainThread(@selector(quit), nil);
    return;
  }
  runInMainThread(@selector(quit), nil);
}
