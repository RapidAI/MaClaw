/*
 * Host regression for the A6 Foreground & Interruption Coordinator shadow
 * decision layer (appendix section 8).
 *
 * The coordinator is compiled here from production source against the shared
 * host mocks.  These tests lock the lease/priority/resume-token contract that
 * the later authoritative cut-over depends on:
 *   - monotonic, never-zero resume tokens;
 *   - highest-priority owner selection with newest-token tie break;
 *   - stale-token release rejection that never displaces a newer lease;
 *   - expected_restore_after() returning the next surviving owner;
 *   - shadow observers mirroring exactly the same decisions;
 *   - DISPLAY_OFF lock borrow rules and alarm-preserving ambient restore;
 *   - authoritative flag defaulting to false (shadow mode stays inert).
 */

#include <stdio.h>

#include "app_ui.h"
#include "services/foreground_coordinator.h"

/* ---- host mock plumbing ------------------------------------------------ */

static int64_t s_fake_time_us;
int64_t esp_timer_get_time(void) {
    s_fake_time_us += 1000;
    return s_fake_time_us;
}

static app_ui_surface_t s_test_surface = APP_UI_SURFACE_PET;
app_ui_model_t app_ui_snapshot(void) {
    app_ui_model_t model = {0};
    model.surface = s_test_surface;
    return model;
}

/* ---- tiny assertion harness -------------------------------------------- */

static int g_checks;
static int g_failures;

#define CHECK(cond)                                                          \
    do {                                                                     \
        ++g_checks;                                                          \
        if (!(cond)) {                                                       \
            ++g_failures;                                                    \
            printf("FAIL %s:%d: %s\n", __FILE__, __LINE__, #cond);           \
        }                                                                    \
    } while (0)

/* ---- tests -------------------------------------------------------------- */

static void test_init_resets_and_defaults_shadow(void) {
    CHECK(foreground_coordinator_init() == DEVICE_STATUS_OK);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_NONE);
    CHECK(!foreground_coordinator_is_authoritative());
    foreground_lease_t lease = {0};
    CHECK(!foreground_coordinator_get_lease(FOREGROUND_OWNER_ALARM, &lease) ||
          !lease.held);
}

static void test_invalid_owners_are_rejected(void) {
    CHECK(foreground_coordinator_acquire(FOREGROUND_OWNER_NONE,
                                         FOREGROUND_PRIORITY_SAFETY,
                                         FOREGROUND_SCENE_AMBIENT) == 0u);
    CHECK(!foreground_coordinator_release(FOREGROUND_OWNER_NONE, 1u));
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_NONE);
}

static void test_tokens_are_monotonic_and_never_zero(void) {
    foreground_coordinator_init();
    const uint32_t t1 = foreground_coordinator_acquire(
        FOREGROUND_OWNER_COMMAND_VOICE, FOREGROUND_PRIORITY_CAPTURE,
        FOREGROUND_SCENE_VOICE_COMMAND);
    const uint32_t t2 = foreground_coordinator_acquire(
        FOREGROUND_OWNER_MEETING, FOREGROUND_PRIORITY_PROGRESS,
        FOREGROUND_SCENE_MEETING_UPLOAD);
    CHECK(t1 != 0u);
    CHECK(t2 == t1 + 1u);
    /* Re-acquiring the same owner publishes a new generation token. */
    const uint32_t t3 = foreground_coordinator_acquire(
        FOREGROUND_OWNER_COMMAND_VOICE, FOREGROUND_PRIORITY_CAPTURE,
        FOREGROUND_SCENE_VOICE_COMMAND);
    CHECK(t3 > t2);
    foreground_lease_t lease = {0};
    CHECK(foreground_coordinator_get_lease(FOREGROUND_OWNER_COMMAND_VOICE,
                                           &lease));
    CHECK(lease.held && lease.token == t3);
    /* The old generation cannot retire the new lease. */
    CHECK(!foreground_coordinator_release(FOREGROUND_OWNER_COMMAND_VOICE, t1));
    CHECK(foreground_coordinator_get_lease(FOREGROUND_OWNER_COMMAND_VOICE,
                                           &lease) && lease.held);
    CHECK(foreground_coordinator_release(FOREGROUND_OWNER_COMMAND_VOICE, t3));
    CHECK(foreground_coordinator_get_lease(FOREGROUND_OWNER_COMMAND_VOICE,
                                           &lease) && !lease.held &&
          lease.released_at_us > 0);
}

static void test_priority_selection_and_tie_break(void) {
    foreground_coordinator_init();
    (void)foreground_coordinator_acquire(FOREGROUND_OWNER_COMMAND_RESULT,
                                         FOREGROUND_PRIORITY_RESULT,
                                         FOREGROUND_SCENE_COMMAND_RESULT);
    (void)foreground_coordinator_acquire(FOREGROUND_OWNER_MEETING,
                                         FOREGROUND_PRIORITY_PROGRESS,
                                         FOREGROUND_SCENE_MEETING_UPLOAD);
    /* Capture outranks result and progress. */
    (void)foreground_coordinator_acquire(FOREGROUND_OWNER_SETUP,
                                         FOREGROUND_PRIORITY_RECOVERY,
                                         FOREGROUND_SCENE_SETUP_PORTAL);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_SETUP);
    /* Same priority resolves to the newest token. */
    (void)foreground_coordinator_acquire(FOREGROUND_OWNER_UPDATE,
                                         FOREGROUND_PRIORITY_PROGRESS,
                                         FOREGROUND_SCENE_MEETING_UPLOAD);
    foreground_lease_t update = {0};
    CHECK(foreground_coordinator_get_lease(FOREGROUND_OWNER_UPDATE, &update));
    foreground_lease_t meeting = {0};
    CHECK(foreground_coordinator_get_lease(FOREGROUND_OWNER_MEETING, &meeting));
    CHECK(update.priority == FOREGROUND_PRIORITY_PROGRESS &&
          meeting.priority == FOREGROUND_PRIORITY_PROGRESS);
    /* Neither UPDATE nor MEETING may win while RECOVERY is held. */
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_SETUP);
    /* Restore skips the releasing owner itself and picks the strongest
     * survivor: the held RESULT outranks both PROGRESS leases. */
    CHECK(foreground_coordinator_expected_restore_after(FOREGROUND_OWNER_SETUP) ==
          FOREGROUND_OWNER_COMMAND_RESULT);
    foreground_lease_t result = {0};
    CHECK(foreground_coordinator_get_lease(FOREGROUND_OWNER_COMMAND_RESULT,
                                           &result));
    CHECK(foreground_coordinator_release(FOREGROUND_OWNER_COMMAND_RESULT,
                                         result.token));
    /* With RESULT gone, the same-priority tie resolves to the newest token,
     * so UPDATE (acquired after MEETING) inherits the foreground. */
    CHECK(foreground_coordinator_expected_restore_after(FOREGROUND_OWNER_SETUP) ==
          FOREGROUND_OWNER_UPDATE);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_SETUP);
}

static void test_expected_restore_skips_released_owner(void) {
    foreground_coordinator_init();
    (void)foreground_coordinator_acquire(FOREGROUND_OWNER_COMMAND_RESULT,
                                         FOREGROUND_PRIORITY_RESULT,
                                         FOREGROUND_SCENE_COMMAND_RESULT);
    const uint32_t meeting_token =
        foreground_coordinator_acquire(FOREGROUND_OWNER_MEETING,
                                       FOREGROUND_PRIORITY_PROGRESS,
                                       FOREGROUND_SCENE_MEETING_UPLOAD);
    CHECK(foreground_coordinator_expected_restore_after(
              FOREGROUND_OWNER_COMMAND_RESULT) == FOREGROUND_OWNER_MEETING);
    CHECK(foreground_coordinator_release(FOREGROUND_OWNER_MEETING,
                                         meeting_token));
    CHECK(foreground_coordinator_expected_restore_after(
              FOREGROUND_OWNER_COMMAND_RESULT) == FOREGROUND_OWNER_NONE);
    /* Releasing the last owner leaves ambient in charge. */
    foreground_lease_t result = {0};
    CHECK(foreground_coordinator_get_lease(FOREGROUND_OWNER_COMMAND_RESULT,
                                           &result));
    CHECK(foreground_coordinator_release(FOREGROUND_OWNER_COMMAND_RESULT,
                                         result.token));
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_NONE);
}

static void test_shadow_observers_mirror_decisions(void) {
    foreground_coordinator_init();
    s_test_surface = APP_UI_SURFACE_RECORDING;
    foreground_coordinator_observe_acquire(FOREGROUND_OWNER_COMMAND_VOICE,
                                           FOREGROUND_PRIORITY_CAPTURE,
                                           FOREGROUND_SCENE_VOICE_COMMAND);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_COMMAND_VOICE);
    foreground_coordinator_observe_release(FOREGROUND_OWNER_COMMAND_VOICE);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_NONE);
    /* Observers are append-only: the App UI surface was never mutated. */
    CHECK(s_test_surface == APP_UI_SURFACE_RECORDING);
    /* Unknown owner release observation is ignored safely. */
    foreground_coordinator_observe_release(FOREGROUND_OWNER_NONE);
    foreground_coordinator_observe_release(FOREGROUND_OWNER_COUNT);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_NONE);
}

static void test_display_lock_borrow_rule(void) {
    foreground_coordinator_init();
    s_test_surface = APP_UI_SURFACE_RECORDING;
    foreground_coordinator_observe_display_lock(true);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_COMMAND_VOICE);
    foreground_coordinator_observe_display_lock(false);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_NONE);

    /* A meeting-owned foreground keeps the borrowed command lock from
     * starting a voice-command lease. */
    (void)foreground_coordinator_acquire(FOREGROUND_OWNER_MEETING,
                                         FOREGROUND_PRIORITY_PROGRESS,
                                         FOREGROUND_SCENE_MEETING_UPLOAD);
    foreground_coordinator_observe_display_lock(true);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_MEETING);
    foreground_coordinator_observe_display_lock(false);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_MEETING);
}

static void test_ambient_restore_spares_alarm(void) {
    foreground_coordinator_init();
    (void)foreground_coordinator_acquire(FOREGROUND_OWNER_ALARM,
                                         FOREGROUND_PRIORITY_ALARM_DUE,
                                         FOREGROUND_SCENE_ALARM_RING);
    (void)foreground_coordinator_acquire(FOREGROUND_OWNER_COMMAND_RESULT,
                                         FOREGROUND_PRIORITY_RESULT,
                                         FOREGROUND_SCENE_COMMAND_RESULT);
    (void)foreground_coordinator_acquire(FOREGROUND_OWNER_MEETING,
                                         FOREGROUND_PRIORITY_PROGRESS,
                                         FOREGROUND_SCENE_MEETING_UPLOAD);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_ALARM);
    foreground_coordinator_observe_ambient_restored();
    foreground_lease_t alarm = {0};
    CHECK(foreground_coordinator_get_lease(FOREGROUND_OWNER_ALARM, &alarm));
    CHECK(alarm.held);
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_ALARM);
    foreground_lease_t result = {0};
    CHECK(foreground_coordinator_get_lease(FOREGROUND_OWNER_COMMAND_RESULT,
                                           &result));
    CHECK(!result.held);
    foreground_lease_t meeting = {0};
    CHECK(foreground_coordinator_get_lease(FOREGROUND_OWNER_MEETING, &meeting));
    CHECK(!meeting.held);
    /* Only the Alarm Service's own release retires the ringing lease. */
    CHECK(foreground_coordinator_release(FOREGROUND_OWNER_ALARM, alarm.token));
    CHECK(foreground_coordinator_current() == FOREGROUND_OWNER_NONE);
}

static void test_authoritative_flag_is_explicit_only(void) {
    foreground_coordinator_init();
    CHECK(!foreground_coordinator_is_authoritative());
    foreground_coordinator_set_authoritative(true);
    CHECK(foreground_coordinator_is_authoritative());
    foreground_coordinator_set_authoritative(false);
    CHECK(!foreground_coordinator_is_authoritative());
}

int main(void) {
    test_init_resets_and_defaults_shadow();
    test_invalid_owners_are_rejected();
    test_tokens_are_monotonic_and_never_zero();
    test_priority_selection_and_tie_break();
    test_expected_restore_skips_released_owner();
    test_shadow_observers_mirror_decisions();
    test_display_lock_borrow_rule();
    test_ambient_restore_spares_alarm();
    test_authoritative_flag_is_explicit_only();

    printf("foreground coordinator host regression: %d checks, %d failures\n",
           g_checks, g_failures);
    return g_failures == 0 ? 0 : 1;
}
