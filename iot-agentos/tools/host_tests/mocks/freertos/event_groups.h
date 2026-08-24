#pragma once

#include "freertos/FreeRTOS.h"

EventGroupHandle_t xEventGroupCreate(void);
void vEventGroupDelete(EventGroupHandle_t event_group);
EventBits_t xEventGroupSetBits(EventGroupHandle_t event_group,
                               EventBits_t bits_to_set);
EventBits_t xEventGroupClearBits(EventGroupHandle_t event_group,
                                 EventBits_t bits_to_clear);
EventBits_t xEventGroupWaitBits(EventGroupHandle_t event_group,
                                EventBits_t bits_to_wait_for,
                                BaseType_t clear_on_exit,
                                BaseType_t wait_for_all_bits,
                                TickType_t ticks_to_wait);
