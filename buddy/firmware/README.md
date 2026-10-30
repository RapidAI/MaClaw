# buddy firmware

ESP-IDF 6.0.2 project for the MaClaw USB companion display. Two panel
profiles are supported (select with `CONFIG_BUDDY_PANEL`):

| Profile | Hardware |
| --- | --- |
| `CONFIG_BUDDY_PANEL_ST77916_360` (default) | EchoEar-2ST / ESP32-S3-R8, ST77916 360×360 round panel over QSPI |
| `CONFIG_BUDDY_PANEL_NV3023_240` | Xingzhi-class "无名星智 4G" module, NV3023A 240×240 over SPI (visible window at GRAM row 80) |

The UI always composes one 360×360 canvas; the NV3023 profile flushes a 3:2
nearest-neighbour reduction of it, so no layout code is panel-aware. That
module also has no ES8311 and no PA gate: its amplifier hangs directly off I2S
(`DOUT 7 / BCLK 15 / WS 16`, no MCLK), and its display pins collide with the
EchoEar codec pins, so the audio wiring moves with the panel choice.

Because nothing gates that amplifier, `main/buddy_sound.c` follows the ESP32
client's direct-I2S path (`iot-agentos`, `fangtang_4g` board — the same wiring):
the transmitter is only clocked while a clip plays (`i2s_channel_enable` in
`play_effect`, `i2s_channel_disable` after the DMA queue drains), samples are
scaled by a software volume of 70% instead of going out at full scale into a
fixed-gain amp, and 8 ms of silence is flushed before the clocks stop. The
codec profile keeps its ES8311 volume register and its always-clocked MCLK,
which the codec needs before it can be programmed.

## Build

```bat
cd buddy\firmware
build.cmd -B build build
```

`build.cmd` clears `MSYSTEM` (export.bat refuses to run under a Git Bash
environment) and calls `C:\esp\v6.0.2\esp-idf\export.bat`.

Output: `build/buddy_firmware.bin` — 1.87 MB, 49% of the 3.625 MB app
partition. `CONFIG_ESP_CONSOLE_NONE` and `CONFIG_ESP_CONSOLE_SECONDARY_NONE`
keep the USB port free of ESP-IDF log output; diagnostics travel as protocol
`log` frames.

## Layout

| Path | Purpose |
| --- | --- |
| `main/buddy_display.c` | Panel bring-up for the selected profile (ST77916 QSPI vendor sequence or NV3023A SPI vendor sequence + IDMOFF), LEDC backlight, PSRAM canvas, 40-row internal-DMA stripes with 360→240 downsampling. The NV3023 glass is mounted rotated 180°, so it needs a mirror; MADCTL `MY` makes it upright and `BUDDY_DISPLAY_FLIP_X` reverses each staged row, because this panel ignores the MADCTL `MX` bit |
| `main/buddy_panel_init.h` / `main/buddy_panel_init_nv3023.h` | Vendor init tables for the ST77916 and NV3023A panels |
| `main/buddy_font.c` | ASCII 5×7 + packed 24×24 CJK (`cjk24_cjk.bin` U+4E00–U+9FFF + generated `cjk24_ext.bin` punctuation/full-width), box fallback counts missing glyphs |
| `main/buddy_ui.c` | Task page (counters/title/progress), standby page (pet + weather + date), prompt page |
| `main/buddy_protocol.c` | USB-Serial/JTAG driver, line framing, cJSON parse, seq gate, acks, heartbeat |
| `main/buddy_state.c` | Shared view + NVS weather cache (namespace `buddy`) |
| `main/buddy_touch.c` | Best-effort CST816 polling on I2C 11/12, compiled out of the NV3023 profile (there 11/12 are the 4G modem UART); missing controller is non-fatal |
| `main/main.c` | Boot order: NVS → state → display → touch → protocol → UI task |
| `components/cjson`, `components/esp_lcd_st77916` | Vendored components (no network needed) |
| `main/idf_component.yml` | Declares the NV3023 driver (`78/esp_lcd_nv3023`), fetched into the git-ignored `managed_components/` on first configure |
| `tools/generate_buddy_font.py` | Regenerates `cjk24_ext.bin` + index from `C:\Windows\Fonts\msyh.ttc` |

## Partitioning

`partitions.csv` is a byte-for-byte copy of `iot-agentos/partitions.csv`
(16 MB layout): app at `0x10000` (3.625 MB), `model`/`storage` untouched, so
flashing only the app keeps NVS and recording data. `tools/flash-buddy.ps1`
backs up the previous app image before writing.
