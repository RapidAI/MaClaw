#pragma once

/* Private Peripheral-to-Audio lifecycle seam for circular boards.
 *
 * The codec source owner alone can create the shared I2C bus.  Peripheral
 * Service uses this semantic preflight to make touch/PMIC/IMU observations
 * available before Input starts; no GPIO, controller or I2C-handle type leaks
 * across the service boundary.
 */

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"

esp_err_t round_audio_lifecycle_prepare_shared_bus(unsigned output_volume,
                                                    uint32_t timeout_ms);

/* The shared-bus owner keeps all physical handles in the selected round
 * adapters.  These lifecycle hooks are intentionally private to that owner:
 * they provide generation/admission fencing without leaking a controller
 * handle into the generic HAL. */
esp_err_t round_audio_lifecycle_shared_bus_begin_bootstrap(void);
esp_err_t round_audio_lifecycle_shared_bus_mark_attached(void);
esp_err_t round_audio_lifecycle_shared_bus_begin_self_test(void);
esp_err_t round_audio_lifecycle_shared_bus_mark_ready(void);
void round_audio_lifecycle_shared_bus_abort_detached_bootstrap(void);

/* A borrower holds the private lifecycle mutex for the complete controller
 * transaction.  This prevents a touch/PMIC/IMU operation from observing a
 * handle while the Audio owner is tearing down or reprobeing the same bus.
 * It is deliberately unsuitable for streaming I2S ownership or long waits. */
bool round_audio_lifecycle_shared_bus_borrow_begin(void);
void round_audio_lifecycle_shared_bus_borrow_end(void);

/* Codec register control is a short shared-I2C transaction too. It has its
 * own scope because public Audio sessions already hold a distinct ownership
 * mutex while issuing volume/mute/gain writes. It never covers PCM I2S
 * streaming. */
bool round_audio_lifecycle_shared_bus_codec_control_begin(void);
void round_audio_lifecycle_shared_bus_codec_control_end(void);

/* Begin/finish teardown is a matched exclusive scope used only by the codec
 * bus owner.  `finish` must receive the observed physical cleanup result;
 * any error leaves admission closed as UNKNOWN_OUTCOME. */
esp_err_t round_audio_lifecycle_shared_bus_begin_teardown(bool *out_active);
void round_audio_lifecycle_shared_bus_finish_teardown(esp_err_t cleanup_result);

/* Explicit recovery entry point for the circular profile-private bus owner.
 * This is not a public Device/Platform capability: health policy decides when
 * to invoke it, and ordinary I2C errors never trigger an automatic reset. */
esp_err_t round_audio_lifecycle_recover_shared_bus(unsigned output_volume,
                                                   uint32_t timeout_ms);
