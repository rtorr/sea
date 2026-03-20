#!/bin/sh
set -e
VERSION="1.15.1"
SRCDIR="$SEA_PROJECT_DIR/_src"
if [ ! -d "$SRCDIR/spdlog-${VERSION}" ]; then
    mkdir -p "$SRCDIR"
    curl -sL "https://github.com/gabime/spdlog/archive/refs/tags/v${VERSION}.tar.gz" \
        | tar xz -C "$SRCDIR"
fi
cd "$SRCDIR/spdlog-${VERSION}"

# Find fmt from sea_packages
FMT_DIR=""
if [ -d "$SEA_PACKAGES_DIR/fmt" ]; then
    FMT_DIR="$SEA_PACKAGES_DIR/fmt"
fi

mkdir -p _build && cd _build
cmake .. \
    -DCMAKE_INSTALL_PREFIX="$SEA_INSTALL_DIR" \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_CXX_COMPILER="${CXX:-c++}" \
    -DSPDLOG_BUILD_SHARED=ON \
    -DSPDLOG_FMT_EXTERNAL=ON \
    ${FMT_DIR:+-DCMAKE_PREFIX_PATH="$FMT_DIR"} \
    2>&1
cmake --build . -j4 2>&1
cmake --install . 2>&1
echo "spdlog ${VERSION} built"
