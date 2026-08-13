#pragma once

/* Private compact visual-profile boundary.  This is intentionally distinct
 * from the hardware HAL: screen geometry/transport stay in Display service,
 * while product artwork, layout and transport badge presentation live here.
 * The shared renderer consumes only this neutral visual contract. */

#include <stdbool.h>
#include <stdint.h>

#include "boards/compact_alarm_layout.h"
#include "boards/compact_profile_identity.h"
#include "boards/compact_profile_render_bridge.h"
#include "boards/compact_recording_layout.h"
#include "boards/compact_response_layout.h"
#include "boards/compact_standby_layout.h"
#include "boards/compact_upload_layout.h"

const compact_response_layout_t *compact_visual_profile_response_layout(void);
const compact_standby_layout_t *compact_visual_profile_standby_layout(void);
const compact_recording_layout_t *compact_visual_profile_recording_layout(void);
const compact_upload_layout_t *compact_visual_profile_upload_layout(void);
const compact_alarm_layout_t *compact_visual_profile_alarm_layout(void);
bool compact_visual_profile_render_startup_art(void);
bool compact_visual_profile_render_state_identity(
    const compact_profile_identity_state_t *identity, bool ambient, uint16_t background);
bool compact_visual_profile_render_status_identity(
    const compact_profile_identity_state_t *identity, const char *title, const char *line,
    uint16_t background);
bool compact_visual_profile_network_transport_is_cellular(void);
bool compact_visual_profile_publish_network_transport(bool cellular);
void compact_visual_profile_bind_renderer(const compact_profile_render_bridge_t *bridge);
