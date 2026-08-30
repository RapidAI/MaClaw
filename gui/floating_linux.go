//go:build linux && cgo

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
static gboolean useAlpha = FALSE;
static int winX = 0, winY = 0, winW = 104, winH = 104;
static gboolean dragging = FALSE;
static gboolean dragMoved = FALSE;
static int dragOffX = 0, dragOffY = 0;

// Forward declarations.
static gboolean on_draw(GtkWidget *widget, cairo_t *cr, gpointer data);
static gboolean on_timer(gpointer data);
static gboolean on_button_press(GtkWidget *widget, GdkEventButton *e, gpointer data);
static gboolean on_button_release(GtkWidget *widget, GdkEventButton *e, gpointer data);
static gboolean on_motion(GtkWidget *widget, GdkEventMotion *e, gpointer data);
static void on_realize(GtkWidget *widget, gpointer data);
static void apply_circle_shape(GtkWidget *widget);
static void clamp_to_workarea(int *x, int *y, int w, int h);
static cairo_surface_t *surface_from_png(const void *data, int len);

extern void maclawLinuxPetClicked(void);
extern void maclawLinuxPetDragged(int x, int y);
extern void maclawLinuxPetMenu(int action);

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

static cairo_surface_t *surface_from_png(const void *data, int len) {
	if (data == NULL || len <= 0) return NULL;
	PngReadCtx ctx = { (const unsigned char *)data, (unsigned int)len, 0 };
	cairo_surface_t *s = cairo_image_surface_create_from_png_stream(png_read_func, &ctx);
	if (s == NULL || cairo_surface_status(s) != CAIRO_STATUS_SUCCESS) {
		if (s != NULL) cairo_surface_destroy(s);
		return NULL;
	}
	return s;
}

// ── Thread marshal ──────────────────────────────────────────────────────────
// Wails already owns the GTK main loop. Startup / config refresh create the
// pet from a goroutine; GTK widgets created off the GUI thread never map.
// g_main_context_invoke runs immediately when no loop owns the default
// context (unit tests) and otherwise wakes the GTK thread.

typedef gboolean (*GtkIdleFn)(gpointer);

typedef struct {
	GMutex mu;
	GCond cond;
	gboolean done;
	gboolean result;
	GtkIdleFn fn;
	gpointer data;
} GtkSyncCall;

static gboolean gtk_sync_idle(gpointer user) {
	GtkSyncCall *call = (GtkSyncCall *)user;
	gboolean result = FALSE;
	if (call->fn != NULL) {
		result = call->fn(call->data);
	}
	g_mutex_lock(&call->mu);
	call->result = result;
	call->done = TRUE;
	g_cond_signal(&call->cond);
	g_mutex_unlock(&call->mu);
	return G_SOURCE_REMOVE;
}

static gboolean gtk_call_sync(GtkIdleFn fn, gpointer data) {
	if (fn == NULL) return FALSE;
	if (!gtk_init_check(NULL, NULL)) return FALSE;

	GMainContext *ctx = g_main_context_default();
	if (g_main_context_is_owner(ctx)) {
		return fn(data);
	}

	GtkSyncCall call;
	memset(&call, 0, sizeof(call));
	g_mutex_init(&call.mu);
	g_cond_init(&call.cond);
	call.fn = fn;
	call.data = data;

	g_main_context_invoke(ctx, gtk_sync_idle, &call);

	g_mutex_lock(&call.mu);
	gint64 deadline = g_get_monotonic_time() + 8 * G_TIME_SPAN_SECOND;
	while (!call.done) {
		if (!g_cond_wait_until(&call.cond, &call.mu, deadline)) {
			break;
		}
	}
	gboolean ok = call.done && call.result;
	g_mutex_unlock(&call.mu);
	g_mutex_clear(&call.mu);
	g_cond_clear(&call.cond);
	return ok;
}

static void fill_workarea(GdkRectangle *geom) {
	geom->x = 0;
	geom->y = 0;
	geom->width = 1920;
	geom->height = 1080;
	GdkDisplay *d = gdk_display_get_default();
	if (!d) return;
	GdkMonitor *m = gdk_display_get_primary_monitor(d);
	if (!m) m = gdk_display_get_monitor(d, 0);
	if (!m) return;
	gdk_monitor_get_workarea(m, geom);
	if (geom->width <= 0) geom->width = 1920;
	if (geom->height <= 0) geom->height = 1080;
}

static void clamp_to_workarea(int *x, int *y, int w, int h) {
	GdkRectangle wa;
	fill_workarea(&wa);
	if (w < 1) w = 1;
	if (h < 1) h = 1;
	if (*x + w > wa.x + wa.width) *x = wa.x + wa.width - w;
	if (*y + h > wa.y + wa.height) *y = wa.y + wa.height - h;
	if (*x < wa.x) *x = wa.x;
	if (*y < wa.y) *y = wa.y;
}

static void apply_circle_shape(GtkWidget *widget) {
	if (widget == NULL || !gtk_widget_get_realized(widget)) return;
	GdkWindow *gw = gtk_widget_get_window(widget);
	if (gw == NULL) return;
	int w = gtk_widget_get_allocated_width(widget);
	int h = gtk_widget_get_allocated_height(widget);
	if (w < 2 || h < 2) {
		w = winW;
		h = winH;
	}
	cairo_surface_t *surface = cairo_image_surface_create(CAIRO_FORMAT_A8, w, h);
	cairo_t *cr = cairo_create(surface);
	cairo_set_source_rgba(cr, 0, 0, 0, 0);
	cairo_paint(cr);
	cairo_set_source_rgba(cr, 1, 1, 1, 1);
	double r = (w < h ? w : h) / 2.0 - 1.0;
	if (r < 1) r = 1;
	cairo_arc(cr, w / 2.0, h / 2.0, r, 0, 2 * M_PI);
	cairo_fill(cr);
	cairo_region_t *region = gdk_cairo_region_create_from_surface(surface);
	gdk_window_shape_combine_region(gw, region, 0, 0);
	cairo_region_destroy(region);
	cairo_destroy(cr);
	cairo_surface_destroy(surface);
}

// ── Create / show / hide (GTK thread only) ──────────────────────────────────

typedef struct {
	int x, y, w, h;
	const void *png;
	int pngLen;
} CreateArgs;

static gboolean createFloatingWindowGTK_on_thread(gpointer data) {
	CreateArgs *a = (CreateArgs *)data;
	if (floatingWin != NULL) return TRUE;
	if (!gtk_init_check(NULL, NULL)) return FALSE;

	if (logoSurface != NULL) {
		cairo_surface_destroy(logoSurface);
		logoSurface = NULL;
	}
	logoSurface = surface_from_png(a->png, a->pngLen);

	winX = a->x;
	winY = a->y;
	winW = a->w > 0 ? a->w : 104;
	winH = a->h > 0 ? a->h : 104;
	clamp_to_workarea(&winX, &winY, winW, winH);

	floatingWin = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_type_hint(GTK_WINDOW(floatingWin), GDK_WINDOW_TYPE_HINT_UTILITY);
	gtk_window_set_keep_above(GTK_WINDOW(floatingWin), TRUE);
	gtk_window_set_decorated(GTK_WINDOW(floatingWin), FALSE);
	gtk_window_set_skip_taskbar_hint(GTK_WINDOW(floatingWin), TRUE);
	gtk_window_set_skip_pager_hint(GTK_WINDOW(floatingWin), TRUE);
	gtk_window_set_accept_focus(GTK_WINDOW(floatingWin), FALSE);
	gtk_window_set_resizable(GTK_WINDOW(floatingWin), FALSE);
	gtk_widget_set_size_request(floatingWin, winW, winH);
	gtk_window_set_default_size(GTK_WINDOW(floatingWin), winW, winH);
	gtk_window_resize(GTK_WINDOW(floatingWin), winW, winH);
	gtk_window_move(GTK_WINDOW(floatingWin), winX, winY);

	// RGBA + app-paintable is invisible on XFCE/remote desktops without a
	// compositor. Only use alpha when the screen is actually composited.
	GdkScreen *screen = gtk_widget_get_screen(floatingWin);
	gboolean composited = screen != NULL && gdk_screen_is_composited(screen);
	GdkVisual *rgba = screen != NULL ? gdk_screen_get_rgba_visual(screen) : NULL;
	useAlpha = composited && rgba != NULL;
	if (useAlpha) {
		gtk_widget_set_visual(floatingWin, rgba);
	}
	gtk_widget_set_app_paintable(floatingWin, TRUE);

	g_signal_connect(floatingWin, "draw", G_CALLBACK(on_draw), NULL);
	g_signal_connect(floatingWin, "realize", G_CALLBACK(on_realize), NULL);
	g_signal_connect(floatingWin, "button-press-event", G_CALLBACK(on_button_press), NULL);
	g_signal_connect(floatingWin, "button-release-event", G_CALLBACK(on_button_release), NULL);
	g_signal_connect(floatingWin, "motion-notify-event", G_CALLBACK(on_motion), NULL);
	gtk_widget_add_events(floatingWin,
		GDK_BUTTON_PRESS_MASK | GDK_BUTTON_RELEASE_MASK |
		GDK_BUTTON_MOTION_MASK | GDK_POINTER_MOTION_MASK);

	if (timerID == 0) {
		timerID = g_timeout_add(50, on_timer, NULL);
	}
	g_message("[floating-window] linux gtk create pos=(%d,%d) size=(%d,%d) composited=%d rgba=%d useAlpha=%d",
		winX, winY, winW, winH, composited, rgba != NULL, useAlpha);
	return TRUE;
}

static gboolean showFloatingWindowGTK_on_thread(gpointer data) {
	(void)data;
	if (floatingWin == NULL) return FALSE;
	gtk_widget_show_all(floatingWin);
	gtk_window_resize(GTK_WINDOW(floatingWin), winW, winH);
	gtk_window_move(GTK_WINDOW(floatingWin), winX, winY);
	gtk_window_set_keep_above(GTK_WINDOW(floatingWin), TRUE);
	gtk_window_present(GTK_WINDOW(floatingWin));
	if (!useAlpha) {
		apply_circle_shape(floatingWin);
	}
	return gtk_widget_get_visible(floatingWin);
}

static gboolean hideFloatingWindowGTK_on_thread(gpointer data) {
	(void)data;
	if (floatingWin != NULL) gtk_widget_hide(floatingWin);
	return TRUE;
}

static gboolean destroyFloatingWindowGTK_on_thread(gpointer data) {
	(void)data;
	if (timerID > 0) { g_source_remove(timerID); timerID = 0; }
	if (floatingWin != NULL) { gtk_widget_destroy(floatingWin); floatingWin = NULL; }
	if (logoSurface != NULL) { cairo_surface_destroy(logoSurface); logoSurface = NULL; }
	dragging = FALSE;
	return TRUE;
}

typedef struct { int x, y; } MoveArgs;

static gboolean moveFloatingWindowGTK_on_thread(gpointer data) {
	MoveArgs *a = (MoveArgs *)data;
	winX = a->x;
	winY = a->y;
	clamp_to_workarea(&winX, &winY, winW, winH);
	if (floatingWin != NULL) gtk_window_move(GTK_WINDOW(floatingWin), winX, winY);
	return TRUE;
}

typedef struct { const void *png; int pngLen; } ImageArgs;

static gboolean setPetImageGTK_on_thread(gpointer data) {
	ImageArgs *a = (ImageArgs *)data;
	cairo_surface_t *next = surface_from_png(a->png, a->pngLen);
	if (next == NULL) return FALSE;
	if (logoSurface != NULL) cairo_surface_destroy(logoSurface);
	logoSurface = next;
	if (floatingWin != NULL) gtk_widget_queue_draw(floatingWin);
	return TRUE;
}

static gboolean linuxFloatingIsMapped_on_thread(gpointer data) {
	(void)data;
	return floatingWin != NULL && gtk_widget_get_mapped(floatingWin);
}

static gboolean linuxFloatingIsVisible_on_thread(gpointer data) {
	(void)data;
	return floatingWin != NULL && gtk_widget_get_visible(floatingWin);
}

static gboolean linuxFloatingUsesAlpha_on_thread(gpointer data) {
	(void)data;
	return useAlpha;
}

static void on_realize(GtkWidget *widget, gpointer data) {
	(void)data;
	if (!useAlpha) apply_circle_shape(widget);
}

// ── Draw callback ───────────────────────────────────────────────────────────

static gboolean on_draw(GtkWidget *widget, cairo_t *cr, gpointer data) {
	(void)data;
	int w = gtk_widget_get_allocated_width(widget);
	int h = gtk_widget_get_allocated_height(widget);
	if (w < 2) w = winW;
	if (h < 2) h = winH;
	double cx = w / 2.0, cy = h / 2.0;
	double logoRadius = (w / 2.0) - 8.0;
	if (logoRadius < 8) logoRadius = (w < h ? w : h) / 2.0 - 2.0;
	double glowOuter = w / 2.0;

	cairo_set_operator(cr, CAIRO_OPERATOR_SOURCE);
	if (useAlpha) {
		cairo_set_source_rgba(cr, 0, 0, 0, 0);
	} else {
		// Opaque fill so a non-composited remote/XFCE session cannot go blank.
		cairo_set_source_rgb(cr, 0.90, 0.92, 0.96);
	}
	cairo_paint(cr);
	cairo_set_operator(cr, CAIRO_OPERATOR_OVER);

	if (useAlpha) {
		double pulse = 0.5 + 0.5 * sin(haloPulse);
		double glowAlpha = 0.3 + 0.5 * pulse;
		cairo_save(cr);
		cairo_arc(cr, cx, cy, glowOuter, 0, 2 * M_PI);
		cairo_arc(cr, cx, cy, logoRadius, 0, 2 * M_PI);
		cairo_set_fill_rule(cr, CAIRO_FILL_RULE_EVEN_ODD);
		cairo_set_source_rgba(cr, 0.4, 0.55, 1.0, glowAlpha);
		cairo_fill(cr);
		cairo_restore(cr);
	}

	cairo_arc(cr, cx, cy, logoRadius, 0, 2 * M_PI);
	cairo_set_source_rgba(cr, 0.90, 0.92, 0.96, useAlpha ? 0.96 : 1.0);
	cairo_fill(cr);

	if (logoSurface != NULL &&
	    cairo_surface_status(logoSurface) == CAIRO_STATUS_SUCCESS) {
		int imgW = cairo_image_surface_get_width(logoSurface);
		int imgH = cairo_image_surface_get_height(logoSurface);
		if (imgW > 0 && imgH > 0) {
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
	}

	return FALSE;
}

static gboolean on_timer(gpointer data) {
	(void)data;
	haloPulse += 0.15;
	if (haloPulse > 2 * M_PI) haloPulse -= 2 * M_PI;
	if (floatingWin != NULL && gtk_widget_get_visible(floatingWin)) {
		gtk_widget_queue_draw(floatingWin);
	}
	return TRUE;
}

// ── Input ───────────────────────────────────────────────────────────────────

static void on_menu_settings(GtkMenuItem *item, gpointer data) {
	(void)item; (void)data;
	maclawLinuxPetMenu(1);
}

static void on_menu_hide(GtkMenuItem *item, gpointer data) {
	(void)item; (void)data;
	maclawLinuxPetMenu(2);
}

static void on_menu_quit(GtkMenuItem *item, gpointer data) {
	(void)item; (void)data;
	maclawLinuxPetMenu(3);
}

static void popup_pet_menu(GdkEventButton *e) {
	GtkWidget *menu = gtk_menu_new();
	GtkWidget *settings = gtk_menu_item_new_with_label("设置");
	GtkWidget *hide = gtk_menu_item_new_with_label("隐藏");
	GtkWidget *quit = gtk_menu_item_new_with_label("退出");
	gtk_menu_shell_append(GTK_MENU_SHELL(menu), settings);
	gtk_menu_shell_append(GTK_MENU_SHELL(menu), hide);
	gtk_menu_shell_append(GTK_MENU_SHELL(menu), quit);
	g_signal_connect(settings, "activate", G_CALLBACK(on_menu_settings), NULL);
	g_signal_connect(hide, "activate", G_CALLBACK(on_menu_hide), NULL);
	g_signal_connect(quit, "activate", G_CALLBACK(on_menu_quit), NULL);
	gtk_widget_show_all(menu);
	gtk_menu_popup_at_pointer(GTK_MENU(menu), (GdkEvent *)e);
}

static gboolean on_button_press(GtkWidget *widget, GdkEventButton *e, gpointer data) {
	(void)widget; (void)data;
	if (e->button == 3) {
		popup_pet_menu(e);
		return TRUE;
	}
	if (e->button != 1) return FALSE;
	dragging = TRUE;
	dragMoved = FALSE;
	int wx = 0, wy = 0;
	if (floatingWin != NULL) gtk_window_get_position(GTK_WINDOW(floatingWin), &wx, &wy);
	dragOffX = (int)e->x_root - wx;
	dragOffY = (int)e->y_root - wy;
	return TRUE;
}

static gboolean on_motion(GtkWidget *widget, GdkEventMotion *e, gpointer data) {
	(void)widget; (void)data;
	if (!dragging) return FALSE;
	int nx = (int)e->x_root - dragOffX;
	int ny = (int)e->y_root - dragOffY;
	int wx = winX, wy = winY;
	if (floatingWin != NULL) gtk_window_get_position(GTK_WINDOW(floatingWin), &wx, &wy);
	if (!dragMoved && (abs(nx - wx) > 5 || abs(ny - wy) > 5)) {
		dragMoved = TRUE;
	}
	winX = nx;
	winY = ny;
	clamp_to_workarea(&winX, &winY, winW, winH);
	if (floatingWin != NULL) gtk_window_move(GTK_WINDOW(floatingWin), winX, winY);
	return TRUE;
}

static gboolean on_button_release(GtkWidget *widget, GdkEventButton *e, gpointer data) {
	(void)widget; (void)data;
	if (e->button != 1) return FALSE;
	dragging = FALSE;
	if (!dragMoved) {
		maclawLinuxPetClicked();
	} else {
		maclawLinuxPetDragged(winX, winY);
	}
	return TRUE;
}

// ── Public C entry points (called from Go; marshal to GTK thread) ───────────

static gboolean createFloatingWindowGTK(int x, int y, int w, int h,
                                     const void *pngData, int pngLen) {
	CreateArgs args = { x, y, w, h, pngData, pngLen };
	return gtk_call_sync(createFloatingWindowGTK_on_thread, &args);
}

static void showFloatingWindowGTK(void) {
	gtk_call_sync(showFloatingWindowGTK_on_thread, NULL);
}

static void hideFloatingWindowGTK(void) {
	gtk_call_sync(hideFloatingWindowGTK_on_thread, NULL);
}

static void destroyFloatingWindowGTK(void) {
	gtk_call_sync(destroyFloatingWindowGTK_on_thread, NULL);
}

static void moveFloatingWindowGTK(int x, int y) {
	MoveArgs args = { x, y };
	gtk_call_sync(moveFloatingWindowGTK_on_thread, &args);
}

static gboolean setPetImageGTK(const void *pngData, int pngLen) {
	ImageArgs args = { pngData, pngLen };
	return gtk_call_sync(setPetImageGTK_on_thread, &args);
}

static int linuxScreenWidth(void) {
	gtk_init_check(NULL, NULL);
	GdkRectangle wa;
	fill_workarea(&wa);
	return wa.width > 0 ? wa.width : 1920;
}

static int linuxScreenHeight(void) {
	gtk_init_check(NULL, NULL);
	GdkRectangle wa;
	fill_workarea(&wa);
	return wa.height > 0 ? wa.height : 1080;
}

static int linuxFloatingIsMapped(void) {
	return gtk_call_sync(linuxFloatingIsMapped_on_thread, NULL) ? 1 : 0;
}

static int linuxFloatingIsVisible(void) {
	return gtk_call_sync(linuxFloatingIsVisible_on_thread, NULL) ? 1 : 0;
}

static int linuxFloatingUsesAlpha(void) {
	return gtk_call_sync(linuxFloatingUsesAlpha_on_thread, NULL) ? 1 : 0;
}

static void linuxPumpGTK(int ms) {
	if (!gtk_init_check(NULL, NULL)) return;
	if (ms < 0) ms = 0;
	gint64 end = g_get_monotonic_time() + (gint64)ms * 1000;
	GMainContext *ctx = g_main_context_default();
	do {
		g_main_context_iteration(ctx, FALSE);
	} while (g_get_monotonic_time() < end);
}

typedef struct { const char *path; } SaveArgs;

static gboolean linuxSaveWindowPNG_on_thread(gpointer data) {
	SaveArgs *a = (SaveArgs *)data;
	if (floatingWin == NULL || a == NULL || a->path == NULL) return FALSE;
	GdkWindow *gw = gtk_widget_get_window(floatingWin);
	if (gw == NULL) return FALSE;
	int w = gdk_window_get_width(gw);
	int h = gdk_window_get_height(gw);
	if (w < 1 || h < 1) return FALSE;
	GdkPixbuf *pb = gdk_pixbuf_get_from_window(gw, 0, 0, w, h);
	if (pb == NULL) return FALSE;
	GError *err = NULL;
	gboolean ok = gdk_pixbuf_save(pb, a->path, "png", &err, NULL);
	if (err != NULL) g_error_free(err);
	g_object_unref(pb);
	return ok;
}

static int linuxSaveWindowPNG(const char *path) {
	SaveArgs args = { path };
	return gtk_call_sync(linuxSaveWindowPNG_on_thread, &args) ? 1 : 0;
}
*/
import "C"

import (
	_ "embed"
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/gui/petpack"
)

//go:embed build/appicon.png
var floatingLogoPNGLinux []byte

// linuxFloatingWindow implements floatingWindow on Linux using native
// GTK3 + Cairo. The window is created/shown on the GTK thread so it actually
// maps. Pack stills are preferred; the embedded logo is the last-resort face.
//
// Remaining stubs (not ported in this change):
//   - 20fps face/skeleton/character animation (Windows render loop)
//   - motion amplitude / interaction-mode posing
//   - pet motion sound
// Runtime states still swap the matching pack still (or keep the logo).
type linuxFloatingWindow struct {
	app              *App
	created          bool
	mu               sync.Mutex
	size             int
	petSkin          string
	petVariant       string
	petRuntimeState  string
	petStateDeadline time.Time
	packFrameCache   *petpack.FrameCache
}

func newFloatingWindow(app *App) floatingWindow {
	return &linuxFloatingWindow{app: app}
}

var globalLinuxFloating struct {
	sync.RWMutex
	window *linuxFloatingWindow
}

func setGlobalLinuxFloating(w *linuxFloatingWindow) {
	globalLinuxFloating.Lock()
	globalLinuxFloating.window = w
	globalLinuxFloating.Unlock()
}

func clearGlobalLinuxFloating(w *linuxFloatingWindow) {
	globalLinuxFloating.Lock()
	if globalLinuxFloating.window == w {
		globalLinuxFloating.window = nil
	}
	globalLinuxFloating.Unlock()
}

func currentLinuxFloating() *linuxFloatingWindow {
	globalLinuxFloating.RLock()
	w := globalLinuxFloating.window
	globalLinuxFloating.RUnlock()
	return w
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

func (w *linuxFloatingWindow) petPNGBytesLocked() []byte {
	sz := w.size
	if sz < 56 {
		sz = defaultPetSize
	}
	if frame := tryLoadPackFrame(w.petSkin, w.petVariant, w.petRuntimeState, sz, w.packFrameCache); frame != nil {
		if b := encodeNRGBAToPNG(frame); len(b) > 0 {
			return b
		}
	}
	if len(floatingLogoPNGLinux) > 0 {
		return floatingLogoPNGLinux
	}
	return nil
}

func (w *linuxFloatingWindow) applyPetImageLocked() {
	png := w.petPNGBytesLocked()
	if len(png) == 0 || !w.created {
		return
	}
	C.setPetImageGTK(unsafe.Pointer(&png[0]), C.int(len(png)))
}

func (w *linuxFloatingWindow) Create(x, y, width, height int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.created {
		return nil
	}

	w.size = width
	w.petSkin = petpack.DefaultPackID
	w.petVariant = petpack.VariantDefault
	w.petRuntimeState = string(petpack.StateIdle)
	w.petStateDeadline = time.Time{}
	w.packFrameCache = petpack.NewFrameCache()
	if w.app != nil {
		if cfg, err := w.app.LoadConfig(); err == nil {
			if cfg.PetSkin != "" {
				w.petSkin = cfg.PetSkin
			}
			w.petVariant = petpack.ResolveVariantForRuntime(cfg.PetVariant)
		}
	}

	png := w.petPNGBytesLocked()
	if len(png) == 0 {
		return fmt.Errorf("linux pet: no pack frame or logo PNG to paint")
	}
	ok := C.createFloatingWindowGTK(C.int(x), C.int(y), C.int(width), C.int(height), unsafe.Pointer(&png[0]), C.int(len(png)))
	if ok == 0 {
		return fmt.Errorf("linux pet: GTK window was not created (gtk_init/main-loop marshal failed)")
	}
	w.created = true
	setGlobalLinuxFloating(w)
	log.Printf("[floating-window] Linux GTK window created: pos=(%d,%d) size=(%d,%d) skin=%s", x, y, width, height, w.petSkin)
	return nil
}

func (w *linuxFloatingWindow) Show() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.showFloatingWindowGTK()
	log.Printf("[floating-window] Linux GTK window shown mapped=%d visible=%d alpha=%d",
		int(C.linuxFloatingIsMapped()), int(C.linuxFloatingIsVisible()), int(C.linuxFloatingUsesAlpha()))
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
	clearGlobalLinuxFloating(w)
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

// Pet sound and motion posing remain stubs on Linux. Skin/variant changes
// still reload the visible still so a settings switch is not a blank window.
func (w *linuxFloatingWindow) UpdateSoundConfig(soundEnabled bool, preset string) {
}

func (w *linuxFloatingWindow) UpdateMotionConfig(motionEnabled, quiet, reducedMotion bool, interactionMode, skin, variant string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	changed := false
	if skin != "" && skin != w.petSkin {
		w.petSkin = skin
		changed = true
	}
	if variant != "" {
		resolved := petpack.ResolveVariantForRuntime(variant)
		if resolved != w.petVariant {
			w.petVariant = resolved
			changed = true
		}
	}
	if changed {
		w.packFrameCache = petpack.NewFrameCache()
		w.applyPetImageLocked()
	}
}

func (w *linuxFloatingWindow) InvalidatePetPackAssets() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.packFrameCache = petpack.NewFrameCache()
	w.applyPetImageLocked()
}

func (w *linuxFloatingWindow) SetPetRuntimeState(state string, ttlMs int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.petRuntimeState = string(petpack.NormalizeState(state))
	if ttlMs > 0 {
		w.petStateDeadline = time.Now().Add(time.Duration(ttlMs) * time.Millisecond)
	} else {
		w.petStateDeadline = time.Time{}
	}
	w.applyPetImageLocked()
}

func (w *linuxFloatingWindow) CurrentPetRuntimeState() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.petStateDeadline.IsZero() && time.Now().After(w.petStateDeadline) {
		w.petRuntimeState = string(petpack.StateIdle)
		w.petStateDeadline = time.Time{}
		w.applyPetImageLocked()
	}
	if w.petRuntimeState == "" {
		return string(petpack.StateIdle)
	}
	return w.petRuntimeState
}

// PetPackRuntimeLevel reports the stub reality: this platform shows a pack
// still (or logo) and does not run the Windows animation loop.
func (w *linuxFloatingWindow) PetPackRuntimeLevel(declared string) (string, string) {
	if declared == petpack.RendererNative {
		return declared, ""
	}
	return petpack.RendererNative, "当前平台暂不支持宠物动画，仅显示静态图像"
}

func linuxPumpGTKMs(ms int) {
	C.linuxPumpGTK(C.int(ms))
}

func linuxFloatingMapped() bool {
	return C.linuxFloatingIsMapped() != 0
}

func linuxFloatingVisible() bool {
	return C.linuxFloatingIsVisible() != 0
}

func linuxFloatingUsesAlpha() bool {
	return C.linuxFloatingUsesAlpha() != 0
}

func linuxSaveWindowPNG(path string) bool {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	return C.linuxSaveWindowPNG(cpath) != 0
}

//export maclawLinuxPetClicked
func maclawLinuxPetClicked() {
	w := currentLinuxFloating()
	if w == nil || w.app == nil {
		return
	}
	go w.app.onFloatingButtonClicked()
}

//export maclawLinuxPetDragged
func maclawLinuxPetDragged(x, y C.int) {
	w := currentLinuxFloating()
	if w == nil || w.app == nil {
		return
	}
	go w.app.onFloatingButtonDragged(int(x), int(y))
}

//export maclawLinuxPetMenu
func maclawLinuxPetMenu(action C.int) {
	w := currentLinuxFloating()
	if w == nil || w.app == nil {
		return
	}
	switch int(action) {
	case 1:
		go w.app.openPetSettingsFromMenu()
	case 2:
		go w.app.DisablePetFromMenu()
	case 3:
		go w.app.QuitApp()
	}
}
