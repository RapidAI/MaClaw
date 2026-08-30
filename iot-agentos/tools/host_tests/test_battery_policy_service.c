#include <stdio.h>
#include <string.h>

#include "freertos/FreeRTOS.h"
#include "battery_policy_service.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static int64_t s_time_us;
static device_power_telemetry_t s_telemetry;
static bool s_provider_reenter_prepare;
static bool s_provider_return_false;
static device_status_t s_reentrant_prepare_status;
static unsigned s_checkpoint_calls;
static device_status_t s_checkpoint_status;

static device_status_t checkpoint_callback(uint32_t timeout_ms, void *context) {
    (void)context;
    if (timeout_ms == 0u) return DEVICE_STATUS_INVALID_ARGUMENT;
    ++s_checkpoint_calls;
    if (context) {
        /* The late-success fixture consumes exactly the callback allowance. */
        s_time_us += (int64_t)timeout_ms * 1000;
    }
    return s_checkpoint_status;
}

int64_t esp_timer_get_time(void) { return s_time_us; }
void vTaskDelay(TickType_t ticks) { s_time_us += (int64_t)(ticks ? ticks : 1u) * 1000; }

bool device_power_get_telemetry(device_power_telemetry_t *out_telemetry) {
    if (!out_telemetry) return false;
    if (s_provider_reenter_prepare) {
        s_reentrant_prepare_status = battery_policy_service_prepare_system_sleep(1);
    }
    *out_telemetry = s_telemetry;
    return s_provider_return_false ? false : s_telemetry.available;
}

static void set_telemetry(bool available, bool charging, uint8_t level) {
    s_telemetry = (device_power_telemetry_t){
        .available = available,
        .charging = charging,
        .level_percent = level,
    };
}

int main(void) {
    device_battery_policy_snapshot_t snapshot = {0};
    device_battery_policy_snapshot_t sentinel = {
        .struct_size = 0xfeedu,
        .abi_version = 0xbeefu,
    };

    CHECK(battery_policy_service_prepare_system_sleep(1) == DEVICE_STATUS_UNAVAILABLE);
    CHECK(battery_policy_service_init() == DEVICE_STATUS_OK);

    set_telemetry(true, false, 20);
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_CONSERVE);
    CHECK(snapshot.optional_work_allowed);
    CHECK(!snapshot.high_power_work_allowed);
    CHECK(battery_policy_service_limit_backlight_percent(100u) == 65u);

    /* An impossible normalized percentage is rejected without publishing a
     * partial snapshot or advancing the hysteresis state. */
    set_telemetry(true, false, 101);
    snapshot = sentinel;
    CHECK(!battery_policy_service_get_snapshot(&snapshot));
    CHECK(memcmp(&snapshot, &sentinel, sizeof(snapshot)) == 0);
    set_telemetry(true, false, 20);
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_CONSERVE);

    /* A provider failure must override any bytes it may have left in the
     * output structure; the policy must not publish that stale observation. */
    set_telemetry(true, false, 1);
    s_provider_return_false = true;
    snapshot = sentinel;
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.telemetry_available == false);
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_NORMAL);
    CHECK(snapshot.optional_work_allowed && snapshot.high_power_work_allowed);
    CHECK(memcmp(&snapshot, &sentinel, sizeof(snapshot)) != 0);
    s_provider_return_false = false;
    set_telemetry(true, false, 20);
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_CONSERVE);

    /* The provider can trigger a reentrant PREPARE while this snapshot still
     * owns an admission. It must close new reads and wait until this read
     * releases; reentrancy cannot publish a stale policy value afterwards. */
    set_telemetry(true, false, 10);
    s_reentrant_prepare_status = DEVICE_STATUS_OK;
    s_provider_reenter_prepare = true;
    snapshot = sentinel;
    CHECK(!battery_policy_service_get_snapshot(&snapshot));
    s_provider_reenter_prepare = false;
    CHECK(s_reentrant_prepare_status == DEVICE_STATUS_TIMEOUT);
    CHECK(memcmp(&snapshot, &sentinel, sizeof(snapshot)) == 0);
    /* The timed-out prepare has deliberately retained admission closed. */
    snapshot = sentinel;
    CHECK(!battery_policy_service_get_snapshot(&snapshot));
    CHECK(memcmp(&snapshot, &sentinel, sizeof(snapshot)) == 0);
    CHECK(!battery_policy_service_allows_optional_work());
    battery_policy_service_abort_system_sleep_prepare();

    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_CONSERVE);
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_CONSERVE);
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_PROTECT);
    CHECK(!snapshot.optional_work_allowed);
    CHECK(!snapshot.high_power_work_allowed);
    CHECK(battery_policy_service_limit_backlight_percent(100u) == 35u);
    CHECK(battery_policy_service_set_emergency_checkpoint_callback(
              checkpoint_callback, NULL) == DEVICE_STATUS_OK);
    s_checkpoint_status = DEVICE_STATUS_TIMEOUT;
    CHECK(battery_policy_service_run_emergency_checkpoint(10) == DEVICE_STATUS_TIMEOUT);
    CHECK(s_checkpoint_calls == 1u);
    /* A failed write is terminal for this PROTECT generation; telemetry
     * polling must not amplify flash damage with retries. */
    CHECK(battery_policy_service_run_emergency_checkpoint(10) == DEVICE_STATUS_BUSY);
    CHECK(s_checkpoint_calls == 1u);
    CHECK(battery_policy_service_set_emergency_checkpoint_callback(
              checkpoint_callback, NULL) == DEVICE_STATUS_OK);
    s_checkpoint_status = DEVICE_STATUS_OK;
    /* Replacing the callback cannot reopen a failed generation. */
    CHECK(battery_policy_service_run_emergency_checkpoint(10) == DEVICE_STATUS_BUSY);

    /* Reset the policy generation without a callback, then install a callback
     * that reports success only after consuming the parent budget. */
    set_telemetry(true, true, 50);
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(battery_policy_service_set_emergency_checkpoint_callback(NULL, NULL) ==
          DEVICE_STATUS_OK);
    set_telemetry(true, false, 10);
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_PROTECT);
    CHECK(battery_policy_service_set_emergency_checkpoint_callback(
              checkpoint_callback, (void *)1) == DEVICE_STATUS_OK);
    s_checkpoint_status = DEVICE_STATUS_OK;
    CHECK(battery_policy_service_run_emergency_checkpoint(10) == DEVICE_STATUS_TIMEOUT);
    CHECK(s_checkpoint_calls == 2u);
    CHECK(battery_policy_service_run_emergency_checkpoint(10) == DEVICE_STATUS_BUSY);
    set_telemetry(true, true, 50);
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_NORMAL);
    CHECK(battery_policy_service_limit_backlight_percent(100u) == 100u);
    set_telemetry(true, false, 10);
    /* Re-entering PROTECT with the callback already installed automatically
     * consumes the one-shot checkpoint budget. */
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_PROTECT);
    CHECK(s_checkpoint_calls == 3u);
    CHECK(battery_policy_service_run_emergency_checkpoint(10) == DEVICE_STATUS_BUSY);
    CHECK(!battery_policy_service_try_begin_emergency_checkpoint());

    /* A transient provider outage must not erase an already-confirmed
     * PROTECT generation. The safety latch survives missing telemetry, while
     * recovery still requires a subsequent valid observation. */
    s_provider_return_false = true;
    snapshot = sentinel;
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_PROTECT);
    CHECK(!snapshot.telemetry_available);
    CHECK(!snapshot.optional_work_allowed);
    CHECK(battery_policy_service_limit_backlight_percent(100u) == 35u);
    s_provider_return_false = false;

    CHECK(battery_policy_service_prepare_system_sleep(10) == DEVICE_STATUS_OK);
    snapshot = sentinel;
    CHECK(!battery_policy_service_get_snapshot(&snapshot));
    CHECK(memcmp(&snapshot, &sentinel, sizeof(snapshot)) == 0);
    CHECK(!battery_policy_service_allows_high_power_work());
    battery_policy_service_abort_system_sleep_prepare();

    set_telemetry(true, true, 1);
    CHECK(battery_policy_service_get_snapshot(&snapshot));
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_NORMAL);
    CHECK(battery_policy_service_allows_optional_work());
    CHECK(battery_policy_service_allows_high_power_work());

    CHECK(battery_policy_service_deinit(10) == DEVICE_STATUS_OK);
    puts("PASS Battery Policy closes telemetry admission during System Sleep");
    return 0;
}
