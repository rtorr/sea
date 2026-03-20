#!/bin/sh
set -e
VERSION="2.4.11"
SRCDIR="$SEA_PROJECT_DIR/_src"
if [ ! -d "$SRCDIR/doctest-${VERSION}" ]; then
    mkdir -p "$SRCDIR"
    curl -sL "https://github.com/doctest/doctest/archive/refs/tags/v${VERSION}.tar.gz" \
        | tar xz -C "$SRCDIR"
fi
mkdir -p "$SEA_INSTALL_DIR/include/doctest"
cp "$SRCDIR/doctest-${VERSION}/doctest/doctest.h" "$SEA_INSTALL_DIR/include/doctest/"
echo "doctest ${VERSION} installed (header-only)"
