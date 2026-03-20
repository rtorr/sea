#!/bin/sh
set -e

VERSION="1.5.6"
SRC_DIR="$SEA_PROJECT_DIR/_src"
ARCHIVE="$SRC_DIR/zstd-${VERSION}.tar.gz"

mkdir -p "$SRC_DIR"

if [ ! -f "$ARCHIVE" ]; then
    echo "Downloading zstd ${VERSION}..."
    curl -L -o "$ARCHIVE" \
        "https://github.com/facebook/zstd/archive/refs/tags/v${VERSION}.tar.gz"
fi

if [ ! -d "$SRC_DIR/zstd-${VERSION}" ]; then
    echo "Extracting zstd..."
    tar xzf "$ARCHIVE" -C "$SRC_DIR"
fi

echo "Building zstd with CMake..."
BUILD_DIR="$SRC_DIR/zstd-${VERSION}/_build"
mkdir -p "$BUILD_DIR"

# zstd's CMakeLists.txt is in build/cmake/ subdirectory
cmake -S "$SRC_DIR/zstd-${VERSION}/build/cmake" -B "$BUILD_DIR" \
    -DCMAKE_C_COMPILER="$CC" \
    -DCMAKE_CXX_COMPILER="$CXX" \
    -DCMAKE_INSTALL_PREFIX="$SEA_INSTALL_DIR" \
    -DCMAKE_BUILD_TYPE=Release

cmake --build "$BUILD_DIR" --parallel
cmake --install "$BUILD_DIR"

echo "zstd installed to $SEA_INSTALL_DIR"
