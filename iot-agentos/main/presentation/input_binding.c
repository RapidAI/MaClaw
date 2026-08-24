#include "presentation/input_binding.h"

#include "esp_err.h"
#include "esp_log.h"
#include "esp_system.h"

#include "alarm_manager.h"
#include "presentation/scene_presenter.h"
#include "presentation/safe_mode_input_policy.h"
#include "services/audio_arbitration_service.h"
#include "configuration_service.h"
#include "fall_detection_service.h"
#include "services/command_service.h"
#include "services/interaction_service.h"
#include "services/meeting_service.h"
#include "services/reply_service.h"
#include "sleep_schedule_service.h"

/* Keep the log tag identical to the original main.c owner so existing input /
 * gesture trace filters and hardware baseline comparisons stay valid. */
static const char *TAG = "maclaw_client";

static input_binding_host_t s_host;
static bool s_host_installed;

void input_binding_handle_event(const app_intent_event_t *event) {
    if (!event || event->struct_size != sizeof(*event) ||
        event->abi_version != APP_INTENT_ABI_VERSION ||
        event->input_generation == 0) {
        ESP_LOGW(TAG, "discarded invalid app intent event");
        return;
    }
    app_intent_type_t action = event->type;
    device_input_source_t source = event->source;
    bool primary_interaction_source = event->primary_interaction_source;
    bool source_can_wake_display = event->display_wake_source;
    static bool suppress_alarm_dismiss_gesture;
    static device_input_source_t alarm_dismiss_source = DEVICE_INPUT_SOURCE_UNKNOWN;
    /* DISPLAY_OFF is exited on a physical down edge, while the input adapter
     * will still subsequently publish the completed short/double/long
     * gesture for that same contact.  Remember its source so waking the panel
     * cannot fall through into voice capture when the gesture completes. */
    static bool suppress_display_wake_gesture;
    static device_input_source_t display_wake_source = DEVICE_INPUT_SOURCE_UNKNOWN;

    /* Decide the retained-alarm/SAFE_MODE admission before any optional
     * foreground policy (fall prompt or command-capture stop) can observe a
     * newly arrived contact.  SAFE_MODE is published under the root's task
     * lock before its quiesce transaction begins; putting this value-only
     * decision here closes the same-event race rather than relying on those
     * nonessential services to have completed their asynchronous stop. */
    const safe_mode_input_route_t safe_mode_route = safe_mode_input_policy_route(
        alarm_manager_is_initialized(), alarm_manager_is_ringing(),
        primary_interaction_source,
        s_host_installed && s_host.safe_mode_active && s_host.safe_mode_active());
    if (safe_mode_route == SAFE_MODE_INPUT_ROUTE_DISMISS_ALARM) {
        alarm_manager_dismiss();
        if (action == APP_INTENT_PRIMARY_CONTACT_DOWN) {
            suppress_alarm_dismiss_gesture = true;
            alarm_dismiss_source = source;
        }
        ESP_LOGI(TAG, "ringing alarm dismissed by input source=%d action=%d",
                 (int)source, (int)action);
        return;
    }
    if (safe_mode_route == SAFE_MODE_INPUT_ROUTE_IGNORE_RINGING_ALARM) {
        ESP_LOGI(TAG, "input ignored while alarm rings: source=%d action=%d",
                 (int)source, (int)action);
        return;
    }

    /* DISPLAY_OFF is a presentation-only state.  Consume the first contact
     * from any profile-declared display-wake source at the shared business
     * boundary, before a recorder or meeting transition can observe it.  The
     * Power Service serializes this with the idle timer; the board adapter
     * restores its own round/rectangular ambient scene.  This remains
     * source-neutral: touch and physical controls follow the same
     * wake-then-act contract. */
    if (source_can_wake_display && scene_presenter_wake_from_idle()) {
        sleep_schedule_service_note_manual_wake();
        suppress_display_wake_gesture = true;
        display_wake_source = source;
        ESP_LOGI(TAG, "primary interaction consumed as display wake: source=%d action=%d",
                 (int)source, (int)action);
        return;
    }

    /* Consume the completed gesture emitted for the interaction that woke the
     * display.  Keep the barrier armed across every contact-down: a first
     * interaction can be a double tap/click, whose second down edge arrives
     * before the scanner emits its final SECONDARY action.  Clearing on that
     * edge would make a wake double-tap start a meeting/command. This keeps
     * the contract identical for touch, Bread/Fangtang's activation key, and
     * future primary controls: one whole initial gesture wakes only; the next
     * completed, deliberate gesture may invoke a command. */
    if (suppress_display_wake_gesture && source == display_wake_source) {
        if (action == APP_INTENT_PRIMARY_CONTACT_DOWN ||
            action == APP_INTENT_AUXILIARY_CONTACT_DOWN) {
            ESP_LOGD(TAG, "display-wake contact retained until gesture completes: source=%d",
                     (int)source);
        } else {
            suppress_display_wake_gesture = false;
            display_wake_source = DEVICE_INPUT_SOURCE_UNKNOWN;
            ESP_LOGI(TAG, "completed display-wake gesture consumed: source=%d action=%d",
                     (int)source, (int)action);
            return;
        }
    }

    /* DISPLAY_OFF is a presentation state, so its first interaction above is
     * still allowed to wake the retained diagnostic/alarm surface. Once the
     * panel is awake, every ordinary SAFE_MODE action is rejected before it
     * can reach a nonessential foreground service. */
    if (safe_mode_route == SAFE_MODE_INPUT_ROUTE_IGNORE_SAFE_MODE) {
        ESP_LOGI(TAG, "input ignored while SAFE_MODE is active: source=%d action=%d",
                 (int)source, (int)action);
        return;
    }

    /* A suspected-fall prompt is a local safety surface.  Accept the normal
     * primary interaction for every profile (touch or physical control) before
     * the gesture can enter voice/meeting policy.  Contact-down cancels early
     * on touch devices; a completed primary action performs the same action on
     * button-only devices. */
    if (primary_interaction_source && fall_detection_service_cancel_from_user()) {
        ESP_LOGI(TAG, "suspected-fall prompt cancelled by input source=%d action=%d",
                 (int)source, (int)action);
        scene_presenter_restore_standby();
        return;
    }

    if (command_service_consume_capture_stop_gesture(
            source, action == APP_INTENT_PRIMARY_CONTACT_DOWN ||
                    action == APP_INTENT_AUXILIARY_CONTACT_DOWN)) {
        return;
    }

    if (!s_host_installed || !s_host.startup_sequence_complete()) {
        // Startup owns the audio/display path until the optional greeting has
        // completed and the wake listener is loaded. Volume keys remain useful,
        // but activation gestures must not overtake this ordering boundary.
        // The configuration gesture is the exception: it is the maintenance
        // escape hatch precisely for the states that can never complete the
        // Welcome sequence (e.g. a saved Wi-Fi password that no longer
        // connects leaves the device on the "network unavailable" surface
        // with the sequence unfinished), so it must always reach the handler
        // that persists the setup request and reboots into the portal.
        if (action != APP_INTENT_INCREASE_VOLUME && action != APP_INTENT_DECREASE_VOLUME &&
            action != APP_INTENT_OPEN_CONFIGURATION) {
            ESP_LOGI(TAG, "input ignored until startup Welcome sequence completes");
            return;
        }
    }

    // A down edge dismisses immediately; consume the completed gesture from
    // that same contact so it cannot also start voice, cancel, or configure.
    if (suppress_alarm_dismiss_gesture &&
        action != APP_INTENT_PRIMARY_CONTACT_DOWN &&
        action != APP_INTENT_AUXILIARY_CONTACT_DOWN &&
        source == alarm_dismiss_source) {
        // A native double gesture may be followed by a delayed short from the
        // same contact-drain window. Keep suppression armed; the next real
        // down edge disarms it below before being handled normally.
        ESP_LOGI(TAG, "completed alarm-dismiss gesture consumed");
        return;
    }
    if (suppress_alarm_dismiss_gesture &&
        (action == APP_INTENT_PRIMARY_CONTACT_DOWN ||
         action == APP_INTENT_AUXILIARY_CONTACT_DOWN)) {
        suppress_alarm_dismiss_gesture = false;
        alarm_dismiss_source = DEVICE_INPUT_SOURCE_UNKNOWN;
    }
    // The down-edge action exists only for latency-sensitive foreground
    // surfaces. Preserve all established behavior on the completed gesture.
    if (action == APP_INTENT_PRIMARY_CONTACT_DOWN ||
        action == APP_INTENT_AUXILIARY_CONTACT_DOWN) {
        if (interaction_service_phase() == INTERACTION_SERVICE_RECORDING) {
            audio_arbitration_request_capture_stop();
            command_service_arm_capture_stop_gesture(source);
        }
        return;
    }
    if (action == APP_INTENT_INCREASE_VOLUME || action == APP_INTENT_DECREASE_VOLUME) {
        // On a response page the available upper side key advances through the
        // reply. This keeps one-key reading in the natural 1 -> 2 -> 3 order;
        // the board renderer wraps the final page back to page 1. If the lower
        // key is confirmed later, it can use the opposite direction.
        int page_delta = action == APP_INTENT_INCREASE_VOLUME ? 1 : -1;
        bool page_handled = scene_presenter_navigate_response(page_delta);
        ESP_LOGI(TAG, "volume key: %s page_delta=%d response_handled=%s",
                 action == APP_INTENT_INCREASE_VOLUME ? "up" : "down", page_delta,
                 page_handled ? "yes" : "no");
        if (page_handled) return;
        uint8_t volume = 0;
        int delta = action == APP_INTENT_INCREASE_VOLUME ? 10 : -10;
        device_status_t volume_status = audio_arbitration_adjust_output_volume(delta, &volume);
        if (volume_status == DEVICE_STATUS_OK) {
            ESP_LOGI(TAG, "output volume: %u%%", volume);
            int32_t save_err = s_host.persist_output_volume
                                   ? s_host.persist_output_volume(volume)
                                   : (int32_t)0;
            if (save_err != 0) {
                ESP_LOGW(TAG, "output volume persistence failed: %s",
                         esp_err_to_name((esp_err_t)save_err));
            }
        }
        return;
    }
    ESP_LOGI(TAG, "input action received: %s",
             action == APP_INTENT_PRIMARY_ACTIVATE ? "primary" :
             action == APP_INTENT_SECONDARY_ACTIVATE ? "secondary" : "configure");
    // The setup screen owns both the display and the radio. Treat touch/BOOT
    // input as inert until the submitted form deliberately restarts the
    // device; otherwise a stray tap starts normal voice UI and repaints the
    // QR while the phone is trying to configure the AP.
    if (device_connectivity_is_provisioning_active()) {
        ESP_LOGI(TAG, "button ignored while setup portal is active");
        return;
    }
    meeting_service_state_t meeting = meeting_service_state();
    /* Reconfiguration is the emergency/maintenance gesture and must take
     * precedence over voice, meeting and upload state. Previously a long hold
     * was detected correctly but silently consumed by the meeting guards. */
    if (action == APP_INTENT_OPEN_CONFIGURATION) {
        bool wifi_configured = s_host_installed && s_host.wifi_configured &&
                               s_host.wifi_configured();
        if (!wifi_configured && !device_connectivity_is_active_cellular()) {
            ESP_LOGI(TAG, "long press ignored while setup portal is active");
            return;
        }
        ESP_LOGW(TAG, "long press: configuration requested (meeting state=%d)", (int)meeting);
        /* Use a clean reboot as the transaction boundary. The next boot sees
         * the persisted setup request before starting STA/TLS, so it can enter
         * AP mode deterministically without racing an active long poll. */
        esp_err_t setup_err = device_status_to_platform_error(
            configuration_service_request_force_setup());
        if (setup_err == ESP_OK) {
            ESP_LOGW(TAG, "configuration request saved; rebooting into setup");
            esp_restart();
        }
        ESP_LOGE(TAG, "cannot persist configuration request: %s", esp_err_to_name(setup_err));
        if (meeting == MEETING_SERVICE_RECORDING || meeting == MEETING_SERVICE_PAUSED) {
            meeting_service_request_finalize();
        }
        if (s_host_installed && s_host.start_deferred_setup &&
            s_host.start_deferred_setup()) {
            ESP_LOGI(TAG, "configuration portal worker created");
        }
        return;
    }
    if (meeting == MEETING_SERVICE_RECORDING || meeting == MEETING_SERVICE_PAUSED) {
        // Stopping must work with the one dependable primary input fitted to
        // each enclosure: touch on EchoEar, or the activation key on Bread and
        // Fangtang. Accept every completed gesture as stop/save; a user should
        // not need a tight double tap while recording.
        // Do not repaint here: this callback runs in a hardware input task and
        // a full LCD DMA present can block it long enough to trip task_wdt. The
        // meeting task observes FINALIZING and owns the following UI updates.
        meeting_service_request_finalize();
        ESP_LOGI(TAG, "meeting stop requested: gesture=%s",
                 action == APP_INTENT_PRIMARY_ACTIVATE ? "primary" :
                 action == APP_INTENT_SECONDARY_ACTIVATE ? "secondary" : "configure");
        return;
    }
    if (meeting_service_is_active()) {
        ESP_LOGW(TAG, "button ignored: meeting transition/upload active");
        return;
    }
    if (action == APP_INTENT_SECONDARY_ACTIVATE) {
        interaction_service_phase_t interaction_phase = interaction_service_phase();
        if (interaction_service_worker_active() ||
            interaction_phase == INTERACTION_SERVICE_RECORDING ||
            interaction_phase == INTERACTION_SERVICE_PROCESSING) {
            // One foreground action owns the activation key until it reaches a
            // result. During processing a double press means cancel; during the
            // fixed-length capture it is simply consumed. It can never fall
            // through and start a meeting recording in either phase.
            if (interaction_phase == INTERACTION_SERVICE_PROCESSING) {
                (void)command_service_request_cancel();
            } else {
                ESP_LOGI(TAG, "secondary input consumed by command recording");
            }
            return;
        }
        if (meeting_service_pending()) {
            if (meeting_service_worker_running()) {
                // A worker is already transferring the retained file. Calling
                // meeting_service_start_recording() again only reports a busy
                // condition; it is not a network failure and must not be
                // labelled as one.
                scene_presenter_publish_message("会议记录续传中", "完成后可开始新会议");
            } else if (meeting_service_ensure_resume_supervisor()) {
                scene_presenter_publish_message("正在续传上次录音", "完成后可开始新会议");
            } else {
                scene_presenter_publish_pet_state("alert");
                scene_presenter_publish_message("续传任务未启动", "设备将稍后自动重试");
            }
            return;
        }
        if (!meeting_service_available()) {
            // A stale handshake must not permanently disable a local hardware
            // feature. Re-negotiate on demand, then continue the same double
            // tap if the current Hub advertises meeting recording.
            if (!meeting_service_refresh_capability()) {
                scene_presenter_publish_pet_state("alert");
                scene_presenter_publish_message("录音启动失败", "无法检查网关支持");
            }
            return;
        }
        // A previous answer may deliberately remain on screen after its task
        // completes. Release that presentation lock as part of the explicit
        // transition into meeting mode so old command UI cannot interleave
        // with the meeting recorder.
        command_service_set_display_locked(false);
        scene_presenter_publish_command_display_lock(false);
        if (!meeting_service_start_recording()) {
            scene_presenter_publish_pet_state("alert");
            scene_presenter_publish_message("录音启动失败", "设备正在处理其它操作");
        }
        return;
    }
    if (action != APP_INTENT_PRIMARY_ACTIVATE) return;
    // A completed short press during command capture requests the same stop
    // as its down edge. Without this branch the release gesture fell through
    // to interaction_service_start_voice() and was silently rejected by the
    // interaction lock.
    if (interaction_service_phase() == INTERACTION_SERVICE_RECORDING) {
        audio_arbitration_request_capture_stop();
        ESP_LOGI(TAG, "command recording stop requested by completed primary gesture");
        return;
    }
    // The result is a deliberate terminal step in the command flow. The first
    // activation press closes it and returns to the clock/date/weather screen;
    // only a later press starts a new recording. This avoids accidentally
    // recording while the user is still reading the answer.
    if (scene_presenter_dismiss_response()) {
        reply_service_dismiss_result_speech();
        interaction_service_set_phase(INTERACTION_SERVICE_IDLE);
        command_service_set_display_locked(false);
        // dismiss_response releases the board guard and publishes the
        // matching PET model atomically.  Calling the board port directly here
        // left the shared model on RESPONSE, so later ambient/profile updates
        // could reason about a result page that was no longer on screen.
        ESP_LOGI(TAG, "response dismissed; ambient screen restored");
        return;
    }
    // A physical press only wakes a sleeping LCD; the offline wake phrase is
    // hands-free and therefore wakes the panel and records in the same event.
    (void)interaction_service_start_voice(true);
}

device_status_t input_binding_init(const input_binding_host_t *host) {
    if (!host || !host->startup_sequence_complete || !host->wifi_configured ||
        !host->persist_output_volume || !host->start_deferred_setup ||
        !host->safe_mode_active) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    s_host = *host;
    s_host_installed = true;
    return DEVICE_STATUS_OK;
}
