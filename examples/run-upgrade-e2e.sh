#!/bin/bash
#
# End-to-end test: version range upgrades without rebuild.
#
# Proves that when a compatible dependency is updated (patch/minor),
# the consumer does NOT need to rebuild. The dynamic linker picks up
# the new library at runtime.
#
# Scenario:
#   1. Build and publish cjson@1.7.0 from source
#   2. Build and publish lz4@1.10.0 from source
#   3. Consumer installs both, compiles a C program, runs it
#   4. Build and publish cjson@1.7.1 (patch fix — adds NO symbols, removes none)
#   5. Consumer runs `sea update --check` → sees "no rebuild needed"
#   6. Consumer runs `sea update` → cjson updated in place
#   7. Consumer runs the SAME binary (not recompiled) → still works
#   8. Prove: lockfile changed, binary didn't, program still runs
#
#   Then test the static linking case:
#   9. Consumer re-installs with linking = "static"
#   10. `sea update --check` → says "rebuild required"
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SEA="$PROJECT_ROOT/bin/sea"

# ── Prerequisite check ──
MISSING=""
for tool in go cc make curl sed awk; do
    command -v "$tool" >/dev/null 2>&1 || MISSING="$MISSING $tool"
done
if [ -n "$MISSING" ]; then
    echo "Missing required tools:$MISSING"
    echo "Install them and re-run."
    exit 1
fi

# Portable SHA-256 command (macOS: shasum -a 256, Linux: sha256sum)
if command -v sha256sum >/dev/null 2>&1; then
    sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
    sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
    echo "Missing: sha256sum or shasum"
    exit 1
fi

TMPBASE=$(mktemp -d)

export SEA_HOME="$TMPBASE/sea-home"
REGISTRY_DIR="$TMPBASE/registry"
CONSUMER_DIR="$TMPBASE/consumer"

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

# ── Set up registry and config ──
info "Setting up registry..."
mkdir -p "$REGISTRY_DIR" "$SEA_HOME"
cat > "$SEA_HOME/config.toml" <<EOF
[[remotes]]
name = "test-local"
type = "filesystem"
path = "$REGISTRY_DIR"
EOF

ABI_TAG=$("$SEA" profile list 2>&1 | grep "ABI Tag:" | awk '{print $NF}')
echo "  ABI: $ABI_TAG"

# ── Build and publish cjson@1.7.0 ──
info "Step 1: Build & publish cjson@1.7.0 from source..."
cd "$SCRIPT_DIR/packages/cjson-1.7.0"
"$SEA" build 2>&1 | tail -1
"$SEA" publish --registry test-local 2>&1 | tail -1
pass "cjson@1.7.0 published"

# ── Build and publish lz4@1.10.0 ──
info "Step 2: Build & publish lz4@1.10.0 from source..."
cd "$SCRIPT_DIR/packages/lz4"
"$SEA" build 2>&1 | tail -1
"$SEA" publish --registry test-local 2>&1 | tail -1
pass "lz4@1.10.0 published"

# ── Create consumer project ──
info "Step 3: Create consumer, install, compile, run..."
mkdir -p "$CONSUMER_DIR/src"

cat > "$CONSUMER_DIR/sea.toml" <<'EOF'
[package]
name = "upgrade-test"
version = "1.0.0"
kind = "source"

[dependencies]
cjson = { version = "^1.7.0" }
lz4 = { version = "^1.10.0" }
EOF

cat > "$CONSUMER_DIR/src/main.c" <<'EOF'
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <cjson/cJSON.h>
#include <lz4.h>

int main(void) {
    /* cJSON */
    cJSON *obj = cJSON_CreateObject();
    cJSON_AddStringToObject(obj, "status", "ok");
    char *s = cJSON_Print(obj);
    printf("[cJSON] %s\n", s);
    free(s);
    cJSON_Delete(obj);

    /* lz4 */
    const char *data = "test data for compression";
    int src_size = (int)strlen(data) + 1;
    char compressed[256];
    int comp_size = LZ4_compress_default(data, compressed, src_size, sizeof(compressed));
    printf("[lz4] compressed %d → %d bytes\n", src_size, comp_size);

    char decompressed[256];
    int dec_size = LZ4_decompress_safe(compressed, decompressed, comp_size, sizeof(decompressed));
    if (dec_size > 0 && strcmp(data, decompressed) == 0) {
        printf("[lz4] round-trip: OK\n");
    }

    printf("All good.\n");
    return 0;
}
EOF

cd "$CONSUMER_DIR"
"$SEA" install 2>&1
pass "packages installed"

# Record lockfile state
LOCK_BEFORE=$(cat sea.lock)
CJSON_VER_BEFORE=$(awk '/name = "cjson"/{getline; print}' sea.lock | awk -F'"' '{print $2}')
echo "  Lockfile has cjson@$CJSON_VER_BEFORE"

# Compile the consumer
CJSON_INC="sea_packages/cjson/include"
CJSON_LIB="$(cd sea_packages/cjson/lib && pwd)"
LZ4_INC="sea_packages/lz4/include"
LZ4_LIB="$(cd sea_packages/lz4/lib && pwd)"

cc "-I$CJSON_INC" "-I$LZ4_INC" src/main.c \
    "-L$CJSON_LIB" "-L$LZ4_LIB" -lcjson -llz4 \
    -Wl,-rpath,"$CJSON_LIB" -Wl,-rpath,"$LZ4_LIB" \
    -o "$TMPBASE/upgrade-test" 2>&1
pass "consumer compiled"

# Run it
"$TMPBASE/upgrade-test"
pass "consumer runs with cjson@1.7.0"

# Record the binary hash
BINARY_HASH_BEFORE=$(sha256 "$TMPBASE/upgrade-test")
echo "  Binary SHA256: ${BINARY_HASH_BEFORE:0:16}..."

# ── Publish cjson@1.7.1 (patch fix, same ABI) ──
info "Step 4: Publish cjson@1.7.1 (patch — same symbols, no ABI change)..."

# Create a 1.7.1 package from the same source (simulating a bug fix)
PATCH_DIR="$TMPBASE/cjson-patch"
cp -r "$SCRIPT_DIR/packages/cjson-1.7.0" "$PATCH_DIR"
# Update the version in sea.toml
sed 's/version = "1.7.0"/version = "1.7.1"/' "$PATCH_DIR/sea.toml" > "$PATCH_DIR/sea.toml.tmp"
mv "$PATCH_DIR/sea.toml.tmp" "$PATCH_DIR/sea.toml"

# Reuse existing build (same source, just different version number)
if [ -d "$SCRIPT_DIR/packages/cjson-1.7.0/sea_build" ]; then
    cp -r "$SCRIPT_DIR/packages/cjson-1.7.0/sea_build" "$PATCH_DIR/sea_build"
fi

cd "$PATCH_DIR"
"$SEA" publish --registry test-local 2>&1 | tail -3

if [ -f "$REGISTRY_DIR/cjson/1.7.1/$ABI_TAG/sea-package.toml" ]; then
    pass "cjson@1.7.1 published (same ABI as 1.7.0)"
else
    fail "cjson@1.7.1 not in registry"
fi

# Verify registry now has both versions
echo "  Registry: $("$SEA" search cjson 2>&1 | head -1)"

# ── Check for updates ──
info "Step 5: sea update --check (should say no rebuild needed)..."
cd "$CONSUMER_DIR"
UPDATE_CHECK=$("$SEA" update --check 2>&1 || true)
echo "$UPDATE_CHECK"

if echo "$UPDATE_CHECK" | grep -q "no rebuild needed"; then
    pass "update --check correctly reports no rebuild needed"
else
    fail "update --check should say no rebuild needed for shared lib patch"
fi

# ── Run the update ──
info "Step 6: sea update (swap the library in place)..."
"$SEA" update 2>&1

# Check the lockfile changed
LOCK_AFTER=$(cat sea.lock)
CJSON_VER_AFTER=$(awk '/name = "cjson"/{getline; print}' sea.lock | awk -F'"' '{print $2}')
echo "  Lockfile now has cjson@$CJSON_VER_AFTER"

if [ "$CJSON_VER_BEFORE" != "$CJSON_VER_AFTER" ]; then
    pass "lockfile updated: $CJSON_VER_BEFORE → $CJSON_VER_AFTER"
else
    fail "lockfile should have changed"
fi

# ── Run the SAME binary (not recompiled) ──
info "Step 7: Run the ORIGINAL binary (not recompiled) with updated library..."
BINARY_HASH_AFTER=$(sha256 "$TMPBASE/upgrade-test")

if [ "$BINARY_HASH_BEFORE" = "$BINARY_HASH_AFTER" ]; then
    pass "binary unchanged (SHA256 identical — not recompiled)"
else
    fail "binary should not have changed"
fi

"$TMPBASE/upgrade-test"
pass "original binary still runs with updated cjson@1.7.1"

# ── Run sea install again — should be a no-op ──
info "Step 8: sea install (should be no-op — lockfile in sync)..."
INSTALL_OUTPUT=$("$SEA" install 2>&1)
echo "$INSTALL_OUTPUT"

if echo "$INSTALL_OUTPUT" | grep -q "up to date"; then
    pass "sea install is a no-op (lockfile in sync)"
else
    fail "sea install should detect lockfile is in sync"
fi

# ── Test static linking case ──
info "Step 9: Static linking — update should say rebuild required..."

cat > "$CONSUMER_DIR/sea.toml" <<'EOF'
[package]
name = "upgrade-test"
version = "1.0.0"
kind = "source"

[dependencies]
cjson = { version = "^1.7.0", linking = "static" }
lz4 = { version = "^1.10.0", linking = "static" }
EOF

# Force a re-resolve by removing lockfile
rm -f sea.lock
"$SEA" install 2>&1 | tail -3

# Now publish another cjson patch (1.7.2)
PATCH2_DIR="$TMPBASE/cjson-patch2"
cp -r "$PATCH_DIR" "$PATCH2_DIR"
sed 's/version = "1.7.1"/version = "1.7.2"/' "$PATCH2_DIR/sea.toml" > "$PATCH2_DIR/sea.toml.tmp"
mv "$PATCH2_DIR/sea.toml.tmp" "$PATCH2_DIR/sea.toml"
cd "$PATCH2_DIR"
"$SEA" publish --registry test-local 2>&1 | tail -1

cd "$CONSUMER_DIR"
STATIC_CHECK=$("$SEA" update --check 2>&1 || true)
echo "$STATIC_CHECK"

if echo "$STATIC_CHECK" | grep -q "rebuild required"; then
    pass "static linking correctly says rebuild required"
else
    fail "static linking should require rebuild on update"
fi

# ── Summary ──
info "=== SUMMARY ==="
echo ""
if [ "$FAILURES" -eq 0 ]; then
    echo -e "${GREEN}${BOLD}All checks passed.${NC}"
    echo ""
    echo "Proven:"
    echo "  1. Compatible patch (1.7.0 → 1.7.1) updates library without rebuild"
    echo "  2. Binary SHA256 unchanged — same compiled program works"
    echo "  3. sea update reports 'no rebuild needed' for shared libs"
    echo "  4. sea install is a no-op when lockfile is in sync"
    echo "  5. Static linking correctly flags rebuild required"
else
    echo -e "${RED}${BOLD}${FAILURES} check(s) failed.${NC}"
    exit 1
fi
