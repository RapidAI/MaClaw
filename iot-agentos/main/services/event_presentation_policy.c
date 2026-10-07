/*
 * Device-side glue for the event presentation policy (plan N1-2).
 *
 * The policy itself is pure and lives in the header.  Only this translation
 * unit may reach for the device services, so the header keeps compiling on
 * the host regression with no ESP-IDF in scope (same split as latency_trace).
 */

#include "services/event_presentation_policy.h"

#include "services/foreground_coordinator.h"
#include "services/meeting_service.h"
#include "sleep_schedule_service.h"

/*
 * Observation mapping (the five section 4.2 columns are observed, never inferred
 * from pixels or audio levels):
 *
 *   FOREGROUND_OWNER_COMMAND_VOICE  -> listening  (capture/thinking locked surface)
 *   FOREGROUND_OWNER_COMMAND_RESULT -> speaking   (result page + result speech)
 *   FOREGROUND_OWNER_MEETING        -> meeting    (recording/upload/result flow)
 *   FOREGROUND_OWNER_NONE           -> standby    (ambient/PET owns the panel)
 *   STARTUP / SETUP / ALARM / UPDATE-> standby
 *
 * Meeting is deliberately taken from *any* meeting phase, not only from
 * `meeting_service_is_active()`: the hard rule exists to keep the recording
 * clean, and the upload/result phases belong to the same user-visible flow.
 * Being conservative here only downgrades an event to display-only - it can
 * never make the device start talking.
 *
 * Not modelled by section 4.2: a locally ringing alarm (FOREGROUND_OWNER_ALARM) is
 * reported as `standby`, i.e. it is deliberately NOT treated as `speaking`.
 * That keeps an `interrupt` event from preempting a ringing alarm - cutting
 * off the thing whose whole job is to wake someone is worse than overlapping
 * it, and audio arbitration already serialises the two.  Flagged here so the
 * next revision can decide it instead of discovering it in the field.
 */
void event_presentation_current_flags(event_device_state_flags_t *out_flags) {
    if (!out_flags) return;

    event_device_state_flags_t flags = {0};

    switch (foreground_coordinator_current()) {
        case FOREGROUND_OWNER_COMMAND_VOICE:
            flags.listening = true;
            break;
        case FOREGROUND_OWNER_COMMAND_RESULT:
            flags.speaking = true;
            break;
        case FOREGROUND_OWNER_MEETING:
            flags.meeting_recording = true;
            break;
        default:
            /* Ambient/startup/setup/update/alarm: no column in the matrix. */
            break;
    }

    /* The recorder can outlive the foreground lease (upload/result handoff),
     * so ask the recorder directly as well. */
    if (meeting_service_is_active()) {
        flags.meeting_recording = true;
    }

    /* Quiet hours are the sleep schedule's active window.  Read through the
     * status snapshot rather than the raw config so a manual wake override is
     * honoured: the user who just tapped the screen is not asleep. */
    sleep_schedule_status_t sleep = {0};
    sleep_schedule_service_get_status(&sleep);
    if (sleep.active_window && !sleep.override_active) {
        flags.quiet_hours = true;
    }

    *out_flags = flags;
}
