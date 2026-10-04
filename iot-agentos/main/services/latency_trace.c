/*
 * Latency trace device glue (plan N0-2).
 *
 * The milestone state machine is pure and lives in latency_trace.h; this file
 * only supplies the monotonic clock and the single log line.  Keep it thin --
 * anything testable belongs in the header.
 */

#include "services/latency_trace.h"

#include "esp_log.h"
#include "esp_timer.h"

static const char *TAG = "latency";

static latency_trace_t s_trace;
/* A turn is "open" between begin() and the next flush().  Marks outside that
 * window belong to no turn (welcome audio, GUI previews, a reply left over
 * from a previous boot) and must not invent an origin. */
static bool s_turn_open;

void latency_trace_begin(void) {
    latency_trace_reset(&s_trace);
    s_turn_open = true;
}

void latency_trace_mark(latency_mark_t mark) {
    if (!s_turn_open) return;
    latency_trace_stamp(&s_trace, mark, esp_timer_get_time());
}

bool latency_trace_active(void) {
    return s_turn_open;
}

const latency_trace_t *latency_trace_current(void) {
    return s_turn_open ? &s_trace : NULL;
}

void latency_trace_flush(void) {
    if (!s_turn_open) return;
    s_turn_open = false;
    if (!latency_trace_worth_logging(&s_trace)) {
        /* Cancelled or empty turn: drop it without noise. */
        latency_trace_reset(&s_trace);
        return;
    }

    char line[LATENCY_TRACE_LINE_CAPACITY];
    (void)latency_trace_format(&s_trace, line, sizeof(line));
    ESP_LOGI(TAG, "response=%lldms %s", (long long)latency_trace_response_ms(&s_trace),
             line);
    latency_trace_reset(&s_trace);
}
