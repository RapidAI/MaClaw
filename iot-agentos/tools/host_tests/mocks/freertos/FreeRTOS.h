#pragma once

#include <stdint.h>

typedef uint32_t TickType_t;
typedef int BaseType_t;
typedef uint32_t EventBits_t;

typedef struct host_event_group *EventGroupHandle_t;
typedef struct {
    uint32_t unused;
} StaticSemaphore_t;
typedef void *SemaphoreHandle_t;
typedef int portMUX_TYPE;

#define pdTRUE 1
#define pdFALSE 0
#define pdMS_TO_TICKS(milliseconds) ((TickType_t)(milliseconds))
#define BIT0 ((EventBits_t)1u)
#define portTICK_PERIOD_MS 1u
#define portMUX_INITIALIZER_UNLOCKED 0
#define taskENTER_CRITICAL(lock) ((void)(lock))
#define taskEXIT_CRITICAL(lock) ((void)(lock))

TickType_t xTaskGetTickCount(void);
void vTaskDelay(TickType_t ticks);
