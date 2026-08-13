#include "compact_visual_profile_service.h"

/* Product artwork and safe-area layout are profile-private.  This source
 * owns their normalized service contract; the selector owns every board
 * choice, so a new compact profile never adds a branch to shared scene code. */
#include "boards/compact_visual_profile_adapter.h"

const compact_response_layout_t *compact_visual_profile_response_layout(void) {
    return compact_visual_profile_adapter_response_layout();
}
const compact_standby_layout_t *compact_visual_profile_standby_layout(void) {
    return compact_visual_profile_adapter_standby_layout();
}
const compact_recording_layout_t *compact_visual_profile_recording_layout(void) {
    return compact_visual_profile_adapter_recording_layout();
}
const compact_upload_layout_t *compact_visual_profile_upload_layout(void) {
    return compact_visual_profile_adapter_upload_layout();
}
const compact_alarm_layout_t *compact_visual_profile_alarm_layout(void) {
    return compact_visual_profile_adapter_alarm_layout();
}
bool compact_visual_profile_render_startup_art(void) {
    return compact_visual_profile_adapter_render_startup_art();
}
bool compact_visual_profile_render_state_identity(
    const compact_profile_identity_state_t *identity, bool ambient, uint16_t background) {
    return compact_visual_profile_adapter_render_state_identity(identity, ambient, background);
}
bool compact_visual_profile_render_status_identity(
    const compact_profile_identity_state_t *identity, const char *title, const char *line,
    uint16_t background) {
    return compact_visual_profile_adapter_render_status_identity(identity, title, line, background);
}
bool compact_visual_profile_network_transport_is_cellular(void) {
    return compact_visual_profile_adapter_network_transport_is_cellular();
}
bool compact_visual_profile_publish_network_transport(bool cellular) {
    return compact_visual_profile_adapter_publish_network_transport(cellular);
}
void compact_visual_profile_bind_renderer(const compact_profile_render_bridge_t *bridge) {
    compact_visual_profile_adapter_bind_renderer(bridge);
}
