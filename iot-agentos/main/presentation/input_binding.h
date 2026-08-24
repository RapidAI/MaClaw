#pragma once

/*
 * Presentation-layer input binding.
 *
 * Owns the business dispatch of abstracted input intents that used to live in
 * main.c's app-intent callback: volume/page keys, meeting gestures, command
 * cancel, alarm dismissal, the configuration long-press, display-wake gesture
 * consumption and the capture-stop barrier collaboration.  It receives
 * already-abstracted intents (app_intent_event_t carries no GPIO, touch
 * coordinate or board identity) and routes them to the business services'
 * typed APIs; it never touches a board adapter or legacy seam.
 *
 * The public contract exposes value types only.  The few composition-root
 * facts the dispatch needs (startup gate, stored Wi-Fi presence, volume
 * persistence, deferred setup worker) arrive through the host table.
 */

#include <stdbool.h>
#include <stdint.h>

#include "app_intent_service.h"
#include "device_api.h"

typedef struct {
    /* Startup owns the audio/display path until the Welcome sequence has
     * completed; activation gestures must not overtake that boundary. */
    bool (*startup_sequence_complete)(void);
    /* A stored station profile exists (used by the configuration gesture). */
    bool (*wifi_configured)(void);
    /* Persists the adjusted output volume (0 on success). */
    int32_t (*persist_output_volume)(uint8_t percent);
    /* Starts the deferred configuration-portal worker. */
    bool (*start_deferred_setup)(void);
    /* SAFE_MODE retains only local alarm dismissal after a proven minimum
     * service set has been composed.  The binding must not let a surviving
     * touch/key gesture re-open voice, meeting, provisioning or Gateway work. */
    bool (*safe_mode_active)(void);
} input_binding_host_t;

device_status_t input_binding_init(const input_binding_host_t *host);
/* Single dispatch entry for the App Intent consumer chain. It runs on the
 * shared App Intent dispatcher, never on a board scanner task; gestures that
 * paint or block are delegated to the owning services exactly as the legacy
 * main.c handler did. */
void input_binding_handle_event(const app_intent_event_t *event);
