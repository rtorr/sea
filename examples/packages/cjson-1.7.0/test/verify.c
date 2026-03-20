#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <cjson/cJSON.h>

int main(void) {
    /* Create JSON */
    cJSON *obj = cJSON_CreateObject();
    if (!obj) return 1;
    cJSON_AddStringToObject(obj, "test", "verify");
    cJSON_AddNumberToObject(obj, "value", 42);

    /* Serialize */
    char *json = cJSON_PrintUnformatted(obj);
    if (!json) return 1;

    /* Parse back */
    cJSON *parsed = cJSON_Parse(json);
    if (!parsed) return 1;

    cJSON *val = cJSON_GetObjectItemCaseSensitive(parsed, "value");
    if (!cJSON_IsNumber(val) || val->valuedouble != 42) {
        fprintf(stderr, "verify: value mismatch\n");
        return 1;
    }

    cJSON_Delete(parsed);
    free(json);
    cJSON_Delete(obj);

    printf("cjson verify: OK\n");
    return 0;
}
