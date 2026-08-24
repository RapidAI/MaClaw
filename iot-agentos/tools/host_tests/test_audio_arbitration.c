/*
 * Host test of Audio Arbitration evaluate helpers (A11 first increment).
 *
 * Compiles the public header only: the exclusive vs appendix §9 matrix is
 * inline and has no ESP-IDF / Audio Service dependency.
 */

#include <stdio.h>

#include "services/audio_arbitration_service.h"

static int fail(const char *msg) {
    fprintf(stderr, "FAIL: %s\n", msg);
    return 1;
}

int main(void) {
    if (audio_arbitration_evaluate_exclusive(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_IDLE) != AUDIO_ARBITRATION_GRANT) {
        return fail("exclusive idle must GRANT");
    }
    if (audio_arbitration_evaluate_exclusive(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE) != AUDIO_ARBITRATION_BUSY) {
        return fail("exclusive capture+alarm must BUSY");
    }
    if (audio_arbitration_evaluate_appendix(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_IDLE) != AUDIO_ARBITRATION_GRANT) {
        return fail("appendix idle alarm must GRANT");
    }
    if (audio_arbitration_evaluate_appendix(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE) != AUDIO_ARBITRATION_WOULD_PREEMPT) {
        return fail("appendix capture+alarm must WOULD_PREEMPT");
    }
    if (audio_arbitration_evaluate_appendix(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_MEETING_STREAM) != AUDIO_ARBITRATION_WOULD_PREEMPT) {
        return fail("appendix meeting+alarm must WOULD_PREEMPT (non-duplex)");
    }
    if (audio_arbitration_evaluate_appendix(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_WAV_PLAYBACK) != AUDIO_ARBITRATION_WOULD_PREEMPT) {
        return fail("appendix wav+alarm must WOULD_PREEMPT");
    }
    if (audio_arbitration_evaluate_appendix(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_PCM_PLAYBACK) != AUDIO_ARBITRATION_WOULD_PREEMPT) {
        return fail("appendix pcm+alarm must WOULD_PREEMPT");
    }
    if (audio_arbitration_evaluate_appendix(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_ALARM_BURST) != AUDIO_ARBITRATION_BUSY) {
        return fail("appendix alarm+alarm must BUSY");
    }
    if (audio_arbitration_evaluate_exclusive(
            AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE,
            AUDIO_ARBITRATION_KIND_IDLE) !=
        audio_arbitration_evaluate_appendix(
            AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE,
            AUDIO_ARBITRATION_KIND_IDLE)) {
        return fail("command on idle must agree");
    }
    if (audio_arbitration_evaluate_appendix(
            AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE,
            AUDIO_ARBITRATION_KIND_MEETING_STREAM) != AUDIO_ARBITRATION_BUSY) {
        return fail("command must not preempt meeting");
    }
    printf("PASS audio_arbitration exclusive vs appendix matrix\n");
    return 0;
}
