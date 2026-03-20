#include <stdio.h>
#include <stdlib.h>
#include <cjson/cJSON.h>

int main(void) {
    cJSON *obj = cJSON_CreateObject();
    cJSON_AddStringToObject(obj, "pinned", "true");
    char *s = cJSON_Print(obj);
    printf("Pinned to cjson 1.7.0: %s\n", s);
    free(s);
    cJSON_Delete(obj);
    return 0;
}
