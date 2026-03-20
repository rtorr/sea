#!/bin/sh
set -e

VERSION="2.2.2"
SRC_DIR="$SEA_PROJECT_DIR/_src"
ARCHIVE="$SRC_DIR/gflags-${VERSION}.tar.gz"

mkdir -p "$SRC_DIR"

if [ ! -f "$ARCHIVE" ]; then
    echo "Downloading gflags ${VERSION}..."
    curl -L -o "$ARCHIVE" \
        "https://github.com/gflags/gflags/archive/refs/tags/v${VERSION}.tar.gz"
fi

if [ ! -d "$SRC_DIR/gflags-${VERSION}" ]; then
    echo "Extracting gflags..."
    tar xzf "$ARCHIVE" -C "$SRC_DIR"
fi

echo "Building gflags with CMake..."
BUILD_DIR="$SRC_DIR/gflags-${VERSION}/_build"
mkdir -p "$BUILD_DIR"

cmake -S "$SRC_DIR/gflags-${VERSION}" -B "$BUILD_DIR" \
    -DCMAKE_C_COMPILER="$CC" \
    -DCMAKE_CXX_COMPILER="$CXX" \
    -DCMAKE_INSTALL_PREFIX="$SEA_INSTALL_DIR" \
    -DBUILD_SHARED_LIBS=ON \
    -DCMAKE_BUILD_TYPE=Release

cmake --build "$BUILD_DIR" --parallel
cmake --install "$BUILD_DIR"

echo "gflags installed to $SEA_INSTALL_DIR"
