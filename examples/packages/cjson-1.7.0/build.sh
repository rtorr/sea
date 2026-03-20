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

${CC:-cc} -c -O2 -fPIC cJSON.c -o cJSON.o
${CC:-cc} -dynamiclib -o "$SEA_INSTALL_DIR/lib/libcjson.dylib" cJSON.o 2>/dev/null || \
${CC:-cc} -shared -o "$SEA_INSTALL_DIR/lib/libcjson.so" cJSON.o 2>/dev/null
ar rcs "$SEA_INSTALL_DIR/lib/libcjson.a" cJSON.o
cp cJSON.h "$SEA_INSTALL_DIR/include/cjson/"
echo "cJSON ${VERSION} built"
