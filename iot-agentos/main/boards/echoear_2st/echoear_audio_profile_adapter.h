#pragma once

/* EchoEar codec/I2S electrical facts.  Peripheral touch state is intentionally
 * elsewhere so the selected Audio profile does not import panel or touch
 * controller implementation. */

#include "driver/gpio.h"
#include "driver/i2s_std.h"

#define ECHOEAR_AUDIO_I2C_SCL GPIO_NUM_11
#define ECHOEAR_AUDIO_I2C_SDA GPIO_NUM_12
#define ECHOEAR_AUDIO_MCLK GPIO_NUM_10
#define ECHOEAR_AUDIO_BCLK GPIO_NUM_15
#define ECHOEAR_AUDIO_WS GPIO_NUM_16
#define ECHOEAR_AUDIO_DOUT GPIO_NUM_14
#define ECHOEAR_AUDIO_DIN GPIO_NUM_13
#define ECHOEAR_AUDIO_PA_ENABLE GPIO_NUM_18
#define ECHOEAR_AUDIO_ES7210_ADDRESS 0x40
#define ECHOEAR_AUDIO_ES8311_ADDRESS 0x18
#define ECHOEAR_AUDIO_ES8311_DAC_MUTE_REG 0x31
#define ECHOEAR_AUDIO_ES8311_DAC_VOLUME_REG 0x32
#define ECHOEAR_AUDIO_OUTPUT_VOLUME_DEFAULT 70
#define ECHOEAR_AUDIO_RATE 16000
#define ECHOEAR_AUDIO_MCLK_MULTIPLE I2S_MCLK_MULTIPLE_256
