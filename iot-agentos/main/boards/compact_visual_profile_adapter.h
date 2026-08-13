#pragma once

/* Compile-time compact visual-profile selection.  Included only by the
 * visual-profile source owner; shared scene code receives the normalized
 * compact_visual_profile_service contract instead of a product choice. */
#include "sdkconfig.h"

#include <stdbool.h>
#include <stdint.h>

#include "boards/compact_alarm_layout.h"
#include "boards/compact_profile_identity.h"
#include "boards/compact_profile_render_bridge.h"
#include "boards/compact_recording_layout.h"
#include "boards/compact_response_layout.h"
#include "boards/compact_standby_layout.h"
#include "boards/compact_upload_layout.h"

#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#include "boards/fangtang_4g/fangtang_alarm_layout_adapter.h"
#include "boards/fangtang_4g/fangtang_recording_layout_adapter.h"
#include "boards/fangtang_4g/fangtang_response_layout_adapter.h"
#include "boards/fangtang_4g/fangtang_standby_layout_adapter.h"
#include "boards/fangtang_4g/fangtang_upload_layout_adapter.h"

bool fangtang_visual_profile_render_startup_art(void);
bool fangtang_visual_profile_render_state_identity(
    const compact_profile_identity_state_t *identity, bool ambient, uint16_t background);
bool fangtang_visual_profile_render_status_identity(
    const compact_profile_identity_state_t *identity, const char *title, const char *line,
    uint16_t background);
bool fangtang_visual_profile_network_transport_is_cellular(void);
bool fangtang_visual_profile_publish_network_transport(bool cellular);
void fangtang_visual_profile_bind_renderer(const compact_profile_render_bridge_t *bridge);

static inline const compact_response_layout_t *compact_visual_profile_adapter_response_layout(void) {
    return fangtang_response_layout_adapter();
}
static inline const compact_standby_layout_t *compact_visual_profile_adapter_standby_layout(void) {
    return fangtang_standby_layout_adapter();
}
static inline const compact_recording_layout_t *compact_visual_profile_adapter_recording_layout(void) {
    return fangtang_recording_layout_adapter();
}
static inline const compact_upload_layout_t *compact_visual_profile_adapter_upload_layout(void) {
    return fangtang_upload_layout_adapter();
}
static inline const compact_alarm_layout_t *compact_visual_profile_adapter_alarm_layout(void) {
    return fangtang_alarm_layout_adapter();
}
static inline bool compact_visual_profile_adapter_render_startup_art(void) {
    return fangtang_visual_profile_render_startup_art();
}
static inline bool compact_visual_profile_adapter_render_state_identity(
    const compact_profile_identity_state_t *identity, bool ambient, uint16_t background) {
    return fangtang_visual_profile_render_state_identity(identity, ambient, background);
}
static inline bool compact_visual_profile_adapter_render_status_identity(
    const compact_profile_identity_state_t *identity, const char *title, const char *line,
    uint16_t background) {
    return fangtang_visual_profile_render_status_identity(identity, title, line, background);
}
static inline bool compact_visual_profile_adapter_network_transport_is_cellular(void) {
    return fangtang_visual_profile_network_transport_is_cellular();
}
static inline bool compact_visual_profile_adapter_publish_network_transport(bool cellular) {
    return fangtang_visual_profile_publish_network_transport(cellular);
}
static inline void compact_visual_profile_adapter_bind_renderer(
    const compact_profile_render_bridge_t *bridge) {
    fangtang_visual_profile_bind_renderer(bridge);
}
#else
#include "boards/bread_compact/bread_alarm_layout_adapter.h"
#include "boards/bread_compact/bread_recording_layout_adapter.h"
#include "boards/bread_compact/bread_response_layout_adapter.h"
#include "boards/bread_compact/bread_standby_layout_adapter.h"
#include "boards/bread_compact/bread_upload_layout_adapter.h"

static inline const compact_response_layout_t *compact_visual_profile_adapter_response_layout(void) {
    return bread_response_layout_adapter();
}
static inline const compact_standby_layout_t *compact_visual_profile_adapter_standby_layout(void) {
    return bread_standby_layout_adapter();
}
static inline const compact_recording_layout_t *compact_visual_profile_adapter_recording_layout(void) {
    return bread_recording_layout_adapter();
}
static inline const compact_upload_layout_t *compact_visual_profile_adapter_upload_layout(void) {
    return bread_upload_layout_adapter();
}
static inline const compact_alarm_layout_t *compact_visual_profile_adapter_alarm_layout(void) {
    return bread_alarm_layout_adapter();
}
static inline bool compact_visual_profile_adapter_render_startup_art(void) { return false; }
static inline bool compact_visual_profile_adapter_render_state_identity(
    const compact_profile_identity_state_t *identity, bool ambient, uint16_t background) {
    (void)identity;
    (void)ambient;
    (void)background;
    return false;
}
static inline bool compact_visual_profile_adapter_render_status_identity(
    const compact_profile_identity_state_t *identity, const char *title, const char *line,
    uint16_t background) {
    (void)identity;
    (void)title;
    (void)line;
    (void)background;
    return false;
}
static inline bool compact_visual_profile_adapter_network_transport_is_cellular(void) { return false; }
static inline bool compact_visual_profile_adapter_publish_network_transport(bool cellular) {
    (void)cellular;
    return false;
}
static inline void compact_visual_profile_adapter_bind_renderer(
    const compact_profile_render_bridge_t *bridge) {
    (void)bridge;
}
#endif
