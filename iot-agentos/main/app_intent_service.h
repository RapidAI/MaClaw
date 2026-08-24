#pragma once

/*
 * Application-facing input boundary.
 *
 * Board adapters publish Device Input events; this service owns the one
 * binding table that turns them into MaClaw business intents.  Application
 * policy must depend on this header, never on gesture recognizers, GPIO keys
 * or a particular touch controller.
 */

#include "device_api.h"

#define APP_INTENT_ABI_VERSION 3u

typedef enum {
    APP_INTENT_PRIMARY_ACTIVATE = 0,
    APP_INTENT_SECONDARY_ACTIVATE,
    APP_INTENT_OPEN_CONFIGURATION,
    APP_INTENT_INCREASE_VOLUME,
    APP_INTENT_DECREASE_VOLUME,
    APP_INTENT_PRIMARY_CONTACT_DOWN,
    APP_INTENT_AUXILIARY_CONTACT_DOWN,
} app_intent_type_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    /* Preserves the producer lifetime together with input_sequence, so
     * asynchronous policy/diagnostic consumers never conflate a reused
     * sequence from a later Input Service generation with an older event. */
    uint32_t input_generation;
    uint32_t input_sequence;
    uint64_t timestamp_us;
    app_intent_type_t type;
    /* Binding-owned classification of the standard local interaction. This
     * prevents application policy from querying a profile to infer it. */
    bool primary_interaction_source;
    /* Binding-owned DISPLAY_OFF wake eligibility. A profile can expose both
     * touch and a physical activation control without leaking either board
     * detail into application policy. */
    bool display_wake_source;
    /* Preserved only for policies such as consuming a single physical
     * contact after an alarm/capture stop. It is not a board identifier. */
    device_input_source_t source;
} app_intent_event_t;

typedef void (*app_intent_cb_t)(const app_intent_event_t *event, void *context);

/* Read-only queue diagnostics. `critical_overflow` is sticky for the active
 * service lifetime: it says a safety-relevant reservation was exhausted and
 * must not be hidden by later successful input. */
typedef struct {
    bool started;
    bool critical_overflow;
    uint32_t critical_pending;
    uint32_t control_pending;
    uint32_t auxiliary_pending;
    uint32_t dropped_critical;
    uint32_t dropped_control;
    uint32_t dropped_auxiliary;
} app_intent_service_snapshot_t;

/* Installs the complete shared binding and starts Device Input delivery.
 * Repeated start/stop behavior follows device_input_start/stop. */
device_status_t app_intent_service_start(app_intent_cb_t on_intent, void *context);
device_status_t app_intent_service_stop(uint32_t timeout_ms);
/* Reversible System Sleep boundary for the profile-neutral input-to-business
 * dispatcher. PREPARE rejects new semantic dispatch and drains a handler that
 * already crossed admission; it deliberately does not stop board scanners or
 * configure wake pins. ABORT restores the same live service generation. */
device_status_t app_intent_service_prepare_system_sleep(uint32_t timeout_ms);
void app_intent_service_abort_system_sleep_prepare(void);
bool app_intent_service_get_snapshot(app_intent_service_snapshot_t *out_snapshot);
