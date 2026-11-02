#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"

/** Installs the USB-Serial/JTAG driver and starts the receive/heartbeat tasks. */
esp_err_t buddy_protocol_start(void);

void buddy_protocol_send_hello(void);
void buddy_protocol_send_state_ack(bool applied, const char *page, int missing_glyphs, int64_t ts_ms);
void buddy_protocol_send_heartbeat(void);
void buddy_protocol_send_log(const char *level, const char *msg);

/** Acknowledges one committed pet-asset chunk so the host can pace the
 * transfer without overflowing the USB receive buffer. */
void buddy_protocol_send_asset_ack(int frame, int bytes, bool ready);

/** Reports the current notification-sound config back to the host. */
void buddy_protocol_send_sound(void);

/** Short-press on the glass. index is the task now shown, or -1. */
void buddy_protocol_send_input(const char *gesture, int index);

/** Builds the current page name from the live view ("tasks"/"standby"/"prompt"). */
const char *buddy_protocol_current_page(void);
