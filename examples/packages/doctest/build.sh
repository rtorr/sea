#!/bin/bash
# Header-only package: just copy the header to the install directory.
set -euo pipefail

SRC_URL="https://github.com/doctest/doctest/archive/refs/tags/v2.4.11.tar.gz"
SRC_DIR="$SEA_PROJECT_DIR/_src"

# Download once
if [ ! -d "$SRC_DIR/src" ]; then
    mkdir -p "$SRC_DIR"
    curl -sSL "$SRC_URL" | tar xz -C "$SRC_DIR"
    mv "$SRC_DIR"/doctest-* "$SRC_DIR/src"
fi

# Install: copy the single header
mkdir -p "$SEA_INSTALL_DIR/include/doctest"
cp "$SRC_DIR/src/doctest/doctest.h" "$SEA_INSTALL_DIR/include/doctest/"

echo "Installed doctest header to $SEA_INSTALL_DIR/include/doctest/"
