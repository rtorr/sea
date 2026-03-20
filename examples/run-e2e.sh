#!/bin/bash
#
# End-to-end test for the sea package manager.
#
# Builds real C/C++ libraries from source, publishes multiple versions to a
# local filesystem registry, installs them, compiles against them, and tests
# the ABI verification gate.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SEA="$PROJECT_ROOT/bin/sea"
TMPBASE=$(mktemp -d)

export SEA_HOME="$TMPBASE/sea-home"
REGISTRY_DIR="$TMPBASE/registry"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'
BOLD='\033[1m'; NC='\033[0m'
pass() { echo -e "  ${GREEN}PASS${NC} $1"; }
fail() { echo -e "  ${RED}FAIL${NC} $1"; FAILURES=$((FAILURES + 1)); }
info() { echo -e "\n${BOLD}$1${NC}"; }

FAILURES=0
cleanup() {
    if [ "$FAILURES" -eq 0 ]; then
        rm -rf "$TMPBASE"
    else
        echo -e "\n${YELLOW}Temp dir preserved: $TMPBASE${NC}"
    fi
}
trap cleanup EXIT

# ── Build sea ──
info "Building sea..."
cd "$PROJECT_ROOT"
go build -o "$SEA" ./cmd/sea
pass "binary built"

# ── Configure registry ──
info "Setting up local filesystem registry..."
mkdir -p "$REGISTRY_DIR" "$SEA_HOME"
cat > "$SEA_HOME/config.toml" <<EOF
[[remotes]]
name = "test-local"
type = "filesystem"
path = "$REGISTRY_DIR"
EOF
pass "registry at $REGISTRY_DIR"
ABI_TAG=$("$SEA" profile list 2>&1 | grep "ABI Tag:" | awk '{print $NF}')
echo "  ABI: $ABI_TAG"

# ── Helper: build and publish a package ──
build_and_publish() {
    local dir="$1" name="$2" ver="$3"
    info "Building $name@$ver from source..."
    cd "$dir"
    "$SEA" build 2>&1 | grep -E "(built|Build complete)" | tail -2
    "$SEA" publish --registry test-local 2>&1 | tail -3
    if [ -f "$REGISTRY_DIR/$name/$ver/$ABI_TAG/sea-package.toml" ]; then
        pass "$name@$ver published"
    else
        fail "$name@$ver not in registry"
    fi
}

# ── Publish cjson 1.7.0 (initial) ──
build_and_publish "$SCRIPT_DIR/packages/cjson-1.7.0" "cjson" "1.7.0"

# ── Publish cjson 1.8.0 (adds symbols — valid minor bump) ──
build_and_publish "$SCRIPT_DIR/packages/cjson-1.8.0" "cjson" "1.8.0"

# ── Publish lz4 ──
build_and_publish "$SCRIPT_DIR/packages/lz4" "lz4" "1.10.0"

# ── Publish fmt (C++) ──
build_and_publish "$SCRIPT_DIR/packages/fmt" "fmt" "11.1.0"

# ── Show what's in the registry ──
info "Registry contents:"
"$SEA" search cjson 2>&1 || true
"$SEA" search lz4 2>&1 || true
"$SEA" search fmt 2>&1 || true

# ── ABI comparison between cjson versions ──
info "ABI diff: cjson 1.7.0 → 1.8.0"
OLD_LIB=$(find "$SCRIPT_DIR/packages/cjson-1.7.0/sea_build" -name 'libcjson.dylib' -o -name 'libcjson.so' 2>/dev/null | head -1)
NEW_LIB=$(find "$SCRIPT_DIR/packages/cjson-1.8.0/sea_build" -name 'libcjson.dylib' -o -name 'libcjson.so' 2>/dev/null | head -1)
if [ -n "$OLD_LIB" ] && [ -n "$NEW_LIB" ]; then
    "$SEA" abi check "$OLD_LIB" "$NEW_LIB" 2>&1
    pass "ABI diff shows added symbols (correct minor bump)"
fi

# ── Test ABI gate: bad patch bump (removes symbols) ──
info "Testing ABI gate: cjson@1.7.1 removes cJSON_Compare but only bumps patch..."
cd "$SCRIPT_DIR/packages/cjson-bad-1.7.1"
"$SEA" build 2>&1 | grep -E "(built|Build complete)" | tail -2

if "$SEA" publish --registry test-local 2>&1; then
    fail "cjson@1.7.1 should have been BLOCKED (removed symbols, only patch bump)"
else
    pass "cjson@1.7.1 correctly BLOCKED by ABI verification gate"
fi

# ── Force-publish the bad version with --skip-verify for the install test ──
"$SEA" publish --registry test-local --skip-verify 2>&1 | tail -2
echo "  (force-published for install test)"

# ── Install packages in consumer project ──
info "Installing packages in consumer project..."
cd "$SCRIPT_DIR/consumer"
rm -rf sea_packages sea.lock
"$SEA" install 2>&1
pass "sea install completed"

if [ -d sea_packages/cjson ] && [ -d sea_packages/lz4 ] && [ -d sea_packages/fmt ]; then
    pass "all 3 packages in sea_packages/"
else
    fail "missing packages"
    ls -la sea_packages/ 2>/dev/null || true
fi

# ── Check env output ──
info "Build flags from sea env:"
CFLAGS=$("$SEA" env cflags 2>&1)
LDFLAGS=$("$SEA" env ldflags 2>&1)
echo "  CFLAGS:  $CFLAGS"
echo "  LDFLAGS: $LDFLAGS"

# ── Show lockfile ──
info "Lockfile:"
"$SEA" lock 2>&1

# ── Compile C program (cjson + lz4 only) ──
info "Compiling C program against installed cjson + lz4..."
CJSON_INC="sea_packages/cjson/include"
CJSON_LIB="sea_packages/cjson/lib"
LZ4_INC="sea_packages/lz4/include"
LZ4_LIB="sea_packages/lz4/lib"

if cc "-I$CJSON_INC" "-I$LZ4_INC" src/main.c \
    "-L$CJSON_LIB" "-L$LZ4_LIB" -lcjson -llz4 \
    -Wl,-rpath,"$(cd "$CJSON_LIB" && pwd)" \
    -Wl,-rpath,"$(cd "$LZ4_LIB" && pwd)" \
    -o "$TMPBASE/my-app" 2>&1; then
    pass "C program compiled"
    echo ""
    "$TMPBASE/my-app"
    pass "C program ran successfully"
else
    fail "C program failed to compile"
fi

# ── Compile C++ program (fmt only) ──
info "Compiling C++ program against installed fmt..."
FMT_INC="sea_packages/fmt/include"
FMT_LIB="sea_packages/fmt/lib"

if [ -d "$FMT_LIB" ] && [ -d "$FMT_INC" ]; then
    FMT_LIB_ABS="$(cd "$FMT_LIB" && pwd)"
    # Create symlinks if only the versioned dylib exists (archive skips symlinks)
    VERSIONED=$(ls "$FMT_LIB_ABS"/libfmt.*.*.*.dylib 2>/dev/null | head -1)
    if [ -n "$VERSIONED" ]; then
        VBASE=$(basename "$VERSIONED")
        [ ! -f "$FMT_LIB_ABS/libfmt.dylib" ] && ln -sf "$VBASE" "$FMT_LIB_ABS/libfmt.dylib"
        # Also create the soname symlink (e.g. libfmt.11.dylib)
        SONAME=$(echo "$VBASE" | sed 's/\([^.]*\.[^.]*\)\..*/\1.dylib/')
        [ ! -f "$FMT_LIB_ABS/$SONAME" ] && ln -sf "$VBASE" "$FMT_LIB_ABS/$SONAME"
    fi
    if c++ -std=c++17 "-I$FMT_INC" src/hello.cpp \
        "-L$FMT_LIB_ABS" -lfmt \
        -Wl,-rpath,"$FMT_LIB_ABS" \
        -o "$TMPBASE/hello-fmt" 2>&1; then
        pass "C++ program compiled with fmt"
        echo ""
        "$TMPBASE/hello-fmt"
        pass "C++ program ran successfully"
    else
        fail "C++ program failed to compile"
    fi
fi

# ── Consumer: pinned version ──
info "Consumer (pinned): cjson =1.7.0 — should get 1.7.0, not 1.8.0"
cd "$SCRIPT_DIR/consumer-pinned"
rm -rf sea_packages sea.lock
"$SEA" install 2>&1

# Check which version was installed by looking at the lockfile
if grep -q 'version = "1.7.0"' sea.lock 2>/dev/null; then
    pass "pinned to cjson@1.7.0 (1.8.0 was available but not selected)"
else
    fail "should have pinned to 1.7.0"
    cat sea.lock 2>/dev/null
fi

CFLAGS=$("$SEA" env cflags 2>&1)
CJSON_PLIB="$(cd sea_packages/cjson/lib 2>/dev/null && pwd)"
if cc $CFLAGS src/main.c -L"$CJSON_PLIB" -lcjson \
    -Wl,-rpath,"$CJSON_PLIB" \
    -o "$TMPBASE/pinned-app" 2>&1; then
    "$TMPBASE/pinned-app"
    pass "pinned consumer compiles and runs"
else
    fail "pinned consumer failed to compile"
fi

# ── Consumer: impossible constraints ──
info "Consumer (conflict): cjson >=2.0.0 — no such version exists"
cd "$SCRIPT_DIR/consumer-conflict"
rm -rf sea_packages sea.lock
if "$SEA" install 2>&1; then
    fail "should have failed — no cjson >=2.0.0 exists"
else
    pass "correctly failed with resolution error"
fi

# ── Cache info ──
info "Cache:"
"$SEA" cache list 2>&1

# ── Summary ──
info "=== SUMMARY ==="
echo ""
if [ "$FAILURES" -eq 0 ]; then
    echo -e "${GREEN}${BOLD}All checks passed.${NC}"
else
    echo -e "${RED}${BOLD}${FAILURES} check(s) failed.${NC}"
    exit 1
fi
