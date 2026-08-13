#include "fangtang_identity_composer.h"

#include <stdio.h>
#include <string.h>

static bool sugar(const fangtang_identity_composer_t *c, int x, int y, int scale,
                  uint16_t background) {
    return c && c->draw_sugar && c->draw_sugar(c->context, &c->art_raster,
                                                x, y, scale, background);
}

static uint16_t rgb(const fangtang_identity_composer_t *c, uint8_t r, uint8_t g,
                    uint8_t b) {
    return c && c->color ? c->color(c->context, r, g, b) : 0;
}

bool fangtang_identity_compose_startup(const fangtang_identity_composer_t *c) {
    if (!c || !c->display_ready || !c->display_ready(c->context) || !c->state_color ||
        !c->begin_frame || !c->finish_frame || !c->fill_screen || !c->draw_text24_centered)
        return true;
    const uint16_t background = c->state_color(c->context, "idle");
    const bool composed = c->begin_frame(c->context);
    c->fill_screen(c->context, background);
    if (!sugar(c, 26, 8, 100, background))
        fangtang_identity_draw_cube(&c->art_raster, "startup", 0, 25, 3, 136, background);
    c->draw_text24_centered(c->context, 207, "MaClaw Mate", rgb(c, 244, 249, 253),
                            background, 11);
    c->finish_frame(c->context, composed);
    return true;
}

bool fangtang_identity_compose_state(const fangtang_identity_composer_t *c,
                                     const compact_profile_identity_state_t *identity,
                                     bool ambient, uint16_t background) {
    if (!c || !identity || !identity->state) return false;
    const char *state = identity->state;
    if (ambient) {
        if (!c->text24_width || !c->draw_ascii || !c->draw_text24 || !c->network_is_cellular ||
            !c->draw_remote_pet || !c->begin_frame || !c->finish_frame || !c->fill_screen) {
            return false;
        }
        /* This profile owns the complete ambient visual layout. The shared
         * renderer has already selected the logical scene, but its composed
         * framebuffer must be prepared before the profile draws clock, status
         * and remote pet into it. Returning true without this transaction used
         * to leave the prior startup/ready surface as the authoritative front
         * frame, preventing the multi-frame pet worker from presenting. */
        const bool composed = c->begin_frame(c->context);
        c->fill_screen(c->context, background);
        const char *clock = identity->ambient_time && identity->ambient_time[0]
                                ? identity->ambient_time : "--:--:--";
        const char *online = identity->gateway_ready ? "在线" : "等待";
        const int online_width = c->text24_width(c->context, online, 2);
        int first_x = (c->panel_width - (int)strlen(clock) * 18 - 8 - online_width) / 2;
        if (first_x < 2) first_x = 2;
        c->draw_ascii(c->context, first_x, 6, clock, rgb(c, 240, 248, 255), background);
        c->draw_text24(c->context, first_x + (int)strlen(clock) * 18 + 8, 4, online,
                       identity->gateway_ready ? rgb(c, 91, 224, 149) : rgb(c, 245, 184, 75),
                       background, 2);
        char calendar[64];
        snprintf(calendar, sizeof(calendar), "%s %s", identity->ambient_date ?: "",
                 identity->ambient_weekday ?: "");
        const bool cellular = c->network_is_cellular(c->context);
        const int calendar_width = c->text24_width(c->context, calendar, 9);
        int calendar_x = (c->panel_width - calendar_width - 9 -
                          fangtang_network_badge_width(cellular)) / 2;
        if (calendar_x < 2) calendar_x = 2;
        c->draw_text24(c->context, calendar_x, 32, calendar, rgb(c, 166, 194, 216),
                       background, 9);
        const uint16_t signal = identity->gateway_ready ? rgb(c, 91, 224, 149)
                                                         : rgb(c, 166, 194, 216);
        fangtang_network_draw_badge(&c->network_raster, calendar_x + calendar_width + 9,
                                    37, cellular, signal,
                                    cellular ? signal : rgb(c, 240, 248, 255), background);
        if (!c->draw_remote_pet(c->context, background) &&
            !sugar(c, 26, 68, 100, background))
            fangtang_identity_draw_cube(&c->art_raster, state, identity->animation_phase,
                                        36, 70, 120, background);
        c->finish_frame(c->context, composed);
        return true;
    }
    if (!c->draw_text24_centered) return false;
    const char *label = !strcmp(state, "listening") ? "正在听取" :
                        !strcmp(state, "thinking") ? identity->command_stage :
                        !strcmp(state, "speaking") ? "正在回复" :
                        !strcmp(state, "alert") ? "请注意" :
                        !strcmp(state, "done") ? "处理完成" : "方糖";
    if (!sugar(c, 43, 4, 82, background))
        fangtang_identity_draw_cube(&c->art_raster, state, identity->animation_phase,
                                    57, 5, 90, background);
    fangtang_identity_draw_activity(&c->art_raster, state, identity->animation_phase,
                                    c->panel_width / 2, 150, background);
    c->draw_text24_centered(c->context, 166, label, rgb(c, 248, 252, 255), background, 8);
    c->draw_text24_centered(c->context, 207,
                            !strcmp(state, "thinking") ? "双击激活键可取消" : "请稍候",
                            rgb(c, 145, 220, 235), background, 8);
    return true;
}

bool fangtang_identity_compose_status(const fangtang_identity_composer_t *c,
                                      const compact_profile_identity_state_t *identity,
                                      const char *title, const char *line,
                                      uint16_t background) {
    if (!c || !identity || !c->draw_text24_centered) return false;
    if (!sugar(c, 60, 0, 64, background))
        fangtang_identity_draw_cube(&c->art_raster, identity->state ?: "idle",
                                    identity->animation_phase, 70, 2, 72, background);
    c->draw_text24_centered(c->context, 112, title && title[0] ? title : "方糖",
                            rgb(c, 248, 252, 255), background, 9);
    c->draw_text24_centered(c->context, 154, line && line[0] ? line : "设备就绪",
                            rgb(c, 121, 210, 224), background, 9);
    c->draw_text24_centered(c->context, 208, "请使用激活键", rgb(c, 157, 184, 205),
                            background, 8);
    return true;
}
