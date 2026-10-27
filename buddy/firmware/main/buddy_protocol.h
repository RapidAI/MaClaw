#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"

/** Installs the USB-Serial/JTAG driver and starts the receive/heartbeat tasks. */
esp_err_t buddy_protocol_start(void);

void buddy_protocol_send_hello(void);
void buddy_protocol_send_state_ack(bool applied, const char *page, int missing_glyphs);
void buddy_protocol_send_heartbeat(void);
void buddy_protocol_send_log(const char *level, const char *msg);

/** Builds the current page name from the live view ("tasks"/"standby"/"prompt"). */
const char *buddy_protocol_current_page(void);
