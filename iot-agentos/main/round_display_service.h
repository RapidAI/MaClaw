#pragma once

/*
 * Private circular-board Display HAL implementation boundary.
 *
 * Scene composition remains in board_port.c.  The selected profile's panel,
 * bus, DMA completion fence, memory-placement and backlight/sleep ordering
 * are owned by round_display_service.c.  This deliberately is not part of
 * the Device or Platform API.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

/* Normalized physical-panel facts.  The shared round renderer uses these to
 * size neutral scene buffers, but it never learns a controller, bus or
 * profile identity.  In particular, transfer stripe height belongs here --
 * it is a DMA/transport limit, not a visual-layout preference. */
int round_display_service_width(void);
int round_display_service_height(void);
int round_display_service_transfer_stripe_rows(void);
uint32_t round_display_service_pet_animation_frame_ms(void);

/* Initialize the selected panel at a semantic brightness level.  The adapter
 * derives its own SPI/QSPI DMA capacity from its physical geometry and
 * transport stripe; framebuffer allocation belongs to the scene renderer and
 * must never parameterize panel-bus setup. */
esp_err_t round_display_service_initialize(unsigned brightness);
bool round_display_service_ready(void);
/*
 * Display ownership transaction.  The service retains the requested
 * brightness while the panel is off, then applies it only as part of the
 * profile-qualified wake sequence.  Scene code therefore never needs to
 * choose between a PWM write and a controller DCS write, or remember a
 * brightness value solely for a later wake.
 */
esp_err_t round_display_service_set_brightness(unsigned percent);
esp_err_t round_display_service_enter_display_off(void);
esp_err_t round_display_service_wake_from_display_off(void);
esp_err_t round_display_service_draw_bitmap_sync(int x0, int y0, int x1, int y1,
                                                  const void *pixels);
uint16_t *round_display_service_stripe_buffer(void);
uint16_t round_display_service_rgb565(uint8_t r, uint8_t g, uint8_t b);
uint16_t round_display_service_rgb565_lerp(uint16_t from, uint16_t to,
                                           uint8_t amount);
void round_display_service_align_dirty_columns(int *left, int *right, int width);
uint16_t *round_display_service_allocate_framebuffer(size_t bytes);
uint16_t *round_display_service_allocate_ambient_overlay(size_t bytes);
void round_display_service_free_render_buffer(void *buffer);
uint8_t *round_display_service_allocate_remote_pet_frame(size_t bytes);
void round_display_service_free_remote_pet_frame(void *buffer);
void round_display_service_release_consumed_pet_source(void *frame);
/* The animator chooses what to compose; Display HAL owns the FreeRTOS task,
 * profile-qualified stack/core placement, interruption notification and
 * bounded join.  This prevents scene code from retaining a task handle or a
 * completion semaphore beside framebuffer state. */
typedef void (*round_display_service_animation_fn_t)(void *context);
esp_err_t round_display_service_start_animation(round_display_service_animation_fn_t entry,
                                                 void *context);
/* Scene code expresses a completed-frame delay in milliseconds.  The Display
 * service owns the selected profile's FreeRTOS wait conversion so renderer
 * code does not depend on task tick types merely to run an animation. */
bool round_display_service_animation_wait_ms(uint32_t timeout_ms);
bool round_display_service_animation_running(void);
esp_err_t round_display_service_stop_animation(uint32_t timeout_ms);
/* Test-build-only lifecycle proof. Production builds return immediately and
 * do not create any synthetic worker. This remains a private Display service
 * seam, not a Device/Platform or board-profile contract. */
esp_err_t round_display_service_run_animation_deadline_test(void);
const char *round_display_service_ambient_overlay_memory_name(void);
bool round_display_service_has_startup_art(void);
void round_display_service_compose_startup_art(uint16_t *frame,
                                               int width, int height);
