#!/bin/sh
set -e
VERSION="11.1.4"
SRCDIR="$SEA_PROJECT_DIR/_src"

if [ ! -d "$SRCDIR/fmt-${VERSION}" ]; then
    mkdir -p "$SRCDIR"
    curl -sL "https://github.com/fmtlib/fmt/archive/refs/tags/${VERSION}.tar.gz" \
        | tar xz -C "$SRCDIR"
fi

cd "$SRCDIR/fmt-${VERSION}"
mkdir -p _build && cd _build
cmake .. \
    -DCMAKE_INSTALL_PREFIX="$SEA_INSTALL_DIR" \
    -DCMAKE_BUILD_TYPE=Release \
    -DFMT_DOC=OFF -DFMT_TEST=OFF \
    -DBUILD_SHARED_LIBS=ON \
    -DCMAKE_CXX_COMPILER="${CXX:-c++}" \
    -DCMAKE_C_COMPILER="${CC:-cc}" \
    2>&1
cmake --build . -j4 2>&1
cmake --install . 2>&1
echo "fmt ${VERSION} built"
