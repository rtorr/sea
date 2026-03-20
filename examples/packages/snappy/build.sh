#!/bin/sh
set -e

VERSION="1.2.1"
SRC_DIR="$SEA_PROJECT_DIR/_src"
ARCHIVE="$SRC_DIR/snappy-${VERSION}.tar.gz"

mkdir -p "$SRC_DIR"

if [ ! -f "$ARCHIVE" ]; then
    echo "Downloading snappy ${VERSION}..."
    curl -L -o "$ARCHIVE" \
        "https://github.com/google/snappy/archive/refs/tags/${VERSION}.tar.gz"
fi

if [ ! -d "$SRC_DIR/snappy-${VERSION}" ]; then
    echo "Extracting snappy..."
    tar xzf "$ARCHIVE" -C "$SRC_DIR"
fi

echo "Building snappy with CMake..."
BUILD_DIR="$SRC_DIR/snappy-${VERSION}/_build"
mkdir -p "$BUILD_DIR"

cmake -S "$SRC_DIR/snappy-${VERSION}" -B "$BUILD_DIR" \
    -DCMAKE_C_COMPILER="$CC" \
    -DCMAKE_CXX_COMPILER="$CXX" \
    -DCMAKE_INSTALL_PREFIX="$SEA_INSTALL_DIR" \
    -DBUILD_SHARED_LIBS=ON \
    -DSNAPPY_BUILD_TESTS=OFF \
    -DSNAPPY_BUILD_BENCHMARKS=OFF \
    -DCMAKE_BUILD_TYPE=Release

cmake --build "$BUILD_DIR" --parallel
cmake --install "$BUILD_DIR"

echo "snappy installed to $SEA_INSTALL_DIR"
