//go:build linux

package main

/*
#cgo pkg-config: gtk+-3.0 gdk-3.0 cairo
#cgo LDFLAGS: -lm

#include <gtk/gtk.h>
#include <gdk/gdk.h>
#include <cairo/cairo.h>
#include <math.h>
#include <string.h>
#include <stdlib.h>

// ── State ───────────────────────────────────────────────────────────────────

static GtkWidget *floatingWin = NULL;
static cairo_surface_t *logoSurface = NULL;
static double haloPulse = 0.0;
static guint timerID = 0;

// Forward declarations.
static gboolean on_draw(GtkWidget *widget, cairo_t *cr, gpointer data);
static gboolean on_timer(gpointer data);

// ── PNG loading from memory ─────────────────────────────────────────────────

typedef struct {
	const unsigned char *data;
	unsigned int len;
	unsigned int pos;
} PngReadCtx;

static cairo_status_t png_read_func(void *closure, unsigned char *buf, unsigned int length) {
	PngReadCtx *ctx = (PngReadCtx *)closure;
	if (ctx->pos + length > ctx->len) return CAIRO_STATUS_READ_ERROR;
	memcpy(buf, ctx->data + ctx->pos, length);
	ctx->pos += length;
	return CAIRO_STATUS_SUCCESS;
}

// ── Create ──────────────────────────────────────────────────────────────────

static void createFloatingWindowGTK(int x, int y, int w, int h,
                                     const void *pngData, int pngLen) {
	if (floatingWin != NULL) return;
	if (!gtk_init_check(NULL, NULL)) return;

	// Load PNG logo from memory.
	PngReadCtx ctx = { (const unsigned char *)pngData, (unsigned int)pngLen, 0 };
	logoSurface = cairo_image_surface_create_from_png_stream(png_read_func, &ctx);

	floatingWin = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_keep_above(GTK_WINDOW(floatingWin), TRUE);
	gtk_window_set_decorated(GTK_WINDOW(floatingWin), FALSE);
	gtk_window_set_skip_taskbar_hint(GTK_WINDOW(floatingWin), TRUE);
	gtk_window_set_skip_pager_hint(GTK_WINDOW(floatingWin), TRUE);
	gtk_window_set_default_size(GTK_WINDOW(floatingWin), w, h);
	gtk_window_set_resizable(GTK_WINDOW(floatingWin), FALSE);
	gtk_window_move(GTK_WINDOW(floatingWin), x, y);
	gtk_widget_set_app_paintable(floatingWin, TRUE);

	// Enable RGBA visual for transparency.
	GdkScreen *screen = gtk_widget_get_screen(floatingWin);
	GdkVisual *visual = gdk_screen_get_rgba_visual(screen);
	if (visual != NULL) {
		gtk_widget_set_visual(floatingWin, visual);
	}

	g_signal_connect(floatingWin, "draw", G_CALLBACK(on_draw), NULL);

	// Animation timer (50ms = 20fps).
	timerID = g_timeout_add(50, on_timer, NULL);
}

// ── Draw callback ───────────────────────────────────────────────────────────

static gboolean on_draw(GtkWidget *widget, cairo_t *cr, gpointer data) {
	int w = gtk_widget_get_allocated_width(widget);
	int h = gtk_widget_get_allocated_height(widget);
	double cx = w / 2.0, cy = h / 2.0;
	double logoRadius = (w / 2.0) - 8.0;
	double glowOuter = w / 2.0;

	// Clear to transparent.
	cairo_set_operator(cr, CAIRO_OPERATOR_SOURCE);
	cairo_set_source_rgba(cr, 0, 0, 0, 0);
	cairo_paint(cr);
	cairo_set_operator(cr, CAIRO_OPERATOR_OVER);

	// Glow ring.
	double pulse = 0.5 + 0.5 * sin(haloPulse);
	double glowAlpha = 0.3 + 0.5 * pulse;
	cairo_save(cr);
	cairo_arc(cr, cx, cy, glowOuter, 0, 2 * M_PI);
	cairo_arc(cr, cx, cy, logoRadius, 0, 2 * M_PI);
	cairo_set_fill_rule(cr, CAIRO_FILL_RULE_EVEN_ODD);
	cairo_set_source_rgba(cr, 0.4, 0.55, 1.0, glowAlpha);
	cairo_fill(cr);
	cairo_restore(cr);

	// Light circular background.
	cairo_arc(cr, cx, cy, logoRadius, 0, 2 * M_PI);
	cairo_set_source_rgba(cr, 0.90, 0.92, 0.96, 0.96);
	cairo_fill(cr);

	// Logo (scaled to fit inside circle).
	if (logoSurface != NULL &&
	    cairo_surface_status(logoSurface) == CAIRO_STATUS_SUCCESS) {
		int imgW = cairo_image_surface_get_width(logoSurface);
		int imgH = cairo_image_surface_get_height(logoSurface);
		double scale = (logoRadius * 2.0) / (imgW > imgH ? imgW : imgH);
		double ox = cx - (imgW * scale) / 2.0;
		double oy = cy - (imgH * scale) / 2.0;

		cairo_save(cr);
		cairo_arc(cr, cx, cy, logoRadius, 0, 2 * M_PI);
		cairo_clip(cr);
		cairo_translate(cr, ox, oy);
		cairo_scale(cr, scale, scale);
		cairo_set_source_surface(cr, logoSurface, 0, 0);
		cairo_paint(cr);
		cairo_restore(cr);
	}

	return FALSE;
}

// ── Timer callback ──────────────────────────────────────────────────────────

static gboolean on_timer(gpointer data) {
	haloPulse += 0.15;
	if (haloPulse > 2 * M_PI) haloPulse -= 2 * M_PI;
	if (floatingWin != NULL && gtk_widget_get_visible(floatingWin)) {
		gtk_widget_queue_draw(floatingWin);
	}
	return TRUE; // keep timer running
}

// ── Show / Hide / Destroy / Move ────────────────────────────────────────────

static void showFloatingWindowGTK(void) {
	if (floatingWin != NULL) gtk_widget_show_all(floatingWin);
}

static void hideFloatingWindowGTK(void) {
	if (floatingWin != NULL) gtk_widget_hide(floatingWin);
}

static void destroyFloatingWindowGTK(void) {
	if (timerID > 0) { g_source_remove(timerID); timerID = 0; }
	if (floatingWin != NULL) { gtk_widget_destroy(floatingWin); floatingWin = NULL; }
	if (logoSurface != NULL) { cairo_surface_destroy(logoSurface); logoSurface = NULL; }
}

static void moveFloatingWindowGTK(int x, int y) {
	if (floatingWin != NULL) gtk_window_move(GTK_WINDOW(floatingWin), x, y);
}

static int linuxScreenWidth(void) {
	GdkDisplay *d = gdk_display_get_default();
	if (!d) return 1920;
	GdkMonitor *m = gdk_display_get_primary_monitor(d);
	if (!m) m = gdk_display_get_monitor(d, 0);
	if (!m) return 1920;
	GdkRectangle geom;
	gdk_monitor_get_geometry(m, &geom);
	return geom.width > 0 ? geom.width : 1920;
}

static int linuxScreenHeight(void) {
	GdkDisplay *d = gdk_display_get_default();
	if (!d) return 1080;
	GdkMonitor *m = gdk_display_get_primary_monitor(d);
	if (!m) m = gdk_display_get_monitor(d, 0);
	if (!m) return 1080;
	GdkRectangle geom;
	gdk_monitor_get_geometry(m, &geom);
	return geom.height > 0 ? geom.height : 1080;
}
*/
import "C"

import (
	_ "embed"
	"log"
	"sync"
	"unsafe"
)

//go:embed build/appicon.png
var floatingLogoPNGLinux []byte

// linuxFloatingWindow implements floatingWindow on Linux using native
// GTK3 + Cairo for the circular logo and pulsing halo glow.
type linuxFloatingWindow struct {
	app     *App
	created bool
	mu      sync.Mutex
}

func newFloatingWindow(app *App) floatingWindow {
	return &linuxFloatingWindow{app: app}
}

func init() {
	platformGetScreenWidth = func() int {
		w := int(C.linuxScreenWidth())
		if w <= 0 {
			return 1920
		}
		return w
	}
	platformGetScreenHeight = func() int {
		h := int(C.linuxScreenHeight())
		if h <= 0 {
			return 1080
		}
		return h
	}
}

func (w *linuxFloatingWindow) Create(x, y, width, height int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.created {
		return nil
	}

	pngPtr := unsafe.Pointer(&floatingLogoPNGLinux[0])
	pngLen := C.int(len(floatingLogoPNGLinux))
	C.createFloatingWindowGTK(C.int(x), C.int(y), C.int(width), C.int(height), pngPtr, pngLen)
	w.created = true
	log.Printf("[floating-window] Linux GTK window created: pos=(%d,%d) size=(%d,%d)", x, y, width, height)
	return nil
}

func (w *linuxFloatingWindow) Show() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.showFloatingWindowGTK()
}

func (w *linuxFloatingWindow) Hide() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.hideFloatingWindowGTK()
}

func (w *linuxFloatingWindow) Destroy() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.destroyFloatingWindowGTK()
	w.created = false
}

func (w *linuxFloatingWindow) MoveTo(x, y int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.moveFloatingWindowGTK(C.int(x), C.int(y))
}

func (w *linuxFloatingWindow) IsCreated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.created
}
