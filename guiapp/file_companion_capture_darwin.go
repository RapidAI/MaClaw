//go:build darwin

package guiapp

/*
#cgo LDFLAGS: -framework Foundation -framework WebKit -framework AppKit -framework CoreAudio
#cgo CFLAGS: -x objective-c
#import <Foundation/Foundation.h>
#import <WebKit/WebKit.h>
#import <AppKit/AppKit.h>
#import <CoreAudio/CoreAudio.h>
#import <objc/message.h>
#import <dlfcn.h>
#import <pthread.h>
#import <stdatomic.h>
#import <stdlib.h>
#import <stdint.h>
#import <string.h>
#import <sys/types.h>

#if __MAC_OS_X_VERSION_MAX_ALLOWED >= 120000
#define kCompanionAudioElement kAudioObjectPropertyElementMain
#else
#define kCompanionAudioElement kAudioObjectPropertyElementMaster
#endif

// HAL process-object selectors. These are fourCCs, not Unix PIDs.
static const AudioObjectPropertySelector kCompanionPropTranslatePID = 0x69643270u; // 'id2p'
static const AudioObjectPropertySelector kCompanionPropProcessList = 0x70727323u;  // 'prs#'
static const AudioObjectPropertySelector kCompanionPropProcessPID = 0x70706964u;   // 'ppid'

static int companionTakeSnapshot(void *view, int x, int y, int w, int h, uint8_t **out, int *outLen) {
	*out = NULL;
	*outLen = 0;
	if (view == NULL) return -1;
	__block uint8_t *buf = NULL;
	__block int n = 0;
	__block int err = 0;
	dispatch_semaphore_t sem = dispatch_semaphore_create(0);
	dispatch_async(dispatch_get_main_queue(), ^{
		WKWebView *web = (__bridge WKWebView *)view;
		if (![web isKindOfClass:[WKWebView class]]) {
			err = 1;
			dispatch_semaphore_signal(sem);
			return;
		}
		WKSnapshotConfiguration *cfg = [WKSnapshotConfiguration new];
		if (w > 0 && h > 0) {
			cfg.rect = NSMakeRect(x, y, w, h);
		}
		[web takeSnapshotWithConfiguration:cfg completionHandler:^(NSImage *image, NSError *snapErr) {
			if (snapErr != nil || image == nil) {
				err = 1;
			} else {
				NSData *tiff = [image TIFFRepresentation];
				NSBitmapImageRep *rep = tiff ? [NSBitmapImageRep imageRepWithData:tiff] : nil;
				NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
				if (png == nil || png.length == 0) {
					err = 1;
				} else {
					n = (int)png.length;
					buf = malloc((size_t)n);
					if (buf == NULL) err = 1;
					else memcpy(buf, png.bytes, (size_t)n);
				}
			}
			dispatch_semaphore_signal(sem);
		}];
	});
	long wait = dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 8LL * NSEC_PER_SEC));
	if (wait != 0 || err || buf == NULL || n <= 0) {
		free(buf);
		return -1;
	}
	*out = buf;
	*outLen = n;
	return 0;
}

// Process-tap reader for macOS 14.2+. The tap list is HAL process AudioObjectIDs
// ('id2p' on the system object, plus 'prs#' objects whose 'ppid' is this pid),
// never the Unix pid. The initializer is darwinProcessTapPlan.InitSelector.
// Aggregate-device dictionary keys are the AudioHardware.h C strings:
// name, uid, private, tapautostart, taps, and per-tap uid / drift.
#define kCompanionTapMaxSamples (16000 * 10800)

typedef struct {
	atomic_int stop;
	int16_t *pcm;
	int count;
	int cap;
	double step;
	double acc;
	int accN;
	pthread_mutex_t mu;
	AudioObjectID tapID;
	AudioObjectID aggID;
	AudioDeviceIOProcID procID;
} CompanionTap;

static void companionTapDestroy(CompanionTap *tap) {
	if (tap == NULL) return;
	atomic_store(&tap->stop, 1);
	if (tap->aggID != 0 && tap->procID != NULL) {
		AudioDeviceStop(tap->aggID, tap->procID);
		AudioDeviceDestroyIOProcID(tap->aggID, tap->procID);
		tap->procID = NULL;
	}
	if (tap->aggID != 0) {
		AudioHardwareDestroyAggregateDevice(tap->aggID);
		tap->aggID = 0;
	}
	if (tap->tapID != 0) {
		typedef int (*DestroyTap)(unsigned int);
		DestroyTap destroyTap = (DestroyTap)dlsym(RTLD_DEFAULT, "AudioHardwareDestroyProcessTap");
		if (destroyTap != NULL) destroyTap(tap->tapID);
		tap->tapID = 0;
	}
}

static void companionTapPush(CompanionTap *tap, float sample) {
	if (tap->step < 1) tap->step = 1;
	tap->acc += sample;
	tap->accN += 1;
	if ((double)tap->accN + 1e-6 < tap->step) return;
	float value = (float)(tap->acc / (double)tap->accN);
	if (value > 1) value = 1;
	if (value < -1) value = -1;
	tap->acc = 0;
	tap->accN = 0;
	if (tap->count >= tap->cap) {
		if (tap->cap >= kCompanionTapMaxSamples) return;
		int next = tap->cap * 2;
		if (next > kCompanionTapMaxSamples) next = kCompanionTapMaxSamples;
		if (next <= tap->cap) return;
		int16_t *grown = realloc(tap->pcm, (size_t)next * sizeof(int16_t));
		if (grown == NULL) return;
		tap->pcm = grown;
		tap->cap = next;
	}
	tap->pcm[tap->count++] = (int16_t)(value * 32767.0f);
}

static int companionTapBufferFrames(const AudioBuffer *buffer) {
	if (buffer == NULL || buffer->mData == NULL || buffer->mNumberChannels == 0) return 0;
	UInt32 bytesPerFrame = buffer->mNumberChannels * (UInt32)sizeof(float);
	if (bytesPerFrame == 0 || buffer->mDataByteSize < bytesPerFrame) return 0;
	return (int)(buffer->mDataByteSize / bytesPerFrame);
}

static OSStatus companionTapIO(AudioObjectID device, const AudioTimeStamp *now, const AudioBufferList *input, const AudioTimeStamp *inTime, AudioBufferList *output, const AudioTimeStamp *outTime, void *user) {
	(void)device; (void)now; (void)inTime; (void)output; (void)outTime;
	CompanionTap *tap = (CompanionTap *)user;
	if (tap == NULL || atomic_load(&tap->stop) || input == NULL || input->mNumberBuffers == 0) return 0;
	int frames = -1;
	for (UInt32 i = 0; i < input->mNumberBuffers; i++) {
		int n = companionTapBufferFrames(&input->mBuffers[i]);
		if (n <= 0) continue;
		if (frames < 0 || n < frames) frames = n;
	}
	if (frames <= 0) return 0;
	pthread_mutex_lock(&tap->mu);
	for (int i = 0; i < frames; i++) {
		double sum = 0;
		int channels = 0;
		for (UInt32 b = 0; b < input->mNumberBuffers; b++) {
			const AudioBuffer *buffer = &input->mBuffers[b];
			if (companionTapBufferFrames(buffer) <= i) continue;
			const float *data = (const float *)buffer->mData;
			UInt32 count = buffer->mNumberChannels;
			for (UInt32 c = 0; c < count; c++) {
				sum += data[(i * (int)count) + (int)c];
				channels++;
			}
		}
		float mono = channels > 0 ? (float)(sum / (double)channels) : 0;
		companionTapPush(tap, mono);
	}
	pthread_mutex_unlock(&tap->mu);
	return 0;
}

static double companionTapRate(AudioObjectID device) {
	AudioObjectPropertyAddress addr = {
		kAudioDevicePropertyNominalSampleRate,
		kAudioObjectPropertyScopeGlobal,
		kCompanionAudioElement
	};
	Float64 rate = 0;
	UInt32 size = sizeof(rate);
	if (AudioObjectGetPropertyData(device, &addr, 0, NULL, &size, &rate) != 0 || rate < 8000 || rate > 384000) {
		return 48000;
	}
	return rate;
}

static void companionTapSetBool(id desc, const char *name, BOOL value) {
	SEL sel = NSSelectorFromString([NSString stringWithUTF8String:name]);
	if (sel == NULL || ![desc respondsToSelector:sel]) return;
	void (*setBool)(id, SEL, BOOL) = (void (*)(id, SEL, BOOL))objc_msgSend;
	setBool(desc, sel, value);
}

static AudioObjectPropertyAddress companionAudioAddr(AudioObjectPropertySelector sel) {
	AudioObjectPropertyAddress addr;
	addr.mSelector = sel;
	addr.mScope = kAudioObjectPropertyScopeGlobal;
	addr.mElement = kCompanionAudioElement;
	return addr;
}

static int companionProcessObjectMatchesPID(AudioObjectID obj, pid_t want) {
	if (obj == kAudioObjectUnknown || want <= 0) return 0;
	AudioObjectPropertyAddress addr = companionAudioAddr(kCompanionPropProcessPID);
	pid_t got = 0;
	UInt32 size = sizeof(got);
	if (AudioObjectGetPropertyData(obj, &addr, 0, NULL, &size, &got) != noErr) return 0;
	if (size < sizeof(pid_t)) return 0;
	return got == want;
}

static AudioObjectID companionTranslatePID(pid_t want) {
	AudioObjectPropertyAddress addr = companionAudioAddr(kCompanionPropTranslatePID);
	AudioObjectID obj = kAudioObjectUnknown;
	UInt32 size = sizeof(obj);
	pid_t qualifier = want;
	OSStatus status = AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, sizeof(qualifier), &qualifier, &size, &obj);
	if (status != noErr || size < sizeof(AudioObjectID) || obj == kAudioObjectUnknown) return kAudioObjectUnknown;
	if (!companionProcessObjectMatchesPID(obj, want)) return kAudioObjectUnknown;
	return obj;
}

// companionProcessObjectIDs returns NSNumbers of HAL process AudioObjectIDs.
// An empty result is NULL. The Unix pid is never placed in the array.
static NSArray *companionProcessObjectIDs(pid_t want) {
	if (want <= 0) return nil;
	NSMutableArray *ids = [NSMutableArray array];
	AudioObjectID translated = companionTranslatePID(want);
	if (translated != kAudioObjectUnknown) {
		[ids addObject:[NSNumber numberWithUnsignedInt:(unsigned int)translated]];
	}
	AudioObjectPropertyAddress addr = companionAudioAddr(kCompanionPropProcessList);
	UInt32 listSize = 0;
	if (AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &addr, 0, NULL, &listSize) == noErr &&
		listSize >= sizeof(AudioObjectID) &&
		listSize <= (1u << 20) &&
		(listSize % sizeof(AudioObjectID)) == 0) {
		UInt32 count = listSize / (UInt32)sizeof(AudioObjectID);
		AudioObjectID *list = (AudioObjectID *)calloc(count, sizeof(AudioObjectID));
		if (list != NULL) {
			UInt32 got = listSize;
			if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL, &got, list) == noErr) {
				UInt32 n = got / (UInt32)sizeof(AudioObjectID);
				if (n > count) n = count;
				for (UInt32 i = 0; i < n; i++) {
					if (!companionProcessObjectMatchesPID(list[i], want)) continue;
					NSNumber *num = [NSNumber numberWithUnsignedInt:(unsigned int)list[i]];
					if (![ids containsObject:num]) [ids addObject:num];
				}
			}
			free(list);
		}
	}
	if (ids.count == 0) return nil;
	return ids;
}

// companionStartProcessTap records one process. A missing CATapDescription class returns NULL.
// A present class is always sent initMonoMixdownOfProcesses: with the HAL process-object array.
void *companionStartProcessTap(int pid) {
	if (pid <= 0) return NULL;
	Class cls = NSClassFromString(@"CATapDescription");
	if (cls == Nil) return NULL;
	NSArray *processes = companionProcessObjectIDs((pid_t)pid);
	if (processes == nil || processes.count == 0) return NULL;
	SEL initSel = NSSelectorFromString(@"initMonoMixdownOfProcesses:");
	id allocated = [cls alloc];
	id desc = ((id (*)(id, SEL, id))objc_msgSend)(allocated, initSel, processes);
	if (desc == nil) return NULL;
	companionTapSetBool(desc, "setPrivate:", YES);
	companionTapSetBool(desc, "setMixdown:", YES);
	companionTapSetBool(desc, "setMono:", YES);
	companionTapSetBool(desc, "setExclusive:", NO);
	SEL nameSel = NSSelectorFromString(@"setName:");
	if ([desc respondsToSelector:nameSel]) {
		void (*setName)(id, SEL, id) = (void (*)(id, SEL, id))objc_msgSend;
		setName(desc, nameSel, @"MaClawProcessTap");
	}
	NSString *tapUID = nil;
	SEL uuidSel = NSSelectorFromString(@"UUID");
	if (![desc respondsToSelector:uuidSel]) uuidSel = NSSelectorFromString(@"uuid");
	if ([desc respondsToSelector:uuidSel]) {
		id uuid = ((id (*)(id, SEL))objc_msgSend)(desc, uuidSel);
		if ([uuid isKindOfClass:[NSUUID class]]) tapUID = [uuid UUIDString];
		else if ([uuid isKindOfClass:[NSString class]]) tapUID = uuid;
	}
	if (tapUID == nil || tapUID.length == 0) return NULL;
	typedef int (*CreateTap)(id, unsigned int *);
	CreateTap createTap = (CreateTap)dlsym(RTLD_DEFAULT, "AudioHardwareCreateProcessTap");
	if (createTap == NULL) return NULL;
	unsigned int tapID = 0;
	if (createTap(desc, &tapID) != 0 || tapID == 0) return NULL;
	CompanionTap *tap = calloc(1, sizeof(CompanionTap));
	if (tap == NULL) {
		typedef int (*DestroyTap)(unsigned int);
		DestroyTap destroyTap = (DestroyTap)dlsym(RTLD_DEFAULT, "AudioHardwareDestroyProcessTap");
		if (destroyTap != NULL) destroyTap(tapID);
		return NULL;
	}
	tap->tapID = tapID;
	tap->cap = 16000 * 60;
	tap->pcm = calloc((size_t)tap->cap, sizeof(int16_t));
	pthread_mutex_init(&tap->mu, NULL);
	if (tap->pcm == NULL) {
		companionTapDestroy(tap);
		free(tap);
		return NULL;
	}
	NSDictionary *aggregate = @{
		@"name": @"MaClawProcessTap",
		@"uid": [[NSUUID UUID] UUIDString],
		@"private": @YES,
		@"tapautostart": @NO,
		@"taps": @[
			@{
				@"uid": tapUID,
				@"drift": @YES,
			},
		],
	};
	AudioObjectID aggID = 0;
	OSStatus status = AudioHardwareCreateAggregateDevice((__bridge CFDictionaryRef)aggregate, &aggID);
	if (status != 0 || aggID == 0) {
		companionTapDestroy(tap);
		pthread_mutex_destroy(&tap->mu);
		free(tap->pcm);
		free(tap);
		return NULL;
	}
	tap->aggID = aggID;
	tap->step = companionTapRate(aggID) / 16000.0;
	AudioDeviceIOProcID procID = NULL;
	status = AudioDeviceCreateIOProcID(aggID, companionTapIO, tap, &procID);
	if (status != 0 || procID == NULL) {
		companionTapDestroy(tap);
		pthread_mutex_destroy(&tap->mu);
		free(tap->pcm);
		free(tap);
		return NULL;
	}
	tap->procID = procID;
	status = AudioDeviceStart(aggID, procID);
	if (status != 0) {
		companionTapDestroy(tap);
		pthread_mutex_destroy(&tap->mu);
		free(tap->pcm);
		free(tap);
		return NULL;
	}
	return tap;
}

// companionFinishProcessTap stops the tap and copies 16 kHz mono s16 bytes.
void companionFinishProcessTap(void *handle, uint8_t **out, int *outLen) {
	if (out != NULL) *out = NULL;
	if (outLen != NULL) *outLen = 0;
	CompanionTap *tap = (CompanionTap *)handle;
	if (tap == NULL) return;
	companionTapDestroy(tap);
	pthread_mutex_lock(&tap->mu);
	int nbytes = tap->count * (int)sizeof(int16_t);
	if (nbytes > 0 && out != NULL && outLen != NULL) {
		uint8_t *buf = malloc((size_t)nbytes);
		if (buf != NULL) {
			memcpy(buf, tap->pcm, (size_t)nbytes);
			*out = buf;
			*outLen = nbytes;
		}
	}
	pthread_mutex_unlock(&tap->mu);
	pthread_mutex_destroy(&tap->mu);
	free(tap->pcm);
	free(tap);
}
*/
import "C"
import (
	"context"
	"image"
	"os"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/maclawcapture"
)

func captureCompanionWebViewPNG(ctx context.Context, rect image.Rectangle) ([]byte, error) {
	fileCompanionCaptureViewCalls.Add(1)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	view := maclawcapture.Current()
	if view == nil || view.Pointer() == 0 {
		return nil, errCompanionWebViewUnbound
	}
	var out *C.uint8_t
	var n C.int
	// Snapshot the whole view. The host crops to the iframe rect in device pixels.
	status := C.companionTakeSnapshot(unsafe.Pointer(view.Pointer()), 0, 0, 0, 0, &out, &n)
	if status != 0 || out == nil || n <= 0 {
		return nil, errCompanionWebViewUnbound
	}
	defer C.free(unsafe.Pointer(out))
	raw := C.GoBytes(unsafe.Pointer(out), n)
	return cropCompanionPNG(raw, rect)
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
	handle := C.companionStartProcessTap(C.int(act.TargetPID))
	if handle == nil {
		return nil, true
	}
	<-ctx.Done()
	var out *C.uint8_t
	var n C.int
	C.companionFinishProcessTap(handle, &out, &n)
	if out == nil || n < 2 {
		if out != nil {
			C.free(unsafe.Pointer(out))
		}
		return nil, true
	}
	defer C.free(unsafe.Pointer(out))
	return pcm16MonoWAV(C.GoBytes(unsafe.Pointer(out), n), 16000), false
}

func capturePlaybackEnded(context.Context) bool { return false }
