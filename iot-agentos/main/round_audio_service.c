#include "round_audio_service.h"
#include "round_audio_lifecycle.h"

#include <math.h>

#include "esp_check.h"
#include "esp_log.h"
#include "esp_timer.h"

#include <stdatomic.h>
#include "round_peripheral_lifecycle.h"
#include "shared_bus_lifecycle.h"
#include "shared_bus_recovery_transaction.h"

/*
 * There must be exactly one compilation-unit owner for the codec transport.
 * Touch/PMIC/IMU share the I2C bus, but their controllers belong to the
 * separate Peripheral source owner.
 */
#include "boards/round_audio_profile_adapter.h"

/* The established codec implementation is header-local by design. Rename its
 * public-looking static helpers to implementation symbols in this one source
 * owner, then expose only the normalized private Audio HAL declared above.
 * This preserves its known-good register and timing sequence byte-for-byte. */
#define round_audio_adapter_initialize round_audio_adapter_initialize_impl
#define round_audio_adapter_release round_audio_adapter_release_impl
#define round_audio_adapter_set_output_volume round_audio_adapter_set_output_volume_impl
#define round_audio_adapter_restore_input_gain round_audio_adapter_restore_input_gain_impl
#define round_audio_adapter_set_power_amplifier round_audio_adapter_set_power_amplifier_impl
#define round_audio_adapter_allocate_command_wav round_audio_adapter_allocate_command_wav_impl
#define round_audio_adapter_free_command_wav round_audio_adapter_free_command_wav_impl
#define round_audio_adapter_allocate_wake_capture_buffer round_audio_adapter_allocate_wake_capture_buffer_impl
#define round_audio_adapter_free_wake_capture_buffer round_audio_adapter_free_wake_capture_buffer_impl
#define round_audio_adapter_capture_wire_bytes round_audio_adapter_capture_wire_bytes_impl
#define round_audio_adapter_extract_capture_mono round_audio_adapter_extract_capture_mono_impl
#define round_audio_adapter_read_pcm round_audio_adapter_read_pcm_impl
#define round_audio_adapter_write_pcm round_audio_adapter_write_pcm_impl
#define round_audio_adapter_playback_prepare round_audio_adapter_playback_prepare_impl
#define round_audio_adapter_playback_reveal round_audio_adapter_playback_reveal_impl
#define round_audio_adapter_playback_abort round_audio_adapter_playback_abort_impl
#define round_audio_adapter_playback_finish round_audio_adapter_playback_finish_impl
#include "boards/round_audio_codec_adapter.h"
#undef round_audio_adapter_playback_finish
#undef round_audio_adapter_playback_abort
#undef round_audio_adapter_playback_reveal
#undef round_audio_adapter_playback_prepare
#undef round_audio_adapter_write_pcm
#undef round_audio_adapter_read_pcm
#undef round_audio_adapter_extract_capture_mono
#undef round_audio_adapter_capture_wire_bytes
#undef round_audio_adapter_free_wake_capture_buffer
#undef round_audio_adapter_allocate_wake_capture_buffer
#undef round_audio_adapter_free_command_wav
#undef round_audio_adapter_allocate_command_wav
#undef round_audio_adapter_set_power_amplifier
#undef round_audio_adapter_restore_input_gain
#undef round_audio_adapter_set_output_volume
#undef round_audio_adapter_release
#undef round_audio_adapter_initialize

static void *round_audio_service_allocate_wake_capture_buffer(size_t bytes);
static void round_audio_service_free_wake_capture_buffer(void *buffer);
static size_t round_audio_service_capture_wire_bytes(size_t frames);
static size_t round_audio_service_extract_capture_mono(const int16_t *wire,
                                                        size_t wire_bytes,
                                                        int16_t *mono,
                                                        size_t mono_capacity);
static void round_audio_service_wake_pcm_reset(void);
static void round_audio_service_wake_pcm_process(int16_t *mono, size_t frames,
                                                  round_audio_wake_pcm_stats_t *out_stats);
static esp_err_t round_audio_service_initialize(unsigned output_volume);
static esp_err_t round_audio_service_acquire(uint32_t timeout_ms);
static void round_audio_service_release_ownership(void);
static esp_err_t round_audio_service_set_output_volume(unsigned percent);
static esp_err_t round_audio_service_restore_input_gain(void);
static esp_err_t round_audio_service_read_pcm(void *buffer, size_t capacity,
                                              size_t *received, TickType_t timeout);
static esp_err_t round_audio_service_write_pcm(const int16_t *pcm, size_t frames,
                                               unsigned channels, size_t *written,
                                               TickType_t timeout);
static esp_err_t round_audio_service_playback_prepare(void);
static esp_err_t round_audio_service_playback_reveal(void);
static esp_err_t round_audio_service_playback_abort(void);
static esp_err_t round_audio_service_playback_finish(const int16_t *silence,
                                                     size_t silence_frames,
                                                     size_t *written,
                                                     TickType_t timeout);
static void round_audio_service_release(void);
static void round_audio_service_capture_pcm_reset(const char *diagnostic_label);
static int32_t round_audio_service_capture_pcm_process(const int16_t *input_mono,
                                                       size_t frames,
                                                       int16_t *output_mono);
static void round_audio_service_capture_pcm_stats(const int16_t *samples,
                                                  size_t frames, int32_t peak,
                                                  round_audio_capture_stats_t *out_stats);
static bool round_audio_service_capture_sample_is_valid(int16_t sample);
static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_quiesce(
    uint32_t timeout_ms, void *context);
static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_drain(
    uint32_t timeout_ms, void *context);
static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_detach_peripherals(
    uint32_t timeout_ms, void *context);
static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_detach_codecs(
    uint32_t timeout_ms, void *context);
static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_delete_bus(
    uint32_t timeout_ms, void *context);
static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_create_bus(
    uint32_t timeout_ms, void *context);
static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_attach_peripherals(
    uint32_t timeout_ms, void *context);
static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_attach_codecs(
    uint32_t timeout_ms, void *context);
static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_self_test(
    uint32_t timeout_ms, void *context);

const char *round_audio_service_name(void) {
    const round_audio_profile_t *profile = round_audio_profile_adapter();
    return profile && profile->name ? profile->name : "round audio";
}

unsigned round_audio_service_default_output_volume(void) {
    const round_audio_profile_t *profile = round_audio_profile_adapter();
    return profile ? profile->output_volume_default : 0;
}

uint32_t round_audio_service_sample_rate(void) {
    const round_audio_profile_t *profile = round_audio_profile_adapter();
    return profile ? profile->sample_rate : 0;
}

/* The adapters retain the physical transaction implementation, including the
 * shared I2C ordering with touch/PMIC/IMU.  This service owns whether that
 * transaction has completed, and makes every partial attempt rollback before
 * a later capture, wake or volume request retries it. */
static bool s_round_audio_ready;
static SemaphoreHandle_t s_round_audio_ownership_mutex;
static SemaphoreHandle_t s_round_shared_bus_mutex;
static SemaphoreHandle_t s_round_shared_bus_codec_control_mutex;
static shared_bus_lifecycle_t s_round_shared_bus_lifecycle;
static bool s_round_shared_bus_lifecycle_initialized;
static shared_bus_lease_t s_round_shared_bus_borrow_lease;
static shared_bus_lease_t s_round_shared_bus_codec_control_lease;
static bool s_round_shared_bus_codec_control_self_test;
static bool s_round_shared_bus_recovery_self_test;
static bool s_round_audio_stream_owned;
static bool s_round_audio_command_capture_owned;
static _Atomic bool s_round_audio_command_capture_stop_requested;
static TaskHandle_t s_round_audio_playback_owner;
static volatile bool s_round_audio_playback_stop_requested;

static esp_err_t round_audio_lifecycle_shared_bus_lock_for(uint32_t timeout_ms) {
    if (!s_round_shared_bus_mutex) {
        s_round_shared_bus_mutex = xSemaphoreCreateMutex();
        if (!s_round_shared_bus_mutex) return ESP_ERR_NO_MEM;
    }
    TickType_t timeout = timeout_ms == UINT32_MAX ? portMAX_DELAY : pdMS_TO_TICKS(timeout_ms);
    if (timeout_ms != 0u && timeout == 0u) timeout = 1u;
    return xSemaphoreTake(s_round_shared_bus_mutex, timeout) == pdTRUE
               ? ESP_OK
               : ESP_ERR_TIMEOUT;
}

static esp_err_t round_audio_lifecycle_shared_bus_lock(void) {
    return round_audio_lifecycle_shared_bus_lock_for(UINT32_MAX);
}

static void round_audio_lifecycle_shared_bus_unlock(void) {
    if (s_round_shared_bus_mutex) (void)xSemaphoreGive(s_round_shared_bus_mutex);
}

static esp_err_t round_audio_lifecycle_shared_bus_result_to_esp_err(
    shared_bus_lifecycle_result_t result) {
    switch (result) {
        case SHARED_BUS_LIFECYCLE_OK: return ESP_OK;
        case SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case SHARED_BUS_LIFECYCLE_RESOURCE_EXHAUSTED: return ESP_ERR_NO_MEM;
        case SHARED_BUS_LIFECYCLE_UNAVAILABLE: return ESP_ERR_INVALID_STATE;
        case SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME: return ESP_ERR_INVALID_STATE;
        case SHARED_BUS_LIFECYCLE_BUSY:
        case SHARED_BUS_LIFECYCLE_STALE_LEASE:
        default: return ESP_ERR_INVALID_STATE;
    }
}

esp_err_t round_audio_lifecycle_shared_bus_begin_bootstrap(void) {
    ESP_RETURN_ON_ERROR(round_audio_lifecycle_shared_bus_lock(), "round_audio",
                        "shared bus lifecycle lock");
    if (!s_round_shared_bus_lifecycle_initialized) {
        shared_bus_lifecycle_reset(&s_round_shared_bus_lifecycle,
                                   FAULT_DOMAIN_ID_SHARED_BUS);
        s_round_shared_bus_lifecycle_initialized = true;
    }
    const esp_err_t err = round_audio_lifecycle_shared_bus_result_to_esp_err(
        shared_bus_lifecycle_begin_reinitialize(&s_round_shared_bus_lifecycle));
    round_audio_lifecycle_shared_bus_unlock();
    return err;
}

esp_err_t round_audio_lifecycle_shared_bus_mark_attached(void) {
    ESP_RETURN_ON_ERROR(round_audio_lifecycle_shared_bus_lock(), "round_audio",
                        "shared bus lifecycle lock");
    const esp_err_t err = !s_round_shared_bus_lifecycle_initialized
                              ? ESP_ERR_INVALID_STATE
                              : round_audio_lifecycle_shared_bus_result_to_esp_err(
                                    shared_bus_lifecycle_mark_attached(
                                        &s_round_shared_bus_lifecycle));
    round_audio_lifecycle_shared_bus_unlock();
    return err;
}

esp_err_t round_audio_lifecycle_shared_bus_begin_self_test(void) {
    ESP_RETURN_ON_ERROR(round_audio_lifecycle_shared_bus_lock(), "round_audio",
                        "shared bus lifecycle lock");
    const esp_err_t err = !s_round_shared_bus_lifecycle_initialized
                              ? ESP_ERR_INVALID_STATE
                              : round_audio_lifecycle_shared_bus_result_to_esp_err(
                                    shared_bus_lifecycle_begin_self_test(
                                        &s_round_shared_bus_lifecycle));
    round_audio_lifecycle_shared_bus_unlock();
    return err;
}

esp_err_t round_audio_lifecycle_shared_bus_mark_ready(void) {
    ESP_RETURN_ON_ERROR(round_audio_lifecycle_shared_bus_lock(), "round_audio",
                        "shared bus lifecycle lock");
    const esp_err_t err = !s_round_shared_bus_lifecycle_initialized
                              ? ESP_ERR_INVALID_STATE
                              : round_audio_lifecycle_shared_bus_result_to_esp_err(
                                    shared_bus_lifecycle_mark_ready(
                                        &s_round_shared_bus_lifecycle));
    round_audio_lifecycle_shared_bus_unlock();
    return err;
}

void round_audio_lifecycle_shared_bus_abort_detached_bootstrap(void) {
    if (round_audio_lifecycle_shared_bus_lock() != ESP_OK) return;
    if (s_round_shared_bus_lifecycle_initialized) {
        shared_bus_lifecycle_snapshot_t snapshot = {0};
        if (shared_bus_lifecycle_get_snapshot(&s_round_shared_bus_lifecycle, &snapshot) &&
            snapshot.attachment == SHARED_BUS_ATTACHMENT_DETACHED &&
            snapshot.active_lease_count == 0u) {
            /* No physical bus was created, so the lifecycle may return to
             * STOPPED without masking an external cleanup outcome. */
            (void)shared_bus_lifecycle_cancel_unattached_start(
                &s_round_shared_bus_lifecycle);
        }
    }
    round_audio_lifecycle_shared_bus_unlock();
}

bool round_audio_lifecycle_shared_bus_borrow_begin(void) {
    if (round_audio_lifecycle_shared_bus_lock() != ESP_OK) return false;
    if (!s_round_shared_bus_lifecycle_initialized ||
        shared_bus_lifecycle_acquire(&s_round_shared_bus_lifecycle,
                                     &s_round_shared_bus_borrow_lease) !=
            SHARED_BUS_LIFECYCLE_OK) {
        round_audio_lifecycle_shared_bus_unlock();
        return false;
    }
    /* The lease is released by borrow_end while the same mutex is held; no
     * profile handle survives this controller transaction. */
    return true;
}

void round_audio_lifecycle_shared_bus_borrow_end(void) {
    if (s_round_shared_bus_lifecycle_initialized &&
        s_round_shared_bus_borrow_lease.token != 0u) {
        (void)shared_bus_lifecycle_release(&s_round_shared_bus_lifecycle,
                                           &s_round_shared_bus_borrow_lease);
        s_round_shared_bus_borrow_lease = (shared_bus_lease_t){0};
    }
    round_audio_lifecycle_shared_bus_unlock();
}

bool round_audio_lifecycle_shared_bus_codec_control_begin(void) {
    if (!s_round_shared_bus_codec_control_mutex) {
        s_round_shared_bus_codec_control_mutex = xSemaphoreCreateMutex();
        if (!s_round_shared_bus_codec_control_mutex) return false;
    }
    /* Recovery owns the shared-bus mutex for its whole rebuild. Its source
     * owner self-test may use codec register helpers, but must not attempt to
     * re-take that non-recursive mutex. */
    if (s_round_shared_bus_recovery_self_test) {
        return xSemaphoreTake(s_round_shared_bus_codec_control_mutex, portMAX_DELAY) == pdTRUE;
    }
    /* Lock in the same order as generic peripheral borrowing: bus first,
     * then codec control. Otherwise a codec writer could retain its private
     * mutex while waiting on the bus just as recovery needs it for self-test. */
    if (round_audio_lifecycle_shared_bus_lock() != ESP_OK) return false;
    if (xSemaphoreTake(s_round_shared_bus_codec_control_mutex, portMAX_DELAY) != pdTRUE) {
        round_audio_lifecycle_shared_bus_unlock();
        return false;
    }
    if (s_round_shared_bus_codec_control_lease.token != 0u ||
        s_round_shared_bus_codec_control_self_test) {
        round_audio_lifecycle_shared_bus_unlock();
        return true;
    }
    shared_bus_lifecycle_result_t result = SHARED_BUS_LIFECYCLE_UNAVAILABLE;
    shared_bus_lifecycle_snapshot_t snapshot = {0};
    if (s_round_shared_bus_lifecycle_initialized &&
        shared_bus_lifecycle_get_snapshot(&s_round_shared_bus_lifecycle, &snapshot)) {
        if (snapshot.domain.phase == FAULT_DOMAIN_SELF_TEST &&
            snapshot.active_lease_count == 0u) {
            /* Codec probing is part of the source owner's atomic physical
             * self-test before READY exists.  No generic borrower can acquire
             * in this phase, and the codec-control mutex keeps the probe
             * sequence single-owner.  This is deliberately not a runtime
             * admission bypass. */
            s_round_shared_bus_codec_control_self_test = true;
            result = SHARED_BUS_LIFECYCLE_OK;
        } else {
            result = shared_bus_lifecycle_acquire(
                &s_round_shared_bus_lifecycle, &s_round_shared_bus_codec_control_lease);
        }
    }
    /* Keep the shared-bus mutex for the complete register transaction. A
     * lifecycle lease alone fences admission but cannot serialize a live I2C
     * transfer against a recovery owner deleting the master bus. `end` both
     * releases the lease and unlocks this mutex. */
    if (result == SHARED_BUS_LIFECYCLE_OK) return true;

    round_audio_lifecycle_shared_bus_unlock();
    (void)xSemaphoreGive(s_round_shared_bus_codec_control_mutex);
    return false;
}

void round_audio_lifecycle_shared_bus_codec_control_end(void) {
    if (!s_round_shared_bus_codec_control_mutex) return;
    if (s_round_shared_bus_codec_control_self_test) {
        s_round_shared_bus_codec_control_self_test = false;
        round_audio_lifecycle_shared_bus_unlock();
    } else if (s_round_shared_bus_recovery_self_test) {
        /* Keep the recovery owner's exclusive bus mutex until the entire
         * rebuild completes; individual codec register writes only release
         * their codec-control mutex. */
    } else if (s_round_shared_bus_codec_control_lease.token != 0u) {
        (void)shared_bus_lifecycle_release(
            &s_round_shared_bus_lifecycle, &s_round_shared_bus_codec_control_lease);
        s_round_shared_bus_codec_control_lease = (shared_bus_lease_t){0};
        round_audio_lifecycle_shared_bus_unlock();
    }
    (void)xSemaphoreGive(s_round_shared_bus_codec_control_mutex);
}

esp_err_t round_audio_lifecycle_shared_bus_begin_teardown(bool *out_active) {
    if (!out_active) return ESP_ERR_INVALID_ARG;
    *out_active = false;
    ESP_RETURN_ON_ERROR(round_audio_lifecycle_shared_bus_lock(), "round_audio",
                        "shared bus lifecycle lock");
    if (!s_round_shared_bus_lifecycle_initialized) {
        round_audio_lifecycle_shared_bus_unlock();
        return ESP_OK;
    }
    shared_bus_lifecycle_snapshot_t snapshot = {0};
    if (!shared_bus_lifecycle_get_snapshot(&s_round_shared_bus_lifecycle, &snapshot)) {
        round_audio_lifecycle_shared_bus_unlock();
        return ESP_ERR_INVALID_STATE;
    }
    if (snapshot.attachment == SHARED_BUS_ATTACHMENT_DETACHED) {
        round_audio_lifecycle_shared_bus_unlock();
        return ESP_OK;
    }
    if (snapshot.domain.phase != FAULT_DOMAIN_READY &&
        snapshot.domain.phase != FAULT_DOMAIN_UNKNOWN_OUTCOME) {
        (void)shared_bus_lifecycle_mark_unknown(&s_round_shared_bus_lifecycle);
    }
    const shared_bus_lifecycle_result_t result =
        shared_bus_lifecycle_begin_recovery(&s_round_shared_bus_lifecycle);
    if (result != SHARED_BUS_LIFECYCLE_OK ||
        !shared_bus_lifecycle_can_detach(&s_round_shared_bus_lifecycle)) {
        round_audio_lifecycle_shared_bus_unlock();
        return round_audio_lifecycle_shared_bus_result_to_esp_err(result);
    }
    *out_active = true;
    return ESP_OK;
}

void round_audio_lifecycle_shared_bus_finish_teardown(esp_err_t cleanup_result) {
    if (!s_round_shared_bus_lifecycle_initialized) {
        round_audio_lifecycle_shared_bus_unlock();
        return;
    }
    if (cleanup_result == ESP_OK) {
        if (shared_bus_lifecycle_mark_detached(&s_round_shared_bus_lifecycle) !=
            SHARED_BUS_LIFECYCLE_OK) {
            (void)shared_bus_lifecycle_mark_unknown(&s_round_shared_bus_lifecycle);
        }
    } else {
        (void)shared_bus_lifecycle_mark_unknown(&s_round_shared_bus_lifecycle);
    }
    round_audio_lifecycle_shared_bus_unlock();
}

typedef struct {
    int64_t deadline_us;
    unsigned output_volume;
} round_audio_shared_bus_recovery_context_t;

static shared_bus_recovery_status_t round_audio_lifecycle_recovery_status(esp_err_t err) {
    if (err == ESP_OK) return SHARED_BUS_RECOVERY_STATUS_OK;
    if (err == ESP_ERR_TIMEOUT) return SHARED_BUS_RECOVERY_STATUS_TIMEOUT;
    if (err == ESP_ERR_NO_MEM) return SHARED_BUS_RECOVERY_STATUS_UNAVAILABLE;
    if (err == ESP_ERR_INVALID_ARG) return SHARED_BUS_RECOVERY_STATUS_INVALID_ARGUMENT;
    if (err == ESP_ERR_INVALID_STATE) return SHARED_BUS_RECOVERY_STATUS_UNAVAILABLE;
    return SHARED_BUS_RECOVERY_STATUS_IO_ERROR;
}

/* Keep recovery failure evidence available to the profile-private health
 * owner.  `ready=false` always keeps shared-bus admission closed; this mapping
 * is diagnostic only and must never be treated as permission to reuse a
 * partially rebuilt bus.  Collapsing every failure to INVALID_STATE would
 * make a bounded supervisor unable to distinguish ordinary contention from a
 * deadline or an observed I/O failure. */
static esp_err_t round_audio_lifecycle_recovery_status_to_esp_err(
    shared_bus_recovery_status_t status) {
    switch (status) {
        case SHARED_BUS_RECOVERY_STATUS_OK:
            return ESP_OK;
        case SHARED_BUS_RECOVERY_STATUS_INVALID_ARGUMENT:
            return ESP_ERR_INVALID_ARG;
        case SHARED_BUS_RECOVERY_STATUS_TIMEOUT:
            return ESP_ERR_TIMEOUT;
        case SHARED_BUS_RECOVERY_STATUS_UNAVAILABLE:
        case SHARED_BUS_RECOVERY_STATUS_BUSY:
            return ESP_ERR_INVALID_STATE;
        case SHARED_BUS_RECOVERY_STATUS_IO_ERROR:
        case SHARED_BUS_RECOVERY_STATUS_INTERNAL_ERROR:
        default:
            return ESP_FAIL;
    }
}

static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_result(
    esp_err_t err) {
    return (shared_bus_recovery_step_result_t){
        .struct_size = sizeof(shared_bus_recovery_step_result_t),
        .abi_version = SHARED_BUS_RECOVERY_TRANSACTION_ABI_VERSION,
        .disposition = err == ESP_OK ? SHARED_BUS_RECOVERY_STEP_OK
                                     : SHARED_BUS_RECOVERY_STEP_REJECTED,
        .status = round_audio_lifecycle_recovery_status(err),
    };
}

static uint32_t round_audio_lifecycle_recovery_remaining_timeout(void *context) {
    const round_audio_shared_bus_recovery_context_t *recovery = context;
    if (!recovery) return 0u;
    const int64_t remaining_us = recovery->deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0u;
    const uint64_t remaining_ms = ((uint64_t)remaining_us + 999u) / 1000u;
    return remaining_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)remaining_ms;
}

/* The explicit recovery caller already owns the Audio session mutex and the
 * shared-bus mutex. This first phase therefore fences new command/playback
 * admission and Input's controller reads before any profile handle changes. */
static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_quiesce(
    uint32_t timeout_ms, void *context) {
    (void)timeout_ms;
    (void)context;
    if (s_round_audio_stream_owned || s_round_audio_command_capture_owned ||
        s_round_audio_playback_owner) {
        return round_audio_lifecycle_recovery_result(ESP_ERR_INVALID_STATE);
    }
    s_round_audio_playback_stop_requested = true;
    s_round_audio_ready = false;
    return round_audio_lifecycle_recovery_result(round_audio_adapter_release_i2s());
}

static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_drain(
    uint32_t timeout_ms, void *context) {
    (void)timeout_ms;
    (void)context;
    /* Borrowers hold the same mutex for their entire I2C transaction. Since
     * the recovery owner holds it now, no old borrower can still be executing.
     * The value core independently verifies can_detach after this callback. */
    return round_audio_lifecycle_recovery_result(ESP_OK);
}

static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_detach_peripherals(
    uint32_t timeout_ms, void *context) {
    (void)timeout_ms;
    (void)context;
    return round_audio_lifecycle_recovery_result(round_peripheral_lifecycle_detach());
}

static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_detach_codecs(
    uint32_t timeout_ms, void *context) {
    (void)timeout_ms;
    (void)context;
    return round_audio_lifecycle_recovery_result(round_audio_adapter_release_codecs());
}

static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_delete_bus(
    uint32_t timeout_ms, void *context) {
    (void)timeout_ms;
    (void)context;
    return round_audio_lifecycle_recovery_result(round_audio_adapter_delete_codec_bus());
}

static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_create_bus(
    uint32_t timeout_ms, void *context) {
    (void)timeout_ms;
    (void)context;
    return round_audio_lifecycle_recovery_result(round_audio_adapter_open_codec_bus());
}

static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_attach_peripherals(
    uint32_t timeout_ms, void *context) {
    (void)timeout_ms;
    const round_audio_shared_bus_recovery_context_t *recovery = context;
    const round_audio_profile_t *profile = round_audio_profile_adapter();
    if (!recovery || !profile || !s_round_audio_i2c_bus) {
        return round_audio_lifecycle_recovery_result(ESP_ERR_INVALID_STATE);
    }
    const esp_err_t err = round_peripheral_lifecycle_attach(s_round_audio_i2c_bus);
    if (err != ESP_OK && !profile->touch_initialization_required) {
        /* Preserve the established optional-controller policy. It is not a
         * successful touch probe; the later HIL plan still has to exercise
         * this profile's optional peripheral behaviour explicitly. */
        ESP_LOGW("round_audio", "shared-bus recovery deferred optional peripheral: %s",
                 esp_err_to_name(err));
        return round_audio_lifecycle_recovery_result(ESP_OK);
    }
    return round_audio_lifecycle_recovery_result(err);
}

static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_attach_codecs(
    uint32_t timeout_ms, void *context) {
    (void)timeout_ms;
    (void)context;
    return round_audio_lifecycle_recovery_result(round_audio_adapter_attach_codecs());
}

static shared_bus_recovery_step_result_t round_audio_lifecycle_recovery_self_test(
    uint32_t timeout_ms, void *context) {
    (void)timeout_ms;
    const round_audio_shared_bus_recovery_context_t *recovery = context;
    const round_audio_profile_t *profile = round_audio_profile_adapter();
    if (!recovery || !profile) return round_audio_lifecycle_recovery_result(ESP_ERR_INVALID_STATE);
    /* Codec register helpers normally acquire a READY lease. During this
     * source-owner self-test READY does not exist yet, so retain the same
     * narrowly scoped bootstrap exception used by initial bring-up. */
    s_round_shared_bus_recovery_self_test = true;
    esp_err_t err = round_audio_adapter_initialize_input_codec(s_round_audio_input_codec);
    if (err == ESP_OK) err = round_audio_adapter_initialize_i2s();
    if (err == ESP_OK) err = round_audio_adapter_initialize_power_amplifier();
    if (err == ESP_OK) {
        err = round_audio_adapter_initialize_output_codec(
            s_round_audio_output_codec, profile->output_mute_register,
            profile->output_volume_register, recovery->output_volume);
    }
    s_round_shared_bus_recovery_self_test = false;
    if (err == ESP_OK) round_audio_adapter_log_i2s_ready(profile->name);
    return round_audio_lifecycle_recovery_result(err);
}

esp_err_t round_audio_lifecycle_recover_shared_bus(unsigned output_volume,
                                                   uint32_t timeout_ms) {
    if (output_volume > 100u || timeout_ms == 0u) return ESP_ERR_INVALID_ARG;
    /* This is one caller-owned recovery budget, not an Audio-mutex budget
     * followed by a separate shared-bus-mutex budget.  Otherwise contention
     * before the physical transaction can make a nominally bounded recovery
     * run for almost twice its advertised timeout. */
    round_audio_shared_bus_recovery_context_t context = {
        .deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000,
        .output_volume = output_volume,
    };
    ESP_RETURN_ON_ERROR(round_audio_service_acquire(timeout_ms), "round_audio",
                        "shared-bus recovery audio ownership timeout");
    const uint32_t remaining_after_audio_lock =
        round_audio_lifecycle_recovery_remaining_timeout(&context);
    if (remaining_after_audio_lock == 0u) {
        round_audio_service_release_ownership();
        return ESP_ERR_TIMEOUT;
    }
    esp_err_t lock_err =
        round_audio_lifecycle_shared_bus_lock_for(remaining_after_audio_lock);
    if (lock_err != ESP_OK) {
        round_audio_service_release_ownership();
        return lock_err;
    }
    if (!s_round_shared_bus_lifecycle_initialized) {
        round_audio_lifecycle_shared_bus_unlock();
        round_audio_service_release_ownership();
        return ESP_ERR_INVALID_STATE;
    }
    const shared_bus_recovery_transaction_callbacks_t callbacks = {
        .struct_size = sizeof(callbacks),
        .abi_version = SHARED_BUS_RECOVERY_TRANSACTION_ABI_VERSION,
        .remaining_timeout_ms = round_audio_lifecycle_recovery_remaining_timeout,
        .quiesce_consumers = round_audio_lifecycle_recovery_quiesce,
        .wait_for_borrowers = round_audio_lifecycle_recovery_drain,
        .detach_peripherals = round_audio_lifecycle_recovery_detach_peripherals,
        .detach_codec = round_audio_lifecycle_recovery_detach_codecs,
        .delete_bus = round_audio_lifecycle_recovery_delete_bus,
        .create_bus = round_audio_lifecycle_recovery_create_bus,
        .attach_peripherals = round_audio_lifecycle_recovery_attach_peripherals,
        .attach_codec = round_audio_lifecycle_recovery_attach_codecs,
        .self_test = round_audio_lifecycle_recovery_self_test,
        .context = &context,
    };
    const shared_bus_recovery_transaction_result_t result =
        shared_bus_recovery_transaction_execute(&s_round_shared_bus_lifecycle, &callbacks);
    s_round_audio_ready = result.ready;
    round_audio_lifecycle_shared_bus_unlock();
    round_audio_service_release_ownership();
    return result.ready ? ESP_OK :
           round_audio_lifecycle_recovery_status_to_esp_err(result.status);
}

static esp_err_t round_audio_service_acquire(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_round_audio_ownership_mutex) {
        s_round_audio_ownership_mutex = xSemaphoreCreateMutex();
        if (!s_round_audio_ownership_mutex) return ESP_ERR_NO_MEM;
    }
    return xSemaphoreTake(s_round_audio_ownership_mutex,
                          pdMS_TO_TICKS(timeout_ms)) == pdTRUE
               ? ESP_OK : ESP_ERR_TIMEOUT;
}

static void round_audio_service_release_ownership(void) {
    if (s_round_audio_ownership_mutex) {
        (void)xSemaphoreGive(s_round_audio_ownership_mutex);
    }
}

esp_err_t round_audio_lifecycle_prepare_shared_bus(unsigned output_volume,
                                                    uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(round_audio_service_acquire(timeout_ms), "round_audio",
                        "peripheral preparation ownership timeout");
    const esp_err_t err = round_audio_service_initialize(output_volume);
    round_audio_service_release_ownership();
    return err;
}

esp_err_t round_audio_service_prepare_for_wake(unsigned output_volume,
                                               uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(round_audio_service_acquire(timeout_ms), "round_audio",
                        "wake preparation ownership timeout");
    const esp_err_t err = round_audio_service_initialize(output_volume);
    round_audio_service_release_ownership();
    return err;
}

esp_err_t round_audio_service_apply_output_volume(unsigned current_volume,
                                                  unsigned requested_volume,
                                                  uint32_t timeout_ms) {
    if (requested_volume > 100 || timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(round_audio_service_acquire(timeout_ms), "round_audio",
                        "volume ownership timeout");
    esp_err_t err = round_audio_service_initialize(current_volume);
    if (err == ESP_OK) err = round_audio_service_set_output_volume(requested_volume);
    round_audio_service_release_ownership();
    return err;
}

esp_err_t round_audio_service_stream_begin(unsigned output_volume,
                                           uint32_t timeout_ms,
                                           const char *diagnostic_label) {
    if (timeout_ms == 0 || s_round_audio_stream_owned) return ESP_ERR_INVALID_STATE;
    ESP_RETURN_ON_ERROR(round_audio_service_acquire(timeout_ms), "round_audio",
                        "foreground stream ownership timeout");
    esp_err_t err = round_audio_service_initialize(output_volume);
    if (err == ESP_OK) err = round_audio_service_restore_input_gain();
    if (err != ESP_OK) {
        round_audio_service_release_ownership();
        return err;
    }
    round_audio_service_capture_pcm_reset(diagnostic_label);
    s_round_audio_stream_owned = true;
    return ESP_OK;
}

esp_err_t round_audio_service_stream_read(int16_t *mono, size_t sample_capacity,
                                          size_t *samples_read, uint16_t *level) {
    if (samples_read) *samples_read = 0;
    if (level) *level = 0;
    if (!s_round_audio_stream_owned || !mono || !samples_read || sample_capacity == 0) {
        return ESP_ERR_INVALID_STATE;
    }
    int16_t wire[512];
    int16_t captured_mono[256];
    size_t total_frames = 0;
    int32_t peak = 0;
    while (total_frames < sample_capacity) {
        size_t received = 0;
        esp_err_t err = round_audio_service_read_pcm(
            wire, sizeof(wire), &received, pdMS_TO_TICKS(1000));
        if (err != ESP_OK) return err;
        size_t frames = round_audio_service_extract_capture_mono(
            wire, received, captured_mono,
            sizeof(captured_mono) / sizeof(captured_mono[0]));
        if (frames == 0) continue;
        if (frames > sample_capacity - total_frames) frames = sample_capacity - total_frames;
        int32_t chunk_peak = round_audio_service_capture_pcm_process(
            captured_mono, frames, mono + total_frames);
        if (chunk_peak > peak) peak = chunk_peak;
        total_frames += frames;
    }
    uint32_t scaled = peak <= 180 ? 0 :
        (uint32_t)(peak - 180) * 1000u / (12000u - 180u);
    if (scaled > 1000) scaled = 1000;
    if (level) *level = (uint16_t)scaled;
    *samples_read = total_frames;
    return ESP_OK;
}

void round_audio_service_stream_end(void) {
    if (!s_round_audio_stream_owned) return;
    s_round_audio_stream_owned = false;
    round_audio_service_release_ownership();
}

esp_err_t round_audio_service_command_capture_begin(unsigned output_volume,
                                                    uint32_t timeout_ms) {
    if (s_round_audio_command_capture_owned) return ESP_ERR_INVALID_STATE;
    ESP_RETURN_ON_ERROR(round_audio_service_stream_begin(output_volume, timeout_ms, "command"),
                        "round_audio", "command capture begin failed");
    s_round_audio_stream_owned = false;
    s_round_audio_command_capture_owned = true;
    /* A new capture generation never inherits a previous command's cancel. */
    round_audio_service_reset_command_capture_stop();
    return ESP_OK;
}

esp_err_t round_audio_service_command_capture_read(int16_t *mono,
                                                    size_t sample_capacity,
                                                    size_t *samples_read,
                                                    round_audio_capture_stats_t *out_stats) {
    if (samples_read) *samples_read = 0;
    if (out_stats) *out_stats = (round_audio_capture_stats_t){0};
    if (!s_round_audio_command_capture_owned || !mono || !samples_read ||
        sample_capacity == 0) return ESP_ERR_INVALID_STATE;
    int16_t wire[512];
    int16_t captured_mono[256];
    size_t received = 0;
    ESP_RETURN_ON_ERROR(round_audio_service_read_pcm(
                            wire, sizeof(wire), &received, pdMS_TO_TICKS(1000)),
                        "round_audio", "command microphone read failed");
    size_t frames = round_audio_service_extract_capture_mono(
        wire, received, captured_mono,
        sizeof(captured_mono) / sizeof(captured_mono[0]));
    if (frames > sample_capacity) frames = sample_capacity;
    if (frames == 0) return ESP_OK;
    const int32_t chunk_peak = round_audio_service_capture_pcm_process(
        captured_mono, frames, mono);
    *samples_read = frames;
    round_audio_service_capture_pcm_stats(mono, frames, chunk_peak, out_stats);
    return ESP_OK;
}

void round_audio_service_command_capture_end(void) {
    if (!s_round_audio_command_capture_owned) return;
    s_round_audio_command_capture_owned = false;
    round_audio_service_release_ownership();
}

void round_audio_service_request_command_capture_stop(void) {
    atomic_store_explicit(&s_round_audio_command_capture_stop_requested,
                          true, memory_order_release);
}

void round_audio_service_reset_command_capture_stop(void) {
    atomic_store_explicit(&s_round_audio_command_capture_stop_requested,
                          false, memory_order_release);
}

bool round_audio_service_command_capture_stop_requested(void) {
    return atomic_load_explicit(&s_round_audio_command_capture_stop_requested,
                                memory_order_acquire);
}

esp_err_t round_audio_service_playback_begin(unsigned output_volume,
                                             uint32_t timeout_ms) {
    if (timeout_ms == 0 || s_round_audio_playback_owner) return ESP_ERR_INVALID_STATE;
    ESP_RETURN_ON_ERROR(round_audio_service_acquire(timeout_ms), "round_audio",
                        "playback ownership timeout");
    esp_err_t err = round_audio_service_initialize(output_volume);
    if (err == ESP_OK) err = round_audio_service_playback_prepare();
    if (err != ESP_OK) {
        (void)round_audio_service_playback_abort();
        round_audio_service_release_ownership();
        return err;
    }
    s_round_audio_playback_stop_requested = false;
    s_round_audio_playback_owner = xTaskGetCurrentTaskHandle();
    return ESP_OK;
}

esp_err_t round_audio_service_playback_write(const int16_t *pcm, size_t frames,
                                             unsigned channels) {
    if (s_round_audio_playback_owner != xTaskGetCurrentTaskHandle() ||
        !pcm || frames == 0 || (channels != 1 && channels != 2)) {
        return ESP_ERR_INVALID_ARG;
    }
    if (s_round_audio_playback_stop_requested) return ESP_ERR_INVALID_STATE;
    size_t offset = 0;
    while (offset < frames) {
        if (s_round_audio_playback_stop_requested) return ESP_ERR_INVALID_STATE;
        size_t count = frames - offset;
        if (count > 256) count = 256;
        size_t written = 0;
        esp_err_t err = round_audio_service_write_pcm(
            pcm + offset * channels, count, channels, &written, pdMS_TO_TICKS(1000));
        if (err != ESP_OK) return err;
        /* The profile adapter reports physical stereo-slot bytes even when
         * the source is mono.  Preserve the established transport contract. */
        if (written != count * 2u * sizeof(int16_t)) return ESP_ERR_TIMEOUT;
        if (offset == 0) {
            err = round_audio_service_playback_reveal();
            if (err != ESP_OK) return err;
        }
        /* `written` is physical stereo-slot bytes, whereas `offset` is source
         * frames.  Advancing by bytes skips samples (and can step beyond the
         * caller's PCM buffer) on every multi-block response. */
        offset += count;
    }
    return ESP_OK;
}

esp_err_t round_audio_service_playback_end(esp_err_t playback_err) {
    if (s_round_audio_playback_owner != xTaskGetCurrentTaskHandle()) {
        return ESP_ERR_INVALID_STATE;
    }
    int16_t silence[256] = {0};
    size_t silence_written = 0;
    esp_err_t finish_err = round_audio_service_playback_finish(
        silence, sizeof(silence) / sizeof(silence[0]), &silence_written,
        pdMS_TO_TICKS(1000));
    s_round_audio_playback_owner = NULL;
    s_round_audio_playback_stop_requested = false;
    round_audio_service_release_ownership();
    return playback_err != ESP_OK ? playback_err : finish_err;
}

void round_audio_service_request_playback_stop(void) {
    if (s_round_audio_playback_owner) s_round_audio_playback_stop_requested = true;
}

/* These values qualify the normalized capture stream, not any business VAD
 * decision.  Keep them beside the selected codec seam so command capture,
 * meeting audio and future round hardware cannot quietly grow independent
 * click suppression or AGC implementations in board_port.c. */
#define ROUND_CAPTURE_INVALID_SAMPLE_ABS 32500
#define ROUND_CAPTURE_DC_BLOCKER_Q15 32604  /* 0.995 */
#define ROUND_CAPTURE_TARGET_RMS 3400
#define ROUND_CAPTURE_MIN_GAIN_Q8 256
#define ROUND_CAPTURE_MAX_GAIN_Q8 (24 * 256)
#define ROUND_CAPTURE_GAIN_ATTACK_SHIFT 1
#define ROUND_CAPTURE_GAIN_RELEASE_SHIFT 4
#define ROUND_CAPTURE_GAIN_UPDATE_FLOOR 96
#define ROUND_CAPTURE_OUTPUT_LIMIT 30000
#define ROUND_WAKE_TARGET_RMS 3400
#define ROUND_WAKE_MIN_GAIN_Q8 256
#define ROUND_WAKE_MAX_GAIN_Q8 (24 * 256)
#define ROUND_WAKE_GAIN_ATTACK_SHIFT 1
#define ROUND_WAKE_GAIN_RELEASE_SHIFT 4
#define ROUND_WAKE_GAIN_UPDATE_FLOOR 96

typedef struct {
    int32_t previous_input;
    int32_t previous_filtered;
    uint32_t gain_q8;
    uint32_t diagnostic_samples;
    uint32_t diagnostic_bad_samples;
    int32_t diagnostic_input_peak;
    int32_t diagnostic_output_peak;
    uint32_t diagnostic_rms;
    const char *diagnostic_label;
} round_capture_pcm_filter_t;

static round_capture_pcm_filter_t s_round_capture_pcm_filter;

typedef struct {
    int16_t last_valid_sample;
    uint32_t gain_q8;
} round_wake_pcm_filter_t;

static round_wake_pcm_filter_t s_round_wake_pcm_filter;
/* The wire-format DMA buffer is an Audio-HAL implementation detail.  A wake
 * capture has a single recognizer owner, so retaining it for the session
 * avoids per-inference allocation while keeping channel packing out of the
 * scene/recognizer policy layer. */
static void *s_round_wake_capture_wire;

static int32_t round_audio_service_sample_magnitude(int32_t sample) {
    return sample < 0 ? -sample : sample;
}

static bool round_audio_service_capture_sample_is_valid(int16_t sample) {
    return round_audio_service_sample_magnitude((int32_t)sample) <
           ROUND_CAPTURE_INVALID_SAMPLE_ABS;
}

static void round_audio_service_capture_pcm_reset(const char *diagnostic_label) {
    memset(&s_round_capture_pcm_filter, 0, sizeof(s_round_capture_pcm_filter));
    /* The known-good codec path is quiet, so a new capture starts ready for
     * nearby speech.  The bounded attack immediately reduces a loud block. */
    s_round_capture_pcm_filter.gain_q8 = ROUND_CAPTURE_MAX_GAIN_Q8;
    s_round_capture_pcm_filter.diagnostic_label = diagnostic_label;
}

static int32_t round_audio_service_capture_pcm_process(const int16_t *input_mono,
                                                       size_t frames,
                                                       int16_t *output_mono) {
    if (!input_mono || !output_mono || frames == 0) return 0;
    round_capture_pcm_filter_t *filter = &s_round_capture_pcm_filter;
    uint64_t energy = 0;
    int32_t chunk_peak = 0;
    for (size_t i = 0; i < frames; ++i) {
        int32_t input = input_mono[i];
        int32_t input_magnitude = round_audio_service_sample_magnitude(input);
        if (input_magnitude > filter->diagnostic_input_peak) {
            filter->diagnostic_input_peak = input_magnitude;
        }
        if (!round_audio_service_capture_sample_is_valid(input_mono[i])) {
            /* Holding the previous input lets the high-pass output decay
             * smoothly instead of turning one damaged word into a click. */
            input = filter->previous_input;
            ++filter->diagnostic_bad_samples;
        }
        int32_t filtered = input - filter->previous_input +
                           (int32_t)(((int64_t)filter->previous_filtered *
                                      ROUND_CAPTURE_DC_BLOCKER_Q15) >> 15);
        filter->previous_input = input;
        filter->previous_filtered = filtered;
        if (filtered > INT16_MAX) filtered = INT16_MAX;
        if (filtered < INT16_MIN) filtered = INT16_MIN;
        output_mono[i] = (int16_t)filtered;
        energy += (uint64_t)((int64_t)filtered * filtered);
    }

    uint32_t rms = (uint32_t)sqrtf((float)(energy / frames));
    if (rms >= ROUND_CAPTURE_GAIN_UPDATE_FLOOR) {
        uint32_t target_q8 = (ROUND_CAPTURE_TARGET_RMS * 256u) / rms;
        if (target_q8 < ROUND_CAPTURE_MIN_GAIN_Q8) target_q8 = ROUND_CAPTURE_MIN_GAIN_Q8;
        if (target_q8 > ROUND_CAPTURE_MAX_GAIN_Q8) target_q8 = ROUND_CAPTURE_MAX_GAIN_Q8;
        unsigned shift = target_q8 < filter->gain_q8
                             ? ROUND_CAPTURE_GAIN_ATTACK_SHIFT
                             : ROUND_CAPTURE_GAIN_RELEASE_SHIFT;
        filter->gain_q8 = (uint32_t)((int32_t)filter->gain_q8 +
                                     ((int32_t)target_q8 - (int32_t)filter->gain_q8) /
                                         (1 << shift));
    }
    filter->diagnostic_rms = rms;

    for (size_t i = 0; i < frames; ++i) {
        int32_t output = (int32_t)(((int64_t)output_mono[i] * filter->gain_q8) >> 8);
        if (output > ROUND_CAPTURE_OUTPUT_LIMIT) output = ROUND_CAPTURE_OUTPUT_LIMIT;
        if (output < -ROUND_CAPTURE_OUTPUT_LIMIT) output = -ROUND_CAPTURE_OUTPUT_LIMIT;
        output_mono[i] = (int16_t)output;
        int32_t output_magnitude = round_audio_service_sample_magnitude(output);
        if (output_magnitude > chunk_peak) chunk_peak = output_magnitude;
        if (output_magnitude > filter->diagnostic_output_peak) {
            filter->diagnostic_output_peak = output_magnitude;
        }
    }

    filter->diagnostic_samples += (uint32_t)frames;
    if (filter->diagnostic_samples >= round_audio_service_sample_rate()) {
        ESP_LOGI("round_audio", "%s mic: peak=%ld rms=%lu bad=%lu; clean peak=%ld gain=%.2f",
                 filter->diagnostic_label ? filter->diagnostic_label : "capture",
                 (long)filter->diagnostic_input_peak,
                 (unsigned long)filter->diagnostic_rms,
                 (unsigned long)filter->diagnostic_bad_samples,
                 (long)filter->diagnostic_output_peak,
                 (double)filter->gain_q8 / 256.0);
        filter->diagnostic_samples = 0;
        filter->diagnostic_bad_samples = 0;
        filter->diagnostic_input_peak = 0;
        filter->diagnostic_output_peak = 0;
    }
    return chunk_peak;
}

/* The shared renderer must not derive signal properties from raw sample
 * buffers.  Keep this beside the selected codec's conditioning so a future
 * round microphone path changes neither VAD code nor recording UI code.
 * `level` retains the established command-start response curve, while
 * `mean_level` uses DC-relative average magnitude for natural-pause VAD. */
static void round_audio_service_capture_pcm_stats(const int16_t *samples,
                                                  size_t frames, int32_t peak,
                                                  round_audio_capture_stats_t *out_stats) {
    if (!out_stats) return;
    *out_stats = (round_audio_capture_stats_t){0};
    if (!samples || frames == 0) return;

    uint32_t scaled_peak = peak <= 180 ? 0 :
        (uint32_t)(peak - 180) * 1000u / (12000u - 180u);
    if (scaled_peak > 1000u) scaled_peak = 1000u;

    int64_t sum = 0;
    for (size_t i = 0; i < frames; ++i) sum += samples[i];
    const int32_t dc = (int32_t)(sum / (int64_t)frames);
    uint64_t deviation_sum = 0;
    for (size_t i = 0; i < frames; ++i) {
        const int32_t deviation = (int32_t)samples[i] - dc;
        deviation_sum += (uint32_t)(deviation < 0 ? -deviation : deviation);
    }
    const uint32_t mean_deviation = (uint32_t)(deviation_sum / frames);
    out_stats->peak = peak;
    out_stats->level = (uint16_t)scaled_peak;
    out_stats->mean_level = mean_deviation >= 12000u ? 1000u :
                            (uint16_t)(mean_deviation * 1000u / 12000u);
}

static void round_audio_service_wake_pcm_reset(void) {
    memset(&s_round_wake_pcm_filter, 0, sizeof(s_round_wake_pcm_filter));
    s_round_wake_pcm_filter.gain_q8 = ROUND_WAKE_MAX_GAIN_Q8;
}

static void round_audio_service_wake_pcm_process(int16_t *mono, size_t frames,
                                                 round_audio_wake_pcm_stats_t *out_stats) {
    if (out_stats) memset(out_stats, 0, sizeof(*out_stats));
    if (!mono || frames == 0) return;

    round_wake_pcm_filter_t *filter = &s_round_wake_pcm_filter;
    int64_t sum = 0;
    uint32_t valid = 0;
    uint16_t invalid = 0;
    int32_t input_peak = 0;
    for (size_t i = 0; i < frames; ++i) {
        const int32_t sample = mono[i];
        const int32_t magnitude = round_audio_service_sample_magnitude(sample);
        if (magnitude > input_peak) input_peak = magnitude;
        if (round_audio_service_capture_sample_is_valid(mono[i])) {
            sum += sample;
            ++valid;
        } else {
            ++invalid;
        }
    }
    const int32_t dc = valid ? (int32_t)(sum / valid) : 0;
    uint64_t energy = 0;
    for (size_t i = 0; i < frames; ++i) {
        if (round_audio_service_capture_sample_is_valid(mono[i])) {
            const int32_t centered = (int32_t)mono[i] - dc;
            energy += (uint64_t)((int64_t)centered * centered);
        }
    }
    const uint32_t rms = (uint32_t)sqrtf((float)(energy / frames));
    if (rms >= ROUND_WAKE_GAIN_UPDATE_FLOOR) {
        uint32_t target_q8 = (ROUND_WAKE_TARGET_RMS * 256u) / rms;
        if (target_q8 < ROUND_WAKE_MIN_GAIN_Q8) target_q8 = ROUND_WAKE_MIN_GAIN_Q8;
        if (target_q8 > ROUND_WAKE_MAX_GAIN_Q8) target_q8 = ROUND_WAKE_MAX_GAIN_Q8;
        const unsigned shift = target_q8 < filter->gain_q8
                                   ? ROUND_WAKE_GAIN_ATTACK_SHIFT
                                   : ROUND_WAKE_GAIN_RELEASE_SHIFT;
        filter->gain_q8 = (uint32_t)((int32_t)filter->gain_q8 +
                                     ((int32_t)target_q8 - (int32_t)filter->gain_q8) /
                                         (1 << shift));
    }
    for (size_t i = 0; i < frames; ++i) {
        int32_t sample;
        if (round_audio_service_capture_sample_is_valid(mono[i])) {
            sample = (int32_t)(((int64_t)((int32_t)mono[i] - dc) * filter->gain_q8) >> 8);
        } else {
            sample = filter->last_valid_sample;
        }
        if (sample > INT16_MAX) sample = INT16_MAX;
        if (sample < INT16_MIN) sample = INT16_MIN;
        filter->last_valid_sample = (int16_t)sample;
        mono[i] = (int16_t)sample;
    }
    if (out_stats) {
        out_stats->input_peak = input_peak;
        out_stats->rms = rms;
        out_stats->invalid_samples = invalid;
        out_stats->gain_q8 = filter->gain_q8;
    }
}

esp_err_t round_audio_service_wake_capture_begin(size_t frames,
                                                 round_audio_wake_capture_t *capture) {
    if (!capture || frames == 0) return ESP_ERR_INVALID_ARG;
    if (s_round_wake_capture_wire) return ESP_ERR_INVALID_STATE;
    memset(capture, 0, sizeof(*capture));
    capture->mono = round_audio_service_allocate_wake_capture_buffer(
        frames * sizeof(*capture->mono));
    s_round_wake_capture_wire = round_audio_service_allocate_wake_capture_buffer(
        round_audio_service_capture_wire_bytes(frames));
    if (!capture->mono || !s_round_wake_capture_wire) {
        round_audio_service_wake_capture_end(capture);
        return ESP_ERR_NO_MEM;
    }
    capture->frames = frames;
    round_audio_service_wake_pcm_reset();
    return ESP_OK;
}

esp_err_t round_audio_service_wake_capture_read(round_audio_wake_capture_t *capture,
                                                uint32_t timeout_ms,
                                                round_audio_wake_pcm_stats_t *out_stats) {
    if (!capture || !capture->mono || !s_round_wake_capture_wire || capture->frames == 0 ||
        timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(round_audio_service_acquire(timeout_ms), "round_audio",
                        "wake microphone ownership timeout");
    size_t received = 0;
    const esp_err_t err = round_audio_service_read_pcm(
        s_round_wake_capture_wire, round_audio_service_capture_wire_bytes(capture->frames),
        &received, pdMS_TO_TICKS(timeout_ms));
    round_audio_service_release_ownership();
    if (err != ESP_OK) return err;
    const size_t frames = round_audio_service_extract_capture_mono(
        s_round_wake_capture_wire, received, capture->mono, capture->frames);
    if (frames < capture->frames) {
        memset(capture->mono + frames, 0,
               (capture->frames - frames) * sizeof(*capture->mono));
    }
    round_audio_service_wake_pcm_process(capture->mono, capture->frames, out_stats);
    return ESP_OK;
}

void round_audio_service_wake_capture_end(round_audio_wake_capture_t *capture) {
    if (!capture) return;
    round_audio_service_free_wake_capture_buffer(capture->mono);
    round_audio_service_free_wake_capture_buffer(s_round_wake_capture_wire);
    s_round_wake_capture_wire = NULL;
    memset(capture, 0, sizeof(*capture));
}

static void round_audio_service_release(void) {
    (void)round_audio_adapter_release_impl();
    s_round_audio_ready = false;
}

static esp_err_t round_audio_service_initialize(unsigned output_volume) {
    if (s_round_audio_ready) return ESP_OK;
    /* A failed initialisation may have attached a codec or opened I2S before
     * returning its error.  Release first so retry cannot inherit a stale
     * controller handle or a partially-owned shared I2C bus. */
    round_audio_service_release();
    const esp_err_t err = round_audio_adapter_initialize_impl(output_volume);
    if (err == ESP_OK) {
        s_round_audio_ready = true;
        return ESP_OK;
    }
    round_audio_service_release();
    return err;
}

static esp_err_t round_audio_service_set_output_volume(unsigned percent) {
    return round_audio_adapter_set_output_volume_impl(percent);
}
static esp_err_t round_audio_service_restore_input_gain(void) {
    return round_audio_adapter_restore_input_gain_impl();
}
void *round_audio_service_allocate_command_wav(size_t bytes) {
    return round_audio_adapter_allocate_command_wav_impl(bytes);
}
void round_audio_service_free_command_wav(void *buffer) {
    round_audio_adapter_free_command_wav_impl(buffer);
}
static void *round_audio_service_allocate_wake_capture_buffer(size_t bytes) {
    return round_audio_adapter_allocate_wake_capture_buffer_impl(bytes);
}
static void round_audio_service_free_wake_capture_buffer(void *buffer) {
    round_audio_adapter_free_wake_capture_buffer_impl(buffer);
}
static size_t round_audio_service_capture_wire_bytes(size_t frames) {
    return round_audio_adapter_capture_wire_bytes_impl(frames);
}
static size_t round_audio_service_extract_capture_mono(const int16_t *wire, size_t wire_bytes,
                                                       int16_t *mono, size_t mono_capacity) {
    return round_audio_adapter_extract_capture_mono_impl(wire, wire_bytes, mono,
                                                         mono_capacity);
}
static esp_err_t round_audio_service_read_pcm(void *buffer, size_t capacity,
                                              size_t *received, TickType_t timeout) {
    return round_audio_adapter_read_pcm_impl(buffer, capacity, received, timeout);
}
static esp_err_t round_audio_service_write_pcm(const int16_t *pcm, size_t frames,
                                               unsigned channels, size_t *written,
                                               TickType_t timeout) {
    return round_audio_adapter_write_pcm_impl(pcm, frames, channels, written, timeout);
}
static esp_err_t round_audio_service_playback_prepare(void) {
    return round_audio_adapter_playback_prepare_impl();
}
static esp_err_t round_audio_service_playback_reveal(void) {
    return round_audio_adapter_playback_reveal_impl();
}
static esp_err_t round_audio_service_playback_abort(void) {
    return round_audio_adapter_playback_abort_impl();
}
static esp_err_t round_audio_service_playback_finish(const int16_t *silence,
                                                     size_t silence_frames,
                                                     size_t *written,
                                                     TickType_t timeout) {
    return round_audio_adapter_playback_finish_impl(silence, silence_frames,
                                                    written, timeout);
}
