#!/bin/sh
set -e

BOOST_VERSION="1.87.0"
BOOST_VERSION_UNDERSCORE="1_87_0"
SRC_DIR="$SEA_PROJECT_DIR/_src"
ARCHIVE="$SRC_DIR/boost_${BOOST_VERSION_UNDERSCORE}.tar.gz"

mkdir -p "$SRC_DIR"

if [ ! -f "$ARCHIVE" ]; then
    echo "Downloading Boost ${BOOST_VERSION}..."
    curl -L -o "$ARCHIVE" \
        "https://github.com/boostorg/boost/releases/download/boost-${BOOST_VERSION}/boost-${BOOST_VERSION}-b2-nodocs.tar.gz"
fi

if [ ! -d "$SRC_DIR/boost_${BOOST_VERSION_UNDERSCORE}" ]; then
    echo "Extracting Boost..."
    tar xzf "$ARCHIVE" -C "$SRC_DIR"
fi

echo "Installing Boost headers..."
mkdir -p "$SEA_INSTALL_DIR/include"
cp -R "$SRC_DIR/boost-${BOOST_VERSION}/boost" "$SEA_INSTALL_DIR/include/"

echo "Boost headers installed to $SEA_INSTALL_DIR/include/boost"
