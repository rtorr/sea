#!/bin/sh
set -e

VERSION="0.7.1"
SRC_DIR="$SEA_PROJECT_DIR/_src"
ARCHIVE="$SRC_DIR/glog-${VERSION}.tar.gz"

mkdir -p "$SRC_DIR"

if [ ! -f "$ARCHIVE" ]; then
    echo "Downloading glog ${VERSION}..."
    curl -L -o "$ARCHIVE" \
        "https://github.com/google/glog/archive/refs/tags/v${VERSION}.tar.gz"
fi

if [ ! -d "$SRC_DIR/glog-${VERSION}" ]; then
    echo "Extracting glog..."
    tar xzf "$ARCHIVE" -C "$SRC_DIR"
fi

echo "Building glog with CMake..."
BUILD_DIR="$SRC_DIR/glog-${VERSION}/_build"
mkdir -p "$BUILD_DIR"

CMAKE_PREFIX_PATH=""
if [ -d "$SEA_PACKAGES_DIR/gflags" ]; then
    CMAKE_PREFIX_PATH="$SEA_PACKAGES_DIR/gflags"
fi

cmake -S "$SRC_DIR/glog-${VERSION}" -B "$BUILD_DIR" \
    -DCMAKE_C_COMPILER="$CC" \
    -DCMAKE_CXX_COMPILER="$CXX" \
    -DCMAKE_INSTALL_PREFIX="$SEA_INSTALL_DIR" \
    -DCMAKE_PREFIX_PATH="$CMAKE_PREFIX_PATH" \
    -DCMAKE_BUILD_TYPE=Release

cmake --build "$BUILD_DIR" --parallel
cmake --install "$BUILD_DIR"

echo "glog installed to $SEA_INSTALL_DIR"
