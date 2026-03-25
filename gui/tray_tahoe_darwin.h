#ifndef TRAY_TAHOE_DARWIN_H
#define TRAY_TAHOE_DARWIN_H

// Forward declarations for Go callbacks (defined in tray_tahoe_darwin.go).
extern void tahoeOnShowClicked(void);
extern void tahoeOnQuitClicked(void);

// C API called from Go.
void TahoeCreateTray(const void *iconData, int iconLen,
                     const char *tooltip,
                     const char *showLabel, const char *quitLabel);
void TahoeUpdateMenu(const char *tooltip,
                     const char *showLabel, const char *quitLabel);

// Bounce the dock icon to draw user attention.
void TahoeDockBounce(void);

#endif
