#!/bin/sh
set -e

VERSION="2024.12.30.00"
SRC_DIR="$SEA_PROJECT_DIR/_src"
ARCHIVE="$SRC_DIR/folly-v${VERSION}.tar.gz"

mkdir -p "$SRC_DIR"

if [ ! -f "$ARCHIVE" ]; then
    echo "Downloading folly ${VERSION}..."
    curl -L -o "$ARCHIVE" \
        "https://github.com/facebook/folly/archive/refs/tags/v${VERSION}.tar.gz"
fi

if [ ! -d "$SRC_DIR/folly-${VERSION}" ]; then
    echo "Extracting folly..."
    tar xzf "$ARCHIVE" -C "$SRC_DIR"
fi

echo "Building folly with CMake..."
BUILD_DIR="$SRC_DIR/folly-${VERSION}/_build"
mkdir -p "$BUILD_DIR"

# Build CMAKE_PREFIX_PATH from all dependencies in SEA_PACKAGES_DIR
CMAKE_PREFIX_PATH=""
for dep in fmt boost-headers double-conversion gflags glog libevent zstd snappy lz4 zlib; do
    if [ -d "$SEA_PACKAGES_DIR/$dep" ]; then
        if [ -z "$CMAKE_PREFIX_PATH" ]; then
            CMAKE_PREFIX_PATH="$SEA_PACKAGES_DIR/$dep"
        else
            CMAKE_PREFIX_PATH="$CMAKE_PREFIX_PATH;$SEA_PACKAGES_DIR/$dep"
        fi
    fi
done

cmake -S "$SRC_DIR/folly-${VERSION}" -B "$BUILD_DIR" \
    -DCMAKE_C_COMPILER="$CC" \
    -DCMAKE_CXX_COMPILER="$CXX" \
    -DCMAKE_INSTALL_PREFIX="$SEA_INSTALL_DIR" \
    -DCMAKE_PREFIX_PATH="$CMAKE_PREFIX_PATH" \
    -DCMAKE_BUILD_TYPE=Release

cmake --build "$BUILD_DIR" --parallel
cmake --install "$BUILD_DIR"

echo "folly installed to $SEA_INSTALL_DIR"
