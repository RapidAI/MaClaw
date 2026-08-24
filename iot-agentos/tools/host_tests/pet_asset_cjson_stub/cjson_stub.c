#include "cJSON.h"

int cJSON_IsObject(const cJSON *item) { (void)item; return 0; }
int cJSON_IsNumber(const cJSON *item) { (void)item; return 0; }
int cJSON_IsString(const cJSON *item) { (void)item; return 0; }
int cJSON_IsArray(const cJSON *item) { (void)item; return 0; }
cJSON *cJSON_GetObjectItemCaseSensitive(const cJSON *object, const char *string) {
    (void)object; (void)string; return 0;
}
int cJSON_GetArraySize(const cJSON *array) { (void)array; return 0; }
cJSON *cJSON_GetArrayItem(const cJSON *array, int index) {
    (void)array; (void)index; return 0;
}
