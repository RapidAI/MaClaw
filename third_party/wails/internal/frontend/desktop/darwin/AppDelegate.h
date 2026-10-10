//
//  AppDelegate.h
//  test
//
//  Created by Lea Anthony on 10/10/21.
//

#ifndef AppDelegate_h
#define AppDelegate_h

#import <Cocoa/Cocoa.h>
#import "WailsContext.h"

@interface AppDelegate : NSResponder <NSApplicationDelegate, NSTouchBarProvider>

@property bool alwaysOnTop;
@property bool startHidden;
@property (retain) NSString* singleInstanceUniqueId;
@property bool singleInstanceLockEnabled;
@property bool startFullscreen;
@property (retain) WailsWindow* mainWindow;

@end

extern void HandleOpenFile(char *);

// HandleLaunchFileBatchReady only releases the Go wait in startFileOpenProcessor.
// It does not start the host app and it does not read the host path queue.
extern void HandleLaunchFileBatchReady(void);

extern void HandleSecondInstanceData(char * message);

void SendDataToFirstInstance(char * singleInstanceUniqueId, char * text);

char* GetMacOsNativeTempDir();

#endif /* AppDelegate_h */
