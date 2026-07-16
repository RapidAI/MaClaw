#ifndef TRAY_TAHOE_DARWIN_H
#define TRAY_TAHOE_DARWIN_H

// Forward declarations for Go callbacks (defined in tray_tahoe_darwin.go).
extern void tahoeOnShowClicked(void);
extern void tahoeOnQuitClicked(void);
extern void tahoeOnCUPauseClicked(void);
extern void tahoeOnCUResumeClicked(void);
extern void tahoeOnCUStopClicked(void);
extern void tahoeOnCUResetClicked(void);

// C API called from Go.
void TahoeCreateTray(const void *iconData, int iconLen,
                     const char *tooltip,
                     const char *showLabel, const char *quitLabel);
void TahoeUpdateMenu(const char *tooltip,
                     const char *showLabel, const char *quitLabel);

// Computer Use submenu: labels + enable flags (1=enabled, 0=disabled).
void TahoeUpdateComputerUseMenu(const char *menuTitle,
                                const char *statusLabel,
                                const char *pauseLabel, int pauseEnabled,
                                const char *resumeLabel, int resumeEnabled,
                                const char *stopLabel, int stopEnabled,
                                const char *resetLabel, int resetEnabled);

// Bounce the dock icon to draw user attention.
void TahoeDockBounce(void);

#endif
