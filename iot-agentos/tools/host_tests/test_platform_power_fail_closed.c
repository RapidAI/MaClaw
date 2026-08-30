#include <stdio.h>

#include "compact_input_service.h"
#include "compact_peripheral_service.h"
#include "platform_power_profile.h"
#include "round_input_service.h"
#include "round_peripheral_service.h"

#ifndef TEST_COMPACT_PROFILE
#error "TEST_COMPACT_PROFILE must be defined as 0 or 1"
#endif

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static esp_err_t s_input_prepare_result = ESP_OK;
static esp_err_t s_peripheral_prepare_result = ESP_OK;
static unsigned s_input_prepare_calls;
static unsigned s_peripheral_prepare_calls;
static unsigned s_input_abort_calls;
static unsigned s_peripheral_abort_calls;
static unsigned s_power_level;
static bool s_power_charging;
static bool s_power_status_available;

esp_err_t compact_input_service_prepare_system_sleep(uint32_t timeout_ms) {
    CHECK(timeout_ms != 0);
    ++s_input_prepare_calls;
    return s_input_prepare_result;
}
void compact_input_service_abort_system_sleep_prepare(void) {
    ++s_input_abort_calls;
}
esp_err_t compact_peripheral_service_prepare_system_sleep(uint32_t timeout_ms) {
    CHECK(timeout_ms != 0);
    ++s_peripheral_prepare_calls;
    return s_peripheral_prepare_result;
}
void compact_peripheral_service_abort_system_sleep_prepare(void) {
    ++s_peripheral_abort_calls;
}
bool compact_peripheral_service_get_power_status(unsigned *level_percent, bool *charging) {
    if (level_percent) *level_percent = s_power_level;
    if (charging) *charging = s_power_charging;
    return s_power_status_available;
}

esp_err_t round_input_service_prepare_system_sleep(uint32_t timeout_ms) {
    CHECK(timeout_ms != 0);
    ++s_input_prepare_calls;
    return s_input_prepare_result;
}
void round_input_service_abort_system_sleep_prepare(void) {
    ++s_input_abort_calls;
}
bool round_peripheral_service_get_power_status(unsigned *level_percent, bool *charging) {
    if (level_percent) *level_percent = s_power_level;
    if (charging) *charging = s_power_charging;
    return s_power_status_available;
}

static void reset_observation(void) {
    s_input_prepare_result = ESP_OK;
    s_peripheral_prepare_result = ESP_OK;
    s_input_prepare_calls = 0;
    s_peripheral_prepare_calls = 0;
    s_input_abort_calls = 0;
    s_peripheral_abort_calls = 0;
    s_power_level = 50u;
    s_power_charging = false;
    s_power_status_available = false;
}

static device_status_t prepare(void) {
    return platform_power_profile_prepare_verified_sleep(
        DEVICE_POWER_STATE_LIGHT_SLEEP, DEVICE_WAKE_SOURCE_TIMER, 10);
}

int main(void) {
    CHECK(platform_power_profile_prepare_verified_sleep(
              DEVICE_POWER_STATE_DISPLAY_OFF, DEVICE_WAKE_SOURCE_TIMER, 10) ==
          DEVICE_STATUS_INVALID_ARGUMENT);

    /* Profile telemetry is normalized at the Platform Power boundary. An
     * impossible percentage must fail closed instead of being clamped. */
    s_power_status_available = true;
    s_power_level = 101u;
    uint8_t level_percent = 0u;
    bool charging = false;
    CHECK(!platform_power_profile_get_telemetry(&level_percent, &charging));
    s_power_level = 50u;
    CHECK(platform_power_profile_get_telemetry(&level_percent, &charging));
    CHECK(level_percent == 50u && !charging);
    s_power_status_available = false;
    CHECK(platform_power_profile_prepare_verified_sleep(
              DEVICE_POWER_STATE_LIGHT_SLEEP, 0, 10) == DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(platform_power_profile_prepare_verified_sleep(
              DEVICE_POWER_STATE_LIGHT_SLEEP, DEVICE_WAKE_SOURCE_TIMER, 0) ==
          DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(platform_power_profile_commit_verified_sleep(
              DEVICE_POWER_STATE_LIGHT_SLEEP, DEVICE_WAKE_SOURCE_TIMER, 0) ==
          DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(platform_power_profile_resume_verified_sleep(
              DEVICE_POWER_STATE_DISPLAY_OFF, 10) == DEVICE_STATUS_INVALID_ARGUMENT);

    reset_observation();
    CHECK(prepare() == DEVICE_STATUS_UNAVAILABLE);
    CHECK(s_input_prepare_calls == 1);
#if TEST_COMPACT_PROFILE
    CHECK(s_peripheral_prepare_calls == 1);
    CHECK(s_input_abort_calls == 1);
    CHECK(s_peripheral_abort_calls == 1);
#else
    CHECK(s_peripheral_prepare_calls == 0);
    CHECK(s_input_abort_calls == 1);
#endif

    reset_observation();
    s_input_prepare_result = ESP_ERR_NOT_SUPPORTED;
    CHECK(prepare() == DEVICE_STATUS_UNAVAILABLE);
    CHECK(s_input_prepare_calls == 1);
    CHECK(s_peripheral_prepare_calls == 0);

#if TEST_COMPACT_PROFILE
    reset_observation();
    s_peripheral_prepare_result = ESP_ERR_TIMEOUT;
    CHECK(prepare() == DEVICE_STATUS_TIMEOUT);
    CHECK(s_input_prepare_calls == 1);
    CHECK(s_peripheral_prepare_calls == 1);
    CHECK(s_input_abort_calls == 1);
    CHECK(s_peripheral_abort_calls == 0);
#endif

    reset_observation();
    CHECK(platform_power_profile_abort_verified_sleep(
              DEVICE_POWER_STATE_LIGHT_SLEEP, 10) == DEVICE_STATUS_OK);
    CHECK(s_input_abort_calls == 1);
#if TEST_COMPACT_PROFILE
    CHECK(s_peripheral_abort_calls == 1);
#endif
    CHECK(platform_power_profile_commit_verified_sleep(
              DEVICE_POWER_STATE_LIGHT_SLEEP, DEVICE_WAKE_SOURCE_TIMER, 10) ==
          DEVICE_STATUS_UNAVAILABLE);
    CHECK(platform_power_profile_resume_verified_sleep(
              DEVICE_POWER_STATE_LIGHT_SLEEP, 10) == DEVICE_STATUS_OK);

    puts("PASS Power profile remains fail-closed until electrical HIL");
    return 0;
}
