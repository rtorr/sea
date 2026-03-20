#!/bin/sh
set -e
VERSION="1.7.17"
SRCDIR="$SEA_PROJECT_DIR/_src"

if [ ! -d "$SRCDIR/cJSON-${VERSION}" ]; then
    mkdir -p "$SRCDIR"
    curl -sL "https://github.com/DaveGamble/cJSON/archive/refs/tags/v${VERSION}.tar.gz" \
        | tar xz -C "$SRCDIR"
fi

cd "$SRCDIR/cJSON-${VERSION}"
mkdir -p "$SEA_INSTALL_DIR/include/cjson" "$SEA_INSTALL_DIR/lib"

# Build original cJSON
${CC:-cc} -c -O2 -fPIC cJSON.c -o cJSON.o

# Add two new extension functions (simulating a minor version bump)
cat > cjson_ext.c <<'EXTC'
#include "cJSON.h"
#include <string.h>
#include <stdlib.h>

/* New in 1.8.0: convenience to get a string field or a default */
const char *cJSON_GetStringOrDefault(const cJSON *object, const char *name, const char *fallback) {
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(object, name);
    if (cJSON_IsString(item) && item->valuestring) return item->valuestring;
    return fallback;
}

/* New in 1.8.0: deep-equal comparison */
int cJSON_DeepEqual(const cJSON *a, const cJSON *b) {
    char *sa = cJSON_PrintUnformatted(a);
    char *sb = cJSON_PrintUnformatted(b);
    if (!sa || !sb) { free(sa); free(sb); return 0; }
    int eq = (strcmp(sa, sb) == 0);
    free(sa); free(sb);
    return eq;
}
EXTC
${CC:-cc} -c -O2 -fPIC cjson_ext.c -o cjson_ext.o

# Link together
${CC:-cc} -dynamiclib -o "$SEA_INSTALL_DIR/lib/libcjson.dylib" cJSON.o cjson_ext.o 2>/dev/null || \
${CC:-cc} -shared -o "$SEA_INSTALL_DIR/lib/libcjson.so" cJSON.o cjson_ext.o 2>/dev/null
ar rcs "$SEA_INSTALL_DIR/lib/libcjson.a" cJSON.o cjson_ext.o

# Header with new declarations
cp cJSON.h "$SEA_INSTALL_DIR/include/cjson/"
cat >> "$SEA_INSTALL_DIR/include/cjson/cJSON.h" <<'EXTH'

/* cJSON extensions added in 1.8.0 */
CJSON_PUBLIC(const char *) cJSON_GetStringOrDefault(const cJSON *object, const char *name, const char *fallback);
CJSON_PUBLIC(int) cJSON_DeepEqual(const cJSON *a, const cJSON *b);
EXTH
echo "cJSON ${VERSION}+extensions built (1.8.0)"
