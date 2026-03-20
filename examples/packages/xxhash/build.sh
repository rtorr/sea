#!/bin/sh
set -e
VERSION="0.8.3"
SRCDIR="$SEA_PROJECT_DIR/_src"
if [ ! -d "$SRCDIR/xxHash-${VERSION}" ]; then
    mkdir -p "$SRCDIR"
    curl -sL "https://github.com/Cyan4973/xxHash/archive/refs/tags/v${VERSION}.tar.gz" \
        | tar xz -C "$SRCDIR"
fi
cd "$SRCDIR/xxHash-${VERSION}"
mkdir -p _build && cd _build
cmake ../cmake_unofficial \
    -DCMAKE_INSTALL_PREFIX="$SEA_INSTALL_DIR" \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_C_COMPILER="${CC:-cc}" \
    -DBUILD_SHARED_LIBS=ON \
    -DXXHASH_BUILD_XXHSUM=OFF \
    2>&1
cmake --build . -j4 2>&1
cmake --install . 2>&1
echo "xxhash ${VERSION} built"
