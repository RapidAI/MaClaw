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
static device_status_t s_reentrant_prepare_status;

int64_t esp_timer_get_time(void) { return s_time_us; }
void vTaskDelay(TickType_t ticks) { s_time_us += (int64_t)(ticks ? ticks : 1u) * 1000; }

bool device_power_get_telemetry(device_power_telemetry_t *out_telemetry) {
    if (!out_telemetry) return false;
    if (s_provider_reenter_prepare) {
        s_reentrant_prepare_status = battery_policy_service_prepare_system_sleep(1);
    }
    *out_telemetry = s_telemetry;
    return s_telemetry.available;
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
    CHECK(snapshot.level == DEVICE_BATTERY_POLICY_PROTECT);
    CHECK(!snapshot.optional_work_allowed);
    CHECK(!snapshot.high_power_work_allowed);

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
