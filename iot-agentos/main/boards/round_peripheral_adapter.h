#pragma once

/* Selected round Peripheral adapter.  It is private to
 * round_peripheral_service.c and must never be included by a renderer, Audio
 * service or public Device/Platform header. */

#include "sdkconfig.h"

#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
#include "waveshare_amoled_1_75c/waveshare_peripheral_adapter.h"
#else
#include "echoear_2st/echoear_peripheral_adapter.h"
#endif
