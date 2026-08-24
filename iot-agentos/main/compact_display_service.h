#pragma once

/*
 * Private compact-board Display HAL implementation boundary.
 *
 * The shared compact renderer owns scenes, frame comparison and presentation
 * policy.  This service is the sole owner of the selected profile's panel,
 * SPI/DMA fence, physical memory placement and display power sequencing.
 * It is deliberately not a Device or Platform API.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"
#include "boards/compact_display_animation.h"
#include "boards/compact_startup_art.h"

/* Decorative worker lifecycle belongs to the Display HAL rather than the
 * common scene renderer.  Scene code supplies only a callback; the selected
 * compact profile still decides task placement/stack requirements below this
 * private service boundary. */
typedef enum {
    COMPACT_DISPLAY_ANIMATION_THINKING = 0,
    COMPACT_DISPLAY_ANIMATION_PET,
    COMPACT_DISPLAY_ANIMATION_COUNT,
} compact_display_service_animation_kind_t;

typedef void (*compact_display_service_animation_fn_t)(void *context);

int compact_display_service_width(void);
int compact_display_service_height(void);
unsigned compact_display_service_default_brightness(void);
/* Bounded physical transport/staging height selected by the display profile.
 * Scene code may size a neutral render buffer from this scalar but never
 * learns controller commands, DMA ownership, or the profile identity. */
int compact_display_service_transfer_stripe_rows(void);
bool compact_display_service_ready(void);
esp_err_t compact_display_service_initialize(void);
void compact_display_service_discard_unpublished_state(void);
esp_err_t compact_display_service_set_brightness(unsigned percent);
esp_err_t compact_display_service_enter_display_off(void);
esp_err_t compact_display_service_wake_from_display_off(unsigned brightness);
/* System Sleep PREPARE uses this profile-private physical fence only after
 * Display Service has stopped new semantic submissions.  It proves no panel
 * DMA source remains borrowed, but deliberately does not alter panel power,
 * DMA ownership, or worker lifecycle. */
esp_err_t compact_display_service_wait_for_scanout_idle(uint32_t timeout_ms);
/* Future System Sleep also parks retained decorative workers after semantic
 * Display admission is closed. ABORT resumes the same worker generations; it
 * never recreates panel/DMA resources or changes scene state. */
esp_err_t compact_display_service_prepare_system_sleep(uint32_t timeout_ms);
void compact_display_service_abort_system_sleep_prepare(void);
esp_err_t compact_display_service_draw_bitmap_sync(int x0, int y0, int x1, int y1,
                                                    const void *pixels);
bool compact_display_service_uses_delta_presentation(void);
bool compact_display_service_uses_profile_thinking_patch(void);
bool compact_display_service_compose_thinking_patch(
    uint16_t *pixels, size_t pixel_capacity, unsigned phase, uint16_t background,
    compact_display_animation_patch_t *out_patch);
compact_startup_full_frame_t compact_display_service_startup_full_frame(void);
uint16_t *compact_display_service_allocate_framebuffer(size_t bytes);
uint16_t *compact_display_service_allocate_transfer_buffer(size_t bytes);
void compact_display_service_free_buffer(void *buffer);
uint16_t *compact_display_service_allocate_temporary_composition_bitmap(size_t bytes);
uint16_t *compact_display_service_allocate_temporary_transfer_bitmap(size_t bytes);
void compact_display_service_free_temporary_bitmap(void *bitmap);
uint8_t *compact_display_service_allocate_remote_pet_frame(size_t bytes);
void compact_display_service_free_remote_pet_frame(void *frame);
void compact_display_service_release_consumed_pet_source(void *frame);
uint32_t compact_display_service_thinking_worker_wait_ms(uint32_t common_interval_ms);
uint32_t compact_display_service_pet_worker_wait_ms(size_t remote_pet_frame_count,
                                                     uint32_t animated_frame_ms);
esp_err_t compact_display_service_start_animation(
    compact_display_service_animation_kind_t kind,
    compact_display_service_animation_fn_t entry, void *context);
bool compact_display_service_animation_wait_ms(
    compact_display_service_animation_kind_t kind, uint32_t timeout_ms);
bool compact_display_service_animation_running(
    compact_display_service_animation_kind_t kind);
esp_err_t compact_display_service_stop_animation(
    compact_display_service_animation_kind_t kind, uint32_t timeout_ms);
/* Test-build-only lifecycle proof. Production builds return immediately and
 * do not create any synthetic worker. This remains a private Display service
 * seam, not a Device/Platform or board-profile contract. */
esp_err_t compact_display_service_run_animation_deadline_test(void);
/* A slow serial panel may not be able to present every interpolation step.
 * This private profile contract lets its animation keep the source pack's
 * intended playback speed by advancing from observed presentation time; other
 * compact panels retain the conservative one-presented-tick progression. */
bool compact_display_service_pet_animation_tracks_elapsed_time(void);
/* Private renderer-to-profile diagnostic hook.  The logical scene remains
 * hardware-neutral; profiles may aggregate this completion timing locally to
 * qualify their own transport without exposing controller data to Device API. */
void compact_display_service_note_pet_animation_tick(uint32_t target_interval_ms,
                                                     bool presented,
                                                     uint32_t presentation_us);
