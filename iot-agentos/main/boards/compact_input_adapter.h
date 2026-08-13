#pragma once

/* Compile-time compact Input selection; only compact_input_service owns it. */
#include "sdkconfig.h"

#define MACLAW_COMPACT_INPUT_ADAPTER_IMPLEMENTATION 1
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#include "boards/fangtang_4g/fangtang_input_adapter.h"
#else
#include "boards/bread_compact/bread_input_adapter.h"
#endif
#undef MACLAW_COMPACT_INPUT_ADAPTER_IMPLEMENTATION
