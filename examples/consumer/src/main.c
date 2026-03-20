#include <stdio.h>
#include <string.h>
#include <cjson/cJSON.h>
#include <lz4.h>

int main(void) {
    printf("=== sea package manager: consumer app ===\n\n");

    /* cJSON: parse and print JSON */
    const char *json_str = "{\"name\":\"sea\",\"version\":\"1.0.0\",\"packages\":3}";
    cJSON *json = cJSON_Parse(json_str);
    if (json) {
        cJSON *name = cJSON_GetObjectItemCaseSensitive(json, "name");
        cJSON *pkgs = cJSON_GetObjectItemCaseSensitive(json, "packages");
        if (cJSON_IsString(name))
            printf("[cJSON] package: %s\n", name->valuestring);
        if (cJSON_IsNumber(pkgs))
            printf("[cJSON] packages installed: %d\n", (int)pkgs->valuedouble);
        cJSON_Delete(json);
    }

    /* lz4: compress and decompress */
    const char *src = "Hello from sea! Real libraries, real packages, real compression.";
    int src_size = (int)strlen(src) + 1;
    char compressed[4096];
    int comp_size = LZ4_compress_default(src, compressed, src_size, sizeof(compressed));
    if (comp_size > 0) {
        printf("[lz4]   compressed %d → %d bytes (%.0f%%)\n",
               src_size, comp_size, 100.0 * comp_size / src_size);
        char decompressed[4096];
        int dec_size = LZ4_decompress_safe(compressed, decompressed, comp_size, sizeof(decompressed));
        if (dec_size > 0 && strcmp(src, decompressed) == 0)
            printf("[lz4]   round-trip: OK\n");
    }

    printf("\nAll good.\n");
    return 0;
}
