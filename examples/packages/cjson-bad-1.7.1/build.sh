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

# Compile cJSON but use an objcopy/strip trick to hide cJSON_Compare
# This simulates someone removing a public API in a patch release
${CC:-cc} -c -O2 -fPIC cJSON.c -o cJSON.o

# Build a shared lib, then use the linker to strip cJSON_Compare from exports
if [ "$(uname)" = "Darwin" ]; then
    # First build with all symbols, then extract the symbol list, remove cJSON_Compare
    ${CC:-cc} -dynamiclib -o /tmp/libcjson_full.dylib cJSON.o
    # Get all exported symbols, remove cJSON_Compare
    nm -gU /tmp/libcjson_full.dylib | awk '{print $NF}' | grep '^_cJSON' | grep -v '_cJSON_Compare' > exports.txt
    # Rebuild with filtered exports
    ${CC:-cc} -dynamiclib -o "$SEA_INSTALL_DIR/lib/libcjson.dylib" \
        -exported_symbols_list exports.txt \
        cJSON.o
    rm -f /tmp/libcjson_full.dylib
else
    # Linux: use a version script
    ${CC:-cc} -shared -o /tmp/libcjson_full.so -fPIC cJSON.o
    nm -D /tmp/libcjson_full.so | awk '/T cJSON/{print $NF}' | grep -v 'cJSON_Compare' > exports.list
    echo "{ global:" > exports.map
    while read sym; do echo "  $sym;"; done < exports.list >> exports.map
    echo "  local: *; };" >> exports.map
    ${CC:-cc} -shared -o "$SEA_INSTALL_DIR/lib/libcjson.so" \
        -Wl,--version-script=exports.map \
        cJSON.o
    rm -f /tmp/libcjson_full.so
fi

ar rcs "$SEA_INSTALL_DIR/lib/libcjson.a" cJSON.o
cp cJSON.h "$SEA_INSTALL_DIR/include/cjson/"
echo "cJSON ${VERSION} built (BAD: cJSON_Compare removed)"
