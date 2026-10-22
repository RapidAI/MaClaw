//go:build linux && cgo

package guiapp

/*
#cgo LDFLAGS: -ldl -lpthread
#include <dlfcn.h>
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

static void *open_sym(const char *lib, const char *name) {
	void *h = dlopen(lib, RTLD_LAZY | RTLD_LOCAL);
	if (h == NULL) return NULL;
	return dlsym(h, name);
}

typedef struct {
	pthread_mutex_t mu;
	pthread_cond_t cv;
	int done;
	int abandoned;
	uint8_t *png;
	int len;
} CompanionSnap;

typedef void (*snapshot_fn)(void *, int, int, void *, void (*)(void *, void *, void *), void *);
typedef void *(*finish_fn)(void *, void *, void **);
typedef int (*save_fn)(void *, char **, unsigned long *, const char *, char **, char **, void **);
typedef void (*drop_fn)(void *);

static snapshot_fn snap_start;
static finish_fn snap_finish;
static save_fn snap_save;
static drop_fn snap_unref;
static drop_fn snap_gfree;
static drop_fn snap_error_free;
static int snap_ready;

static int companion_snap_load(void) {
	if (snap_ready) return 0;
	snap_start = (snapshot_fn)open_sym("libwebkit2gtk-4.1.so.0", "webkit_web_view_get_snapshot");
	snap_finish = (finish_fn)open_sym("libwebkit2gtk-4.1.so.0", "webkit_web_view_get_snapshot_finish");
	if (snap_start == NULL || snap_finish == NULL) {
		snap_start = (snapshot_fn)open_sym("libwebkit2gtk-4.0.so.37", "webkit_web_view_get_snapshot");
		snap_finish = (finish_fn)open_sym("libwebkit2gtk-4.0.so.37", "webkit_web_view_get_snapshot_finish");
	}
	snap_save = (save_fn)open_sym("libgdk_pixbuf-2.0.so.0", "gdk_pixbuf_save_to_bufferv");
	snap_unref = (drop_fn)open_sym("libgobject-2.0.so.0", "g_object_unref");
	snap_gfree = (drop_fn)open_sym("libglib-2.0.so.0", "g_free");
	snap_error_free = (drop_fn)open_sym("libglib-2.0.so.0", "g_error_free");
	if (snap_start == NULL || snap_finish == NULL || snap_save == NULL || snap_unref == NULL || snap_gfree == NULL) {
		return -1;
	}
	snap_ready = 1;
	return 0;
}

// The finish callback runs on the GTK main loop. It must not wait for Go.
static void companion_snap_cb(void *source, void *result, void *user) {
	CompanionSnap *snap = (CompanionSnap *)user;
	uint8_t *png = NULL;
	int n = 0;
	void *err = NULL;
	void *pixbuf = snap_finish(source, result, &err);
	if (err != NULL && snap_error_free != NULL) snap_error_free(err);
	if (pixbuf != NULL) {
		char *buffer = NULL;
		unsigned long size = 0;
		void *saveErr = NULL;
		if (snap_save(pixbuf, &buffer, &size, "png", NULL, NULL, &saveErr) && buffer != NULL && size > 0 && size < 32ul * 1024ul * 1024ul) {
			png = malloc(size);
			if (png != NULL) {
				memcpy(png, buffer, size);
				n = (int)size;
			}
			snap_gfree(buffer);
		}
		if (saveErr != NULL && snap_error_free != NULL) snap_error_free(saveErr);
		snap_unref(pixbuf);
	}
	pthread_mutex_lock(&snap->mu);
	if (snap->abandoned) {
		pthread_mutex_unlock(&snap->mu);
		free(png);
		pthread_cond_destroy(&snap->cv);
		pthread_mutex_destroy(&snap->mu);
		free(snap);
		return;
	}
	snap->png = png;
	snap->len = n;
	snap->done = 1;
	pthread_cond_signal(&snap->cv);
	pthread_mutex_unlock(&snap->mu);
}

// companion_webkit_start must run on the GTK thread. Region 0 is the visible view.
void *companion_webkit_start(void *view) {
	if (view == NULL || companion_snap_load() != 0) return NULL;
	CompanionSnap *snap = calloc(1, sizeof(*snap));
	if (snap == NULL) return NULL;
	pthread_mutex_init(&snap->mu, NULL);
	pthread_cond_init(&snap->cv, NULL);
	snap_start(view, 0, 0, NULL, companion_snap_cb, snap);
	return snap;
}

int companion_webkit_wait(void *handle, uint8_t **out, int *outLen) {
	if (out != NULL) *out = NULL;
	if (outLen != NULL) *outLen = 0;
	CompanionSnap *snap = (CompanionSnap *)handle;
	if (snap == NULL) return -1;
	struct timespec ts;
	if (clock_gettime(CLOCK_REALTIME, &ts) != 0) return -1;
	ts.tv_sec += 8;
	pthread_mutex_lock(&snap->mu);
	while (!snap->done) {
		if (pthread_cond_timedwait(&snap->cv, &snap->mu, &ts) != 0) {
			snap->abandoned = 1;
			pthread_mutex_unlock(&snap->mu);
			return -1;
		}
	}
	int ok = -1;
	if (snap->png != NULL && snap->len > 0 && out != NULL && outLen != NULL) {
		*out = snap->png;
		*outLen = snap->len;
		snap->png = NULL;
		ok = 0;
	}
	pthread_mutex_unlock(&snap->mu);
	pthread_cond_destroy(&snap->cv);
	pthread_mutex_destroy(&snap->mu);
	free(snap->png);
	free(snap);
	return ok;
}
*/
import "C"
import (
	"bytes"
	"context"
	"errors"
	"image"
	"os"
	"os/exec"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/maclawcapture"
)

func captureCompanionWebViewPNG(ctx context.Context, rect image.Rectangle) ([]byte, error) {
	fileCompanionCaptureViewCalls.Add(1)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	view := maclawcapture.Current()
	if view == nil || view.Pointer() == 0 || !maclawcapture.HasUIRunner() {
		return nil, errCompanionWebViewUnbound
	}
	var snap uintptr
	ok := maclawcapture.PostUI(func() {
		snap = uintptr(C.companion_webkit_start(unsafe.Pointer(view.Pointer())))
	})
	if !ok || snap == 0 {
		return nil, errCompanionWebViewUnbound
	}
	var out *C.uint8_t
	var n C.int
	if C.companion_webkit_wait(unsafe.Pointer(snap), &out, &n) != 0 || out == nil || n <= 0 {
		if out != nil {
			C.free(unsafe.Pointer(out))
		}
		return nil, errCompanionWebViewUnbound
	}
	defer C.free(unsafe.Pointer(out))
	return cropCompanionPNG(C.GoBytes(unsafe.Pointer(out), n), rect)
}

func captureProcessAudio(ctx context.Context) ([]byte, bool) {
	fileCompanionCaptureAudioCalls.Add(1)
	act := companionProcessAudioActivate(uint32(os.Getpid()))
	if act.StreamFlags&audclntStreamFlagsLoopback != 0 || !act.IncludeProcessTree || act.TargetPID == 0 {
		return nil, true
	}
	if ctx.Err() != nil || !companionCaptureViewBound() {
		return nil, true
	}
	// PipeWire only. A PulseAudio sink monitor would record every process.
	wav, err := recordPipeWirePID(ctx, int(act.TargetPID))
	if err != nil || len(wav) < 44 {
		return nil, true
	}
	return wav, false
}

func capturePlaybackEnded(context.Context) bool { return false }

// recordPipeWirePID runs pw-record against the output node whose
// application.process.id is this process. It does not open a sink monitor.
func recordPipeWirePID(ctx context.Context, pid int) ([]byte, error) {
	if pid <= 0 {
		return nil, errors.New("no pid")
	}
	dumpBin, err := exec.LookPath("pw-dump")
	if err != nil {
		return nil, err
	}
	recordBin, err := exec.LookPath("pw-record")
	if err != nil {
		return nil, err
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probe, dumpBin).Output()
	if err != nil {
		return nil, err
	}
	target := pipeWireOutputNode(out, pid)
	if target == "" {
		return nil, errors.New("no pipewire output node for this process")
	}
	file, err := os.CreateTemp("", "maclaw-pw-*.wav")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	_ = file.Close()
	defer os.Remove(path)
	cmd := exec.CommandContext(ctx, recordBin, "--target", target, "--rate", "16000", "--channels", "1", "--format", "s16", path)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	_ = cmd.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 44 || !bytes.HasPrefix(data, []byte("RIFF")) {
		return nil, errors.New("pipewire record was empty")
	}
	return data, nil
}
