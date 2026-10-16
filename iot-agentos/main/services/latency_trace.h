#pragma once

/*
 * Turn-scoped latency trace (plan N0-2).
 *
 * Records the milestone timestamps of one voice turn so end-to-end response
 * latency can be attributed instead of guessed.  This is a pure value type:
 * every helper takes the caller's `now_us`, so the whole state machine is
 * exercised by the host regression without a timer mock.
 *
 * The device-side singleton that supplies the monotonic clock lives in
 * latency_trace.c; this header must stay free of ESP-IDF.
 *
 * Milestones follow the Muse gadget SDK turn marks (release / sent / ack /
 * text / done / tts / audio) with one local addition, `decode`, because this
 * device decodes server audio itself and the decode step is where a stall
 * shows up first.
 *
 * Semantics:
 *   - first stamp wins per mark, so a retried uplink cannot move `sent`;
 *   - the first stamp of a turn becomes the origin, so every offset is
 *     measured from the moment the user stopped speaking;
 *   - a turn is only worth logging once it produced either a final text or an
 *     audible frame, which keeps cancelled turns out of the log.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>

typedef enum {
    LATENCY_MARK_RELEASE = 0, /* user finished speaking / capture stop requested */
    LATENCY_MARK_SENT,        /* uplink accepted by the transport */
    LATENCY_MARK_ACK,         /* server accepted it / first reply frame dequeued */
    LATENCY_MARK_TEXT,        /* first assistant text token */
    LATENCY_MARK_DONE,        /* assistant text final */
    LATENCY_MARK_TTS,         /* first server audio byte available */
    LATENCY_MARK_DECODE,      /* first audio frame decoded */
    LATENCY_MARK_AUDIO,       /* first audio frame actually rendered */
    LATENCY_MARK_COUNT
} latency_mark_t;

/* Fits all eight marks at their widest ("decode+123456"). */
#define LATENCY_TRACE_LINE_CAPACITY 128u

typedef struct {
    int64_t origin_us;
    int64_t marks[LATENCY_MARK_COUNT];
} latency_trace_t;

static inline const char *latency_mark_name(latency_mark_t mark) {
    static const char *const names[LATENCY_MARK_COUNT] = {
        "release", "sent", "ack", "text",
        "done",    "tts",  "decode", "audio",
    };
    if ((unsigned)mark >= (unsigned)LATENCY_MARK_COUNT) return "?";
    return names[(unsigned)mark];
}

static inline void latency_trace_reset(latency_trace_t *trace) {
    if (!trace) return;
    trace->origin_us = 0;
    for (unsigned i = 0u; i < (unsigned)LATENCY_MARK_COUNT; ++i) {
        trace->marks[i] = 0;
    }
}

/* First stamp wins.  The first stamp of the turn establishes the origin. */
static inline void latency_trace_stamp(latency_trace_t *trace, latency_mark_t mark,
                                       int64_t now_us) {
    if (!trace || (unsigned)mark >= (unsigned)LATENCY_MARK_COUNT) return;
    if (trace->origin_us == 0) trace->origin_us = now_us;
    if (trace->marks[(unsigned)mark] == 0) {
        trace->marks[(unsigned)mark] = now_us;
    }
}

static inline bool latency_trace_has(const latency_trace_t *trace,
                                     latency_mark_t mark) {
    if (!trace || (unsigned)mark >= (unsigned)LATENCY_MARK_COUNT) return false;
    return trace->marks[(unsigned)mark] != 0;
}

/* Milliseconds from the turn origin, or -1 when the mark never arrived. */
static inline int64_t latency_trace_offset_ms(const latency_trace_t *trace,
                                              latency_mark_t mark) {
    if (!trace || (unsigned)mark >= (unsigned)LATENCY_MARK_COUNT) return -1;
    if (trace->origin_us == 0 || trace->marks[(unsigned)mark] == 0) return -1;
    return (trace->marks[(unsigned)mark] - trace->origin_us) / 1000;
}

/* The metric that matters most: user stopped speaking -> first sound out.
 * Falls back to the decode stamp, then to the first server audio byte, so a
 * render stall is still measurable and a WAV reply which never reports a
 * separate decode step still contributes a number. */
static inline int64_t latency_trace_response_ms(const latency_trace_t *trace) {
    int64_t value = latency_trace_offset_ms(trace, LATENCY_MARK_AUDIO);
    if (value < 0) value = latency_trace_offset_ms(trace, LATENCY_MARK_DECODE);
    if (value < 0) value = latency_trace_offset_ms(trace, LATENCY_MARK_TTS);
    return value;
}

/* Cancelled turns produce neither a final text nor an audible frame and must
 * not pollute the log. */
static inline bool latency_trace_worth_logging(const latency_trace_t *trace) {
    if (!trace || trace->origin_us == 0) return false;
    return latency_trace_has(trace, LATENCY_MARK_DONE) ||
           latency_trace_has(trace, LATENCY_MARK_AUDIO);
}

/*
 * Render "latency turn release+0 sent+120 ack+340 ..." into `out`.
 * Returns the number of bytes written excluding the terminator, always
 * leaving `out` NUL-terminated and never writing past `capacity`.
 */
static inline size_t latency_trace_format(const latency_trace_t *trace, char *out,
                                          size_t capacity) {
    if (!out || capacity == 0u) return 0u;
    out[0] = '\0';
    if (!trace) return 0u;

    const int head = snprintf(out, capacity, "latency turn");
    if (head < 0) {
        out[0] = '\0';
        return 0u;
    }
    size_t used = ((size_t)head >= capacity) ? capacity - 1u : (size_t)head;
    if (used + 1u >= capacity) return used;

    for (unsigned i = 0u; i < (unsigned)LATENCY_MARK_COUNT; ++i) {
        if (trace->marks[i] == 0) continue;
        const int written = snprintf(out + used, capacity - used, " %s+%lld",
                                     latency_mark_name((latency_mark_t)i),
                                     (long long)latency_trace_offset_ms(
                                         trace, (latency_mark_t)i));
        if (written < 0) break;
        if ((size_t)written >= capacity - used) {
            used = capacity - 1u;
            break;
        }
        used += (size_t)written;
    }
    return used;
}

/* ---- Device-side singleton (implemented in latency_trace.c) ---- */

/* Begin a new turn: clears every mark and opens the turn. */
void latency_trace_begin(void);

/* Stamp one milestone with the current monotonic clock.  No-op unless a turn
 * is open, so unrelated audio cannot invent an origin. */
void latency_trace_mark(latency_mark_t mark);

/* True between begin() and the next flush(). */
bool latency_trace_active(void);

/* Emit the turn as one log line, close it and reset.  No-op when no turn is
 * open; a turn with no terminal mark is dropped silently. */
void latency_trace_flush(void);

/* Read-only view of the in-flight turn, or NULL when no turn is open. */
const latency_trace_t *latency_trace_current(void);
