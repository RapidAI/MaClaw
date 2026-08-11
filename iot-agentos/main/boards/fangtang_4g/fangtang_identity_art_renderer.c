/* Fangtang-private sugar/cube product-art renderer. */

#include "fangtang_identity_art_renderer.h"

#include <math.h>
#include <string.h>

#define FANGTANG_SUGAR_WIDTH 188
#define FANGTANG_SUGAR_HEIGHT 164

static uint16_t fangtang_color(uint8_t red, uint8_t green, uint8_t blue) {
    uint16_t native = (uint16_t)(((red & 0xf8u) << 8) |
                                 ((green & 0xfcu) << 3) | (blue >> 3));
    return (uint16_t)((native << 8) | (native >> 8));
}

static uint16_t fangtang_mix(uint8_t ar, uint8_t ag, uint8_t ab,
                             uint8_t br, uint8_t bg, uint8_t bb, unsigned mix) {
    if (mix > 255u) mix = 255u;
    const unsigned inverse = 255u - mix;
    return fangtang_color((uint8_t)((ar * inverse + br * mix) / 255u),
                          (uint8_t)((ag * inverse + bg * mix) / 255u),
                          (uint8_t)((ab * inverse + bb * mix) / 255u));
}

static bool fangtang_triangle(float px, float py, float ax, float ay,
                              float bx, float by, float cx, float cy) {
    const float ab = (px - ax) * (by - ay) - (py - ay) * (bx - ax);
    const float bc = (px - bx) * (cy - by) - (py - by) * (cx - bx);
    const float ca = (px - cx) * (ay - cy) - (py - cy) * (ax - cx);
    return (ab >= 0.0f && bc >= 0.0f && ca >= 0.0f) ||
           (ab <= 0.0f && bc <= 0.0f && ca <= 0.0f);
}

static float fangtang_segment_distance_sq(float px, float py, float ax, float ay,
                                          float bx, float by) {
    float dx = bx - ax;
    float dy = by - ay;
    const float length_sq = dx * dx + dy * dy;
    float t = length_sq > 0.0f ? ((px - ax) * dx + (py - ay) * dy) / length_sq : 0.0f;
    if (t < 0.0f) t = 0.0f;
    if (t > 1.0f) t = 1.0f;
    dx = px - (ax + t * dx);
    dy = py - (ay + t * dy);
    return dx * dx + dy * dy;
}

static uint32_t fangtang_texture_hash(unsigned x, unsigned y) {
    uint32_t value = x * 0x45d9f3bu ^ y * 0x119de1f3u ^ 0x9e3779b9u;
    value ^= value >> 16;
    value *= 0x45d9f3bu;
    return value ^ (value >> 16);
}

static bool fangtang_draw(const fangtang_identity_raster_t *raster, int x, int y,
                          int width, int height, const uint16_t *pixels) {
    return raster && raster->draw_bitmap && pixels && width > 0 && height > 0 &&
           x >= 0 && y >= 0 && x + width <= raster->panel_width &&
           y + height <= raster->panel_height &&
           raster->draw_bitmap(raster->context, x, y, width, height, pixels);
}

static void fangtang_fill(const fangtang_identity_raster_t *raster, int x, int y,
                          int width, int height, uint16_t color) {
    if (raster && raster->fill_rect && width > 0 && height > 0)
        raster->fill_rect(raster->context, x, y, width, height, color);
}

void fangtang_identity_draw_cube(const fangtang_identity_raster_t *raster,
                                 const char *state, unsigned phase, int offset_x,
                                 int offset_y, int scale_percent, uint16_t bg) {
    if (!raster || !raster->allocate_bitmap || !raster->free_bitmap || scale_percent <= 0)
        return;
    const int width = (140 * scale_percent + 99) / 100;
    const int height = (130 * scale_percent + 99) / 100;
    if (width <= 0 || height <= 0 || offset_x < 0 || offset_y < 0 ||
        offset_x + width > raster->panel_width || offset_y + height > raster->panel_height)
        return;
    uint16_t *bitmap = raster->allocate_bitmap(raster->context,
                                                (size_t)width * height * sizeof(*bitmap));
    if (!bitmap) return;
    const bool listening = state && !strcmp(state, "listening");
    const bool thinking = state && !strcmp(state, "thinking");
    const bool speaking = state && !strcmp(state, "speaking");
    const bool alert = state && !strcmp(state, "alert");
    const bool done = state && !strcmp(state, "done");
    const uint8_t accent_r = alert ? 255 : thinking ? 181 : done ? 88 : 70;
    const uint8_t accent_g = alert ? 91 : thinking ? 152 : done ? 232 : 213;
    const uint8_t accent_b = alert ? 82 : thinking ? 255 : done ? 158 : 246;
    const float ax = 70, ay = 7, bx = 128, by = 38, cx = 70, cy = 69, dx = 12, dy = 38;
    const float ex = 12, ey = 91, fx = 70, fy = 122, gx = 128, gy = 91;
    for (int py = 0; py < height; ++py) {
        const float y = ((float)py + 0.5f) * 100.0f / scale_percent;
        for (int px = 0; px < width; ++px) {
            const float x = ((float)px + 0.5f) * 100.0f / scale_percent;
            uint16_t pixel = bg;
            const float sx = (x - 70.0f) / 53.0f, sy = (y - 121.0f) / 8.0f;
            if (sx * sx + sy * sy < 1.0f) {
                const unsigned shadow = (unsigned)(34.0f + (1.0f - sx * sx - sy * sy) * 42.0f);
                pixel = fangtang_mix(18, 24, 38, 72, 78, 84, shadow);
            }
            const bool top = fangtang_triangle(x, y, ax, ay, bx, by, cx, cy) ||
                             fangtang_triangle(x, y, ax, ay, cx, cy, dx, dy);
            const bool left = fangtang_triangle(x, y, dx, dy, cx, cy, fx, fy) ||
                              fangtang_triangle(x, y, dx, dy, fx, fy, ex, ey);
            const bool right = fangtang_triangle(x, y, cx, cy, bx, by, gx, gy) ||
                               fangtang_triangle(x, y, cx, cy, gx, gy, fx, fy);
            if (top) { const unsigned shade = (unsigned)(248 - y * 10 / 69); pixel = fangtang_color(shade, shade - 2, shade - 12); }
            else if (left) { const unsigned shade = (unsigned)(230 - (y - 38) * 20 / 84); pixel = fangtang_color(shade, shade - 4, shade - 16); }
            else if (right) { const unsigned shade = (unsigned)(213 - (y - 38) * 18 / 84); pixel = fangtang_color(shade, shade - 4, shade - 13); }
            if (top || left || right) {
                const uint32_t grain = fangtang_texture_hash((unsigned)(x * 3.0f), (unsigned)(y * 3.0f));
                if ((grain & 0x3fu) == 0u) { const unsigned pore = 164u + ((grain >> 8) & 0x0fu); pixel = fangtang_color(pore, pore, pore - 5u); }
                else if ((grain & 0x1fu) == 1u) { const unsigned crystal = 248u + ((grain >> 8) & 0x03u); pixel = fangtang_color(crystal, crystal, crystal - 4u); }
            }
            float seam = fangtang_segment_distance_sq(x, y, ax, ay, bx, by);
            const float segments[][4] = {{bx,by,gx,gy},{gx,gy,fx,fy},{fx,fy,ex,ey},{ex,ey,dx,dy},{dx,dy,ax,ay},{dx,dy,cx,cy},{cx,cy,bx,by},{cx,cy,fx,fy}};
            for (size_t i = 0; i < sizeof(segments) / sizeof(segments[0]); ++i) { const float d = fangtang_segment_distance_sq(x, y, segments[i][0], segments[i][1], segments[i][2], segments[i][3]); if (d < seam) seam = d; }
            if ((top || left || right) && seam < 2.3f) { const unsigned edge = seam < 0.55f ? 174u : 204u; pixel = fangtang_color(edge, edge - 3u, edge - 10u); }
            static const float crystal_x[7] = {42,55,68,82,96,61,84};
            static const float crystal_y[7] = {37,27,18,27,38,44,47};
            for (int i = 0; i < 7 && top; ++i) { const float qx=x-crystal_x[i], qy=y-crystal_y[i], radius=1.4f+(float)(i%3)*0.45f; if (qx*qx+qy*qy < radius*radius) pixel = i == (int)(phase % 7u) && thinking ? fangtang_color(accent_r, accent_g, accent_b) : fangtang_color(188,185,174); }
            if (left || right) {
                if (alert && x > 67 && x < 73 && y > 78 && y < 101) pixel = fangtang_color(accent_r, accent_g, accent_b);
                else if (done && (fangtang_segment_distance_sq(x,y,51,87,64,99) < 3.0f || fangtang_segment_distance_sq(x,y,64,99,91,75) < 3.0f)) pixel = fangtang_color(accent_r, accent_g, accent_b);
                else if (listening || speaking) { const int bar=(int)((x-48)/11); const int bar_height=speaking ? 9+((bar+(int)phase)%3)*6 : 8+(bar%2)*5; if (bar >= 0 && bar < 5 && fabsf(x-(52+bar*11)) < 2.3f && y > 91-bar_height/2 && y < 91+bar_height/2) pixel=fangtang_color(accent_r, accent_g, accent_b); }
                else if (thinking) { static const float dots[3]={55,70,85}; for (int i=0;i<3;++i) { const float qx=x-dots[i], qy=y-91, radius=i==(int)(phase%3u)?4.0f:2.4f; if(qx*qx+qy*qy<radius*radius) pixel=fangtang_color(accent_r,accent_g,accent_b); } }
            }
            bitmap[(size_t)py * width + px] = pixel;
        }
    }
    (void)fangtang_draw(raster, offset_x, offset_y, width, height, bitmap);
    raster->free_bitmap(raster->context, bitmap);
}

bool fangtang_identity_draw_sugar(const fangtang_identity_raster_t *raster,
                                  const uint8_t *rgb, size_t rgb_bytes,
                                  const uint8_t *alpha, size_t alpha_bytes,
                                  int offset_x, int offset_y, int scale_percent,
                                  uint16_t bg) {
    const size_t source_pixels = (size_t)FANGTANG_SUGAR_WIDTH * FANGTANG_SUGAR_HEIGHT;
    if (!raster || !rgb || !alpha || !raster->allocate_bitmap || !raster->free_bitmap ||
        rgb_bytes != source_pixels * sizeof(uint16_t) || alpha_bytes != source_pixels ||
        scale_percent <= 0) return false;
    const int width = (FANGTANG_SUGAR_WIDTH * scale_percent + 99) / 100;
    const int height = (FANGTANG_SUGAR_HEIGHT * scale_percent + 99) / 100;
    if (width <= 0 || height <= 0 || offset_x < 0 || offset_y < 0 ||
        offset_x + width > raster->panel_width || offset_y + height > raster->panel_height) return false;
    uint16_t *bitmap = raster->allocate_bitmap(raster->context, (size_t)width * height * sizeof(*bitmap));
    if (!bitmap) return false;
    const uint16_t bg_native = (uint16_t)((bg << 8) | (bg >> 8));
    const uint8_t bg_r=(uint8_t)((bg_native>>11)*255/31), bg_g=(uint8_t)(((bg_native>>5)&0x3f)*255/63), bg_b=(uint8_t)((bg_native&0x1f)*255/31);
    const uint16_t *source = (const uint16_t *)rgb;
    for (int y=0;y<height;++y) for (int x=0;x<width;++x) {
        const size_t source_index=(size_t)(y*FANGTANG_SUGAR_HEIGHT/height)*FANGTANG_SUGAR_WIDTH + x*FANGTANG_SUGAR_WIDTH/width;
        const uint8_t a=alpha[source_index];
        if (!a) { bitmap[(size_t)y*width+x]=bg; continue; }
        const uint16_t native=(uint16_t)((source[source_index]<<8)|(source[source_index]>>8));
        const uint8_t fr=(uint8_t)((native>>11)*255/31), fg=(uint8_t)(((native>>5)&0x3f)*255/63), fb=(uint8_t)((native&0x1f)*255/31);
        bitmap[(size_t)y*width+x]=fangtang_color((uint8_t)(((unsigned)fr*a+(unsigned)bg_r*(255u-a)+127u)/255u), (uint8_t)(((unsigned)fg*a+(unsigned)bg_g*(255u-a)+127u)/255u), (uint8_t)(((unsigned)fb*a+(unsigned)bg_b*(255u-a)+127u)/255u));
    }
    const bool drawn=fangtang_draw(raster, offset_x, offset_y, width, height, bitmap);
    raster->free_bitmap(raster->context, bitmap);
    return drawn;
}

void fangtang_identity_draw_activity(const fangtang_identity_raster_t *raster,
                                     const char *state, unsigned phase, int center_x,
                                     int center_y, uint16_t bg) {
    const bool thinking=state && !strcmp(state,"thinking"), listening=state && !strcmp(state,"listening"), speaking=state && !strcmp(state,"speaking");
    const uint16_t active=thinking ? fangtang_color(196,169,255) : listening ? fangtang_color(96,220,255) : speaking ? fangtang_color(104,240,170) : fangtang_color(220,225,230);
    if (thinking) { for(int i=0;i<3;++i) { const int radius=i==(int)(phase%3u)?4:2, dot_x=center_x+(i-1)*15; for(int y=-4;y<=4;++y) for(int x=-4;x<=4;++x) if(x*x+y*y<=radius*radius) fangtang_fill(raster,dot_x+x,center_y+y,1,1,active); } return; }
    if (listening || speaking) { for(int i=0;i<5;++i) { const int height=speaking ? 5+((i+(int)phase)%3)*4 : 5+(i%2)*4; fangtang_fill(raster,center_x-22+i*10,center_y-height/2,3,height,active); } return; }
    fangtang_fill(raster,center_x-14,center_y-1,28,2,bg);
}
