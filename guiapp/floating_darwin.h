#ifndef FLOATING_DARWIN_H
#define FLOATING_DARWIN_H

// Go callbacks (defined in floating_darwin.go via //export).
extern void maclawDarwinPetClicked(void);
extern void maclawDarwinPetDragged(int x, int y);
extern void maclawDarwinPetMenu(int action);

// C API called from Go.
int  createFloatingWindowDarwin(int x, int y, int w, int h, const void *pngData, int pngLen);
void showFloatingWindowDarwin(void);
void hideFloatingWindowDarwin(void);
void destroyFloatingWindowDarwin(void);
void moveFloatingWindowDarwin(int x, int y);
void setPetImageDarwin(const void *pngData, int pngLen);
int darwinScreenWidth(void);
int darwinScreenHeight(void);
int darwinBackingScale(void);

#endif
