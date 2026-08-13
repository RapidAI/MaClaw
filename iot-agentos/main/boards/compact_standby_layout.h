/* Physical standby-scene geometry for compact display profiles.
 *
 * These numbers describe only fixed display safe areas.  The shared renderer
 * still owns status wording, weather content, pet selection and animation.
 */
#pragma once

typedef struct {
    int weather_text_y;
    int weather_scale_num;
    int weather_scale_den;
    int pet_top;
    int pet_max_width;
    int native_pet_scale_percent;
} compact_standby_layout_t;
