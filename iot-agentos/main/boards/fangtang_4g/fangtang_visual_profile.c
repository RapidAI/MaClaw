/* Fangtang-4G visual profile implementation.
 *
 * This is product artwork/layout composition only.  Hardware panel ownership
 * remains in compact_display_service and business scenes remain in the shared
 * compact renderer.
 */

#include "sdkconfig.h"
#include <string.h>
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "../compact_profile_render_bridge.h"
#include "fangtang_identity_art_renderer.h"
#include "fangtang_identity_composer.h"
#include "fangtang_identity_network_renderer.h"

extern const uint8_t _binary_fangtang_sugar_rgb565_start[];
extern const uint8_t _binary_fangtang_sugar_rgb565_end[];
extern const uint8_t _binary_fangtang_sugar_a8_start[];
extern const uint8_t _binary_fangtang_sugar_a8_end[];

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
#error "Fangtang visual profile may only be compiled for CONFIG_MACLAW_BOARD_FANGTANG_4G"
#endif

static compact_profile_render_bridge_t s_fangtang_renderer;

void fangtang_visual_profile_bind_renderer(const compact_profile_render_bridge_t *bridge) {
    if (bridge) s_fangtang_renderer = *bridge;
    else memset(&s_fangtang_renderer, 0, sizeof(s_fangtang_renderer));
}

static fangtang_identity_raster_t fangtang_identity_raster(void) {
    return (fangtang_identity_raster_t){
        .context = NULL,
        .panel_width = s_fangtang_renderer.panel_width,
        .panel_height = s_fangtang_renderer.panel_height,
        .allocate_bitmap = s_fangtang_renderer.allocate_bitmap,
        .free_bitmap = s_fangtang_renderer.free_bitmap,
        .draw_bitmap = s_fangtang_renderer.draw_bitmap,
        .fill_rect = s_fangtang_renderer.fill_rect,
    };
}

static bool fangtang_composer_draw_sugar(void *context,
                                         const fangtang_identity_raster_t *raster,
                                         int x, int y, int scale_percent,
                                         uint16_t background) {
    (void)context;
    return fangtang_identity_draw_sugar(
        raster, _binary_fangtang_sugar_rgb565_start,
        (size_t)(_binary_fangtang_sugar_rgb565_end - _binary_fangtang_sugar_rgb565_start),
        _binary_fangtang_sugar_a8_start,
        (size_t)(_binary_fangtang_sugar_a8_end - _binary_fangtang_sugar_a8_start),
        x, y, scale_percent, background);
}

static fangtang_identity_composer_t fangtang_identity_composer(void) {
    return (fangtang_identity_composer_t){
        .context = s_fangtang_renderer.context,
        .panel_width = s_fangtang_renderer.panel_width,
        .display_ready = s_fangtang_renderer.display_ready,
        .begin_frame = s_fangtang_renderer.begin_frame,
        .finish_frame = s_fangtang_renderer.finish_frame,
        .fill_screen = s_fangtang_renderer.fill_screen,
        .state_color = s_fangtang_renderer.state_color,
        .color = s_fangtang_renderer.color,
        .text24_width = s_fangtang_renderer.text24_width,
        .draw_ascii = s_fangtang_renderer.draw_ascii,
        .draw_text24 = s_fangtang_renderer.draw_text24,
        .draw_text24_centered = s_fangtang_renderer.draw_text24_centered,
        .draw_remote_pet = s_fangtang_renderer.draw_remote_pet,
        .network_is_cellular = s_fangtang_renderer.network_is_cellular,
        .art_raster = fangtang_identity_raster(),
        .network_raster = { .context = s_fangtang_renderer.context,
                            .fill_rect = s_fangtang_renderer.fill_rect },
        .draw_sugar = fangtang_composer_draw_sugar,
    };
}

bool fangtang_visual_profile_render_startup_art(void) {
    const fangtang_identity_composer_t composer = fangtang_identity_composer();
    return fangtang_identity_compose_startup(&composer);
}

bool fangtang_visual_profile_render_state_identity(const compact_profile_identity_state_t *identity,
                                                    bool ambient, uint16_t background) {
    const fangtang_identity_composer_t composer = fangtang_identity_composer();
    return fangtang_identity_compose_state(&composer, identity, ambient, background);
}

bool fangtang_visual_profile_render_status_identity(const compact_profile_identity_state_t *identity,
                                                     const char *title, const char *line,
                                                     uint16_t background) {
    const fangtang_identity_composer_t composer = fangtang_identity_composer();
    return fangtang_identity_compose_status(&composer, identity, title, line, background);
}
