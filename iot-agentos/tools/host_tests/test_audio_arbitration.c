/*
 * Host test of Audio Arbitration evaluate helpers (A11 first increment).
 *
 * Compiles the public header only: the exclusive vs appendix §9 matrix is
 * inline and has no ESP-IDF / Audio Service dependency.
 */

#include <stdio.h>
#include <stdint.h>

#include "services/audio_arbitration_service.h"

static int fail(const char *msg) {
    fprintf(stderr, "FAIL: %s\n", msg);
    return 1;
}

int main(void) {
    if (!audio_arbitration_alarm_interruption_generation_allowed(
            10u, 10u, AUDIO_ARBITRATION_KIND_IDLE) ||
        audio_arbitration_alarm_interruption_generation_allowed(
            10u, 11u, AUDIO_ARBITRATION_KIND_ALARM_BURST) ||
        audio_arbitration_alarm_interruption_generation_allowed(
            10u, 19u, AUDIO_ARBITRATION_KIND_ALARM_BURST) ||
        audio_arbitration_alarm_interruption_generation_allowed(
            10u, 12u, AUDIO_ARBITRATION_KIND_IDLE)) {
        return fail("stale alarm interruption generations accepted");
    }
    if (audio_arbitration_alarm_interruption_generation_allowed(
            10u, 11u, AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE) ||
        audio_arbitration_alarm_interruption_generation_allowed(
            10u, 12u, AUDIO_ARBITRATION_KIND_PCM_PLAYBACK) ||
        audio_arbitration_alarm_interruption_generation_allowed(
            0u, 10u, AUDIO_ARBITRATION_KIND_IDLE)) {
        return fail("stale or malformed interruption generations accepted");
    }
    if (!audio_arbitration_alarm_interruption_generation_allowed(
            UINT32_MAX, UINT32_MAX, AUDIO_ARBITRATION_KIND_IDLE) ||
        audio_arbitration_alarm_interruption_generation_allowed(
            UINT32_MAX, 1u, AUDIO_ARBITRATION_KIND_ALARM_BURST) ||
        audio_arbitration_alarm_interruption_generation_allowed(
            UINT32_MAX, 9u, AUDIO_ARBITRATION_KIND_ALARM_BURST) ||
        audio_arbitration_alarm_interruption_generation_allowed(
            UINT32_MAX, 2u, AUDIO_ARBITRATION_KIND_IDLE) ||
        audio_arbitration_alarm_interruption_generation_allowed(
            UINT32_MAX, 3u, AUDIO_ARBITRATION_KIND_WAV_PLAYBACK)) {
        return fail("alarm interruption generation fence is incorrect");
    }
    if (!audio_arbitration_alarm_interruption_generation_allowed_scoped(
            10u, 11u, AUDIO_ARBITRATION_KIND_ALARM_BURST, true, 7u, 7u, false, 0u) ||
        !audio_arbitration_alarm_interruption_generation_allowed_scoped(
            10u, 19u, AUDIO_ARBITRATION_KIND_IDLE, true, 7u, 7u, false, 0u) ||
        !audio_arbitration_alarm_interruption_generation_allowed_scoped(
            10u, 19u, AUDIO_ARBITRATION_KIND_IDLE, false, 7u, 7u, true, 19u) ||
        audio_arbitration_alarm_interruption_generation_allowed_scoped(
            10u, 19u, AUDIO_ARBITRATION_KIND_ALARM_BURST, true, 7u, 8u, false, 0u) ||
        audio_arbitration_alarm_interruption_generation_allowed_scoped(
            10u, 19u, AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE, true, 7u, 7u, false, 0u) ||
        audio_arbitration_alarm_interruption_generation_allowed_scoped(
            10u, 20u, AUDIO_ARBITRATION_KIND_IDLE, false, 7u, 7u, true, 19u)) {
        return fail("alarm transaction epoch fence is incorrect");
    }
    if (audio_arbitration_alarm_preemption_allowed(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE, false)) {
        return fail("shadow alarm must not preempt capture");
    }
    if (!audio_arbitration_alarm_preemption_allowed(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE, true) ||
        !audio_arbitration_alarm_preemption_allowed(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_MEETING_STREAM, true) ||
        !audio_arbitration_alarm_preemption_allowed(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_WAV_PLAYBACK, true) ||
        !audio_arbitration_alarm_preemption_allowed(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_PCM_PLAYBACK, true)) {
        return fail("authoritative alarm must preempt active non-duplex owners");
    }
    if (audio_arbitration_alarm_preemption_allowed(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_IDLE, true) ||
        audio_arbitration_alarm_preemption_allowed(
            AUDIO_ARBITRATION_KIND_ALARM_BURST,
            AUDIO_ARBITRATION_KIND_ALARM_BURST, true) ||
        audio_arbitration_alarm_preemption_allowed(
            AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE,
            AUDIO_ARBITRATION_KIND_MEETING_STREAM, true)) {
        return fail("invalid alarm preemption combinations must be rejected");
    }
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
