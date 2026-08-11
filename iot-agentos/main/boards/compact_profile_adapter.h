#pragma once

/*
 * Selected compact-profile seam.
 *
 * The shared compact renderer consumes normalized display, input, audio and
 * layout contracts.  This private selector is the sole place that maps the
 * configured profile to its concrete adapter headers, so future compact
 * hardware does not require adding another include/layout conditional to the
 * business-facing compact renderer.
 */

#include "sdkconfig.h"
#include "compact_profile_identity.h"
#include "compact_profile_render_bridge.h"
#include "compact_startup_art.h"

#if CONFIG_MACLAW_BOARD_FANGTANG_4G

#include "fangtang_4g/fangtang_display_adapter.h"
#include "fangtang_4g/fangtang_audio_adapter.h"
#include "fangtang_4g/fangtang_input_adapter.h"
#include "fangtang_4g/fangtang_recording_layout_adapter.h"
#include "fangtang_4g/fangtang_response_layout_adapter.h"
#include "fangtang_4g/fangtang_standby_layout_adapter.h"
#include "fangtang_4g/fangtang_upload_layout_adapter.h"
#include "fangtang_4g/fangtang_alarm_layout_adapter.h"
#include "fangtang_4g/fangtang_peripheral_adapter.h"
#include "fangtang_4g/fangtang_cellular_adapter.h"

static inline const compact_response_layout_t *compact_profile_response_layout(void) {
    return fangtang_response_layout_adapter();
}

static inline const compact_standby_layout_t *compact_profile_standby_layout(void) {
    return fangtang_standby_layout_adapter();
}

static inline const compact_recording_layout_t *compact_profile_recording_layout(void) {
    return fangtang_recording_layout_adapter();
}

static inline const compact_upload_layout_t *compact_profile_upload_layout(void) {
    return fangtang_upload_layout_adapter();
}

static inline const compact_alarm_layout_t *compact_profile_alarm_layout(void) {
    return fangtang_alarm_layout_adapter();
}

/* Fangtang's alpha-composed sugar mark is implemented by its selected
 * transition unit because it uses that profile's private asset/raster code. */
bool compact_profile_render_startup_art(void);

/* These are product-identity compositions, not state transitions.  The
 * shared renderer has already opened/fillled its frame and remains owner of
 * all scene state, presentation, locking and redraw policy. */
bool compact_profile_render_state_identity(const compact_profile_identity_state_t *identity,
                                           bool ambient,
                                           uint16_t background);
bool compact_profile_render_status_identity(const compact_profile_identity_state_t *identity,
                                            const char *title, const char *line,
                                            uint16_t background);

/* Transport is a shared connectivity fact. Only this profile maps it to a
 * standby identity; shared renderer retains its lock, redraw admission and
 * all scene state. */
bool compact_profile_network_transport_is_cellular(void);
bool compact_profile_publish_network_transport(bool cellular);
/* Called by the shared compact renderer after its display primitives become
 * valid. The selected Fangtang composer copies this value-only bridge and
 * never reaches into the renderer's static symbols. */
void compact_profile_bind_renderer(const compact_profile_render_bridge_t *bridge);

#elif CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD

#include "bread_compact/bread_display_adapter.h"
#include "bread_compact/bread_audio_adapter.h"
#include "bread_compact/bread_input_adapter.h"
#include "bread_compact/bread_recording_layout_adapter.h"
#include "bread_compact/bread_response_layout_adapter.h"
#include "bread_compact/bread_standby_layout_adapter.h"
#include "bread_compact/bread_upload_layout_adapter.h"
#include "bread_compact/bread_alarm_layout_adapter.h"
#include "bread_compact/bread_peripheral_adapter.h"
#include "bread_compact/bread_connectivity_adapter.h"

static inline const compact_response_layout_t *compact_profile_response_layout(void) {
    return bread_response_layout_adapter();
}

static inline const compact_standby_layout_t *compact_profile_standby_layout(void) {
    return bread_standby_layout_adapter();
}

static inline const compact_recording_layout_t *compact_profile_recording_layout(void) {
    return bread_recording_layout_adapter();
}

static inline const compact_upload_layout_t *compact_profile_upload_layout(void) {
    return bread_upload_layout_adapter();
}

static inline const compact_alarm_layout_t *compact_profile_alarm_layout(void) {
    return bread_alarm_layout_adapter();
}

/* Bread supplies a validated immutable full frame through its display
 * adapter, so it has no composed boot-art phase. */
static inline bool compact_profile_render_startup_art(void) {
    return false;
}

/* Bread uses the shared robot identity. */
static inline bool compact_profile_render_state_identity(
                                                         const compact_profile_identity_state_t *identity,
                                                         bool ambient,
                                                         uint16_t background) {
    (void)identity;
    (void)ambient;
    (void)background;
    return false;
}

static inline bool compact_profile_render_status_identity(
                                                          const compact_profile_identity_state_t *identity,
                                                          const char *title,
                                                          const char *line,
                                                          uint16_t background) {
    (void)identity;
    (void)title;
    (void)line;
    (void)background;
    return false;
}

static inline bool compact_profile_network_transport_is_cellular(void) {
    return false;
}

static inline bool compact_profile_publish_network_transport(bool cellular) {
    (void)cellular;
    return false;
}

static inline void compact_profile_bind_renderer(
    const compact_profile_render_bridge_t *bridge) {
    (void)bridge;
}

#else
#error "Compact profile adapter requires Bread Compact or Fangtang-4G"
#endif

static inline compact_startup_full_frame_t compact_profile_startup_full_frame(void) {
    return compact_display_adapter_startup_full_frame();
}
