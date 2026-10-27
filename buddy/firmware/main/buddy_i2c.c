#include "buddy_i2c.h"

#include "driver/i2c_master.h"

#define BUDDY_I2C_SCL_GPIO 11
#define BUDDY_I2C_SDA_GPIO 12

static i2c_master_bus_handle_t s_bus;
static bool s_attempted;

i2c_master_bus_handle_t buddy_i2c_bus(void) {
    if (s_bus) return s_bus;
    if (s_attempted) return NULL;
    s_attempted = true;

    const i2c_master_bus_config_t bus_cfg = {
        .i2c_port = I2C_NUM_0,
        .sda_io_num = BUDDY_I2C_SDA_GPIO,
        .scl_io_num = BUDDY_I2C_SCL_GPIO,
        .clk_source = I2C_CLK_SRC_DEFAULT,
        .glitch_ignore_cnt = 7,
        .flags.enable_internal_pullup = true,
    };
    if (i2c_new_master_bus(&bus_cfg, &s_bus) != ESP_OK) {
        s_bus = NULL;
    }
    return s_bus;
}
