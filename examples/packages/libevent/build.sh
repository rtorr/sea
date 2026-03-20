#!/bin/sh
set -e

VERSION="2.1.12-stable"
SRC_DIR="$SEA_PROJECT_DIR/_src"
ARCHIVE="$SRC_DIR/libevent-release-${VERSION}.tar.gz"

mkdir -p "$SRC_DIR"

if [ ! -f "$ARCHIVE" ]; then
    echo "Downloading libevent ${VERSION}..."
    curl -L -o "$ARCHIVE" \
        "https://github.com/libevent/libevent/archive/refs/tags/release-${VERSION}.tar.gz"
fi

if [ ! -d "$SRC_DIR/libevent-release-${VERSION}" ]; then
    echo "Extracting libevent..."
    tar xzf "$ARCHIVE" -C "$SRC_DIR"
fi

echo "Building libevent with CMake..."
BUILD_DIR="$SRC_DIR/libevent-release-${VERSION}/_build"
mkdir -p "$BUILD_DIR"

cmake -S "$SRC_DIR/libevent-release-${VERSION}" -B "$BUILD_DIR" \
    -DCMAKE_C_COMPILER="$CC" \
    -DCMAKE_CXX_COMPILER="$CXX" \
    -DCMAKE_INSTALL_PREFIX="$SEA_INSTALL_DIR" \
    -DBUILD_SHARED_LIBS=ON \
    -DEVENT__DISABLE_TESTS=ON \
    -DCMAKE_BUILD_TYPE=Release

cmake --build "$BUILD_DIR" --parallel
cmake --install "$BUILD_DIR"

echo "libevent installed to $SEA_INSTALL_DIR"
