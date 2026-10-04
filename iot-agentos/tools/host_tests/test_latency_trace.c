/*
 * Host test of the latency trace value type (plan N0-2).
 *
 * Compiles the public header only: the milestone state machine and the line
 * formatter are inline and have no ESP-IDF dependency, so the whole contract
 * is exercised here without a timer mock.
 */

#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "services/latency_trace.h"

static int fail(const char *msg) {
    fprintf(stderr, "FAIL: %s\n", msg);
    return 1;
}

static int test_reset_and_first_stamp_wins(void) {
    latency_trace_t trace;
    latency_trace_reset(&trace);
    if (trace.origin_us != 0 || latency_trace_has(&trace, LATENCY_MARK_RELEASE)) {
        return fail("reset left marks behind");
    }

    /* The first stamp of the turn becomes the origin. */
    latency_trace_stamp(&trace, LATENCY_MARK_RELEASE, 1000000);
    latency_trace_stamp(&trace, LATENCY_MARK_SENT, 1120000);
    /* A retried uplink must not move `sent`. */
    latency_trace_stamp(&trace, LATENCY_MARK_SENT, 9999999);

    if (latency_trace_offset_ms(&trace, LATENCY_MARK_RELEASE) != 0) {
        return fail("release must be the origin");
    }
    if (latency_trace_offset_ms(&trace, LATENCY_MARK_SENT) != 120) {
        return fail("retried stamp moved `sent`");
    }
    return 0;
}

static int test_origin_falls_back_to_first_mark(void) {
    latency_trace_t trace;
    latency_trace_reset(&trace);
    latency_trace_stamp(&trace, LATENCY_MARK_SENT, 1000);
    latency_trace_stamp(&trace, LATENCY_MARK_ACK, 2000);
    if (latency_trace_offset_ms(&trace, LATENCY_MARK_SENT) != 0) {
        return fail("first mark must establish the origin");
    }
    if (latency_trace_offset_ms(&trace, LATENCY_MARK_ACK) != 1) {
        return fail("offset must be measured from the first mark");
    }
    return 0;
}

static int test_missing_marks_are_minus_one(void) {
    latency_trace_t trace;
    latency_trace_reset(&trace);
    if (latency_trace_offset_ms(&trace, LATENCY_MARK_AUDIO) != -1) {
        return fail("empty trace must report -1");
    }
    latency_trace_stamp(&trace, LATENCY_MARK_RELEASE, 5000);
    if (latency_trace_offset_ms(&trace, LATENCY_MARK_AUDIO) != -1) {
        return fail("unstamped mark must report -1");
    }
    if (latency_trace_has(&trace, LATENCY_MARK_AUDIO)) {
        return fail("unstamped mark must not report present");
    }
    return 0;
}

static int test_out_of_range_marks_are_inert(void) {
    latency_trace_t trace;
    latency_trace_reset(&trace);
    latency_trace_stamp(&trace, LATENCY_MARK_RELEASE, 100);
    latency_trace_stamp(&trace, (latency_mark_t)99, 200);

    if (latency_trace_has(&trace, (latency_mark_t)99)) {
        return fail("out-of-range mark must not be recorded");
    }
    if (latency_trace_offset_ms(&trace, (latency_mark_t)99) != -1) {
        return fail("out-of-range mark must report -1");
    }
    if (strcmp(latency_mark_name((latency_mark_t)99), "?") != 0) {
        return fail("out-of-range mark name must be ?");
    }
    /* A null trace must be tolerated by every accessor. */
    if (latency_trace_has(NULL, LATENCY_MARK_RELEASE)) {
        return fail("null trace must not report a mark");
    }
    if (latency_trace_offset_ms(NULL, LATENCY_MARK_RELEASE) != -1) {
        return fail("null trace must report -1");
    }
    latency_trace_reset(NULL);
    latency_trace_stamp(NULL, LATENCY_MARK_RELEASE, 1);
    return 0;
}

static int test_response_metric(void) {
    latency_trace_t trace;
    latency_trace_reset(&trace);
    if (latency_trace_response_ms(&trace) != -1) {
        return fail("empty trace must have no response metric");
    }

    latency_trace_stamp(&trace, LATENCY_MARK_RELEASE, 1000000);
    latency_trace_stamp(&trace, LATENCY_MARK_DECODE, 2700000);
    if (latency_trace_response_ms(&trace) != 1700) {
        return fail("response metric must fall back to decode");
    }

    latency_trace_stamp(&trace, LATENCY_MARK_AUDIO, 2850000);
    if (latency_trace_response_ms(&trace) != 1850) {
        return fail("response metric must prefer the rendered frame");
    }

    /* A WAV reply hands bytes to the renderer without a separate decode stamp;
     * the first server audio byte must still produce a number. */
    latency_trace_reset(&trace);
    latency_trace_stamp(&trace, LATENCY_MARK_RELEASE, 1000000);
    latency_trace_stamp(&trace, LATENCY_MARK_TTS, 2400000);
    if (latency_trace_response_ms(&trace) != 1400) {
        return fail("response metric must fall back to the first audio byte");
    }
    return 0;
}

static int test_worth_logging_policy(void) {
    latency_trace_t trace;
    latency_trace_reset(&trace);
    if (latency_trace_worth_logging(&trace)) return fail("empty turn logged");
    if (latency_trace_worth_logging(NULL)) return fail("null turn logged");

    latency_trace_stamp(&trace, LATENCY_MARK_RELEASE, 1000);
    if (latency_trace_worth_logging(&trace)) return fail("bare release logged");

    latency_trace_stamp(&trace, LATENCY_MARK_SENT, 2000);
    if (latency_trace_worth_logging(&trace)) return fail("cancelled turn logged");

    latency_trace_stamp(&trace, LATENCY_MARK_DONE, 3000);
    if (!latency_trace_worth_logging(&trace)) return fail("final text turn dropped");

    latency_trace_reset(&trace);
    latency_trace_stamp(&trace, LATENCY_MARK_RELEASE, 1000);
    latency_trace_stamp(&trace, LATENCY_MARK_AUDIO, 2000);
    if (!latency_trace_worth_logging(&trace)) return fail("audible turn dropped");
    return 0;
}

static int test_format_exact_line(void) {
    latency_trace_t trace;
    latency_trace_reset(&trace);
    latency_trace_stamp(&trace, LATENCY_MARK_RELEASE, 1000000);
    latency_trace_stamp(&trace, LATENCY_MARK_SENT, 1120000);
    latency_trace_stamp(&trace, LATENCY_MARK_ACK, 1340000);
    latency_trace_stamp(&trace, LATENCY_MARK_TEXT, 1890000);
    latency_trace_stamp(&trace, LATENCY_MARK_DONE, 2500000);
    latency_trace_stamp(&trace, LATENCY_MARK_TTS, 2600000);
    latency_trace_stamp(&trace, LATENCY_MARK_DECODE, 2700000);
    latency_trace_stamp(&trace, LATENCY_MARK_AUDIO, 2850000);

    char line[LATENCY_TRACE_LINE_CAPACITY];
    const size_t n = latency_trace_format(&trace, line, sizeof(line));
    static const char kExpected[] =
        "latency turn release+0 sent+120 ack+340 text+890 done+1500 "
        "tts+1600 decode+1700 audio+1850";
    if (n != strlen(kExpected)) {
        fprintf(stderr, "FAIL: format length %u, want %u\n", (unsigned)n,
                (unsigned)strlen(kExpected));
        return 1;
    }
    if (strcmp(line, kExpected) != 0) {
        fprintf(stderr, "FAIL: format output\n  got  <%s>\n  want <%s>\n", line,
                kExpected);
        return 1;
    }
    return 0;
}

static int test_format_skips_unstamped_marks(void) {
    latency_trace_t trace;
    latency_trace_reset(&trace);
    latency_trace_stamp(&trace, LATENCY_MARK_RELEASE, 1000000);
    latency_trace_stamp(&trace, LATENCY_MARK_AUDIO, 1500000);

    char line[LATENCY_TRACE_LINE_CAPACITY];
    (void)latency_trace_format(&trace, line, sizeof(line));
    if (strcmp(line, "latency turn release+0 audio+500") != 0) {
        fprintf(stderr, "FAIL: sparse format <%s>\n", line);
        return 1;
    }
    return 0;
}

static int test_format_is_bounded(void) {
    latency_trace_t trace;
    latency_trace_reset(&trace);
    latency_trace_stamp(&trace, LATENCY_MARK_RELEASE, 1000000);
    latency_trace_stamp(&trace, LATENCY_MARK_AUDIO, 2850000);

    char line[LATENCY_TRACE_LINE_CAPACITY];

    /* Capacity that cuts mid-token must still terminate and stay in bounds. */
    const size_t n = latency_trace_format(&trace, line, 20u);
    if (n != 19u) {
        fprintf(stderr, "FAIL: truncated length %u, want 19\n", (unsigned)n);
        return 1;
    }
    if (line[19] != '\0') return fail("truncated line not terminated");
    if (strncmp(line, "latency turn release", 19u) != 0) {
        fprintf(stderr, "FAIL: truncated prefix <%s>\n", line);
        return 1;
    }

    /* Capacity smaller than the fixed prefix. */
    const size_t tiny = latency_trace_format(&trace, line, 5u);
    if (tiny != 4u) return fail("tiny capacity length wrong");
    if (line[4] != '\0') return fail("tiny capacity not terminated");

    /* Degenerate inputs must not write. */
    line[0] = 'x';
    if (latency_trace_format(&trace, line, 0u) != 0u) {
        return fail("zero capacity must write nothing");
    }
    if (line[0] != 'x') return fail("zero capacity wrote to the buffer");
    if (latency_trace_format(NULL, line, sizeof(line)) != 0u) {
        return fail("null trace must format nothing");
    }
    if (latency_trace_format(&trace, NULL, 8u) != 0u) {
        return fail("null buffer must be tolerated");
    }
    return 0;
}

int main(void) {
    if (test_reset_and_first_stamp_wins()) return 1;
    if (test_origin_falls_back_to_first_mark()) return 1;
    if (test_missing_marks_are_minus_one()) return 1;
    if (test_out_of_range_marks_are_inert()) return 1;
    if (test_response_metric()) return 1;
    if (test_worth_logging_policy()) return 1;
    if (test_format_exact_line()) return 1;
    if (test_format_skips_unstamped_marks()) return 1;
    if (test_format_is_bounded()) return 1;

    printf("latency_trace host tests passed\n");
    return 0;
}
