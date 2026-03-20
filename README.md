# sea

A C/C++ package manager where versions mean compatibility, not hashes.

Sea fixes Conan 2's core problem: hash-based package identity that breaks on any config change regardless of actual ABI compatibility. Sea uses **semver + human-readable ABI tags** and automatically verifies that version numbers match actual symbol-level changes.

## Install

```bash
# macOS / Linux
curl -sSL https://github.com/rtorr/sea/releases/latest/download/sea-$(uname -s)-$(uname -m).tar.gz | tar xz
sudo mv sea /usr/local/bin/

# Or build from source
go install github.com/rtorr/sea/cmd/sea@latest
```

## Quick Start

```bash
# Set up the public package registry
sea remote add packages github-releases rtorr/sea-packages

# Create a project
mkdir myapp && cd myapp
sea init myapp

# Add dependencies
cat >> sea.toml << 'EOF'
[dependencies]
cjson = { version = "^1.7.0" }
lz4 = { version = "^1.10.0" }
EOF

# Install
sea install

# Use with any build system
cc $(sea env cflags) main.c $(sea env ldflags) -o myapp

# Or with CMake — one line in CMakeLists.txt:
# include(sea_packages/sea-cmake.cmake)
# find_package(cjson REQUIRED)
```

## What makes sea different

**Versions are the compatibility signal.** `^1.7.0` means "any 1.x that won't break my build." Not a hash of your compiler flags.

**ABI safety is automatic.** When you publish, sea extracts every exported symbol from your libraries and compares against the previous version. Remove a symbol? Sea blocks the publish unless you bump the major version. Add symbols? Minor bump required. No changes? Patch is fine.

**Compatible updates don't cascade rebuilds.** When a dependency publishes a security fix (1.7.0 → 1.7.1), your consumers run `sea update` and the shared library is swapped in place. No rebuild. The dynamic linker picks up the new version at runtime.

**Static linking doesn't leak symbols.** Sea detects when statically-linked dependency symbols leak into your shared library's export table and blocks the publish with a generated linker script to fix it.

## Commands

```
sea init [name]              Create a new project
sea install [pkg@ver]        Install dependencies
sea install --locked         Reproducible install from lockfile
sea update [name]            Update to latest compatible versions
sea update --check           Show available updates without installing
sea uninstall <name>         Remove a dependency
sea build                    Build from source (auto-detects CMake/Make/Meson)
sea publish                  Publish to a registry with ABI verification
sea publish --dry-run        See what would be published
sea publish init --expect    Declare expected platforms for multi-platform release
sea publish status           Show platform availability
sea abi check <old> <new>    Compare ABI between two library versions
sea abi symbols <lib>        List exported symbols
sea abi verify               Verify version bump matches ABI changes
sea env cflags               Output -I flags
sea env ldflags              Output -L and -l flags
sea env cmake-prefix         Output CMAKE_PREFIX_PATH
sea env shell                Output all vars for eval
sea profile list             Show available profiles
sea profile create <name>    Create from host detection
sea remote add/remove/list   Manage registries
sea search <query>           Search packages
sea cache list/clean/info    Manage local cache
```

## Registry

The default public registry is at [rtorr/sea-packages](https://github.com/rtorr/sea-packages). Packages are stored as GitHub Release assets — no clone needed.

```toml
# ~/.sea/config.toml
[[remotes]]
name = "packages"
type = "github-releases"
url = "rtorr/sea-packages"

# Per-package routing for private packages
[registry]
default = "packages"
[registry.packages]
my-internal-lib = "corp-registry"
```

Registry types: `github-releases`, `filesystem`, `artifactory`, `github`.

## Package Format

```toml
# sea.toml
[package]
name = "mylib"
version = "1.2.0"
kind = "source"        # source | prebuilt | header-only

[dependencies]
zlib = { version = "^1.3.0" }
openssl = { version = "^3.0.0", linking = "static" }

[features.ssl]
description = "TLS support"
[features.ssl.dependencies]
openssl = { version = "^3.0.0" }

[build]
script = "build.sh"    # optional — auto-detects CMake/Make/Meson
test = "test/verify.c" # optional — compile+link+run after build

[publish]
registry = "packages"
```

## Available Packages

| Package | Type | Description |
|---------|------|-------------|
| cjson | C | Ultralightweight JSON parser |
| lz4 | C | Fast compression |
| zlib | C | Compression (the original) |
| sqlite3 | C | Embedded SQL database |
| xxhash | C | Fast hashing |
| fmt | C++ | Modern formatting |
| spdlog | C++ | Fast logging (depends on fmt) |
| nlohmann-json | C++ header-only | JSON for Modern C++ |
| doctest | C++ header-only | Testing framework |

## License

MIT
