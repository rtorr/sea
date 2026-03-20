#!/bin/sh
set -e
VERSION="1.3.1"
SRCDIR="$SEA_PROJECT_DIR/_src"
if [ ! -d "$SRCDIR/zlib-${VERSION}" ]; then
    mkdir -p "$SRCDIR"
    curl -sL "https://github.com/madler/zlib/releases/download/v${VERSION}/zlib-${VERSION}.tar.gz" \
        | tar xz -C "$SRCDIR"
fi
cd "$SRCDIR/zlib-${VERSION}"
mkdir -p _build && cd _build
cmake .. \
    -DCMAKE_INSTALL_PREFIX="$SEA_INSTALL_DIR" \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_C_COMPILER="${CC:-cc}" \
    2>&1
cmake --build . -j4 2>&1
cmake --install . 2>&1
echo "zlib ${VERSION} built"
