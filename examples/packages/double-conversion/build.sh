#!/bin/sh
set -e

VERSION="3.3.0"
SRC_DIR="$SEA_PROJECT_DIR/_src"
ARCHIVE="$SRC_DIR/double-conversion-${VERSION}.tar.gz"

mkdir -p "$SRC_DIR"

if [ ! -f "$ARCHIVE" ]; then
    echo "Downloading double-conversion ${VERSION}..."
    curl -L -o "$ARCHIVE" \
        "https://github.com/google/double-conversion/archive/refs/tags/v${VERSION}.tar.gz"
fi

if [ ! -d "$SRC_DIR/double-conversion-${VERSION}" ]; then
    echo "Extracting double-conversion..."
    tar xzf "$ARCHIVE" -C "$SRC_DIR"
fi

echo "Building double-conversion with CMake..."
BUILD_DIR="$SRC_DIR/double-conversion-${VERSION}/_build"
mkdir -p "$BUILD_DIR"

cmake -S "$SRC_DIR/double-conversion-${VERSION}" -B "$BUILD_DIR" \
    -DCMAKE_C_COMPILER="$CC" \
    -DCMAKE_CXX_COMPILER="$CXX" \
    -DCMAKE_INSTALL_PREFIX="$SEA_INSTALL_DIR" \
    -DBUILD_SHARED_LIBS=ON \
    -DCMAKE_BUILD_TYPE=Release

cmake --build "$BUILD_DIR" --parallel
cmake --install "$BUILD_DIR"

echo "double-conversion installed to $SEA_INSTALL_DIR"
