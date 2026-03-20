#!/bin/sh
set -e
VERSION="1.10.0"
SRCDIR="$SEA_PROJECT_DIR/_src"

if [ ! -d "$SRCDIR/lz4-${VERSION}" ]; then
    mkdir -p "$SRCDIR"
    curl -sL "https://github.com/lz4/lz4/archive/refs/tags/v${VERSION}.tar.gz" \
        | tar xz -C "$SRCDIR"
fi

cd "$SRCDIR/lz4-${VERSION}"
make -C lib clean 2>/dev/null || true
make -C lib -j4 CC="${CC:-cc}" PREFIX="$SEA_INSTALL_DIR" BUILD_SHARED=yes BUILD_STATIC=yes
make -C lib install CC="${CC:-cc}" PREFIX="$SEA_INSTALL_DIR" BUILD_SHARED=yes BUILD_STATIC=yes
echo "lz4 ${VERSION} built"
