#pragma once

/* Compile-time compact Peripheral selection.  Fangtang's implementation is
 * intentionally included in this one source-owner translation unit because
 * it carries its private ADC worker and hardware state. */
#include "sdkconfig.h"

#define MACLAW_COMPACT_PERIPHERAL_ADAPTER_IMPLEMENTATION 1
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#include "boards/fangtang_4g/fangtang_peripheral_adapter.h"
#include "boards/fangtang_4g/fangtang_peripheral_adapter.c"
#else
#include "boards/bread_compact/bread_peripheral_adapter.h"
#endif
#undef MACLAW_COMPACT_PERIPHERAL_ADAPTER_IMPLEMENTATION
