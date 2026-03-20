#!/bin/sh
set -e
# SQLite amalgamation — single .c file, no build system needed
VERSION="3450300"
YEAR="2024"
SRCDIR="$SEA_PROJECT_DIR/_src"
if [ ! -d "$SRCDIR/sqlite-amalgamation-${VERSION}" ]; then
    mkdir -p "$SRCDIR"
    curl -sL "https://www.sqlite.org/${YEAR}/sqlite-amalgamation-${VERSION}.zip" \
        -o "$SRCDIR/sqlite.zip"
    cd "$SRCDIR" && unzip -qo sqlite.zip
fi
cd "$SRCDIR/sqlite-amalgamation-${VERSION}"
mkdir -p "$SEA_INSTALL_DIR/include" "$SEA_INSTALL_DIR/lib"

# Build shared + static
${CC:-cc} -c -O2 -fPIC -DSQLITE_DQS=0 -DSQLITE_THREADSAFE=1 sqlite3.c -o sqlite3.o
${CC:-cc} -dynamiclib -o "$SEA_INSTALL_DIR/lib/libsqlite3.dylib" sqlite3.o 2>/dev/null || \
${CC:-cc} -shared -o "$SEA_INSTALL_DIR/lib/libsqlite3.so" sqlite3.o 2>/dev/null
ar rcs "$SEA_INSTALL_DIR/lib/libsqlite3.a" sqlite3.o

cp sqlite3.h sqlite3ext.h "$SEA_INSTALL_DIR/include/"
echo "sqlite3 built"
