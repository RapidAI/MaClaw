#include "connectivity_service.h"

#include "freertos/FreeRTOS.h"

/*
 * The service owns only the small, hardware-neutral observed/selected state.
 * Wi-Fi and ML307 remain independent transport adapters; they publish their
 * readiness here after their own start/recovery work.  Business code can then
 * ask one question -- is the selected uplink ready? -- without importing a
 * modem implementation or a board Kconfig symbol.
 */
static portMUX_TYPE s_connectivity_lock = portMUX_INITIALIZER_UNLOCKED;
static device_uplink_t s_active_uplink = DEVICE_UPLINK_WIFI;
static bool s_wifi_ready;
static bool s_cellular_ready;

void connectivity_service_set_active_uplink(device_uplink_t uplink) {
    if (uplink != DEVICE_UPLINK_WIFI && uplink != DEVICE_UPLINK_CELLULAR) return;
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_active_uplink != uplink) {
        s_active_uplink = uplink;
        /* A readiness observation belongs to one bounded transport session.
         * When a profile changes its selected uplink, a prior session of the
         * newly selected adapter must not make the shared query report ready
         * before that adapter has completed a fresh start/recovery cycle. */
        if (uplink == DEVICE_UPLINK_CELLULAR) {
            s_cellular_ready = false;
        } else {
            s_wifi_ready = false;
        }
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

bool connectivity_service_is_active_cellular(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool cellular = s_active_uplink == DEVICE_UPLINK_CELLULAR;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return cellular;
}

void connectivity_service_set_wifi_ready(bool ready) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    s_wifi_ready = ready;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

void connectivity_service_set_cellular_ready(bool ready) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    s_cellular_ready = ready;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

bool connectivity_service_is_active_uplink_ready(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool ready = s_active_uplink == DEVICE_UPLINK_CELLULAR
                     ? s_cellular_ready
                     : s_wifi_ready;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return ready;
}

bool connectivity_service_get_snapshot(device_connectivity_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_connectivity_lock);
    out_snapshot->active_uplink = s_active_uplink;
    out_snapshot->wifi_ready = s_wifi_ready;
    out_snapshot->cellular_ready = s_cellular_ready;
    out_snapshot->ready = s_active_uplink == DEVICE_UPLINK_CELLULAR
                              ? s_cellular_ready
                              : s_wifi_ready;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return true;
}
