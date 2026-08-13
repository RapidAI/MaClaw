#pragma once

/* Selected circular Display adapter.
 *
 * This is the one profile-selection seam for round_display_service.c.  The
 * service owns normalized display lifecycle and animation semantics; the
 * selector owns only the compile-time mapping to a controller implementation.
 * Renderers, visual profiles and public Device/Platform contracts must never
 * include it, otherwise a future panel would again require a shared-scene
 * change merely to select its physical transport.
 */

#include "sdkconfig.h"

#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
#include "boards/waveshare_amoled_1_75c/waveshare_display_adapter.h"
#else
/* EchoEar's established adapter exposes its static controller helpers only
 * while included by its Display source owner.  Keep that implementation
 * guard in this selector rather than leaking it into the shared service. */
#define MACLAW_ROUND_DISPLAY_ADAPTER_IMPLEMENTATION 1
#include "boards/echoear_2st/echoear_hardware_adapter.h"
#undef MACLAW_ROUND_DISPLAY_ADAPTER_IMPLEMENTATION
#endif
