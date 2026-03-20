#!/bin/sh
set -e

VERSION="1.7.17"
SRCDIR="$SEA_PROJECT_DIR/_src"
INSTALLDIR="$SEA_INSTALL_DIR"

# Download source if not cached
if [ ! -d "$SRCDIR/cJSON-${VERSION}" ]; then
    mkdir -p "$SRCDIR"
    curl -sL "https://github.com/DaveGamble/cJSON/archive/refs/tags/v${VERSION}.tar.gz" \
        | tar xz -C "$SRCDIR"
fi

cd "$SRCDIR/cJSON-${VERSION}"

# Build — cJSON is a single .c file, no cmake needed
mkdir -p "$INSTALLDIR/include/cjson" "$INSTALLDIR/lib"

${CC:-cc} -c -O2 -fPIC -DCJSON_EXPORT_SYMBOLS -DCJSON_API_VISIBILITY \
    cJSON.c -o cJSON.o

# Static library
ar rcs "$INSTALLDIR/lib/libcjson.a" cJSON.o

# Shared library
${CC:-cc} -shared -o "$INSTALLDIR/lib/libcjson.dylib" cJSON.o 2>/dev/null || \
${CC:-cc} -shared -o "$INSTALLDIR/lib/libcjson.so" cJSON.o 2>/dev/null || true

# Headers
cp cJSON.h "$INSTALLDIR/include/cjson/"

echo "cJSON ${VERSION} built successfully"
