#pragma once

/* Compile-time compact Display selection.  Included only by the Display
 * source owner; renderers receive its normalized service contract instead. */
#include "sdkconfig.h"

#define MACLAW_COMPACT_DISPLAY_ADAPTER_IMPLEMENTATION 1
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#include "boards/fangtang_4g/fangtang_display_adapter.h"
#else
#include "boards/bread_compact/bread_display_adapter.h"
#endif
#undef MACLAW_COMPACT_DISPLAY_ADAPTER_IMPLEMENTATION
