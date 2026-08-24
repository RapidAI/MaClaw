#pragma once

/* Minimal parser declaration surface for host-testing Pet Asset's value
 * contract. The test does not call Hub JSON decoding; it links with section
 * garbage collection so these parser-only functions need no implementation. */

typedef struct cJSON {
    int valueint;
    char *valuestring;
} cJSON;

int cJSON_IsObject(const cJSON *item);
int cJSON_IsNumber(const cJSON *item);
int cJSON_IsString(const cJSON *item);
int cJSON_IsArray(const cJSON *item);
cJSON *cJSON_GetObjectItemCaseSensitive(const cJSON *object, const char *string);
int cJSON_GetArraySize(const cJSON *array);
cJSON *cJSON_GetArrayItem(const cJSON *array, int index);
